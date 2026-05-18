package agent

import (
	"strings"

	planpkg "github.com/fulcrus/hopclaw/planner"
)

type RunHarnessSpec struct {
	Domains  []string
	Approval RunHarnessApprovalSpec
	Model    RunHarnessModelSpec
	Recovery RunHarnessRecoverySpec
	Budget   RunHarnessBudgetSpec
}

type RunHarnessApprovalSpec struct {
	NeedsConfirmation    bool
	RequiresApproval     bool
	RequiresExternalSide bool
}

type RunHarnessModelSpec struct {
	RequireThinking bool
}

type RunHarnessRecoverySpec struct {
	TransparentIntent *transparentRecoveryIntent
	MissingDomains    []string
	ExtraAttempts     int
}

type RunHarnessBudgetSpec struct {
	ExtraToolRounds int
}

type RunHarnessSummary struct {
	Domains                    []string `json:"domains,omitempty"`
	MissingDomains             []string `json:"missing_domains,omitempty"`
	NeedsConfirmation          bool     `json:"needs_confirmation,omitempty"`
	RequiresApproval           bool     `json:"requires_approval,omitempty"`
	RequiresExternalSideEffect bool     `json:"requires_external_side_effect,omitempty"`
	RequireThinkingModel       bool     `json:"require_thinking_model,omitempty"`
	TransparentRecoveryIntent  string   `json:"transparent_recovery_intent,omitempty"`
	ExtraToolRounds            int      `json:"extra_tool_rounds,omitempty"`
	ExtraRecoveryAttempts      int      `json:"extra_recovery_attempts,omitempty"`
}

func buildRunHarnessSpec(run *Run, task *planpkg.Task, prompt string, tools []ToolDefinition) RunHarnessSpec {
	domains := routingDomainsForTurn(run, prompt)
	structuredDomains := harnessStructuredDomains(run)
	missingDomains := missingActivatedDomainsForTurn(run, prompt, tools)
	recoveryIntent := harnessTransparentRecoveryIntent(run, task, prompt, tools)
	researchHeavy := harnessNeedsResearchHeavyBudget(run, task, structuredDomains)
	spec := RunHarnessSpec{
		Domains: domains,
		Approval: RunHarnessApprovalSpec{
			NeedsConfirmation:    preflightHasCheck(runPreflight(run), "expected_confirmation"),
			RequiresApproval:     runTaskContract(run) != nil && runTaskContract(run).RequiresApproval,
			RequiresExternalSide: runTaskContract(run) != nil && runTaskContract(run).RequiresExternalEffect,
		},
		Model: RunHarnessModelSpec{
			RequireThinking: recoveryIntent != nil || researchHeavy,
		},
		Recovery: RunHarnessRecoverySpec{
			TransparentIntent: recoveryIntent,
			MissingDomains:    append([]string(nil), missingDomains...),
			ExtraAttempts:     buildRunHarnessRecoveryExtra(run),
		},
	}
	spec.Budget = buildRunHarnessBudgetSpec(run, task, spec, researchHeavy)
	return spec
}

func ProjectRunHarnessSummary(run *Run, prompt string) *RunHarnessSummary {
	spec := buildRunHarnessSpec(run, nil, prompt, nil)
	summary := RunHarnessSummary{
		Domains:                    append([]string(nil), spec.Domains...),
		MissingDomains:             append([]string(nil), spec.Recovery.MissingDomains...),
		NeedsConfirmation:          spec.Approval.NeedsConfirmation,
		RequiresApproval:           spec.Approval.RequiresApproval,
		RequiresExternalSideEffect: spec.Approval.RequiresExternalSide,
		RequireThinkingModel:       spec.Model.RequireThinking,
		TransparentRecoveryIntent:  harnessIntentKey(spec),
		ExtraToolRounds:            spec.Budget.ExtraToolRounds,
		ExtraRecoveryAttempts:      spec.Recovery.ExtraAttempts,
	}
	if summary.empty() {
		return nil
	}
	return &summary
}

func buildRunHarnessBudgetSpec(run *Run, task *planpkg.Task, spec RunHarnessSpec, researchHeavy bool) RunHarnessBudgetSpec {
	extra := 0
	structuredDomains := harnessStructuredDomains(run)
	if run != nil {
		switch run.ExecutionMode {
		case ExecutionModePlanned, ExecutionModeWorkflow:
			extra += 4
		}
	}
	if task != nil {
		switch task.Kind {
		case planpkg.TaskExecute, planpkg.TaskResearch:
			extra += 1
		}
	}
	if harnessNeedsInteractiveSurfaceBudget(run, task, structuredDomains) {
		extra += 3
	}
	if harnessNeedsMultiStepBudget(run, task, structuredDomains) {
		extra += 1
	}
	if researchHeavy {
		extra += 2
	}
	if spec.Recovery.TransparentIntent != nil {
		extra += 2
	}
	if extra > 8 {
		extra = 8
	}
	return RunHarnessBudgetSpec{ExtraToolRounds: extra}
}

func harnessTransparentRecoveryIntent(run *Run, task *planpkg.Task, fallbackGoal string, tools []ToolDefinition) *transparentRecoveryIntent {
	if len(tools) == 0 {
		return inferTransparentRecoveryIntentForRun(run, task, fallbackGoal, []ToolDefinition{{Name: "skill.ensure"}})
	}
	return inferTransparentRecoveryIntentForRun(run, task, fallbackGoal, tools)
}

func missingActivatedDomainsForTurn(run *Run, prompt string, tools []ToolDefinition) []string {
	visible := filterModelVisibleTools(tools)
	if len(visible) == 0 {
		return nil
	}
	activated := activatedDomainsForMissingRecovery(run, prompt, visible)
	missing := checkEmptyActivatedDomains(activated, visible)
	if len(missing) == 0 {
		return nil
	}
	out := make([]string, 0, len(missing))
	for _, domain := range missing {
		out = append(out, string(domain))
	}
	return out
}

func activatedDomainsForMissingRecovery(run *Run, prompt string, tools []ToolDefinition) map[ToolDomain]bool {
	if run != nil && run.TaskContract != nil {
		if domains := semanticDomainMap(run.TaskContract.SuggestedDomains); len(domains) > 0 {
			return domains
		}
	}
	if run != nil && run.Preflight != nil {
		if domains := semanticDomainMap(run.Preflight.DetectedDomains); len(domains) > 0 {
			return domains
		}
		if domains := semanticDomainMap(run.Preflight.SuggestedDomains); len(domains) > 0 {
			return domains
		}
	}
	if run != nil && run.Triage != nil {
		if domains := semanticDomainMap(run.Triage.SuggestedDomains); len(domains) > 0 {
			return domains
		}
	}
	signals := inferToolSelectionSignals(prompt, nil, runTaskContract(run))
	return resolveActivatedDomainsForToolSelection(tools, prompt, signals)
}

func semanticDomainMap(items []string) map[ToolDomain]bool {
	normalized := normalizeSemanticDomains(items)
	if len(normalized) == 0 {
		return nil
	}
	out := make(map[ToolDomain]bool, len(normalized))
	for _, item := range normalized {
		out[ToolDomain(item)] = true
	}
	return out
}

func harnessNeedsResearchHeavyBudget(run *Run, task *planpkg.Task, structuredDomains []string) bool {
	if task != nil {
		if task.Kind == planpkg.TaskResearch {
			return true
		}
		if hasCapability(task.RequiredCapabilities, "rss.fetch", "news.fetch", "search.news", "news.digest", "search.web", "email.search", "email.read") {
			return true
		}
	}
	if hasSemanticDomain(structuredDomains, DomainEmail, DomainNews) {
		return true
	}
	if run == nil || run.TaskContract == nil {
		return false
	}
	switch strings.TrimSpace(run.TaskContract.JobType) {
	case taskContractJobResearch:
		return true
	case taskContractJobDelivery:
		return hasSemanticDomain(structuredDomains, DomainEmail)
	}
	return false
}

func runPreflight(run *Run) *RunPreflightReport {
	if run == nil {
		return nil
	}
	return run.Preflight
}

func runTaskContract(run *Run) *TaskContract {
	if run == nil {
		return nil
	}
	return run.TaskContract
}

func buildRunHarnessRecoveryExtra(run *Run) int {
	if run == nil {
		return 0
	}
	switch run.ExecutionMode {
	case ExecutionModePlanned, ExecutionModeWorkflow:
		return 2
	default:
		return 0
	}
}

func harnessNeedsInteractiveSurfaceBudget(run *Run, task *planpkg.Task, structuredDomains []string) bool {
	if task != nil && hasCapability(task.RequiredCapabilities, "desktop", "browser") {
		return true
	}
	return harnessHasStructuredInteractiveSurface(run, structuredDomains)
}

func harnessNeedsMultiStepBudget(run *Run, task *planpkg.Task, structuredDomains []string) bool {
	return harnessHasStructuredMultiStepComplexity(run, task, structuredDomains)
}

func harnessStructuredDomains(run *Run) []string {
	if run == nil {
		return nil
	}
	values := make([]string, 0, 8)
	if run.Preflight != nil {
		values = append(values, run.Preflight.SuggestedDomains...)
	}
	if run.TaskContract != nil {
		values = append(values, run.TaskContract.SuggestedDomains...)
	}
	if run.Triage != nil {
		values = append(values, run.Triage.SuggestedDomains...)
	}
	return normalizeSemanticDomains(values)
}

func harnessHasStructuredInteractiveSurface(run *Run, structuredDomains []string) bool {
	if hasSemanticDomain(structuredDomains, DomainBrowser, DomainDesktop) {
		return true
	}
	if run == nil || run.TaskContract == nil {
		return false
	}
	for _, item := range run.TaskContract.ExpectedDeliverables {
		switch strings.TrimSpace(item.Kind) {
		case taskDeliverableBrowserEvidence, taskDeliverableDesktopEvidence:
			return true
		}
	}
	return false
}

func harnessHasStructuredMultiStepComplexity(run *Run, task *planpkg.Task, structuredDomains []string) bool {
	if task != nil {
		return len(task.DependsOn) > 0 || len(task.Outputs) > 1 || len(task.RequiredCapabilities) > 1
	}
	if run == nil || run.TaskContract == nil {
		return false
	}
	contract := run.TaskContract
	switch contract.JobType {
	case taskContractJobResearch, taskContractJobDevelopment, taskContractJobDeployment, taskContractJobAutomation:
		return true
	case taskContractJobDelivery:
		if contract.RequiresExternalEffect || contract.RequiresApproval {
			return true
		}
		if hasSemanticDomain(structuredDomains, DomainDocument, DomainSheet, DomainPresentation, DomainEmail, DomainChannel, DomainCalendar) {
			return true
		}
	}
	if len(contract.ExpectedDeliverables) > 1 {
		return true
	}
	if len(structuredDomains) >= 2 {
		return true
	}
	return false
}

func (s RunHarnessSummary) empty() bool {
	return len(s.Domains) == 0 &&
		len(s.MissingDomains) == 0 &&
		!s.NeedsConfirmation &&
		!s.RequiresApproval &&
		!s.RequiresExternalSideEffect &&
		!s.RequireThinkingModel &&
		s.TransparentRecoveryIntent == "" &&
		s.ExtraToolRounds == 0 &&
		s.ExtraRecoveryAttempts == 0
}

func harnessIntentKey(spec RunHarnessSpec) string {
	if spec.Recovery.TransparentIntent != nil {
		return strings.TrimSpace(spec.Recovery.TransparentIntent.Key)
	}
	return ""
}
