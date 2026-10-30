package kubectl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/globlit/toolbox/internal/testutil"
)

func TestRealKubectlContextsAndApprovedNamespaces(t *testing.T) {
	binary := os.Getenv("TOOLBOX_TEST_KUBECTL")
	if binary == "" {
		t.Skip("set TOOLBOX_TEST_KUBECTL to an absolute kubectl binary")
	}
	c := testutil.MultiContextConfig(t)
	c.Kubectl.Binary = binary
	c.Kubectl.ExecutableSHA256 = testutil.BinaryHash(t, binary)
	var mu sync.Mutex
	var requests []string
	servers := map[string]*httptest.Server{}
	for _, name := range []string{"a", "b"} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			requests = append(requests, name+" "+r.Method+" "+r.URL.Path)
			mu.Unlock()
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer credential-"+name+"-canary" {
				t.Error("incorrect context credentials or write request")
				w.WriteHeader(403)
				return
			}
			ns := "billing"
			if name == "a" {
				ns = "apps"
				if strings.Contains(r.URL.Path, "/observability/") {
					ns = "observability"
				}
			}
			if !strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/"+ns+"/pods") {
				t.Errorf("unapproved route %s", r.URL.Path)
				w.WriteHeader(403)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/log") {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte(`{"level":"info","message":"credential-a-canary credential-b-canary"}` + "\n"))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "PodList", "items": []any{map[string]any{"metadata": map[string]any{"name": "api-" + name, "namespace": ns}, "message": "credential-a-canary credential-b-canary"}}})
		}))
		servers[name] = server
		defer server.Close()
	}
	b, err := os.ReadFile(c.Kubectl.Kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	var kc kubeConfig
	if json.Unmarshal(b, &kc) != nil {
		t.Fatal("invalid fixture")
	}
	for i := range kc.Clusters {
		server := servers[kc.Clusters[i].Name]
		kc.Clusters[i].Cluster.Server = server.URL
		kc.Clusters[i].Cluster.CAData = base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	}
	b, _ = json.Marshal(kc)
	if err = os.WriteFile(c.Kubectl.Kubeconfig, b, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Concurrent requests must preserve their selected credentials and namespaces.
	var wg sync.WaitGroup
	for _, name := range []string{"a", "b"} {
		wg.Go(func() {
			out, err := s.Get(context.Background(), GetInput{Context: "prod-" + name, Resource: "pods", AllNamespaces: true})
			if err != nil || out.Error != "" || out.Context != "prod-"+name || !strings.Contains(string(out.Data), "api-"+name) || strings.Contains(string(out.Data), "canary") {
				t.Errorf("context routing failed: %+v %v", out, err)
			}
		})
	}
	wg.Wait()
	mu.Lock()
	before := len(requests)
	mu.Unlock()
	for _, name := range []string{"", "unapproved", "prod-c"} {
		if _, err = s.Get(context.Background(), GetInput{Context: name, Resource: "pods", AllNamespaces: true}); err == nil {
			t.Fatal("unapproved context accepted")
		}
	}
	if _, err = s.Raw(context.Background(), RawInput{Context: "prod-b", Path: "/api/v1/namespaces/apps/pods"}); err == nil {
		t.Fatal("cross-context namespace accepted")
	}
	mu.Lock()
	after := len(requests)
	mu.Unlock()
	if before != 3 || after != before {
		t.Fatal("unexpected request count", before, after)
	}
	out, err := s.Logs(context.Background(), LogsInput{Context: "prod-b", Pod: "api"})
	if err != nil || out.Error != "" || out.Context != "prod-b" || strings.Contains(out.Text, "canary") {
		t.Fatal(out, err)
	}
	out, err = s.Raw(context.Background(), RawInput{Context: "prod-a", Path: "/api/v1/namespaces/apps/pods"})
	if err != nil || out.Error != "" || out.Context != "prod-a" {
		t.Fatal(out, err)
	}
	mu.Lock()
	defer mu.Unlock()
	counts := map[string]int{}
	for _, request := range requests {
		counts[request]++
	}
	want := map[string]int{"a GET /api/v1/namespaces/apps/pods": 2, "a GET /api/v1/namespaces/observability/pods": 1, "b GET /api/v1/namespaces/billing/pods": 1, "b GET /api/v1/namespaces/billing/pods/api/log": 1}
	if !reflect.DeepEqual(counts, want) {
		t.Fatal(counts)
	}
	t.Logf("observed context-specific GET requests: %v", counts)
}
