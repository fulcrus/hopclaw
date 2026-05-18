package normalize

import (
	"fmt"
	"strings"
)

func String(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func DedupeStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func StringSlice(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return DedupeStrings(append([]string(nil), typed...))
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := String(item); text != "" {
				out = append(out, text)
			}
		}
		return DedupeStrings(out)
	default:
		if text := String(typed); text != "" {
			return []string{text}
		}
		return nil
	}
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func BoolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
