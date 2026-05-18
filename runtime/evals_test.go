package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

func TestServiceRunEvalSuiteUsesConfiguredRunner(t *testing.T) {
	t.Parallel()

	svc := (&Service{}).WithEvalRunner(&evalSequenceRunner{
		results: []*EvalExecutionResult{
			runtimeEvalExecutionResult("eval-pass", "browser", agent.RunCompleted, RunOutcomeCompleted, verifyrt.StatusPassed, false),
		},
	})

	report, err := svc.RunEvalSuite(context.Background(), EvalRunRequest{
		SuiteID: "browser.smoke",
		CaseIDs: []string{"read_example_domain"},
	})
	if err != nil {
		t.Fatalf("RunEvalSuite() error = %v", err)
	}
	if report.CaseCount != 1 || report.Passed != 1 || report.Status != "passed" {
		t.Fatalf("report = %#v", report)
	}
	if report.Quality == nil || report.Quality.RunCount != 1 {
		t.Fatalf("Quality = %#v", report.Quality)
	}
}

func TestExecuteEvalSuiteMarksFalseSuccessFailure(t *testing.T) {
	t.Parallel()

	suite := EvalSuite{
		ID:   "suite.test",
		Name: "suite",
		Cases: []EvalCase{{
			ID:                         "case-1",
			Name:                       "case",
			Prompt:                     "do something",
			ExpectedSurface:            "desktop",
			RequireCompleted:           true,
			RequireEvidenceCheckPassed: true,
			AllowedVerification:        []string{"passed", "warning"},
		}},
	}
	report, err := executeEvalSuite(context.Background(), &evalSequenceRunner{
		results: []*EvalExecutionResult{
			runtimeEvalExecutionResult("eval-false-success", "desktop", agent.RunCompleted, RunOutcomeCompleted, verifyrt.StatusFailed, true),
		},
	}, EvalRunRequest{SuiteID: suite.ID}, suite)
	if err != nil {
		t.Fatalf("executeEvalSuite() error = %v", err)
	}
	if report.Status != "failed" || report.Failed != 1 {
		t.Fatalf("report = %#v", report)
	}
	if report.Cases[0].FalseSuccess != true {
		t.Fatalf("case report = %#v", report.Cases[0])
	}
	if report.Quality == nil || report.Quality.FalseSuccess.Count != 1 {
		t.Fatalf("Quality = %#v", report.Quality)
	}
}

func TestExecuteEvalSuiteHarnessOnlyCase(t *testing.T) {
	t.Parallel()

	suite := EvalSuite{
		ID:   "suite.harness",
		Name: "suite",
		Cases: []EvalCase{{
			ID:                       "case-harness",
			Name:                     "case",
			Prompt:                   "搜索我邮箱里最近关于 invoice 的邮件",
			HarnessOnly:              true,
			ExpectedHarnessIntent:    "email",
			ExpectedHarnessDomains:   []string{"email"},
			RequireThinkingModel:     true,
			MinExtraToolRounds:       2,
			MinExtraRecoveryAttempts: 2,
		}},
	}
	report, err := executeEvalSuite(context.Background(), &evalSequenceRunner{
		results: []*EvalExecutionResult{{
			Run: &agent.Run{
				ID:        "run-harness",
				SessionID: "session-run-harness",
				Status:    agent.RunQueued,
			},
			Harness: &agent.RunHarnessSummary{
				TransparentRecoveryIntent: "email",
				Domains:                   []string{"email", "text"},
				RequireThinkingModel:      true,
				ExtraToolRounds:           2,
				ExtraRecoveryAttempts:     2,
			},
		}},
	}, EvalRunRequest{SuiteID: suite.ID}, suite)
	if err != nil {
		t.Fatalf("executeEvalSuite() error = %v", err)
	}
	if report.Status != "passed" || report.Passed != 1 {
		t.Fatalf("report = %#v", report)
	}
	if len(report.Cases) != 1 || report.Cases[0].Harness == nil {
		t.Fatalf("case report = %#v", report.Cases)
	}
	if report.Cases[0].Harness.TransparentRecoveryIntent != "email" {
		t.Fatalf("harness = %#v", report.Cases[0].Harness)
	}
	if len(report.Cases[0].Assertions) < 5 {
		t.Fatalf("assertions = %#v, want domain/budget assertions too", report.Cases[0].Assertions)
	}
}

func TestBuiltinEvalSuitesIncludeHarnessRecoverySuite(t *testing.T) {
	t.Parallel()

	suite, err := lookupEvalSuite("harness.recovery")
	if err != nil {
		t.Fatalf("lookupEvalSuite() error = %v", err)
	}
	if len(suite.Cases) < 3 {
		t.Fatalf("suite.Cases = %#v, want >= 3", suite.Cases)
	}
	if suite.Cases[0].ID != "CS01_rss_discovery" || !suite.Cases[0].HarnessOnly {
		t.Fatalf("first harness case = %#v, want CS01 harness-only", suite.Cases[0])
	}
	if suite.Cases[1].ID != "CS03_email_search" {
		t.Fatalf("second harness case = %#v, want CS03 email case", suite.Cases[1])
	}
	if suite.Cases[2].ID != "C14_network_request" {
		t.Fatalf("third harness case = %#v, want C14 network case", suite.Cases[2])
	}
}

type evalSequenceRunner struct {
	results []*EvalExecutionResult
	errs    []error
	index   int
}

func (r *evalSequenceRunner) Execute(context.Context, EvalExecutionRequest) (*EvalExecutionResult, error) {
	result := r.results[r.index]
	var err error
	if len(r.errs) > r.index {
		err = r.errs[r.index]
	}
	r.index++
	return result, err
}

func runtimeEvalExecutionResult(runID, surface string, status agent.RunStatus, outcome RunOutcome, verificationStatus verifyrt.Status, fallback bool) *EvalExecutionResult {
	startedAt := time.Unix(100, 0).UTC()
	finishedAt := startedAt.Add(1500 * time.Millisecond)
	trace := capprofile.ExecutionTrace{
		Surface:         surface,
		Capability:      surface,
		ChosenTransport: capprofile.TransportSemanticUIAction,
		ProfileHit:      true,
	}
	if fallback {
		trace.FallbackPath = []string{capprofile.TransportSemanticUIAction}
		trace.ChosenTransport = capprofile.TransportOCRAnchoredVisual
		trace.ExecutionMode = capprofile.ModeVisualFallback
	}
	checks := []verifyrt.Check{{Name: surface + ".result", Status: verifyrt.StatusPassed}}
	verification := &verifyrt.RunVerification{
		RunID:   runID,
		Status:  verificationStatus,
		Summary: "verification",
		Checks:  checks,
	}
	if verificationStatus == verifyrt.StatusFailed {
		verification.Failures = 1
		verification.RequiredFailures = 1
		verification.Checks = append(verification.Checks, verifyrt.Check{Name: "task.contract", Status: verifyrt.StatusFailed})
	}
	return &EvalExecutionResult{
		Run: &agent.Run{
			ID:         runID,
			SessionID:  "session-" + runID,
			Status:     status,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			UpdatedAt:  finishedAt,
		},
		Completion: &RunCompletion{
			RunID:   runID,
			Status:  status,
			Outcome: outcome,
			Result: &RunResult{
				RunID:           runID,
				Status:          status,
				Outcome:         outcome,
				ExecutionTraces: []capprofile.ExecutionTrace{trace},
			},
			Verification: verification,
		},
	}
}
