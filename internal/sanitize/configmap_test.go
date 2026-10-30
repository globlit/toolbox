package sanitize

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/globlit/toolbox/internal/config"
	"go.yaml.in/yaml/v3"
)

func configMapInput(t testing.TB, data any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "settings", "annotations": map[string]string{"note": "annotation-canary"}}, "data": data, "binaryData": map[string]string{"blob": "binary-canary"}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestConfigMapReadableData(t *testing.T) {
	s, err := New(config.Redaction{Literals: []string{"known-canary"}, Patterns: []string{`account-[0-9]+`}}, []string{"credential-canary"})
	if err != nil {
		t.Fatal(err)
	}
	input := configMapInput(t, map[string]string{
		"LOG_LEVEL": "debug", "PORT": "8080", "ENABLED": "true", "EMPTY": "", "GREETING": "hello world",
		"db.password": "key-canary", "API_KEY": "key-canary", "pass\u200bword": "key-canary",
		"CUSTOM": "before known-canary after", "OTHER": "credential-canary", "ACCOUNT": "account-123",
		"ENDPOINT": "https://example.org/health", "DSN_TEXT": "postgres://reader:uri-canary@database/app",
		"HEADER_TEXT":    "Bearer bearer-canary",
		"settings.json":  `{"port":8080,"password":"json-canary","nested":{"enabled":true,"api_key":"json-canary"}}`,
		"app.properties": "port=8080\npassword=properties-canary\nlog_level=debug\n",
		"settings.yaml":  "port: 8080\ncredentials:\n  password: yaml-canary\nfeature:\n  enabled: true\nmessage: hello world\n",
	})
	out, r, err := s.ConfigMapJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"key-canary", "known-canary", "credential-canary", "account-123", "uri-canary", "bearer-canary", "json-canary", "properties-canary", "yaml-canary", "annotation-canary", "binary-canary"} {
		if strings.Contains(string(out), secret) {
			t.Fatal("ConfigMap leaked", secret, string(out))
		}
	}
	var object struct {
		Data       map[string]string `json:"data"`
		BinaryData string            `json:"binaryData"`
	}
	if json.Unmarshal(out, &object) != nil || object.BinaryData != Marker || r.Redactions == 0 {
		t.Fatal(string(out), r)
	}
	for key, want := range map[string]string{"LOG_LEVEL": "debug", "PORT": "8080", "ENABLED": "true", "EMPTY": "", "GREETING": "hello world", "ENDPOINT": "https://example.org/health", "db.password": Marker, "API_KEY": Marker, "password": Marker, "CUSTOM": "before " + Marker + " after"} {
		if object.Data[key] != want {
			t.Errorf("%s: %q; want %q", key, object.Data[key], want)
		}
	}
	if !strings.Contains(object.Data["app.properties"], "port=8080") || !strings.Contains(object.Data["app.properties"], "log_level=debug") {
		t.Fatal(object.Data)
	}
	var settings map[string]any
	if yaml.Unmarshal([]byte(object.Data["settings.yaml"]), &settings) != nil || settings["port"] != 8080 || settings["message"] != "hello world" {
		t.Fatal(object.Data["settings.yaml"])
	}
	var embedded map[string]any
	if json.Unmarshal([]byte(object.Data["settings.json"]), &embedded) != nil || embedded["port"] != float64(8080) || embedded["password"] != Marker {
		t.Fatal(object.Data)
	}
}

func TestConfigMapExceptionScope(t *testing.T) {
	s := unconfigured(t)
	object := configMapInput(t, map[string]string{"ordinary": "visible-value"})
	out, _, err := s.JSON(object)
	if err != nil || strings.Contains(string(out), "visible-value") {
		t.Fatal("generic JSON adopted ConfigMap exception", string(out), err)
	}
	logs, _ := s.Text(object)
	if strings.Contains(logs, "visible-value") {
		t.Fatal("log kind spoof bypass", logs)
	}
	nested := configMapInput(t, map[string]string{"nested.json": string(object)})
	out, _, err = s.ConfigMapJSON(nested)
	if err != nil || strings.Contains(string(out), "visible-value") {
		t.Fatal("nested kind spoof bypass", string(out), err)
	}
	for _, input := range []string{`{}`, `{"apiVersion":"v1","kind":"Secret","data":{"x":"x"}}`, `{"apiVersion":"v1","kind":"ConfigMapList","items":[{"kind":"Secret"}]}`, `{"apiVersion":"v1","kind":"ConfigMapList","items":null}`, `{"apiVersion":"evil/v1","kind":"ConfigMap"}`} {
		if _, _, err := s.ConfigMapJSON([]byte(input)); err == nil {
			t.Fatal("wrong response accepted", input)
		}
	}
	list := `{"apiVersion":"v1","kind":"ConfigMapList","items":[{"metadata":{"name":"a"},"data":{"port":"8080","password":"list-canary"}}]}`
	out, _, err = s.ConfigMapJSON([]byte(list))
	if err != nil || !strings.Contains(string(out), `"port":"8080"`) || strings.Contains(string(out), "list-canary") {
		t.Fatal(string(out), err)
	}
}

func TestConfigMapMalformedAndYAMLValues(t *testing.T) {
	s, err := New(config.Redaction{Literals: []string{"654321", "first:\nsecond:"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, input string
		wantMarker  bool
	}{
		{"malformed.yaml", "port: [oops\npassword: hidden-canary", true},
		{"duplicate.yaml", "port: 80\nport: 90\npassword: hidden-canary", true},
		{"aliases.yaml", "ordinary: &shared hidden-canary\npassword: *shared\nport: 8080", false},
		{"cycle.yaml", "items: &cycle [*cycle]\nport: 8080", false},
		{"tags.yaml", "ordinary: !vault hidden-canary\nport: 8080", false},
		{"binary.yaml", "ordinary: !!binary aGlkZGVuLWNhbmFyeQ==\nport: 8080", false},
		{"comments.yaml", "port: 8080 # hidden-canary\n# hidden-canary", false},
		{"numeric.yaml", "ordinary: 0x9fbf1\nport: 8080", false},
		{"multidoc.yaml", "port: 8080\n---\npassword: hidden-canary", false},
		{"crossline.yaml", "first:\nsecond:\nport: 8080", true},
		{"malformed.json", `{"password":{"ordinary":"hidden-canary"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := s.ConfigMapJSON(configMapInput(t, map[string]string{tc.name: tc.input, "MODE": "production"}))
			if err != nil || strings.Contains(string(out), "hidden-canary") || strings.Contains(string(out), "aGlkZGVuLWNhbmFyeQ==") || strings.Contains(string(out), "654321") {
				t.Fatal(string(out), err)
			}
			var result struct{ Data map[string]string }
			if json.Unmarshal(out, &result) != nil || result.Data["MODE"] != "production" {
				t.Fatal(string(out))
			}
			if tc.wantMarker && result.Data[tc.name] != Marker {
				t.Fatal("ambiguous file not redacted", string(out))
			}
		})
	}
	for _, data := range []any{[]any{"hidden-canary"}, map[string]any{"bad": map[string]string{"x": "hidden-canary"}, "port": "8080"}} {
		out, _, err := s.ConfigMapJSON(configMapInput(t, data))
		if err != nil || strings.Contains(string(out), "hidden-canary") {
			t.Fatal(string(out), err)
		}
	}
}

func FuzzConfigMapData(f *testing.F) {
	f.Add([]byte("hello"), uint8(0))
	f.Add([]byte("quotes\"\npassword=unknown"), uint8(1))
	f.Fuzz(func(t *testing.T, noise []byte, format uint8) {
		if len(noise) > 4096 {
			t.Skip()
		}
		value := syntheticSecret + string(noise)
		data := map[string]string{"SAFE": "readable", "password": value}
		switch format % 3 {
		case 0:
			b, _ := json.Marshal(map[string]string{"password": value, "mode": "debug"})
			data["app.json"] = string(b)
		case 1:
			b, _ := yaml.Marshal(map[string]string{"password": value, "mode": "debug"})
			data["app.yaml"] = string(b)
		case 2:
			data["app.properties"] = "mode=debug\npassword=" + jsonQuote(value) + "\n"
		}
		out, _, err := unconfigured(t).ConfigMapJSON(configMapInput(t, data))
		if err != nil || !json.Valid(out) {
			t.Fatal(err)
		}
		assertSecretAbsent(t, string(out))
		var result struct{ Data map[string]string }
		if json.Unmarshal(out, &result) != nil || result.Data["SAFE"] != "readable" {
			t.Fatal(string(out))
		}
	})
}

func TestConfigMapYAMLConfiguredScalarSpellings(t *testing.T) {
	s, err := New(config.Redaction{Literals: []string{"0x10", "TRUE", "654_321"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := s.ConfigMapJSON(configMapInput(t, map[string]string{"app.yaml": "hex: 0x10\nflag: TRUE\nnumber: 654_321\nport: 8080"}))
	if err != nil {
		t.Fatal(err)
	}
	var result struct{ Data map[string]string }
	if json.Unmarshal(out, &result) != nil {
		t.Fatal(string(out))
	}
	var values map[string]any
	if yaml.Unmarshal([]byte(result.Data["app.yaml"]), &values) != nil {
		t.Fatal(result.Data)
	}
	for _, key := range []string{"hex", "flag", "number"} {
		if values[key] != Marker {
			t.Errorf("configured scalar %s escaped via normalization: %v", key, values[key])
		}
	}
	if values["port"] != 8080 {
		t.Fatal(values)
	}
}
