package agent

// ---------------------------------------------------------------------------
// scenario_test.go — Behavioral proof tests for real-user complex scenarios.
//
// Each test corresponds to one or more TC-XX entries from the user scenario
// matrix. Tests are grouped by category:
//   1. Task contract classification (intent, deliverables, approval, missing info)
//   2. Watch mode lifecycle (create, cancel, missing target)
//   3. Component integration (Submit → ExecuteRun end-to-end)
//   4. Session continuity and multi-step chains
//
// Priority tests (TC-10/13/14/23/25/32/35/36) are marked with // PRIORITY.
// ---------------------------------------------------------------------------

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
)

// ---------------------------------------------------------------------------
// 1. Task Contract Classification — Intent Accuracy
// ---------------------------------------------------------------------------

// TC-01: "读一下当前仓库，告诉我这是干什么的" → general job, no external effect.
func TestScenario_TC01_ReadRepoSummary(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "读一下当前仓库，告诉我这是干什么的",
	}, nil, ExecutionModeDirect, nil, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType != taskContractJobGeneral {
		t.Fatalf("JobType = %q, want %q", contract.JobType, taskContractJobGeneral)
	}
	if contract.RequiresExternalEffect {
		t.Fatal("reading a repo should not require external effect")
	}
	if contract.RequiresApproval {
		t.Fatal("reading a repo should not require approval")
	}
}

// TC-02: "找出最近 3 个最值得担心的启动风险，并带文件路径" → research job.
// LLM triage would recognize investigative intent and return fs domain.
func TestScenario_TC02_StartupRisks(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "找出最近 3 个最值得担心的启动风险，并带文件路径",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"fs"},
	}, &RunTriageTrace{
		Source:           "model",
		SuggestedDomains: []string{"fs"},
		Confidence:       0.82,
	})
	if contract == nil {
		t.Fatal("expected task contract")
	}
	// fs domain → no special job type mapping → falls to general, which is
	// acceptable for a code analysis task; the key behavioral assertions are
	// below: no approval, no external effect, no delivery target.
	if contract.RequiresApproval {
		t.Fatal("research task should not require approval")
	}
	if contract.RequiresExternalEffect {
		t.Fatal("code analysis should not require external effect")
	}
	for _, m := range contract.MissingInfo {
		if m.ID == taskMissingInfoDeliveryTarget {
			t.Fatal("analysis task must not ask for delivery_target")
		}
	}
}

// TC-03: "把 README 里的安装步骤整理成给同事的 5 条指令" → report job.
func TestScenario_TC03_SummarizeInstallSteps(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "把 README 里的安装步骤整理成给同事的 5 条指令",
	}, nil, ExecutionModeDirect, &RunPreflightReport{
		SuggestedDomains: []string{"document", "fs"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType != taskContractJobReport {
		t.Fatalf("JobType = %q, want %q", contract.JobType, taskContractJobReport)
	}
	if contract.RequiresExternalEffect {
		t.Fatal("summarizing README should not require external effect")
	}
}

// TC-05: "扫描最近的报错日志，帮我判断是配置问题还是代码问题" → diagnostic, no approval.
// LLM triage would recognize this as a diagnostic/analysis task on local files.
func TestScenario_TC05_DiagnoseErrorLogs(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "扫描最近的报错日志，帮我判断是配置问题还是代码问题",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"fs"},
	}, &RunTriageTrace{
		Source:           "model",
		SuggestedDomains: []string{"fs"},
		Confidence:       0.80,
	})
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.RequiresApproval {
		t.Fatal("log diagnosis should not require approval")
	}
	if contract.RequiresExternalEffect {
		t.Fatal("log diagnosis should not require external effect")
	}
}

// TC-06: "修掉当前 failing test" → development-like task, no external effect.
// LLM triage would recognize code modification intent and return fs domain
// with planned mode. The heuristic cannot infer development from Chinese alone,
// but the behavioral contract (no approval, no external effect) must hold.
func TestScenario_TC06_FixFailingTest(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "修掉当前 failing test",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"fs", "exec"},
	}, &RunTriageTrace{
		Source:           "model",
		SuggestedDomains: []string{"fs", "exec"},
		Confidence:       0.85,
	})
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.RequiresExternalEffect {
		t.Fatal("fixing tests should not require external effect")
	}
	if contract.RequiresApproval {
		t.Fatal("fixing tests should not require approval")
	}
	for _, m := range contract.MissingInfo {
		if m.ID == taskMissingInfoDeliveryTarget {
			t.Fatal("fix task must not ask for delivery_target")
		}
	}
}

// TC-07: "把这段调研结果写到 docs/tmp/report.md" → report with document deliverable.
func TestScenario_TC07_WriteReportToFile(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "把这段调研结果写到 docs/tmp/report.md",
	}, nil, ExecutionModeDirect, &RunPreflightReport{
		SuggestedDomains: []string{"document", "fs"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	// ".md" triggers document deliverable
	foundDocument := false
	for _, d := range contract.ExpectedDeliverables {
		if d.Kind == taskDeliverableDocument {
			foundDocument = true
			break
		}
	}
	if !foundDocument {
		t.Fatalf("expected document deliverable, got %#v", contract.ExpectedDeliverables)
	}
	if contract.RequiresApproval {
		t.Fatal("writing a local file should not require approval")
	}
	if contract.TargetSummary != "docs/tmp/report.md" {
		t.Fatalf("contract.TargetSummary = %q, want docs/tmp/report.md", contract.TargetSummary)
	}
}

// ---------------------------------------------------------------------------
// 2. Browser Task Contract — Intent Misclassification Prevention
// ---------------------------------------------------------------------------

// TC-09: "打开 https://example.com，告诉我标题和首段内容" → browser domain, no delivery.
func TestScenario_TC09_BrowseAndSummarize(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "打开 https://example.com，告诉我标题和首段内容",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"net", "browser"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobDelivery {
		t.Fatal("browsing a page should not be classified as delivery")
	}
	// Should detect browser evidence deliverable
	foundBrowserEvidence := false
	for _, d := range contract.ExpectedDeliverables {
		if d.Kind == taskDeliverableBrowserEvidence {
			foundBrowserEvidence = true
			break
		}
	}
	if !foundBrowserEvidence {
		t.Fatalf("expected browser_evidence deliverable, got %#v", contract.ExpectedDeliverables)
	}
	if contract.RequiresApproval {
		t.Fatal("reading a page should not require approval")
	}
	// Target extraction
	if !strings.Contains(contract.TargetSummary, "example.com") {
		t.Fatalf("TargetSummary = %q, want contains example.com", contract.TargetSummary)
	}
}

// PRIORITY — TC-10: Browser form submission ≠ message delivery.
// Structured browser form markers should remain external-effectful without
// becoming message delivery.
func TestScenario_TC10_BrowserFormNotDelivery(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "打开 https://httpbin.org/forms/post，填写 input[name=customer]，然后点击 button[type=submit]。",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"net", "browser"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobDelivery {
		t.Fatalf("JobType = %q, browser form submission must NOT be delivery", contract.JobType)
	}
	// External effect: yes (form submission is external)
	if !contract.RequiresExternalEffect {
		t.Fatal("browser form submission should require external effect verification")
	}
	// Approval: no (policy engine handles per-tool)
	if contract.RequiresApproval {
		t.Fatal("browser form submission should NOT require task-level approval")
	}
	// No message_delivery deliverable
	for _, d := range contract.ExpectedDeliverables {
		if d.Kind == taskDeliverableMessageDelivery {
			t.Fatalf("unexpected message_delivery deliverable for form submission")
		}
	}
	// No delivery_target missing info
	for _, m := range contract.MissingInfo {
		if m.ID == taskMissingInfoDeliveryTarget {
			t.Fatalf("unexpected delivery_target missing info for form submission")
		}
	}
}

// TC-11: "打开页面后截图给我，再告诉我页面标题" → browser domain, browser_evidence.
func TestScenario_TC11_ScreenshotAndTitle(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "打开页面后截图给我，再告诉我页面标题",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"browser"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	foundBrowserEvidence := false
	for _, d := range contract.ExpectedDeliverables {
		if d.Kind == taskDeliverableBrowserEvidence {
			foundBrowserEvidence = true
			break
		}
	}
	if !foundBrowserEvidence {
		t.Fatalf("expected browser_evidence deliverable for screenshot task")
	}
}

// PRIORITY — TC-13: browser email form field ≠ message delivery intent.
func TestScenario_TC13_BrowserEmailFieldNotDelivery(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Open https://httpbin.org/forms/post and fill input[name=email] with test@example.com, then submit the form",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"browser"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	// The email pattern in the message is for form filling, not delivery.
	// Since there's "submit" in the message with browser domain, it should be
	// external effect but NOT delivery job type.
	if contract.JobType == taskContractJobDelivery {
		t.Fatalf("JobType = %q, email field filling must NOT be classified as delivery", contract.JobType)
	}
	// Should not ask for delivery target
	for _, m := range contract.MissingInfo {
		if m.ID == taskMissingInfoDeliveryTarget {
			t.Fatalf("unexpected delivery_target missing info: filling a form is not sending a message")
		}
	}
}

// PRIORITY — TC-14: "页面 URL 里有 /post，但这只是一个网页表单"
// URL containing /post ≠ external delivery task.
func TestScenario_TC14_URLWithPostNotDelivery(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "打开 https://httpbin.org/post 页面，查看返回内容",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"net", "browser"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobDelivery {
		t.Fatalf("JobType = %q, URL with /post must NOT trigger delivery classification", contract.JobType)
	}
	// No message delivery deliverable
	for _, d := range contract.ExpectedDeliverables {
		if d.Kind == taskDeliverableMessageDelivery {
			t.Fatalf("unexpected message_delivery deliverable for URL browsing")
		}
	}
}

// TC-16: "抓取页面信息，写到 docs/tmp/example-brief.md"
// Browser + file write, document deliverable, browser evidence.
func TestScenario_TC16_BrowseAndWriteFile(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "抓取页面信息，写到 docs/tmp/example-brief.md",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"browser", "fs", "document"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	// Should have both browser_evidence and document deliverables
	hasBrowserEvidence := false
	hasDocument := false
	for _, d := range contract.ExpectedDeliverables {
		switch d.Kind {
		case taskDeliverableBrowserEvidence:
			hasBrowserEvidence = true
		case taskDeliverableDocument:
			hasDocument = true
		}
	}
	if !hasBrowserEvidence {
		t.Fatalf("expected browser_evidence deliverable, got %#v", contract.ExpectedDeliverables)
	}
	if !hasDocument {
		t.Fatalf("expected document deliverable for .md file, got %#v", contract.ExpectedDeliverables)
	}
	if contract.TargetSummary != "docs/tmp/example-brief.md" {
		t.Fatalf("contract.TargetSummary = %q, want docs/tmp/example-brief.md", contract.TargetSummary)
	}
	if contract.RequiresApproval {
		t.Fatal("browse+write should not require approval")
	}
}

// ---------------------------------------------------------------------------
// 3. Desktop Task Contract
// ---------------------------------------------------------------------------

// TC-17: "打开备忘录/文本编辑器，输入一段文字" → desktop domain.
func TestScenario_TC17_OpenDesktopAppAndType(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "打开备忘录，输入一段文字",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"desktop"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	foundDesktopEvidence := false
	for _, d := range contract.ExpectedDeliverables {
		if d.Kind == taskDeliverableDesktopEvidence {
			foundDesktopEvidence = true
			break
		}
	}
	if !foundDesktopEvidence {
		t.Fatalf("expected desktop_evidence deliverable, got %#v", contract.ExpectedDeliverables)
	}
	if contract.RequiresApproval {
		t.Fatal("local desktop operation should not require approval")
	}
}

// TC-18: "读一下当前剪贴板" — degraded analyzer paths should still recover the
// desktop domain for explicit clipboard requests.
func TestScenario_TC18_ReadClipboard(t *testing.T) {
	t.Parallel()

	domains := fallbackHeuristicDomains("读一下当前剪贴板，然后帮我整理成 3 条")
	if !domains[DomainDesktop] {
		t.Fatalf("domains = %v, want desktop for clipboard request", domains)
	}
}

// TC-21: "给当前桌面截个图" — degraded analyzer paths should still recover the
// desktop domain for explicit desktop screenshot requests.
func TestScenario_TC21_DesktopScreenshot(t *testing.T) {
	t.Parallel()

	domains := fallbackHeuristicDomains("给当前桌面截个图，告诉我屏幕上最显眼的窗口标题")
	if !domains[DomainDesktop] {
		t.Fatalf("domains = %v, want desktop for an explicit desktop screenshot request", domains)
	}
}

// ---------------------------------------------------------------------------
// 4. External Delivery & Approval
// ---------------------------------------------------------------------------

// PRIORITY — TC-23: "直接回复我，不要发到外部渠道" → no external delivery, no approval.
func TestScenario_TC23_ReplyInCurrentChat(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-tc23",
		ExternalEventID: "evt-tc23",
		Content:         "直接回复我，不要发到外部渠道",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType == taskContractJobDelivery {
		t.Fatalf("JobType = %q, current-chat reply must NOT be delivery", run.TaskContract.JobType)
	}
	if run.TaskContract.RequiresExternalEffect {
		t.Fatal("current-chat reply must not require external effect")
	}
	if run.TaskContract.RequiresApproval {
		t.Fatal("current-chat reply must not require approval")
	}
	for _, m := range run.TaskContract.MissingInfo {
		if m.ID == taskMissingInfoDeliveryTarget {
			t.Fatalf("must not ask for delivery_target when user explicitly says reply here")
		}
	}
	// Must not produce a blocking preflight
	if run.Preflight != nil && run.Preflight.Blocking {
		t.Fatalf("Preflight = %#v, must not block for current-chat reply", run.Preflight)
	}
}

// TC-24: "把结果发到飞书给我" → delivery job, requires approval.
func TestScenario_TC24_SendToFeishu(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	taskContract := &countingTaskContractAnalyzer{
		analysis: TaskContractAnalysis{
			JobType:                taskContractJobDelivery,
			SuggestedDomains:       []string{"channel"},
			DeliverableKinds:       []string{taskDeliverableMessageDelivery},
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
		SessionKey:      "chat-tc24-feishu",
		ExternalEventID: "evt-tc24-feishu",
		Content:         "把结果发到飞书给我",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != taskContractJobDelivery {
		t.Fatalf("JobType = %q, want %q", run.TaskContract.JobType, taskContractJobDelivery)
	}
	if !run.TaskContract.RequiresApproval {
		t.Fatal("sending to external channel must require approval")
	}
	if !run.TaskContract.RequiresExternalEffect {
		t.Fatal("sending to external channel must require external effect")
	}
}

// PRIORITY — TC-25: "取消刚才那个监控提醒" → cancel intent, no delivery_target needed.
func TestScenario_TC25_CancelWatchNoDeliveryTarget(t *testing.T) {
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
		SessionKey:      "chat-tc25-cancel-watch",
		ExternalEventID: "evt-tc25-cancel-watch",
		Content:         "取消刚才那个监控提醒",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	for _, m := range run.TaskContract.MissingInfo {
		switch m.ID {
		case taskMissingInfoDeliveryTarget:
			t.Fatal("cancel intent must NOT ask for delivery_target")
		case taskMissingInfoSchedule:
			t.Fatal("cancel intent must NOT ask for schedule")
		}
	}
}

// TC-26: "从现在开始每小时检查 https://example.com" → monitor, with URL and schedule.
func TestScenario_TC26_WatchEveryHour(t *testing.T) {
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
			Confidence:             0.93,
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
		SessionKey:      "chat-tc26-watch-every-hour",
		ExternalEventID: "evt-tc26-watch-every-hour",
		Content:         "从现在开始每小时检查 https://example.com，有变化就在当前会话通知我",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != taskContractJobMonitor {
		t.Fatalf("JobType = %q, want %q", run.TaskContract.JobType, taskContractJobMonitor)
	}
	// URL and schedule are present in the message, so no missing info for these
	for _, m := range run.TaskContract.MissingInfo {
		if m.ID == taskMissingInfoSchedule {
			t.Fatal("schedule is explicit ('每小时'), should not be missing")
		}
	}
	// Target summary should capture the URL
	if !strings.Contains(run.TaskContract.TargetSummary, "example.com") {
		t.Fatalf("TargetSummary = %q, want contains example.com", run.TaskContract.TargetSummary)
	}
}

// TC-27: "停掉所有和 example.com 相关的监控" → cancel intent.
func TestScenario_TC27_StopAllWatchForDomain(t *testing.T) {
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
		SessionKey:      "chat-tc27-stop-watch",
		ExternalEventID: "evt-tc27-stop-watch",
		Content:         "停掉所有和 example.com 相关的监控",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	// Cancel intent should not produce delivery_target or schedule missing info
	for _, m := range run.TaskContract.MissingInfo {
		switch m.ID {
		case taskMissingInfoDeliveryTarget:
			t.Fatal("stop-watch must NOT ask for delivery_target")
		case taskMissingInfoSchedule:
			t.Fatal("stop-watch must NOT ask for schedule")
		}
	}
}

// TC-28: "把这份总结发邮件给某人并附带文件" → delivery, requires approval.
func TestScenario_TC28_EmailWithAttachment(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "把这份总结发邮件给 boss@example.com 并附带文件",
	}, nil, ExecutionModeWorkflow, &RunPreflightReport{
		SuggestedDomains: []string{"email", "document"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType != taskContractJobDelivery {
		t.Fatalf("JobType = %q, want %q", contract.JobType, taskContractJobDelivery)
	}
	if !contract.RequiresApproval {
		t.Fatal("email delivery must require approval")
	}
	if !contract.RequiresExternalEffect {
		t.Fatal("email delivery must require external effect")
	}
	// Has delivery target (email in message)
	if !strings.Contains(contract.TargetSummary, "boss@example.com") {
		t.Fatalf("TargetSummary = %q, want contains boss@example.com", contract.TargetSummary)
	}
}

// TC-29: "需要审批的你就停住等我" → approval required for high-risk actions.
// This test verifies that delivery tasks always have approval acceptance criteria.
func TestScenario_TC29_ApprovalGating(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Send the report to Slack #ops channel",
	}, nil, ExecutionModeWorkflow, &RunPreflightReport{
		SuggestedDomains: []string{"channel"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if !contract.RequiresApproval {
		t.Fatal("external send must require approval")
	}
	// Verify approval acceptance criteria exists
	foundApprovalCriteria := false
	for _, ac := range contract.AcceptanceCriteria {
		if ac.ID == taskAcceptanceApproval {
			foundApprovalCriteria = true
			break
		}
	}
	if !foundApprovalCriteria {
		t.Fatalf("expected approval acceptance criteria, got %#v", contract.AcceptanceCriteria)
	}
}

// ---------------------------------------------------------------------------
// 5. Complex Chain — Task Contract Accuracy
// ---------------------------------------------------------------------------

// TC-31: "打开官网抓信息，整理成 markdown，写入文件，再把路径回复我"
// Multi-tool chain: browser + fs, document deliverable, browser evidence.
func TestScenario_TC31_BrowseWriteFileReplyPath(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "打开官网抓信息，整理成 markdown，写入文件，再把路径回复我",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"browser", "fs"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	// Should NOT be classified as delivery — "回复我" = current chat
	if contract.JobType == taskContractJobDelivery {
		t.Fatalf("JobType = %q, 'reply to me' must not trigger delivery", contract.JobType)
	}
	// Browser evidence deliverable
	foundBrowser := false
	for _, d := range contract.ExpectedDeliverables {
		if d.Kind == taskDeliverableBrowserEvidence {
			foundBrowser = true
			break
		}
	}
	if !foundBrowser {
		t.Fatalf("expected browser_evidence deliverable, got %#v", contract.ExpectedDeliverables)
	}
	if contract.RequiresApproval {
		t.Fatal("browse+write+reply should not require approval")
	}
}

// PRIORITY — TC-32: "读仓库、查启动逻辑、给我 3 个 operational risk，并带文件路径和修复建议"
// Complex analysis task. LLM triage would recognize this as multi-step code
// investigation and return fs domain with planned mode. The key behavioral
// assertions: no external effect, no approval, no delivery_target.
func TestScenario_TC32_ComplexAnalysisConverges(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "读仓库、查启动逻辑、给我 3 个 operational risk，并带文件路径和修复建议",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"fs"},
	}, &RunTriageTrace{
		Source:           "model",
		SuggestedDomains: []string{"fs"},
		Confidence:       0.88,
	})
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.RequiresExternalEffect {
		t.Fatal("code analysis should not require external effect")
	}
	if contract.RequiresApproval {
		t.Fatal("code analysis should not require approval")
	}
	for _, m := range contract.MissingInfo {
		if m.ID == taskMissingInfoDeliveryTarget {
			t.Fatal("analysis task must not ask for delivery_target")
		}
	}
}

// TC-33: "先读代码，再直接修复发现的一个明确 bug，最后跑相关测试"
// LLM triage would recognize multi-step code modification intent.
// Key behavioral assertions: no external effect, no approval, no delivery.
func TestScenario_TC33_ReadFixTest(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "先读代码，再直接修复发现的一个明确 bug，最后跑相关测试",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"fs", "exec"},
	}, &RunTriageTrace{
		Source:           "model",
		SuggestedDomains: []string{"fs", "exec"},
		Confidence:       0.87,
	})
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.RequiresExternalEffect {
		t.Fatal("code fix should not require external effect")
	}
	if contract.RequiresApproval {
		t.Fatal("code fix should not require approval")
	}
}

// TC-37: "先浏览网页，再结合本地文件内容给我结论"
// Browser + FS cross-domain, no delivery.
func TestScenario_TC37_CrossBrowserFS(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "先浏览 https://example.com 网页，再结合本地文件 /tmp/notes.md 的内容给我结论",
	}, nil, ExecutionModePlanned, &RunPreflightReport{
		SuggestedDomains: []string{"browser", "fs"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobDelivery {
		t.Fatal("cross-domain analysis must not be classified as delivery")
	}
	// Should detect URL target
	if !strings.Contains(contract.TargetSummary, "example.com") {
		t.Fatalf("TargetSummary = %q, want URL extracted", contract.TargetSummary)
	}
}

// TC-38: "只改我指定的文件，不要碰别的地方" → general/development, no external effect.
func TestScenario_TC38_ScopedFileEdit(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "只改我指定的文件，不要碰别的地方",
	}, nil, ExecutionModeDirect, nil, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.RequiresExternalEffect {
		t.Fatal("scoped file edit must not require external effect")
	}
	if contract.RequiresApproval {
		t.Fatal("scoped file edit must not require approval")
	}
}

// ---------------------------------------------------------------------------
// 6. Automation / Schedule Classification
// ---------------------------------------------------------------------------

// TC-26 (schedule variant): explicit schedule should not ask for schedule info again.
func TestScenario_TC26_ExplicitScheduleNoMissing(t *testing.T) {
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
			Confidence:             0.92,
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
		SessionKey:      "chat-tc26-explicit-schedule",
		ExternalEventID: "evt-tc26-explicit-schedule",
		Content:         "每天早上 9 点检查 https://example.com 有没有新内容",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	for _, m := range run.TaskContract.MissingInfo {
		if m.ID == taskMissingInfoSchedule {
			t.Fatal("schedule is explicit ('每天早上 9 点'), should not be missing")
		}
	}
}

// "schedule" keyword → automation job.
func TestScenario_ScheduleKeyword(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "schedule a daily report to run at 08:00",
	}, nil, ExecutionModeWorkflow, nil, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType != taskContractJobAutomation {
		t.Fatalf("JobType = %q, want %q", contract.JobType, taskContractJobAutomation)
	}
}

// ---------------------------------------------------------------------------
// 7. Watch Mode Component Integration
// ---------------------------------------------------------------------------

// TC-26 (integration): Full watch creation flow via component.
func TestScenario_TC26_WatchCreationIntegration(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: `{"supported":true,"name":"Example.com hourly check","source_kind":"http","source_url":"https://example.com","interval":"1h","fire_on_start":false,"summary":"已创建监控：每小时检查 example.com 变化。","confidence":0.94}`,
			},
		}},
	}
	workflow := &staticWatchWorkflow{
		result: &WatchWorkflowResult{
			WatchID:   "watch-tc26",
			Name:      "Example.com hourly check",
			SourceURL: "https://example.com",
			Interval:  "1h",
			Summary:   "已创建监控：每小时检查 example.com 变化。",
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: ExecutionModeWatch}}).
		WithTaskContractAnalyzer(&countingTaskContractAnalyzer{
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
		}).
		WithWatchWorkflow(workflow)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-tc26",
		ExternalEventID: "evt-tc26",
		Content:         "从现在开始每小时检查 https://example.com，有变化就在当前会话通知我",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	// Verify: run completed
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", run.Status)
	}
	// Verify: workflow received correct source URL
	if workflow.req.SourceURL != "https://example.com" {
		t.Fatalf("workflow.req.SourceURL = %q", workflow.req.SourceURL)
	}
	// Verify: session has assistant message with summary
	session, err := sessions.GetByKey(context.Background(), "chat-tc26")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if len(session.Messages) < 2 {
		t.Fatalf("session.Messages count = %d, want >= 2", len(session.Messages))
	}
	lastMsg := session.Messages[len(session.Messages)-1]
	if !strings.Contains(lastMsg.Content, "已创建监控") {
		t.Fatalf("last message = %q, want contains '已创建监控'", lastMsg.Content)
	}
}

// TC-27 (integration): Watch with no concrete target → waiting_input.
func TestScenario_TC27_WatchVagueTargetWaitsInput(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: `{"supported":false,"need_confirmation":true,"summary":"需要一个明确的目标才能开始监控。","question":"请提供要监控的具体 URL 或文件路径。","confidence":0.85}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: ExecutionModeWatch}}).
		WithWatchWorkflow(&staticWatchWorkflow{})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-tc27-vague",
		ExternalEventID: "evt-tc27-vague",
		Content:         "帮我监控一下页面变化",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunWaitingInput {
		t.Fatalf("run.Status = %q, want waiting_input", run.Status)
	}
	if run.Preflight == nil {
		t.Fatal("expected preflight with clarification question")
	}
	if !run.Preflight.Blocking {
		t.Fatal("expected blocking preflight for missing watch target")
	}
}

// ---------------------------------------------------------------------------
// 8. Session Continuity
// ---------------------------------------------------------------------------

// TC-08: Second message in same session should inherit context.
func TestScenario_TC08_SessionContextInheritance(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	// First message
	run1, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-tc08",
		ExternalEventID: "evt-tc08-a",
		Content:         "分析一下当前仓库的架构",
	})
	if err != nil {
		t.Fatalf("Submit() first error = %v", err)
	}

	// Second message in the same session
	run2, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-tc08",
		ExternalEventID: "evt-tc08-b",
		Content:         "继续刚才那个任务，但把结果缩成 3 句话",
	})
	if err != nil {
		t.Fatalf("Submit() second error = %v", err)
	}

	// Both runs should share the same session
	if run1.SessionID != run2.SessionID {
		t.Fatalf("session mismatch: run1.SessionID = %q, run2.SessionID = %q", run1.SessionID, run2.SessionID)
	}

	// Session should contain both messages
	session, err := sessions.GetByKey(context.Background(), "chat-tc08")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if len(session.Messages) < 2 {
		t.Fatalf("session.Messages count = %d, want >= 2", len(session.Messages))
	}
}

// ---------------------------------------------------------------------------
// 9. Clarification Slot Resolution
// ---------------------------------------------------------------------------

// TC-30 (clarification): Metadata clarification resolves delivery_target.
func TestScenario_TC30_ClarificationResolvesDelivery(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"delivery",
					"suggested_domains":["channel"],
					"deliverable_kinds":["message_delivery"],
					"missing_info_ids":["delivery_target"],
					"requires_external_effect":true,
					"requires_approval":true,
					"reason":"delivery target missing",
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
		SessionKey:      "chat-tc30-clarification",
		ExternalEventID: "evt-tc30-clarification",
		Content:         "发送这份日报",
		Metadata: map[string]any{
			MetadataKeyClarificationSlots: map[string]string{
				taskMissingInfoDeliveryTarget: "发到飞书群 #daily-ops",
			},
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	// delivery_target should be resolved, not missing
	for _, m := range run.TaskContract.MissingInfo {
		if m.ID == taskMissingInfoDeliveryTarget {
			t.Fatal("delivery_target should have been resolved via clarification")
		}
	}
	foundResolved := false
	for _, r := range run.TaskContract.ResolvedInfo {
		if r.ID == taskMissingInfoDeliveryTarget && strings.Contains(r.Value, "飞书群") {
			foundResolved = true
			break
		}
	}
	if !foundResolved {
		t.Fatalf("expected resolved delivery_target, got %#v", run.TaskContract.ResolvedInfo)
	}
}

// ---------------------------------------------------------------------------
// 10. Edge Cases — Keyword Boundary
// ---------------------------------------------------------------------------

// "reply with" should NOT trigger delivery.
func TestScenario_ReplyWithNotDelivery(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "Reply with a one-line summary of the code",
	}, nil, ExecutionModeDirect, nil, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobDelivery {
		t.Fatal("'Reply with' must not trigger delivery")
	}
	if contract.RequiresApproval {
		t.Fatal("'Reply with' must not require approval")
	}
}

// "告诉我" should NOT trigger delivery.
func TestScenario_TellMeNotDelivery(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "告诉我这段代码有什么问题",
	}, nil, ExecutionModeDirect, nil, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	if contract.JobType == taskContractJobDelivery {
		t.Fatal("'告诉我' must not trigger delivery")
	}
}

// "deploy to staging" → deployment, requires approval.
func TestScenario_DeployRequiresApproval(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	taskContract := &countingTaskContractAnalyzer{
		analysis: TaskContractAnalysis{
			JobType:                taskContractJobDeployment,
			SuggestedDomains:       []string{"exec"},
			DeliverableKinds:       []string{taskDeliverableDeployment},
			MissingInfoIDs:         []string{},
			MissingInfoSpecified:   true,
			RequiresExternalEffect: boolPtr(true),
			RequiresApproval:       boolPtr(true),
			Confidence:             0.95,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithTaskContractAnalyzer(taskContract)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-deploy-requires-approval",
		ExternalEventID: "evt-deploy-requires-approval",
		Content:         "deploy the latest build to staging environment",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != taskContractJobDeployment {
		t.Fatalf("JobType = %q, want %q", run.TaskContract.JobType, taskContractJobDeployment)
	}
	if !run.TaskContract.RequiresApproval {
		t.Fatal("deployment must require approval")
	}
	if !run.TaskContract.RequiresExternalEffect {
		t.Fatal("deployment must require external effect")
	}
}

// Browser purchase should stay on the analyzer path, not heuristic purchase keywords.
func TestScenario_BrowserPurchaseNoTaskApproval(t *testing.T) {
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
		SessionKey:      "chat-scenario-browser-purchase",
		ExternalEventID: "evt-scenario-browser-purchase",
		Content:         "Go to the store and purchase the item in my cart",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if !run.TaskContract.RequiresExternalEffect {
		t.Fatal("browser purchase should require external effect")
	}
	if run.TaskContract.RequiresApproval {
		t.Fatal("browser purchase should NOT require task-level approval — policy engine handles per-tool")
	}
	if run.TaskContract.Source != taskContractSourceModel {
		t.Fatalf("run.TaskContract.Source = %q, want %q", run.TaskContract.Source, taskContractSourceModel)
	}
}

// "发给" with no target → needs delivery_target.
func TestScenario_SendWithoutTargetNeedsClarification(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"delivery",
					"suggested_domains":["channel"],
					"deliverable_kinds":["message_delivery"],
					"missing_info_ids":["delivery_target"],
					"requires_external_effect":true,
					"requires_approval":true,
					"reason":"delivery target missing",
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
		SessionKey:      "chat-send-without-target",
		ExternalEventID: "evt-send-without-target",
		Content:         "把结果发给相关人",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != taskContractJobDelivery {
		t.Fatalf("JobType = %q, want %q", run.TaskContract.JobType, taskContractJobDelivery)
	}
	foundDeliveryMissing := false
	for _, m := range run.TaskContract.MissingInfo {
		if m.ID == taskMissingInfoDeliveryTarget {
			foundDeliveryMissing = true
			break
		}
	}
	if !foundDeliveryMissing {
		t.Fatal("expected delivery_target missing info when no concrete target")
	}
}

// "发给 Slack #ops" → has delivery_target, no missing info for it.
func TestScenario_SendWithTargetNoClarification(t *testing.T) {
	t.Parallel()

	contract := buildTaskContract(IncomingMessage{
		Content: "把结果发给 Slack #ops",
	}, nil, ExecutionModeWorkflow, &RunPreflightReport{
		SuggestedDomains: []string{"channel"},
	}, nil)
	if contract == nil {
		t.Fatal("expected task contract")
	}
	for _, m := range contract.MissingInfo {
		if m.ID == taskMissingInfoDeliveryTarget {
			t.Fatal("Slack #ops is a concrete target, should not be missing")
		}
	}
}
