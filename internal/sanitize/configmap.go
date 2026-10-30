package sanitize

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ConfigMapJSON sanitizes data entries on an approved ConfigMap read endpoint.
// Generic JSON and embedded objects do not receive this exception.
func (s *Sanitizer) ConfigMapJSON(b []byte) (json.RawMessage, Report, error) {
	v, ok := parseJSON(b)
	root, object := v.(map[string]any)
	report := Report{}
	if !ok || !object || root["apiVersion"] != "v1" {
		return nil, report, errors.New("invalid ConfigMap response. Response discarded")
	}
	var cleaned any
	switch root["kind"] {
	case "ConfigMap":
		cleaned = s.configMap(root, &report)
	case "ConfigMapList":
		items, ok := root["items"].([]any)
		if !ok {
			return nil, report, errors.New("invalid ConfigMap list. Response discarded")
		}
		copy := make(map[string]any, len(root))
		for k, v := range root {
			if k != "items" {
				copy[k] = v
			}
		}
		result := s.walk(copy, 0, &report).(map[string]any)
		sanitized := make([]any, 0, len(items))
		for _, item := range items {
			object, ok := item.(map[string]any)
			if !ok || (object["kind"] != nil && object["kind"] != "ConfigMap") || (object["apiVersion"] != nil && object["apiVersion"] != "v1") {
				return nil, report, errors.New("invalid ConfigMap list item. Response discarded")
			}
			sanitized = append(sanitized, s.configMap(object, &report))
		}
		result[s.plain("items", &report)] = sanitized
		cleaned = result
	default:
		return nil, report, errors.New("unexpected ConfigMap response kind. Response discarded")
	}
	out, err := json.Marshal(cleaned)
	return out, report, err
}

func (s *Sanitizer) configMap(object map[string]any, report *Report) map[string]any {
	copy := make(map[string]any, len(object))
	for k, v := range object {
		if k != "data" {
			copy[k] = v
		}
	}
	result := s.walk(copy, 0, report).(map[string]any)
	if data, exists := object["data"]; exists {
		result[s.plain("data", report)] = s.configMapData(data, report)
	}
	return result
}

func (s *Sanitizer) configMapData(value any, report *Report) any {
	if value == nil {
		return nil
	}
	data, ok := value.(map[string]any)
	if !ok {
		report.Redactions++
		return Marker
	}
	out := make(map[string]any, len(data))
	for key, value := range data {
		cleanKey := s.plain(key, report)
		text, ok := value.(string)
		if !ok || sensitive(key) {
			out[cleanKey] = Marker
			report.Redactions++
			continue
		}
		if len(s.multilineMatches(normalizedText(text))) != 0 {
			out[cleanKey] = Marker
			report.Redactions++
			continue
		}
		if v, ok := parseJSON([]byte(text)); ok {
			encoded, _ := json.Marshal(s.walk(v, 1, report))
			out[cleanKey] = string(encoded)
		} else if lower := strings.ToLower(key); strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
			out[cleanKey] = s.configYAML(text, report)
		} else {
			out[cleanKey] = s.text(text, false, 1, report)
		}
	}
	return out
}

func (s *Sanitizer) configYAML(input string, report *Report) string {
	decoder := yaml.NewDecoder(strings.NewReader(input))
	var documents []string
	for {
		var node yaml.Node
		err := decoder.Decode(&node)
		if err == io.EOF {
			break
		}
		if err != nil {
			report.Redactions++
			return Marker
		}
		clean := s.configYAMLNode(&node, 0, report)
		if text, ok := clean.(string); ok && text == Marker {
			return Marker
		}
		encoded, err := yaml.Marshal(clean)
		if err != nil {
			report.Redactions++
			return Marker
		}
		documents = append(documents, string(encoded))
	}
	return strings.Join(documents, "---\n")
}

// configYAMLNode omits comments and masks anchors, aliases, unsupported tags, and deep nodes.
func (s *Sanitizer) configYAMLNode(node *yaml.Node, depth int, report *Report) any {
	mask := func() any { report.Redactions++; return Marker }
	if depth > 64 || node.Anchor != "" {
		return mask()
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) != 1 {
			return mask()
		}
		return s.configYAMLNode(node.Content[0], depth+1, report)
	case yaml.MappingNode:
		if node.Tag != "!!map" || len(node.Content)%2 != 0 {
			return mask()
		}
		out := map[string]any{}
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || seen[key.Value] {
				return mask()
			}
			seen[key.Value] = true
			cleanKey := s.plain(key.Value, report)
			if sensitive(key.Value) {
				out[cleanKey] = mask()
			} else {
				out[cleanKey] = s.configYAMLNode(value, depth+1, report)
			}
		}
		return out
	case yaml.SequenceNode:
		if node.Tag != "!!seq" {
			return mask()
		}
		out := make([]any, 0, len(node.Content))
		for _, value := range node.Content {
			out = append(out, s.configYAMLNode(value, depth+1, report))
		}
		return out
	case yaml.ScalarNode:
		if node.Tag == "!!str" {
			return s.text(node.Value, false, depth+1, report)
		}
		if s.configuredMatch(node.Value) {
			return mask()
		}
		switch node.Tag {
		case "!!null", "!!bool", "!!int", "!!float":
		default:
			return mask()
		}
		var scalar any
		if node.Decode(&scalar) != nil {
			return mask()
		}
		encoded, err := json.Marshal(scalar)
		if err != nil {
			return mask()
		}
		value, ok := parseJSON(encoded)
		if !ok {
			return mask()
		}
		clean := s.walk(value, depth+1, report)
		if _, numeric := clean.(json.Number); numeric {
			return scalar
		}
		return clean
	default:
		return mask()
	}
}
