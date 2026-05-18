package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

const (
	defaultEvalTimeout      = 45 * time.Second
	defaultEvalPollInterval = 250 * time.Millisecond
)

type EvalRunner interface {
	Execute(ctx context.Context, req EvalExecutionRequest) (*EvalExecutionResult, error)
}

type EvalExecutionRequest struct {
	Submit       SubmitRequest
	Timeout      time.Duration
	PollInterval time.Duration
	HarnessOnly  bool
}

type EvalExecutionResult struct {
	Run        *agent.Run
	Completion *RunCompletion
	Harness    *agent.RunHarnessSummary
}

type EvalRunRequest struct {
	SuiteID        string   `json:"suite_id"`
	CaseIDs        []string `json:"case_ids,omitempty"`
	Model          string   `json:"model,omitempty"`
	AutomationID   string   `json:"automation_id,omitempty"`
	SessionPrefix  string   `json:"session_prefix,omitempty"`
	TimeoutMS      int      `json:"timeout_ms,omitempty"`
	PollIntervalMS int      `json:"poll_interval_ms,omitempty"`
}

type EvalSuite struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description,omitempty"`
	Surface       string     `json:"surface,omitempty"`
	Prerequisites []string   `json:"prerequisites,omitempty"`
	Cases         []EvalCase `json:"cases,omitempty"`
}

type EvalCase struct {
	ID                         string   `json:"id"`
	Name                       string   `json:"name"`
	Prompt                     string   `json:"prompt"`
	HarnessOnly                bool     `json:"harness_only,omitempty"`
	ExpectedSurface            string   `json:"expected_surface,omitempty"`
	ExpectedHarnessIntent      string   `json:"expected_harness_intent,omitempty"`
	ExpectedHarnessDomains     []string `json:"expected_harness_domains,omitempty"`
	RequireThinkingModel       bool     `json:"require_thinking_model,omitempty"`
	MinExtraToolRounds         int      `json:"min_extra_tool_rounds,omitempty"`
	MinExtraRecoveryAttempts   int      `json:"min_extra_recovery_attempts,omitempty"`
	RequireCompleted           bool     `json:"require_completed,omitempty"`
	RequireEvidenceCheckPassed bool     `json:"require_evidence_check_passed,omitempty"`
	AllowedVerification        []string `json:"allowed_verification,omitempty"`
}

type EvalAssertionResult struct {
	ID      string `json:"id"`
	Passed  bool   `json:"passed"`
	Summary string `json:"summary,omitempty"`
}

type EvalCaseRunReport struct {
	ID                  string                   `json:"id"`
	Name                string                   `json:"name"`
	Prompt              string                   `json:"prompt"`
	SessionKey          string                   `json:"session_key"`
	Status              string                   `json:"status"`
	RunID               string                   `json:"run_id,omitempty"`
	RunStatus           string                   `json:"run_status,omitempty"`
	Outcome             RunOutcome               `json:"outcome,omitempty"`
	Harness             *agent.RunHarnessSummary `json:"harness,omitempty"`
	VerificationStatus  string                   `json:"verification_status,omitempty"`
	VerificationSummary string                   `json:"verification_summary,omitempty"`
	FalseSuccess        bool                     `json:"false_success,omitempty"`
	ExecutionTraceCount int                      `json:"execution_trace_count,omitempty"`
	ExpectedSurface     string                   `json:"expected_surface,omitempty"`
	ObservedSurfaces    []string                 `json:"observed_surfaces,omitempty"`
	DurationMS          int64                    `json:"duration_ms,omitempty"`
	Assertions          []EvalAssertionResult    `json:"assertions,omitempty"`
	Error               string                   `json:"error,omitempty"`
}

type EvalSuiteRunReport struct {
	Suite      EvalSuite           `json:"suite"`
	Status     string              `json:"status"`
	StartedAt  string              `json:"started_at,omitempty"`
	FinishedAt string              `json:"finished_at,omitempty"`
	DurationMS int64               `json:"duration_ms,omitempty"`
	CaseCount  int                 `json:"case_count"`
	Passed     int                 `json:"passed"`
	Failed     int                 `json:"failed"`
	Errored    int                 `json:"errored"`
	Quality    *QualitySummary     `json:"quality,omitempty"`
	Cases      []EvalCaseRunReport `json:"cases,omitempty"`
}

func (s *Service) ListEvalSuites() []EvalSuite {
	return cloneEvalSuites(builtinEvalSuites())
}

func (s *Service) RunEvalSuite(ctx context.Context, req EvalRunRequest) (*EvalSuiteRunReport, error) {
	suite, err := lookupEvalSuite(req.SuiteID)
	if err != nil {
		return nil, err
	}
	runner := s.evalRunner
	if runner == nil {
		runner = serviceEvalRunner{service: s}
	}
	return executeEvalSuite(ctx, runner, req, suite)
}

func executeEvalSuite(ctx context.Context, runner EvalRunner, req EvalRunRequest, suite EvalSuite) (*EvalSuiteRunReport, error) {
	if runner == nil {
		return nil, fmt.Errorf("eval runner is required")
	}
	selectedCases, err := selectEvalCases(suite, req.CaseIDs)
	if err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()
	report := &EvalSuiteRunReport{
		Suite:     cloneEvalSuite(suite),
		Status:    "passed",
		StartedAt: startedAt.Format(time.RFC3339),
		CaseCount: len(selectedCases),
		Cases:     make([]EvalCaseRunReport, 0, len(selectedCases)),
	}

	snapshots := make([]qualityRunSnapshot, 0, len(selectedCases))
	for index, evalCase := range selectedCases {
		sessionKey := buildEvalSessionKey(req.SessionPrefix, suite.ID, evalCase.ID, index)
		execReq := EvalExecutionRequest{
			Submit: SubmitRequest{
				SessionKey:   sessionKey,
				Content:      evalCase.Prompt,
				Model:        strings.TrimSpace(req.Model),
				AutomationID: strings.TrimSpace(req.AutomationID),
				Metadata: map[string]any{
					"eval_suite_id": suite.ID,
					"eval_case_id":  evalCase.ID,
				},
			},
			Timeout:      evalTimeout(req),
			PollInterval: evalPollInterval(req),
			HarnessOnly:  evalCase.HarnessOnly,
		}
		result, runErr := runner.Execute(ctx, execReq)
		caseReport := evaluateEvalCaseRun(evalCase, sessionKey, result, runErr)
		report.Cases = append(report.Cases, caseReport)
		switch caseReport.Status {
		case "passed":
			report.Passed++
		case "error":
			report.Errored++
			report.Status = "failed"
		default:
			report.Failed++
			report.Status = "failed"
		}
		if result != nil && result.Run != nil && result.Completion != nil {
			snapshots = append(snapshots, qualityRunSnapshot{
				run:        result.Run,
				completion: result.Completion,
			})
		}
	}

	finishedAt := time.Now().UTC()
	report.FinishedAt = finishedAt.Format(time.RFC3339)
	report.DurationMS = finishedAt.Sub(startedAt).Milliseconds()
	report.Quality = buildQualitySummary(QualitySummaryRequest{Limit: len(snapshots)}, snapshots)
	return report, nil
}

type serviceEvalRunner struct {
	service *Service
}

func (r serviceEvalRunner) Execute(ctx context.Context, req EvalExecutionRequest) (*EvalExecutionResult, error) {
	if r.service == nil {
		return nil, fmt.Errorf("runtime service is required")
	}
	submitReq := req.Submit
	if submitReq.Execute == nil {
		execute := true
		submitReq.Execute = &execute
	}
	run, err := r.service.Submit(ctx, submitReq)
	if err != nil {
		return nil, err
	}
	harness := r.service.BuildRunHarnessSummary(ctx, run)
	if req.HarnessOnly {
		return &EvalExecutionResult{Run: run, Harness: harness}, nil
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultEvalTimeout
	}
	pollInterval := req.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultEvalPollInterval
	}
	deadline := time.Now().Add(timeout)
	current := run
	for time.Now().Before(deadline) {
		current, err = r.service.GetRun(ctx, run.ID)
		if err != nil {
			return &EvalExecutionResult{Run: run}, err
		}
		if isQualityTerminalStatus(current.Status) {
			completion, completionErr := r.service.GetRunCompletion(ctx, current.ID)
			if completionErr != nil {
				return &EvalExecutionResult{Run: current}, completionErr
			}
			return &EvalExecutionResult{Run: current, Completion: completion, Harness: r.service.BuildRunHarnessSummary(ctx, current)}, nil
		}
		select {
		case <-ctx.Done():
			return &EvalExecutionResult{Run: current}, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	current, err = r.service.GetRun(ctx, run.ID)
	if err != nil {
		return &EvalExecutionResult{Run: run}, fmt.Errorf("eval timeout and failed to refresh run %s: %w", run.ID, err)
	}
	completion, completionErr := r.service.GetRunCompletion(ctx, current.ID)
	if completionErr != nil {
		return &EvalExecutionResult{Run: current, Harness: r.service.BuildRunHarnessSummary(ctx, current)}, fmt.Errorf("eval timeout for run %s", run.ID)
	}
	return &EvalExecutionResult{Run: current, Completion: completion, Harness: r.service.BuildRunHarnessSummary(ctx, current)}, fmt.Errorf("eval timeout for run %s", run.ID)
}

func evaluateEvalCaseRun(evalCase EvalCase, sessionKey string, result *EvalExecutionResult, runErr error) EvalCaseRunReport {
	report := EvalCaseRunReport{
		ID:              evalCase.ID,
		Name:            evalCase.Name,
		Prompt:          evalCase.Prompt,
		SessionKey:      sessionKey,
		Status:          "passed",
		ExpectedSurface: strings.TrimSpace(evalCase.ExpectedSurface),
	}
	if result != nil && result.Run != nil {
		report.RunID = strings.TrimSpace(result.Run.ID)
		if durationMS, ok := qualityRunDurationMS(result.Run); ok {
			report.DurationMS = durationMS
		}
	}
	if result != nil && result.Harness != nil {
		report.Harness = result.Harness
	}
	if runErr != nil {
		report.Status = "error"
		report.Error = strings.TrimSpace(runErr.Error())
	}
	if evalCase.HarnessOnly {
		evaluateHarnessOnlyCase(&report, evalCase)
		return report
	}
	if result == nil || result.Completion == nil {
		if report.Status == "passed" {
			report.Status = "failed"
			report.Error = "missing run completion"
		}
		return report
	}

	completion := result.Completion
	report.RunStatus = strings.TrimSpace(string(completion.Status))
	report.Outcome = completion.Outcome
	if completion.Verification != nil {
		report.VerificationStatus = strings.TrimSpace(string(completion.Verification.Status))
		report.VerificationSummary = strings.TrimSpace(completion.Verification.Summary)
		report.FalseSuccess = completion.Status == agent.RunCompleted && completion.Verification.Status == verifyrt.StatusFailed
	}
	traces := qualityExecutionTraces(completion)
	report.ExecutionTraceCount = len(traces)
	report.ObservedSurfaces = evalObservedSurfaces(traces, completion.Verification)

	assertions := make([]EvalAssertionResult, 0, 4)
	appendAssertion := func(id string, passed bool, summary string) {
		assertions = append(assertions, EvalAssertionResult{
			ID:      id,
			Passed:  passed,
			Summary: strings.TrimSpace(summary),
		})
		if !passed && report.Status == "passed" {
			report.Status = "failed"
		}
	}

	appendAssertion("no_false_success", !report.FalseSuccess, "run must not report success while verification fails")

	if evalCase.RequireCompleted {
		appendAssertion(
			"completed",
			completion.Status == agent.RunCompleted,
			fmt.Sprintf("expected completed status, got %s", completion.Status),
		)
	}
	if len(evalCase.AllowedVerification) > 0 {
		allowed := false
		actual := strings.TrimSpace(report.VerificationStatus)
		for _, candidate := range evalCase.AllowedVerification {
			if actual == strings.TrimSpace(candidate) {
				allowed = true
				break
			}
		}
		appendAssertion(
			"verification",
			allowed,
			fmt.Sprintf("expected verification in %v, got %s", evalCase.AllowedVerification, actual),
		)
	}
	if surface := strings.TrimSpace(evalCase.ExpectedSurface); surface != "" {
		present := false
		for _, observed := range report.ObservedSurfaces {
			if observed == surface {
				present = true
				break
			}
		}
		appendAssertion(
			"expected_surface",
			present,
			fmt.Sprintf("expected %s execution evidence", surface),
		)
		if evalCase.RequireEvidenceCheckPassed {
			appendAssertion(
				"surface_evidence",
				evalSurfaceEvidencePassed(surface, completion.Verification),
				fmt.Sprintf("expected %s evidence verification to pass", surface),
			)
		}
	}

	report.Assertions = assertions
	return report
}

func evaluateHarnessOnlyCase(report *EvalCaseRunReport, evalCase EvalCase) {
	assertions := make([]EvalAssertionResult, 0, 3)
	appendAssertion := func(id string, passed bool, summary string) {
		assertions = append(assertions, EvalAssertionResult{
			ID:      id,
			Passed:  passed,
			Summary: strings.TrimSpace(summary),
		})
		if !passed && report.Status == "passed" {
			report.Status = "failed"
		}
	}
	if report.Harness == nil {
		appendAssertion("harness_present", false, "expected harness summary to be present")
		report.Assertions = assertions
		if report.Error == "" {
			report.Error = "missing harness summary"
		}
		return
	}
	appendAssertion("harness_present", true, "harness summary captured")
	if want := strings.TrimSpace(evalCase.ExpectedHarnessIntent); want != "" {
		appendAssertion(
			"harness_intent",
			strings.TrimSpace(report.Harness.TransparentRecoveryIntent) == want,
			fmt.Sprintf("expected harness transparent recovery intent %q, got %q", want, report.Harness.TransparentRecoveryIntent),
		)
	}
	if evalCase.RequireThinkingModel {
		appendAssertion(
			"harness_thinking_model",
			report.Harness.RequireThinkingModel,
			"expected harness to require a thinking-capable model",
		)
	}
	if len(evalCase.ExpectedHarnessDomains) > 0 {
		missing := make([]string, 0, len(evalCase.ExpectedHarnessDomains))
		for _, want := range evalCase.ExpectedHarnessDomains {
			found := false
			for _, got := range report.Harness.Domains {
				if strings.TrimSpace(got) == strings.TrimSpace(want) {
					found = true
					break
				}
			}
			if !found {
				missing = append(missing, want)
			}
		}
		appendAssertion(
			"harness_domains",
			len(missing) == 0,
			fmt.Sprintf("expected harness domains to include %v, missing %v", evalCase.ExpectedHarnessDomains, missing),
		)
	}
	if evalCase.MinExtraToolRounds > 0 {
		appendAssertion(
			"harness_extra_tool_rounds",
			report.Harness.ExtraToolRounds >= evalCase.MinExtraToolRounds,
			fmt.Sprintf("expected harness extra_tool_rounds >= %d, got %d", evalCase.MinExtraToolRounds, report.Harness.ExtraToolRounds),
		)
	}
	if evalCase.MinExtraRecoveryAttempts > 0 {
		appendAssertion(
			"harness_extra_recovery_attempts",
			report.Harness.ExtraRecoveryAttempts >= evalCase.MinExtraRecoveryAttempts,
			fmt.Sprintf("expected harness extra_recovery_attempts >= %d, got %d", evalCase.MinExtraRecoveryAttempts, report.Harness.ExtraRecoveryAttempts),
		)
	}
	report.Assertions = assertions
}

func evalObservedSurfaces(traces []capprofile.ExecutionTrace, verification *verifyrt.RunVerification) []string {
	seen := make(map[string]struct{}, 4)
	for _, trace := range traces {
		surface := qualityTraceSurface(trace.Normalized())
		if surface == "" {
			continue
		}
		seen[surface] = struct{}{}
	}
	if verification != nil {
		for _, check := range verification.Checks {
			if surface := qualitySurfaceForVerificationCheck(check.Name); surface != "" {
				seen[surface] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for surface := range seen {
		out = append(out, surface)
	}
	sort.Strings(out)
	return out
}

func evalSurfaceEvidencePassed(surface string, verification *verifyrt.RunVerification) bool {
	if verification == nil {
		return false
	}
	surface = strings.TrimSpace(strings.ToLower(surface))
	for _, check := range verification.Checks {
		if qualitySurfaceForVerificationCheck(check.Name) != surface {
			continue
		}
		return check.Status == verifyrt.StatusPassed
	}
	return false
}

func selectEvalCases(suite EvalSuite, caseIDs []string) ([]EvalCase, error) {
	if len(caseIDs) == 0 {
		return append([]EvalCase(nil), suite.Cases...), nil
	}
	index := make(map[string]EvalCase, len(suite.Cases))
	for _, evalCase := range suite.Cases {
		index[evalCase.ID] = evalCase
	}
	selected := make([]EvalCase, 0, len(caseIDs))
	for _, caseID := range caseIDs {
		evalCase, ok := index[strings.TrimSpace(caseID)]
		if !ok {
			return nil, fmt.Errorf("eval case %q not found in suite %q", strings.TrimSpace(caseID), suite.ID)
		}
		selected = append(selected, evalCase)
	}
	return selected, nil
}

func lookupEvalSuite(id string) (EvalSuite, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return EvalSuite{}, fmt.Errorf("suite_id is required")
	}
	for _, suite := range builtinEvalSuites() {
		if suite.ID == id {
			return cloneEvalSuite(suite), nil
		}
	}
	return EvalSuite{}, fmt.Errorf("eval suite %q not found", id)
}

func buildEvalSessionKey(prefix, suiteID, caseID string, index int) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "eval"
	}
	return fmt.Sprintf("%s:%s:%s:%d", prefix, strings.TrimSpace(suiteID), strings.TrimSpace(caseID), index+1)
}

func evalTimeout(req EvalRunRequest) time.Duration {
	if req.TimeoutMS > 0 {
		return time.Duration(req.TimeoutMS) * time.Millisecond
	}
	return defaultEvalTimeout
}

func evalPollInterval(req EvalRunRequest) time.Duration {
	if req.PollIntervalMS > 0 {
		return time.Duration(req.PollIntervalMS) * time.Millisecond
	}
	return defaultEvalPollInterval
}

func cloneEvalSuites(items []EvalSuite) []EvalSuite {
	if len(items) == 0 {
		return nil
	}
	out := make([]EvalSuite, 0, len(items))
	for _, item := range items {
		out = append(out, cloneEvalSuite(item))
	}
	return out
}

func cloneEvalSuite(item EvalSuite) EvalSuite {
	out := item
	if len(item.Prerequisites) > 0 {
		out.Prerequisites = append([]string(nil), item.Prerequisites...)
	}
	if len(item.Cases) > 0 {
		out.Cases = make([]EvalCase, len(item.Cases))
		for i, evalCase := range item.Cases {
			out.Cases[i] = evalCase
			if len(evalCase.AllowedVerification) > 0 {
				out.Cases[i].AllowedVerification = append([]string(nil), evalCase.AllowedVerification...)
			}
		}
	}
	return out
}

func builtinEvalSuites() []EvalSuite {
	return []EvalSuite{
		{
			ID:          "browser.smoke",
			Name:        "Browser Smoke",
			Description: "Basic browser navigation, form interaction, and evidence verification.",
			Surface:     "browser",
			Cases: []EvalCase{
				{
					ID:                         "read_example_domain",
					Name:                       "Read Example Domain",
					Prompt:                     "打开 https://example.com，告诉我标题和首段内容",
					ExpectedSurface:            "browser",
					RequireCompleted:           true,
					RequireEvidenceCheckPassed: true,
					AllowedVerification:        []string{"passed", "warning"},
				},
				{
					ID:                         "submit_httpbin_form",
					Name:                       "Submit HTTPBin Form",
					Prompt:                     "打开 https://httpbin.org/forms/post，填写表单并提交",
					ExpectedSurface:            "browser",
					RequireCompleted:           true,
					RequireEvidenceCheckPassed: true,
					AllowedVerification:        []string{"passed", "warning"},
				},
			},
		},
		{
			ID:          "desktop.smoke",
			Name:        "Desktop Smoke",
			Description: "Generic desktop window, clipboard, and screenshot operations.",
			Surface:     "desktop",
			Cases: []EvalCase{
				{
					ID:                         "list_windows",
					Name:                       "List Open Windows",
					Prompt:                     "列出当前打开的应用和窗口，告诉我哪个最像浏览器",
					ExpectedSurface:            "desktop",
					RequireCompleted:           true,
					RequireEvidenceCheckPassed: true,
					AllowedVerification:        []string{"passed", "warning"},
				},
				{
					ID:                         "screen_capture",
					Name:                       "Capture Desktop Screenshot",
					Prompt:                     "给当前桌面截个图，告诉我屏幕上最显眼的窗口标题",
					ExpectedSurface:            "desktop",
					RequireCompleted:           true,
					RequireEvidenceCheckPassed: true,
					AllowedVerification:        []string{"passed", "warning"},
				},
			},
		},
		{
			ID:          "desktop.media_apps.smoke",
			Name:        "Desktop Media Apps Smoke",
			Description: "High-value app-profile smoke coverage for media and editor workflows.",
			Surface:     "desktop",
			Prerequisites: []string{
				"Requires supported desktop apps such as Douyin, QQMusic, or a timeline editor to be installed and permitted.",
			},
			Cases: []EvalCase{
				{
					ID:                         "douyin_search_open",
					Name:                       "Douyin Search And Open",
					Prompt:                     "打开抖音，搜索“刘德华”，并点开一个搜索结果",
					ExpectedSurface:            "desktop",
					RequireCompleted:           true,
					RequireEvidenceCheckPassed: true,
					AllowedVerification:        []string{"passed", "warning"},
				},
				{
					ID:                         "qqmusic_search_capture",
					Name:                       "QQMusic Search And Capture",
					Prompt:                     "打开 QQ音乐，搜索“发如雪”，然后截图保存到桌面",
					ExpectedSurface:            "desktop",
					RequireCompleted:           true,
					RequireEvidenceCheckPassed: true,
					AllowedVerification:        []string{"passed", "warning"},
				},
				{
					ID:                         "timeline_editor_play",
					Name:                       "Timeline Editor Play",
					Prompt:                     "打开视频剪辑软件的第一个历史项目，然后点击播放",
					ExpectedSurface:            "desktop",
					RequireCompleted:           true,
					RequireEvidenceCheckPassed: true,
					AllowedVerification:        []string{"passed", "warning"},
				},
			},
		},
		{
			ID:          "harness.recovery",
			Name:        "Harness Recovery",
			Description: "Result-first harness routing and transparent capability recovery classification.",
			Surface:     "harness",
			Cases: []EvalCase{
				{
					ID:                       "CS01_rss_discovery",
					Name:                     "CS01 RSS Discovery",
					Prompt:                   "读取这个 RSS feed 并总结最近三条：https://example.com/feed.xml",
					HarnessOnly:              true,
					ExpectedHarnessIntent:    "rss",
					ExpectedHarnessDomains:   []string{"browser", "net", "news"},
					RequireThinkingModel:     true,
					MinExtraToolRounds:       2,
					MinExtraRecoveryAttempts: 2,
				},
				{
					ID:                       "CS03_email_search",
					Name:                     "CS03 Email Search",
					Prompt:                   "搜索我邮箱里最近关于 invoice 的邮件",
					HarnessOnly:              true,
					ExpectedHarnessIntent:    "email",
					ExpectedHarnessDomains:   []string{"email"},
					RequireThinkingModel:     true,
					MinExtraToolRounds:       2,
					MinExtraRecoveryAttempts: 2,
				},
				{
					ID:                     "C14_network_request",
					Name:                   "C14 Network Request",
					Prompt:                 "访问 https://example.com 和 https://example.org，比较两页内容差异后总结成表格",
					HarnessOnly:            true,
					ExpectedHarnessDomains: []string{"browser", "net", "web"},
					RequireThinkingModel:   true,
					MinExtraToolRounds:     2,
				},
			},
		},
	}
}
