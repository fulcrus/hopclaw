package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func fetchSessionSummaries(ctx context.Context, client *GatewayClient) ([]replpkg.SessionSummary, error) {
	var response struct {
		Items []agent.SessionSummary `json:"items"`
	}
	if err := client.Get(ctx, "/runtime/sessions", &response); err != nil {
		return nil, err
	}
	return mapSessionSummaries(response.Items), nil
}

func mapSessionSummaries(items []agent.SessionSummary) []replpkg.SessionSummary {
	out := make([]replpkg.SessionSummary, 0, len(items))
	for _, item := range items {
		out = append(out, replpkg.SessionSummary{
			ID:           item.ID,
			Key:          item.Key,
			Model:        item.Model,
			MessageCount: item.MessageCount,
		})
	}
	return out
}

func fetchSessionDetail(ctx context.Context, client *GatewayClient, id string) (*replpkg.SessionDetail, error) {
	var resp agent.Session
	if err := client.Get(ctx, "/runtime/sessions/"+id+"?include=messages", &resp); err != nil {
		return nil, err
	}
	return mapSessionDetail(&resp), nil
}

func mapSessionDetail(session *agent.Session) *replpkg.SessionDetail {
	if session == nil {
		return nil
	}
	detail := &replpkg.SessionDetail{
		Summary: replpkg.SessionSummary{
			ID:           session.ID,
			Key:          session.Key,
			Model:        session.Model,
			MessageCount: session.TotalMessageCount(),
		},
		Messages: make([]replpkg.SessionMessage, 0, len(session.Messages)),
	}
	for _, message := range session.Messages {
		detail.Messages = append(detail.Messages, replpkg.SessionMessage{
			Role:      string(message.Role),
			Content:   message.TextContent(),
			CreatedAt: message.CreatedAt.Format(time.RFC3339),
		})
	}
	return detail
}

func mapApprovalSummaries(items []*runtimesvc.ApprovalView) []replpkg.ApprovalSummary {
	out := make([]replpkg.ApprovalSummary, 0, len(items))
	for _, item := range items {
		if summary := mapApprovalSummary(item); summary != nil {
			out = append(out, *summary)
		}
	}
	return out
}

func mapApprovalSummary(item *runtimesvc.ApprovalView) *replpkg.ApprovalSummary {
	if item == nil {
		return nil
	}
	toolName := ""
	if len(item.ToolCalls) > 0 {
		toolName = strings.TrimSpace(item.ToolCalls[0].Name)
	} else if item.Governance != nil && len(item.Governance.ToolNames) > 0 {
		toolName = strings.TrimSpace(item.Governance.ToolNames[0])
	}
	policySummary := ""
	if item.Governance != nil {
		policySummary = strings.TrimSpace(item.Governance.Summary)
		if policySummary == "" && item.Governance.Policy != nil {
			policySummary = strings.TrimSpace(item.Governance.Policy.Summary)
		}
	}
	return &replpkg.ApprovalSummary{
		ID:            strings.TrimSpace(item.ID),
		RunID:         strings.TrimSpace(item.RunID),
		SessionID:     strings.TrimSpace(item.SessionID),
		Status:        strings.TrimSpace(string(item.Status)),
		ToolName:      toolName,
		PolicySummary: policySummary,
		CreatedAt:     item.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func mapQualitySnapshot(summary *runtimesvc.QualitySummary, report *runtimesvc.ReleaseReadinessReport) *replpkg.QualitySnapshot {
	if summary == nil && report == nil {
		return nil
	}
	snapshot := &replpkg.QualitySnapshot{}
	if summary != nil {
		snapshot.RunCount = summary.RunCount
		snapshot.TerminalRunCount = summary.TerminalRunCount
		snapshot.TaskSuccess = formatQualityRate(summary.TaskSuccess)
		snapshot.FalseSuccess = formatQualityRate(summary.FalseSuccess)
		snapshot.VerificationFailure = formatQualityRate(summary.VerificationFailure)
		snapshot.TraceCount = summary.TraceCount
	}
	if report != nil {
		snapshot.Ready = report.Ready
		snapshot.CheckCount = len(report.Checks)
		snapshot.BlockerCount = len(report.Blockers)
		snapshot.Blockers = make([]string, 0, len(report.Blockers))
		for _, blocker := range report.Blockers {
			label := strings.TrimSpace(blocker.ID)
			summary := strings.TrimSpace(blocker.Summary)
			switch {
			case label != "" && summary != "":
				snapshot.Blockers = append(snapshot.Blockers, label+": "+summary)
			case summary != "":
				snapshot.Blockers = append(snapshot.Blockers, summary)
			case label != "":
				snapshot.Blockers = append(snapshot.Blockers, label)
			}
		}
	}
	return snapshot
}

func mapEvalSuites(items []runtimesvc.EvalSuite) []replpkg.EvalSuiteSummary {
	out := make([]replpkg.EvalSuiteSummary, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = strings.TrimSpace(item.ID)
		}
		out = append(out, replpkg.EvalSuiteSummary{
			ID:        strings.TrimSpace(item.ID),
			Name:      name,
			Surface:   strings.TrimSpace(item.Surface),
			CaseCount: len(item.Cases),
		})
	}
	return out
}

func mapEvalRunSummary(report *runtimesvc.EvalSuiteRunReport) *replpkg.EvalRunSummary {
	if report == nil {
		return nil
	}
	return &replpkg.EvalRunSummary{
		SuiteID:   strings.TrimSpace(report.Suite.ID),
		Status:    strings.TrimSpace(report.Status),
		CaseCount: report.CaseCount,
		Passed:    report.Passed,
		Failed:    report.Failed,
		Errored:   report.Errored,
	}
}

func mapRunSummaries(items []*runtimesvc.RunListView) []replpkg.RunSummary {
	out := make([]replpkg.RunSummary, 0, len(items))
	for _, item := range items {
		if summary := mapRunSummary(item); summary != nil {
			out = append(out, *summary)
		}
	}
	return out
}

func mapRunSummary(item *runtimesvc.RunListView) *replpkg.RunSummary {
	if item == nil {
		return nil
	}
	toolName := ""
	if len(item.PendingTools) > 0 {
		toolName = strings.TrimSpace(item.PendingTools[0].Name)
	}
	if toolName == "" && item.Governance != nil && len(item.Governance.ToolNames) > 0 {
		toolName = strings.TrimSpace(item.Governance.ToolNames[0])
	}
	scopeSummary := ""
	if item.Governance != nil {
		scopeSummary = strings.TrimSpace(item.Governance.Summary)
		if scopeSummary == "" && item.Governance.Policy != nil {
			scopeSummary = strings.TrimSpace(item.Governance.Policy.Summary)
		}
	}
	attention := ""
	switch {
	case strings.TrimSpace(item.ApprovalID) != "":
		attention = "approval"
	case strings.TrimSpace(item.Error) != "":
		attention = "error"
	case strings.TrimSpace(item.VerificationStatus) != "" && !strings.EqualFold(strings.TrimSpace(item.VerificationStatus), "passed"):
		attention = strings.TrimSpace(item.VerificationStatus)
	}
	var automation *replpkg.AutomationProjection
	if automationID := strings.TrimSpace(item.Scope.AutomationID); automationID != "" {
		kind := strings.TrimSpace(string(item.ExecutionMode))
		if kind == "" {
			kind = "runtime"
		}
		automation = &replpkg.AutomationProjection{
			ID:   automationID,
			Name: automationID,
			Kind: kind,
		}
	}
	resumable := item.WorkflowState != nil && item.WorkflowState.NeedsContinuation()
	return &replpkg.RunSummary{
		ID:                  strings.TrimSpace(item.ID),
		SessionID:           strings.TrimSpace(item.SessionID),
		Status:              strings.TrimSpace(string(item.Status)),
		Phase:               strings.TrimSpace(string(item.Phase)),
		Model:               strings.TrimSpace(item.Model),
		Error:               strings.TrimSpace(item.Error),
		CreatedAt:           item.CreatedAt.UTC().Format(time.RFC3339),
		ToolName:            toolName,
		ScopeSummary:        scopeSummary,
		Attention:           attention,
		Outcome:             strings.TrimSpace(item.Outcome),
		VerificationStatus:  strings.TrimSpace(item.VerificationStatus),
		VerificationSummary: strings.TrimSpace(item.VerificationSummary),
		Resumable:           resumable,
		Automation:          automation,
	}
}

func mapRunDetail(item *runtimesvc.RunListView, result *runtimesvc.RunResult, output string) *replpkg.RunDetail {
	summary := mapRunSummary(item)
	if summary == nil {
		return nil
	}
	detail := &replpkg.RunDetail{
		Run:    *summary,
		Output: strings.TrimSpace(output),
	}
	if detail.Output == "" && result != nil {
		detail.Output = strings.TrimSpace(result.Output)
	}
	if item != nil {
		detail.Scope = strings.TrimSpace(summary.ScopeSummary)
		detail.Tool = strings.TrimSpace(summary.ToolName)
		detail.ScopeDetails = mapRunScopeDetails(item, result)
		detail.Semantic = mapRunSemanticDetails(item)
		detail.Workflow = mapRunWorkflowDetails(item)
		detail.Delegation = mapRunDelegationDetails(item, result)
		detail.ExecutionGraph = mapRunExecutionGraphDetails(item)
	}
	if result != nil {
		detail.Delivery = mapRunDelivery(result)
		detail.Automation = mapAutomationProjection(summary.Automation, result)
		detail.Run.Delivery = detail.Delivery
		detail.Run.Automation = detail.Automation
	}
	return detail
}

func mapRunScopeDetails(item *runtimesvc.RunListView, result *runtimesvc.RunResult) *replpkg.RunScopeDetails {
	if item == nil && result == nil {
		return nil
	}
	resources := make([]string, 0, 8)
	sideEffects := make([]string, 0, 4)
	if item != nil && item.ExecutionGraph != nil {
		for _, task := range item.ExecutionGraph.Tasks {
			if scope := strings.TrimSpace(string(task.SideEffectScope)); scope != "" {
				sideEffects = append(sideEffects, scope)
			}
			resources = append(resources, task.ResourceKeys...)
		}
	}
	summary := ""
	destructive := false
	if item != nil && item.Governance != nil && item.Governance.Policy != nil {
		summary = strings.TrimSpace(item.Governance.Policy.Summary)
		destructive = !strings.EqualFold(strings.TrimSpace(string(item.Governance.Policy.Action)), "allow")
	}
	if summary == "" && result != nil && result.Governance != nil && result.Governance.Policy != nil {
		summary = strings.TrimSpace(result.Governance.Policy.Summary)
		destructive = destructive || !strings.EqualFold(strings.TrimSpace(string(result.Governance.Policy.Action)), "allow")
	}
	if summary == "" && item != nil && item.Governance != nil {
		summary = strings.TrimSpace(item.Governance.Summary)
	}
	if summary == "" && result != nil && result.Governance != nil {
		summary = strings.TrimSpace(result.Governance.Summary)
	}
	if summary == "" && item != nil && item.TaskContract != nil {
		summary = strings.TrimSpace(item.TaskContract.TargetSummary)
	}
	sideEffects = dedupeTrimmedStrings(sideEffects)
	resources = dedupeTrimmedStrings(resources)
	sideEffectScope := strings.Join(sideEffects, ", ")
	if sideEffectScope == "" && item != nil && item.TaskContract != nil && item.TaskContract.RequiresExternalEffect {
		sideEffectScope = "external_effect"
	}
	if sideEffectScope == "" && len(resources) == 0 && summary == "" {
		return nil
	}
	return &replpkg.RunScopeDetails{
		SideEffectScope: sideEffectScope,
		Destructive:     destructive,
		Resources:       resources,
		Summary:         summary,
	}
}

func mapRunSemanticDetails(item *runtimesvc.RunListView) *replpkg.RunSemanticDetails {
	if item == nil || item.SemanticSignal == nil {
		return nil
	}
	signal := item.SemanticSignal
	language := strings.TrimSpace(signal.Language.Family)
	if script := strings.TrimSpace(signal.Language.Script); script != "" {
		if language != "" {
			language += "-" + script
		} else {
			language = script
		}
	}
	if language == "" {
		language = "unknown"
	}
	if !signal.RequiresCurrentInfo &&
		!signal.NeedsReference &&
		!signal.NeedsConfirmation &&
		len(signal.SuggestedDomains) == 0 &&
		strings.TrimSpace(signal.JobType) == "" &&
		strings.TrimSpace(signal.TargetSummary) == "" &&
		len(signal.CapabilityHints) == 0 &&
		len(signal.DeliverableKinds) == 0 &&
		len(signal.MissingInfoIDs) == 0 &&
		!signal.TriageReady &&
		!signal.TaskContractReady &&
		strings.TrimSpace(signal.Reason) == "" &&
		language == "unknown" {
		return nil
	}
	return &replpkg.RunSemanticDetails{
		Language:            language,
		RequiresCurrentInfo: signal.RequiresCurrentInfo,
		NeedsReference:      signal.NeedsReference,
		NeedsConfirmation:   signal.NeedsConfirmation,
		SuggestedDomains:    dedupeTrimmedStrings(signal.SuggestedDomains),
		JobType:             strings.TrimSpace(signal.JobType),
		TargetSummary:       strings.TrimSpace(signal.TargetSummary),
		CapabilityHints:     dedupeTrimmedStrings(signal.CapabilityHints),
		DeliverableKinds:    dedupeTrimmedStrings(signal.DeliverableKinds),
		MissingInfoIDs:      dedupeTrimmedStrings(signal.MissingInfoIDs),
		TriageReady:         signal.TriageReady,
		TaskContractReady:   signal.TaskContractReady,
		Reason:              strings.TrimSpace(signal.Reason),
	}
}

func mapRunWorkflowDetails(item *runtimesvc.RunListView) *replpkg.RunWorkflowDetails {
	if item == nil {
		return nil
	}
	mode := strings.TrimSpace(string(item.ExecutionMode))
	if mode == "" && item.WorkflowState == nil {
		return nil
	}
	if mode == "" {
		mode = "workflow"
	}
	out := &replpkg.RunWorkflowDetails{Mode: mode}
	if item.WorkflowState != nil {
		out.ContinuationIndex = item.WorkflowState.ContinuationIndex
		out.TotalRoundsUsed = item.WorkflowState.TotalRoundsUsed
		out.Yielded = item.WorkflowState.Yielded
		out.YieldReason = strings.TrimSpace(item.WorkflowState.YieldReason)
	}
	return out
}

func mapRunDelegationDetails(item *runtimesvc.RunListView, result *runtimesvc.RunResult) *replpkg.RunDelegationDetails {
	if item == nil && result == nil {
		return nil
	}
	contract := (*agent.DelegationContract)(nil)
	if item != nil {
		contract = item.Delegation
	}
	if contract == nil && result != nil {
		contract = result.Delegation
	}
	parallelTasks := 0
	serialFallback := 0
	if item != nil && item.ExecutionGraph != nil {
		for _, task := range item.ExecutionGraph.Tasks {
			if strings.EqualFold(strings.TrimSpace(string(task.MergeStrategy)), "serial_only") {
				serialFallback++
				continue
			}
			parallelTasks++
		}
	}
	if contract == nil && parallelTasks == 0 && serialFallback == 0 {
		return nil
	}
	return &replpkg.RunDelegationDetails{
		Enabled:         contract != nil || parallelTasks > 0 || serialFallback > 0,
		ParallelTasks:   parallelTasks,
		SerialFallback:  serialFallback,
		SideEffectClass: strings.TrimSpace(firstNonEmpty(sideEffectClassFromDelegation(contract), "")),
	}
}

func mapRunExecutionGraphDetails(item *runtimesvc.RunListView) *replpkg.RunExecutionGraphDetails {
	if item == nil || item.ExecutionGraph == nil || len(item.ExecutionGraph.Tasks) == 0 {
		return nil
	}
	out := &replpkg.RunExecutionGraphDetails{
		SingleSession:  item.ExecutionGraph.SingleSession,
		SessionLocking: item.ExecutionGraph.SessionLocking,
		Tasks:          make([]replpkg.RunExecutionTask, 0, len(item.ExecutionGraph.Tasks)),
	}
	for _, task := range item.ExecutionGraph.Tasks {
		out.Tasks = append(out.Tasks, replpkg.RunExecutionTask{
			ID:              strings.TrimSpace(task.ID),
			Title:           strings.TrimSpace(firstNonEmpty(task.Title, task.Goal, task.ID)),
			Status:          strings.TrimSpace(string(task.Status)),
			AttemptCount:    task.AttemptCount,
			MergeStrategy:   strings.TrimSpace(string(task.MergeStrategy)),
			SideEffectScope: strings.TrimSpace(string(task.SideEffectScope)),
			ResourceKeys:    dedupeTrimmedStrings(task.ResourceKeys),
			Summary:         taskExecutionSummary(task),
		})
	}
	return out
}

func taskExecutionSummary(task agent.ExecutionTask) string {
	if task.LastOutcome != nil {
		if task.LastOutcome.Error != nil && strings.TrimSpace(task.LastOutcome.Error.Message) != "" {
			return strings.TrimSpace(task.LastOutcome.Error.Message)
		}
		if summary := strings.TrimSpace(task.LastOutcome.Summary); summary != "" {
			return summary
		}
	}
	return strings.TrimSpace(task.Goal)
}

func sideEffectClassFromDelegation(contract *agent.DelegationContract) string {
	if contract == nil {
		return ""
	}
	return strings.TrimSpace(contract.SideEffectClass)
}

func dedupeTrimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func mapRunDelivery(result *runtimesvc.RunResult) *replpkg.RunDelivery {
	if result == nil {
		return nil
	}
	delivery := result.Delivery
	if delivery == nil && len(result.Receipts) == 0 {
		return nil
	}
	view := &replpkg.RunDelivery{
		Status:  strings.TrimSpace(result.VerificationStatus),
		Summary: strings.TrimSpace(result.VerificationSummary),
	}
	if delivery != nil {
		if view.Summary == "" {
			view.Summary = strings.TrimSpace(delivery.Summary)
		}
		if view.Status == "" {
			view.Status = "delivered"
		}
	}
	if len(result.Receipts) > 0 {
		latest := result.Receipts[len(result.Receipts)-1]
		view.ReceiptCount = len(result.Receipts)
		if strings.TrimSpace(latest.Status) != "" {
			view.Status = strings.TrimSpace(latest.Status)
		}
		if strings.TrimSpace(latest.Summary) != "" {
			view.Summary = strings.TrimSpace(latest.Summary)
		}
		if latest.Attempts > 0 || latest.MaxAttempts > 0 {
			view.Attempt = fmt.Sprintf("%d/%d", latest.Attempts, max(latest.MaxAttempts, latest.Attempts))
		}
		if !latest.NextAttemptAt.IsZero() {
			view.NextAttempt = latest.NextAttemptAt.Local().Format("15:04")
		}
	}
	if strings.TrimSpace(view.Status) == "" {
		view.Status = "unknown"
	}
	if strings.TrimSpace(view.Summary) == "" {
		view.Summary = strings.TrimSpace(result.Summary)
	}
	return view
}

func mapAutomationProjection(base *replpkg.AutomationProjection, result *runtimesvc.RunResult) *replpkg.AutomationProjection {
	if base == nil && (result == nil || result.Governance == nil || strings.TrimSpace(result.Governance.Scope.AutomationID) == "") {
		return nil
	}
	proj := &replpkg.AutomationProjection{}
	if base != nil {
		*proj = *base
	}
	if result != nil {
		if result.Governance != nil && strings.TrimSpace(result.Governance.Scope.AutomationID) != "" {
			proj.ID = strings.TrimSpace(result.Governance.Scope.AutomationID)
			if proj.Name == "" {
				proj.Name = proj.ID
			}
		}
		if proj.Kind == "" && strings.TrimSpace(string(result.Canonical.Source)) != "" {
			proj.Kind = strings.TrimSpace(string(result.Canonical.Source))
		}
		if summary := strings.TrimSpace(result.Canonical.Summary); summary != "" {
			proj.Health = summary
		}
	}
	if proj.Name == "" {
		proj.Name = proj.ID
	}
	if proj.Kind == "" {
		proj.Kind = "automation"
	}
	return proj
}

func mapDoctorChecks(items []checkResult) []replpkg.DoctorCheck {
	out := make([]replpkg.DoctorCheck, 0, len(items))
	for _, item := range items {
		out = append(out, replpkg.DoctorCheck{
			Category: strings.TrimSpace(item.Category),
			Name:     strings.TrimSpace(item.Name),
			Status:   strings.TrimSpace(item.Status),
			Detail:   strings.TrimSpace(item.Detail),
			Fix:      strings.TrimSpace(item.Fix),
		})
	}
	return out
}

func resetSessionByKey(ctx context.Context, client *GatewayClient, sessionKey string) error {
	sessionID, err := externalSessionIDByKey(ctx, client, sessionKey)
	if err != nil {
		return err
	}
	return client.Delete(ctx, "/runtime/sessions/"+sessionID, nil)
}

func externalSessionIDByKey(ctx context.Context, client *GatewayClient, key string) (string, error) {
	sessions, err := fetchSessionSummaries(ctx, client)
	if err != nil {
		return "", err
	}
	for _, item := range sessions {
		if item.Key == key {
			return item.ID, nil
		}
	}
	return "", fmt.Errorf("session %q not found", key)
}
