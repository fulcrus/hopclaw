package policy

import "strings"

type BlockedCommandMatcher struct {
	names    map[string]struct{}
	prefixes []string
}

func NewBlockedCommandMatcher(items []string) *BlockedCommandMatcher {
	if len(items) == 0 {
		return nil
	}
	names := make(map[string]struct{}, len(items))
	prefixes := make([]string, 0, len(items))
	for _, item := range items {
		normalized := normalizeBlockedCommand(item)
		if normalized == "" {
			continue
		}
		if strings.Contains(normalized, " ") {
			prefixes = append(prefixes, normalized)
			continue
		}
		names[normalized] = struct{}{}
	}
	if len(names) == 0 && len(prefixes) == 0 {
		return nil
	}
	prefixes = dedupeReasons(prefixes)
	return &BlockedCommandMatcher{
		names:    names,
		prefixes: prefixes,
	}
}

func (m *BlockedCommandMatcher) Match(command string) (string, bool) {
	if m == nil {
		return "", false
	}
	normalized := normalizeBlockedCommand(command)
	if normalized == "" {
		return "", false
	}
	fields := strings.Fields(normalized)
	if len(fields) == 0 {
		return "", false
	}
	if _, ok := m.names[fields[0]]; ok {
		return fields[0], true
	}
	for _, prefix := range m.prefixes {
		if normalized == prefix || strings.HasPrefix(normalized, prefix+" ") {
			return prefix, true
		}
	}
	return "", false
}

func normalizeBlockedCommand(raw string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(raw)), " "))
}
