package kubectl

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"toolbox/internal/runner"
	"toolbox/internal/testutil"
)

type executorFunc func(context.Context, []string) (runner.Result, error)

func (f executorFunc) Run(ctx context.Context, args []string) (runner.Result, error) {
	return f(ctx, args)
}

func TestContextPolicyAndAllNamespaces(t *testing.T) {
	s, err := New(testutil.MultiContextConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var calls []string
	for name, scoped := range s.contexts {
		scoped.runner = executorFunc(func(ctx context.Context, args []string) (runner.Result, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return runner.Result{Stdout: []byte(`{"kind":"PodList","items":[{"metadata":{"name":"api"},"message":"credential-a-canary credential-b-canary"}]}`)}, nil
		})
	}
	out, err := s.Get(context.Background(), GetInput{Context: "prod-a", Resource: "pods", AllNamespaces: true, Selector: "app=api"})
	if err != nil || out.Error != "" || out.Context != "prod-a" || strings.Contains(string(out.Data), "canary") {
		t.Fatal(out, err)
	}
	want := []string{"prod-a get --raw=/api/v1/namespaces/apps/pods?labelSelector=app%3Dapi", "prod-a get --raw=/api/v1/namespaces/observability/pods?labelSelector=app%3Dapi"}
	if !reflect.DeepEqual(calls, want) || !reflect.DeepEqual(out.Namespaces, []string{"apps", "observability"}) {
		t.Fatal(calls, out)
	}
	var list struct{ Items []json.RawMessage }
	if json.Unmarshal(out.Data, &list) != nil || len(list.Items) != 2 {
		t.Fatal(string(out.Data))
	}
	calls = nil
	for _, in := range []GetInput{
		{Resource: "pods"}, {Context: "unapproved", Resource: "pods"}, {Context: "--context=prod-a", Resource: "pods"},
		{Context: "prod-b", Resource: "pods", Namespace: "apps"}, {Context: "prod-b", Resource: "secrets", AllNamespaces: true},
		{Context: "prod-a", Resource: "pods", AllNamespaces: true, Namespace: "apps"}, {Context: "prod-a", Resource: "pods", AllNamespaces: true, Name: "api"},
		{Context: "prod-a", Resource: "nodes", AllNamespaces: true},
	} {
		if _, err := s.Get(context.Background(), in); err == nil {
			t.Fatal("accepted", in)
		}
	}
	for _, name := range []string{"", "unapproved"} {
		if _, err := s.Raw(context.Background(), RawInput{Context: name, Path: "/version"}); err == nil {
			t.Fatal("raw context accepted")
		}
		if _, err := s.Logs(context.Background(), LogsInput{Context: name, Pod: "api"}); err == nil {
			t.Fatal("log context accepted")
		}
		if _, err := s.ResourceCatalog(ContextInput{Context: name}); err == nil {
			t.Fatal("catalog context accepted")
		}
	}
	if len(calls) != 0 {
		t.Fatal("denial executed", calls)
	}
	if _, err := s.Raw(context.Background(), RawInput{Context: "prod-b", Path: "/api/v1/namespaces/apps/pods"}); err == nil {
		t.Fatal("raw namespace bypass")
	}
	if _, err := s.Logs(context.Background(), LogsInput{Context: "prod-b", Namespace: "apps", Pod: "api"}); err == nil {
		t.Fatal("logs namespace bypass")
	}
	out, err = s.Get(context.Background(), GetInput{Context: "prod-b", Resource: "pods"})
	if err != nil || out.Context != "prod-b" || len(calls) != 1 || calls[0] != "prod-b get --raw=/api/v1/namespaces/billing/pods" {
		t.Fatal(out, calls, err)
	}
	catalog := s.Contexts()
	catalog.Contexts[0].Namespaces[0] = "evil"
	if s.Contexts().Contexts[0].Namespaces[0] != "apps" {
		t.Fatal("catalog changed policy")
	}
}

func TestAllNamespacesFailsWithoutPartialData(t *testing.T) {
	for _, failure := range []string{"process", "malformed", "not-list", "pagination", "overflow", "timeout"} {
		t.Run(failure, func(t *testing.T) {
			s, err := New(testutil.Config(t))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			scoped, _ := s.selectContext("")
			scoped.settings.MaxOutputBytes = 4096
			calls := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			scoped.runner = executorFunc(func(ctx context.Context, args []string) (runner.Result, error) {
				calls++
				data := `{"items":[{"metadata":{"name":"api"}}]}`
				if failure == "overflow" {
					data = `{"items":[` + strings.TrimSuffix(strings.Repeat(`{"metadata":{"name":"api"}},`, 100), ",") + `]}`
				}
				if calls == 2 {
					switch failure {
					case "process":
						return runner.Result{Stdout: []byte("partial-sensitive-stdout"), Stderr: []byte("password=canary")}, errors.New("tool request failed")
					case "malformed":
						data = `{"items":[`
					case "not-list":
						data = `{"kind":"Pod"}`
					case "pagination":
						data = `{"items":[],"metadata":{"continue":"next-page"}}`
					case "timeout":
						cancel()
					}
				}
				return runner.Result{Stdout: []byte(data)}, nil
			})
			out, err := s.Get(ctx, GetInput{Resource: "pods", AllNamespaces: true})
			if (err == nil && out.Error == "") || len(out.Data) != 0 || strings.Contains(out.Diagnostics, "canary") {
				t.Fatal(out, err)
			}
			if calls != 2 {
				t.Fatal("unexpected query count", calls)
			}
		})
	}
}

func TestContextTunnelsShareLimitsAndRemainInOriginalContext(t *testing.T) {
	c := testutil.MultiContextConfig(t)
	c.Kubectl.Contexts[1].AllowPortForward = true
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.StartForward(context.Background(), ForwardInput{Context: "prod-a", Target: "api", Ports: []string{":8080"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.StartForward(context.Background(), ForwardInput{Context: "prod-b", Target: "api", Ports: []string{":8080"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Context != "prod-a" || a.Namespace != "apps" || b.Context != "prod-b" || b.Namespace != "billing" {
		t.Fatal(a, b)
	}
	out, err := s.Get(context.Background(), GetInput{Context: "prod-a", Resource: "pods"})
	if err != nil || !strings.Contains(out.Error, "busy") {
		t.Fatal("contexts bypassed shared process limit", out, err)
	}
	if _, err = s.StartForward(context.Background(), ForwardInput{Context: "prod-b", Target: "api", Ports: []string{":8081"}}); err == nil {
		t.Fatal("third tunnel accepted")
	}
	if _, err = s.StopForward(b.ID); err != nil {
		t.Fatal(err)
	}
	out, err = s.Get(context.Background(), GetInput{Context: "prod-b", Resource: "pods"})
	if err != nil || out.Error != "" || out.Context != "prod-b" {
		t.Fatal(out, err)
	}
	status, err := s.ForwardStatus(a.ID)
	if err != nil || status.Context != "prod-a" || status.Status != "active" {
		t.Fatal(status, err)
	}
	echoTunnel(t, status)
	s.Close()
	assertTunnelClosed(t, a)
}

func TestContextTunnelDenialsAndSingletonCompatibility(t *testing.T) {
	c := testutil.MultiContextConfig(t)
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, in := range []ForwardInput{
		{Target: "api", Ports: []string{":8080"}}, {Context: "unapproved", Target: "api", Ports: []string{":8080"}},
		{Context: "prod-b", Target: "api", Ports: []string{":8080"}}, {Context: "prod-a", Namespace: "billing", Target: "api", Ports: []string{":8080"}},
	} {
		if _, err := s.StartForward(context.Background(), in); err == nil {
			t.Fatal("tunnel policy bypass", in)
		}
	}
	c.Kubectl.Contexts = c.Kubectl.Contexts[:1]
	single, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer single.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := single.Get(ctx, GetInput{Resource: "pods"})
	if err != nil || out.Context != "prod-a" || out.Error != "" {
		t.Fatal(out, err)
	}
}
