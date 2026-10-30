package kubectl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/globlit/toolbox/internal/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSecurityRegressionsOverCLIAndMCP(t *testing.T) {
	kubectlBinary := os.Getenv("TOOLBOX_TEST_KUBECTL")
	if kubectlBinary == "" {
		t.Skip("set TOOLBOX_TEST_KUBECTL for real kubectl against a local TLS mock")
	}
	fixtureBytes, err := os.ReadFile("../../sanitize/testdata/complex_logs.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		ID, Input string
		Closed    bool
	}
	if err := json.Unmarshal(fixtureBytes, &fixtures); err != nil {
		t.Fatal(err)
	}
	var complexLog strings.Builder
	var checkpoints []string
	for i, fixture := range fixtures {
		if fixture.Closed {
			marker := fmt.Sprintf("INFO checkpoint-%03d ready", i)
			checkpoints = append(checkpoints, marker)
			complexLog.WriteString(marker + "\n" + fixture.Input + "\n")
		}
	}
	complexLog.WriteString("INFO healthy-after\n")
	checkComplex := func(out []byte) {
		t.Helper()
		if strings.Contains(string(out), "SYNTHETIC_SECRET_7mQ2") {
			t.Fatal("complex log corpus leaked", string(out))
		}
		for _, marker := range append(checkpoints, "INFO healthy-after") {
			if !strings.Contains(string(out), marker) {
				t.Fatal("complex log lost independent record", marker)
			}
		}
	}
	var mu sync.Mutex
	var requests []string
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		if r.Method != "GET" {
			t.Error("write method observed")
			http.Error(w, "denied", 405)
			return
		}
		if r.Header.Get("Authorization") != "Bearer credential-canary" {
			t.Error("unexpected identity")
		}
		w.Header().Set("Content-Type", "text/plain")
		switch {
		case strings.Contains(r.URL.Path, "/complex/"):
			_, _ = w.Write([]byte(complexLog.String()))
		case strings.Contains(r.URL.Path, "/suffix/"):
			_, _ = w.Write([]byte("INFO healthy-before\n" + `request_body={"customer":"TOPSECRET42"}` + "\nINFO healthy-after\n"))
		case strings.Contains(r.URL.Path, "/tailcase/"):
			body := "password: |\n  TOPSECRET42\n"
			if r.URL.Query().Get("tailLines") == "1" || r.URL.Query().Get("sinceSeconds") == "1" {
				body = "  TOPSECRET42\n"
			}
			_, _ = w.Write([]byte(body))
		case strings.Contains(r.URL.Path, "/number/"):
			_, _ = w.Write([]byte(`{"otp":654321,"account":987654}` + "\n"))
		case strings.Contains(r.URL.Path, "/python/"):
			_, _ = w.Write([]byte("INFO healthy-before\n" + `{'headers': {'X-Custom': 'TOPSECRET42'}}` + "\nINFO healthy-after\n"))
		case strings.Contains(r.URL.Path, "/quoted/"):
			_, _ = w.Write([]byte("INFO healthy-before\npassword=\"first line\nTOPSECRET42\"\nINFO healthy-after\n"))
		case strings.Contains(r.URL.Path, "/stderr/"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(403)
			_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "Status", "status": "Failure", "reason": "Forbidden", "code": 403, "message": `request_body={"customer":"TOPSECRET42"}`})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	c := testutil.Config(t)
	c.Kubectl.Binary = kubectlBinary
	c.Redaction.Literals = []string{"654321"}
	c.Redaction.Patterns = []string{`987654`}
	b, _ := os.ReadFile(c.Kubectl.Kubeconfig)
	var kc map[string]any
	_ = json.Unmarshal(b, &kc)
	cl := kc["clusters"].([]any)[0].(map[string]any)["cluster"].(map[string]any)
	cl["server"] = api.URL
	cl["certificate-authority-data"] = base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: api.Certificate().Raw}))
	b, _ = json.Marshal(kc)
	_ = os.WriteFile(c.Kubectl.Kubeconfig, b, 0600)
	binary, _ := testutil.BuildGateway(t, c)
	for _, tc := range []struct {
		pod, tail, canary string
	}{{"complex", "1000", "SYNTHETIC_SECRET_7mQ2"}, {"suffix", "200", "TOPSECRET42"}, {"tailcase", "200", "TOPSECRET42"}, {"tailcase", "1", "TOPSECRET42"}, {"number", "200", "654321"}, {"python", "200", "TOPSECRET42"}, {"quoted", "200", "TOPSECRET42"}, {"stderr", "200", "TOPSECRET42"}} {
		out, err := exec.Command(binary, "kubectl", "logs", tc.pod, "--tail", tc.tail).CombinedOutput()
		if (tc.pod == "stderr") != (err != nil) {
			t.Fatal(err, string(out))
		}
		var result Result
		if json.Unmarshal(out, &result) != nil || !result.Untrusted || result.Redactions == 0 || (tc.pod == "stderr") != (result.Error != "") {
			t.Fatal("expected a sanitized result envelope", string(out))
		}
		leak := strings.Contains(string(out), tc.canary)
		if leak || strings.Contains(string(out), "987654") {
			t.Fatalf("unexpected result for %s: %s", tc.pod, out)
		}
		if tc.pod == "complex" {
			checkComplex(out)
		}
		if tc.pod == "suffix" || tc.pod == "python" || tc.pod == "quoted" {
			if !strings.Contains(string(out), "healthy-before") || !strings.Contains(string(out), "healthy-after") {
				t.Fatal("surrounding diagnostics discarded", string(out))
			}
		}
		t.Logf("CLI pod=%s tail=%s leak=%v out=%s", tc.pod, tc.tail, leak, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "audit", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(binary, "mcp")}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, pod := range []string{"complex", "suffix", "tailcase", "number", "python", "quoted", "stderr"} {
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "kubectl_logs", Arguments: map[string]any{"pod": pod, "tail": 1}})
		if err != nil {
			t.Fatal(err)
		}
		if r.IsError != (pod == "stderr") || len(r.Content) == 0 {
			t.Fatal("unexpected MCP result", r)
		}
		b, _ := json.Marshal(r)
		if pod == "complex" {
			checkComplex(b)
		}
		canary := "TOPSECRET42"
		if pod == "number" {
			canary = "654321"
		}
		if strings.Contains(string(b), canary) || strings.Contains(string(b), "987654") {
			t.Fatal("MCP regression leaked", string(b))
		}
		if pod == "suffix" || pod == "python" || pod == "quoted" {
			if !strings.Contains(string(b), "healthy-before") || !strings.Contains(string(b), "healthy-after") {
				t.Fatal("MCP discarded neighboring lines", string(b))
			}
		}
		t.Logf("MCP pod=%s leak=false isError=%v", pod, r.IsError)
	}
	out, err := exec.Command(binary, "kubectl", "logs", "tailcase", "--since", "1s").CombinedOutput()
	if err != nil || strings.Contains(string(out), "TOPSECRET42") {
		t.Fatal("since-boundary leak", string(out), err)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Logf("HTTP requests=%v", requests)
}
