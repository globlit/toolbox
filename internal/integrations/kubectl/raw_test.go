package kubectl

import (
	"context"
	"strings"
	"testing"
	"toolbox/internal/config"
	"toolbox/internal/sanitize"
	"toolbox/internal/testutil"
)

func TestRawPolicy(t *testing.T) {
	for _, path := range []string{"/version", "/api", "/apis", "/api/v1", "/apis/apps", "/apis/apps/v1", "/api/v1/namespaces/apps/pods", "/api/v1/namespaces/apps/pods/api", "/apis/apps/v1/namespaces/apps/deployments/api", "/api/v1/namespaces/apps/pods?labelSelector=app%3Dapi&fieldSelector=metadata.name%3Dapi&limit=10"} {
		args, err := rawArgs(settings(), RawInput{Path: path})
		if err != nil || len(args) != 2 || args[0] != "get" {
			t.Fatal(path, args, err)
		}
	}
	denied := []string{"/", "//evil/api", "https://evil/api", "/api/v1/pods", "/api/v1/nodes", "/api/v1/namespaces/other/pods", "/api/v1/namespaces/apps/secrets", "/api/v1/namespaces/apps/configmaps", "/api/v1/namespaces/apps/pods/a/exec", "/api/v1/namespaces/apps/pods/a/attach", "/api/v1/namespaces/apps/pods/a/proxy", "/api/v1/namespaces/apps/services/a/proxy/write", "/api/v1/namespaces/apps/pods/a/portforward", "/api/v1/namespaces/apps/pods/a/log", "/apis/evil/v1/namespaces/apps/pods", "/api/v1/namespaces/apps/pods/../secrets", "/api/v1/namespaces/apps/pods/%2e%2e", "/api/v1/namespaces/apps/%70ods", "/api/v1/namespaces/apps/pods/a%252fexec", "/api/v1/namespaces/apps/pods/", "/api/v1/namespaces/apps/pods?watch=true", "/api/v1/namespaces/apps/pods?limit=0", "/api/v1/namespaces/apps/pods?limit=2001", "/api/v1/namespaces/apps/pods?labelSelector=a&labelSelector=b", "/api/v1/namespaces/apps/pods?fieldSelector=%0A--raw=/", "/api/v1/namespaces/apps/pods?timeout=0", "/version?x=y", "/api/v1/namespaces/apps/pods#fragment"}
	spy := &spy{}
	san, _ := sanitize.New(config.Redaction{}, nil)
	service := &Service{settings: settings(), runner: spy, sanitizer: san}
	for _, path := range denied {
		if _, err := service.Raw(context.Background(), RawInput{Path: path}); err == nil {
			t.Fatal("accepted", path)
		}
	}
	if spy.calls != 0 {
		t.Fatal("denied raw request executed")
	}
	k := settings()
	k.AllowClusterScoped = true
	if _, err := rawArgs(k, RawInput{Path: "/api/v1/nodes"}); err != nil {
		t.Fatal(err)
	}
}
func TestRawSanitizes(t *testing.T) {
	s, err := New(testutil.Config(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, path := range []string{"/api/v1/namespaces/apps/pods", "/api/v1/namespaces/apps/pods/failure"} {
		out, err := s.Raw(context.Background(), RawInput{Path: path})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out.Data)+out.Diagnostics, "canary") {
			t.Fatal("raw leak", out)
		}
	}
}
func FuzzRawPath(f *testing.F) {
	f.Add("/api/v1/namespaces/apps/pods")
	f.Add("/api/v1/namespaces/apps/pods/%2fexec")
	f.Fuzz(func(t *testing.T, path string) {
		args, err := rawArgs(settings(), RawInput{Path: path})
		if err == nil {
			if len(args) != 2 || args[0] != "get" || !strings.HasPrefix(args[1], "--raw=/") || strings.ContainsAny(args[1], "\n\r\x00") {
				t.Fatal(args)
			}
		}
	})
}
