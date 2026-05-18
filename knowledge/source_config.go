package knowledge

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/keychain"
)

func configString(config map[string]any, key string) string {
	if len(config) == 0 {
		return ""
	}
	raw, ok := config[key]
	if !ok || raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func configBool(config map[string]any, key string) bool {
	if len(config) == 0 {
		return false
	}
	raw, ok := config[key]
	if !ok || raw == nil {
		return false
	}
	switch typed := raw.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func configStrings(config map[string]any, key string) []string {
	if len(config) == 0 {
		return nil
	}
	raw, ok := config[key]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return cleanConfigStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str := strings.TrimSpace(fmt.Sprintf("%v", item)); str != "" {
				out = append(out, str)
			}
		}
		return cleanConfigStrings(out)
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return cleanConfigStrings(strings.Split(typed, "\n"))
	default:
		return nil
	}
}

func cleanConfigStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func trimBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	return raw
}

func absoluteURL(baseURL, rawPath string) string {
	baseURL = trimBaseURL(baseURL)
	rawPath = strings.TrimSpace(rawPath)
	if strings.HasPrefix(rawPath, "http://") || strings.HasPrefix(rawPath, "https://") {
		return rawPath
	}
	return baseURL + "/" + strings.TrimLeft(rawPath, "/")
}

func normalizeNotionID(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "/")
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "notion.so") {
		if parsed, err := url.Parse(raw); err == nil {
			raw = parsed.Path
		}
	}
	raw = strings.Trim(raw, "/")
	if idx := strings.LastIndex(raw, "-"); idx >= 0 {
		raw = raw[idx+1:]
	}
	raw = strings.ReplaceAll(raw, "-", "")
	if len(raw) != 32 {
		return strings.TrimSpace(raw)
	}
	return raw[0:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
}

func notionTitle(properties map[string]any) string {
	for _, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if propType := configString(prop, "type"); propType != "title" {
			continue
		}
		if title, ok := prop["title"].([]any); ok {
			return strings.TrimSpace(joinRichText(title))
		}
	}
	return ""
}

func joinRichText(items []any) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, raw := range items {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if plain := configString(entry, "plain_text"); plain != "" {
			parts = append(parts, plain)
			continue
		}
		textObj, _ := entry["text"].(map[string]any)
		if content := configString(textObj, "content"); content != "" {
			parts = append(parts, content)
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func resolveSecretConfigString(config map[string]any, key string) (string, error) {
	value := configString(config, key)
	if value == "" {
		return "", nil
	}
	resolved, err := keychain.ResolveSecret(value)
	if err != nil {
		return "", fmt.Errorf("resolve secret field %q: %w", key, err)
	}
	return strings.TrimSpace(resolved), nil
}
