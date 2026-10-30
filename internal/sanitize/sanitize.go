// Package sanitize is a mandatory output boundary, independent of integrations.
// It reduces disclosure; it cannot classify every secret in arbitrary prose.
package sanitize

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"toolbox/internal/config"
)

const Marker = "[REDACTED]"

type Report struct {
	Redactions    int `json:"redactions"`
	WithheldLines int `json:"withheld_lines,omitempty"`
}
type Sanitizer struct {
	literals []string
	patterns []*regexp.Regexp
}

func New(c config.Redaction, credentials []string) (*Sanitizer, error) {
	s := &Sanitizer{literals: append(append([]string{}, c.Literals...), credentials...)}
	sort.Slice(s.literals, func(i, j int) bool { return len(s.literals[i]) > len(s.literals[j]) })
	for _, p := range c.Patterns {
		r, err := regexp.Compile(p)
		if err != nil {
			return nil, errors.New("invalid sanitizer pattern")
		}
		s.patterns = append(s.patterns, r)
	}
	return s, nil
}

var normalize = regexp.MustCompile(`[^a-z0-9]`)

func sensitive(key string) bool {
	k := normalize.ReplaceAllString(strings.ToLower(key), "")
	for _, part := range []string{"password", "passwd", "requirepass", "masterauth", "secret", "token", "authorization", "credential", "apikey", "privatekey", "accesskey", "connectionstring", "cookie"} {
		if strings.Contains(k, part) {
			return true
		}
	}
	switch k {
	case "pwd", "auth", "authentication", "headers", "httpheaders", "env", "envfrom", "value", "data", "stringdata", "binarydata", "annotations", "managedfields", "command", "args", "lastappliedconfiguration", "clientkeydata", "clientcertificatedata", "tlskey", "tlscert", "query", "sql", "statement", "parameters", "detail", "context", "body", "request", "response", "requestbody", "responsebody", "payload", "dsn", "databaseurl", "email", "username", "user", "dbname":
		return true
	}
	return false
}

type rule struct {
	re          *regexp.Regexp
	replacement string
}

var rules = []rule{
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*(?:PRIVATE KEY|CERTIFICATE)-----.*?(?:-----END [A-Z0-9 ]*(?:PRIVATE KEY|CERTIFICATE)-----|$)`), Marker},
	{regexp.MustCompile(`(?im)\b(?:AUTH|requirepass|masterauth)\s+[^\r\n]+`), Marker},
	{regexp.MustCompile(`(?i)\b(?:Bearer|Basic)\s+[A-Za-z0-9+/=_~.:-]+`), Marker},
	{regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s/"'<>]*@`), "[REDACTED_URI_CREDENTIALS]@"},
	{regexp.MustCompile(`(?i)(https?://[^\s?"'<>]+)\?[^\s"'<>]*`), "${1}?[REDACTED_QUERY]"},
	{regexp.MustCompile(`(?i)\b(?:eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]*)?|AKIA[A-Z0-9]{16}|ASIA[A-Z0-9]{16}|gh[pousr]_[A-Za-z0-9]+|github_pat_[A-Za-z0-9_]+|glpat-[A-Za-z0-9_-]+|xox[baprs]-[A-Za-z0-9-]+)\b`), Marker},
	// In free text, consume the remainder of a sensitive assignment's line.
	// This deliberately sacrifices detail to cover spaces, quotes and malformed JSON.
	{regexp.MustCompile(`(?im)(?:\b[\w.-]*(?:password|passwd|requirepass|masterauth|secret|token|authorization|credential|api[_-]?key|private[_-]?key|access[_-]?key|cookie|connection[_-]?string)[\w.-]*|\b(?:pwd|auth|user(?:name)?|email|dsn|database_url|statement|query|sql|parameters|detail|request_body|response_body|payload))['"]?\s*[:=]\s*[^\r\n]*`), Marker},
	{regexp.MustCompile(`(?i)\b(?:user(?:name)?|database|email)\s+(?:'[^'\r\n]*'|"[^"\r\n]*")`), Marker},
	{regexp.MustCompile(`(?i)\b[A-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[A-Z0-9-]+(?:\.[A-Z0-9-]+)+\b`), "[REDACTED_EMAIL]"},
	{regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`), "[REDACTED_IP]"},
	{regexp.MustCompile(`\b[A-Za-z0-9_+/=-]{40,}\b`), Marker},
}
var ipv6 = regexp.MustCompile(`[0-9a-fA-F:]*:[0-9a-fA-F:]+(?:%[a-zA-Z0-9]+)?`)
var ansi = regexp.MustCompile("\x1b(?:\\[[0-?]*[ -/]*[@-~]|\\][^\x07]*(?:\x07|\x1b\\\\))")
var multilineSecret = regexp.MustCompile(`(?i)(?:password|passwd|requirepass|masterauth|secret|token|credential|private[_-]?key|api[_-]?key|payload|body)\s*[:=]\s*[|>+-]*\s*$`)

func (s *Sanitizer) replace(text string, r *regexp.Regexp, replacement string, report *Report) string {
	return r.ReplaceAllStringFunc(text, func(match string) string { report.Redactions++; return r.ReplaceAllString(match, replacement) })
}

func (s *Sanitizer) plain(text string, report *Report) string {
	text = ansi.ReplaceAllString(text, "")
	text = strings.Map(func(r rune) rune {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)

	for _, r := range rules {
		text = s.replace(text, r.re, r.replacement, report)
	}
	text = ipv6.ReplaceAllStringFunc(text, func(m string) string {
		host := strings.Split(m, "%")[0]
		if net.ParseIP(host) != nil {
			report.Redactions++
			return "[REDACTED_IP]"
		}
		return m
	})
	for _, literal := range s.literals {
		if literal != "" {
			report.Redactions += strings.Count(text, literal)
			text = strings.ReplaceAll(text, literal, Marker)
		}
	}

	for _, r := range s.patterns {
		text = s.replace(text, r, Marker, report)
	}
	return text
}

func parseJSON(b []byte) (any, bool) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil || d.Decode(new(any)) != io.EOF {
		return nil, false
	}
	return v, true
}

func (s *Sanitizer) walk(v any, depth int, report *Report) any {
	if depth > 64 {
		report.Redactions++
		return Marker
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, value := range x {
			key := s.plain(k, report)
			if sensitive(k) {
				out[key] = Marker
				report.Redactions++
			} else {
				out[key] = s.walk(value, depth+1, report)
			}
		}
		return out
	case []any:
		for i := range x {
			x[i] = s.walk(x[i], depth+1, report)
		}
		return x
	case string:
		return s.text(x, false, depth+1, report)
	case json.Number:
		if s.scalarMatch(x.String()) {
			report.Redactions++
			return Marker
		}
		return x
	case bool:
		if s.configuredMatch(strconv.FormatBool(x)) {
			report.Redactions++
			return Marker
		}
		return x
	case nil:
		if s.configuredMatch("null") {
			report.Redactions++
			return Marker
		}
		return nil
	default:
		return v
	}
}

func (s *Sanitizer) JSON(b []byte) (json.RawMessage, Report, error) {
	v, ok := parseJSON(b)
	if !ok {
		return nil, Report{}, errors.New("invalid structured output; response withheld")
	}
	report := Report{}
	result, err := json.Marshal(s.walk(v, 0, &report))
	return result, report, err
}

func (s *Sanitizer) Text(b []byte) (string, Report) {
	report := Report{}
	return s.text(string(b), true, 0, &report), report
}

func jsonSuffix(line string) (int, any, bool) {
	pos := 0
	for attempt := 0; attempt < 16; attempt++ {
		i := strings.IndexAny(line[pos:], "{[")
		if i < 0 {
			break
		}
		pos += i
		if v, ok := parseJSON([]byte(line[pos:])); ok {
			return pos, v, true
		}
		pos++
	}
	return 0, nil, false
}
