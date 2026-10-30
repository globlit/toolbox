package sanitize

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/globlit/toolbox/internal/config"
)

func FuzzComplexLogCombinations(f *testing.F) {
	cases := complexLogs(f)
	var closed []logFixture
	for _, c := range cases {
		if c.Closed {
			closed = append(closed, c)
		}
	}
	for i := range closed {
		f.Add(uint16(i), uint16((i+1)%len(closed)), uint8(i%3))
	}
	s := unconfigured(f)
	f.Fuzz(func(t *testing.T, a, b uint16, framing uint8) {
		first, second := closed[int(a)%len(closed)], closed[int(b)%len(closed)]
		input := safeBefore + "\n" + first.Input + "\n" + safeBetween + "\n" + second.Input + "\n" + safeAfter + "\n"
		var out string
		var r Report
		switch framing % 3 {
		case 1:
			input = strings.ReplaceAll(input, "\n", "\r\n")
		case 2:
			// A JSON transport envelope must preserve the log stream's policy.
			// Bare single-line values gain field context, so test the multiline dump.
			b, _ := json.Marshal(map[string]string{"level": "error", "message": input})
			encoded, report, err := s.JSON(b)
			if err != nil {
				t.Fatal(err)
			}
			assertSecretAbsent(t, string(encoded))
			var record map[string]string
			if json.Unmarshal(encoded, &record) != nil {
				t.Fatal(string(encoded))
			}
			out, r = record["message"], report
		}
		if framing%3 != 2 {
			out, r = s.Text([]byte(input))
		}
		assertSecretAbsent(t, out)
		for _, marker := range []string{safeBefore, safeBetween, safeAfter} {
			if !strings.Contains(out, marker) {
				t.Fatalf("independent record lost: %q -> %q", input, out)
			}
		}
		if !utf8.ValidString(out) || r.Redactions < r.WithheldLines {
			t.Fatal("invalid output or report")
		}
	})
}

func FuzzSensitiveLogfmtValues(f *testing.F) {
	for _, seed := range []string{"space value", "quotes\" and \\slashes", "\nINFO fake record", "\x00\u200b", "password='unclosed"} {
		f.Add(seed, uint8(0), uint8(0))
	}
	keys := []string{"password", "api_key", "Authorization", "db.password", "request_body", "headers", "api key", "private-key", "connectionString"}
	s := unconfigured(f)
	f.Fuzz(func(t *testing.T, value string, keyID, separator uint8) {
		if len(value) > 4096 {
			t.Skip()
		}
		key := keys[int(keyID)%len(keys)]
		if strings.Contains(key, " ") {
			key = jsonQuote(key)
		}
		payload := syntheticSecret + " " + value
		encoded, _ := json.Marshal(payload)
		sep := "="
		if separator%2 != 0 {
			sep = ":"
		}
		input := safeBefore + "\nlevel=error " + key + sep + string(encoded) + "\n" + safeAfter + "\n"
		out, _ := s.Text([]byte(input))
		assertSecretAbsent(t, out)
		if !strings.HasPrefix(out, safeBefore+"\n") || !strings.HasSuffix(out, safeAfter+"\n") {
			t.Fatal("quoted value changed independent records", out)
		}
	})
}

func jsonQuote(value string) string { b, _ := json.Marshal(value); return string(b) }

func FuzzMalformedJSONRecovery(f *testing.F) {
	f.Add("diagnostic", uint8(0))
	f.Add("quotes\" and [brackets]", uint8(1))
	f.Add("INFO fake record\n", uint8(2))
	s := unconfigured(f)
	f.Fuzz(func(t *testing.T, value string, kind uint8) {
		if len(value) > 4096 {
			t.Skip()
		}
		inner, _ := json.Marshal(map[string]string{"item": syntheticSecret, "description": value})
		prefixes := []string{`INFO {"pass\u0077ord":`, `INFO [invalid, `, `level=error {"request_body":`, `[ERROR] {broken: `}
		bad := prefixes[int(kind)%len(prefixes)] + string(inner)
		out, r := s.Text([]byte(safeBefore + "\n" + bad + "\n" + safeAfter + "\n"))
		if out != safeBefore+"\n"+Withheld+"\n"+safeAfter+"\n" || r.WithheldLines != 1 {
			t.Fatal("malformed parent recovered as a record", out, r)
		}
	})
}

func FuzzKnownCredentialInArbitraryLogs(f *testing.F) {
	f.Add([]byte("INFO arbitrary message "), uint8(0), uint16(3))
	f.Add([]byte(`{"level":"warn","message":"anything"}`), uint8(1), uint16(30))
	f.Add([]byte("\x1b]8;;\n\x07"), uint8(2), uint16(0))
	f.Fuzz(func(t *testing.T, data []byte, transform uint8, position uint16) {
		if len(data) > 16384 {
			t.Skip()
		}
		sum := sha256.Sum256(data)
		token := "key_" + hex.EncodeToString(sum[:6]) + "_end"
		decorated := token
		switch transform % 4 {
		case 1:
			decorated = token[:4] + "\u200b" + token[4:]
		case 2:
			decorated = token[:4] + "\x1b[31m" + token[4:] + "\x1b[0m"
		case 3:
			decorated = token[:4] + "\x00" + token[4:]
		}
		s, err := New(config.Redaction{}, []string{decorated})
		if err != nil {
			t.Fatal(err)
		}
		pos := int(position) % (len(data) + 1)
		input := string(data[:pos]) + decorated + string(data[pos:])
		out, _ := s.Text([]byte(input))
		if strings.Contains(out, token) || strings.Contains(out, decorated) {
			t.Fatalf("configured credential leaked: %q", out)
		}
		b, _ := json.Marshal(map[string]string{"level": "warn", "message": input})
		encoded, _, err := s.JSON(b)
		if err != nil || strings.Contains(string(encoded), token) {
			t.Fatal("JSON credential leak", string(encoded), err)
		}
	})
}

func FuzzComplexStructuredLogs(f *testing.F) {
	f.Add([]byte("failure"), uint8(1), uint8(0))
	f.Add([]byte("\xff\n\x1b[31m"), uint8(70), uint8(1))
	f.Add([]byte("deep array"), uint8(70), uint8(0))
	f.Add([]byte("serialized record"), uint8(4), uint8(2))
	f.Fuzz(func(t *testing.T, data []byte, depth, shape uint8) {
		if len(data) > 2048 {
			t.Skip()
		}
		var value any = map[string]any{"password": syntheticSecret, "headers": map[string]string{"X-Private": syntheticSecret}, "message": string(data)}
		for i := 0; i < int(depth)%80; i++ {
			kind := int(shape) % 4
			if kind == 3 {
				kind = i % 3
			}
			switch kind {
			case 0:
				value = []any{value}
			case 1:
				value = map[string]any{"record": value}
			case 2:
				b, _ := json.Marshal(value)
				if len(b) > 32768 {
					t.Skip()
				}
				value = map[string]any{"record": string(b)}
			}
		}
		input, _ := json.Marshal(map[string]any{"level": "error", "record": value, "status": "outer-health"})
		s := unconfigured(t)
		out, r, err := s.JSON(input)
		if err != nil || !json.Valid(out) {
			t.Fatal("invalid structured output", err)
		}
		assertSecretAbsent(t, string(out))
		if !strings.Contains(string(out), "outer-health") || r.Redactions < r.WithheldLines {
			t.Fatal("diagnostic or report lost")
		}
		text, _ := s.Text([]byte(safeBefore + "\n" + string(input) + "\n" + safeAfter + "\n"))
		assertSecretAbsent(t, text)
		if !strings.HasPrefix(text, safeBefore+"\n") || !strings.HasSuffix(text, safeAfter+"\n") {
			t.Fatal("structured record consumed independent lines", text)
		}
	})
}

func FuzzArbitraryLogBytes(f *testing.F) {
	for _, c := range complexLogs(f) {
		f.Add([]byte(c.Input))
	}
	for _, seed := range []string{"\xff\xfe\x00", "INFO before\nINFO \x1b]8;;hidden\nsecret\x07label\nINFO after\n", `{"credential-canary":"x","other-credential-canary":"y"}`} {
		f.Add([]byte(seed))
	}
	s := unconfigured(f)
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 32768 {
			t.Skip()
		}
		out, r := s.Text(input)
		if !utf8.ValidString(out) {
			t.Fatal("invalid UTF-8 output")
		}
		if strings.Count(out, "\n") != strings.Count(string(input), "\n") {
			t.Fatal("physical line count changed", fmt.Sprintf("%q -> %q", input, out))
		}
		for _, ch := range out {
			if unicode.IsControl(ch) && ch != '\n' && ch != '\t' {
				t.Fatalf("raw control character %U", ch)
			}
		}
		if r.Redactions < 0 || r.WithheldLines < 0 || r.Redactions < r.WithheldLines {
			t.Fatal("invalid redaction counts", r)
		}
	})
}

func FuzzEmbeddedSource(f *testing.F) {
	for language := uint8(0); language < 6; language++ {
		f.Add([]byte("quotes\";\n$(printf ignored)"), language, uint8(0))
		f.Add([]byte{0, 255, '\n'}, language, uint8(1))
	}
	s := unconfigured(f)
	f.Fuzz(func(t *testing.T, data []byte, language, framing uint8) {
		if len(data) > 2048 {
			t.Skip()
		}
		value := jsonQuote(syntheticSecret + " " + string(data))
		sources := []string{
			"package main\nfunc main() { password := " + value + "; println(password) }",
			"password = " + value + "\nprint(password)",
			"const password = " + value + ";\nconsole.log(password);",
			"SELECT * FROM credentials WHERE password = " + value + ";",
			"password=" + value + "; printf '%s' \"$password\"",
			"<?php $password = " + value + "; echo $password; ?>",
		}
		input := safeBefore + "\n" + sources[int(language)%len(sources)] + "\n" + safeAfter + "\n"
		var out string
		if framing%2 == 0 {
			out, _ = s.Text([]byte(input))
		} else {
			b, _ := json.Marshal(map[string]string{"level": "error", "message": input})
			encoded, _, err := s.JSON(b)
			if err != nil {
				t.Fatal(err)
			}
			assertSecretAbsent(t, string(encoded))
			var record map[string]string
			if err := json.Unmarshal(encoded, &record); err != nil {
				t.Fatal(err)
			}
			out = record["message"]
		}
		assertSecretAbsent(t, out)
		if !strings.HasPrefix(out, safeBefore+"\n") || !strings.HasSuffix(out, safeAfter+"\n") {
			t.Fatal("source snippet affected independent records", out)
		}
	})
}
