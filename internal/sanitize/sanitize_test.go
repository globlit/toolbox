package sanitize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globlit/toolbox/internal/config"
)

func clean(t testing.TB) *Sanitizer {
	t.Helper()
	s, err := New(config.Redaction{Literals: []string{"literal-canary"}, Patterns: []string{`customer-id=\d+`}}, []string{"credential-canary"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLogCorpus(t *testing.T) {
	files, err := filepath.Glob("testdata/*.log")
	if err != nil || len(files) < 8 {
		t.Fatal("missing service fixtures")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			out, report := clean(t).Text(b)
			if strings.Contains(out, "CANARY") || strings.Contains(out, "@example.org") || strings.Contains(out, "10.20.30.40") || strings.Contains(out, "2001:db8::1") {
				t.Fatalf("leak: %s", out)
			}
			if !strings.Contains(out, "diagnostic-preserved") || report.Redactions == 0 {
				t.Fatalf("diagnostic lost: %s", out)
			}
		})
	}
}

func TestStructuredAndNestedJSON(t *testing.T) {
	in := `{"metadata":{"name":"worker","annotations":{"innocent":"CANARY"}},"spec":{"containers":[{"env":[{"name":"ORDINARY","value":"CANARY"}],"args":["CANARY"]}]},"record":"{\"password\":\"CANARY\",\"message\":\"useful\"}","large":9007199254740993,"status":{"phase":"Running"},"api-token":"CANARY"}`
	out, report, err := clean(t).JSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) || strings.Contains(string(out), "CANARY") || !strings.Contains(string(out), "9007199254740993") || !strings.Contains(string(out), "Running") || report.Redactions < 4 {
		t.Fatal(string(out), report)
	}
	if _, _, err := clean(t).JSON([]byte(`{"password":"partial`)); err == nil {
		t.Fatal("malformed output accepted")
	}
}

func TestMultilineAndControlSequences(t *testing.T) {
	for _, in := range []string{
		"password: |\n  MULTILINE_CANARY\n  MORE_CANARY\nINFO diagnostic-preserved",
		"-----BEGIN PRIVATE KEY-----\nPEM_CANARY\n-----END PRIVATE KEY-----\nINFO diagnostic-preserved",
		"INFO diagnostic-preserved\n-----BEGIN PRIVATE KEY-----\nINCOMPLETE_CANARY",
		"password=\x1b[31mCONTROL_CANARY\x1b[0m\nINFO diagnostic-preserved",
		"pass\u200bword=UNICODE_CANARY\nINFO diagnostic-preserved",
		"[info] {\"pass\\u0077ord\":\"ESCAPED_CANARY\",\"message\":\"diagnostic-preserved\"}",
	} {
		out, _ := clean(t).Text([]byte(in))
		if strings.Contains(out, "CANARY") || !strings.Contains(out, "diagnostic-preserved") {
			t.Fatal(out)
		}
	}
}

func TestAdditionalRulesAndCredentialRedaction(t *testing.T) {
	out, _ := clean(t).Text([]byte("literal-canary credential-canary customer-id=123"))
	if strings.Contains(out, "canary") || strings.Contains(out, "123") {
		t.Fatal(out)
	}
}

func FuzzSensitiveJSON(f *testing.F) {
	f.Add("a secret with spaces")
	f.Add("\n\"escape\\")
	f.Fuzz(func(t *testing.T, secret string) {
		b, _ := json.Marshal(map[string]any{"password": secret, "env": []any{map[string]any{"name": "X", "value": secret}}})
		out, _, err := clean(t).JSON(b)
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if json.Unmarshal(out, &v) != nil || v["password"] != Marker || v["env"] != Marker {
			t.Fatal(string(out))
		}
	})
}
