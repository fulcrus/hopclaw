package semanticschema

import (
	"fmt"
	"strconv"
	"strings"
)

var (
	suggestedDomains = []string{
		"browser", "desktop", "search", "web", "news",
		"spreadsheet", "document", "presentation",
		"email", "channel", "watch", "calendar",
		"pdf", "canvas", "media", "vision", "nodes", "speech",
		"gateway", "agent",
		"net", "fs", "proc", "db", "cron", "crypto", "text", "archive", "git",
	}
	detectedDomainsTier3 = []string{
		"browser", "desktop", "email", "calendar", "pdf", "spreadsheet",
		"document", "presentation", "media", "vision", "speech", "canvas",
		"archive", "channel", "cron", "watch", "agent", "nodes", "gateway", "proc",
	}
	taskContractJobTypes = []string{
		"general", "report", "research", "monitor", "delivery", "deployment", "development", "automation",
	}
	taskContractCapabilityHints = []string{
		"search.news", "news.digest", "search.web", "email.search", "email.send", "translate.run", "calculator.eval",
	}
	taskContractDeliverableKinds = []string{
		"summary", "document", "spreadsheet", "presentation",
		"browser_evidence", "desktop_evidence", "message_delivery", "watch_alert", "deployment",
	}
	taskContractMissingInfoIDs = []string{
		"source_target", "delivery_target", "schedule", "deployment_target",
	}
	alwaysAvailableDomains = []string{
		"fs", "exec", "text", "net", "git", "db", "search", "web", "news", "env", "skill", "memory",
	}
)

func SuggestedDomains() []string {
	return cloneStrings(suggestedDomains)
}

func DetectedDomainsTier3() []string {
	return cloneStrings(detectedDomainsTier3)
}

func TaskContractJobTypes() []string {
	return cloneStrings(taskContractJobTypes)
}

func TaskContractCapabilityHints() []string {
	return cloneStrings(taskContractCapabilityHints)
}

func TaskContractDeliverableKinds() []string {
	return cloneStrings(taskContractDeliverableKinds)
}

func TaskContractMissingInfoIDs() []string {
	return cloneStrings(taskContractMissingInfoIDs)
}

func AlwaysAvailableDomains() []string {
	return cloneStrings(alwaysAvailableDomains)
}

func IsTaskContractJobType(value string) bool {
	return containsNormalized(taskContractJobTypes, value)
}

func IsTaskContractDeliverableKind(value string) bool {
	return containsNormalized(taskContractDeliverableKinds, value)
}

func IsTaskContractMissingInfoID(value string) bool {
	return containsNormalized(taskContractMissingInfoIDs, value)
}

func BuildRunTriagePrompt() string {
	return fmt.Sprintf(`You are HopClaw's internal run triage engine. Analyze the latest user request and return JSON only.

Output format:
{"execution_mode":"direct|planned|watch|workflow","needs_reference":true|false,"needs_confirmation":true|false,"requires_current_info":true|false,"suggested_domains":%s,"reason":"...","confidence":0.0-1.0}

Rules:
- Work from semantics, not language-specific keywords.
- semantic_signal is the upstream shared semantic seed for this request. Use it as a hint source rather than re-inventing language-specific heuristics.
- language_hint and main_semantic_path are upstream hints. When main_semantic_path=true, rely on semantics rather than keyword resemblance.
- When semantic_signal.requires_current_info=true, treat it as an upstream freshness hint and preserve it unless the request is clearly stable.
- execution_mode:
  - direct: one focused step or simple answer/action
  - planned: multi-step work needing decomposition or verification
  - watch: recurring or trigger-based monitoring
  - workflow: long-running multi-system automation
- needs_reference=true only when the task depends on a concrete file, URL, screenshot, repo, mailbox, or similar target that was not provided.
- needs_confirmation=true only when the likely next step is destructive, externally visible, or high-risk.
- requires_current_info=true when the request depends on current, recent, or time-sensitive facts such as news, weather, prices, schedules, versions, or regulations.
- suggested_domains must use only the canonical semantic domains shown in the output format.
- suggested_domains should contain only materially relevant capability groups.
- The request may be in any language.
- Return JSON only.`, jsonArrayLiteral(suggestedDomains))
}

func BuildPreflightAnalyzerPrompt() string {
	return fmt.Sprintf(`You are HopClaw's internal preflight analyzer. Inspect the user request semantically and return JSON only.

Output format:
{"needs_reference":true|false,"needs_confirmation":true|false,"suggested_domains":%s,"detected_domains":%s,"browser_context_only":true|false,"reason":"...","confidence":0.0-1.0}

Rules:
- Work from semantics, not language-specific keywords.
- semantic_signal contains the shared upstream semantic signal. Reuse it as the primary structured hint when present instead of reinterpreting the request from scratch.
- Set needs_reference=true only when the task depends on a concrete file, URL, screenshot, repository, mailbox, or similar target that is not actually provided.
- Set needs_confirmation=true only when the likely next step is destructive, externally visible, or otherwise high risk.
- suggested_domains should contain only domains from the canonical semantic domain list shown in the output format.
- detected_domains should contain only tool domain names from the detected_domains list shown in the output format.
- Analyze the user message and return detected_domains as the Tier-3 tool domains the request truly needs for activation.
- Set browser_context_only=true when the task should stay on the existing browser/current-page context and must NOT be treated as monitoring or scheduled automation.
- Available Tier-3 detected_domains: %s.
- Tier 1-2 domains such as %s are always available. Do not include them in detected_domains.
- The request may be written in any language.
- Return JSON only.`, jsonArrayLiteral(suggestedDomains), jsonArrayLiteral(detectedDomainsTier3), strings.Join(detectedDomainsTier3, ", "), strings.Join(alwaysAvailableDomains, ", "))
}

func BuildTaskContractAnalyzerPrompt() string {
	return fmt.Sprintf(`You are HopClaw's internal task-contract analyzer. Inspect the request semantically and return JSON only.

Output format:
{"job_type":"%s","target_summary":"...","suggested_domains":%s,"capability_hints":%s,"deliverable_kinds":%s,"missing_info_ids":%s,"browser_context_only":true|false,"requires_external_effect":true|false,"requires_approval":true|false,"reason":"...","confidence":0.0-1.0}

Rules:
- Work from semantics, not language-specific keywords.
- The request may be written in any language.
- semantic_signal contains the shared upstream triage/preflight signal. Use it as the primary structured hint when present.
- Use execution_mode, preflight_state, and suggested_domains as hints, not hard constraints.
- job_type, deliverable_kinds, and missing_info_ids must use only the canonical values shown in the output format.
- Use capability_hints for non-core capabilities that the runtime may need to recover transparently. Prefer concrete tool families or tool names over vague prose.
- Set browser_context_only=true when the request should stay on the current browser/page context and must NOT be treated as watch/monitor automation.
- Keep requires_approval=true for delivery to other people/systems and deployments. Routine browser form submissions can require external effect while still keeping requires_approval=false because per-tool policy handles approval later.
- Set missing_info_ids only for mandatory unresolved inputs.
- Prefer conservative, reviewable classifications over creative guesses.
- Return JSON only.`, strings.Join(taskContractJobTypes, "|"), jsonArrayLiteral(suggestedDomains), jsonArrayLiteral(taskContractCapabilityHints), jsonArrayLiteral(taskContractDeliverableKinds), jsonArrayLiteral(taskContractMissingInfoIDs))
}

func BuildIngressRoutingPrompt(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		role = "ingress router"
	}
	return fmt.Sprintf("You are HopClaw's %s. A chat already has an active task or pending approval. Classify the latest user message and return ONLY compact JSON with keys intent, reason, confidence. Allowed intent values: reply_status, cancel_run, steer_current_run, enqueue_task, smalltalk, safe_queue. Prefer machine-friendly reason codes such as progress_check, cancel_request, scope_change, issue_report, follow_up_task, or chit_chat. Rules: use reply_status when the user is asking for progress, checking whether the task is still working, asking for status/results timing, or asking whether anything is happening. Use cancel_run only when the user clearly wants the current task stopped, cancelled, aborted, or dropped. Use steer_current_run when the user is changing the currently running task's instructions, scope, source, output format, or is reporting that the current result is broken, missing, invalid, stuck, or needs troubleshooting right now. Use enqueue_task when the user adds extra work that should happen after the current task, such as additional deliverables or follow-up tasks. Use smalltalk when the message is casual acknowledgement, gratitude, encouragement, or chit-chat that should not create work. Use safe_queue when uncertain. Approval replies are handled elsewhere; do not invent approval decisions. Never answer the user directly. Never call tools. Return JSON only.", role)
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	return append([]string(nil), items...)
}

func containsNormalized(items []string, value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	for _, item := range items {
		if strings.ToLower(strings.TrimSpace(item)) == value {
			return true
		}
	}
	return false
}

func jsonArrayLiteral(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		quoted = append(quoted, strconv.Quote(item))
	}
	return "[" + strings.Join(quoted, ",") + "]"
}
