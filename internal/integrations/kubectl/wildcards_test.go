package kubectl

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/globlit/toolbox/internal/config"
	"github.com/globlit/toolbox/internal/runner"
	"github.com/globlit/toolbox/internal/testutil"
)

func TestWildcardNamespacePolicy(t *testing.T) {
	k := settings()
	k.Namespaces = []string{"*"}
	k.ConfigMapNamespaces = []string{"*"}
	for _, ns := range []string{"apps", "another-production", "kube-system"} {
		for _, resource := range []string{"pods", "cm"} {
			if _, err := getArgs(k, GetInput{Resource: resource, Namespace: ns}); err != nil {
				t.Fatal(err)
			}
			if _, err := rawArgs(k, RawInput{Path: "/api/v1/namespaces/" + ns + "/" + map[string]string{"pods": "pods", "cm": "configmaps"}[resource]}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := logsArgs(k, LogsInput{Pod: "app", Namespace: ns}); err != nil {
			t.Fatal(err)
		}
	}
	for _, resource := range []string{"pods", "cm", "deployments"} {
		if _, err := getArgs(k, GetInput{Resource: resource, AllNamespaces: true}); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/api/v1/pods", "/api/v1/configmaps", "/apis/apps/v1/deployments"} {
		if _, err := rawArgs(k, RawInput{Path: path}); err != nil {
			t.Fatal(err)
		}
	}
	for _, ns := range []string{"*", "prod-*", "../apps", "apps/pods", "--all-namespaces", "apps\n"} {
		if _, err := getArgs(k, GetInput{Resource: "pods", Namespace: ns}); err == nil {
			t.Fatal("invalid namespace accepted", ns)
		}
	}
	for _, path := range []string{"/api/v1/secrets", "/api/v1/nodes", "/api/v1/namespaces", "/api/v1/pods/app", "/api/v1/pods?watch=true", "/api/v1/namespaces/apps/pods/x/exec"} {
		if _, err := rawArgs(k, RawInput{Path: path}); err == nil {
			t.Fatal("wildcard widened resource policy", path)
		}
	}
	k.DisabledConfigMapNamespaces = []string{"private"}
	if _, err := rawArgs(k, RawInput{Path: "/api/v1/configmaps"}); err == nil {
		t.Fatal("cluster list bypassed exclusion")
	}
	if _, err := rawArgs(k, RawInput{Path: "/api/v1/namespaces/private/configmaps"}); err == nil {
		t.Fatal("namespace exclusion bypass")
	}
}

func TestWildcardConfigMapPlans(t *testing.T) {
	for _, tc := range []struct {
		name        string
		resource    string
		allow, deny []string
		disable     bool
		want        []string
	}{
		{name: "pods", resource: "pods", want: []string{"/api/v1/pods"}},
		{name: "default", resource: "cm", want: []string{"/api/v1/configmaps"}},
		{name: "explicit-star", resource: "cm", allow: []string{"*"}, want: []string{"/api/v1/configmaps"}},
		{name: "explicit-namespace", resource: "cm", allow: []string{"apps", "private"}, deny: []string{"private"}, want: []string{"/api/v1/namespaces/apps/configmaps"}},
		{name: "discover", resource: "cm", deny: []string{"private"}, want: []string{"/api/v1/namespaces", "/api/v1/namespaces/apps/configmaps"}},
		{name: "discover-star", resource: "cm", allow: []string{"*"}, deny: []string{"private"}, want: []string{"/api/v1/namespaces", "/api/v1/namespaces/apps/configmaps"}},
		{name: "deny-all", resource: "cm", deny: []string{"*"}},
		{name: "global-wins", resource: "cm", allow: []string{"*"}, disable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testutil.Config(t)
			c.Kubectl.Namespaces = []string{"*"}
			c.Kubectl.ConfigMapNamespaces = tc.allow
			c.Kubectl.DisabledConfigMapNamespaces = tc.deny
			c.Kubectl.DisableConfigMaps = tc.disable
			s, err := New(c)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			scoped, _ := s.selectContext("")
			var calls []string
			scoped.runner = executorFunc(func(_ context.Context, args []string) (runner.Result, error) {
				path := strings.TrimPrefix(args[1], "--raw=")
				calls = append(calls, path)
				if path == "/api/v1/namespaces" {
					return runner.Result{Stdout: []byte(`{"items":[{"metadata":{"name":"apps"}},{"metadata":{"name":"private"}}]}`)}, nil
				}
				return runner.Result{Stdout: []byte(`{"apiVersion":"v1","kind":"ConfigMapList","items":[]}`)}, nil
			})
			_, err = s.Get(context.Background(), GetInput{Resource: tc.resource, AllNamespaces: true})
			if (err != nil) != (tc.want == nil) || !reflect.DeepEqual(calls, tc.want) {
				t.Fatal(err, calls, tc.want)
			}
		})
	}
}

func TestWildcardDiscoveryFailsClosed(t *testing.T) {
	for _, response := range []string{`{"items":[{"metadata":{"name":"apps"}}],"metadata":{"continue":"more"}}`, `{"items":[{"metadata":{"name":"../bad"}}]}`, `{"items":[{"metadata":{"name":"apps"}},{"metadata":{"name":"apps"}}]}`, `{}`, `forbidden`} {
		c := testutil.Config(t)
		c.Kubectl.Namespaces = []string{"*"}
		c.Kubectl.DisabledConfigMapNamespaces = []string{"private"}
		s, err := New(c)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		scoped, _ := s.selectContext("")
		calls := 0
		scoped.runner = executorFunc(func(_ context.Context, args []string) (runner.Result, error) {
			calls++
			if response == "forbidden" {
				return runner.Result{}, errors.New("denied")
			}
			return runner.Result{Stdout: []byte(response)}, nil
		})
		out, err := s.Get(context.Background(), GetInput{Resource: "cm", AllNamespaces: true})
		if err == nil || len(out.Data) > 0 || calls != 1 {
			t.Fatal(out, err, calls)
		}
	}
}

func FuzzConfigMapNamespacePolicy(f *testing.F) {
	f.Add("apps", uint8(0), true)
	f.Add("private", uint8(1), false)
	f.Add("../apps", uint8(2), true)
	f.Add("*", uint8(3), false)
	f.Fuzz(func(t *testing.T, ns string, mode uint8, wildcard bool) {
		if len(ns) > 256 {
			t.Skip()
		}
		k := settings()
		if wildcard {
			k.Namespaces = []string{"*"}
		}
		switch mode % 4 {
		case 1:
			k.DisableConfigMaps = true
		case 2:
			k.DisabledConfigMapNamespaces = []string{ns}
		case 3:
			k.ConfigMapNamespaces = []string{"apps"}
			k.DisabledConfigMapNamespaces = []string{"private"}
		}
		want := config.Namespace(ns) && (wildcard || ns == "apps") && (mode%4 == 0 || (mode%4 == 3 && ns == "apps"))
		// Empty input selects the default namespace in typed requests only.
		if ns == "" {
			return
		}
		for _, alias := range []string{"cm", "configmap", "configmaps", "configmaps."} {
			args, err := getArgs(k, GetInput{Resource: alias, Namespace: ns})
			if (err == nil) != want {
				t.Fatal("typed policy mismatch", ns, mode, wildcard, err)
			}
			if err == nil && (len(args) != 2 || args[0] != "get" || args[1] != "--raw=/api/v1/namespaces/"+ns+"/configmaps") {
				t.Fatal(args)
			}
		}
		_, err := rawArgs(k, RawInput{Path: "/api/v1/namespaces/" + ns + "/configmaps"})
		if (err == nil) != want {
			t.Fatal("raw policy mismatch", ns, mode, wildcard, err)
		}
	})
}

func FuzzWildcardRawPath(f *testing.F) {
	for _, path := range []string{"/api/v1/configmaps", "/api/v1/namespaces/private/configmaps", "/api/v1/namespaces/apps/configmaps/example", "/api/v1/configmaps?watch=true", "/api/v1/namespaces/private/configmaps/%2e%2e/pods", "/api/v1/secrets", "/apis/apps/v1/deployments"} {
		f.Add(path, false, true)
	}
	f.Fuzz(func(t *testing.T, path string, disable, exclude bool) {
		if len(path) > 4096 {
			t.Skip()
		}
		k := settings()
		k.Namespaces = []string{"*"}
		k.DisableConfigMaps = disable
		if exclude {
			k.DisabledConfigMapNamespaces = []string{"private"}
		}
		args, err := rawArgs(k, RawInput{Path: path})
		if err != nil {
			return
		}
		if len(args) != 2 || args[0] != "get" || !strings.HasPrefix(args[1], "--raw=/") || strings.ContainsAny(args[1], "\r\n\x00") {
			t.Fatal(args)
		}
		parsed, err := url.Parse(strings.TrimPrefix(args[1], "--raw="))
		if err != nil || parsed.Host != "" || parsed.Scheme != "" || parsed.RawPath != "" {
			t.Fatal(args, err)
		}
		parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
		if len(parts) < 3 {
			return
		} // Approved discovery endpoints have no resource segment.
		if parts[0] == "api" {
			resource := parts[2]
			if resource == "namespaces" {
				if len(parts) < 5 {
					t.Fatal("cluster namespace access enabled", args)
				}
				resource = parts[4]
				if resource == "configmaps" && exclude && parts[3] == "private" {
					t.Fatal("namespace exclusion bypass", args)
				}
			} else if resource == "configmaps" && exclude {
				t.Fatal("cluster list bypass", args)
			}
			if resource == "secrets" || resource == "nodes" || (resource == "configmaps" && disable) {
				t.Fatal("resource policy bypass", args)
			}
		}
	})
}

func TestWildcardDiscoverySharesOutputBudget(t *testing.T) {
	c := testutil.Config(t)
	c.Kubectl.Namespaces = []string{"*"}
	c.Kubectl.DisabledConfigMapNamespaces = []string{"private"}
	c.Kubectl.MaxOutputBytes = 4096
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	scoped, _ := s.selectContext("")
	calls := 0
	scoped.runner = executorFunc(func(_ context.Context, args []string) (runner.Result, error) {
		calls++
		value := map[string]any{"apiVersion": "v1", "kind": "ConfigMapList", "items": []any{}, "notice": strings.Repeat("resource info ", 200)}
		if args[1] == "--raw=/api/v1/namespaces" {
			value = map[string]any{"items": []any{map[string]any{"metadata": map[string]string{"name": "apps"}}}, "notice": strings.Repeat("scope info ", 150)}
		}
		b, _ := json.Marshal(value)
		return runner.Result{Stdout: b}, nil
	})
	out, err := s.Get(context.Background(), GetInput{Resource: "cm", AllNamespaces: true})
	if err == nil || !strings.Contains(err.Error(), "combined output") || len(out.Data) > 0 || calls != 2 {
		t.Fatal(out, err, calls)
	}
}
