package sanitize

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestCodeInLogsHasNoSideEffects(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	original := []byte("unchanged synthetic file content")
	if err := os.WriteFile(marker, original, 0600); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	path, url := jsonQuote(marker), jsonQuote(server.URL)
	snippets := map[string]string{
		"go":         fmt.Sprintf("package main\nimport (\"os\"; \"net/http\")\nfunc main() { os.WriteFile(%s, []byte(\"executed\"), 0600); http.Get(%s) }", path, url),
		"python":     fmt.Sprintf("from pathlib import Path\nfrom urllib.request import urlopen\nPath(%s).write_text('executed')\nurlopen(%s)", path, url),
		"javascript": fmt.Sprintf("require('fs').writeFileSync(%s, 'executed'); fetch(%s);", path, url),
		"ruby":       fmt.Sprintf("File.write(%s, 'executed'); require 'net/http'; Net::HTTP.get(URI(%s))", path, url),
		"php":        fmt.Sprintf("<?php file_put_contents(%s, 'executed'); file_get_contents(%s); ?>", path, url),
		"shell":      fmt.Sprintf("printf executed > %s; curl %s", path, url),
		"powershell": fmt.Sprintf("Set-Content -LiteralPath %s -Value executed; Invoke-WebRequest %s", path, url),
		"sql":        fmt.Sprintf("COPY (SELECT 'executed') TO PROGRAM 'printf executed > %s';", marker),
		"template":   "{{7 * 7}} ${7*7} $(printf executed) `printf executed`",
		"read-file":  fmt.Sprintf("cat %s; print(open(%s).read()); require('fs').readFileSync(%s)", path, path, path),
	}
	s := unconfigured(t)
	for language, source := range snippets {
		t.Run(language, func(t *testing.T) {
			inputs := []string{source, "INFO source=" + jsonQuote(source), "```" + language + "\n" + source + "\n```"}
			for _, input := range inputs {
				text, _ := s.Text([]byte(input))
				b, _ := json.Marshal(map[string]string{"level": "warn", "message": input})
				structured, _, err := s.JSON(b)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(text, string(original)) || strings.Contains(string(structured), string(original)) {
					t.Fatal("log source read the file")
				}
				after, err := os.ReadFile(marker)
				if err != nil || string(after) != string(original) {
					t.Fatal("log source changed the file", err)
				}
				if requests.Load() != 0 {
					t.Fatal("log source made a network request")
				}
			}
		})
	}
}

func TestGeneratedMixedLogFormats(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5eed))
	s := unconfigured(t)
	for i := 0; i < 400; i++ {
		noise := make([]byte, rng.Intn(2048))
		_, _ = rng.Read(noise)
		value := syntheticSecret + " " + string(noise)
		encoded := jsonQuote(value)
		var record string
		switch i % 8 {
		case 0:
			record = `{"level":"warn","password":` + encoded + `,"status":"failed"}`
		case 1:
			record = `level=warn "api key"=` + encoded + ` status=failed`
		case 2:
			record = `[2026-10-30T12:34:56Z] [ERROR] {"headers":{"X-Custom":` + encoded + `}}`
		case 3:
			record = `INFO {"pass\u0077ord":{"unknown":` + encoded + `}`
		case 4:
			record = `level=warn password="begin` + "\nINFO " + syntheticSecret + "\nend\""
		case 5:
			record = `{"level":"error","query":` + jsonQuote("SELECT '"+value+"' FROM credentials") + `}`
		case 6:
			record = `level=error payload=` + jsonQuote(`{"password":`+encoded+`}`)
		case 7:
			record = "Traceback (most recent call last):\n  File \"/srv/generated.py\", line 42, in handler\nValueError: password=" + encoded
		}
		input := safeBefore + "\n" + record + "\n" + safeAfter + "\n"
		if rng.Intn(2) == 1 {
			input = strings.ReplaceAll(input, "\n", "\r\n")
		}
		out, r := s.Text([]byte(input))
		assertSecretAbsent(t, out)
		if !strings.HasPrefix(out, safeBefore+"\n") || !strings.HasSuffix(out, safeAfter+"\n") || strings.Count(out, "\n") != strings.Count(input, "\n") || r.Redactions == 0 {
			t.Fatalf("generated case %d lost boundaries or report: %q -> %q", i, input, out)
		}
	}
}

func TestTinyLogs(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", ""}, {"\n", "\n"}, {"\r\n", "\n"}, {"\x00", ""}, {"\xff", ""},
		{"x", Withheld}, {"0", Withheld}, {"[]", Withheld}, {"{}", "{}"}, {"\"", Withheld}, {"=", Withheld}, {"\t", "\t"},
	} {
		out, _ := unconfigured(t).Text([]byte(tc.input))
		if out != tc.want {
			t.Fatalf("%q -> %q; want %q", tc.input, out, tc.want)
		}
	}
	for _, value := range []string{"x", "42", "'", "\"", "\\", "\x00", "\n"} {
		input, _ := json.Marshal(map[string]string{"password": value, "status": "ok"})
		out, _, err := unconfigured(t).JSON(input)
		var result map[string]string
		if err != nil || json.Unmarshal(out, &result) != nil || result["password"] != Marker || result["status"] != "ok" {
			t.Fatal(string(out), err)
		}
	}
}

func TestDeterministicRandomLogBytes(t *testing.T) {
	s := unconfigured(t)
	for seed := int64(0); seed < 16; seed++ {
		rng := rand.New(rand.NewSource(seed))
		for _, size := range []int{1, 7, 64, 1024, 16384, 65536} {
			bytes := make([]byte, size)
			_, _ = rng.Read(bytes)
			mid := size / 2
			input := append([]byte{}, bytes[:mid]...)
			input = append(input, []byte("\nINFO password="+syntheticSecret+"\n")...)
			input = append(input, bytes[mid:]...)
			out, r := s.Text(input)
			assertSecretAbsent(t, out)
			if !utf8.ValidString(out) || strings.Count(out, "\n") != strings.Count(string(input), "\n") || r.Redactions < r.WithheldLines {
				t.Fatal("random byte invariants failed", seed, size)
			}
			for _, ch := range out {
				if (unicode.IsControl(ch) && ch != '\n' && ch != '\t') || unicode.Is(unicode.Cf, ch) {
					t.Fatalf("random bytes exposed control character %U", ch)
				}
			}
		}
	}
}

func TestLargeLogs(t *testing.T) {
	s := unconfigured(t)
	for _, size := range []int{64 << 10, 1 << 20, 8 << 20} {
		t.Run(fmt.Sprintf("single-line-%d", size), func(t *testing.T) {
			input := safeBefore + "\n" + `INFO {"password":"` + syntheticSecret + `", broken: ` + strings.Repeat("x ", size/2) + "\n" + safeAfter + "\n"
			out, r := s.Text([]byte(input))
			if out != safeBefore+"\n"+Withheld+"\n"+safeAfter+"\n" || r.WithheldLines != 1 {
				t.Fatal("large malformed line affected independent records", r)
			}
		})
	}
	t.Run("mixed-dump-1MiB", func(t *testing.T) {
		var input strings.Builder
		input.Grow((1 << 20) + 100)
		input.WriteString(safeBefore + "\n")
		sensitive := 0
		for i := 0; input.Len() < 1<<20; i++ {
			if i%257 == 0 {
				input.WriteString("level=warn password=" + syntheticSecret + "\n")
				sensitive++
			} else {
				input.WriteString("INFO status=ready operation=healthcheck\n")
			}
		}
		input.WriteString(safeAfter + "\n")
		out, r := s.Text([]byte(input.String()))
		assertSecretAbsent(t, out)
		if !strings.HasPrefix(out, safeBefore+"\n") || !strings.HasSuffix(out, safeAfter+"\n") || strings.Count(out, "\n") != strings.Count(input.String(), "\n") || r.Redactions < sensitive {
			t.Fatal("large dump lost records or report", r)
		}
	})
}
