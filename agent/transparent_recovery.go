package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	planpkg "github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/resultmodel"
)

const (
	transparentRecoveryMetadataKey       = "transparent_capability_recovery"
	transparentRecoveryIntentMetadataKey = "transparent_recovery_intent"
)

type transparentRecoveryIntent struct {
	Key           string
	Query         string
	Goal          string
	RequiredTools []string
}

func (a *AgentComponent) maybeAttemptTransparentCapabilityRecovery(
	ctx context.Context,
	run *Run,
	session *Session,
	prompt string,
	available []ToolDefinition,
) (bool, error) {
	return a.maybeAttemptTransparentCapabilityRecoveryIntent(ctx, run, session, inferTransparentRecoveryIntentForRun(run, nil, prompt, available))
}

func (a *AgentComponent) maybeAttemptTransparentCapabilityRecoveryIntent(
	ctx context.Context,
	run *Run,
	session *Session,
	intent *transparentRecoveryIntent,
) (bool, error) {
	if a == nil || a.tools == nil || a.context == nil || run == nil || session == nil {
		return false, nil
	}
	if intent == nil {
		return false, nil
	}
	if hasTransparentRecoveryAttempt(session, run.ID, intent.Key) {
		return false, nil
	}

	call := ToolCall{
		ID:   "auto-recover-" + intent.Key,
		Name: "skill.ensure",
		Input: map[string]any{
			"query": intent.Query,
			"goal":  intent.Goal,
			"limit": 3,
		},
	}
	if len(intent.RequiredTools) > 0 {
		call.Input["required_tools"] = append([]string(nil), intent.RequiredTools...)
	}

	results, err := a.tools.ExecuteBatch(ctx, run, session, []ToolCall{call})
	if err != nil {
		results = buildToolExecutionFailureResults([]ToolCall{call}, err, 1, 0)
	}
	if len(results) == 0 {
		results = []contextengine.ToolResult{{
			ToolName:       call.Name,
			ToolCallID:     call.ID,
			Status:         resultmodel.ToolResultError,
			TranscriptText: "automatic capability recovery produced no result",
			Summary:        "capability recovery failed",
			Error: &resultmodel.ResultError{
				Code:    "transparent_recovery_empty",
				Message: "automatic capability recovery produced no result",
			},
		}}
	}

	appendAssistantToolCallsMessage(run, session, &ModelResponse{
		ToolCalls: []ToolCall{call},
	})
	tagTransparentRecoveryResults(results, intent.Key)
	results = toolResultsForRun(results, run.ID)
	if err := a.context.AppendToolResults(ctx, &session.Session, results); err != nil {
		return false, err
	}
	session.UpdatedAt = time.Now().UTC()
	return true, nil
}

func inferTransparentRecoveryIntentWithDomains(prompt string, domains []string, available []ToolDefinition) *transparentRecoveryIntent {
	return inferTransparentRecoveryIntentWithHints(prompt, structuredRecoveryCapabilityHints(domains, nil), available)
}

func inferTransparentRecoveryIntentForRun(run *Run, task *planpkg.Task, fallbackGoal string, available []ToolDefinition) *transparentRecoveryIntent {
	goal := strings.TrimSpace(fallbackGoal)
	switch {
	case task != nil:
		goal = strings.TrimSpace(task.Goal)
	case run != nil && run.TaskContract != nil:
		if contractGoal := strings.TrimSpace(run.TaskContract.Goal); contractGoal != "" {
			goal = contractGoal
		}
	}
	if intent := inferTransparentRecoveryIntentWithHints(goal, transparentRecoveryCapabilityHintsForRun(run, task), available); intent != nil {
		return intent
	}
	if !canAssessMissingDomainRecovery(available) {
		return nil
	}
	return inferTransparentRecoveryIntentForMissingDomains(goal, missingActivatedDomainsForTurn(run, goal, available))
}

func inferTransparentRecoveryIntentWithHints(goal string, hints []string, available []ToolDefinition) *transparentRecoveryIntent {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return nil
	}
	if !toolDefinitionsContain(available, "skill.ensure") {
		return nil
	}
	if skillEnsureRequiresApproval(available) {
		return nil
	}
	for _, hint := range normalizeCapabilityHints(hints) {
		switch normalizeCapabilityHint(hint) {
		case "search.news", "news.digest":
			if hasCurrentNewsServiceTools(available) {
				continue
			}
			if hasToolNamePrefix(toolDefinitionNames(available), "rss.") || toolDefinitionsContain(available, "news.fetch") {
				continue
			}
			return &transparentRecoveryIntent{
				Key:           "rss",
				Query:         "rss atom feed reader fetch and summarize latest news updates",
				Goal:          goal,
				RequiredTools: []string{"rss.fetch"},
			}
		case "search.web":
			continue
		case "translate.run":
			if hasToolNamePrefix(toolDefinitionNames(available), "translate.") {
				continue
			}
			return &transparentRecoveryIntent{
				Key:           "translate",
				Query:         "translation language translate text between languages",
				Goal:          goal,
				RequiredTools: []string{"translate.run"},
			}
		case "calculator.eval":
			if hasToolNamePrefix(toolDefinitionNames(available), "calculator.") {
				continue
			}
			return &transparentRecoveryIntent{
				Key:           "calculator",
				Query:         "calculator evaluate math conversion statistics",
				Goal:          goal,
				RequiredTools: []string{"calculator.eval"},
			}
		case "rss.fetch":
			if hasToolNamePrefix(toolDefinitionNames(available), "rss.") || toolDefinitionsContain(available, "news.fetch") {
				continue
			}
			return &transparentRecoveryIntent{
				Key:           "rss",
				Query:         "rss atom feed reader fetch and summarize updates",
				Goal:          goal,
				RequiredTools: []string{"rss.fetch"},
			}
		case "email.send", "email.search":
			if hasToolNamePrefix(toolDefinitionNames(available), "email.") {
				continue
			}
			intent := &transparentRecoveryIntent{
				Key:  "email",
				Goal: goal,
			}
			if normalizeCapabilityHint(hint) == "email.send" {
				intent.Query = "email outbound delivery compose and send messages attachments"
				intent.RequiredTools = []string{"email.send"}
			} else {
				intent.Query = "email inbox search read recent messages attachments"
				intent.RequiredTools = []string{"email.search"}
			}
			return intent
		}
	}
	return nil
}

func inferTransparentRecoveryIntentForMissingDomains(goal string, missingDomains []string) *transparentRecoveryIntent {
	goal = strings.TrimSpace(goal)
	if goal == "" || len(missingDomains) == 0 {
		return nil
	}
	for _, item := range normalizeSemanticDomains(missingDomains) {
		domain := ToolDomain(item)
		query := transparentRecoveryQueryForMissingDomain(domain)
		if query == "" {
			continue
		}
		return &transparentRecoveryIntent{
			Key:   "missing-domain-" + item,
			Query: query,
			Goal:  goal,
		}
	}
	return nil
}

func canAssessMissingDomainRecovery(available []ToolDefinition) bool {
	for _, tool := range available {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		if name == "skill.ensure" || name == "skill.install" {
			continue
		}
		return true
	}
	return false
}

func transparentRecoveryQueryForMissingDomain(domain ToolDomain) string {
	switch domain {
	case DomainBrowser:
		return "browser automation webpage interaction open click snapshot"
	case DomainDesktop:
		return "desktop automation windows clipboard screenshot app control"
	case DomainCalendar:
		return "calendar scheduling caldav ics events create list update"
	case DomainPDF:
		return "pdf parse extract merge create annotate documents"
	case DomainSheet:
		return "spreadsheet excel csv xlsx read write formulas tables"
	case DomainDocument:
		return "document docx writing generation editing templates"
	case DomainPresentation:
		return "presentation pptx slide deck create edit export"
	case DomainMedia:
		return "media image audio video processing conversion editing"
	case DomainVision:
		return "vision ocr image understanding screenshot extraction"
	case DomainSpeech:
		return "speech transcription text to speech audio voice"
	case DomainCanvas:
		return "canvas ui rendering dom interaction screenshot pdf"
	case DomainArchive:
		return "archive zip unzip tar compress extract files"
	case DomainChannel:
		return "messaging channel slack discord telegram webhook send"
	case DomainCron:
		return "cron schedule recurring automation timers"
	case DomainWatch:
		return "watch monitoring alerts polling triggers subscriptions"
	case DomainAgent:
		return "agent delegation sub-agent orchestration planning"
	case DomainNodes:
		return "nodes device desktop machine control automation"
	case DomainGateway:
		return "gateway runtime integration inspection health"
	default:
		return ""
	}
}

func transparentRecoveryCapabilityHintsForRun(run *Run, task *planpkg.Task) []string {
	values := make([]string, 0, 8)
	if task != nil {
		values = append(values, task.RequiredCapabilities...)
		if task.Kind == planpkg.TaskTranslate {
			values = append(values, "translate.run")
		}
	}
	if run != nil && run.TaskContract != nil {
		values = append(values, run.TaskContract.CapabilityHints...)
		values = append(values, inferTaskContractCapabilityHints(
			run.TaskContract.JobType,
			run.TaskContract.SuggestedDomains,
			run.TaskContract.ExpectedDeliverables,
		)...)
	}
	values = append(values, structuredRecoveryCapabilityHints(harnessStructuredDomains(run), runTaskContract(run))...)
	return normalizeCapabilityHints(values)
}

func structuredRecoveryCapabilityHints(domains []string, contract *TaskContract) []string {
	hints := make([]string, 0, 2)
	if hasSemanticDomain(domains, DomainNews) {
		if contract != nil && (strings.TrimSpace(contract.JobType) == taskContractJobReport ||
			taskContractDeliverablesContain(contract.ExpectedDeliverables, taskDeliverableSpreadsheet, taskDeliverableDocument, taskDeliverablePresentation)) {
			hints = append(hints, "news.digest")
		} else {
			hints = append(hints, "search.news")
		}
	}
	if hasSemanticDomain(domains, DomainEmail) {
		if contract != nil && (strings.TrimSpace(contract.JobType) == taskContractJobDelivery ||
			taskContractDeliverablesContain(contract.ExpectedDeliverables, taskDeliverableMessageDelivery)) {
			hints = append(hints, "email.send")
		} else {
			hints = append(hints, "email.search")
		}
	}
	return normalizeCapabilityHints(hints)
}

func (a *AgentComponent) transparentRecoveryToolDefinitions(session *Session, run *Run) []ToolDefinition {
	if a == nil || a.tools == nil {
		return nil
	}
	provider, ok := a.tools.(ToolDefinitionProvider)
	if !ok {
		return nil
	}
	return filterToolDefinitionsForRun(provider.ToolDefinitions(session), run)
}

func hasTransparentRecoveryAttempt(session *Session, runID, intentKey string) bool {
	if session == nil || strings.TrimSpace(runID) == "" || strings.TrimSpace(intentKey) == "" {
		return false
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role != contextengine.RoleTool || !messageMatchesRunID(msg.Metadata, runID) {
			continue
		}
		if strings.TrimSpace(msg.Name) != "skill.ensure" {
			continue
		}
		if value, ok := msg.Metadata[transparentRecoveryIntentMetadataKey]; ok && strings.TrimSpace(fmt.Sprint(value)) == intentKey {
			return true
		}
		if result, ok := resultmodel.DecodeToolResultMetadata(msg.Metadata); ok {
			if value, ok := result.Metadata[transparentRecoveryIntentMetadataKey]; ok && strings.TrimSpace(fmt.Sprint(value)) == intentKey {
				return true
			}
		}
	}
	return false
}

func tagTransparentRecoveryResults(results []contextengine.ToolResult, intentKey string) {
	for i := range results {
		metadata := cloneMap(results[i].Metadata)
		if metadata == nil {
			metadata = make(map[string]any, 2)
		}
		metadata[transparentRecoveryMetadataKey] = true
		metadata[transparentRecoveryIntentMetadataKey] = intentKey
		results[i].Metadata = metadata
	}
}

func skillEnsureRequiresApproval(available []ToolDefinition) bool {
	for _, tool := range available {
		if strings.TrimSpace(tool.Name) != "skill.ensure" {
			continue
		}
		return tool.RequiresApproval
	}
	return false
}

func hasCurrentNewsServiceTools(available []ToolDefinition) bool {
	return toolDefinitionsContain(available, "search.news") || toolDefinitionsContain(available, "news.digest")
}

func toolDefinitionsContain(tools []ToolDefinition, want string) bool {
	for _, tool := range tools {
		if strings.EqualFold(strings.TrimSpace(tool.Name), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func toolDefinitionNames(tools []ToolDefinition) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}
