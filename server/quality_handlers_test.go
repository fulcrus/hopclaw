package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	rt "github.com/fulcrus/hopclaw/runtime"
	rtverify "github.com/fulcrus/hopclaw/runtime/verify"
)

func TestServerQualitySummaryEndpoint(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: repeatedServerResponses(10, "done"),
		},
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "quality-summary",
		"content":     "say done",
	}, http.StatusAccepted)
	waitForRunStatus(t, handler, run.ID, agent.RunCompleted)

	req := httptest.NewRequest(http.MethodGet, "/runtime/quality/summary?limit=10", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/quality/summary status = %d body=%s", rec.Code, rec.Body.String())
	}

	var summary rt.QualitySummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("Decode(summary) error = %v", err)
	}
	if summary.RunCount != 1 {
		t.Fatalf("RunCount = %d, want 1", summary.RunCount)
	}
	if summary.TaskSuccess.Count != 1 {
		t.Fatalf("TaskSuccess = %#v, want count 1", summary.TaskSuccess)
	}
}

func TestServerReleaseReadinessEndpoint(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	svc := rt.NewService(nil, sessions, runs, nil, eventbus.NewInMemoryBus(), nil)
	handler := New(svc, Config{}).Handler()

	for i := 0; i < 5; i++ {
		seedCompletedRunForReadiness(t, sessions, runs, "release-readiness-"+string(rune('a'+i)), "done")
	}

	req := httptest.NewRequest(http.MethodGet, "/runtime/release-readiness?limit=10", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/release-readiness status = %d body=%s", rec.Code, rec.Body.String())
	}

	var report rt.ReleaseReadinessReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("Decode(report) error = %v", err)
	}
	if !report.Ready {
		t.Fatalf("Ready = false, blockers=%#v", report.Blockers)
	}
	if report.Summary == nil || report.Summary.TerminalRunCount < 5 {
		t.Fatalf("Summary = %#v", report.Summary)
	}
}

func TestServerEvalEndpoints(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{})
	svc.WithEvalRunner(serverStaticEvalRunner{
		result: serverEvalExecutionResult("eval-run-1"),
	})
	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/runtime/evals/suites", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/evals/suites status = %d body=%s", rec.Code, rec.Body.String())
	}
	var suites listResponse
	if err := json.NewDecoder(rec.Body).Decode(&suites); err != nil {
		t.Fatalf("Decode(suites) error = %v", err)
	}
	if suites.Count == 0 {
		t.Fatal("expected built-in eval suites")
	}

	body, err := json.Marshal(rt.EvalRunRequest{
		SuiteID: "browser.smoke",
		CaseIDs: []string{"read_example_domain"},
	})
	if err != nil {
		t.Fatalf("Marshal(body) error = %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/runtime/evals/run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /runtime/evals/run status = %d body=%s", rec.Code, rec.Body.String())
	}

	var report rt.EvalSuiteRunReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("Decode(report) error = %v", err)
	}
	if report.Passed != 1 || report.Status != "passed" {
		t.Fatalf("report = %#v", report)
	}
	if report.Quality == nil || report.Quality.RunCount != 1 {
		t.Fatalf("Quality = %#v", report.Quality)
	}
}

func TestServerEvalRunRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{})
	svc.WithEvalRunner(serverStaticEvalRunner{
		result: serverEvalExecutionResult("eval-run-1"),
	})
	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/runtime/evals/run", bytes.NewBufferString(`{"suite_id":"browser.smoke"} {"extra":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /runtime/evals/run trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}
}

type serverStaticEvalRunner struct {
	result *rt.EvalExecutionResult
	err    error
}

func (r serverStaticEvalRunner) Execute(context.Context, rt.EvalExecutionRequest) (*rt.EvalExecutionResult, error) {
	return r.result, r.err
}

func serverEvalExecutionResult(runID string) *rt.EvalExecutionResult {
	startedAt := time.Unix(100, 0).UTC()
	finishedAt := startedAt.Add(2 * time.Second)
	run := &agent.Run{
		ID:         runID,
		SessionID:  "eval-session",
		Status:     agent.RunCompleted,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
	completion := &rt.RunCompletion{
		RunID:   runID,
		Status:  agent.RunCompleted,
		Outcome: rt.RunOutcomeCompleted,
		Result: &rt.RunResult{
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
	}
	return &rt.EvalExecutionResult{
		Run:        run,
		Completion: completion,
	}
}

func repeatedServerResponses(count int, content string) []*agent.ModelResponse {
	out := make([]*agent.ModelResponse, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, &agent.ModelResponse{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: content,
			},
		})
	}
	return out
}

func seedCompletedRunForReadiness(t *testing.T, sessions *agent.InMemorySessionStore, runs *agent.InMemoryRunStore, sessionKey, output string) {
	t.Helper()

	ctx := context.Background()
	session, err := sessions.GetOrCreate(ctx, sessionKey, "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: sessionKey,
		Content:    "say done",
	}, agent.AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	loaded, unlock, err := sessions.LoadForExecution(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	loaded.Messages = append(loaded.Messages, contextengine.Message{
		Role:    contextengine.RoleAssistant,
		Content: output,
		Metadata: map[string]any{
			meta.KeyRunID: run.ID,
		},
	})
	loaded.MessageCount = len(loaded.Messages)
	if err := sessions.Save(ctx, loaded); err != nil {
		unlock()
		t.Fatalf("sessions.Save() error = %v", err)
	}
	unlock()

	run.Status = agent.RunCompleted
	run.StartedAt = time.Unix(100, 0).UTC()
	run.FinishedAt = run.StartedAt.Add(1500 * time.Millisecond)
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}
}
