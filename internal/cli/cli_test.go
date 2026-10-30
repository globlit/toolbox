package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"toolbox/internal/config"

	"toolbox/internal/testutil"
)

func init() { testutil.Hook() }

func TestNoPassthrough(t *testing.T) {
	for _, cmd := range []string{"delete pods", "apply -f file", "patch pod app", "replace pod app", "edit pod app", "exec app -- sh", "debug app", "cp app:/tmp /tmp", "proxy", "config view --raw", "get pods --raw=/", "get pods -o jsonpath=@/etc/passwd", "get pods --watch", "get pods --server=https://evil", "get pods --as=admin", "get pods --token=CANARY", "get pods --kubeconfig=evil", "get pods --v=9", "logs app --follow", "logs app --all-containers", "get pods -- --raw=/"} {
		t.Run(cmd, func(t *testing.T) {
			var out, stderr bytes.Buffer
			args := append([]string{"kubectl"}, strings.Fields(cmd)...)
			code := Run(context.Background(), args, &out, &stderr)
			if code != 2 || out.Len() != 0 || strings.Contains(stderr.String(), "CANARY") || strings.Contains(stderr.String(), "cannot open") {
				t.Fatal("parser failed closed-policy check", code, stderr.String())
			}
		})
	}
}

func TestCLIUsesSanitizedService(t *testing.T) {
	c := testutil.Config(t)
	for _, cmd := range [][]string{{"get", "pods", "-n", "apps"}, {"logs", "api", "--tail=10"}, {"resources"}} {
		var out, stderr bytes.Buffer
		args := append([]string{"kubectl"}, cmd...)
		code := run(context.Background(), args, &out, &stderr, func() (config.Config, error) { return c, nil })
		if code != 0 || !json.Valid(out.Bytes()) || strings.Contains(out.String(), "canary") {
			t.Fatal(code, out.String(), stderr.String())
		}
	}
}

func TestParserRejectsDuplicateAndTrailingInputs(t *testing.T) {
	for _, args := range [][]string{{"get", "pods", "a", "b"}, {"get", "pods", "-n", "apps", "--namespace=apps"}, {"logs", "api", "--previous=true"}, {"get", "pods", "--selector="}, {"mcp", "anything"}} {
		_, err := parse(append([]string{"kubectl"}, args...))
		if err == nil {
			t.Fatal(args)
		}
	}
}

func TestRawAndForwardParsing(t *testing.T) {
	for _, args := range [][]string{{"get", "--raw=/api/v1/namespaces/apps/pods"}, {"get", "--raw", "/version"}, {"port-forward", "svc/api", "18080:80", "-n", "apps", "--duration", "1m"}, {"port-forward", "api", ":8080", ":8081"}} {
		r, err := parse(append([]string{"kubectl"}, args...))
		if err != nil || (r.mode != "raw" && r.mode != "port-forward") {
			t.Fatal(r, err)
		}
	}
	for _, args := range [][]string{{"delete", "--raw=/api/v1/namespaces/apps/pods"}, {"get", "pods", "--raw=/version"}, {"get", "--raw=/version", "-n", "apps"}, {"get", "--raw=/version", "--raw=/api"}, {"port-forward", "api", "8080:80", "--address=0.0.0.0"}, {"port-forward", "api", "8080:80", "--kubeconfig=evil"}, {"port-forward", "api", "8080:80", "--duration=0s"}, {"port-forward", "api", "8080:80", "--duration=2h"}} {
		if _, err := parse(append([]string{"kubectl"}, args...)); err == nil {
			t.Fatal(args)
		}
	}
}

func TestContextJSONAndAllNamespaceCLI(t *testing.T) {
	c := testutil.MultiContextConfig(t)
	for _, command := range []string{
		"get pods --context prod-a -A -o json",
		"--context=prod-b get pods --output=json",
		"get --context prod-a pods --all-namespaces -o=json",
		"get --raw=/api/v1/namespaces/billing/pods --context prod-b -o json",
		"logs api --context prod-b", "resources --context prod-a", "contexts",
	} {
		var out, stderr bytes.Buffer
		code := run(context.Background(), append([]string{"kubectl"}, strings.Fields(command)...), &out, &stderr, func() (config.Config, error) { return c, nil })
		if code != 0 || !json.Valid(out.Bytes()) || strings.Contains(out.String(), "canary") {
			t.Fatal(command, code, out.String(), stderr.String())
		}
		if strings.Contains(command, "-A") || strings.Contains(command, "--all-namespaces") {
			if !strings.Contains(out.String(), `"namespaces":["apps","observability"]`) || !strings.Contains(out.String(), `"context":"prod-a"`) {
				t.Fatal(out.String())
			}
		}
	}
	for _, command := range []string{
		"get pods -A -n apps", "get pods api -A", "get pods -A=true", "get pods -A --all-namespaces",
		"get pods -o yaml", "get pods -o json --output=json", "get pods -o jsonpath={.data}",
		"logs api -A", "logs api -o json", "get --raw=/version -A",
		"get pods --context", "get pods --context=", "get pods --context prod-a --context prod-b", "contexts --context prod-a",
	} {
		if _, err := parse(append([]string{"kubectl"}, strings.Fields(command)...)); err == nil {
			t.Fatal("invalid CLI accepted", command)
		}
	}
	for _, command := range []string{"get pods", "get pods --context unapproved", "get pods --context prod-b -n apps", "get nodes --context prod-b -A"} {
		var out, stderr bytes.Buffer
		code := run(context.Background(), append([]string{"kubectl"}, strings.Fields(command)...), &out, &stderr, func() (config.Config, error) { return c, nil })
		if code == 0 || out.Len() != 0 {
			t.Fatal("request did not fail closed", command, out.String())
		}
	}
	r, err := parse([]string{"kubectl", "port-forward", "api", ":8080", "--context", "prod-a"})
	if err != nil || r.forward.Context != "prod-a" {
		t.Fatal(r, err)
	}
}
