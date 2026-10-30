package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"toolbox/internal/integrations/kubectl"
	"toolbox/internal/testutil"
)

func init() { testutil.Hook() }

func TestStdioEndToEnd(t *testing.T) {
	c := testutil.Config(t)
	c.Kubectl.AllowPortForward = true
	c.Kubectl.PortForwardMaxSeconds = 5
	binary, pinned := testutil.BuildGateway(t, c)
	approved, err := os.ReadFile(pinned)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	script := filepath.Join(dir, "attacker.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf executed > '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	attacker := c
	attacker.Kubectl.Binary = script
	payload, _ := json.Marshal(attacker)
	attackPath := filepath.Join(dir, "attacker.json")
	_ = os.WriteFile(attackPath, payload, 0600)
	for _, args := range [][]string{{"--config", attackPath, "kubectl", "get", "pods"}, {"mcp", "--config", attackPath}, {"kubectl", "get", "pods", "--config=" + attackPath}} {
		if out, err := exec.Command(binary, args...).CombinedOutput(); err == nil {
			t.Fatal("non-native configured executable accepted", string(out))
		}
	}
	cmd := exec.Command(binary, "kubectl", "get", "pods")
	cmd.Env = append(os.Environ(), "TOOLBOX_CONFIG="+attackPath, "TOOLBOX_CONFIG_PATH="+attackPath, "KUBECONFIG="+attackPath, "HOME="+dir, "USERPROFILE="+dir, "ProgramData="+dir)
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "Running") {
		t.Fatal("environment changed trust anchors", string(out), err)
	}
	if err := os.WriteFile(pinned, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(binary, "kubectl", "get", "pods").CombinedOutput(); err == nil || !strings.Contains(string(out), "integrity check failed") {
		t.Fatal("replaced pinned config accepted", string(out), err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("attacker executable ran")
	}
	if err := os.WriteFile(pinned, approved, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-smoke", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(binary, "mcp")}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 9 {
		t.Fatal(list, err)
	}
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_logs", Arguments: map[string]any{"pod": "api"}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "canary") || !strings.Contains(string(b), "ready") {
		t.Fatal(string(b))
	}
	r, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_get", Arguments: map[string]any{"resource": "secrets"}})
	if err == nil && !r.IsError {
		t.Fatal("denied read succeeded over stdio")
	}
	r, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_raw", Arguments: map[string]any{"path": "/api/v1/namespaces/apps/pods"}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	b, _ = json.Marshal(r)
	if strings.Contains(string(b), "canary") || !strings.Contains(string(b), "Running") {
		t.Fatal(string(b))
	}
	start := func() kubectl.ForwardResult {
		t.Helper()
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_port_forward", Arguments: map[string]any{"target": "api", "ports": []string{":8080"}}})
		if err != nil || r.IsError {
			t.Fatal(r, err)
		}
		b, _ := json.Marshal(r.StructuredContent)
		var forward kubectl.ForwardResult
		if json.Unmarshal(b, &forward) != nil || forward.Status != "active" || len(forward.Ports) != 1 {
			t.Fatal(string(b))
		}
		return forward
	}
	forward := start()
	address := fmt.Sprintf("127.0.0.1:%d", forward.Ports[0].Local)
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(time.Second))
	conn.Write([]byte("test"))
	echo := make([]byte, 4)
	if _, err = io.ReadFull(conn, echo); err != nil || string(echo) != "test" {
		t.Fatal(string(echo), err)
	}
	conn.Close()
	r, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_port_forward_stop", Arguments: map[string]any{"id": forward.ID}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	if conn, err = net.DialTimeout("tcp", address, time.Second); err == nil {
		conn.Close()
		t.Fatal("stopped tunnel is listening")
	}
	forward = start()
	address = fmt.Sprintf("127.0.0.1:%d", forward.Ports[0].Local)
	if err = cs.Close(); err != nil {
		t.Fatal(err)
	}
	if conn, err = net.DialTimeout("tcp", address, time.Second); err == nil {
		conn.Close()
		t.Fatal("MCP exit left tunnel listening")
	}
	output, err := exec.Command(binary, "kubectl", "port-forward", "api", ":8080", "--duration=1s").CombinedOutput()
	if err != nil {
		t.Fatal(string(output), err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	var active, expired kubectl.ForwardResult
	if decoder.Decode(&active) != nil || decoder.Decode(&expired) != nil || active.Status != "active" || expired.Status != "expired" {
		t.Fatal(string(output))
	}
}

func TestProvisionedMultipleContexts(t *testing.T) {
	binary, _ := testutil.BuildGateway(t, testutil.MultiContextConfig(t))
	alternate := testutil.Config(t)
	alternatePath := filepath.Join(t.TempDir(), "alternate.json")
	content, _ := json.Marshal(alternate)
	if err := os.WriteFile(alternatePath, content, 0600); err != nil {
		t.Fatal(err)
	}
	selected, err := exec.Command(binary, "--config", alternatePath, "kubectl", "get", "pods").CombinedOutput()
	if err != nil || !strings.Contains(string(selected), `"context":"reader"`) {
		t.Fatal("explicit config did not override built-in default", string(selected), err)
	}

	out, err := exec.Command(binary, "kubectl", "get", "pods", "--context", "prod-a", "-A", "-o", "json").CombinedOutput()
	if err != nil || !strings.Contains(string(out), `"context":"prod-a"`) || !strings.Contains(string(out), `"namespaces":["apps","observability"]`) || strings.Contains(string(out), "canary") {
		t.Fatal(string(out), err)
	}
	for _, args := range [][]string{
		{"kubectl", "get", "pods"},
		{"kubectl", "get", "pods", "--context", "unapproved", "-A"},
		{"kubectl", "get", "pods", "--context", "prod-b", "-n", "apps"},
	} {
		if out, err := exec.Command(binary, args...).CombinedOutput(); err == nil {
			t.Fatal("provisioned policy bypass", args, string(out))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "multi-context-smoke", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(binary, "mcp")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, name := range []string{"prod-a", "prod-b"} {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_get", Arguments: map[string]any{"resource": "pods", "context": name, "all_namespaces": true}})
		if err != nil || result.IsError {
			t.Fatal(result, err)
		}
		encoded, _ := json.Marshal(result.StructuredContent)
		var r kubectl.Result
		if json.Unmarshal(encoded, &r) != nil || r.Context != name || len(r.Namespaces) == 0 || strings.Contains(string(encoded), "canary") {
			t.Fatal(string(encoded))
		}
	}
}

func TestOrdinaryBuildWithExplicitConfig(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "toolbox")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-p=2", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	c := testutil.MultiContextConfig(t)
	path := filepath.Join(dir, "config.json")
	b, _ := json.Marshal(c)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(ctx, binary, "--config", path, "kubectl", "get", "pods", "--context", "prod-b", "-A", "-o", "json").CombinedOutput()
	if err != nil || !strings.Contains(string(out), `"context":"prod-b"`) || strings.Contains(string(out), "canary") {
		t.Fatal(string(out), err)
	}
	noConfig := exec.CommandContext(ctx, binary, "kubectl", "get", "pods")
	noConfig.Env = append(os.Environ(), "TOOLBOX_CONFIG="+path, "KUBECONFIG="+c.Kubectl.Kubeconfig)
	if out, err := noConfig.CombinedOutput(); err == nil || !strings.Contains(string(out), "configuration required") {
		t.Fatal("environment selected runtime config", string(out), err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "runtime-config-smoke", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(binary, "mcp", "--config", path)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, tc := range []struct {
		tool    string
		args    map[string]any
		failure bool
	}{
		{"kubectl_get", map[string]any{"context": "prod-a", "resource": "pods", "all_namespaces": true}, false},
		{"kubectl_get", map[string]any{"context": "prod-b", "resource": "secrets"}, true},
		{"kubectl_get", map[string]any{"context": "prod-a", "resource": "pods", "config": path}, true},
		{"kubectl_get", map[string]any{"context": "prod-a", "resource": "pods", "method": "DELETE"}, true},
	} {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
		if err != nil {
			if tc.failure {
				continue
			}
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(result)
		if result.IsError != tc.failure || strings.Contains(string(encoded), "canary") {
			t.Fatal(tc, string(encoded))
		}
	}
	started, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_port_forward", Arguments: map[string]any{"context": "prod-a", "target": "api", "ports": []string{":8080"}}})
	if err != nil || started.IsError {
		t.Fatal(started, err)
	}
	b, _ = json.Marshal(started.StructuredContent)
	var tunnel kubectl.ForwardResult
	if json.Unmarshal(b, &tunnel) != nil || tunnel.Context != "prod-a" || tunnel.Status != "active" {
		t.Fatal(string(b))
	}
	stopped, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_port_forward_stop", Arguments: map[string]any{"id": tunnel.ID}})
	if err != nil || stopped.IsError {
		t.Fatal(stopped, err)
	}
}
