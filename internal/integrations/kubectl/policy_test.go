package kubectl

import (
	"context"
	"strings"
	"testing"

	"github.com/globlit/toolbox/internal/config"
	"github.com/globlit/toolbox/internal/runner"
	"github.com/globlit/toolbox/internal/sanitize"
	"github.com/globlit/toolbox/internal/testutil"
)

func init() { testutil.Hook() }
func settings() config.Kubectl {
	return config.Kubectl{DefaultNamespace: "apps", Namespaces: []string{"apps"}}
}

func TestGetPolicy(t *testing.T) {
	for _, r := range []string{"secrets", "secret", "configmaps", "all", "pods/secrets", "pods,secret", "/api/v1/secrets", "pods/status", "pods/exec", "pods/proxy", "--raw=/api/v1/secrets", "pods;delete", "pods\nsecrets", "roles.rbac.authorization.k8s.io", "nodes"} {
		t.Run(r, func(t *testing.T) {
			if _, err := getArgs(settings(), GetInput{Resource: r}); err == nil {
				t.Fatal("unsafe resource accepted")
			}
		})
	}
	for _, in := range []GetInput{{Resource: "pods", Name: "--raw=/"}, {Resource: "pods", Name: "demo/exec"}, {Resource: "pods", Name: "../secret"}, {Resource: "pods", Namespace: "elsewhere"}, {Resource: "pods", Namespace: "*"}, {Resource: "pods", Selector: "x\n--all-namespaces"}} {
		if _, err := getArgs(settings(), in); err == nil {
			t.Fatalf("unsafe get accepted: %+v", in)
		}
	}
	args, err := getArgs(settings(), GetInput{Resource: "po", Name: "demo-0", Selector: "app in (api,worker)"})
	if err != nil {
		t.Fatal(err)
	}
	if args[0] != "get" || !strings.HasPrefix(args[1], "--raw=/api/v1/namespaces/apps/pods/demo-0?") {
		t.Fatal(args)
	}
	k := settings()
	k.AllowClusterScoped = true
	if _, err := getArgs(k, GetInput{Resource: "nodes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := getArgs(k, GetInput{Resource: "nodes", Namespace: "apps"}); err == nil {
		t.Fatal("cluster scope silently ignored namespace")
	}
}

func TestLogsPolicy(t *testing.T) {
	for _, in := range []LogsInput{{Pod: "--help"}, {Pod: "deploy/app"}, {Pod: "app", Namespace: "other"}, {Pod: "app", Container: "x --follow"}, {Pod: "app", Tail: -1}, {Pod: "app", Tail: 2001}, {Pod: "app", Since: "48h"}, {Pod: "app", Since: "-1s"}, {Pod: "app", Since: "1ns"}} {
		if _, err := logsArgs(settings(), in); err == nil {
			t.Fatalf("unsafe logs accepted: %+v", in)
		}
	}
	args, err := logsArgs(settings(), LogsInput{Pod: "api-0"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tailLines=200", "sinceSeconds=3600", "follow=false", "/namespaces/apps/"} {
		if !strings.Contains(strings.Join(args, " "), want) {
			t.Fatal(args)
		}
	}
}

type spy struct{ calls int }

func (s *spy) Run(context.Context, []string) (runner.Result, error) {
	s.calls++
	return runner.Result{}, nil
}
func TestDeniedRequestsNeverExecute(t *testing.T) {
	r := &spy{}
	san, _ := sanitize.New(config.Redaction{}, nil)
	s := &Service{settings: settings(), runner: r, sanitizer: san}
	_, _ = s.Get(context.Background(), GetInput{Resource: "secrets"})
	_, _ = s.Logs(context.Background(), LogsInput{Pod: "../exec"})
	if r.calls != 0 {
		t.Fatal("denial reached executor")
	}
}

func TestServiceSanitizesSuccessAndFailure(t *testing.T) {
	s, err := New(testutil.Config(t))
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Get(context.Background(), GetInput{Resource: "pods"})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"annotation-canary", "env-canary"} {
		if strings.Contains(string(result.Data), secret) {
			t.Fatal("resource leak")
		}
	}
	if !strings.Contains(string(result.Data), "Running") || result.Redactions == 0 {
		t.Fatal(result)
	}
	result, err = s.Logs(context.Background(), LogsInput{Pod: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Text, "canary") || !strings.Contains(result.Text, "ready") {
		t.Fatal(result)
	}
	result, err = s.Get(context.Background(), GetInput{Resource: "pods", Name: "failure"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error == "" || len(result.Data) != 0 || strings.Contains(result.Diagnostics, "canary") {
		t.Fatal(result)
	}
	if _, err = s.Get(context.Background(), GetInput{Resource: "pods", Name: "malformed"}); err == nil {
		t.Fatal("malformed JSON not withheld")
	}
}

func FuzzPolicy(f *testing.F) {
	f.Add("pods", "demo", "apps", "app=api")
	f.Add("--raw=/", "../exec", "*", "x\n--all-namespaces")
	f.Fuzz(func(t *testing.T, r, n, ns, sel string) {
		args, err := getArgs(settings(), GetInput{Resource: r, Name: n, Namespace: ns, Selector: sel})
		if err == nil {
			if args[0] != "get" || !strings.Contains(strings.Join(args, " "), "/namespaces/apps/") {
				t.Fatal(args)
			}
			for _, arg := range args {
				if strings.ContainsAny(arg, "\r\n\x00") {
					t.Fatal(args)
				}
			}
		}
	})
}
