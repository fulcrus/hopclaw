package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	rtverify "github.com/fulcrus/hopclaw/runtime/verify"
	"github.com/fulcrus/hopclaw/server"
)

func TestGatewayOperatorQualitySummary(t *testing.T) {
	t.Parallel()

	gw := newRunnableTestGateway(t)
	handler := gw.Handler()

	runID := gatewayPostRun(t, handler, "operator-quality", "say done")
	gatewayWaitForRunStatus(t, handler, runID, agent.RunCompleted)

	rec := doRequest(t, handler, http.MethodGet, "/operator/quality/summary", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/quality/summary status = %d body=%s", rec.Code, rec.Body.String())
	}

	var summary runtimepkg.QualitySummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("Decode(summary) error = %v", err)
	}
	if summary.RunCount != 1 {
		t.Fatalf("RunCount = %d, want 1", summary.RunCount)
	}
}

func TestGatewayOperatorReleaseReadiness(t *testing.T) {
	t.Parallel()

	gw := newRunnableTestGateway(t)
	handler := gw.Handler()

	for i := 0; i < 5; i++ {
		runID := gatewayPostRun(t, handler, "operator-release", "say done")
		gatewayWaitForRunStatus(t, handler, runID, agent.RunCompleted)
	}

	rec := doRequest(t, handler, http.MethodGet, "/operator/quality/release-readiness", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/quality/release-readiness status = %d body=%s", rec.Code, rec.Body.String())
	}

	var report runtimepkg.ReleaseReadinessReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("Decode(report) error = %v", err)
	}
	if !report.Ready {
		t.Fatalf("Ready = false, blockers=%#v", report.Blockers)
	}
}

func TestGatewayOperatorEvalEndpoints(t *testing.T) {
	t.Parallel()

	gw := newRunnableTestGateway(t)
	gw.runtime.WithEvalRunner(gatewayStaticEvalRunner{
		result: gatewayEvalExecutionResult("gateway-eval-run"),
	})
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/evals/suites", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/evals/suites status = %d body=%s", rec.Code, rec.Body.String())
	}
	var suites countedItemsResponse
	if err := json.NewDecoder(rec.Body).Decode(&suites); err != nil {
		t.Fatalf("Decode(suites) error = %v", err)
	}
	if suites.Count == 0 {
		t.Fatal("expected built-in eval suites")
	}

	rec = doRequest(t, handler, http.MethodPost, "/operator/evals/run", `{"suite_id":"browser.smoke","case_ids":["read_example_domain"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /operator/evals/run status = %d body=%s", rec.Code, rec.Body.String())
	}
	var report runtimepkg.EvalSuiteRunReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("Decode(report) error = %v", err)
	}
	if report.Passed != 1 || report.Status != "passed" {
		t.Fatalf("report = %#v", report)
	}
}

func TestGatewayOperatorEvalRunRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw := newRunnableTestGateway(t)
	gw.runtime.WithEvalRunner(gatewayStaticEvalRunner{
		result: gatewayEvalExecutionResult("gateway-eval-run"),
	})

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/evals/run", `{"suite_id":"browser.smoke","case_ids":["read_example_domain"]} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/evals/run trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func newRunnableTestGateway(t *testing.T) *Gateway {
	t.Helper()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	engine := contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "You are a gateway test runtime.",
		IncludeSkillCatalog:  false,
		DefaultContextWindow: 512,
		DefaultOutputTokens:  64,
	}, nil)
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), engine, gatewayTestModelClient{}, nil, nil)
	runtimeSvc := runtimepkg.NewService(component, sessions, runs, nil, bus, nil)
	srv := server.New(runtimeSvc, server.Config{AuthToken: "test-token"})
	return gatewayFromServer(srv, Config{
		AuthToken: "test-token",
		Runtime:   runtimeSvc,
	})
}

func gatewayPostRun(t *testing.T, handler http.Handler, sessionKey, content string) string {
	t.Helper()

	rec := doRequest(t, handler, http.MethodPost, "/runtime/runs", `{"session_key":"`+sessionKey+`","content":"`+content+`"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /runtime/runs status = %d body=%s", rec.Code, rec.Body.String())
	}
	var run agent.Run
	if err := json.NewDecoder(rec.Body).Decode(&run); err != nil {
		t.Fatalf("Decode(run) error = %v", err)
	}
	return run.ID
}

func gatewayWaitForRunStatus(t *testing.T, handler http.Handler, runID string, want agent.RunStatus) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec := doRequest(t, handler, http.MethodGet, "/runtime/runs/"+runID, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /runtime/runs/%s status = %d body=%s", runID, rec.Code, rec.Body.String())
		}
		var run agent.Run
		if err := json.NewDecoder(rec.Body).Decode(&run); err != nil {
			t.Fatalf("Decode(run) error = %v", err)
		}
		if run.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach status %q", runID, want)
}

type gatewayTestModelClient struct{}

func (gatewayTestModelClient) Chat(context.Context, agent.ChatRequest) (*agent.ModelResponse, error) {
	return &agent.ModelResponse{
		Message: contextengine.Message{
			Role:    contextengine.RoleAssistant,
			Content: "done",
		},
	}, nil
}

type gatewayStaticEvalRunner struct {
	result *runtimepkg.EvalExecutionResult
	err    error
}

func (r gatewayStaticEvalRunner) Execute(context.Context, runtimepkg.EvalExecutionRequest) (*runtimepkg.EvalExecutionResult, error) {
	return r.result, r.err
}

func gatewayEvalExecutionResult(runID string) *runtimepkg.EvalExecutionResult {
	startedAt := time.Unix(100, 0).UTC()
	finishedAt := startedAt.Add(2 * time.Second)
	return &runtimepkg.EvalExecutionResult{
		Run: &agent.Run{
			ID:         runID,
			SessionID:  "gateway-eval-session",
			Status:     agent.RunCompleted,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			UpdatedAt:  finishedAt,
		},
		Completion: &runtimepkg.RunCompletion{
			RunID:   runID,
			Status:  agent.RunCompleted,
			Outcome: runtimepkg.RunOutcomeCompleted,
			Result: &runtimepkg.RunResult{
				RunID:  runID,
				Status: agent.RunCompleted,
				ExecutionTraces: []capprofile.ExecutionTrace{{
					Surface:         "browser",
					Capability:      "browser",
					ChosenTransport: capprofile.TransportBrowserDOM,
					ProfileHit:      true,
				}},
			},
			Verification: &rtverify.RunVerification{
				RunID:   runID,
				Status:  rtverify.StatusPassed,
				Summary: "verification passed",
				Checks: []rtverify.Check{{
					Name:   "browser.result",
					Status: rtverify.StatusPassed,
				}},
			},
		},
	}
}
