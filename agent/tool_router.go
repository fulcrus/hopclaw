package agent

import "strings"

// ---------------------------------------------------------------------------
// System Prompt Generation
// ---------------------------------------------------------------------------

// BuildToolGuidancePrompt generates tool usage guidelines based on user intent.
func BuildToolGuidancePrompt(intent ToolIntent, tools []ToolDefinition) string {
	var sb strings.Builder

	if intent.RequiresCurrentInfo {
		sb.WriteString("## Current Information Rule\n\n")
		sb.WriteString("This request likely depends on current, recent, or time-sensitive facts. ")
		if hasCurrentDataTools(tools) {
			sb.WriteString("You MUST verify those facts with the current-data tools already available in this turn before answering. ")
			if toolNamesContain(tools, "news.digest") {
				sb.WriteString("Prefer `news.digest` for today's news, hot-topic summaries, and source-aware news tables. ")
			}
			if toolNamesContain(tools, "search.news") {
				sb.WriteString("Use `search.news` for recent news results when you need article discovery without a full digest. ")
			}
			if toolNamesContain(tools, "search.web") {
				sb.WriteString("Use `search.web` first for other current web facts such as weather, versions, schedules, regulations, and product updates. ")
			}
			if toolNamesContain(tools, "net.fetch") {
				sb.WriteString("Use `net.fetch` only when you already have a trustworthy public URL from the user or from an earlier discovery step. ")
			}
		} else {
			sb.WriteString("No dedicated current-data tool is available in this turn. ")
			sb.WriteString("If you answer anyway, explicitly say the result may be outdated, do not imply live verification, and anchor time references with exact calendar dates instead of vague terms like 'today' or 'latest'. ")
		}
		sb.WriteString("\n\n")
	}

	// If user explicitly requested commands, respect that
	if len(intent.ExplicitCommands) > 0 {
		sb.WriteString("## Tool Selection\n\n")
		sb.WriteString("The user explicitly requested using: ")
		sb.WriteString(strings.Join(intent.ExplicitCommands, ", "))
		sb.WriteString(". Respect their choice and use the specified tool(s).\n\n")
		return sb.String()
	}

	// Knowledge question — minimize tool usage
	if intent.MessageType == MessageTypeKnowledge {
		sb.WriteString("## Response Guidelines\n\n")
		sb.WriteString("This appears to be a knowledge question. ")
		sb.WriteString("For stable, well-established facts, answer directly from your knowledge. ")
		sb.WriteString("Use tools when the user asks for current information, recent events, or specific facts that benefit from verification.\n\n")
		return sb.String()
	}

	// Action/hybrid — provide tool preference guidance
	sb.WriteString("## Tool Usage Guidelines\n\n")
	sb.WriteString("**Prefer built-in tools over shell commands when possible:**\n\n")
	sb.WriteString("| Instead of | Use |\n")
	sb.WriteString("|------------|-----|\n")
	sb.WriteString("| `openssl rand` | `crypto.random` |\n")
	sb.WriteString("| `openssl dgst`, `shasum`, `md5` | `crypto.hash` |\n")
	sb.WriteString("| `curl` | `net.fetch` |\n")
	sb.WriteString("| `wget` | `net.download` |\n")
	sb.WriteString("| `base64` | `text.base64` |\n")
	sb.WriteString("| `jq` | `text.json` |\n\n")

	if toolNamesContain(tools, "skill.ensure") {
		sb.WriteString("**Treat `skill.ensure` as internal capability recovery, not as a user-facing workflow.** ")
		sb.WriteString("If the user wants a concrete result and the current tool list truly lacks the needed specialized capability ")
		sb.WriteString("(for example RSS, weather, translation, QR code, or currency), call `skill.ensure` once early, then continue with the recovered tool. ")
		sb.WriteString("Do not explain skills unless recovery fails and the missing capability matters to the final answer. ")
		sb.WriteString("Check the tools already available in this turn first. ")
		sb.WriteString("Do NOT install packages via pip/npm or write scripts from scratch.\n\n")
	}

	sb.WriteString("**Anti-loop policy:** before repeating a tool or command, check whether the last attempt changed the state or produced new evidence. ")
	sb.WriteString("If two similar attempts return the same result, error, or empty outcome, stop retrying the same path. ")
	sb.WriteString("Either choose a different high-confidence action, ask for missing information, or deliver the best verifiable answer you can from the evidence already gathered.\n\n")

	return sb.String()
}

func toolGuidanceIntentForRun(run *Run, message string) ToolIntent {
	intent := DetectFast(message)
	if runRequiresCurrentInfo(run) {
		intent.RequiresCurrentInfo = true
	}
	return intent
}

func runRequiresCurrentInfo(run *Run) bool {
	if run == nil {
		return false
	}
	if run.Triage != nil && run.Triage.RequiresCurrentInfo {
		return true
	}
	domains := make([]string, 0, 8)
	if run.Preflight != nil {
		domains = append(domains, run.Preflight.DetectedDomains...)
		domains = append(domains, run.Preflight.SuggestedDomains...)
	}
	if run.TaskContract != nil {
		domains = append(domains, run.TaskContract.SuggestedDomains...)
	}
	if run.Triage != nil {
		domains = append(domains, run.Triage.SuggestedDomains...)
	}
	return semanticDomainsRequireCurrentInfo(domains)
}

func semanticDomainsRequireCurrentInfo(domains []string) bool {
	normalized := normalizeSemanticDomains(domains)
	return hasSemanticDomain(normalized, DomainNews, DomainWatch)
}

func hasCurrentDataTools(tools []ToolDefinition) bool {
	for _, name := range []string{"search.web", "search.news", "news.digest", "net.fetch"} {
		if toolNamesContain(tools, name) {
			return true
		}
	}
	return false
}

func toolNamesContain(tools []ToolDefinition, want string) bool {
	want = strings.TrimSpace(want)
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == want {
			return true
		}
	}
	return false
}
