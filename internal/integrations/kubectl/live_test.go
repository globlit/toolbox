package kubectl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"toolbox/internal/testutil"
)

// Opt-in test uses a real kubectl against an ephemeral TLS API, never a cluster.
func TestRealKubectlOnlyUsesGET(t *testing.T) {
	binary := os.Getenv("TOOLBOX_TEST_KUBECTL")
	if binary == "" {
		t.Skip("set TOOLBOX_TEST_KUBECTL to an absolute kubectl binary")
	}
	var mu sync.Mutex
	var paths []string
	var hideCore atomic.Bool
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method != "GET" {
			t.Errorf("non-read HTTP method: %s", r.Method)
			w.WriteHeader(405)
			return
		}
		if r.Header.Get("Authorization") != "Bearer credential-canary" {
			t.Error("wrong credential")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			_, _ = w.Write([]byte(`{"kind":"APIVersions","apiVersion":"v1","versions":["v1"]}`))
		case "/apis":
			_, _ = w.Write([]byte(`{"kind":"APIGroupList","apiVersion":"v1","groups":[{"name":"evil.example","versions":[{"groupVersion":"evil.example/v1","version":"v1"}],"preferredVersion":{"groupVersion":"evil.example/v1","version":"v1"}}]}`))
		case "/apis/evil.example/v1":
			_, _ = w.Write([]byte(`{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"evil.example/v1","resources":[{"name":"pods","namespaced":true,"kind":"Pod","verbs":["get","list"]}]}`))
		case "/api/v1":
			if hideCore.Load() {
				_, _ = w.Write([]byte(`{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"v1","resources":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"v1","resources":[{"name":"pods","singularName":"pod","namespaced":true,"kind":"Pod","verbs":["get","list"]},{"name":"pods/log","namespaced":true,"kind":"Pod","verbs":["get"]}]}`))
		case "/api/v1/namespaces/apps/pods":
			if hideCore.Load() {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"kind":"PodList","apiVersion":"v1","metadata":{},"items":[{"kind":"Pod","apiVersion":"v1","metadata":{"name":"api","namespace":"apps"},"spec":{"containers":[{"name":"app","env":[{"name":"X","value":"RESOURCE_CANARY"}]}]},"status":{"phase":"Running"}}]}`))
		case "/api/v1/namespaces/apps/pods/api":
			_, _ = w.Write([]byte(`{"kind":"Pod","apiVersion":"v1","metadata":{"name":"api","namespace":"apps"},"spec":{"containers":[{"name":"app"}]},"status":{"phase":"Running"}}`))
		case "/api/v1/namespaces/apps/pods/api/log":
			w.Header().Set("Content-Type", "text/plain")
			if r.URL.Query().Get("follow") == "true" {
				t.Error("logs followed")
			}
			_, _ = w.Write([]byte("ready password=LOG_CANARY\n"))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	c := testutil.Config(t)
	c.Kubectl.Binary = binary
	c.Kubectl.ExecutableSHA256 = testutil.BinaryHash(t, binary)
	b, _ := os.ReadFile(c.Kubectl.Kubeconfig)
	var kc map[string]any
	_ = json.Unmarshal(b, &kc)
	cl := kc["clusters"].([]any)[0].(map[string]any)["cluster"].(map[string]any)
	cl["server"] = api.URL
	cl["certificate-authority-data"] = base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: api.Certificate().Raw}))
	b, _ = json.Marshal(kc)
	if err := os.WriteFile(c.Kubectl.Kubeconfig, b, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Get(context.Background(), GetInput{Resource: "pods"})
	if err != nil || result.Error != "" {
		t.Fatalf("get: %+v %v", result, err)
	}
	if strings.Contains(string(result.Data), "CANARY") || !strings.Contains(string(result.Data), "Running") {
		t.Fatal(string(result.Data))
	}
	result, err = s.Logs(context.Background(), LogsInput{Pod: "api", Container: "app"})
	if err != nil || result.Error != "" {
		t.Fatalf("logs: %+v %v", result, err)
	}
	if strings.Contains(result.Text, "CANARY") || !strings.Contains(result.Text, "ready") {
		t.Fatal(result)
	}
	mu.Lock()
	count := len(paths)
	mu.Unlock()
	if _, err := s.Get(context.Background(), GetInput{Resource: "secrets"}); err == nil {
		t.Fatal("secret read accepted")
	}
	mu.Lock()
	if len(paths) != count {
		t.Fatal("denial sent network traffic")
	}
	for _, path := range paths {
		if strings.Contains(path, "/apis/evil") || path == "GET /api" || path == "GET /apis" {
			t.Fatal("discovery must not be used", path)
		}
	}
	t.Logf("observed read-only requests: %v", paths)
	mu.Unlock()
	for _, path := range []string{"/api/v1/namespaces/apps/pods?limit=10", "/api/v1"} {
		result, err = s.Raw(context.Background(), RawInput{Path: path})
		if err != nil || result.Error != "" || strings.Contains(string(result.Data), "CANARY") {
			t.Fatal(result, err)
		}
	}
	mu.Lock()
	count = len(paths)
	mu.Unlock()
	for _, path := range []string{"/api/v1/namespaces/apps/pods/api/exec", "/api/v1/namespaces/apps/secrets", "/api/v1/namespaces/apps/pods?watch=true"} {
		if _, err = s.Raw(context.Background(), RawInput{Path: path}); err == nil {
			t.Fatal("raw denial failed", path)
		}
	}
	mu.Lock()
	if len(paths) != count {
		t.Fatal("raw denial reached API")
	}
	mu.Unlock()
	hideCore.Store(true)
	result, err = s.Get(context.Background(), GetInput{Resource: "pods"})
	if err == nil && result.Error == "" {
		t.Fatal("core resource resolved to a foreign group")
	}
}
