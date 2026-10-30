package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/globlit/toolbox/internal/integrations/kubectl"
	"github.com/globlit/toolbox/internal/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { testutil.Hook() }

func TestMCPDiscoveryAndPolicy(t *testing.T) {
	for _, version := range []string{"2025-06-18", "2025-11-25", "2026-07-28"} {
		t.Run(version, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			service, err := kubectl.New(testutil.Config(t))
			if err != nil {
				t.Fatal(err)
			}
			a, b := mcp.NewInMemoryTransports()
			ss, err := New(service).Connect(ctx, a, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			cs, err := client.Connect(ctx, b, &mcp.ClientSessionOptions{ProtocolVersion: version})
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			list, err := cs.ListTools(ctx, nil)
			if err != nil || len(list.Tools) != 9 {
				t.Fatal(list, err)
			}
			for _, tool := range list.Tools {
				wantReadOnly := tool.Name != "kubectl_port_forward" && tool.Name != "kubectl_port_forward_stop"
				if tool.Annotations.ReadOnlyHint != wantReadOnly {
					t.Fatal("incorrect read-only hint", tool.Name)
				}
			}
			for _, tc := range []struct {
				name    string
				args    map[string]any
				failure bool
			}{
				{"kubectl_get", map[string]any{"resource": "pods"}, false},
				{"kubectl_logs", map[string]any{"pod": "api"}, false},
				{"kubectl_raw", map[string]any{"path": "/api/v1/namespaces/apps/pods"}, false},
				{"kubectl_raw", map[string]any{"path": "/api/v1/namespaces/apps/secrets"}, true},
				{"kubectl_raw", map[string]any{"path": "/api/v1/namespaces/apps/pods", "method": "DELETE"}, true},
				{"kubectl_port_forward", map[string]any{"target": "api", "ports": []string{":8080"}}, true},
				{"kubectl_get", map[string]any{"resource": "secrets"}, true},
				{"kubectl_get", map[string]any{"resource": "pods", "namespace": "other"}, true},
				{"kubectl_get", map[string]any{"resource": "pods", "args": []string{"--raw=/"}}, true},
				{"kubectl_get", map[string]any{"resource": "pods", "name": "failure"}, true},
				{"kubectl_logs", map[string]any{"pod": "app", "follow": true}, true},
			} {
				result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
				if err != nil {
					if tc.failure {
						continue
					}
					t.Fatal(err)
				}
				if result.IsError != tc.failure {
					t.Fatalf("%+v: %+v", tc, result)
				}
				encoded, _ := json.Marshal(result)
				if strings.Contains(string(encoded), "canary") {
					t.Fatalf("MCP leaked: %s", encoded)
				}
			}
		})
	}
}

func TestMCPContextSelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	service, err := kubectl.New(testutil.MultiContextConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	a, b := mcp.NewInMemoryTransports()
	ss, err := New(service).Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, tc := range []struct {
		name     string
		args     map[string]any
		failure  bool
		contains string
	}{
		{"kubectl_contexts", map[string]any{}, false, `"name":"prod-a"`},
		{"kubectl_resources", map[string]any{"context": "prod-b"}, false, `"namespaces":["billing"]`},
		{"kubectl_get", map[string]any{"resource": "pods", "context": "prod-a", "all_namespaces": true}, false, `"namespaces":["apps","observability"]`},
		{"kubectl_get", map[string]any{"resource": "pods", "context": "prod-b"}, false, `"context":"prod-b"`},
		{"kubectl_logs", map[string]any{"pod": "api", "context": "prod-b"}, false, `"context":"prod-b"`},
		{"kubectl_raw", map[string]any{"path": "/api/v1/namespaces/billing/pods", "context": "prod-b"}, false, `"context":"prod-b"`},
		{"kubectl_get", map[string]any{"resource": "pods"}, true, ""},
		{"kubectl_get", map[string]any{"resource": "pods", "context": "unapproved"}, true, ""},
		{"kubectl_get", map[string]any{"resource": "pods", "context": "prod-b", "namespace": "apps"}, true, ""},
		{"kubectl_get", map[string]any{"resource": "pods", "context": "prod-b", "all_namespaces": true, "namespace": "billing"}, true, ""},
		{"kubectl_get", map[string]any{"resource": "pods", "context": "prod-b", "all_namespaces": true, "name": "api"}, true, ""},
		{"kubectl_get", map[string]any{"resource": "pods", "context": "prod-b", "all_namespaces": "true"}, true, ""},
		{"kubectl_get", map[string]any{"resource": "secrets", "context": "prod-a", "all_namespaces": true}, true, ""},
		{"kubectl_get", map[string]any{"resource": "pods", "context": "prod-b", "kubeconfig": "/tmp/evil"}, true, ""},
		{"kubectl_get", map[string]any{"resource": "pods", "context": "prod-b", "output": "jsonpath=@/etc/passwd"}, true, ""},
		{"kubectl_logs", map[string]any{"pod": "api", "context": "prod-a", "all_namespaces": true}, true, ""},
		{"kubectl_raw", map[string]any{"path": "/api/v1/namespaces/apps/pods", "context": "prod-b"}, true, ""},
		{"kubectl_resources", map[string]any{}, true, ""},
		{"kubectl_port_forward", map[string]any{"target": "api", "ports": []string{":8080"}, "context": "prod-b"}, true, ""},
	} {
		out, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		if err != nil {
			if tc.failure {
				continue
			}
			t.Fatal(tc, err)
		}
		encoded, _ := json.Marshal(out.StructuredContent)
		if out.IsError != tc.failure || (!tc.failure && !strings.Contains(string(encoded), tc.contains)) || strings.Contains(string(encoded), "canary") {
			t.Fatal(tc, out, string(encoded))
		}
	}
	started, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_port_forward", Arguments: map[string]any{"target": "api", "ports": []string{":8080"}, "context": "prod-a"}})
	if err != nil || started.IsError {
		t.Fatal(started, err)
	}
	bdata, _ := json.Marshal(started.StructuredContent)
	var tunnel kubectl.ForwardResult
	if json.Unmarshal(bdata, &tunnel) != nil || tunnel.Context != "prod-a" {
		t.Fatal(string(bdata))
	}
	stopped, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_port_forward_stop", Arguments: map[string]any{"id": tunnel.ID}})
	if err != nil || stopped.IsError {
		t.Fatal(stopped, err)
	}
}
