package contextengine

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"regexp"
	"sort"
	"strings"
)

const (
	defaultToolOutputCompactThreshold = 2000
	toolOutputGenericKeepTokens       = 1500
	toolOutputJSONPreviewItems        = 3
	toolOutputJSONPreviewMaxChars     = 200
	toolOutputLogHeadLines            = 20
	toolOutputLogTailLines            = 10
)

var (
	htmlTagPattern             = regexp.MustCompile(`(?is)<[a-z!/][^>]*>`)
	htmlScriptPattern          = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	htmlStylePattern           = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	htmlCommentPattern         = regexp.MustCompile(`(?is)<!--.*?-->`)
	htmlTitlePattern           = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
	htmlHeadingPattern         = regexp.MustCompile(`(?is)<h([1-3])\b[^>]*>(.*?)</h[1-3]>`)
	htmlMetaTagPattern         = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	htmlMetaNamePattern        = regexp.MustCompile(`(?i)\bname\s*=\s*("description"|'description')`)
	htmlMetaContentAttrPattern = regexp.MustCompile(`(?is)\bcontent\s*=\s*("([^"]*)"|'([^']*)')`)
	logSignalPattern           = regexp.MustCompile(`(?i)(^\d{4}-\d{2}-\d{2}|^\[\w+\]|^\$ |INFO|WARN|ERROR|DEBUG|TRACE|FATAL|panic|exception|stack trace|exit status)`)
)

// CompactToolOutput 对超长工具输出做轻量压缩。
// threshold: 超过此 token 数才压缩；<=0 时默认 2000。
func CompactToolOutput(output string, threshold int) string {
	if output == "" {
		return output
	}
	if threshold <= 0 {
		threshold = defaultToolOutputCompactThreshold
	}

	totalTokens := estimateToolOutputTokens(output)
	if totalTokens <= threshold {
		return output
	}

	if compacted, ok := compactJSONOutput(output, totalTokens); ok {
		return compacted
	}
	if compacted, ok := compactHTMLOutput(output, totalTokens); ok {
		return compacted
	}
	if compacted, ok := compactLogOutput(output, totalTokens); ok {
		return compacted
	}
	return compactPlainToolOutput(output, totalTokens)
}

func estimateToolOutputTokens(output string) int {
	if output == "" {
		return 0
	}
	estimator := CharRatioEstimator{
		CharsPerToken:        defaultCharsPerToken,
		ToolCharsPerToken:    2.0,
		EmptyMessageOverhead: 0,
		SafetyMargin:         1.0,
	}
	ratio := estimator.ratioForText(output, true)
	if ratio <= 0 {
		ratio = 2.0
	}
	return int(math.Max(1, math.Ceil(float64(len(output))/ratio)))
}

func compactJSONOutput(output string, totalTokens int) (string, bool) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return "", false
	}
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return "", false
	}

	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return "", false
	}

	lines := []string{"[tool output compacted: json]"}
	switch value := decoded.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		lines = append(lines, "Top-level keys: "+summarizeKeys(keys))
		previewCount := minInt(toolOutputJSONPreviewItems, len(keys))
		for i := 0; i < previewCount; i++ {
			key := keys[i]
			lines = append(lines, fmt.Sprintf("- %s: %s", key, previewJSONValue(value[key])))
		}
		lines = append(lines, fmt.Sprintf("... (%d items total, ~%d tokens)", len(keys), totalTokens))
	case []any:
		previewCount := minInt(toolOutputJSONPreviewItems, len(value))
		for i := 0; i < previewCount; i++ {
			lines = append(lines, fmt.Sprintf("- [%d] %s", i, previewJSONValue(value[i])))
		}
		lines = append(lines, fmt.Sprintf("... (%d items total, ~%d tokens)", len(value), totalTokens))
	default:
		lines = append(lines, previewJSONValue(value))
		lines = append(lines, fmt.Sprintf("... (~%d tokens)", totalTokens))
	}
	return strings.Join(lines, "\n"), true
}

func compactHTMLOutput(output string, totalTokens int) (string, bool) {
	trimmed := strings.TrimSpace(output)
	if !looksLikeHTML(trimmed) {
		return "", false
	}

	sanitized := htmlScriptPattern.ReplaceAllString(output, " ")
	sanitized = htmlStylePattern.ReplaceAllString(sanitized, " ")
	sanitized = htmlCommentPattern.ReplaceAllString(sanitized, " ")

	var lines []string
	lines = append(lines, "[tool output compacted: html]")

	if title := firstHTMLMatch(htmlTitlePattern, sanitized); title != "" {
		lines = append(lines, "Title: "+title)
	}
	if description := extractMetaDescription(sanitized); description != "" {
		lines = append(lines, "Description: "+description)
	}
	headings := extractHTMLHeadings(sanitized)
	if len(headings) > 0 {
		lines = append(lines, "Headings:")
		for _, heading := range headings {
			lines = append(lines, "- "+heading)
		}
	}

	body := normalizeWhitespace(stripHTMLTags(sanitized))
	if len(lines) == 1 && body == "" {
		return "", false
	}
	if body != "" {
		body = clampText(body, 400)
		lines = append(lines, "Body preview: "+body)
	}
	lines = append(lines, fmt.Sprintf("... (~%d tokens)", totalTokens))
	return strings.Join(lines, "\n"), true
}

func compactLogOutput(output string, totalTokens int) (string, bool) {
	lines := splitLines(output)
	if !looksLikeLogOutput(lines) {
		return "", false
	}
	if len(lines) <= toolOutputLogHeadLines+toolOutputLogTailLines {
		return "", false
	}

	omitted := len(lines) - toolOutputLogHeadLines - toolOutputLogTailLines
	compacted := make([]string, 0, toolOutputLogHeadLines+toolOutputLogTailLines+2)
	compacted = append(compacted, "[tool output compacted: log]")
	compacted = append(compacted, lines[:toolOutputLogHeadLines]...)
	compacted = append(compacted, fmt.Sprintf("... (%d lines omitted, ~%d tokens total)", omitted, totalTokens))
	compacted = append(compacted, lines[len(lines)-toolOutputLogTailLines:]...)
	return strings.Join(compacted, "\n"), true
}

func compactPlainToolOutput(output string, totalTokens int) string {
	ratio := CharRatioEstimator{
		CharsPerToken:        defaultCharsPerToken,
		ToolCharsPerToken:    2.0,
		EmptyMessageOverhead: 0,
		SafetyMargin:         1.0,
	}.ratioForText(output, true)
	if ratio <= 0 {
		ratio = 2.0
	}

	maxChars := int(math.Ceil(float64(toolOutputGenericKeepTokens) * ratio))
	if maxChars <= 0 {
		maxChars = len(output) / 2
	}
	if maxChars >= len(output) {
		maxChars = len(output) / 2
	}
	if maxChars <= 0 {
		maxChars = len(output)
	}

	trimmed := strings.TrimSpace(output)
	if len(trimmed) > maxChars {
		trimmed = strings.TrimSpace(trimmed[:maxChars])
	}
	return trimmed + fmt.Sprintf("\n... (truncated, ~%d tokens total)", totalTokens)
}

func summarizeKeys(keys []string) string {
	if len(keys) == 0 {
		return "(none)"
	}
	if len(keys) <= 12 {
		return strings.Join(keys, ", ")
	}
	return strings.Join(keys[:12], ", ") + ", ..."
}

func previewJSONValue(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return "<unserializable>"
	}
	return clampText(string(body), toolOutputJSONPreviewMaxChars)
}

func looksLikeHTML(text string) bool {
	if text == "" {
		return false
	}
	if !htmlTagPattern.MatchString(text) {
		return false
	}
	lower := strings.ToLower(text)
	for _, marker := range []string{"<!doctype html", "<html", "<head", "<body", "<title", "<meta", "<h1", "<h2", "<h3"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func firstHTMLMatch(pattern *regexp.Regexp, text string) string {
	matches := pattern.FindStringSubmatch(text)
	if len(matches) < 2 {
		return ""
	}
	return normalizeWhitespace(stripHTMLTags(matches[1]))
}

func extractMetaDescription(text string) string {
	tags := htmlMetaTagPattern.FindAllString(text, -1)
	for _, tag := range tags {
		if !htmlMetaNamePattern.MatchString(tag) {
			continue
		}
		matches := htmlMetaContentAttrPattern.FindStringSubmatch(tag)
		if len(matches) >= 4 {
			content := matches[2]
			if content == "" {
				content = matches[3]
			}
			return normalizeWhitespace(html.UnescapeString(content))
		}
	}
	return ""
}

func extractHTMLHeadings(text string) []string {
	matches := htmlHeadingPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	headings := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		heading := normalizeWhitespace(stripHTMLTags(match[2]))
		if heading == "" {
			continue
		}
		headings = append(headings, heading)
	}
	return headings
}

func stripHTMLTags(text string) string {
	text = htmlTagPattern.ReplaceAllString(text, " ")
	return html.UnescapeString(text)
}

func looksLikeLogOutput(lines []string) bool {
	if len(lines) >= 40 {
		return true
	}
	if len(lines) < toolOutputLogHeadLines {
		return false
	}
	hits := 0
	for _, line := range lines {
		if logSignalPattern.MatchString(line) {
			hits++
		}
	}
	return hits*4 >= len(lines)
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}

func normalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func clampText(text string, maxChars int) string {
	text = normalizeWhitespace(text)
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	return strings.TrimSpace(text[:maxChars]) + " ..."
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
