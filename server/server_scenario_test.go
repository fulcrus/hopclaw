package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/policy"
	rt "github.com/fulcrus/hopclaw/runtime"
)

func TestHTTP_SubmitAndCompleteRun(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "scenario complete",
				},
			}},
		},
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "http-submit-complete",
		"content":     "finish the task",
	}, http.StatusAccepted)
	if run.Status != agent.RunQueued {
		t.Fatalf("run.Status = %q, want %q", run.Status, agent.RunQueued)
	}

	completed := waitForRunStatus(t, handler, run.ID, agent.RunCompleted)
	if completed.Status != agent.RunCompleted {
		t.Fatalf("completed.Status = %q, want %q", completed.Status, agent.RunCompleted)
	}

	result := getRunResult(t, handler, run.ID)
	if result.RunID != run.ID {
		t.Fatalf("result.RunID = %q, want %q", result.RunID, run.ID)
	}
	if result.Outcome != rt.RunOutcomeCompleted {
		t.Fatalf("result.Outcome = %q, want %q", result.Outcome, rt.RunOutcomeCompleted)
	}
	if result.Output != "scenario complete" {
		t.Fatalf("result.Output = %q, want %q", result.Output, "scenario complete")
	}
}

func TestHTTP_ListRuns_Empty(t *testing.T) {
	t.Parallel()

	handler := New(newRuntimeService(t, runtimeFixture{}), Config{}).Handler()

	rec := doScenarioRequest(t, handler, http.MethodGet, "/runtime/runs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Count int `json:"count"`
	}
	decodeScenarioBody(t, rec, &payload)
	if payload.Count != 0 {
		t.Fatalf("payload.Count = %d, want 0", payload.Count)
	}
	if len(payload.Items) != 0 {
		t.Fatalf("len(payload.Items) = %d, want 0", len(payload.Items))
	}
}

func TestHTTP_GetRun_NotFound(t *testing.T) {
	t.Parallel()

	handler := New(newRuntimeService(t, runtimeFixture{}), Config{}).Handler()

	rec := doScenarioRequest(t, handler, http.MethodGet, "/runtime/runs/nonexistent", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /runtime/runs/nonexistent status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_ListSessions(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done",
				},
			}},
		},
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "http-session-list",
		"content":     "hello",
	}, http.StatusAccepted)
	if run.SessionID == "" {
		t.Fatal("expected run to include session id")
	}

	rec := doScenarioRequest(t, handler, http.MethodGet, "/runtime/sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/sessions status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []agent.SessionSummary `json:"items"`
		Count int                    `json:"count"`
	}
	decodeScenarioBody(t, rec, &payload)
	if payload.Count == 0 || len(payload.Items) == 0 {
		t.Fatalf("expected non-empty session list, got %+v", payload)
	}
	found := false
	for _, item := range payload.Items {
		if item.Key == "http-session-list" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("session http-session-list not found in %+v", payload.Items)
	}
}

func TestHTTP_MemoryGetSetDelete(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{}).WithMemoryStore(agent.NewInMemoryKVStore())
	handler := New(svc, Config{}).Handler()

	rec := doScenarioRequest(t, handler, http.MethodPut, "/runtime/memory/user.name", map[string]any{
		"value": "Alice",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /runtime/memory/user.name status = %d body=%s", rec.Code, rec.Body.String())
	}

	var ok healthResponse
	decodeScenarioBody(t, rec, &ok)
	if !ok.OK {
		t.Fatalf("PUT ok = %#v, want ok=true", ok)
	}

	rec = doScenarioRequest(t, handler, http.MethodGet, "/runtime/memory/user.name", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/memory/user.name status = %d body=%s", rec.Code, rec.Body.String())
	}
	var entry agent.MemoryEntry
	decodeScenarioBody(t, rec, &entry)
	if entry.Key != "user.name" {
		t.Fatalf("entry.Key = %q, want %q", entry.Key, "user.name")
	}
	if entry.Value != "Alice" {
		t.Fatalf("entry.Value = %q, want %q", entry.Value, "Alice")
	}

	rec = doScenarioRequest(t, handler, http.MethodDelete, "/runtime/memory/user.name", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /runtime/memory/user.name status = %d body=%s", rec.Code, rec.Body.String())
	}
	decodeScenarioBody(t, rec, &ok)
	if !ok.OK {
		t.Fatalf("DELETE ok = %#v, want ok=true", ok)
	}

	rec = doScenarioRequest(t, handler, http.MethodGet, "/runtime/memory/user.name", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /runtime/memory/user.name after delete status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_ApprovalWorkflow(t *testing.T) {
	t.Parallel()

	skillSvc := newSkillService(t, "fs.write", "local_write")
	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{
				{
					ToolCalls: []agent.ToolCall{{
						ID:   "call-1",
						Name: "fs.write",
					}},
				},
				{
					Message: contextengine.Message{
						Role:    contextengine.RoleAssistant,
						Content: "approved and finished",
					},
				},
			},
		},
		tools: serverToolExecutor{
			results: []contextengine.ToolResult{{
				ToolName:   "fs.write",
				ToolCallID: "call-1",
				Content:    "written",
			}},
		},
		policy: policy.NewDefaultEngine(policy.Config{
			RequireApprovalForWrite: true,
		}),
		approvals: approval.NewInMemoryStore(),
		skills:    skillSvc,
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "http-approval-workflow",
		"content":     "write file",
	}, http.StatusAccepted)
	run = waitForRunStatus(t, handler, run.ID, agent.RunWaitingApproval)
	if run.ApprovalID == "" {
		t.Fatal("expected run to have pending approval id")
	}

	items := listApprovals(t, handler, "pending")
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].ID != run.ApprovalID {
		t.Fatalf("items[0].ID = %q, want %q", items[0].ID, run.ApprovalID)
	}

	view := resolveApproval(t, handler, run.ApprovalID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	})
	if view.Status != approval.StatusApproved {
		t.Fatalf("view.Status = %q, want %q", view.Status, approval.StatusApproved)
	}

	run = waitForRunStatus(t, handler, run.ID, agent.RunCompleted)
	if run.Status != agent.RunCompleted {
		t.Fatalf("run.Status = %q, want %q", run.Status, agent.RunCompleted)
	}
}

func TestHTTP_CancelRun(t *testing.T) {
	t.Parallel()

	handler := New(newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	}), Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "http-cancel-run",
		"content":     "cancel me",
		"execute":     false,
	}, http.StatusCreated)
	if run.Status != agent.RunQueued {
		t.Fatalf("run.Status = %q, want %q", run.Status, agent.RunQueued)
	}

	rec := doScenarioRequest(t, handler, http.MethodPost, "/runtime/runs/"+run.ID+"/cancel", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /runtime/runs/%s/cancel status = %d body=%s", run.ID, rec.Code, rec.Body.String())
	}

	var cancelled agent.Run
	decodeScenarioBody(t, rec, &cancelled)
	if cancelled.Status != agent.RunCancelled {
		t.Fatalf("cancelled.Status = %q, want %q", cancelled.Status, agent.RunCancelled)
	}

	latest := getRun(t, handler, run.ID)
	if latest.Status != agent.RunCancelled {
		t.Fatalf("latest.Status = %q, want %q", latest.Status, agent.RunCancelled)
	}
}

func TestHTTP_HealthCheck(t *testing.T) {
	t.Parallel()

	handler := New(newRuntimeService(t, runtimeFixture{}), Config{AuthToken: "test-token"}).Handler()

	rec := doScenarioRequest(t, handler, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d body=%s", rec.Code, rec.Body.String())
	}

	var health healthResponse
	decodeScenarioBody(t, rec, &health)
	if !health.OK {
		t.Fatalf("health = %#v, want ok=true", health)
	}
}

func doScenarioRequest(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("Marshal(body) error = %v", err)
		}
		req = httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeScenarioBody(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()

	if err := json.NewDecoder(rec.Body).Decode(target); err != nil {
		t.Fatalf("Decode(response) error = %v body=%s", err, rec.Body.String())
	}
}
