package kubectl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globlit/toolbox/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestKindEndToEnd(t *testing.T) {
	path, binary := os.Getenv("TOOLBOX_KIND_CONFIG"), os.Getenv("TOOLBOX_KIND_BINARY")
	if path == "" || binary == "" {
		t.Skip("run scripts/e2e/kind.sh for an isolated real cluster")
	}
	base, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	fixtureBytes, err := os.ReadFile("../../sanitize/testdata/complex_logs.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Closed bool `json:"closed"`
	}
	if err := json.Unmarshal(fixtureBytes, &fixtures); err != nil {
		t.Fatal(err)
	}
	logMarkers := []string{"INFO healthy-after"}
	for i, fixture := range fixtures {
		if fixture.Closed {
			logMarkers = append(logMarkers, fmt.Sprintf("INFO checkpoint-%03d ready", i))
		}
	}
	check := func(t *testing.T, output []byte, denied bool, err error, want, absent []string) {
		t.Helper()
		if (err != nil) != denied {
			t.Fatalf("denied=%v error=%v output=%s", denied, err, output)
		}
		for _, value := range append(absent, "SYNTHETIC_CM_SECRET", "SYNTHETIC_SECRET_7mQ2", "SYNTHETIC_SECRET_ONLY") {
			if strings.Contains(string(output), value) {
				t.Fatal("unexpected output", value, string(output))
			}
		}
		if !denied {
			for _, value := range want {
				if !strings.Contains(string(output), value) {
					t.Fatal("missing result", value, string(output))
				}
			}
		}
	}
	for _, scenario := range []struct {
		name                    string
		disable                 bool
		allow, deny, namespaces []string
		blocked                 bool
	}{
		{name: "default"},
		{name: "allow-all", allow: []string{"*"}},
		{name: "exclude-private", deny: []string{"private"}},
		{name: "allow-apps", allow: []string{"apps"}},
		{name: "restricted-scope", allow: []string{"*"}, namespaces: []string{"apps"}},
		{name: "global-disable", disable: true, allow: []string{"*"}, deny: []string{"private"}, blocked: true},
		{name: "exclude-all", deny: []string{"*"}, blocked: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			c := base
			c.Kubectl.Contexts = append([]config.Context{}, base.Kubectl.Contexts...)
			c.Kubectl.DisableConfigMaps = scenario.disable
			for i := range c.Kubectl.Contexts {
				c.Kubectl.Contexts[i].ConfigMapNamespaces = scenario.allow
				c.Kubectl.Contexts[i].DisabledConfigMapNamespaces = scenario.deny
				if scenario.namespaces != nil {
					c.Kubectl.Contexts[i].Namespaces = scenario.namespaces
				}
			}
			bytes, _ := json.Marshal(c)
			configPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(configPath, bytes, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			client := mcp.NewClient(&mcp.Implementation{Name: "kind-test", Version: "1"}, nil)
			session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(binary, "--config", configPath, "mcp")}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			privateDenied := scenario.blocked || len(scenario.deny) > 0 || (len(scenario.allow) > 0 && scenario.allow[0] != "*") || scenario.namespaces != nil
			absent := []string{}
			if privateDenied {
				absent = append(absent, "settings-private")
			}
			for _, test := range []struct {
				name         string
				cli          []string
				tool         string
				args         map[string]any
				denied       bool
				want, absent []string
			}{
				{"named", []string{"get", "cm", "settings-apps", "-n", "apps", "-o", "json"}, "kubectl_get", map[string]any{"resource": "cm", "name": "settings-apps", "namespace": "apps"}, scenario.blocked, []string{"settings-apps", "readable-setting", "[REDACTED]"}, nil},
				{"all", []string{"get", "configmaps", "-A"}, "kubectl_get", map[string]any{"resource": "configmaps", "all_namespaces": true}, scenario.blocked, []string{"settings-apps", "readable-setting"}, absent},
				{"private", []string{"get", "configmaps", "-n", "private"}, "kubectl_get", map[string]any{"resource": "configmaps", "namespace": "private"}, privateDenied, []string{"settings-private", "readable-setting"}, nil},
				{"raw-private", []string{"get", "--raw", "/api/v1/namespaces/private/configmaps"}, "kubectl_raw", map[string]any{"path": "/api/v1/namespaces/private/configmaps"}, privateDenied, []string{"settings-private", "readable-setting"}, nil},
				{"raw-all", []string{"get", "--raw", "/api/v1/configmaps"}, "kubectl_raw", map[string]any{"path": "/api/v1/configmaps"}, privateDenied, []string{"settings-private", "settings-apps", "readable-setting"}, nil},
				{"secrets", []string{"get", "secrets", "-n", "apps"}, "kubectl_get", map[string]any{"resource": "secrets", "namespace": "apps"}, true, nil, nil},
				{"logs", []string{"logs", "log-fixture", "-n", "apps", "--tail", "1000"}, "kubectl_logs", map[string]any{"pod": "log-fixture", "namespace": "apps", "tail": 1000}, false, logMarkers, nil},
			} {
				t.Run(test.name, func(t *testing.T) {
					args := append([]string{"--config", configPath, "kubectl"}, test.cli...)
					args = append(args, "--context", "reader")
					out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
					check(t, out, test.denied, err, test.want, test.absent)
					test.args["context"] = "reader"
					result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: test.tool, Arguments: test.args})
					if err == nil && result.IsError {
						err = errToolDenied{}
					}
					encoded, _ := json.Marshal(result)
					check(t, encoded, test.denied, err, test.want, test.absent)
				})
			}
			// Kubernetes rejects wildcard lists for the namespace-only service account.
			for _, args := range [][]string{{"get", "pods", "-n", "private"}, {"get", "pods", "-A"}} {
				command := append([]string{"--config", configPath, "kubectl"}, args...)
				command = append(command, "--context", "limited")
				out, err := exec.CommandContext(ctx, binary, command...).CombinedOutput()
				// Explicit scope restriction rejects before Kubernetes in this scenario.
				denied := scenario.namespaces == nil || args[len(args)-1] != "-A"
				check(t, out, denied, err, nil, nil)
				input := map[string]any{"context": "limited", "resource": "pods"}
				if args[len(args)-1] == "-A" {
					input["all_namespaces"] = true
				} else {
					input["namespace"] = "private"
				}
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_get", Arguments: input})
				if err == nil && result.IsError {
					err = errToolDenied{}
				}
				encoded, _ := json.Marshal(result)
				check(t, encoded, denied, err, nil, nil)
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_get", Arguments: map[string]any{"context": "limited", "resource": "pods", "namespace": "apps"}})
			if err != nil || result.IsError {
				t.Fatal("namespace-bound reader failed", err, result)
			}
			for _, verb := range []string{"delete", "patch", "apply", "edit", "exec"} {
				out, err := exec.CommandContext(ctx, binary, "--config", configPath, "kubectl", verb, "configmap", "settings-apps", "--context", "reader").CombinedOutput()
				check(t, out, true, err, nil, nil)
			}
		})
	}
	for _, args := range [][]string{{"delete", "configmaps"}, {"get", "secrets"}} {
		command := []string{"--kubeconfig", base.Kubectl.Kubeconfig, "--context", "reader", "auth", "can-i"}
		command = append(command, args...)
		command = append(command, "-n", "apps")
		out, err := exec.Command(base.Kubectl.Binary, command...).CombinedOutput()
		if err == nil || strings.TrimSpace(string(out)) != "no" {
			t.Fatal("reader RBAC is too broad", args, string(out), err)
		}
	}
}

type errToolDenied struct{}

func (errToolDenied) Error() string { return "tool rejected request" }
