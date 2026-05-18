package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	delegationContractSourceHeuristic = "heuristic"

	delegationDefaultMaxTurns     = 4
	delegationPlannedMaxTurns     = 6
	delegationDefaultBudgetTokens = 4000
	delegationRichBudgetTokens    = 8000
	delegationToolHintLimit       = 12
)

func buildDelegationContract(message string, mode ExecutionMode, preflight *RunPreflightReport, task *TaskContract) *DelegationContract {
	if task == nil {
		return nil
	}
	if preflight != nil && preflight.Blocking {
		return nil
	}
	if hasRequiredTaskContractMissingInfo(task.MissingInfo) {
		return nil
	}
	message = strings.TrimSpace(message)
	if !shouldEnableDelegation(mode, task) {
		return nil
	}

	allowedDomains := inferDelegationAllowedDomains(message, task)
	if len(allowedDomains) == 0 {
		return nil
	}

	goal := strings.TrimSpace(task.Goal)
	if goal == "" {
		goal = message
	}
	return &DelegationContract{
		Goal:                goal,
		AllowedDomains:      allowedDomains,
		SideEffectClass:     inferDelegationSideEffectClass(task),
		MaxTurns:            inferDelegationMaxTurns(mode, task),
		MaxBudgetTokens:     inferDelegationMaxBudgetTokens(mode, task),
		RequiresApproval:    task.RequiresApproval,
		VerificationPlanRef: inferDelegationVerificationPlanRef(task),
		Source:              delegationContractSourceHeuristic,
		GeneratedAt:         time.Now().UTC(),
	}
}

func deriveEffectiveDelegationContract(run *Run, contract *DelegationContract) *DelegationContract {
	if contract == nil {
		return nil
	}
	effective := cloneDelegationContract(contract)
	if run == nil || run.WorkflowState == nil || run.WorkflowState.Budget == nil {
		return effective
	}

	budget := run.WorkflowState.Budget
	switch budget.Mode {
	case WorkflowBudgetModeStopped, WorkflowBudgetModeFinishOnly:
		return nil
	case WorkflowBudgetModeEconomy:
		if workflowBudgetDisablesDelegation(budget, budget.Mode) {
			return nil
		}
	}

	remainingRounds := 0
	if budget.Policy.HardTotalRounds > 0 {
		remainingRounds = max(budget.Policy.HardTotalRounds-run.WorkflowState.TotalRoundsUsed, 0)
	}
	if remainingRounds <= 1 && remainingRounds != 0 {
		return nil
	}
	if remainingRounds > 0 {
		turnCap := max(1, min(remainingRounds/2, delegationDefaultMaxTurns))
		if turnCap == 0 {
			turnCap = 1
		}
		if effective.MaxTurns <= 0 || turnCap < effective.MaxTurns {
			effective.MaxTurns = turnCap
		}
	}

	remainingTokens := workflowBudgetRemainingModelTokens(run.WorkflowState)
	if remainingTokens > 0 {
		tokenCap := remainingTokens / 4
		if budget.Policy.MaxDelegatedTokenFraction > 0 {
			fractionCap := int(float64(remainingTokens) * budget.Policy.MaxDelegatedTokenFraction)
			if tokenCap == 0 || (fractionCap > 0 && fractionCap < tokenCap) {
				tokenCap = fractionCap
			}
		}
		if tokenCap <= 0 {
			return nil
		}
		if effective.MaxBudgetTokens <= 0 || tokenCap < effective.MaxBudgetTokens {
			effective.MaxBudgetTokens = tokenCap
		}
	}

	if budget.Mode == WorkflowBudgetModeEconomy {
		if effective.MaxTurns > delegationDefaultMaxTurns {
			effective.MaxTurns = delegationDefaultMaxTurns
		}
		if effective.MaxBudgetTokens > 0 {
			effective.MaxBudgetTokens = max(1, effective.MaxBudgetTokens/2)
		}
	}

	return effective
}

func shouldEnableDelegation(mode ExecutionMode, task *TaskContract) bool {
	if task == nil {
		return false
	}
	if mode == ExecutionModePlanned || mode == ExecutionModeWorkflow {
		return true
	}
	if len(task.SuggestedDomains) >= 2 || len(task.ExpectedDeliverables) >= 3 {
		return true
	}
	switch strings.TrimSpace(task.JobType) {
	case taskContractJobDevelopment, taskContractJobResearch, taskContractJobReport:
		return len(task.ExpectedDeliverables) >= 2
	default:
		return false
	}
}

func inferDelegationAllowedDomains(message string, task *TaskContract) []string {
	domains := append([]string(nil), task.SuggestedDomains...)
	if len(domains) == 0 {
		domains = domainsToStrings(detectStructuredEvidence(message))
	}
	domains = append(domains, string(DomainText))
	for _, deliverable := range task.ExpectedDeliverables {
		switch strings.TrimSpace(deliverable.Kind) {
		case taskDeliverableBrowserEvidence:
			domains = append(domains, string(DomainBrowser))
		case taskDeliverableDesktopEvidence:
			domains = append(domains, string(DomainDesktop))
		case taskDeliverableDocument:
			domains = append(domains, string(DomainDocument), string(DomainFS))
		case taskDeliverableSpreadsheet:
			domains = append(domains, string(DomainSheet), string(DomainFS))
		case taskDeliverablePresentation:
			domains = append(domains, string(DomainPresentation), string(DomainFS))
		case taskDeliverableMessageDelivery:
			domains = append(domains, string(DomainChannel), string(DomainEmail))
		case taskDeliverableWatchAlert:
			domains = append(domains, string(DomainWatch), string(DomainCron))
		case taskDeliverableDeployment:
			domains = append(domains, string(DomainNet), string(DomainGateway), string(DomainProc))
		}
	}
	switch strings.TrimSpace(task.JobType) {
	case taskContractJobDevelopment:
		domains = append(domains, string(DomainFS), string(DomainExec), string(DomainGit))
	case taskContractJobDelivery:
		domains = append(domains, string(DomainChannel), string(DomainEmail))
	case taskContractJobMonitor, taskContractJobAutomation:
		domains = append(domains, string(DomainWatch), string(DomainCron))
	}
	normalized := normalizeSemanticDomains(domains)
	out := make([]string, 0, len(normalized))
	for _, item := range normalized {
		if ToolDomain(item) == DomainAgent {
			continue
		}
		out = append(out, item)
	}
	return out
}

func inferDelegationSideEffectClass(task *TaskContract) string {
	if task == nil {
		return "read"
	}
	if task.RequiresExternalEffect ||
		taskContractDeliverablesContain(task.ExpectedDeliverables, taskDeliverableMessageDelivery, taskDeliverableWatchAlert, taskDeliverableDeployment) {
		return "external_write"
	}
	if strings.TrimSpace(task.JobType) == taskContractJobDevelopment ||
		taskContractDeliverablesContain(task.ExpectedDeliverables, taskDeliverableDocument, taskDeliverableSpreadsheet, taskDeliverablePresentation) ||
		looksLikeLocalFileToken(strings.TrimSpace(task.TargetSummary)) {
		return "local_write"
	}
	return "read"
}

func inferDelegationMaxTurns(mode ExecutionMode, task *TaskContract) int {
	maxTurns := delegationDefaultMaxTurns
	if mode == ExecutionModePlanned || mode == ExecutionModeWorkflow {
		maxTurns = delegationPlannedMaxTurns
	}
	if task != nil && len(task.ExpectedDeliverables) >= 3 {
		maxTurns++
	}
	if task != nil && task.RequiresApproval && maxTurns > delegationDefaultMaxTurns {
		maxTurns = delegationDefaultMaxTurns
	}
	if maxTurns < 2 {
		return 2
	}
	if maxTurns > 8 {
		return 8
	}
	return maxTurns
}

func inferDelegationMaxBudgetTokens(mode ExecutionMode, task *TaskContract) int {
	budget := delegationDefaultBudgetTokens
	if mode == ExecutionModePlanned || mode == ExecutionModeWorkflow {
		budget = delegationRichBudgetTokens
	}
	if task != nil {
		switch strings.TrimSpace(task.JobType) {
		case taskContractJobDevelopment, taskContractJobResearch, taskContractJobReport:
			budget = delegationRichBudgetTokens
		}
		if task.RequiresApproval && budget > 6000 {
			budget = 6000
		}
	}
	return budget
}

func inferDelegationVerificationPlanRef(task *TaskContract) string {
	if task == nil {
		return ""
	}
	values := make([]string, 0, len(task.AcceptanceCriteria)+len(task.ExpectedDeliverables))
	for _, item := range task.AcceptanceCriteria {
		if !item.Required {
			continue
		}
		if trimmed := strings.TrimSpace(item.ID); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if len(values) == 0 {
		for _, item := range task.ExpectedDeliverables {
			if !item.Required {
				continue
			}
			if trimmed := strings.TrimSpace(item.Kind); trimmed != "" {
				values = append(values, trimmed)
			}
		}
	}
	if len(values) == 0 {
		return ""
	}
	values = normalizeDelegationRefs(values)
	if len(values) > 4 {
		values = values[:4]
	}
	return "task_contract:" + strings.Join(values, ",")
}

func buildDelegationContractPrompt(run *Run, tools []ToolDefinition) string {
	if run == nil || !hasVisibleAgentTool(tools) {
		return ""
	}
	contract := deriveEffectiveDelegationContract(run, run.Delegation)
	if contract == nil {
		return ""
	}
	lines := []string{
		"<delegation_contract>",
		"Sub-agents are allowed for this run only as a bounded scheduling primitive.",
		"Use `agent.spawn` only when delegation materially improves latency, parallelism, or isolation. Keep the immediate blocking step in the main run when you need the result before continuing.",
		"Child contract:",
	}
	if goal := truncatePlanningText(strings.TrimSpace(contract.Goal), 220); goal != "" {
		lines = append(lines, "- goal: "+goal)
	}
	if len(contract.AllowedDomains) > 0 {
		lines = append(lines, "- allowed domains: "+strings.Join(contract.AllowedDomains, ", "))
	}
	if allowedTools := delegationAllowedToolHints(contract, tools); len(allowedTools) > 0 {
		lines = append(lines, "- allowed child tools: "+strings.Join(allowedTools, ", "))
	}
	if sideEffect := strings.TrimSpace(contract.SideEffectClass); sideEffect != "" {
		lines = append(lines, "- side-effect ceiling: "+sideEffect)
	}
	if contract.MaxTurns > 0 {
		lines = append(lines, fmt.Sprintf("- max turns per child: %d", contract.MaxTurns))
	}
	if contract.MaxBudgetTokens > 0 {
		lines = append(lines, fmt.Sprintf("- max budget tokens per child: %d", contract.MaxBudgetTokens))
	}
	lines = append(lines, fmt.Sprintf("- requires approval before privileged side effects: %t", contract.RequiresApproval))
	if ref := strings.TrimSpace(contract.VerificationPlanRef); ref != "" {
		lines = append(lines, "- verification plan reference: "+ref)
	}
	lines = append(lines,
		"Delegation rules:",
		"- do not expand privileges, domains, or tools beyond this contract",
		"- do not spawn recursive sub-agents unless the parent run explicitly re-authorizes it",
		"- every child must return concrete evidence or reusable output, not only a status update",
		"</delegation_contract>",
	)
	return strings.Join(lines, "\n")
}

func hasVisibleAgentTool(tools []ToolDefinition) bool {
	for _, tool := range tools {
		if extractToolDomain(tool.Name) == DomainAgent {
			return true
		}
	}
	return false
}

func delegationAllowedToolHints(contract *DelegationContract, tools []ToolDefinition) []string {
	if contract == nil {
		return nil
	}
	if len(contract.AllowedTools) > 0 {
		return limitDelegationNames(contract.AllowedTools)
	}
	if len(tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if !toolAllowedByDelegation(contract, tool) {
			continue
		}
		names = append(names, strings.TrimSpace(tool.Name))
	}
	return limitDelegationNames(names)
}

func limitDelegationNames(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	if len(out) <= delegationToolHintLimit {
		return out
	}
	limited := append([]string(nil), out[:delegationToolHintLimit]...)
	limited = append(limited, fmt.Sprintf("+%d more", len(out)-delegationToolHintLimit))
	return limited
}

func toolAllowedByDelegation(contract *DelegationContract, tool ToolDefinition) bool {
	name := strings.TrimSpace(tool.Name)
	if name == "" || extractToolDomain(name) == DomainAgent {
		return false
	}
	if len(contract.AllowedTools) > 0 {
		for _, allowed := range contract.AllowedTools {
			if strings.EqualFold(strings.TrimSpace(allowed), name) {
				return delegationSideEffectWithin(tool.SideEffectClass, contract.SideEffectClass)
			}
		}
		return false
	}
	if len(contract.AllowedDomains) > 0 {
		domain := normalizeToolDefinition(tool).Domain
		matched := false
		for _, allowed := range contract.AllowedDomains {
			if strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(domain)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return delegationSideEffectWithin(tool.SideEffectClass, contract.SideEffectClass)
}

func delegationSideEffectWithin(toolEffect, ceiling string) bool {
	ceilingRank := delegationSideEffectRank(ceiling)
	if ceilingRank < 0 {
		return true
	}
	toolRank := delegationSideEffectRank(toolEffect)
	if toolRank < 0 {
		return false
	}
	return toolRank <= ceilingRank
}

func delegationSideEffectRank(effect string) int {
	switch strings.ToLower(strings.TrimSpace(effect)) {
	case "":
		return -1
	case "read":
		return 0
	case "local_write":
		return 1
	case "external_write", "remote_write":
		return 2
	case "destructive":
		return 3
	default:
		return -1
	}
}

func normalizeDelegationRefs(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}
