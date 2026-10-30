package kubectl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/globlit/toolbox/internal/testutil"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestRealKubectlForwardStartupAndExpiry(t *testing.T) {
	binary := os.Getenv("TOOLBOX_TEST_KUBECTL")
	if binary == "" {
		t.Skip("set TOOLBOX_TEST_KUBECTL for a local mock API")
	}
	var mu sync.Mutex
	var requests []string
	var connections []net.Conn
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer credential-canary" {
			t.Error("unexpected identity")
		}
		if r.URL.Path == "/api/v1/namespaces/apps/pods/api/portforward" {
			if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				http.Error(w, "use SPDY", 400)
				return
			}
			if r.Method != "POST" || !strings.EqualFold(r.Header.Get("Upgrade"), "SPDY/3.1") {
				t.Errorf("unexpected forward request %s %s", r.Method, r.Header.Get("Upgrade"))
				http.Error(w, "denied", 405)
				return
			}
			conn, buffer, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
			fmt.Fprint(buffer, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: SPDY/3.1\r\nX-Stream-Protocol-Version: portforward.k8s.io\r\n\r\n")
			buffer.Flush()
			go func() { defer conn.Close(); io.Copy(io.Discard, conn) }()
			return
		}
		if r.Method != "GET" {
			t.Error("unexpected mutation", r.Method, r.URL.Path)
			http.Error(w, "denied", 405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			fmt.Fprint(w, `{"kind":"APIVersions","apiVersion":"v1","versions":["v1"]}`)
		case "/apis":
			fmt.Fprint(w, `{"kind":"APIGroupList","apiVersion":"v1","groups":[]}`)
		case "/api/v1":
			fmt.Fprint(w, `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"v1","resources":[{"name":"pods","singularName":"pod","namespaced":true,"kind":"Pod","verbs":["get","list"]}]}`)
		case "/api/v1/namespaces/apps/pods/api":
			fmt.Fprint(w, `{"kind":"Pod","apiVersion":"v1","metadata":{"name":"api","namespace":"apps"},"spec":{"containers":[{"name":"app","ports":[{"containerPort":8080}]}]},"status":{"phase":"Running"}}`)
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range connections {
			conn.Close()
		}
	}()
	c := testutil.Config(t)
	c.Kubectl.Binary = binary
	c.Kubectl.ExecutableSHA256 = testutil.BinaryHash(t, binary)
	c.Kubectl.AllowPortForward = true
	c.Kubectl.PortForwardMaxSeconds = 2
	b, err := os.ReadFile(c.Kubectl.Kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	var kc map[string]any
	json.Unmarshal(b, &kc)
	cl := kc["clusters"].([]any)[0].(map[string]any)["cluster"].(map[string]any)
	cl["server"] = api.URL
	cl["certificate-authority-data"] = base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: api.Certificate().Raw}))
	b, _ = json.Marshal(kc)
	os.WriteFile(c.Kubectl.Kubeconfig, b, 0600)
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result, err := s.StartForward(context.Background(), ForwardInput{Target: "api", Ports: []string{":8080"}})
	if err != nil {
		mu.Lock()
		t.Log(requests)
		mu.Unlock()
		t.Fatal(err)
	}
	if result.Status != "active" || len(result.Ports) != 1 || result.Ports[0].Remote != 8080 {
		t.Fatal(result)
	}
	final, err := s.WaitForward(context.Background(), result.ID)
	if err != nil || final.Status != "expired" {
		t.Fatal(final, err)
	}
	assertTunnelClosed(t, result)
	mu.Lock()
	defer mu.Unlock()
	t.Logf("forward startup requests: %v", requests)
	if len(connections) != 1 {
		t.Fatal("missing forwarding upgrade")
	}
}
