package sanitize

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/globlit/toolbox/internal/config"
)

const syntheticSecret = "SYNTHETIC_SECRET_7mQ2"
const safeBefore = "INFO health-before ready"
const safeBetween = "INFO health-between ready"
const safeAfter = "INFO health-after ready"

type logFixture struct {
	BoundaryOnly bool     `json:"boundary_only"`
	ID           string   `json:"id"`
	Family       string   `json:"family"`
	Input        string   `json:"input"`
	Retain       []string `json:"retain"`
	Ambiguous    bool     `json:"ambiguous"`
	Closed       bool     `json:"closed"`
}

func complexLogs(t testing.TB) []logFixture {
	t.Helper()
	b, err := os.ReadFile("testdata/complex_logs.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []logFixture
	if err = json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 45 {
		t.Fatal("require at least 45 distinct log fixtures")
	}
	ids, inputs := map[string]bool{}, map[string]bool{}
	for _, c := range cases {
		if c.ID == "" || c.Family == "" || ids[c.ID] || inputs[c.Input] || !strings.Contains(c.Input, syntheticSecret) {
			t.Fatal("invalid or duplicate fixture", c.ID)
		}
		ids[c.ID], inputs[c.Input] = true, true
	}
	return cases
}

func unconfigured(t testing.TB) *Sanitizer {
	t.Helper()
	s, err := New(config.Redaction{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func assertSecretAbsent(t testing.TB, out string) {
	t.Helper()
	if strings.Contains(out, syntheticSecret) {
		t.Fatalf("synthetic secret leaked: %q", out)
	}
}

func TestComplexLogCorpus(t *testing.T) {
	s := unconfigured(t)
	for _, c := range complexLogs(t) {
		t.Run(c.Family+"/"+c.ID, func(t *testing.T) {
			for _, eol := range []string{"\n", "\r\n"} {
				input := safeBefore + "\n" + c.Input + "\n"
				if c.Closed {
					input += safeAfter + "\n"
				}
				input = strings.ReplaceAll(input, "\n", eol)
				out, r := s.Text([]byte(input))
				assertSecretAbsent(t, out)
				if !strings.HasPrefix(out, safeBefore+"\n") || (c.Closed && !strings.HasSuffix(out, safeAfter+"\n")) {
					t.Fatalf("independent records lost: %q", out)
				}
				if strings.Count(out, "\n") != strings.Count(input, "\n") || r.Redactions == 0 {
					t.Fatal("line boundaries or redaction report lost", out, r)
				}
				for _, keep := range c.Retain {
					if !strings.Contains(out, keep) {
						t.Fatalf("diagnostic %q lost: %q", keep, out)
					}
				}
				if c.Ambiguous && r.WithheldLines == 0 {
					t.Fatalf("ambiguous input was not replaced per line: %q", out)
				}
			}
		})
	}
}

func TestComplexLogPairCombinations(t *testing.T) {
	cases := complexLogs(t)
	s := unconfigured(t)
	for _, a := range cases {
		if !a.Closed {
			continue
		}
		for _, b := range cases {
			if !b.Closed {
				continue
			}
			t.Run(a.ID+"/"+b.ID, func(t *testing.T) {
				input := safeBefore + "\n" + a.Input + "\n" + safeBetween + "\n" + b.Input + "\n" + safeAfter + "\n"
				out, _ := s.Text([]byte(input))
				assertSecretAbsent(t, out)
				for _, marker := range []string{safeBefore, safeBetween, safeAfter} {
					if !strings.Contains(out, marker+"\n") {
						t.Fatalf("independent record lost: %s: %q", marker, out)
					}
				}
				if strings.Count(input, "\n") != strings.Count(out, "\n") {
					t.Fatal("combination changed physical line count")
				}
			})
		}
	}
}

func TestComplexLogJSONEnvelopes(t *testing.T) {
	s := unconfigured(t)
	for _, c := range complexLogs(t) {
		if c.BoundaryOnly {
			continue
		}
		t.Run(c.ID, func(t *testing.T) {
			for depth := 0; depth < 3; depth++ {
				var value any = map[string]any{"level": "error", "message": c.Input, "status": "outer-diagnostic"}
				for i := 0; i < depth; i++ {
					b, _ := json.Marshal(value)
					value = map[string]any{"level": "warn", "record": string(b), "status": "outer-diagnostic"}
				}
				b, _ := json.Marshal(value)
				out, _, err := s.JSON(b)
				if err != nil || !json.Valid(out) {
					t.Fatal(err, string(out))
				}
				assertSecretAbsent(t, string(out))
				if !strings.Contains(string(out), "outer-diagnostic") {
					t.Fatal("outer diagnostic lost")
				}
			}
		})
	}
}

func TestComplexLogMixedDump(t *testing.T) {
	var input strings.Builder
	count := 0
	for _, c := range complexLogs(t) {
		if !c.Closed {
			continue
		}
		fmt.Fprintf(&input, "INFO checkpoint-%03d ready\n%s\n", count, c.Input)
		count++
	}
	input.WriteString(safeAfter + "\n")
	out, r := unconfigured(t).Text([]byte(input.String()))
	assertSecretAbsent(t, out)
	for i := 0; i < count; i++ {
		if !strings.Contains(out, fmt.Sprintf("INFO checkpoint-%03d ready\n", i)) {
			t.Fatalf("checkpoint %d lost", i)
		}
	}
	if !strings.HasSuffix(out, safeAfter+"\n") || strings.Count(out, "\n") != strings.Count(input.String(), "\n") || r.Redactions < count {
		t.Fatal("mixed dump lost records or reports", r)
	}
}

func TestConfiguredCredentialNormalization(t *testing.T) {
	for _, credential := range []string{"key_\u200b9a41f0_end", "key_\x1b[31m9a41f0_end\x1b[0m", "key_\x009a41f0_end"} {
		s, err := New(config.Redaction{}, []string{credential})
		if err != nil {
			t.Fatal(err)
		}
		input := "INFO configured credential " + credential
		out, _ := s.Text([]byte(input))
		if strings.Contains(out, "key_9a41f0_end") {
			t.Fatal("normalized credential leaked", out)
		}
	}
}

func TestLogRequestStateIsolation(t *testing.T) {
	s := unconfigured(t)
	s.Text([]byte("password=\"unclosed\nINFO " + syntheticSecret))
	out, r := s.Text([]byte(safeAfter + "\n"))
	if out != safeAfter+"\n" || r.Redactions != 0 {
		t.Fatal("quote state crossed requests", out, r)
	}
	for _, c := range complexLogs(t) {
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel()
			out, _ := s.Text([]byte(c.Input))
			assertSecretAbsent(t, out)
		})
	}
}

func TestQuotedAssignmentBoundaries(t *testing.T) {
	for _, input := range []string{
		`func main() { password := "sensitive ="; println(password) }`,
		`func main() { fmt.Println("text =") }`,
		`if password == "sensitive =" { return }`,
		`ValueError: password="sensitive note='unfinished"`,
		`level=error msg="password='unfinished" status=failed`,
		`level=error msg='password="unfinished' status=failed`,
		`level=error msg="escaped \" password='unfinished" status=failed`,
		`level=error password="first" api_key="second" status=failed`,
		`{"level":"error","message":"password='unfinished","status":"failed"}`,
	} {
		out, _ := unconfigured(t).Text([]byte(input + "\n" + safeAfter + "\n"))
		if !strings.HasSuffix(out, safeAfter+"\n") {
			t.Fatalf("quoted assignment consumed independent record: %q -> %q", input, out)
		}
	}
}

func TestKnownCredentialTailWindows(t *testing.T) {
	s, err := New(config.Redaction{}, []string{syntheticSecret})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range complexLogs(t) {
		// Byte truncation can remove labels. Known credentials must still be masked.
		for cut := 0; cut <= strings.Index(c.Input, syntheticSecret); cut++ {
			input := c.Input[cut:]
			out, _ := s.Text([]byte(input))
			assertSecretAbsent(t, out)
		}
	}
}
