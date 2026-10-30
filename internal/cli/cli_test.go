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
