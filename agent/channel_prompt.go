package agent

import (
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/skill"
)

type channelPromptBehavior struct {
	Interactive    bool
	Threading      bool
	Mobile         bool
	InlineDelivery bool
}

// ---------------------------------------------------------------------------
// Channel-aware system prompt
// ---------------------------------------------------------------------------

func renderChannelBehaviorPrompt(session *Session) string {
	behavior := channelPromptBehaviorFromSession(session)
	if !behavior.Interactive || !behavior.InlineDelivery {
		return ""
	}

	lines := []string{"<channel_rules>"}
	if behavior.Mobile {
		lines = append(lines, "You are responding inside a mobile-first interactive chat channel. The user is likely reading your reply in a messaging app on a phone.")
	} else {
		lines = append(lines, "You are responding inside an interactive chat channel. The user reads your reply in a messaging app.")
	}

	lines = append(lines,
		"",
		"Delivery rule:",
		"- Your chat message IS the delivery mechanism. When the user asks for information, analysis, tables, summaries, or reports, put the result directly in your reply message.",
	)
	if behavior.Mobile {
		lines = append(lines, "- Do NOT save the answer to a file and tell the user to look at it somewhere else. They may only have the chat client open right now.")
	} else {
		lines = append(lines, "- Prefer delivering informational results directly in the reply instead of redirecting the user to a generated file, unless they explicitly asked for a file artifact.")
	}
	lines = append(lines, "- Writing project files (source code, configs, scripts) IS allowed and expected when the user asks you to build or modify their codebase. The distinction: answers go in the message, code goes in files.")

	if behavior.Threading {
		lines = append(lines,
			"",
			"Threading rule:",
			"- Keep the response scoped to the current thread or topic when the channel provides thread context.",
		)
	}

	lines = append(lines,
		"",
		"Response style:",
		"- Be concise by default, but match response depth to task complexity. For complex analysis, retain the key reasoning, caveats, limitations, and next-step recommendations instead of compressing the answer too aggressively.",
		"- Do NOT include work-process recaps, step-by-step logs, emoji checklists, or data source sections unless explicitly asked.",
		"- Use simple markdown. Avoid decorative formatting, excessive headers, and dividers.",
		"</channel_rules>",
	)
	return strings.Join(lines, "\n")
}

func effectiveSystemPrompt(base string, run *Run, session *Session) string {
	return effectiveSystemPromptWithRuntimeContext(base, run, session, skill.RuntimeContext{})
}

func effectiveSystemPromptWithRuntimeContext(base string, run *Run, session *Session, runtimeCtx skill.RuntimeContext) string {
	parts := make([]string, 0, 4)
	if trimmed := strings.TrimSpace(base); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if run != nil && run.EffectiveProfile != nil {
		if trimmed := strings.TrimSpace(run.EffectiveProfile.SystemPrompt); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if prompt := renderProjectRulePrompt(loadProjectRuleBundles(runtimeCtx)); prompt != "" {
		parts = append(parts, prompt)
	}
	if prompt := renderChannelBehaviorPrompt(session); prompt != "" {
		parts = append(parts, prompt)
	}
	return strings.Join(parts, "\n\n")
}

func channelPromptBehaviorFromSession(session *Session) channelPromptBehavior {
	if session == nil {
		return channelPromptBehavior{}
	}
	return channelPromptBehaviorFromMetadata(session.Metadata)
}

func channelPromptBehaviorFromMetadata(metadataMap map[string]any) channelPromptBehavior {
	descriptor, ok := normalizedChannelCapabilityDescriptor(metadataMap)
	if !ok {
		return channelPromptBehavior{}
	}
	return channelPromptBehavior{
		Interactive:    descriptorBool(descriptor, "interactive"),
		Threading:      descriptorBool(descriptor, "threading"),
		Mobile:         descriptorBool(descriptor, "mobile"),
		InlineDelivery: descriptorBool(descriptor, "inline_delivery"),
	}
}

func metadataBool(metadataMap map[string]any, key string) bool {
	if len(metadataMap) == 0 {
		return false
	}
	value, ok := metadataMap[key]
	if !ok {
		return false
	}
	return anyBool(value)
}

func descriptorBool(raw any, key string) bool {
	switch current := raw.(type) {
	case map[string]any:
		return metadataBool(current, key)
	case map[string]bool:
		return current[key]
	default:
		return false
	}
}

func anyBool(value any) bool {
	switch current := value.(type) {
	case bool:
		return current
	case string:
		switch strings.ToLower(strings.TrimSpace(current)) {
		case "1", "true", "yes":
			return true
		}
	case fmt.Stringer:
		switch strings.ToLower(strings.TrimSpace(current.String())) {
		case "1", "true", "yes":
			return true
		}
	}
	return false
}
