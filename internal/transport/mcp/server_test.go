package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"toolbox/internal/integrations/kubectl"
	"toolbox/internal/testutil"
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
			if err != nil || len(list.Tools) != 3 {
				t.Fatal(list, err)
			}
			for _, tool := range list.Tools {
				if !tool.Annotations.ReadOnlyHint {
					t.Fatal("missing read-only hint")
				}
			}
			for _, tc := range []struct {
				name    string
				args    map[string]any
				failure bool
			}{
				{"kubectl_get", map[string]any{"resource": "pods"}, false},
				{"kubectl_logs", map[string]any{"pod": "api"}, false},
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
