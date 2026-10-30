package sanitize

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/globlit/toolbox/internal/config"
)

func TestAuditF1F4FragmentsFailClosedPerLine(t *testing.T) {
	s := clean(t)
	for name, bad := range map[string]string{
		"F1-json-suffix":      `request_body={"customer":"TOPSECRET42"}`,
		"F1-array-suffix":     `password=["TOPSECRET42"]`,
		"F1-error-diagnostic": `Error from server (Forbidden): request_body={"customer":"TOPSECRET42"}`,
		"F1-indented-json":    "password: |\n  {\"blob\":\"TOPSECRET42\"}",
		"F4-python-repr":      `{'headers': {'X-Custom': 'TOPSECRET42'}}`,
		"F4-trailing-garbage": `{"pass\u0077ord":"TOPSECRET42"} invalid trailing text`,
		"F4-malformed-json":   `{"body":{"customer":"TOPSECRET42"`,
	} {
		t.Run(name, func(t *testing.T) {
			input := "INFO before-record\n" + bad + "\nINFO after-record\n"
			out, r := s.Text([]byte(input))
			if strings.Contains(out, "TOPSECRET42") || !strings.Contains(out, Withheld) || r.WithheldLines == 0 {
				t.Fatal(out, r)
			}
			if !strings.HasPrefix(out, "INFO before-record\n") || !strings.HasSuffix(out, "\nINFO after-record\n") || strings.Count(out, "\n") != strings.Count(input, "\n") {
				t.Fatal("neighboring records or line boundaries lost", out)
			}
		})
	}
	for _, message := range []string{`info {"pass\u0077ord":"TOPSECRET42"}`, `{'headers': {'X-Custom': 'TOPSECRET42'}}`} {
		b, _ := json.Marshal(map[string]any{"level": "error", "message": message, "status": "useful-diagnostic"})
		out, _, err := s.JSON(b)
		if err != nil || strings.Contains(string(out), "TOPSECRET42") || !strings.Contains(string(out), "useful-diagnostic") {
			t.Fatal(string(out), err)
		}
	}
}

func TestAuditF2OrphanTailLinesFailClosed(t *testing.T) {
	s := clean(t)
	for _, fragment := range []string{"  TOPSECRET42", "\tTOPSECRET42", "TOPSECRET42", "TOPSECRET42 with spaces", `"TOPSECRET42"`, `['TOPSECRET42']`, `["TOPSECRET42"]`, `  {"blob":"TOPSECRET42"}`, "2026-10-29T12:00:01Z   TOPSECRET42"} {
		out, r := s.Text([]byte(fragment + "\nINFO unrelated healthy record\n"))
		if strings.Contains(out, "TOPSECRET42") || !strings.HasPrefix(out, Withheld+"\n") || !strings.Contains(out, "INFO unrelated healthy record") || r.WithheldLines != 1 {
			t.Fatal(out, r)
		}
	}
}

func TestAuditF3MultilineStatesAndRecovery(t *testing.T) {
	for _, bad := range []string{
		"password=\"first line\nTOPSECRET42\"",
		"password='''first line\nTOPSECRET42'''",
		"password: |\n  TOPSECRET42",
		"2026-10-29T12:00:00Z password: |\n2026-10-29T12:00:01Z   TOPSECRET42",
		"password=\"first line\nINFO TOPSECRET42\nlast line\"",
		"  password=\"first line\nINFO TOPSECRET42\nlast line\"",
		"2026-10-29T12:00:00Z   password=\"first line\nINFO TOPSECRET42\nlast line\"",
		"-----BEGIN PRIVATE KEY-----\nTOPSECRET42\n-----END PRIVATE KEY-----",
	} {
		input := "INFO before-record\n" + bad + "\nINFO after-record\n"
		out, r := clean(t).Text([]byte(input))
		if strings.Contains(out, "TOPSECRET42") || !strings.Contains(out, "INFO before-record") || !strings.Contains(out, "INFO after-record") || strings.Count(out, "\n") != strings.Count(input, "\n") || r.WithheldLines == 0 {
			t.Fatal(out, r)
		}
	}
}

func TestUnclosedSecretSpansDoNotTrustFakeRecordPrefixes(t *testing.T) {
	for _, prefix := range []string{"password=\"unterminated", "-----BEGIN PRIVATE KEY-----"} {
		input := "INFO safe-before\n" + prefix + "\nINFO TOPSECRET42\n"
		out, r := clean(t).Text([]byte(input))
		if strings.Contains(out, "TOPSECRET42") || !strings.HasPrefix(out, "INFO safe-before\n") || r.WithheldLines != 2 || strings.Count(input, "\n") != strings.Count(out, "\n") {
			t.Fatal(out, r)
		}
	}
}

func TestAuditF5RulesApplyToAllScalarTypes(t *testing.T) {
	s, err := New(config.Redaction{Literals: []string{"654321", "true", "null"}, Patterns: []string{`987654`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := `{"otp":654321,"other":987654,"exponent":6.54321e5,"array":[987654,true,null],"unmatched":9007199254740993,"message":"useful"}`
	for _, logs := range []bool{false, true} {
		var out []byte
		if logs {
			text, _ := s.Text([]byte(input))
			out = []byte(text)
		} else {
			var err error
			out, _, err = s.JSON([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
		}
		var v map[string]any
		d := json.NewDecoder(strings.NewReader(string(out)))
		d.UseNumber()
		if d.Decode(&v) != nil {
			t.Fatal(string(out))
		}
		if v["otp"] != Marker || v["other"] != Marker || v["exponent"] != Marker || v["unmatched"] != json.Number("9007199254740993") || v["message"] != "useful" {
			t.Fatal(string(out))
		}
		for _, item := range v["array"].([]any) {
			if item != Marker {
				t.Fatal(string(out))
			}
		}
	}
}

func FuzzSensitiveSuffix(f *testing.F) {
	f.Add("short secret")
	f.Add("quotes\"and\nnewlines")
	f.Fuzz(func(t *testing.T, value string) {
		b, _ := json.Marshal(map[string]string{"customer": value})
		out, _ := clean(t).Text([]byte("INFO before\nrequest_body=" + string(b) + "\nINFO after"))
		if out != "INFO before\n"+Withheld+"\nINFO after" {
			t.Fatal(out)
		}
	})
}

func TestConfiguredRulesCannotEraseContextAndCanSpanLines(t *testing.T) {
	s, _ := New(config.Redaction{Literals: []string{"password", "INFO FIRST\nINFO TOPSECRET42"}}, nil)
	out, _ := s.Text([]byte("INFO before\npassword=TOPSECRET42\nINFO FIRST\nINFO TOPSECRET42\nINFO after"))
	if strings.Contains(out, "TOPSECRET42") || !strings.HasPrefix(out, "INFO before\n") || !strings.HasSuffix(out, "\nINFO after") {
		t.Fatal(out)
	}
	s, _ = New(config.Redaction{Patterns: []string{`INFO FIRST\nINFO TOPSECRET42`}}, nil)
	out, _ = s.Text([]byte("INFO before\nINFO FIRST\nINFO TOPSECRET42\nINFO after"))
	if out != "INFO before\n"+Withheld+"\n"+Withheld+"\nINFO after" {
		t.Fatal(out)
	}
}
