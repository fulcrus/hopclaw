package agent

import (
	"regexp"
	"sort"
	"strings"
)

var toolDiscoveryTokenPattern = regexp.MustCompile(`[a-z0-9][a-z0-9._:-]{1,}`)
var toolDiscoverySafeBareMarkers = buildToolDiscoverySafeBareMarkers()

// DiscoverDomainsByToolCatalog performs a strict catalog search over tool
// metadata using only technical/system identifiers. It intentionally avoids
// natural-language stop-word filtering, stemming, or description keyword
// heuristics so fallback activation stays language-agnostic and reviewable.
func DiscoverDomainsByToolCatalog(message string, catalog []ToolDefinition) map[ToolDomain]bool {
	tokens := toolDiscoveryTokens(message)
	if len(tokens) == 0 || len(catalog) == 0 {
		return nil
	}

	matchedDomains := make(map[ToolDomain]bool)
	for _, tool := range canonicalToolDefinitions(catalog) {
		domain := extractToolDomain(tool.Name)
		if domainTier[domain] != 3 {
			continue
		}
		if toolDiscoveryToolMatches(tool, tokens) {
			matchedDomains[domain] = true
		}
	}
	if len(matchedDomains) == 0 {
		return nil
	}
	return matchedDomains
}

func toolDiscoveryTokens(message string) []string {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return nil
	}
	raw := toolDiscoveryTokenPattern.FindAllString(lower, -1)
	if len(raw) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(strings.ToLower(strings.Trim(token, `"'()[]{}<>.,;:!?`)))
		if !toolDiscoveryTokenAllowed(token) {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func toolDiscoveryTokenAllowed(token string) bool {
	token = strings.TrimSpace(strings.ToLower(token))
	if token == "" {
		return false
	}
	if _, ok := toolDiscoverySafeBareMarkers[token]; ok {
		return true
	}
	return strings.ContainsAny(token, "._:-")
}

func buildToolDiscoverySafeBareMarkers() map[string]struct{} {
	out := map[string]struct{}{
		"api":            {},
		"atom":           {},
		"caldav":         {},
		"console":        {},
		"cron":           {},
		"css":            {},
		"csv":            {},
		"devtools":       {},
		"discord":        {},
		"dns":            {},
		"docx":           {},
		"ffmpeg":         {},
		"feishu":         {},
		"gmail":          {},
		"graphql":        {},
		"hnrss":          {},
		"hmac":           {},
		"http":           {},
		"https":          {},
		"ical":           {},
		"ics":            {},
		"imap":           {},
		"json":           {},
		"jwt":            {},
		"kubectl":        {},
		"localstorage":   {},
		"mailgun":        {},
		"mailto":         {},
		"md5":            {},
		"mp3":            {},
		"mp4":            {},
		"ocr":            {},
		"openssl":        {},
		"pdf":            {},
		"png":            {},
		"postgres":       {},
		"pptx":           {},
		"regex":          {},
		"redis":          {},
		"rss":            {},
		"rrule":          {},
		"sdk":            {},
		"selector":       {},
		"ses":            {},
		"sessionstorage": {},
		"sha1":           {},
		"sha256":         {},
		"sha512":         {},
		"slack":          {},
		"smtp":           {},
		"sqlite":         {},
		"stt":            {},
		"svg":            {},
		"telegram":       {},
		"tsv":            {},
		"tts":            {},
		"uuid":           {},
		"wav":            {},
		"webhook":        {},
		"wechat":         {},
		"xlsx":           {},
		"xml":            {},
		"xpath":          {},
		"yaml":           {},
	}
	for token := range structuredEvidenceTokenToDomain {
		out[token] = struct{}{}
	}
	return out
}

func toolDiscoveryToolMatches(tool ToolDefinition, tokens []string) bool {
	markers := toolDiscoveryMarkers(tool)
	if len(markers) == 0 {
		return false
	}
	for _, token := range tokens {
		if _, ok := markers[token]; ok {
			return true
		}
	}
	return false
}

func toolDiscoveryMarkers(tool ToolDefinition) map[string]struct{} {
	out := make(map[string]struct{}, 8)
	add := func(token string) {
		token = strings.TrimSpace(strings.ToLower(strings.Trim(token, `"'()[]{}<>.,;:!?`)))
		if !toolDiscoveryTokenAllowed(token) {
			return
		}
		out[token] = struct{}{}
	}

	for _, field := range []string{
		strings.TrimSpace(tool.Name),
		strings.TrimSpace(tool.Description),
		strings.TrimSpace(tool.Domain),
		strings.TrimSpace(tool.Category),
	} {
		field = strings.ToLower(field)
		if field == "" {
			continue
		}
		add(field)
		for _, token := range toolDiscoveryTokenPattern.FindAllString(field, -1) {
			add(token)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
