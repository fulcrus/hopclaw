package agent

import (
	"encoding/json"
	"strings"

	"github.com/fulcrus/hopclaw/contextengine"
)

type toolProgressTracker struct {
	lastBatchSignature string
	stagnantCount      int
}

func (t *toolProgressTracker) Observe(calls []ToolCall, results []contextengine.ToolResult) int {
	signature := toolBatchProgressSignature(calls, results)
	if signature == "" {
		t.lastBatchSignature = ""
		t.stagnantCount = 0
		return 0
	}
	if signature == t.lastBatchSignature {
		t.stagnantCount++
		return t.stagnantCount
	}
	t.lastBatchSignature = signature
	t.stagnantCount = 0
	return 0
}

func toolBatchProgressSignature(calls []ToolCall, results []contextengine.ToolResult) string {
	callSig := toolNameSignature(calls)
	resultSig := toolResultSignature(results)
	if callSig == "" && resultSig == "" {
		return ""
	}
	return callSig + "|" + resultSig
}

func toolNameSignature(calls []ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	parts := make([]string, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(strings.ToLower(call.Name))
		if name == "" {
			name = "tool"
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, ";")
}

func toolResultSignature(results []contextengine.ToolResult) string {
	if len(results) == 0 {
		return ""
	}
	parts := make([]string, 0, len(results))
	for _, result := range results {
		content := compactProgressText(result.Content)
		if result.ArtifactURI != "" && !toolArtifactURIIgnoredForProgress(result.ToolName) {
			if content != "" {
				content += "|"
			}
			content += strings.TrimSpace(strings.ToLower(result.ArtifactURI))
		}
		if content == "" {
			content = "{}"
		}
		parts = append(parts, strings.TrimSpace(strings.ToLower(result.ToolName))+":"+content)
	}
	return strings.Join(parts, ";")
}

func toolArtifactURIIgnoredForProgress(toolName string) bool {
	switch strings.TrimSpace(strings.ToLower(toolName)) {
	case "browser.snapshot", "browser.snapshot_aria":
		return true
	default:
		return false
	}
}

func compactProgressText(text string) string {
	text = strings.TrimSpace(strings.ToLower(text))
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if likelyJSON(text) {
		var payload any
		if err := json.Unmarshal([]byte(text), &payload); err == nil {
			if encoded, err := json.Marshal(payload); err == nil {
				text = string(encoded)
			}
		}
	}
	const maxLen = 160
	if len(text) > maxLen {
		text = text[:maxLen]
	}
	return text
}

func likelyJSON(text string) bool {
	if len(text) < 2 {
		return false
	}
	return (strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}")) ||
		(strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]"))
}
