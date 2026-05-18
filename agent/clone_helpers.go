package agent

import (
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	planpkg "github.com/fulcrus/hopclaw/planner"
)

func clonePlan(in *planpkg.Plan) *planpkg.Plan {
	if in == nil {
		return nil
	}
	out := *in
	out.Tasks = clonePlanTasks(in.Tasks)
	out.RunningTasks = cloneStrings(in.RunningTasks)
	out.CoverageWarnings = cloneStrings(in.CoverageWarnings)
	return &out
}

func clonePlanTasks(in []planpkg.Task) []planpkg.Task {
	if in == nil {
		return nil
	}
	out := make([]planpkg.Task, len(in))
	for i, task := range in {
		out[i] = task
		out[i].DependsOn = cloneStrings(task.DependsOn)
		out[i].Outputs = cloneStrings(task.Outputs)
		out[i].RequiredCapabilities = cloneStrings(task.RequiredCapabilities)
		out[i].VerificationHints = cloneStrings(task.VerificationHints)
	}
	return out
}

func cloneTaskContract(in *TaskContract) *TaskContract {
	if in == nil {
		return nil
	}
	out := *in
	out.SuggestedDomains = cloneStrings(in.SuggestedDomains)
	out.CapabilityHints = cloneStrings(in.CapabilityHints)
	out.ExpectedDeliverables = cloneTaskContractDeliverables(in.ExpectedDeliverables)
	out.AcceptanceCriteria = cloneTaskContractAcceptance(in.AcceptanceCriteria)
	out.MissingInfo = cloneTaskContractMissingInfo(in.MissingInfo)
	out.ResolvedInfo = cloneTaskContractResolvedInfo(in.ResolvedInfo)
	return &out
}

func cloneTaskContractDeliverables(in []TaskContractDeliverable) []TaskContractDeliverable {
	if in == nil {
		return nil
	}
	out := make([]TaskContractDeliverable, len(in))
	copy(out, in)
	return out
}

func cloneTaskContractAcceptance(in []TaskContractAcceptance) []TaskContractAcceptance {
	if in == nil {
		return nil
	}
	out := make([]TaskContractAcceptance, len(in))
	for i, item := range in {
		out[i] = item
		out[i].DeliverableKinds = cloneStrings(item.DeliverableKinds)
		out[i].EvidenceHints = cloneStrings(item.EvidenceHints)
	}
	return out
}

func cloneTaskContractMissingInfo(in []TaskContractMissingInfo) []TaskContractMissingInfo {
	if in == nil {
		return nil
	}
	out := make([]TaskContractMissingInfo, len(in))
	for i, item := range in {
		out[i] = item
		out[i].Hints = cloneStrings(item.Hints)
	}
	return out
}

func cloneTaskContractResolvedInfo(in []TaskContractResolvedInfo) []TaskContractResolvedInfo {
	if in == nil {
		return nil
	}
	out := make([]TaskContractResolvedInfo, len(in))
	copy(out, in)
	return out
}

func cloneDelegationContract(in *DelegationContract) *DelegationContract {
	if in == nil {
		return nil
	}
	out := *in
	out.AllowedDomains = cloneStrings(in.AllowedDomains)
	out.AllowedTools = cloneStrings(in.AllowedTools)
	return &out
}

func cloneRunPreflightReport(in *RunPreflightReport) *RunPreflightReport {
	if in == nil {
		return nil
	}
	out := *in
	out.ReplyHints = cloneStrings(in.ReplyHints)
	out.SuggestedDomains = cloneStrings(in.SuggestedDomains)
	out.DetectedDomains = cloneStrings(in.DetectedDomains)
	out.Checks = cloneRunPreflightChecks(in.Checks)
	out.ClarificationSlots = cloneRunClarificationSlots(in.ClarificationSlots)
	return &out
}

func cloneRunPreflightChecks(in []RunPreflightCheck) []RunPreflightCheck {
	if in == nil {
		return nil
	}
	out := make([]RunPreflightCheck, len(in))
	copy(out, in)
	return out
}

func cloneRunClarificationSlots(in []RunClarificationSlot) []RunClarificationSlot {
	if in == nil {
		return nil
	}
	out := make([]RunClarificationSlot, len(in))
	for i, slot := range in {
		out[i] = slot
		out[i].Hints = cloneStrings(slot.Hints)
	}
	return out
}

func cloneRunTriageTrace(in *RunTriageTrace) *RunTriageTrace {
	if in == nil {
		return nil
	}
	out := *in
	out.SuggestedDomains = cloneStrings(in.SuggestedDomains)
	return &out
}

func cloneGovernanceEvaluation(in *domaingov.Evaluation) *domaingov.Evaluation {
	if in == nil {
		return nil
	}
	out := *in
	out.Decision = out.Decision.Normalized()
	out.Decision.Reasons = cloneStrings(in.Decision.Reasons)
	out.ToolNames = cloneStrings(in.ToolNames)
	return &out
}

func cloneWorkflowState(in *WorkflowState) *WorkflowState {
	if in == nil {
		return nil
	}
	out := *in
	if in.Budget != nil {
		budget := *in.Budget
		out.Budget = &budget
	}
	out.PriorRunSummaries = cloneStrings(in.PriorRunSummaries)
	out.CompletedTaskIDs = cloneStrings(in.CompletedTaskIDs)
	return &out
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneMap(in map[string]any) map[string]any {
	return supportmaps.Clone(in)
}
