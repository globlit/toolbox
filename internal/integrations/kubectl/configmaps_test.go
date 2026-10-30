package kubectl

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/globlit/toolbox/internal/config"
	"github.com/globlit/toolbox/internal/runner"
	"github.com/globlit/toolbox/internal/testutil"
)

func TestConfigMapPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed []string
		denied  []string
		disable bool
		want    bool
	}{
		{name: "default", want: true},
		{name: "explicit", allowed: []string{"apps"}, want: true},
		{name: "other-namespace", allowed: []string{"other"}},
		{name: "excluded", denied: []string{"apps"}},
		{name: "exclusion-wins", allowed: []string{"apps"}, denied: []string{"apps"}},
		{name: "global", disable: true},
		{name: "global-wins", disable: true, allowed: []string{"apps"}, denied: []string{"other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := settings()
			k.DisableConfigMaps = tc.disable
			k.ConfigMapNamespaces = tc.allowed
			k.DisabledConfigMapNamespaces = tc.denied
			for _, alias := range []string{"cm", "configmap", "configmaps", "configmaps."} {
				for _, name := range []string{"", "app-settings"} {
					_, err := getArgs(k, GetInput{Resource: alias, Name: name})
					if (err == nil) != tc.want {
						t.Fatal(alias, name, err)
					}
				}
			}
			for _, suffix := range []string{"", "/app-settings"} {
				_, err := rawArgs(k, RawInput{Path: "/api/v1/namespaces/apps/configmaps" + suffix})
				if (err == nil) != tc.want {
					t.Fatal(suffix, err)
				}
			}
			if _, err := getArgs(k, GetInput{Resource: "pods"}); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/api/v1/namespaces/apps/configmaps/x/proxy", "/api/v1/namespaces/apps/secrets", "/api/v1/namespaces/other/configmaps"} {
				if _, err := rawArgs(k, RawInput{Path: path}); err == nil {
					t.Fatal("policy bypass", path)
				}
			}
		})
	}
}

func TestConfigMapExclusionsBeforeExecution(t *testing.T) {
	c := testutil.Config(t)
	c.Kubectl.Namespaces = []string{"apps", "private"}
	c.Kubectl.DisabledConfigMapNamespaces = []string{"private"}
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	scoped, _ := s.selectContext("")
	var calls []string
	scoped.runner = executorFunc(func(_ context.Context, args []string) (runner.Result, error) {
		calls = append(calls, strings.Join(args, " "))
		return runner.Result{Stdout: []byte(`{"apiVersion":"v1","kind":"ConfigMapList","items":[{"metadata":{"name":"app-settings","namespace":"apps"},"data":{"ordinary":"readable-setting","password":"SYNTHETIC_CM_SECRET"},"binaryData":{"opaque":"SYNTHETIC_CM_SECRET"}}]}`)}, nil
	})
	out, err := s.Get(context.Background(), GetInput{Resource: "cm", AllNamespaces: true})
	if err != nil || out.Error != "" || strings.Contains(string(out.Data), "SYNTHETIC_CM_SECRET") || (!strings.Contains(string(out.Data), "app-settings") || !strings.Contains(string(out.Data), "readable-setting")) {
		t.Fatal(out, err)
	}
	if !reflect.DeepEqual(out.Namespaces, []string{"apps"}) || !reflect.DeepEqual(calls, []string{"get --raw=/api/v1/namespaces/apps/configmaps"}) {
		t.Fatal(out, calls)
	}
	if _, err := s.Get(context.Background(), GetInput{Resource: "cm", Namespace: "private"}); err == nil {
		t.Fatal("denied namespace accepted")
	}
	if _, err := s.Raw(context.Background(), RawInput{Path: "/api/v1/namespaces/private/configmaps"}); err == nil {
		t.Fatal("raw exclusion bypass")
	}
	scoped.settings.DisableConfigMaps = true
	if _, err := s.Get(context.Background(), GetInput{Resource: "cm", AllNamespaces: true}); err == nil {
		t.Fatal("global disable bypass")
	}
	if len(calls) != 1 {
		t.Fatal("denied request executed", calls)
	}
}

func TestConfigMapGlobalContextOverride(t *testing.T) {
	k := config.Kubectl{DisableConfigMaps: true, Contexts: []config.Context{{Name: "a", ConfigMapNamespaces: []string{"apps"}}, {Name: "b", DisabledConfigMapNamespaces: []string{"other"}}}}
	for _, scope := range k.Scopes() {
		if scope.ConfigMapsAllowed("apps") {
			t.Fatal("global disable lost", scope)
		}
	}
}

func TestConfigMapOutputScope(t *testing.T) {
	c := testutil.Config(t)
	c.Kubectl.Namespaces = []string{"*"}
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	scoped, _ := s.selectContext("")
	scoped.runner = executorFunc(func(_ context.Context, _ []string) (runner.Result, error) {
		return runner.Result{Stdout: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings"},"data":{"ordinary":"readable-setting","password":"sensitive-canary","custom":"credential-canary"}}`)}, nil
	})
	for _, tc := range []struct {
		name     string
		read     func() (Result, error)
		readable bool
	}{
		{"typed", func() (Result, error) { return s.Get(context.Background(), GetInput{Resource: "cm", Name: "settings"}) }, true},
		{"raw", func() (Result, error) {
			return s.Raw(context.Background(), RawInput{Path: "/api/v1/namespaces/apps/configmaps/settings"})
		}, true},
		{"pod-kind-spoof", func() (Result, error) {
			return s.Get(context.Background(), GetInput{Resource: "pods", Name: "settings"})
		}, false},
		{"log-kind-spoof", func() (Result, error) { return s.Logs(context.Background(), LogsInput{Pod: "app"}) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.read()
			if err != nil {
				t.Fatal(err)
			}
			value := string(out.Data) + out.Text
			if strings.Contains(value, "sensitive-canary") || strings.Contains(value, "credential-canary") || strings.Contains(value, "readable-setting") != tc.readable {
				t.Fatal(out)
			}
		})
	}
}
