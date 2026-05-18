package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/triage"
)

type countingTaskContractAnalyzer struct {
	calls    int
	lastReq  TaskContractAnalysisRequest
	analysis TaskContractAnalysis
	err      error
}

func (a *countingTaskContractAnalyzer) Analyze(_ context.Context, req TaskContractAnalysisRequest) (TaskContractAnalysis, error) {
	a.calls++
	a.lastReq = req
	if a.err != nil {
		return TaskContractAnalysis{}, a.err
	}
	return a.analysis, nil
}

type staticPreflightAnalyzer struct {
	analysis PreflightAnalysis
	err      error
	calls    int
	lastReq  PreflightAnalysisRequest
}

func (a *staticPreflightAnalyzer) Analyze(_ context.Context, req PreflightAnalysisRequest) (PreflightAnalysis, error) {
	a.calls++
	a.lastReq = req
	if a.err != nil {
		return PreflightAnalysis{}, a.err
	}
	return a.analysis, nil
}

func TestBuildRunPreflightNeedsConfirmationForAmbiguousReference(t *testing.T) {
	t.Parallel()

	report := buildRunPreflightWithAnalysis("把这个文件改一下", PreflightAnalysis{
		NeedsReference: true,
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
	if len(report.Checks) == 0 || report.Checks[0].ID != "reference_gap" {
		t.Fatalf("report.Checks = %#v", report.Checks)
	}
	if report.Question == "" || len(report.ReplyHints) == 0 || report.ContinueHint == "" {
		t.Fatalf("report guidance = %#v", report)
	}
}

func TestNormalizePreflightAnalysisTreatsPositiveAnalyzerSignalsAsExplicit(t *testing.T) {
	t.Parallel()

	analysis := normalizePreflightAnalysisWithOptions(PreflightAnalysis{
		NeedsReference: true,
	}, PreflightAnalysisRequest{
		Message: "把这个文件改一下",
		SemanticSignal: &SemanticSignal{
			NeedsReference:    false,
			NeedsReferenceSet: true,
		},
	}, false)

	if !analysis.NeedsReference {
		t.Fatal("expected positive analyzer decision to be preserved")
	}
	if !analysis.NeedsReferenceSet {
		t.Fatal("expected positive analyzer decision to become explicit")
	}
}

func TestBuildRunPreflightAutoPreparingForChartRender(t *testing.T) {
	t.Parallel()

	report := buildRunPreflightWithAnalysis("render a chart", PreflightAnalysis{
		SuggestedDomains: []string{"canvas"},
	})
	if report == nil {
		t.Fatal("expected preflight report")
	}
	if report.State != RunPreflightAutoPreparing {
		t.Fatalf("report.State = %q", report.State)
	}
	if report.Blocking {
		t.Fatal("expected non-blocking preflight")
	}
}

func TestBuildRunPreflightBrowserReferenceHints(t *testing.T) {
	t.Parallel()

	report := buildRunPreflightWithAnalysis("看看这个网页然后总结", PreflightAnalysis{
		NeedsReference:   true,
		SuggestedDomains: []string{"browser"},
	})
	if report == nil {
		t.Fatal("expected preflight report")
	}
	if report.Question != "Which exact URL or webpage should I use?" {
		t.Fatalf("report.Question = %q", report.Question)
	}
	if len(report.ReplyHints) == 0 {
		t.Fatal("expected reply hints")
	}
	foundURLHint := false
	for _, hint := range report.ReplyHints {
		if hint == "https://example.com/page" {
			foundURLHint = true
			break
		}
	}
	if !foundURLHint {
		t.Fatalf("reply hints = %#v", report.ReplyHints)
	}
}

func TestFallbackPreflightAnalysisNeedsReferenceForBrowserToFileWithoutURL(t *testing.T) {
	t.Parallel()

	// The output path is not enough for a browser-sourced task; fallback must
	// still ask which page to inspect.
	analysis := fallbackPreflightAnalysis(PreflightAnalysisRequest{
		Message: "抓取页面信息，写到 docs/tmp/example-brief.md",
	})
	if !analysis.NeedsReference {
		t.Fatalf("analysis = %#v, want browser task without source URL to need reference", analysis)
	}
	if !containsTestString(analysis.SuggestedDomains, "browser") {
		t.Fatalf("analysis domains = %#v, want browser from natural-language page reference", analysis.SuggestedDomains)
	}
	if !containsTestString(analysis.SuggestedDomains, "fs") {
		t.Fatalf("analysis domains = %#v, want fs from file path evidence", analysis.SuggestedDomains)
	}
}

func TestFallbackPreflightAnalysisNeedsReferenceForNaturalLanguageFileEditWithoutPath(t *testing.T) {
	t.Parallel()

	analysis := fallbackPreflightAnalysis(PreflightAnalysisRequest{
		Message: "把这个文件改一下",
	})
	if !analysis.NeedsReference {
		t.Fatalf("analysis = %#v, want ambiguous file edit to need reference", analysis)
	}
}

func TestInferredNeedsReferenceWithContractForFileEditMissingSourceTarget(t *testing.T) {
	t.Parallel()

	needsReference := inferredNeedsReferenceWithContract(PreflightAnalysisRequest{
		Message: "把这个文件改一下",
	}, []string{"fs"}, &TaskContract{
		MissingInfo: []TaskContractMissingInfo{
			{ID: taskMissingInfoSourceTarget, Required: true},
		},
	})
	if !needsReference {
		t.Fatal("expected file-edit task contract with missing source_target to require reference")
	}
}

func TestNormalizePreflightAnalysisNeedsReferenceForGenericFileEditWithoutPath(t *testing.T) {
	t.Parallel()

	analysis := normalizePreflightAnalysis(PreflightAnalysis{
		NeedsReference: true,
	}, PreflightAnalysisRequest{
		Message: "把这个文件改一下",
	})
	if !analysis.NeedsReference {
		t.Fatalf("analysis = %#v, want needs_reference for unresolved file target", analysis)
	}
}

func TestNormalizePreflightAnalysisAllowsExploratoryFileReadWithoutPath(t *testing.T) {
	t.Parallel()

	analysis := normalizePreflightAnalysis(PreflightAnalysis{}, PreflightAnalysisRequest{
		Message: "read the file and summarize it",
	})
	if analysis.NeedsReference {
		t.Fatalf("analysis = %#v, want exploratory file read to proceed without blocking reference", analysis)
	}
}

func TestNormalizePreflightAnalysisAllowsExploratoryReadmeInspectionWithoutPath(t *testing.T) {
	t.Parallel()

	analysis := normalizePreflightAnalysis(PreflightAnalysis{}, PreflightAnalysisRequest{
		Message: "inspect the readme and summarize it",
	})
	if analysis.NeedsReference {
		t.Fatalf("analysis = %#v, want readme inspection to proceed without blocking reference", analysis)
	}
}

func TestNormalizePreflightAnalysisKeepsSessionBrowserContext(t *testing.T) {
	t.Parallel()

	analysis := normalizePreflightAnalysis(PreflightAnalysis{}, PreflightAnalysisRequest{
		Message:        "抓取页面信息，写到 docs/tmp/example-brief.md",
		SessionSummary: "Current page context | https://example.com | title=Example Domain | session=sess-followup",
	})
	if analysis.NeedsReference {
		t.Fatalf("analysis = %#v, expected session browser context to satisfy reference", analysis)
	}
	if !containsTestString(analysis.SuggestedDomains, "browser") {
		t.Fatalf("analysis.SuggestedDomains = %#v, want browser for session page follow-up", analysis.SuggestedDomains)
	}
}

func TestNormalizePreflightAnalysisUsesSessionBrowserContextForButtonFollowUp(t *testing.T) {
	t.Parallel()

	analysis := normalizePreflightAnalysis(PreflightAnalysis{}, PreflightAnalysisRequest{
		Message:        "如果按钮点不到，就自己换 selector 再试，不要直接失败",
		SessionSummary: "Current page context | https://example.com | title=Example Domain | session=sess-followup",
	})
	if analysis.NeedsReference {
		t.Fatalf("analysis = %#v, expected current page context to satisfy selector follow-up", analysis)
	}
	if !containsTestString(analysis.SuggestedDomains, "browser") {
		t.Fatalf("analysis.SuggestedDomains = %#v, want browser for selector follow-up", analysis.SuggestedDomains)
	}
}

func TestNormalizePreflightAnalysisSearchResultsStillNeedsReference(t *testing.T) {
	t.Parallel()

	// A non-search current page is not enough context for a request that asks to
	// open a page and extract search results.
	analysis := normalizePreflightAnalysis(PreflightAnalysis{}, PreflightAnalysisRequest{
		Message:        "打开页面，等搜索结果加载出来，再提取前 5 条",
		SessionSummary: "Current page context | https://httpbin.org/forms/post | title=HTTPBin Form | session=sess-followup",
	})
	if !analysis.NeedsReference {
		t.Fatalf("analysis = %#v, want new navigation/search target to need reference", analysis)
	}
	if !containsTestString(analysis.SuggestedDomains, "browser") {
		t.Fatalf("analysis.SuggestedDomains = %#v, want browser for search-results follow-up", analysis.SuggestedDomains)
	}
}

func TestNormalizePreflightAnalysisSearchResultsReuseCurrentSearchPage(t *testing.T) {
	t.Parallel()

	analysis := normalizePreflightAnalysis(PreflightAnalysis{}, PreflightAnalysisRequest{
		Message:        "打开页面，等搜索结果加载出来，再提取前 5 条",
		SessionSummary: "Current page context | https://www.bing.com/search?q=openai | title=openai - Search | session=sess-search",
	})
	if analysis.NeedsReference {
		t.Fatalf("analysis = %#v, expected current search-results page context to satisfy reference", analysis)
	}
	if !containsTestString(analysis.SuggestedDomains, "browser") {
		t.Fatalf("analysis.SuggestedDomains = %#v, want browser for search-results reuse", analysis.SuggestedDomains)
	}
}

func TestNormalizePreflightAnalysisSuppressesEmailDomainForBrowserFormField(t *testing.T) {
	t.Parallel()

	analysis := normalizePreflightAnalysis(PreflightAnalysis{
		SuggestedDomains: []string{"browser", "email"},
	}, PreflightAnalysisRequest{
		Message: "Open https://httpbin.org/forms/post and fill input[name=email] with qa@example.com before submit.",
	})
	if containsTestString(analysis.SuggestedDomains, "email") {
		t.Fatalf("analysis.SuggestedDomains = %#v, want browser-only domains for email form field task", analysis.SuggestedDomains)
	}
	if !containsTestString(analysis.SuggestedDomains, "browser") {
		t.Fatalf("analysis.SuggestedDomains = %#v, want browser", analysis.SuggestedDomains)
	}
}

func TestNormalizePreflightAnalysisKeepsSemanticNewsAndSearchDomains(t *testing.T) {
	t.Parallel()

	analysis := normalizePreflightAnalysis(PreflightAnalysis{
		SuggestedDomains: []string{"news", "search", "web"},
	}, PreflightAnalysisRequest{
		Message: "collect the latest updates from the target source and compare the top items",
	})
	if !containsTestString(analysis.SuggestedDomains, "news") {
		t.Fatalf("analysis.SuggestedDomains = %#v, want news", analysis.SuggestedDomains)
	}
	if !containsTestString(analysis.SuggestedDomains, "search") {
		t.Fatalf("analysis.SuggestedDomains = %#v, want search", analysis.SuggestedDomains)
	}
	if !containsTestString(analysis.SuggestedDomains, "web") {
		t.Fatalf("analysis.SuggestedDomains = %#v, want web", analysis.SuggestedDomains)
	}
}

func TestNormalizePreflightAnalysisSuppressesWatchDomainForBrowserKeepPageRequest(t *testing.T) {
	t.Parallel()

	analysis := normalizePreflightAnalysis(PreflightAnalysis{
		SuggestedDomains:   []string{"browser", "watch"},
		BrowserContextOnly: true,
	}, PreflightAnalysisRequest{
		Message: "打开 https://example.com 并保持在当前页面，不要继续分析。",
	})
	if containsTestString(analysis.SuggestedDomains, "watch") {
		t.Fatalf("analysis.SuggestedDomains = %#v, want browser-only domains for keep-page browser task", analysis.SuggestedDomains)
	}
	if !containsTestString(analysis.SuggestedDomains, "browser") {
		t.Fatalf("analysis.SuggestedDomains = %#v, want browser", analysis.SuggestedDomains)
	}
}

func TestNormalizePreflightAnalysisClearsModelReferenceGapWhenSessionPageExists(t *testing.T) {
	t.Parallel()

	analysis := normalizePreflightAnalysis(PreflightAnalysis{
		NeedsReference:   true,
		SuggestedDomains: []string{"browser", "fs"},
	}, PreflightAnalysisRequest{
		Message:        "抓取页面信息，写到 docs/tmp/example-brief.md",
		SessionSummary: "Current page context | https://example.com | title=Example Domain | session=sess-followup",
	})
	if analysis.NeedsReference {
		t.Fatalf("analysis = %#v, expected current page context to clear model reference gap", analysis)
	}
}

func TestSessionReferenceSummaryUsesRecentBrowserToolMessages(t *testing.T) {
	t.Parallel()

	session := &Session{}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:    contextengine.RoleTool,
		Name:    "browser.navigate",
		Content: `{"url":"https://example.com","title":"Example Domain","session_id":"sess-1"}`,
	})

	summary := sessionReferenceSummary(session)
	if !strings.Contains(summary, "https://example.com") {
		t.Fatalf("summary = %q, want recent browser URL", summary)
	}
	if !strings.Contains(summary, "Current page context") {
		t.Fatalf("summary = %q, want current-page marker", summary)
	}
}

func TestSessionReferenceSummaryKeepsLatestBrowserContextAcrossNoisySession(t *testing.T) {
	t.Parallel()

	session := &Session{}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:    contextengine.RoleTool,
		Name:    "browser.snapshot",
		Content: `{"url":"https://httpbin.org/forms/post","title":"HTTPBin Form","session_id":"sess-2"}`,
	})
	for i := 0; i < 40; i++ {
		session.Messages = append(session.Messages, contextengine.Message{
			Role:    contextengine.RoleTool,
			Name:    "exec.shell",
			Content: `{"stdout":"noise"}`,
		})
	}

	summary := sessionReferenceSummary(session)
	if !strings.Contains(summary, "https://httpbin.org/forms/post") {
		t.Fatalf("summary = %q, want latest browser URL despite non-browser noise", summary)
	}
}

func TestSubmitBrowserFollowUpUsesRecentSessionBrowserContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	session, err := sessions.GetOrCreate(ctx, "chat-browser-followup", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleTool,
		Name:      "browser.snapshot",
		Content:   `{"url":"https://example.com","title":"Example Domain","content":"hello"}`,
		CreatedAt: time.Now().UTC().Add(-time.Minute),
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "chat-browser-followup",
		ExternalEventID: "evt-browser-followup",
		Content:         "打开页面后截图给我，再告诉我页面标题",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status == RunWaitingInput {
		t.Fatalf("run.Status = %q, expected follow-up browser context to avoid waiting_input", run.Status)
	}
	if run.Preflight != nil && preflightHasCheck(run.Preflight, "reference_gap") {
		t.Fatalf("run.Preflight = %#v, expected recent browser context to satisfy reference", run.Preflight)
	}
}

func TestSubmitBrowserFollowUpUsesRecentBrowserToolContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	session, err := sessions.GetOrCreate(ctx, "chat-browser-followup-assistant-summary", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleTool,
		Name:      "browser.snapshot",
		Content:   `{"url":"https://httpbin.org/forms/post","title":"HTTPBin Form","session_id":"sess-browser"}`,
		CreatedAt: time.Now().UTC().Add(-time.Minute),
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "chat-browser-followup-assistant-summary",
		ExternalEventID: "evt-browser-followup-assistant-summary",
		Content:         "页面 URL 里有 /post，但这只是一个网页表单",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status == RunWaitingInput {
		t.Fatalf("run.Status = %q, expected recent browser tool context to avoid waiting_input", run.Status)
	}
	if run.Preflight != nil && preflightHasCheck(run.Preflight, "reference_gap") {
		t.Fatalf("run.Preflight = %#v, expected recent browser tool context to satisfy reference", run.Preflight)
	}
}

func TestSaveSessionPersistsCurrentBrowserContextIntoSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	session, err := sessions.GetOrCreate(ctx, "chat-browser-summary-persist", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := sessions.LoadForExecution(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	defer unlock()

	locked.Messages = append(locked.Messages, contextengine.Message{
		Role:      contextengine.RoleTool,
		Name:      "browser.snapshot",
		Content:   `{"url":"https://httpbin.org/forms/post","title":"HTTPBin Form","session_id":"sess-browser"}`,
		CreatedAt: time.Now().UTC().Add(-time.Minute),
	})
	if err := component.saveSession(ctx, nil, locked); err != nil {
		t.Fatalf("saveSession() error = %v", err)
	}

	stored, err := sessions.Get(ctx, locked.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !strings.Contains(stored.Summary, "Current page context | https://httpbin.org/forms/post") {
		t.Fatalf("stored.Summary = %q, want persisted current page context", stored.Summary)
	}
}

func TestSubmitBrowserFollowUpUsesPersistedCurrentPageSummaryContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	session, err := sessions.GetOrCreate(ctx, "chat-browser-summary-only", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := sessions.LoadForExecution(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	locked.Summary = "Previous summary\nCurrent page context | https://httpbin.org/forms/post | session=sess-followup"
	locked.SummaryAt = time.Now().UTC().Add(-time.Minute)
	if err := sessions.Save(ctx, locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "chat-browser-summary-only",
		ExternalEventID: "evt-browser-summary-only",
		Content:         "页面 URL 里有 /post，但这只是一个网页表单",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status == RunWaitingInput {
		t.Fatalf("run.Status = %q, expected persisted current page summary to avoid waiting_input", run.Status)
	}
	if run.Preflight != nil && preflightHasCheck(run.Preflight, "reference_gap") {
		t.Fatalf("run.Preflight = %#v, expected persisted current page summary to satisfy reference", run.Preflight)
	}
}

func TestSubmitBrowserFollowUpWithoutURLBlocksForReference(t *testing.T) {
	t.Parallel()

	// A browser session id alone is not enough; without a concrete current page
	// summary or URL, a natural-language page follow-up should block for a
	// concrete reference.
	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	session, err := sessions.GetOrCreate(ctx, "chat-browser-followup-session-only", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleTool,
		Name:      "browser.create_session",
		Content:   `{"session_id":"sess-3"}`,
		CreatedAt: time.Now().UTC().Add(-time.Minute),
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "chat-browser-followup-session-only",
		ExternalEventID: "evt-browser-followup-session-only",
		Content:         "打开页面后截图给我，再告诉我页面标题",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status != RunWaitingInput {
		t.Fatalf("run.Status = %q, want waiting_input", run.Status)
	}
	if run.Preflight == nil || !preflightHasCheck(run.Preflight, "reference_gap") {
		t.Fatalf("run.Preflight = %#v, want blocking reference_gap", run.Preflight)
	}
}

func TestBuildRunPreflightCreatesClarificationSlots(t *testing.T) {
	t.Parallel()

	report := buildRunPreflightWithContract("定时监控网站价格并发邮件通知", PreflightAnalysis{
		SuggestedDomains: []string{"watch", "email"},
	}, &TaskContract{
		MissingInfo: []TaskContractMissingInfo{
			{
				ID:          taskMissingInfoDeliveryTarget,
				Label:       taskMissingInfoLabel(taskMissingInfoDeliveryTarget),
				Summary:     "The task needs an explicit recipient or channel.",
				Question:    "Where should I send or post the result?",
				InputMode:   taskMissingInfoInputMode(taskMissingInfoDeliveryTarget),
				Placeholder: taskMissingInfoPlaceholder(taskMissingInfoDeliveryTarget),
				Required:    true,
				Hints:       []string{"发送到 Slack #ops"},
			},
			{
				ID:          taskMissingInfoSchedule,
				Label:       taskMissingInfoLabel(taskMissingInfoSchedule),
				Summary:     "The task needs a repeat schedule.",
				Question:    "When should this run, and how often should it repeat?",
				InputMode:   taskMissingInfoInputMode(taskMissingInfoSchedule),
				Placeholder: taskMissingInfoPlaceholder(taskMissingInfoSchedule),
				Required:    true,
				Hints:       []string{"每周一 08:30"},
			},
		},
	})
	if report == nil {
		t.Fatal("expected preflight report")
	}
	if len(report.ClarificationSlots) != 2 {
		t.Fatalf("clarification slots = %#v", report.ClarificationSlots)
	}
	if report.ReplyTemplate == "" {
		t.Fatalf("expected reply template, got %#v", report)
	}
	for _, expected := range []string{"delivery_target (发送位置):", "schedule (执行时间):"} {
		if !strings.Contains(report.ReplyTemplate, expected) {
			t.Fatalf("reply template = %q, want %q", report.ReplyTemplate, expected)
		}
	}
}

func TestBuildRunPreflightAddsVerificationPlanForOfficeTask(t *testing.T) {
	t.Parallel()

	report := buildRunPreflightWithAnalysis("生成一个季度汇总表", PreflightAnalysis{
		SuggestedDomains: []string{"spreadsheet"},
	})
	if report == nil {
		t.Fatal("expected preflight report")
	}
	foundExpected := false
	foundVerification := false
	for _, check := range report.Checks {
		switch check.ID {
		case "expected_output":
			foundExpected = true
		case "verification_plan":
			foundVerification = true
		}
	}
	if !foundExpected || !foundVerification {
		t.Fatalf("report.Checks = %#v", report.Checks)
	}
}

func TestSubmitBuildsPreflightReportAndEmitsEvent(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithEventBus(bus).
		WithPreflightAnalyzer(NewModelPreflightAnalyzer(&stubModelClient{
			responses: []*ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: `{"suggested_domains":["canvas"],"confidence":0.9}`,
				},
			}},
		}, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-preflight",
		ExternalEventID: "evt-preflight",
		Content:         "render a chart",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Preflight == nil || run.Preflight.State != RunPreflightAutoPreparing {
		t.Fatalf("run.Preflight = %#v", run.Preflight)
	}
	if run.Status != RunQueued {
		t.Fatalf("run.Status = %q, want queued", run.Status)
	}

	found := false
	for _, event := range bus.Snapshot() {
		if event.Type != eventbus.EventRunPreflightUpdated || event.RunID != run.ID {
			continue
		}
		if payload, ok := event.RunPreflightUpdatedPayload(); !ok || payload.State != string(RunPreflightAutoPreparing) {
			t.Fatalf("event.RunPreflightUpdatedPayload() = %#v, %v", payload, ok)
		}
		if got, _ := event.Attrs["state"].(string); got != string(RunPreflightAutoPreparing) {
			t.Fatalf("event.Attrs[state] = %#v", event.Attrs["state"])
		}
		found = true
	}
	if !found {
		t.Fatal("expected run.preflight_updated event")
	}
}

func TestSubmitHonorsExplicitModelPreflightWithoutReferenceGap(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPreflightAnalyzer(NewModelPreflightAnalyzer(&stubModelClient{
			responses: []*ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: `{"needs_reference":false,"needs_confirmation":false,"suggested_domains":["fs"],"reason":"the workspace target is already implicit","confidence":0.96}`,
				},
			}},
		}, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-preflight-model-no-reference",
		ExternalEventID: "evt-preflight-model-no-reference",
		Content:         "write the file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status == RunWaitingInput {
		t.Fatalf("run.Status = %q, expected explicit model preflight to avoid waiting_input", run.Status)
	}
	if run.Preflight != nil && preflightHasCheck(run.Preflight, "reference_gap") {
		t.Fatalf("run.Preflight = %#v, expected no reference_gap when model preflight explicitly opts out", run.Preflight)
	}
}

func TestSubmitPreflightAnalyzerRequestCarriesSpanishSemanticSignal(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: `{"needs_reference":false,"suggested_domains":["browser"],"confidence":0.87}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPreflightAnalyzer(NewModelPreflightAnalyzer(model, 0))

	_, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-preflight-semantic-es",
		ExternalEventID: "evt-preflight-semantic-es",
		Content:         "Resume esta pagina en docs/tmp/resumen.md",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if len(model.lastRequest.Messages) != 1 {
		t.Fatalf("lastRequest.Messages = %#v", model.lastRequest.Messages)
	}
	var req PreflightAnalysisRequest
	if err := json.Unmarshal([]byte(model.lastRequest.Messages[0].Content), &req); err != nil {
		t.Fatalf("unmarshal preflight analyzer request: %v", err)
	}
	if req.SemanticSignal == nil {
		t.Fatal("expected semantic signal on preflight analyzer request")
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
	if !req.SemanticSignal.TriageReady {
		t.Fatalf("req.SemanticSignal.TriageReady = %v, want true after triage seed is established", req.SemanticSignal.TriageReady)
	}
	if req.SemanticSignal.TaskContractReady {
		t.Fatalf("req.SemanticSignal.TaskContractReady = %v, want false before task-contract analysis", req.SemanticSignal.TaskContractReady)
	}
}

func TestSubmitRunsPreflightAnalyzerAfterModelRunTriageWithSharedSemanticSignal(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{
		decision: triage.RunDecision{
			ExecutionMode:    "planned",
			SuggestedDomains: []string{"browser"},
			Confidence:       0.91,
		},
	}
	preflight := &staticPreflightAnalyzer{
		analysis: PreflightAnalysis{
			NeedsReference:   false,
			SuggestedDomains: []string{"browser"},
			Reason:           "preflight_confirmed",
			Confidence:       0.83,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager).
		WithPreflightAnalyzer(preflight)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-preflight-after-triage",
		ExternalEventID: "evt-preflight-after-triage",
		Content:         "Resume esta página en docs/tmp/resumen.md",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if triager.calls != 1 {
		t.Fatalf("triager.calls = %d, want 1", triager.calls)
	}
	if preflight.calls != 1 {
		t.Fatalf("preflight.calls = %d, want 1", preflight.calls)
	}
	if preflight.lastReq.SemanticSignal == nil {
		t.Fatal("expected semantic signal on preflight request")
	}
	if preflight.lastReq.SemanticSignal.Language.Family != "es" {
		t.Fatalf("preflight.lastReq.SemanticSignal.Language.Family = %q, want es", preflight.lastReq.SemanticSignal.Language.Family)
	}
	if !preflight.lastReq.SemanticSignal.TriageReady {
		t.Fatalf("preflight.lastReq.SemanticSignal.TriageReady = %v, want true", preflight.lastReq.SemanticSignal.TriageReady)
	}
	if preflight.lastReq.SemanticSignal.TaskContractReady {
		t.Fatalf("preflight.lastReq.SemanticSignal.TaskContractReady = %v, want false", preflight.lastReq.SemanticSignal.TaskContractReady)
	}
	if run.Preflight == nil {
		t.Fatal("expected preflight report")
	}
	if run.Triage == nil || run.Triage.Source != "model" {
		t.Fatalf("run.Triage = %#v, want model trace", run.Triage)
	}
}

func TestSubmitPreflightAnalyzerCanBlockAfterRunTriageSeedsUnknowns(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{
		decision: triage.RunDecision{
			ExecutionMode: "planned",
			Confidence:    0.72,
		},
	}
	preflight := &staticPreflightAnalyzer{
		analysis: PreflightAnalysis{
			NeedsReference: true,
			Reason:         "need_source_target",
			Confidence:     0.88,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager).
		WithPreflightAnalyzer(preflight)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-preflight-block-after-triage",
		ExternalEventID: "evt-preflight-block-after-triage",
		Content:         "把这个文件改一下",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if triager.calls != 1 {
		t.Fatalf("triager.calls = %d, want 1", triager.calls)
	}
	if preflight.calls != 1 {
		t.Fatalf("preflight.calls = %d, want 1", preflight.calls)
	}
	if run.Status != RunWaitingInput {
		t.Fatalf("run.Status = %q, want waiting_input", run.Status)
	}
	if run.Preflight == nil || !run.Preflight.Blocking {
		t.Fatalf("run.Preflight = %#v, want blocking preflight", run.Preflight)
	}
}

func TestSubmitUsesTaskContractDomainsToRequireBrowserReferenceForSpanishPageWriteTask(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"report",
					"suggested_domains":["browser","fs","document"],
					"deliverable_kinds":["browser_evidence","document"],
					"missing_info_ids":[],
					"requires_external_effect":false,
					"requires_approval":false,
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
		SessionKey:      "chat-preflight-contract-es-reference",
		ExternalEventID: "evt-preflight-contract-es-reference",
		Content:         "Resume esta página en docs/tmp/resumen.md",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status != RunWaitingInput {
		t.Fatalf("run.Status = %q, want waiting_input", run.Status)
	}
	if run.Preflight == nil || !preflightHasCheck(run.Preflight, "reference_gap") {
		t.Fatalf("run.Preflight = %#v, want reference_gap", run.Preflight)
	}
}

func TestSubmitUsesSessionBrowserContextForSpanishPageFollowUp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"report",
					"suggested_domains":["browser","document"],
					"deliverable_kinds":["browser_evidence","document"],
					"missing_info_ids":[],
					"requires_external_effect":false,
					"requires_approval":false,
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

	session, err := sessions.GetOrCreate(ctx, "chat-preflight-contract-es-followup", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleTool,
		Name:      "browser.snapshot",
		Content:   `{"url":"https://example.com","title":"Example Domain","content":"hello"}`,
		CreatedAt: time.Now().UTC().Add(-time.Minute),
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "chat-preflight-contract-es-followup",
		ExternalEventID: "evt-preflight-contract-es-followup",
		Content:         "Resume esta página en docs/tmp/resumen.md",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status == RunWaitingInput {
		t.Fatalf("run.Status = %q, expected existing browser context to satisfy reference", run.Status)
	}
	if run.Preflight != nil && preflightHasCheck(run.Preflight, "reference_gap") {
		t.Fatalf("run.Preflight = %#v, expected recent browser context to satisfy reference", run.Preflight)
	}
}

func containsTestString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestSubmitBlockingPreflightWaitsForInput(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPreflightAnalyzer(NewModelPreflightAnalyzer(&stubModelClient{
			responses: []*ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: `{"needs_reference":true,"confidence":0.91}`,
				},
			}},
		}, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-blocking-preflight",
		ExternalEventID: "evt-blocking-preflight",
		Content:         "把这个文件改一下",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status != RunWaitingInput {
		t.Fatalf("run.Status = %q, want waiting_input", run.Status)
	}
	if run.Preflight == nil || !run.Preflight.Blocking {
		t.Fatalf("run.Preflight = %#v", run.Preflight)
	}
	if run.Preflight.Prompt == "" {
		t.Fatal("expected preflight prompt")
	}
}

func TestSubmitBlockingPreflightSkipsTaskContractAnalyzer(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	taskContract := &countingTaskContractAnalyzer{
		analysis: TaskContractAnalysis{
			JobType:          taskContractJobReport,
			SuggestedDomains: []string{"browser", "document"},
			DeliverableKinds: []string{"browser_evidence", "document"},
			Confidence:       0.92,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPreflightAnalyzer(NewModelPreflightAnalyzer(&stubModelClient{
			responses: []*ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: `{"needs_reference":true,"suggested_domains":["browser"],"confidence":0.93}`,
				},
			}},
		}, 0)).
		WithTaskContractAnalyzer(taskContract)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-blocking-preflight-skip-contract",
		ExternalEventID: "evt-blocking-preflight-skip-contract",
		Content:         "Resume esta página.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status != RunWaitingInput {
		t.Fatalf("run.Status = %q, want waiting_input", run.Status)
	}
	if taskContract.calls != 0 {
		t.Fatalf("taskContract.calls = %d, want 0", taskContract.calls)
	}
	if run.TaskContract == nil {
		t.Fatal("expected heuristic task contract seed even when model contract is deferred")
	}
	if run.TaskContract.Source != taskContractSourceHeuristic {
		t.Fatalf("run.TaskContract.Source = %q, want %q", run.TaskContract.Source, taskContractSourceHeuristic)
	}
}

func TestSubmitFallbackBlockingPreflightStillUsesTaskContractAnalyzer(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	taskContract := &countingTaskContractAnalyzer{
		analysis: TaskContractAnalysis{
			JobType:              taskContractJobDevelopment,
			SuggestedDomains:     []string{"fs"},
			MissingInfoIDs:       []string{taskMissingInfoSourceTarget},
			MissingInfoSpecified: true,
			Confidence:           0.92,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPreflightAnalyzer(&staticPreflightAnalyzer{}).
		WithTaskContractAnalyzer(taskContract)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-fallback-blocking-preflight-uses-contract",
		ExternalEventID: "evt-fallback-blocking-preflight-uses-contract",
		Content:         "把这个文件改一下",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status != RunWaitingInput {
		t.Fatalf("run.Status = %q, want waiting_input", run.Status)
	}
	if taskContract.calls != 1 {
		t.Fatalf("taskContract.calls = %d, want 1", taskContract.calls)
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

func TestSubmitModelPreflightNeedsReferenceDoesNotBackfillHeuristicDomains(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPreflightAnalyzer(NewModelPreflightAnalyzer(&stubModelClient{
			responses: []*ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: `{"needs_reference":true,"confidence":0.91}`,
				},
			}},
		}, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-blocking-preflight-no-domain-backfill",
		ExternalEventID: "evt-blocking-preflight-no-domain-backfill",
		Content:         "把这个文件改一下",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Preflight == nil {
		t.Fatal("expected preflight")
	}
	if len(run.Preflight.SuggestedDomains) != 0 {
		t.Fatalf("run.Preflight.SuggestedDomains = %#v, want no heuristic backfill when model omitted domains", run.Preflight.SuggestedDomains)
	}
}

func TestExecuteRunMarksAutoPreparingPreflightReady(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "截图已完成",
			},
		}},
	}, nil, nil).WithPreflightAnalyzer(NewModelPreflightAnalyzer(&stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: `{"suggested_domains":["canvas"],"confidence":0.9}`,
			},
		}},
	}, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-preflight-ready",
		ExternalEventID: "evt-preflight-ready",
		Content:         "render a chart",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Preflight == nil || run.Preflight.State != RunPreflightAutoPreparing {
		t.Fatalf("initial run.Preflight = %#v", run.Preflight)
	}

	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Preflight == nil || run.Preflight.State != RunPreflightReady {
		t.Fatalf("run.Preflight after execute = %#v", run.Preflight)
	}
	if run.Preflight.Summary != "Ready to execute." {
		t.Fatalf("run.Preflight.Summary = %q", run.Preflight.Summary)
	}
}
