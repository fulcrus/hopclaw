package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/triage"
)

func boolPtr(v bool) *bool {
	return &v
}

func TestSubmitBuildsTaskContract(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract",
		ExternalEventID: "evt-contract",
		Content:         "整理这个季度销售数据并输出到 reports/q1-sales.xlsx",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != taskContractJobReport {
		t.Fatalf("run.TaskContract.JobType = %q", run.TaskContract.JobType)
	}
	if len(run.TaskContract.ExpectedDeliverables) == 0 {
		t.Fatal("expected deliverables in task contract")
	}
	foundSpreadsheet := false
	for _, item := range run.TaskContract.ExpectedDeliverables {
		if item.Kind == taskDeliverableSpreadsheet {
			foundSpreadsheet = true
			break
		}
	}
	if !foundSpreadsheet {
		t.Fatalf("expected spreadsheet deliverable, got %#v", run.TaskContract.ExpectedDeliverables)
	}
}

func TestBuildRunPreflightWithContractAddsDeliveryAndScheduleChecks(t *testing.T) {
	t.Parallel()

	report := buildRunPreflightWithContract("定时监控网站价格并发邮件通知", PreflightAnalysis{
		SuggestedDomains: []string{"watch", "email"},
	}, &TaskContract{
		MissingInfo: []TaskContractMissingInfo{
			{
				ID:       taskMissingInfoDeliveryTarget,
				Summary:  "The task needs an explicit recipient or channel.",
				Required: true,
			},
			{
				ID:       taskMissingInfoSchedule,
				Summary:  "The task needs a repeat schedule.",
				Required: true,
			},
		},
	})
	if report == nil {
		t.Fatal("expected preflight report")
	}
	if report.State != RunPreflightNeedsConfirmation {
		t.Fatalf("report.State = %q", report.State)
	}
	if !report.Blocking {
		t.Fatal("expected blocking preflight")
	}
	if !preflightChecksContain(report.Checks, taskMissingInfoDeliveryTarget) {
		t.Fatalf("expected delivery target check, got %#v", report.Checks)
	}
	if !preflightChecksContain(report.Checks, taskMissingInfoSchedule) {
		t.Fatalf("expected schedule check, got %#v", report.Checks)
	}
	if report.Question == "" {
		t.Fatal("expected follow-up question")
	}
	if len(report.ReplyHints) == 0 {
		t.Fatal("expected reply hints")
	}
}

func TestFinalizeTaskContractAppliesClarificationMetadata(t *testing.T) {
	t.Parallel()

	contract := finalizeTaskContract(&TaskContract{
		MissingInfo: []TaskContractMissingInfo{{
			ID:        taskMissingInfoDeliveryTarget,
			Label:     taskMissingInfoLabel(taskMissingInfoDeliveryTarget),
			InputMode: taskMissingInfoInputMode(taskMissingInfoDeliveryTarget),
			Required:  true,
		}},
	}, map[string]any{
		MetadataKeyClarificationSlots: map[string]string{
			taskMissingInfoDeliveryTarget: "发送到 Slack #ops",
		},
	})
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if len(contract.ResolvedInfo) == 0 {
		t.Fatalf("expected resolved info, got %#v", contract)
	}
	foundResolved := false
	for _, item := range contract.ResolvedInfo {
		if item.ID == taskMissingInfoDeliveryTarget && item.Value == "发送到 Slack #ops" {
			foundResolved = true
			break
		}
	}
	if !foundResolved {
		t.Fatalf("resolved info = %#v", contract.ResolvedInfo)
	}
	for _, item := range contract.MissingInfo {
		if item.ID == taskMissingInfoDeliveryTarget {
			t.Fatalf("delivery target should have been resolved, got %#v", contract.MissingInfo)
		}
	}
}

func TestSubmitTaskContractUsesAnalyzerForMissingSourceTarget(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	taskContract := &countingTaskContractAnalyzer{
		analysis: TaskContractAnalysis{
			JobType:              taskContractJobDevelopment,
			SuggestedDomains:     []string{"fs"},
			MissingInfoIDs:       []string{taskMissingInfoSourceTarget},
			MissingInfoSpecified: true,
			Confidence:           0.9,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithTaskContractAnalyzer(taskContract)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-source-target",
		ExternalEventID: "evt-contract-source-target",
		Content:         "把这个文件改一下",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	foundMissing := false
	for _, item := range run.TaskContract.MissingInfo {
		if item.ID == taskMissingInfoSourceTarget && item.Required {
			foundMissing = true
			break
		}
	}
	if !foundMissing {
		t.Fatalf("run.TaskContract.MissingInfo = %#v, want required source_target", run.TaskContract.MissingInfo)
	}
}

func TestSubmitDoesNotTreatCurrentChatReplyAsExternalDelivery(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-inline-reply",
		ExternalEventID: "evt-inline-reply",
		Content:         "Reply with exactly OK and do not use any tools.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status != RunQueued {
		t.Fatalf("run.Status = %q, want queued", run.Status)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != taskContractJobGeneral {
		t.Fatalf("run.TaskContract.JobType = %q, want %q", run.TaskContract.JobType, taskContractJobGeneral)
	}
	if run.TaskContract.RequiresExternalEffect {
		t.Fatal("expected current-chat reply to avoid external effect requirements")
	}
	for _, item := range run.TaskContract.MissingInfo {
		if item.ID == taskMissingInfoDeliveryTarget {
			t.Fatalf("unexpected delivery target missing info: %#v", run.TaskContract.MissingInfo)
		}
	}
	if run.Preflight != nil && run.Preflight.Blocking {
		t.Fatalf("run.Preflight = %#v, want non-blocking", run.Preflight)
	}
}

func TestBuildTaskContractNaturalLanguageWorkspaceEditNeedsSourceTarget(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "把这个文件改一下",
	}, nil, ExecutionModePlanned, nil, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	foundMissing := false
	for _, item := range contract.MissingInfo {
		if item.ID == taskMissingInfoSourceTarget && item.Required {
			foundMissing = true
			break
		}
	}
	if !foundMissing {
		t.Fatalf("contract.MissingInfo = %#v, want required source_target", contract.MissingInfo)
	}
}

func TestBuildTaskContractNaturalLanguageDeliveryWithoutTargetNeedsClarification(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "把结果发给相关人",
	}, nil, ExecutionModeWorkflow, nil, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType != taskContractJobDelivery {
		t.Fatalf("contract.JobType = %q, want delivery", contract.JobType)
	}
	if !contract.RequiresExternalEffect {
		t.Fatal("expected delivery to require external effect")
	}
	if !contract.RequiresApproval {
		t.Fatal("expected delivery to require approval")
	}
	foundMissing := false
	for _, item := range contract.MissingInfo {
		if item.ID == taskMissingInfoDeliveryTarget && item.Required {
			foundMissing = true
			break
		}
	}
	if !foundMissing {
		t.Fatalf("contract.MissingInfo = %#v, want required delivery_target", contract.MissingInfo)
	}
}

func TestBuildTaskContractNaturalLanguageDeploymentCountsAsDeployment(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Deploy the latest build to staging.",
	}, nil, ExecutionModeWorkflow, nil, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType != taskContractJobDeployment {
		t.Fatalf("contract.JobType = %q, want deployment", contract.JobType)
	}
	if !contract.RequiresExternalEffect {
		t.Fatal("expected deployment to require external effect")
	}
	if !contract.RequiresApproval {
		t.Fatal("expected deployment to require approval")
	}
}

func TestBuildTaskContractNaturalLanguageWriteupCountsAsReport(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Summarize it into a markdown report.",
	}, nil, ExecutionModePlanned, nil, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType != taskContractJobReport {
		t.Fatalf("contract.JobType = %q, want report", contract.JobType)
	}
	if !testTaskContractHasDeliverable(contract, taskDeliverableDocument) {
		t.Fatalf("expected document deliverable, got %#v", contract.ExpectedDeliverables)
	}
}

func TestBuildTaskContractMarkdownParserFixStaysNonReport(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Fix the markdown parser regression.",
	}, nil, ExecutionModePlanned, nil, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobReport {
		t.Fatalf("contract.JobType = %q, markdown parser fix should stay non-report", contract.JobType)
	}
	if testTaskContractHasDeliverable(contract, taskDeliverableDocument) {
		t.Fatalf("expected no document deliverable, got %#v", contract.ExpectedDeliverables)
	}
}

func TestBuildTaskContractPresentationLayerReviewStaysNonReport(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Review the presentation layer for regressions.",
	}, nil, ExecutionModePlanned, nil, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobReport {
		t.Fatalf("contract.JobType = %q, presentation layer review should stay non-report", contract.JobType)
	}
	if testTaskContractHasDeliverable(contract, taskDeliverablePresentation) {
		t.Fatalf("expected no presentation deliverable, got %#v", contract.ExpectedDeliverables)
	}
}

func TestSubmitTaskContractUsesAnalyzerForMonitorCancellation(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	taskContract := &countingTaskContractAnalyzer{
		analysis: TaskContractAnalysis{
			JobType:                taskContractJobGeneral,
			SuggestedDomains:       []string{"watch"},
			DeliverableKinds:       []string{taskDeliverableSummary},
			MissingInfoIDs:         []string{},
			MissingInfoSpecified:   true,
			RequiresExternalEffect: boolPtr(false),
			RequiresApproval:       boolPtr(false),
			Confidence:             0.9,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: ExecutionModeWatch}}).
		WithTaskContractAnalyzer(taskContract)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-watch-cancel",
		ExternalEventID: "evt-contract-watch-cancel",
		Content:         "取消这个价格监控通知",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	for _, item := range run.TaskContract.MissingInfo {
		switch item.ID {
		case taskMissingInfoDeliveryTarget, taskMissingInfoSchedule:
			t.Fatalf("unexpected missing info for cancel intent: %#v", run.TaskContract.MissingInfo)
		}
	}
}

func TestBuildTaskContractMonitorCronDoesNotAskForScheduleAgain(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Create a watch that checks https://example.com on cron 0 * * * * and reports when the page title changes.",
	}, nil, ExecutionModeWatch, &RunPreflightReport{
		SuggestedDomains: []string{"watch", "browser"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	for _, item := range contract.MissingInfo {
		if item.ID == taskMissingInfoSchedule {
			t.Fatalf("unexpected schedule missing info: %#v", contract.MissingInfo)
		}
	}
}

func TestBuildTaskContractBrowserFormDoesNotRequireMessageDelivery(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Using browser tools, open https://httpbin.org/forms/post, fill input[name=customer] as HopClaw QA, fill input[name=telephone] as 123456, fill input[name=email] as qa@example.com, fill textarea[name=comments] as browser form regression test, submit the form, and report the echoed customer name and comments.",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"net", "browser", "email", "channel", "fs"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobDelivery {
		t.Fatalf("contract.JobType = %q, want non-delivery", contract.JobType)
	}
	if !contract.RequiresExternalEffect {
		t.Fatal("expected browser form submission to require external effect verification")
	}
	if contract.RequiresApproval {
		t.Fatal("expected routine browser form submission NOT to require approval")
	}
	for _, item := range contract.ExpectedDeliverables {
		if item.Kind == taskDeliverableMessageDelivery {
			t.Fatalf("unexpected message_delivery deliverable: %#v", contract.ExpectedDeliverables)
		}
	}
	for _, item := range contract.MissingInfo {
		if item.ID == taskMissingInfoDeliveryTarget {
			t.Fatalf("unexpected delivery target missing info: %#v", contract.MissingInfo)
		}
	}
}

func TestSubmitTaskContractUsesModelAnalyzerForBrowserPurchaseExternalEffect(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"suggested_domains":["browser"],
					"deliverable_kinds":["browser_evidence"],
					"missing_info_ids":[],
					"requires_external_effect":true,
					"requires_approval":false,
					"reason":"browser purchase flow",
					"confidence":0.91
				}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-browser-purchase",
		ExternalEventID: "evt-contract-browser-purchase",
		Content:         "Go to https://shop.example.com, find the cheapest laptop, and place order for me",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if !run.TaskContract.RequiresExternalEffect {
		t.Fatal("expected browser purchase to require external effect")
	}
	// Task-contract-level approval is NOT set for browser actions.
	// Per-action approval is handled by the policy engine at tool execution time.
	if run.TaskContract.RequiresApproval {
		t.Fatal("expected browser purchase NOT to require task-level approval — policy engine handles per-tool approval")
	}
	if run.TaskContract.Source != taskContractSourceModel {
		t.Fatalf("run.TaskContract.Source = %q, want %q", run.TaskContract.Source, taskContractSourceModel)
	}
}

func TestSubmitTaskContractUsesAnalyzerForEmailDeliveryApproval(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	taskContract := &countingTaskContractAnalyzer{
		analysis: TaskContractAnalysis{
			JobType:                taskContractJobDelivery,
			SuggestedDomains:       []string{"email", "document"},
			DeliverableKinds:       []string{taskDeliverableMessageDelivery, taskDeliverableDocument},
			MissingInfoIDs:         []string{},
			MissingInfoSpecified:   true,
			RequiresExternalEffect: boolPtr(true),
			RequiresApproval:       boolPtr(true),
			Confidence:             0.93,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithTaskContractAnalyzer(taskContract)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-email-delivery-approval",
		ExternalEventID: "evt-contract-email-delivery-approval",
		Content:         "Send the weekly report to ceo@example.com via email",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if !run.TaskContract.RequiresApproval {
		t.Fatal("expected email delivery to require task-level approval")
	}
	if !run.TaskContract.RequiresExternalEffect {
		t.Fatal("expected email delivery to require external effect")
	}
	if run.TaskContract.Source != taskContractSourceModel {
		t.Fatalf("run.TaskContract.Source = %q, want %q", run.TaskContract.Source, taskContractSourceModel)
	}
}

func TestSubmitTaskContractUsesAnalyzerForNaturalLanguageDeliveryNegation(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	taskContract := &countingTaskContractAnalyzer{
		analysis: TaskContractAnalysis{
			JobType:                taskContractJobReport,
			SuggestedDomains:       []string{"document"},
			DeliverableKinds:       []string{taskDeliverableDocument},
			MissingInfoIDs:         []string{},
			MissingInfoSpecified:   true,
			RequiresExternalEffect: boolPtr(false),
			RequiresApproval:       boolPtr(false),
			Confidence:             0.92,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithTaskContractAnalyzer(taskContract)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-delivery-negation",
		ExternalEventID: "evt-contract-delivery-negation",
		Content:         "请把周报准备好，但先不要发给 ceo@example.com。",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType == taskContractJobDelivery {
		t.Fatalf("run.TaskContract.JobType = %q, want non-delivery", run.TaskContract.JobType)
	}
	if run.TaskContract.RequiresExternalEffect {
		t.Fatal("expected natural-language negation to stay analyzer-owned without external effect")
	}
	if run.TaskContract.RequiresApproval {
		t.Fatal("expected natural-language negation to avoid task-level approval")
	}
	for _, item := range run.TaskContract.MissingInfo {
		if item.ID == taskMissingInfoDeliveryTarget {
			t.Fatalf("unexpected delivery target missing info: %#v", run.TaskContract.MissingInfo)
		}
	}
}

func TestBuildTaskContractMonitorDoesNotRequireApproval(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Watch https://example.com on cron 0 * * * * and tell me when the homepage title changes",
	}, nil, ExecutionModeWatch, &RunPreflightReport{
		SuggestedDomains: []string{"watch", "browser"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if !contract.RequiresExternalEffect {
		t.Fatal("expected monitor to require external effect")
	}
	if contract.RequiresApproval {
		t.Fatal("expected monitor NOT to require approval — watch flow handles its own confirmation")
	}
}

func TestBuildTaskContractWatchWithoutScheduleRequestsSchedule(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Vigila https://example.com y avísame aquí cuando cambie el título.",
	}, nil, ExecutionModeWatch, &RunPreflightReport{
		SuggestedDomains: []string{"watch", "browser"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	foundScheduleMissing := false
	for _, item := range contract.MissingInfo {
		if item.ID == taskMissingInfoSchedule {
			foundScheduleMissing = true
			break
		}
	}
	if !foundScheduleMissing {
		t.Fatalf("expected schedule missing info, got %#v", contract.MissingInfo)
	}
}

func TestSubmitTaskContractUsesAnalyzerForNaturalLanguageChineseWatchSchedule(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	taskContract := &countingTaskContractAnalyzer{
		analysis: TaskContractAnalysis{
			JobType:                taskContractJobMonitor,
			SuggestedDomains:       []string{"watch", "browser"},
			DeliverableKinds:       []string{taskDeliverableWatchAlert, taskDeliverableBrowserEvidence},
			MissingInfoIDs:         []string{},
			MissingInfoSpecified:   true,
			RequiresExternalEffect: boolPtr(true),
			RequiresApproval:       boolPtr(false),
			Confidence:             0.94,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithTaskContractAnalyzer(taskContract)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-zh-watch-schedule",
		ExternalEventID: "evt-contract-zh-watch-schedule",
		Content:         "从现在开始每小时检查 https://example.com，有变化就在当前会话通知我",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != taskContractJobMonitor {
		t.Fatalf("run.TaskContract.JobType = %q, want %q", run.TaskContract.JobType, taskContractJobMonitor)
	}
	for _, item := range run.TaskContract.MissingInfo {
		if item.ID == taskMissingInfoSchedule {
			t.Fatalf("unexpected schedule missing info: %#v", run.TaskContract.MissingInfo)
		}
	}
	if run.TaskContract.RequiresApproval {
		t.Fatal("expected analyzer-driven watch schedule to avoid task-level approval")
	}
}

func TestBuildTaskContractWatchWithISOScheduleDoesNotAskAgain(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Vigila https://example.com 2026-03-24 09:30 y avísame aquí cuando cambie el título.",
	}, nil, ExecutionModeWatch, &RunPreflightReport{
		SuggestedDomains: []string{"watch", "browser"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	for _, item := range contract.MissingInfo {
		if item.ID == taskMissingInfoSchedule {
			t.Fatalf("unexpected schedule missing info: %#v", contract.MissingInfo)
		}
	}
}

func TestBuildTaskContractCronDomainWithoutScheduleRequestsSchedule(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Prepare the recurring ops digest.",
	}, nil, ExecutionModeWorkflow, &RunPreflightReport{
		SuggestedDomains: []string{"cron", "document"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	foundScheduleMissing := false
	for _, item := range contract.MissingInfo {
		if item.ID == taskMissingInfoSchedule {
			foundScheduleMissing = true
			break
		}
	}
	if !foundScheduleMissing {
		t.Fatalf("expected schedule missing info from cron semantic domain, got %#v", contract.MissingInfo)
	}
}

func TestBuildTaskContractWatchNotifyMeDefaultsToCurrentChat(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Watch https://example.com on cron 0 * * * * and notify me when the homepage title changes.",
	}, nil, ExecutionModeWatch, &RunPreflightReport{
		SuggestedDomains: []string{"watch", "browser"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	for _, item := range contract.MissingInfo {
		if item.ID == taskMissingInfoDeliveryTarget {
			t.Fatalf("unexpected delivery target missing info: %#v", contract.MissingInfo)
		}
	}
}

func TestSubmitTaskContractUsesModelAnalyzerForNotificationDelivery(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"delivery",
					"suggested_domains":["email"],
					"deliverable_kinds":["message_delivery"],
					"missing_info_ids":[],
					"requires_external_effect":true,
					"requires_approval":true,
					"reason":"notification delivery",
					"confidence":0.9
				}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-notify-delivery",
		ExternalEventID: "evt-contract-notify-delivery",
		Content:         "Notify ceo@example.com when the report is ready.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != taskContractJobDelivery {
		t.Fatalf("run.TaskContract.JobType = %q, want delivery", run.TaskContract.JobType)
	}
	if !run.TaskContract.RequiresExternalEffect {
		t.Fatal("expected notification delivery to require external effect")
	}
	if !run.TaskContract.RequiresApproval {
		t.Fatal("expected notification delivery to require approval")
	}
	if run.TaskContract.Source != taskContractSourceModel {
		t.Fatalf("run.TaskContract.Source = %q, want %q", run.TaskContract.Source, taskContractSourceModel)
	}
}

func TestBuildTaskContractSemanticEmailTargetInWorkflowCountsAsDelivery(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Informe semanal para ceo@example.com.",
	}, nil, ExecutionModeWorkflow, &RunPreflightReport{
		SuggestedDomains: []string{"email", "document"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType != taskContractJobDelivery {
		t.Fatalf("contract.JobType = %q, want delivery", contract.JobType)
	}
}

func TestBuildTaskContractBrowserSubmissionWithEmailFieldStaysNonDeliveryEvenInWorkflow(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Using browser tools, open https://httpbin.org/forms/post, fill input[name=email] as qa@example.com, submit the form, and show me the confirmation.",
	}, nil, ExecutionModeWorkflow, &RunPreflightReport{
		SuggestedDomains: []string{"browser", "email"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobDelivery {
		t.Fatalf("contract.JobType = %q, browser form submission must stay non-delivery", contract.JobType)
	}
}

func TestBuildTaskContractBrowserKeepPageOpenDoesNotBecomeMonitor(t *testing.T) {
	t.Parallel()

	contract := buildHeuristicTaskContract(IncomingMessage{
		Content: "打开 https://example.com 并保持在当前页面，不要继续分析。",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"browser", "watch"},
	}, nil, &SemanticSignal{
		SuggestedDomains:   []string{"browser", "watch"},
		BrowserContextOnly: true,
	})
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobMonitor {
		t.Fatalf("contract.JobType = %q, keep-page browser task must not become monitor", contract.JobType)
	}
	if containsTestString(contract.SuggestedDomains, string(DomainWatch)) {
		t.Fatalf("contract.SuggestedDomains = %#v, keep-page browser task must not keep watch domain", contract.SuggestedDomains)
	}
	for _, item := range contract.MissingInfo {
		if item.ID == taskMissingInfoSchedule {
			t.Fatalf("unexpected schedule missing info: %#v", contract.MissingInfo)
		}
	}
	for _, item := range contract.ExpectedDeliverables {
		if item.Kind == taskDeliverableWatchAlert {
			t.Fatalf("unexpected watch_alert deliverable: %#v", contract.ExpectedDeliverables)
		}
	}
	if contract.RequiresExternalEffect {
		t.Fatal("keep-page browser task should not require external effect")
	}
}

func TestBuildTaskContractStructuredCalendarScheduleCountsAsAutomation(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Programar informe 2026-03-24 08:00",
	}, nil, ExecutionModeWorkflow, &RunPreflightReport{
		SuggestedDomains: []string{"calendar", "document"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType != taskContractJobAutomation {
		t.Fatalf("contract.JobType = %q, want automation", contract.JobType)
	}
}

func TestBuildTaskContractStructuredScheduleFlagsCountAsAutomation(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: `hopclaw automation create --schedule-kind every --every 1h --content "Run the ops digest."`,
	}, nil, ExecutionModeWorkflow, &RunPreflightReport{
		SuggestedDomains: []string{"cron", "document"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType != taskContractJobAutomation {
		t.Fatalf("contract.JobType = %q, want automation", contract.JobType)
	}
	for _, item := range contract.MissingInfo {
		if item.ID == taskMissingInfoSchedule {
			t.Fatalf("unexpected schedule missing info: %#v", contract.MissingInfo)
		}
	}
}

func TestBuildTaskContractReadOnlyGitReviewDoesNotBecomeDevelopment(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Review git diff in cmd/server for risky changes.",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"git", "fs"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobDevelopment {
		t.Fatalf("contract.JobType = %q, read-only git review should stay non-development", contract.JobType)
	}
}

func TestSubmitTaskContractUsesAnalyzerForNaturalLanguageDeploymentMissingTarget(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	taskContract := &countingTaskContractAnalyzer{
		analysis: TaskContractAnalysis{
			JobType:                taskContractJobDeployment,
			SuggestedDomains:       []string{"exec"},
			DeliverableKinds:       []string{taskDeliverableDeployment},
			MissingInfoIDs:         []string{taskMissingInfoDeploymentScope},
			MissingInfoSpecified:   true,
			RequiresExternalEffect: boolPtr(true),
			RequiresApproval:       boolPtr(true),
			Confidence:             0.94,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithTaskContractAnalyzer(taskContract)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-deploy-missing-target",
		ExternalEventID: "evt-contract-deploy-missing-target",
		Content:         "Deploy the latest build.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != taskContractJobDeployment {
		t.Fatalf("contract.JobType = %q, want deployment", run.TaskContract.JobType)
	}
	if !run.TaskContract.RequiresApproval {
		t.Fatal("deployment should require approval")
	}
	if !run.TaskContract.RequiresExternalEffect {
		t.Fatal("deployment should require external effect")
	}
	found := false
	for _, item := range run.TaskContract.MissingInfo {
		if item.ID == taskMissingInfoDeploymentScope {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected deployment target missing info, got %#v", run.TaskContract.MissingInfo)
	}
}

func TestBuildTaskContractReleaseNotesStayNonDeployment(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Write release notes for version 1.2.3 in docs/release-notes.md",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"fs", "document"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobDeployment {
		t.Fatalf("contract.JobType = %q, release notes should stay non-deployment", contract.JobType)
	}
	if contract.RequiresApproval {
		t.Fatal("release notes should not require approval")
	}
	if contract.RequiresExternalEffect {
		t.Fatal("release notes should not require external effect")
	}
}

func TestBuildTaskContractPublishLocalDocumentStaysNonDeployment(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Publish the project summary into docs/summary.md",
	}, nil, ExecutionModeWorkflow, &RunPreflightReport{
		SuggestedDomains: []string{"fs", "document"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobDeployment {
		t.Fatalf("contract.JobType = %q, local document publish should stay non-deployment", contract.JobType)
	}
	if contract.RequiresApproval {
		t.Fatal("local document publish should not require approval")
	}
	if contract.RequiresExternalEffect {
		t.Fatal("local document publish should not require external effect")
	}
}

func TestBuildTaskContractStructuredSummaryCountsAsReport(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Collect the README guidance into docs/new-teammates-summary.md",
	}, nil, ExecutionModeDirect, &RunPreflightReport{
		SuggestedDomains: []string{"document", "fs"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType != taskContractJobReport {
		t.Fatalf("contract.JobType = %q, want report", contract.JobType)
	}
}

func TestBuildTaskContractNewsReportPrefersDigestCapabilityHint(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Collect today's top news into docs/daily-news-brief.md",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"news", "search", "document"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if len(contract.CapabilityHints) != 1 || contract.CapabilityHints[0] != "news.digest" {
		t.Fatalf("contract.CapabilityHints = %#v, want [news.digest]", contract.CapabilityHints)
	}
}

func TestBuildTaskContractAnalyzeAndFixPrefersDevelopmentOverResearch(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Analyze the failing test and fix it.",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"git", "fs", "exec"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType != taskContractJobDevelopment {
		t.Fatalf("contract.JobType = %q, want development", contract.JobType)
	}
}

func TestBuildTaskContractAnalyzeExistingCSVDoesNotBecomeReport(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Analyze /tmp/report.csv for anomalies and explain the top issues.",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"fs"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobReport {
		t.Fatalf("contract.JobType = %q, reading an existing csv should stay non-report", contract.JobType)
	}
}

func TestSubmitTaskContractUsesModelAnalyzerForSpanishDelivery(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"delivery",
					"deliverable_kinds":["message_delivery"],
					"missing_info_ids":[],
					"requires_external_effect":true,
					"requires_approval":true,
					"reason":"Spanish email delivery request",
					"confidence":0.94
				}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-es-delivery",
		ExternalEventID: "evt-contract-es-delivery",
		Content:         "Envía el informe semanal a ceo@example.com por correo electrónico.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != taskContractJobDelivery {
		t.Fatalf("run.TaskContract.JobType = %q, want %q", run.TaskContract.JobType, taskContractJobDelivery)
	}
	if !run.TaskContract.RequiresExternalEffect {
		t.Fatal("expected external effect for Spanish delivery request")
	}
	if !run.TaskContract.RequiresApproval {
		t.Fatal("expected task-level approval for Spanish delivery request")
	}
	if run.TaskContract.Source != taskContractSourceModel {
		t.Fatalf("run.TaskContract.Source = %q, want %q", run.TaskContract.Source, taskContractSourceModel)
	}
	if !testTaskContractHasDeliverable(run.TaskContract, taskDeliverableMessageDelivery) {
		t.Fatalf("expected message delivery deliverable, got %#v", run.TaskContract.ExpectedDeliverables)
	}
	for _, item := range run.TaskContract.MissingInfo {
		if item.ID == taskMissingInfoDeliveryTarget {
			t.Fatalf("unexpected delivery target missing info: %#v", run.TaskContract.MissingInfo)
		}
	}
}

func TestSubmitTaskContractAnalyzerRequestUsesStructuredSeedOnly(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: `{}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(&staticRunTriage{
			decision: triage.RunDecision{
				ExecutionMode:    "planned",
				SuggestedDomains: []string{"browser"},
			},
		}).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	_, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-structured-seed-only",
		ExternalEventID: "evt-contract-structured-seed-only",
		Content:         "Resume https://example.com en docs/tmp/resumen.md",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if len(model.lastRequest.Messages) != 1 {
		t.Fatalf("lastRequest.Messages = %#v", model.lastRequest.Messages)
	}
	var req TaskContractAnalysisRequest
	if err := json.Unmarshal([]byte(model.lastRequest.Messages[0].Content), &req); err != nil {
		t.Fatalf("unmarshal task contract analyzer request: %v", err)
	}
	if len(req.SuggestedDomains) != 1 || req.SuggestedDomains[0] != "browser" {
		t.Fatalf("req.SuggestedDomains = %#v, want only upstream structured domains", req.SuggestedDomains)
	}
	if req.SemanticSignal == nil {
		t.Fatal("expected semantic signal on task contract request")
	}
	if req.SemanticSignal.Language.Family != "es" {
		t.Fatalf("req.SemanticSignal.Language.Family = %q, want es", req.SemanticSignal.Language.Family)
	}
	if req.SemanticSignal.Language.Script != "Latn" {
		t.Fatalf("req.SemanticSignal.Language.Script = %q, want Latn", req.SemanticSignal.Language.Script)
	}
	if !req.SemanticSignal.Language.MainSemanticPath {
		t.Fatalf("req.SemanticSignal.Language.MainSemanticPath = %v, want true", req.SemanticSignal.Language.MainSemanticPath)
	}
	if len(req.SemanticSignal.SuggestedDomains) != 1 || req.SemanticSignal.SuggestedDomains[0] != "browser" {
		t.Fatalf("req.SemanticSignal.SuggestedDomains = %#v, want [browser]", req.SemanticSignal.SuggestedDomains)
	}
	if !req.SemanticSignal.TriageReady {
		t.Fatalf("req.SemanticSignal.TriageReady = %v, want true before task-contract analysis", req.SemanticSignal.TriageReady)
	}
	if req.SemanticSignal.TaskContractReady {
		t.Fatalf("req.SemanticSignal.TaskContractReady = %v, want false before task-contract analysis", req.SemanticSignal.TaskContractReady)
	}
}

func TestSubmitTaskContractAnalyzerDeliverableDrivesApprovalWithoutVerbReplay(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"suggested_domains":["email","document"],
					"deliverable_kinds":["message_delivery"],
					"missing_info_ids":[],
					"reason":"semantic delivery to a mailbox recipient",
					"confidence":0.88
				}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-deliverable-driven",
		ExternalEventID: "evt-contract-deliverable-driven",
		Content:         "Entregar el informe semanal a ceo@example.com.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if !run.TaskContract.RequiresExternalEffect {
		t.Fatal("expected deliverable-driven external effect")
	}
	if !run.TaskContract.RequiresApproval {
		t.Fatal("expected deliverable-driven approval")
	}
	if !testTaskContractHasDeliverable(run.TaskContract, taskDeliverableMessageDelivery) {
		t.Fatalf("expected message delivery deliverable, got %#v", run.TaskContract.ExpectedDeliverables)
	}
}

func TestSubmitTaskContractUsesModelAnalyzerForSpanishMonitor(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"monitor",
					"suggested_domains":["watch","browser"],
					"deliverable_kinds":["watch_alert"],
					"missing_info_ids":[],
					"requires_external_effect":true,
					"requires_approval":false,
					"reason":"Spanish recurring watch request",
					"confidence":0.89
				}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-es-monitor",
		ExternalEventID: "evt-contract-es-monitor",
		Content:         "Vigila https://example.com cada hora y avísame aquí cuando cambie el título.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != taskContractJobMonitor {
		t.Fatalf("run.TaskContract.JobType = %q, want %q", run.TaskContract.JobType, taskContractJobMonitor)
	}
	if !run.TaskContract.RequiresExternalEffect {
		t.Fatal("expected external effect for recurring Spanish monitor request")
	}
	if run.TaskContract.RequiresApproval {
		t.Fatal("expected monitor request to avoid task-level approval")
	}
	if !containsTestString(run.TaskContract.SuggestedDomains, string(DomainWatch)) {
		t.Fatalf("run.TaskContract.SuggestedDomains = %#v, want watch", run.TaskContract.SuggestedDomains)
	}
	if !testTaskContractHasDeliverable(run.TaskContract, taskDeliverableWatchAlert) {
		t.Fatalf("expected watch alert deliverable, got %#v", run.TaskContract.ExpectedDeliverables)
	}
	for _, item := range run.TaskContract.MissingInfo {
		if item.ID == taskMissingInfoSchedule {
			t.Fatalf("unexpected schedule missing info: %#v", run.TaskContract.MissingInfo)
		}
	}
}

func TestSubmitTaskContractPreservesAnalyzerCapabilityHints(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"general",
					"suggested_domains":["text"],
					"capability_hints":["translate.run"],
					"deliverable_kinds":["summary"],
					"missing_info_ids":[],
					"requires_external_effect":false,
					"requires_approval":false,
					"confidence":0.92
				}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-capability-hints",
		ExternalEventID: "evt-contract-capability-hints",
		Content:         "Traduce este párrafo al inglés.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.Source != taskContractSourceModel {
		t.Fatalf("run.TaskContract.Source = %q, want %q", run.TaskContract.Source, taskContractSourceModel)
	}
	if len(run.TaskContract.CapabilityHints) != 1 || run.TaskContract.CapabilityHints[0] != "translate.run" {
		t.Fatalf("run.TaskContract.CapabilityHints = %#v, want [translate.run]", run.TaskContract.CapabilityHints)
	}
}

func TestSubmitTaskContractTreatsAnalyzerDeliverablesAsAuthoritative(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"general",
					"suggested_domains":["browser"],
					"deliverable_kinds":["browser_evidence"],
					"missing_info_ids":[],
					"requires_external_effect":false,
					"requires_approval":false,
					"confidence":0.9
				}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-authoritative-deliverables",
		ExternalEventID: "evt-contract-authoritative-deliverables",
		Content:         "Resume esta página en docs/tmp/resumen.md",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if !testTaskContractHasDeliverable(run.TaskContract, taskDeliverableBrowserEvidence) {
		t.Fatalf("expected browser_evidence deliverable, got %#v", run.TaskContract.ExpectedDeliverables)
	}
	if testTaskContractHasDeliverable(run.TaskContract, taskDeliverableDocument) {
		t.Fatalf("expected analyzer deliverables to stay authoritative, got %#v", run.TaskContract.ExpectedDeliverables)
	}
}

func TestSubmitTaskContractSanitizesModelAnalyzerForBrowserKeepPageOpen(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"monitor",
					"suggested_domains":["browser","watch"],
					"deliverable_kinds":["browser_evidence","watch_alert"],
					"missing_info_ids":["schedule"],
					"browser_context_only":true,
					"requires_external_effect":true,
					"requires_approval":false,
					"reason":"Misread keep-page request as monitoring",
					"confidence":0.88
				}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-keep-page",
		ExternalEventID: "evt-contract-keep-page",
		Content:         "打开 https://example.com 并保持在当前页面，不要继续分析。",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType == taskContractJobMonitor {
		t.Fatalf("run.TaskContract.JobType = %q, keep-page browser task must not become monitor", run.TaskContract.JobType)
	}
	if containsTestString(run.TaskContract.SuggestedDomains, string(DomainWatch)) {
		t.Fatalf("run.TaskContract.SuggestedDomains = %#v, want watch removed", run.TaskContract.SuggestedDomains)
	}
	for _, item := range run.TaskContract.MissingInfo {
		if item.ID == taskMissingInfoSchedule {
			t.Fatalf("unexpected schedule missing info: %#v", run.TaskContract.MissingInfo)
		}
	}
	if taskContractHasDeliverable(run.TaskContract, taskDeliverableWatchAlert) {
		t.Fatalf("unexpected watch_alert deliverable: %#v", run.TaskContract.ExpectedDeliverables)
	}
	if run.TaskContract.RequiresExternalEffect {
		t.Fatal("keep-page browser task should not require external effect")
	}
}

func TestSubmitTaskContractFallsBackWhenAnalyzerReturnsNoSemanticDecision(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: `{}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-contract-fallback",
		ExternalEventID: "evt-contract-fallback",
		Content:         "整理这个季度销售数据并输出到 reports/q1-sales.xlsx",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != taskContractJobReport {
		t.Fatalf("run.TaskContract.JobType = %q, want %q", run.TaskContract.JobType, taskContractJobReport)
	}
	if run.TaskContract.Source != taskContractSourceHeuristic {
		t.Fatalf("run.TaskContract.Source = %q, want heuristic fallback", run.TaskContract.Source)
	}
	if !testTaskContractHasDeliverable(run.TaskContract, taskDeliverableSpreadsheet) {
		t.Fatalf("expected spreadsheet deliverable, got %#v", run.TaskContract.ExpectedDeliverables)
	}
}

func testTaskContractHasDeliverable(contract *TaskContract, kind string) bool {
	if contract == nil {
		return false
	}
	for _, item := range contract.ExpectedDeliverables {
		if item.Kind == kind {
			return true
		}
	}
	return false
}

func taskContractHasDeliverable(contract *TaskContract, kind string) bool {
	return testTaskContractHasDeliverable(contract, kind)
}
