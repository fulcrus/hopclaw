package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/jsonrepair"
)

const DefaultExecutionModeSelectorTimeout = 2 * time.Second

type ModelExecutionModeSelector struct {
	model        ModelClient
	timeout      time.Duration
	defaultModel string
}

func NewModelExecutionModeSelector(model ModelClient, timeout time.Duration) *ModelExecutionModeSelector {
	if model == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultExecutionModeSelectorTimeout
	}
	return &ModelExecutionModeSelector{model: model, timeout: timeout}
}

func (s *ModelExecutionModeSelector) WithDefaultModel(model string) *ModelExecutionModeSelector {
	if s != nil {
		s.defaultModel = strings.TrimSpace(model)
	}
	return s
}

func (s *ModelExecutionModeSelector) Select(ctx context.Context, req ExecutionModeRequest) (ExecutionModeDecision, error) {
	if s == nil || s.model == nil {
		return ExecutionModeDecision{Mode: defaultExecutionMode(req.PlannerAvailable)}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	payload, err := json.Marshal(req)
	if err != nil {
		return ExecutionModeDecision{Mode: defaultExecutionMode(req.PlannerAvailable)}, err
	}
	modelName := strings.TrimSpace(req.Model)
	if s.defaultModel != "" {
		modelName = s.defaultModel
	}
	response, err := s.model.Chat(ctx, ChatRequest{
		Model:        modelName,
		SystemPrompt: executionModeSelectorSystemPrompt,
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: string(payload),
		}},
		Budget: contextengine.Budget{
			ContextWindow:  2048,
			MaxInputTokens: 1024,
			ReservedOutput: 160,
		},
	})
	if err != nil || response == nil {
		return ExecutionModeDecision{Mode: defaultExecutionMode(req.PlannerAvailable)}, err
	}
	return normalizeExecutionModeDecision(parseExecutionModeDecision(response.Message.Content), req.PlannerAvailable), nil
}

const executionModeSelectorSystemPrompt = `You are HopClaw's internal execution mode selector. Classify a user request into exactly one mode and return JSON only.

Output format: {"mode":"direct|planned|watch|workflow","confidence":0.0-1.0,"reason":"..."}

Mode definitions:
- "direct": one focused step, immediate answer, or a simple action that does not need multi-step orchestration.
- "planned": multi-step work that benefits from decomposition, intermediate outputs, verification, or multiple capabilities.
- "watch": recurring, scheduled, or trigger-based monitoring/checking that should continue over time.
- "workflow": long-running or multi-system automation with staged side effects, hand-offs, or delivery across systems.

Rules:
- Base the decision on semantics, not on any specific natural language keywords.
- The request may be written in any language.
- If planner_available is false, prefer "direct" unless a non-direct mode is still clearly necessary.
- Return JSON only.`

func (a *AgentComponent) selectExecutionMode(ctx context.Context, msg IncomingMessage, session *Session) ExecutionMode {
	defaultModel := strings.TrimSpace(msg.Model)
	plannerAvailable := false
	if a != nil {
		defaultModel = defaultString(msg.Model, a.config.DefaultModel)
		plannerAvailable = a.planner != nil
	}
	req := ExecutionModeRequest{
		Model:            defaultModel,
		Message:          strings.TrimSpace(msg.Content),
		PlannerAvailable: plannerAvailable,
	}
	if session != nil {
		req.SessionSummary = strings.TrimSpace(session.Summary)
	}
	if a != nil && a.modeSelector != nil {
		if decision, err := a.modeSelector.Select(ctx, req); err == nil {
			return normalizeExecutionModeDecision(decision, req.PlannerAvailable).Mode
		}
	}
	return defaultExecutionMode(req.PlannerAvailable)
}

func defaultExecutionMode(plannerAvailable bool) ExecutionMode {
	if plannerAvailable {
		return ExecutionModePlanned
	}
	return ExecutionModeDirect
}

func containsAny(s string, patterns ...string) bool {
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(s, pattern) {
			return true
		}
	}
	return false
}

func shouldUsePlanner(run *Run, plannerAvailable bool) bool {
	if !plannerAvailable || run == nil {
		return false
	}
	switch run.ExecutionMode {
	case "", ExecutionModePlanned, ExecutionModeWatch, ExecutionModeWorkflow:
		return true
	default:
		return false
	}
}

func parseExecutionModeDecision(raw string) ExecutionModeDecision {
	var decision ExecutionModeDecision
	if err := jsonrepair.DecodeJSONObjectCandidate(raw, &decision); err != nil {
		return ExecutionModeDecision{}
	}
	return decision
}

func normalizeExecutionModeDecision(decision ExecutionModeDecision, plannerAvailable bool) ExecutionModeDecision {
	switch decision.Mode {
	case ExecutionModeDirect, ExecutionModePlanned, ExecutionModeWatch, ExecutionModeWorkflow:
	default:
		decision.Mode = defaultExecutionMode(plannerAvailable)
		return decision
	}
	if !plannerAvailable {
		decision.Mode = ExecutionModeDirect
	}
	return decision
}

func refineExecutionModeWithTaskContract(current ExecutionMode, plannerAvailable bool, preflight *RunPreflightReport, contract *TaskContract) ExecutionMode {
	if !plannerAvailable {
		return ExecutionModeDirect
	}
	if contract == nil {
		return current
	}
	refined := inferExecutionModeFromTaskContract(preflight, contract)
	if refined == "" {
		return current
	}
	return refined
}

func inferExecutionModeFromTaskContract(preflight *RunPreflightReport, contract *TaskContract) ExecutionMode {
	if contract == nil {
		return ""
	}
	domains := taskContractExecutionDomains(preflight, contract)
	switch {
	case taskContractIndicatesWatch(contract, domains):
		return ExecutionModeWatch
	case taskContractIndicatesWorkflow(contract, domains):
		return ExecutionModeWorkflow
	case taskContractIndicatesPlanned(contract, domains):
		return ExecutionModePlanned
	case taskContractSuggestsDirectExecution(contract, domains):
		return ExecutionModeDirect
	default:
		return ""
	}
}

func taskContractExecutionDomains(preflight *RunPreflightReport, contract *TaskContract) []string {
	var domains []string
	if preflight != nil {
		domains = append(domains, preflight.SuggestedDomains...)
	}
	if contract != nil {
		domains = append(domains, contract.SuggestedDomains...)
	}
	return normalizeSemanticDomains(domains)
}

func taskContractIndicatesWatch(contract *TaskContract, domains []string) bool {
	if contract == nil {
		return false
	}
	if contract.JobType == taskContractJobMonitor {
		return true
	}
	if contract.JobType == taskContractJobAutomation &&
		(hasSemanticDomain(domains, DomainWatch, DomainCron, DomainCalendar) ||
			taskContractHasMissingInfo(contract, taskMissingInfoSchedule) ||
			contractHasDeliverableKind(contract, taskDeliverableWatchAlert)) {
		return true
	}
	return hasSemanticDomain(domains, DomainWatch)
}

func taskContractIndicatesWorkflow(contract *TaskContract, domains []string) bool {
	if contract == nil {
		return false
	}
	switch contract.JobType {
	case taskContractJobDelivery, taskContractJobDeployment:
		return true
	case taskContractJobAutomation:
		return !taskContractIndicatesWatch(contract, domains)
	}
	if contractHasDeliverableKind(contract, taskDeliverableMessageDelivery, taskDeliverableDeployment) {
		return true
	}
	if contract.RequiresApproval && contract.RequiresExternalEffect {
		return true
	}
	return false
}

func taskContractIndicatesPlanned(contract *TaskContract, domains []string) bool {
	if contract == nil {
		return false
	}
	switch contract.JobType {
	case taskContractJobResearch, taskContractJobReport, taskContractJobDevelopment:
		return true
	}
	return hasSemanticDomain(domains, DomainSearch, DomainWeb, DomainNews, DomainGit)
}

func taskContractSuggestsDirectExecution(contract *TaskContract, domains []string) bool {
	if contract == nil {
		return false
	}
	if contract.RequiresExternalEffect || contract.RequiresApproval || len(contract.MissingInfo) > 0 {
		return false
	}
	switch contract.JobType {
	case taskContractJobMonitor, taskContractJobDelivery, taskContractJobDeployment, taskContractJobAutomation, taskContractJobDevelopment:
		return false
	}
	if contractHasDeliverableKind(contract,
		taskDeliverableDocument,
		taskDeliverableSpreadsheet,
		taskDeliverablePresentation,
		taskDeliverableMessageDelivery,
		taskDeliverableWatchAlert,
		taskDeliverableDeployment,
	) {
		return false
	}
	if !taskContractDomainsFitDirectExecution(domains) {
		return false
	}
	return true
}

func taskContractDomainsFitDirectExecution(domains []string) bool {
	if len(domains) == 0 {
		return false
	}
	allowed := map[ToolDomain]bool{
		DomainBrowser: true,
		DomainDesktop: true,
		DomainPDF:     true,
		DomainFS:      true,
		DomainText:    true,
		DomainNet:     true,
		DomainMedia:   true,
		DomainVision:  true,
		DomainSearch:  true,
		DomainWeb:     true,
	}
	for _, item := range domains {
		domain := ToolDomain(strings.TrimSpace(item))
		if domain == "" {
			continue
		}
		if !allowed[domain] {
			return false
		}
	}
	return true
}

func hasSemanticDomain(domains []string, candidates ...ToolDomain) bool {
	for _, item := range domains {
		domain := ToolDomain(strings.TrimSpace(item))
		for _, candidate := range candidates {
			if domain == candidate {
				return true
			}
		}
	}
	return false
}

func taskContractHasMissingInfo(contract *TaskContract, ids ...string) bool {
	if contract == nil {
		return false
	}
	for _, item := range contract.MissingInfo {
		for _, id := range ids {
			if strings.TrimSpace(item.ID) == strings.TrimSpace(id) {
				return true
			}
		}
	}
	return false
}

func contractHasDeliverableKind(contract *TaskContract, kinds ...string) bool {
	if contract == nil {
		return false
	}
	return taskContractDeliverablesContain(contract.ExpectedDeliverables, kinds...)
}
