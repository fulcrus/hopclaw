package agent

import (
	"fmt"
	"strings"
)

func normalizeToolCalls(calls []ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(calls))
	for i, call := range calls {
		call.Name = strings.TrimSpace(call.Name)
		if call.Name == "" {
			continue
		}
		call.ID = strings.TrimSpace(call.ID)
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%s_%d", sanitizeToolCallIDPart(call.Name), i+1)
		}
		if call.Input == nil {
			call.Input = make(map[string]any)
		}
		out = append(out, call)
	}
	return out
}

func sanitizeToolCallIDPart(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return "tool"
	}
	var b strings.Builder
	for _, ch := range name {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		case ch == '.', ch == '-', ch == '_':
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "tool"
	}
	return b.String()
}
