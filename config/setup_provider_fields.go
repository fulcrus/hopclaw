package config

import (
	"fmt"
	"strings"
)

func SetupProviderFieldType(field SetupProviderField) string {
	switch strings.TrimSpace(strings.ToLower(field.Type)) {
	case "url", "password", "duration", "string_list", "string_map", "text":
		return strings.TrimSpace(strings.ToLower(field.Type))
	default:
		if field.Secret {
			return "password"
		}
		if strings.TrimSpace(field.ID) == "base_url" {
			return "url"
		}
		return "text"
	}
}

func SplitSetupProviderFieldList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ParseSetupProviderFieldMap(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	out := make(map[string]string, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		index := strings.Index(line, ":")
		if index <= 0 {
			return nil, fmt.Errorf("invalid entry %q: expected key: value", line)
		}
		key := strings.TrimSpace(line[:index])
		if key == "" {
			return nil, fmt.Errorf("invalid entry %q: key is required", line)
		}
		out[key] = strings.TrimSpace(line[index+1:])
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func SetupProviderFieldHasValue(field SetupProviderField, raw string) bool {
	switch SetupProviderFieldType(field) {
	case "string_list":
		return len(SplitSetupProviderFieldList(raw)) > 0
	case "string_map":
		items, err := ParseSetupProviderFieldMap(raw)
		return err == nil && len(items) > 0
	default:
		return strings.TrimSpace(raw) != ""
	}
}
