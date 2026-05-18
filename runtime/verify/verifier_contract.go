package verify

import (
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type contractVerifier struct{}

func (contractVerifier) Name() string             { return "task.contract" }
func (contractVerifier) Applies(input Input) bool { return input.Contract != nil }

func (contractVerifier) Verify(input Input) Check {
	check := Check{Name: "task.contract", Domain: "contract", Requirement: RequirementRequired}
	if input.Contract == nil {
		check.Status = StatusSkipped
		check.Summary = "task contract verification skipped because no contract was recorded"
		return check
	}
	if !runCompleted(input.Status) {
		check.Status = StatusSkipped
		check.Summary = "task contract verification skipped because the run did not complete"
		return check
	}

	requiredMissingInfo := collectRequiredMissingInfo(input.Contract)
	if len(requiredMissingInfo) > 0 {
		check.Status = StatusFailed
		check.Summary = "run completed while required task inputs were still unresolved"
		for _, item := range requiredMissingInfo {
			check.Issues = append(check.Issues, Issue{
				Code:     IssueCodeContractMissingInfoUnresolved,
				Severity: SeverityError,
				Message:  item,
			})
		}
		return check
	}

	missingDeliverables := collectMissingRequiredDeliverables(input)
	missingAcceptance := collectMissingAcceptanceCriteria(input)
	missingExternalEffect := input.Contract.RequiresExternalEffect && !hasExternalEffectEvidence(input)

	switch {
	case missingExternalEffect:
		check.Status = StatusFailed
		check.Summary = "run completed without clear evidence for the required external side effect"
		check.Issues = []Issue{{
			Code:     IssueCodeContractExternalEffectMissing,
			Severity: SeverityError,
			Message:  "expected delivery, notification, or deployment evidence before reporting success",
		}}
	case len(missingDeliverables) > 0 || len(missingAcceptance) > 0:
		check.Status = StatusWarning
		check.Summary = "run completed, but parts of the persisted task contract were not clearly evidenced"
		for _, item := range missingDeliverables {
			check.Issues = append(check.Issues, Issue{
				Code:     IssueCodeContractDeliverableMissing,
				Severity: SeverityWarning,
				Message:  item,
			})
		}
		for _, item := range missingAcceptance {
			check.Issues = append(check.Issues, Issue{
				Code:     IssueCodeContractAcceptanceMissing,
				Severity: SeverityWarning,
				Message:  item,
			})
		}
	default:
		check.Status = StatusPassed
		check.Summary = "run satisfied the persisted task contract"
		check.Evidence = collectContractEvidence(input)
	}
	return check
}

func collectRequiredMissingInfo(contract *Contract) []string {
	if contract == nil || len(contract.MissingInfo) == 0 {
		return nil
	}
	out := make([]string, 0, len(contract.MissingInfo))
	for _, item := range contract.MissingInfo {
		if !item.Required {
			continue
		}
		summary := strings.TrimSpace(item.Summary)
		if summary == "" {
			summary = firstNonEmptyTrimmed(strings.TrimSpace(item.ID), "required task input is unresolved")
		}
		out = append(out, summary)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectMissingRequiredDeliverables(input Input) []string {
	if input.Contract == nil {
		return nil
	}
	out := make([]string, 0, len(input.Contract.ExpectedDeliverables))
	for _, item := range input.Contract.ExpectedDeliverables {
		if !item.Required || taskContractDeliverableSatisfied(input, item.Kind) {
			continue
		}
		out = append(out, fmt.Sprintf("missing required deliverable evidence for %s", item.Kind))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectMissingAcceptanceCriteria(input Input) []string {
	if input.Contract == nil {
		return nil
	}
	out := make([]string, 0, len(input.Contract.AcceptanceCriteria))
	for _, item := range input.Contract.AcceptanceCriteria {
		if !item.Required {
			continue
		}
		if contractAcceptanceSatisfied(input, item) {
			continue
		}
		out = append(out, firstNonEmptyTrimmed(strings.TrimSpace(item.Summary), "required acceptance criteria not met"))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func taskContractDeliverableSatisfied(input Input, kind string) bool {
	kind = strings.TrimSpace(kind)
	switch kind {
	case "", contractDeliverableSummary:
		return strings.TrimSpace(input.Output) != "" || strings.TrimSpace(input.Summary) != ""
	case contractDeliverableDocument:
		return hasDocumentEvidence(input)
	case contractDeliverableSpreadsheet:
		return hasSpreadsheetEvidence(input)
	case contractDeliverablePresentation:
		return hasPresentationEvidence(input)
	case contractDeliverableBrowserEvidence:
		return len(collectBrowserEvidence(input)) > 0
	case contractDeliverableDesktopEvidence:
		return len(collectDesktopEvidence(input)) > 0
	case contractDeliverableMessageDelivery:
		return hasMessageDeliveryEvidence(input)
	case contractDeliverableWatchAlert:
		return hasWatchAlertEvidence(input)
	case contractDeliverableDeployment:
		return hasDeploymentEvidence(input)
	default:
		for _, item := range input.Deliverables {
			if strings.EqualFold(strings.TrimSpace(item.Kind), kind) {
				return true
			}
		}
	}
	return false
}

func contractAcceptanceSatisfied(input Input, acceptance ContractAcceptance) bool {
	switch strings.TrimSpace(acceptance.ID) {
	case contractAcceptanceVisibleResult:
		return hasUserVisibleResult(input)
	case contractAcceptanceDeliverables:
		for _, kind := range acceptance.DeliverableKinds {
			if !taskContractDeliverableSatisfied(input, kind) {
				return false
			}
		}
		return len(acceptance.DeliverableKinds) > 0
	case contractAcceptanceExternalEffect:
		return hasExternalEffectEvidence(input)
	case contractAcceptanceApproval:
		if !input.Contract.RequiresApproval {
			return true
		}
		return hasExternalEffectEvidence(input) || !runCompleted(input.Status)
	default:
		if len(acceptance.DeliverableKinds) == 0 {
			return true
		}
		for _, kind := range acceptance.DeliverableKinds {
			if !taskContractDeliverableSatisfied(input, kind) {
				return false
			}
		}
		return true
	}
}

func hasMessageDeliveryEvidence(input Input) bool {
	if input.HasToolPrefix("email.") || input.HasToolPrefix("channel.") {
		return true
	}
	for _, name := range input.ToolNames {
		trimmed := strings.TrimSpace(name)
		if strings.HasPrefix(trimmed, "slack.") ||
			strings.HasPrefix(trimmed, "discord.") ||
			strings.HasPrefix(trimmed, "telegram.") ||
			strings.HasPrefix(trimmed, "whatsapp.") ||
			strings.HasPrefix(trimmed, "wechat.") ||
			strings.HasPrefix(trimmed, "feishu.") ||
			strings.HasPrefix(trimmed, "lark.") {
			return true
		}
	}
	for _, raw := range input.ToolOutputs {
		payload := parseJSONObject(raw)
		if len(payload) == 0 {
			continue
		}
		if hasBool(payload, "success", true) || hasKey(payload, "message_id") || hasKey(payload, "delivery_id") {
			return true
		}
	}
	return false
}

func hasWatchAlertEvidence(input Input) bool {
	watchRelated := input.HasToolPrefix("watch.") || strings.HasPrefix(strings.TrimSpace(input.SessionKey), "watch:")
	for _, ref := range input.Deliverables {
		if strings.EqualFold(strings.TrimSpace(ref.Kind), contractDeliverableWatchAlert) {
			watchRelated = true
			break
		}
	}
	if !watchRelated {
		return false
	}
	if strings.TrimSpace(input.Output) != "" {
		return true
	}
	for _, ref := range input.Deliverables {
		if strings.EqualFold(strings.TrimSpace(ref.Kind), contractDeliverableWatchAlert) {
			return true
		}
	}
	return true
}

func hasDeploymentEvidence(input Input) bool {
	deploymentRelated := input.Contract != nil && strings.EqualFold(strings.TrimSpace(input.Contract.JobType), "deployment")
	for _, ref := range input.Deliverables {
		if strings.EqualFold(strings.TrimSpace(ref.Kind), contractDeliverableDeployment) {
			deploymentRelated = true
			return true
		}
		if strings.Contains(strings.ToLower(strings.TrimSpace(ref.URI)), "deploy") {
			deploymentRelated = true
			return true
		}
	}
	for _, raw := range input.ToolOutputs {
		payload := parseJSONObject(raw)
		if len(payload) == 0 {
			continue
		}
		if hasKey(payload, "deployment") || hasKey(payload, "service") || hasKey(payload, "endpoint") {
			return true
		}
		if deploymentRelated && hasKey(payload, "url") {
			return true
		}
	}
	return false
}

func hasExternalEffectEvidence(input Input) bool {
	return hasMessageDeliveryEvidence(input) ||
		hasWatchAlertEvidence(input) ||
		hasDeploymentEvidence(input) ||
		hasBrowserExternalEffectEvidence(input)
}

func hasBrowserExternalEffectEvidence(input Input) bool {
	if input.Contract == nil || !input.Contract.RequiresExternalEffect {
		return false
	}
	if !browserTaskPresent(input) {
		return false
	}
	if browserHasResultArtifactEvidence(input) {
		return true
	}
	if browserHasStructuredResultEvidence(input) {
		return true
	}
	return browserHasDistinctNavigationEvidence(input)
}

func browserTaskPresent(input Input) bool {
	if input.HasToolPrefix("browser.") {
		return true
	}
	for _, ref := range input.Deliverables {
		if strings.HasPrefix(strings.TrimSpace(ref.ToolName), "browser.") {
			return true
		}
	}
	for _, item := range input.Contract.ExpectedDeliverables {
		if strings.EqualFold(strings.TrimSpace(item.Kind), contractDeliverableBrowserEvidence) {
			return true
		}
	}
	return false
}

func browserHasResultArtifactEvidence(input Input) bool {
	for _, ref := range input.Deliverables {
		if deliverableMatches(ref, ".png", ".jpg", ".jpeg", ".webp", ".html", ".json") &&
			(strings.HasPrefix(strings.TrimSpace(ref.ToolName), "browser.") || strings.EqualFold(strings.TrimSpace(ref.Kind), contractDeliverableBrowserEvidence)) {
			return true
		}
	}
	return false
}

func browserHasStructuredResultEvidence(input Input) bool {
	actionEvidence := false
	pageEvidence := false
	for _, raw := range input.ToolOutputs {
		payload := parseJSONObject(raw)
		if len(payload) == 0 {
			continue
		}
		if hasAnyKey(payload, "selector", "values", "clicked", "visible", "found") {
			actionEvidence = true
		}
		if hasAnyKey(payload, "html", "content", "artifact_uri", "artifact_ref", "entries", "messages", "requests", "tree", "text") {
			pageEvidence = true
		}
		if hasAnyKey(payload, "result", "response", "response_body", "form", "fields", "values", "items", "entries") && hasAnyKey(payload, "url", "title", "content", "html") {
			return true
		}
	}
	return actionEvidence && pageEvidence
}

func browserHasDistinctNavigationEvidence(input Input) bool {
	urls := make(map[string]struct{}, 2)
	for _, raw := range input.ToolOutputs {
		payload := parseJSONObject(raw)
		if len(payload) == 0 {
			continue
		}
		url := strings.TrimSpace(normalize.String(payload["url"]))
		if url == "" {
			continue
		}
		urls[url] = struct{}{}
		if len(urls) >= 2 {
			return true
		}
	}
	return false
}

func collectContractEvidence(input Input) []string {
	out := make([]string, 0, 4)
	if summary := strings.TrimSpace(input.Summary); summary != "" {
		out = append(out, summary)
	}
	if output := strings.TrimSpace(input.Output); output != "" && output != strings.TrimSpace(input.Summary) {
		out = append(out, output)
	}
	for _, item := range input.Deliverables {
		label := firstNonEmptyTrimmed(strings.TrimSpace(item.Kind), strings.TrimSpace(item.ToolName), strings.TrimSpace(item.URI))
		if label != "" {
			out = append(out, label)
		}
		if len(out) >= 4 {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
