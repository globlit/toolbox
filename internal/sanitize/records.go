package sanitize

import (
	"encoding/json"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const Withheld = "[WITHHELD: ambiguous log fragment]"

var assignment = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])(?:"([^"\r\n]+)"|'([^'\r\n]+)'|([A-Za-z_][A-Za-z0-9_.-]*))\s*[:=]\s*`)
var timestamp = regexp.MustCompile(`^(?:\d{4}-\d{2}-\d{2}[T ][0-9:.]+(?:Z|[+-]\d{2}:\d{2})?|\[\d{4}[-/]\d{2}[-/]\d{2}[ T][0-9:.]+(?:Z|[+-]\d{2}:\d{2})?\])[ \t]`)
var level = regexp.MustCompile(`(?i)^(?:\[(?:trace|debug|info|warn|warning|error|fatal|critical)\]|(?:trace|debug|info|warn|warning|error|fatal|critical)\b)[ :\t]+`)
var logfmtEnvelope = regexp.MustCompile(`(?:^|\s)(?:level|severity|ts|timestamp|logger|msg|message)=`)
var diagnosticEnvelope = regexp.MustCompile(`^(?:[IWEF]\d{4} \d{2}:\d{2}:\d{2}|[A-Za-z_][A-Za-z0-9_.]*(?:Error|Exception):|Error from server\b|Traceback \(most recent call last\):)`)
var structuredFragment = regexp.MustCompile(`\{|\[\s*(?:["'{\[\]0-9-]|true\b|false\b|null\b)|\\[uUx][0-9a-fA-F]`)

// Returns the start of the sensitive assignment and its value. This recognizes
// the same normalized keys as the structured walker, without evaluating logs.
func sensitiveValue(line string) (int, string, bool) {
	for _, m := range assignment.FindAllStringSubmatchIndex(line, -1) {
		start, end := assignmentKey(m)
		if sensitive(line[start:end]) {
			return start, line[m[1]:], true
		}
	}
	return 0, "", false
}

func assignmentKey(match []int) (int, int) {
	for i := 2; i < len(match); i += 2 {
		if match[i] >= 0 {
			return match[i], match[i+1]
		}
	}
	return 0, 0
}

// continuation tracks open values without interpreting assignments inside quotes.
func continuation(line string) (string, bool) {
	skipUntil := 0
	block := false
	for _, m := range assignment.FindAllStringSubmatchIndex(line, -1) {
		start, end := assignmentKey(m)
		if start < skipUntil {
			continue
		}
		value := line[m[1]:]
		for _, delimiter := range []string{`"""`, `'''`, `"`, `'`} {
			if !strings.HasPrefix(value, delimiter) {
				continue
			}
			close := quoteEndIndex(value[len(delimiter):], delimiter)
			if close < 0 {
				return delimiter, false
			}
			skipUntil = m[1] + len(delimiter)*2 + close
			break
		}
		if sensitive(line[start:end]) && (strings.TrimSpace(value) == "" || multilineSecret.MatchString(line[start:])) {
			block = true
		}
	}
	return "", block
}

func body(line string) string { return timestamp.ReplaceAllString(line, "") }

func recordStart(line string) bool {
	b := body(line)
	if level.MatchString(b) || logfmtEnvelope.MatchString(b) || diagnosticEnvelope.MatchString(b) {
		return true
	}
	if v, ok := parseJSON([]byte(b)); ok {
		if m, ok := v.(map[string]any); ok {
			for _, key := range []string{"level", "time", "timestamp", "msg", "message", "logger"} {
				if _, ok := m[key]; ok {
					return true
				}
			}
		}
	}
	return false
}

func quoteEnd(s, delimiter string) bool {
	return quoteEndIndex(s, delimiter) >= 0
}

func quoteEndIndex(s, delimiter string) int {
	for pos := 0; pos < len(s); {
		i := strings.Index(s[pos:], delimiter)
		if i < 0 {
			return -1
		}
		i += pos
		backslashes := 0
		for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			return i
		}
		pos = i + len(delimiter)
	}
	return -1
}

func normalizedText(s string) string {
	s = ansi.ReplaceAllStringFunc(strings.ToValidUTF8(s, ""), func(sequence string) string {
		return strings.Repeat("\n", strings.Count(sequence, "\n"))
	})
	return strings.Map(func(r rune) rune {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

// text fails closed at physical-line boundaries. A bad line never discards an
// otherwise independent record. Multiline sensitivity is resolved before JSON
// suffix parsing, so splitting a record cannot detach its sensitive label.
// strictBoundary distinguishes tailed log streams from context-bearing JSON fields.
func (s *Sanitizer) text(input string, strictBoundary bool, depth int, report *Report) string {
	if depth > 64 {
		report.Redactions++
		return Marker
	}
	normalized := normalizedText(input)
	lines := strings.Split(normalized, "\n")
	multilineMatches := s.multilineMatches(normalized)
	var quote string
	block, pem := false, false
	withhold := func(i int) { lines[i] = Withheld; report.Redactions++; report.WithheldLines++ }
	for i, line := range lines {
		b := body(line)
		trim := strings.TrimSpace(b)
		if trim == "" {
			continue
		}
		boundary := recordStart(line)
		if pem {
			if strings.Contains(line, "-----END ") {
				pem = false
				withhold(i)
				continue
			}
			withhold(i)
			continue
		}
		if strings.Contains(line, "-----BEGIN ") && (strings.Contains(line, "PRIVATE KEY-----") || strings.Contains(line, "CERTIFICATE-----")) {
			pem = !strings.Contains(line, "-----END ")
			withhold(i)
			continue
		}
		if quote != "" {
			// A secret can itself contain "INFO ..." or a JSON-looking line.
			// Never mistake that content for a new record before the quote closes.
			if quoteEnd(line, quote) {
				quote = ""
			}
			withhold(i)
			continue
		}
		if block {
			if !boundary {
				withhold(i)
				continue
			}
			block = false
		}
		nextQuote, nextBlock := continuation(line)
		if nextQuote != "" {
			quote = nextQuote
			withhold(i)
			continue
		}
		// Indentation in a tailed stream is an orphan continuation, even when
		// the remaining fragment happens to parse as a complete JSON value.
		if strictBoundary && (strings.HasPrefix(b, " ") || strings.HasPrefix(b, "\t")) {
			// Withholding an opening line must not discard its sensitivity
			// context: a following value may itself resemble a log envelope.
			block = nextBlock
			withhold(i)
			continue
		}
		start, v, ok := jsonSuffix(line)
		if ok {
			if _, _, sensitivePrefix := sensitiveValue(line[:start]); sensitivePrefix {
				withhold(i)
				continue
			}
			if _, object := v.(map[string]any); strictBoundary && !object {
				withhold(i)
				continue
			}
			clean, _ := json.Marshal(s.walk(v, depth+1, report))
			lines[i] = s.plain(line[:start], report) + string(clean)
			continue
		}
		if at, _, sensitive := sensitiveValue(line); sensitive {
			block = nextBlock
			quotedKey := at > 0 && strings.ContainsAny(line[at-1:at], `"'`)
			if block || quotedKey || structuredFragment.MatchString(line) {
				withhold(i)
			} else {
				// Preserve a harmless diagnostic prefix for ordinary assignments.
				lines[i] = s.plain(line[:at], report) + Marker
				report.Redactions++
			}
			continue
		}
		if structuredFragment.MatchString(level.ReplaceAllString(b, "")) {
			withhold(i)
			continue
		}
		if strictBoundary {
			// A bare atom, quoted value, scalar or closing delimiter lacks its
			// originating field. Do not guess that an isolated tail line is safe.
			_, scalar := parseJSON([]byte(trim))
			if scalar || strings.ContainsAny(trim[:1], "\"'}]),") || (!strings.ContainsAny(trim, " \t=") && !boundary) {
				withhold(i)
				continue
			}
			if !boundary && !timestamp.MatchString(line) {
				withhold(i)
				continue
			}
		}
		lines[i] = s.plain(line, report)
	}
	for i := range multilineMatches {
		if lines[i] != Withheld {
			withhold(i)
		}
	}
	return strings.Join(lines, "\n")
}

// Cross-line configured rules retain their meaning after record splitting. Mask
// whole affected lines, after parsing has recorded any continuation state.
func (s *Sanitizer) multilineMatches(input string) map[int]bool {
	masked := map[int]bool{}
	if !strings.Contains(input, "\n") {
		return masked
	}
	starts := []int{0}
	for i, b := range []byte(input) {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	mark := func(start, end int) {
		if end <= start || !strings.Contains(input[start:end], "\n") {
			return
		}
		first := sort.Search(len(starts), func(i int) bool { return starts[i] > start }) - 1
		last := sort.Search(len(starts), func(i int) bool { return starts[i] > end-1 }) - 1
		for i := first; i <= last; i++ {
			masked[i] = true
		}
	}
	for _, literal := range s.literals {
		if !strings.Contains(literal, "\n") {
			continue
		}
		for pos := 0; pos < len(input); {
			i := strings.Index(input[pos:], literal)
			if i < 0 {
				break
			}
			start := pos + i
			mark(start, start+len(literal))
			pos = start + len(literal)
		}
	}
	for _, pattern := range s.patterns {
		for _, span := range pattern.FindAllStringIndex(input, -1) {
			mark(span[0], span[1])
		}
	}
	return masked
}

func (s *Sanitizer) configuredMatch(value string) bool {
	for _, literal := range s.literals {
		if literal != "" && strings.Contains(value, literal) {
			return true
		}
	}
	for _, pattern := range s.patterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func (s *Sanitizer) scalarMatch(value string) bool {
	if s.configuredMatch(value) {
		return true
	}
	// Match equivalent exponent forms without float64 precision loss. Bound the
	// expansion so hostile JSON such as 1e999999999 cannot allocate unbounded memory.
	if len(s.literals) == 0 && len(s.patterns) == 0 {
		return false
	}
	if len(value) > 128 {
		return true
	}
	if i := strings.IndexAny(value, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(value[i+1:])
		if err != nil || exponent < -300 || exponent > 300 {
			return true
		}
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return true
	}
	if r.IsInt() {
		return s.configuredMatch(r.Num().String())
	}
	canonical := strings.TrimRight(strings.TrimRight(r.FloatString(428), "0"), ".")
	return s.configuredMatch(canonical)
}
