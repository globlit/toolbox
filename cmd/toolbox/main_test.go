package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"toolbox/internal/testutil"
)

func init() { testutil.Hook() }

func TestStdioEndToEnd(t *testing.T) {
	c := testutil.Config(t)
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
			t.Fatal("caller config accepted", string(out))
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
	if err != nil || len(list.Tools) != 3 {
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
}
