package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/hooks"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	rtplanner "github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/policy"
	rt "github.com/fulcrus/hopclaw/runtime"
	rtverify "github.com/fulcrus/hopclaw/runtime/verify"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolruntime"
)

type serverOperationalWarningSourceStub struct {
	warnings []controlplane.OperationalWarning
}

func (s serverOperationalWarningSourceStub) OperationalWarnings() []controlplane.OperationalWarning {
	return append([]controlplane.OperationalWarning(nil), s.warnings...)
}

func TestServerSubmitRunAndGetRun(t *testing.T) {
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
		classifier: serverStubInteractionClassifier{
			decision: rt.InteractionDecision{
				SpeechAct:   rt.SpeechActNewTask,
				TargetScope: rt.TargetScopeNewRun,
				ReplyAct:    rt.ReplyActTaskAccept,
				Confidence:  0.95,
				Reason:      "explicit_new_task",
			},
		},
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key":   "chat-1",
		"content":       "hello",
		"automation_id": "auto-1",
	}, http.StatusAccepted)
	if run.Status != agent.RunQueued {
		t.Fatalf("run.Status = %q", run.Status)
	}
	if run.Scope.AutomationID != "auto-1" {
		t.Fatalf("run.Scope = %#v", run.Scope)
	}

	gotRun := waitForRunStatus(t, handler, run.ID, agent.RunCompleted)
	if gotRun.Status != agent.RunCompleted {
		t.Fatalf("gotRun.Status = %q", gotRun.Status)
	}
	if gotRun.Scope.AutomationID != "auto-1" {
		t.Fatalf("gotRun.Scope = %#v", gotRun.Scope)
	}

	events := getEvents(t, handler, "/runtime/events")
	if len(events) == 0 {
		t.Fatal("expected runtime events")
	}
}

func TestServerSubmitHighRiskRunWaitsForReleaseGateApproval(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: repeatedServerResponses(1, "done"),
		},
		approvals: approval.NewInMemoryStore(),
	})
	svc.WithReleaseExecutionGate(rt.DefaultReleaseExecutionGatePolicy())
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "release-gate-http",
		"content":     "Deploy the latest build to staging.",
	}, http.StatusAccepted)
	if run.Status != agent.RunWaitingApproval {
		t.Fatalf("run.Status = %q, want %q", run.Status, agent.RunWaitingApproval)
	}
	if run.ApprovalID == "" {
		t.Fatal("expected approval id")
	}

	ticket := getApproval(t, handler, run.ApprovalID)
	if ticket.Governance == nil || ticket.Governance.Policy == nil {
		t.Fatalf("ticket.Governance = %#v", ticket.Governance)
	}
	if ticket.Governance.Policy.PolicySource != "runtime.release_readiness_gate" {
		t.Fatalf("ticket.Governance.Policy = %#v", ticket.Governance.Policy)
	}
	resolveApproval(t, handler, run.ApprovalID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	})
	run = waitForRunStatus(t, handler, run.ID, agent.RunCompleted)
	if run.Status != agent.RunCompleted {
		t.Fatalf("run.Status = %q, want completed", run.Status)
	}
}

func TestServerRunResponsesIncludeHarnessSummary(t *testing.T) {
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
		classifier: serverStubInteractionClassifier{
			decision: rt.InteractionDecision{
				SpeechAct:   rt.SpeechActNewTask,
				TargetScope: rt.TargetScopeNewRun,
				ReplyAct:    rt.ReplyActTaskAccept,
				Confidence:  0.95,
				Reason:      "explicit_new_task",
			},
		},
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-harness",
		"content":     "search the recent invoice correspondence for ops@example.com",
	}, http.StatusAccepted)

	req := httptest.NewRequest(http.MethodGet, "/runtime/runs/"+run.ID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs/%s status = %d body=%s", run.ID, rec.Code, rec.Body.String())
	}
	var detail struct {
		ID      string                   `json:"id"`
		Harness *agent.RunHarnessSummary `json:"harness"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail run: %v", err)
	}
	if detail.Harness == nil {
		t.Fatal("expected harness summary in run detail response")
	}
	if detail.Harness.TransparentRecoveryIntent != "email" {
		t.Fatalf("detail harness transparent_recovery_intent = %q, want email", detail.Harness.TransparentRecoveryIntent)
	}

	req = httptest.NewRequest(http.MethodGet, "/runtime/runs", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs status = %d body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []*rt.RunListView `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode run list: %v", err)
	}
	if len(list.Items) == 0 || list.Items[0] == nil || list.Items[0].Harness == nil {
		t.Fatalf("expected harness summary in run list response, got %#v", list.Items)
	}
}

func TestServerRunResponsesIncludeExecutionGraphDiagnostics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	svc := rt.NewService(nil, sessions, runs, approval.NewInMemoryStore(), eventbus.NewInMemoryBus(), nil)

	session, err := sessions.GetOrCreate(ctx, "chat-exec-graph", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "chat-exec-graph",
		Content:    "prepare the brief",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = agent.RunRunning
	run.ExecutionGraph = &agent.ExecutionGraph{
		RunID:          run.ID,
		SessionID:      session.ID,
		Scope:          "single_session",
		SingleSession:  true,
		SessionLocking: true,
		MergeStrategy:  agent.MergeStrategyTaskOrder,
		Tasks: []agent.ExecutionTask{{
			ID:              "deliver-brief",
			Status:          rtplanner.TaskRunning,
			ResourceKeys:    []string{"output:brief.md"},
			SideEffectScope: agent.SideEffectScopeWorkspace,
			MergeStrategy:   agent.MergeStrategyTaskOrder,
			IdempotencyKey:  "task:brief",
		}},
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/runtime/runs/"+run.ID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs/%s status = %d body=%s", run.ID, rec.Code, rec.Body.String())
	}
	var detail struct {
		ID             string                `json:"id"`
		ExecutionGraph *agent.ExecutionGraph `json:"execution_graph"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail run: %v", err)
	}
	if detail.ExecutionGraph == nil || !detail.ExecutionGraph.SingleSession {
		t.Fatalf("detail.ExecutionGraph = %#v", detail.ExecutionGraph)
	}
	if len(detail.ExecutionGraph.Tasks) != 1 || detail.ExecutionGraph.Tasks[0].IdempotencyKey != "task:brief" {
		t.Fatalf("detail.ExecutionGraph.Tasks = %#v", detail.ExecutionGraph.Tasks)
	}

	req = httptest.NewRequest(http.MethodGet, "/runtime/runs", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs status = %d body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []*rt.RunListView `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode run list: %v", err)
	}
	if len(list.Items) == 0 || list.Items[0] == nil {
		t.Fatalf("list.Items = %#v", list.Items)
	}
	if list.Items[0].ExecutionGraph != nil {
		t.Fatalf("list execution_graph = %#v, want nil by default", list.Items[0].ExecutionGraph)
	}

	req = httptest.NewRequest(http.MethodGet, "/runtime/runs?include=execution_graph", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs?include=execution_graph status = %d body=%s", rec.Code, rec.Body.String())
	}
	list = struct {
		Items []*rt.RunListView `json:"items"`
	}{}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode run list with execution graph: %v", err)
	}
	if len(list.Items) == 0 || list.Items[0] == nil || list.Items[0].ExecutionGraph == nil {
		t.Fatalf("list.Items with execution graph = %#v", list.Items)
	}
	if got := list.Items[0].ExecutionGraph.Tasks[0].IdempotencyKey; got != "task:brief" {
		t.Fatalf("list.Items[0].ExecutionGraph.Tasks[0].IdempotencyKey = %q, want task:brief", got)
	}
}

func TestServerRunResponsesIncludeSemanticSignalDiagnostics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	svc := rt.NewService(nil, sessions, runs, approval.NewInMemoryStore(), eventbus.NewInMemoryBus(), nil)

	session, err := sessions.GetOrCreate(ctx, "chat-semantic-run", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "chat-semantic-run",
		Content:    "prepare the summary",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = agent.RunRunning
	run.SemanticSignal = &agent.SemanticSignal{
		Language: agent.LanguageProfile{
			Family:           "es",
			Script:           "Latn",
			MainSemanticPath: true,
		},
		ExecutionMode:       agent.ExecutionModeDirect,
		RequiresCurrentInfo: true,
		SuggestedDomains:    []string{"browser", "fs"},
		JobType:             "report",
		TargetSummary:       "docs/tmp/resumen.md",
		TriageReady:         true,
		TaskContractReady:   true,
		Reason:              "fresh_page_state",
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/runtime/runs/"+run.ID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs/%s status = %d body=%s", run.ID, rec.Code, rec.Body.String())
	}
	var detail struct {
		ID             string                `json:"id"`
		SemanticSignal *agent.SemanticSignal `json:"semantic_signal"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail run: %v", err)
	}
	if detail.SemanticSignal == nil || detail.SemanticSignal.Language.Family != "es" {
		t.Fatalf("detail.SemanticSignal = %#v", detail.SemanticSignal)
	}
	if !detail.SemanticSignal.RequiresCurrentInfo || !detail.SemanticSignal.TaskContractReady {
		t.Fatalf("detail.SemanticSignal readiness = %#v", detail.SemanticSignal)
	}

	req = httptest.NewRequest(http.MethodGet, "/runtime/runs", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs status = %d body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []*rt.RunListView `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode run list: %v", err)
	}
	if len(list.Items) == 0 || list.Items[0] == nil || list.Items[0].SemanticSignal == nil {
		t.Fatalf("list.Items semantic signal = %#v", list.Items)
	}
	if list.Items[0].SemanticSignal.TargetSummary != "docs/tmp/resumen.md" {
		t.Fatalf("list.Items[0].SemanticSignal.TargetSummary = %q, want docs/tmp/resumen.md", list.Items[0].SemanticSignal.TargetSummary)
	}
}

func TestServerSubmitRunAcceptsImages(t *testing.T) {
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
		"session_key": "chat-images",
		"content":     "describe this screenshot",
		"images":      []string{"data:image/png;base64,ZmFrZS1wbmc="},
	}, http.StatusAccepted)

	session, err := svc.GetSession(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) == 0 || len(session.Messages[0].ContentBlocks) != 2 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if session.Messages[0].ContentBlocks[1].Type != contextengine.ContentBlockImage {
		t.Fatalf("image block = %#v", session.Messages[0].ContentBlocks[1])
	}
}

func TestServerSubmitRunAcceptsContentBlocksWithoutText(t *testing.T) {
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
		"session_key": "chat-content-blocks-only",
		"content_blocks": []map[string]any{
			{"type": "file", "label": "brief.md", "media_ref": "upload://brief-1"},
		},
	}, http.StatusAccepted)

	session, err := svc.GetSession(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) == 0 || len(session.Messages[0].ContentBlocks) != 1 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if session.Messages[0].ContentBlocks[0].Type != contextengine.ContentBlockFile {
		t.Fatalf("file block = %#v", session.Messages[0].ContentBlocks[0])
	}
}

func TestServerInteractCreatesRunForTaskAccept(t *testing.T) {
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
		classifier: serverStubInteractionClassifier{
			decision: rt.InteractionDecision{
				SpeechAct:   rt.SpeechActNewTask,
				TargetScope: rt.TargetScopeNewRun,
				ReplyAct:    rt.ReplyActTaskAccept,
				Confidence:  0.95,
				Reason:      "explicit_new_task",
			},
		},
	})
	handler := New(svc, Config{}).Handler()

	resp := postInteract(t, handler, map[string]any{
		"session_key":   "chat-interact-task",
		"content":       "帮我写一份周报",
		"automation_id": "auto-interact",
	}, http.StatusAccepted)
	if resp.Decision.ReplyAct != rt.ReplyActTaskAccept {
		t.Fatalf("ReplyAct = %q, want %q", resp.Decision.ReplyAct, rt.ReplyActTaskAccept)
	}
	if resp.Run == nil || resp.Run.ID == "" {
		t.Fatalf("Run = %#v, want non-empty run", resp.Run)
	}
	if resp.Run.Scope.AutomationID != "auto-interact" {
		t.Fatalf("Run.Scope = %#v", resp.Run.Scope)
	}
}

func TestServerInteractTaskAcceptAcceptsImages(t *testing.T) {
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
		classifier: serverStubInteractionClassifier{
			decision: rt.InteractionDecision{
				SpeechAct:   rt.SpeechActNewTask,
				TargetScope: rt.TargetScopeNewRun,
				ReplyAct:    rt.ReplyActTaskAccept,
				Confidence:  0.95,
				Reason:      "image_prompt",
			},
		},
	})
	handler := New(svc, Config{}).Handler()

	resp := postInteract(t, handler, map[string]any{
		"session_key": "chat-interact-images",
		"content":     "what's in this image?",
		"images":      []string{"data:image/png;base64,ZmFrZS1wbmc="},
	}, http.StatusAccepted)
	if resp.Run == nil {
		t.Fatal("expected interact to create a run")
	}

	session, err := svc.GetSession(context.Background(), resp.Run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) == 0 || len(session.Messages[0].ContentBlocks) != 2 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if session.Messages[0].ContentBlocks[1].Type != contextengine.ContentBlockImage {
		t.Fatalf("image block = %#v", session.Messages[0].ContentBlocks[1])
	}
}

func TestServerInteractTaskAcceptAcceptsContentBlocksWithoutText(t *testing.T) {
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

	resp := postInteract(t, handler, map[string]any{
		"session_key": "chat-interact-content-blocks-only",
		"content_blocks": []map[string]any{
			{"type": "file", "label": "brief.md", "media_ref": "upload://brief-1"},
		},
	}, http.StatusAccepted)
	if resp.Run == nil {
		t.Fatal("expected interact to create a run")
	}

	session, err := svc.GetSession(context.Background(), resp.Run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) == 0 || len(session.Messages[0].ContentBlocks) != 1 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if session.Messages[0].ContentBlocks[0].Type != contextengine.ContentBlockFile {
		t.Fatalf("file block = %#v", session.Messages[0].ContentBlocks[0])
	}
}

func TestServerInteractStructuredRetryReusesOriginalInput(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{
				{Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "done"}},
				{Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "done again"}},
			},
		},
	})
	handler := New(svc, Config{}).Handler()

	original, err := svc.Submit(context.Background(), rt.SubmitRequest{
		SessionKey: "chat-interact-structured-retry",
		Content:    "review these assets",
		ContentBlocks: []contextengine.ContentBlock{
			{Type: contextengine.ContentBlockText, Text: "review these assets"},
			{Type: contextengine.ContentBlockFile, Label: "brief.txt", Path: "/tmp/brief.txt"},
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	waitForRunStatus(t, handler, original.ID, agent.RunCompleted)

	resp := postInteract(t, handler, map[string]any{
		"session_key": "chat-interact-structured-retry",
		"content":     "",
		"structured_command": map[string]any{
			"kind":   "retry",
			"run_id": original.ID,
		},
	}, http.StatusAccepted)
	if resp.Decision.ReplyAct != rt.ReplyActTaskAccept {
		t.Fatalf("ReplyAct = %q, want %q", resp.Decision.ReplyAct, rt.ReplyActTaskAccept)
	}
	if resp.Run == nil || resp.Run.ParentRunID != original.ID {
		t.Fatalf("Run = %#v, want retry run parented to %q", resp.Run, original.ID)
	}
	if resp.SubmitRequest == nil {
		t.Fatal("expected submit request")
	}
	if resp.SubmitRequest.Content != "review these assets" {
		t.Fatalf("SubmitRequest.Content = %q, want %q", resp.SubmitRequest.Content, "review these assets")
	}
	if len(resp.SubmitRequest.ContentBlocks) != 2 || resp.SubmitRequest.ContentBlocks[1].Type != contextengine.ContentBlockFile {
		t.Fatalf("SubmitRequest.ContentBlocks = %#v", resp.SubmitRequest.ContentBlocks)
	}
	if got := resp.SubmitRequest.Metadata["retry_run_id"]; got != original.ID {
		t.Fatalf("retry_run_id = %#v, want %q", got, original.ID)
	}
}

func TestServerInteractReturnsMessageForIdleChatReply(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "You are welcome.",
				},
			}},
		},
		classifier: serverStubInteractionClassifier{
			decision: rt.InteractionDecision{
				SpeechAct:   rt.SpeechActCasualChat,
				TargetScope: rt.TargetScopeNone,
				ReplyAct:    rt.ReplyActChatReply,
				Reason:      "chit_chat",
				Confidence:  0.96,
			},
		},
	})
	handler := New(svc, Config{}).Handler()

	resp := postInteract(t, handler, map[string]any{
		"session_key": "chat-interact-chat",
		"content":     "thanks",
	}, http.StatusAccepted)
	if resp.Decision.ReplyAct != rt.ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q", resp.Decision.ReplyAct, rt.ReplyActChatReply)
	}
	if resp.Run != nil {
		t.Fatalf("Run = %#v, want nil", resp.Run)
	}
	if strings.TrimSpace(resp.Message) == "" {
		t.Fatal("expected non-empty interaction message")
	}
}

func TestServerGetRunResult(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "report finished",
				},
			}},
		},
	})
	svc.WithEffectiveConfigSnapshot(&controlsnapshot.EffectiveConfigSnapshot{
		ID: "ecs-result-1",
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key":   "chat-result",
		"content":       "generate report",
		"automation_id": "auto-result",
	}, http.StatusAccepted)
	waitForRunStatus(t, handler, run.ID, agent.RunCompleted)

	result := getRunResult(t, handler, run.ID)
	if result.RunID != run.ID {
		t.Fatalf("RunID = %q, want %q", result.RunID, run.ID)
	}
	if result.Outcome != rt.RunOutcomeCompleted {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, rt.RunOutcomeCompleted)
	}
	if result.Output != "report finished" {
		t.Fatalf("Output = %q, want %q", result.Output, "report finished")
	}
	if result.Summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if result.Bundle == nil {
		t.Fatal("expected canonical bundle in API response")
	}
	if result.Bundle.FinalText != "report finished" {
		t.Fatalf("Bundle.FinalText = %q, want %q", result.Bundle.FinalText, "report finished")
	}
	if result.Governance == nil || result.Governance.EffectiveConfigSnapshotID != "ecs-result-1" {
		t.Fatalf("Governance = %#v", result.Governance)
	}
	if result.Governance.Scope.AutomationID != "auto-result" {
		t.Fatalf("Governance.Scope = %#v", result.Governance.Scope)
	}
}

func TestServerGetRunResultIncludesEventLedgerAndDelivery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	svc := rt.NewService(nil, sessions, runs, approval.NewInMemoryStore(), bus, nil)

	session, err := sessions.GetOrCreate(ctx, "chat-server-result-ledger", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey:      "chat-server-result-ledger",
		ExternalEventID: "evt-server-result-ledger",
		Content:         "prepare the report",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-2 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "prepare the report",
			CreatedAt: now.Add(-time.Second),
			Metadata: map[string]any{
				"channel":    "slack",
				"message_id": "msg-ledger-1",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "Report finished.",
			CreatedAt: now,
			Metadata:  map[string]any{"run_id": run.ID},
		},
	)
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	for _, event := range []eventbus.Event{
		{
			ID:        "evt-ledger-1",
			Type:      eventbus.EventToolExecuted,
			RunID:     run.ID,
			SessionID: session.ID,
			Attrs: map[string]any{
				"tool_name":     "report.generate",
				"artifact_uris": []string{"artifact://report-1"},
			},
		},
		{
			ID:        "evt-ledger-2",
			Type:      eventbus.EventGovernanceDeliveryDelivered,
			RunID:     run.ID,
			SessionID: session.ID,
			Attrs: map[string]any{
				"adapter_name":      "audit-hub",
				"delivery_status":   "delivered",
				"source_event_id":   "evt-ledger-1",
				"source_event_type": string(eventbus.EventToolExecuted),
			},
		},
	} {
		if err := bus.Publish(ctx, event); err != nil {
			t.Fatalf("Publish(%s) error = %v", event.ID, err)
		}
	}

	handler := New(svc, Config{}).Handler()
	result := getRunResult(t, handler, run.ID)
	if result.RunID != run.ID {
		t.Fatalf("RunID = %q, want %q", result.RunID, run.ID)
	}
	if result.EventLedger == nil || len(result.EventLedger.Events) != 2 {
		t.Fatalf("EventLedger = %#v, want 2 events", result.EventLedger)
	}
	if result.EventLedger.Events[0].EventClass != rt.EventClassEvidence || result.EventLedger.Events[1].EventClass != rt.EventClassDelivery {
		t.Fatalf("EventLedger classes = %#v", result.EventLedger.Events)
	}
	if result.Delivery == nil {
		t.Fatal("expected delivery envelope")
	}
	if len(result.Delivery.Attachments) != 1 || result.Delivery.Attachments[0].URI != "artifact://report-1" {
		t.Fatalf("Delivery.Attachments = %#v", result.Delivery.Attachments)
	}
	if result.Delivery.Conversation == nil || result.Delivery.Conversation.Channel != "slack" {
		t.Fatalf("Delivery.Conversation = %#v", result.Delivery.Conversation)
	}
}

func TestServerGetRunCompletion(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "report finished",
				},
			}},
		},
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-completion",
		"content":     "generate report",
	}, http.StatusAccepted)
	waitForRunStatus(t, handler, run.ID, agent.RunCompleted)

	completion := getRunCompletion(t, handler, run.ID)
	if completion.RunID != run.ID {
		t.Fatalf("RunID = %q, want %q", completion.RunID, run.ID)
	}
	if completion.Result == nil || completion.Verification == nil {
		t.Fatalf("completion = %#v", completion)
	}
	if completion.Result.Bundle == nil || completion.Bundle == nil {
		t.Fatalf("completion bundle = %#v / %#v", completion.Result, completion.Bundle)
	}
	if completion.Verification.Status != "passed" {
		t.Fatalf("Verification.Status = %q, want passed", completion.Verification.Status)
	}
	if completion.Outcome != rt.RunOutcomeCompleted || completion.Result.Outcome != rt.RunOutcomeCompleted {
		t.Fatalf("Outcome = %q / %q, want %q", completion.Outcome, completion.Result.Outcome, rt.RunOutcomeCompleted)
	}
}

func TestServerGetRunVerification(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "report finished",
				},
			}},
		},
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-verify",
		"content":     "generate report",
	}, http.StatusAccepted)
	waitForRunStatus(t, handler, run.ID, agent.RunCompleted)

	verification := getRunVerification(t, handler, run.ID)
	if verification.RunID != run.ID {
		t.Fatalf("RunID = %q, want %q", verification.RunID, run.ID)
	}
	if verification.Status != "passed" {
		t.Fatalf("Status = %q, want passed", verification.Status)
	}
	if verification.Summary == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestServerGetEffectiveConfig(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{})
	svc.WithEffectiveConfigSnapshot(&controlsnapshot.EffectiveConfigSnapshot{
		ID:           "ecs-test",
		DefaultModel: "test-model",
		Approval: controlsnapshot.ApprovalPolicy{
			RequireApprovalForWrite: true,
			DefaultGrantScope:       "once",
			MaxGrantScope:           "session",
			HasPolicyOverlay:        true,
		},
	})
	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/runtime/config/effective", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/config/effective status = %d body=%s", rec.Code, rec.Body.String())
	}
	var snapshot controlsnapshot.EffectiveConfigSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if snapshot.ID != "ecs-test" {
		t.Fatalf("snapshot.ID = %q", snapshot.ID)
	}
	if !snapshot.Approval.RequireApprovalForWrite || snapshot.Approval.DefaultGrantScope != "once" || snapshot.Approval.MaxGrantScope != "session" {
		t.Fatalf("snapshot.Approval = %#v", snapshot.Approval)
	}
}

func TestServerGetRunGovernance(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "report finished",
				},
			}},
		},
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-governance",
		"content":     "generate report",
	}, http.StatusAccepted)
	waitForRunStatus(t, handler, run.ID, agent.RunCompleted)

	req := httptest.NewRequest(http.MethodGet, "/runtime/runs/"+run.ID+"/governance", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs/{id}/governance status = %d body=%s", rec.Code, rec.Body.String())
	}
	var snapshot rt.GovernanceSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if snapshot.RunID != run.ID {
		t.Fatalf("snapshot.RunID = %q, want %q", snapshot.RunID, run.ID)
	}
}

func TestServerGetRunGovernanceIncludesPolicyExplainability(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-1",
					Name: "fs.write",
				}},
			}},
		},
		tools:     serverToolExecutor{},
		policy:    serverRequireApprovalPolicyEngine{},
		approvals: approval.NewInMemoryStore(),
	})
	svc.WithEffectiveConfigSnapshot(&controlsnapshot.EffectiveConfigSnapshot{
		ID: "ecs-server-1",
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key":   "chat-governance-http",
		"content":       "write file",
		"automation_id": "auto-http",
	}, http.StatusAccepted)
	run = waitForRunStatus(t, handler, run.ID, agent.RunWaitingApproval)

	req := httptest.NewRequest(http.MethodGet, "/runtime/runs/"+run.ID+"/governance", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs/{id}/governance status = %d body=%s", rec.Code, rec.Body.String())
	}
	var snapshot rt.GovernanceSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if snapshot.EffectiveConfigSnapshotID != "ecs-server-1" {
		t.Fatalf("EffectiveConfigSnapshotID = %q", snapshot.EffectiveConfigSnapshotID)
	}
	if snapshot.Scope.AutomationID != "auto-http" {
		t.Fatalf("Scope = %#v", snapshot.Scope)
	}
	if snapshot.Policy == nil || snapshot.Policy.PolicySource == "" || snapshot.Policy.Summary == "" {
		t.Fatalf("Policy = %#v", snapshot.Policy)
	}
	if snapshot.Approval == nil || snapshot.Approval.Status != approval.StatusPending {
		t.Fatalf("Approval = %#v", snapshot.Approval)
	}
	if _, err := svc.UpsertApprovalExternalRef(context.Background(), run.ApprovalID, approval.ExternalReference{
		Provider:   "jira",
		ExternalID: "JIRA-HTTP-1",
		Status:     "pending_remote",
		SyncedAt:   time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("UpsertApprovalExternalRef() error = %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/runtime/runs/"+run.ID+"/governance", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs/{id}/governance status(after external)= %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode response after external ref: %v", err)
	}
	if snapshot.Approval == nil || len(snapshot.Approval.External) != 1 {
		t.Fatalf("snapshot.Approval.External = %#v", snapshot.Approval)
	}
	if snapshot.Approval.External[0].Provider != "jira" || snapshot.Approval.External[0].ExternalID != "JIRA-HTTP-1" {
		t.Fatalf("snapshot.Approval.External[0] = %#v", snapshot.Approval.External[0])
	}
	if len(snapshot.PolicyToolNames) != 1 || snapshot.PolicyToolNames[0] != "fs.write" {
		t.Fatalf("PolicyToolNames = %#v", snapshot.PolicyToolNames)
	}
}

func TestServerApprovalEndpointsIncludeGovernanceView(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-1",
					Name: "fs.write",
				}},
			}},
		},
		tools:     serverToolExecutor{},
		policy:    serverRequireApprovalPolicyEngine{},
		approvals: approval.NewInMemoryStore(),
	})
	svc.WithEffectiveConfigSnapshot(&controlsnapshot.EffectiveConfigSnapshot{
		ID: "ecs-approval-view-1",
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key":   "chat-approval-view",
		"content":       "write file",
		"automation_id": "auto-approval",
	}, http.StatusAccepted)
	run = waitForRunStatus(t, handler, run.ID, agent.RunWaitingApproval)

	items := listApprovals(t, handler, "pending")
	if len(items) != 1 {
		t.Fatalf("len(items) = %d", len(items))
	}
	item := items[0]
	if item == nil || item.Governance == nil {
		t.Fatalf("item = %#v", item)
	}
	if item.Governance.EffectiveConfigSnapshotID != "ecs-approval-view-1" {
		t.Fatalf("EffectiveConfigSnapshotID = %q", item.Governance.EffectiveConfigSnapshotID)
	}
	if item.Governance.Scope.AutomationID != "auto-approval" {
		t.Fatalf("Scope = %#v", item.Governance.Scope)
	}
	if item.Governance.Policy == nil || item.Governance.Policy.PolicySource == "" || item.Governance.Policy.Summary == "" {
		t.Fatalf("Policy = %#v", item.Governance.Policy)
	}
	if item.Governance.Approval == nil || item.Governance.Approval.Status != approval.StatusPending {
		t.Fatalf("Approval = %#v", item.Governance.Approval)
	}
	if len(item.Governance.ToolNames) != 1 || item.Governance.ToolNames[0] != "fs.write" {
		t.Fatalf("ToolNames = %#v", item.Governance.ToolNames)
	}

	got := getApproval(t, handler, run.ApprovalID)
	if got.Governance == nil || got.Governance.Policy == nil {
		t.Fatalf("getApproval governance = %#v", got.Governance)
	}
	if got.Governance.Policy.PolicySource != "test.policy/server" {
		t.Fatalf("PolicySource = %q", got.Governance.Policy.PolicySource)
	}
	if _, err := svc.UpsertApprovalExternalRef(context.Background(), run.ApprovalID, approval.ExternalReference{
		Provider:   "jira",
		ExternalID: "JIRA-VIEW-1",
		Status:     "pending_remote",
		SyncedAt:   time.Date(2026, 3, 19, 10, 2, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("UpsertApprovalExternalRef() error = %v", err)
	}
	got = getApproval(t, handler, run.ApprovalID)
	if got.Governance == nil || got.Governance.Approval == nil || len(got.Governance.Approval.External) != 1 {
		t.Fatalf("getApproval governance external = %#v", got.Governance)
	}
	if got.Governance.Approval.External[0].Provider != "jira" || got.Governance.Approval.External[0].ExternalID != "JIRA-VIEW-1" {
		t.Fatalf("getApproval governance external[0] = %#v", got.Governance.Approval.External[0])
	}
}

func TestServerListRunsCanIncludeVerification(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "report finished",
				},
			}},
		},
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-list-verify",
		"content":     "generate report",
	}, http.StatusAccepted)
	waitForRunStatus(t, handler, run.ID, agent.RunCompleted)

	req := httptest.NewRequest(http.MethodGet, "/runtime/runs?include=verification", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs?include=verification status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []struct {
			Outcome             string `json:"outcome"`
			ID                  string `json:"id"`
			VerificationStatus  string `json:"verification_status"`
			VerificationSummary string `json:"verification_summary"`
		} `json:"items"`
		Count int `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(list runs with verification) error = %v", err)
	}
	if payload.Count == 0 || len(payload.Items) == 0 {
		t.Fatalf("expected non-empty items, got %+v", payload)
	}
	found := false
	for _, item := range payload.Items {
		if item.ID != run.ID {
			continue
		}
		found = true
		if item.Outcome != string(rt.RunOutcomeCompleted) {
			t.Fatalf("Outcome = %q, want %q", item.Outcome, rt.RunOutcomeCompleted)
		}
		if item.VerificationStatus != "passed" {
			t.Fatalf("VerificationStatus = %q, want passed", item.VerificationStatus)
		}
		if item.VerificationSummary == "" {
			t.Fatal("expected non-empty verification summary")
		}
	}
	if !found {
		t.Fatalf("run %s not found in payload %+v", run.ID, payload.Items)
	}
}

func TestServerListRunsIncludesGovernanceProjection(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-1",
					Name: "fs.write",
				}},
			}},
		},
		tools:     serverToolExecutor{},
		policy:    serverRequireApprovalPolicyEngine{},
		approvals: approval.NewInMemoryStore(),
	})
	svc.WithEffectiveConfigSnapshot(&controlsnapshot.EffectiveConfigSnapshot{
		ID: "ecs-run-list-1",
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key":   "chat-run-list-governance",
		"content":       "write file",
		"automation_id": "auto-list",
	}, http.StatusAccepted)
	run = waitForRunStatus(t, handler, run.ID, agent.RunWaitingApproval)

	req := httptest.NewRequest(http.MethodGet, "/runtime/runs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []*rt.RunListView `json:"items"`
		Count int               `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(run list) error = %v", err)
	}
	if payload.Count == 0 || len(payload.Items) == 0 {
		t.Fatalf("expected run list items, got %+v", payload)
	}

	found := false
	for _, item := range payload.Items {
		if item == nil || item.ID != run.ID {
			continue
		}
		found = true
		if item.CreatedAt.IsZero() {
			t.Fatal("expected created_at in run list view")
		}
		if item.Outcome != string(rt.RunOutcomeNeedsConfirmation) {
			t.Fatalf("Outcome = %q, want %q", item.Outcome, rt.RunOutcomeNeedsConfirmation)
		}
		if item.Governance == nil {
			t.Fatalf("Governance = %#v", item.Governance)
		}
		if item.Governance.EffectiveConfigSnapshotID != "ecs-run-list-1" {
			t.Fatalf("EffectiveConfigSnapshotID = %q", item.Governance.EffectiveConfigSnapshotID)
		}
		if item.Governance.Scope.AutomationID != "auto-list" {
			t.Fatalf("Scope = %#v", item.Governance.Scope)
		}
		if item.Governance.Policy == nil || item.Governance.Policy.PolicySource != "test.policy/server" {
			t.Fatalf("Policy = %#v", item.Governance.Policy)
		}
		if item.Governance.Approval == nil || item.Governance.Approval.Status != approval.StatusPending {
			t.Fatalf("Approval = %#v", item.Governance.Approval)
		}
	}
	if !found {
		t.Fatalf("run %s not found in payload %+v", run.ID, payload.Items)
	}
}

func TestServerListSessionsIncludesScope(t *testing.T) {
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

	postRun(t, handler, map[string]any{
		"session_key":   "chat-session-scope",
		"content":       "hello",
		"automation_id": "auto-session",
	}, http.StatusAccepted)

	req := httptest.NewRequest(http.MethodGet, "/runtime/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/sessions status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []agent.SessionSummary `json:"items"`
		Count int                    `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(session list) error = %v", err)
	}
	if payload.Count == 0 || len(payload.Items) == 0 {
		t.Fatalf("expected non-empty sessions payload, got %+v", payload)
	}
	found := false
	for _, item := range payload.Items {
		if item.Key != "chat-session-scope" {
			continue
		}
		found = true
		if item.Scope.AutomationID != "auto-session" {
			t.Fatalf("Scope = %#v", item.Scope)
		}
	}
	if !found {
		t.Fatalf("session chat-session-scope not found in payload %+v", payload.Items)
	}
}

func TestServerDeleteSessionRemovesRuntimeSession(t *testing.T) {
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
		"session_key": "chat-session-delete",
		"content":     "hello",
	}, http.StatusAccepted)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/runtime/sessions/"+run.SessionID, nil)
	deleteRec := httptest.NewRecorder()
	handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("DELETE /runtime/sessions/%s status = %d body=%s", run.SessionID, deleteRec.Code, deleteRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/runtime/sessions/"+run.SessionID, nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("GET /runtime/sessions/%s after delete status = %d body=%s", run.SessionID, getRec.Code, getRec.Body.String())
	}
}

func TestServerStartSessionEpisode(t *testing.T) {
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
		"session_key": "chat-session-episode",
		"content":     "hello",
	}, http.StatusAccepted)

	req := httptest.NewRequest(http.MethodPost, "/runtime/sessions/"+run.SessionID+"/episode", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /runtime/sessions/%s/episode status = %d body=%s", run.SessionID, rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(episode response) error = %v", err)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("payload = %#v, want ok=true", payload)
	}
	if episodeID, _ := payload["episode_id"].(string); strings.TrimSpace(episodeID) == "" {
		t.Fatalf("payload = %#v, want episode_id", payload)
	}
}

func TestServerListEventsIncludesGovernanceProjection(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-1",
					Name: "fs.write",
				}},
			}},
		},
		tools:     serverToolExecutor{},
		policy:    serverRequireApprovalPolicyEngine{},
		approvals: approval.NewInMemoryStore(),
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key":   "chat-event-governance",
		"content":       "write file",
		"automation_id": "auto-events",
	}, http.StatusAccepted)
	waitForRunStatus(t, handler, run.ID, agent.RunWaitingApproval)

	req := httptest.NewRequest(http.MethodGet, "/runtime/events", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/events status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []rt.EventView `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(event list) error = %v", err)
	}
	if len(payload.Items) == 0 {
		t.Fatal("expected non-empty event list")
	}

	found := false
	for _, item := range payload.Items {
		if item.Type != eventbus.EventApprovalRequested {
			continue
		}
		found = true
		if item.Governance == nil {
			t.Fatalf("Governance = %#v", item.Governance)
		}
		if item.Governance.Scope.AutomationID != "auto-events" {
			t.Fatalf("Scope = %#v", item.Governance.Scope)
		}
		if item.Governance.Policy == nil || item.Governance.Policy.PolicySource != "test.policy/server" {
			t.Fatalf("Policy = %#v", item.Governance.Policy)
		}
		if item.Governance.Approval == nil || item.Governance.Approval.Status != approval.StatusPending {
			t.Fatalf("Approval = %#v", item.Governance.Approval)
		}
		if item.Summary == "" {
			t.Fatal("expected non-empty event summary")
		}
	}
	if !found {
		t.Fatalf("approval.requested event not found in payload %+v", payload.Items)
	}
}

func TestServerListEventsSupportsTypeRunAndSessionFilters(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	for _, event := range []eventbus.Event{
		{Type: eventbus.EventRunStarted, RunID: "run-filter-1", SessionID: "session-filter-1"},
		{Type: eventbus.EventRunCompleted, RunID: "run-filter-1", SessionID: "session-filter-1"},
		{Type: eventbus.EventRunCompleted, RunID: "run-filter-2", SessionID: "session-filter-2"},
	} {
		if err := bus.Publish(context.Background(), event); err != nil {
			t.Fatalf("publish event: %v", err)
		}
	}

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
		bus:   bus,
	})
	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/runtime/events?type=run.completed&run_id=run-filter-1&session_id=session-filter-1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/events status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []rt.EventView `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(event list) error = %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(payload.Items))
	}
	if payload.Items[0].Type != eventbus.EventRunCompleted || payload.Items[0].RunID != "run-filter-1" || payload.Items[0].SessionID != "session-filter-1" {
		t.Fatalf("item = %#v", payload.Items[0])
	}
}

func TestServerMessageReceivedHookFires(t *testing.T) {
	t.Parallel()

	store := hooks.NewInMemoryStore()
	if _, err := store.Add(context.Background(), hooks.Hook{
		Name:    "message-received",
		Enabled: true,
		Trigger: hooks.TriggerMessageReceived,
		Kind:    hooks.KindCommand,
		Command: "cat >/dev/null",
		Phase:   hooks.HookPhasePost,
	}); err != nil {
		t.Fatalf("hook add: %v", err)
	}
	executor := hooks.NewExecutor(store)

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done",
				},
			}},
		},
		hooks: executor,
	})
	handler := New(svc, Config{}).Handler()

	postRun(t, handler, map[string]any{
		"session_key":   "chat-hook-message",
		"content":       "hello",
		"automation_id": "auto-hook-message",
	}, http.StatusAccepted)

	results := executor.RecentResultsByHook("hook-000001", 1)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Trigger != hooks.TriggerMessageReceived {
		t.Fatalf("Trigger = %q, want %q", results[0].Trigger, hooks.TriggerMessageReceived)
	}
	if results[0].SessionID == "" {
		t.Fatal("expected session id in message hook result")
	}
	if results[0].PayloadPreview["scope"] == nil {
		t.Fatalf("PayloadPreview = %#v, expected scope", results[0].PayloadPreview)
	}
}

func TestServerBeforeAgentStartHookBlocksRun(t *testing.T) {
	t.Parallel()

	store := hooks.NewInMemoryStore()
	if _, err := store.Add(context.Background(), hooks.Hook{
		Name:    "block-agent-start",
		Enabled: true,
		Trigger: hooks.TriggerBeforeAgentStart,
		Kind:    hooks.KindCommand,
		Command: "exit 9",
		Phase:   hooks.HookPhasePre,
	}); err != nil {
		t.Fatalf("hook add: %v", err)
	}
	executor := hooks.NewExecutor(store)

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "should not run",
				},
			}},
		},
		hooks: executor,
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-hook-agent-start",
		"content":     "hello",
	}, http.StatusAccepted)
	run = waitForRunStatus(t, handler, run.ID, agent.RunFailed)

	if !strings.Contains(run.Error, "action rejected by hook") {
		t.Fatalf("run.Error = %q", run.Error)
	}
	results := executor.RecentResultsByHook("hook-000001", 1)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Trigger != hooks.TriggerBeforeAgentStart || results[0].Status != "error" {
		t.Fatalf("result = %#v", results[0])
	}
}

func TestServerBeforeToolCallHookBlocksExecution(t *testing.T) {
	t.Parallel()

	store := hooks.NewInMemoryStore()
	if _, err := store.Add(context.Background(), hooks.Hook{
		Name:    "block-tool-call",
		Enabled: true,
		Trigger: hooks.TriggerBeforeToolCall,
		Kind:    hooks.KindCommand,
		Command: "exit 7",
		Phase:   hooks.HookPhasePre,
	}); err != nil {
		t.Fatalf("hook add: %v", err)
	}
	executor := hooks.NewExecutor(store)
	toolExec := &serverCountingToolExecutor{}

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-hook-1",
					Name: "fs.write",
				}},
			}},
		},
		tools: toolExec,
		hooks: executor,
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-hook-tool-block",
		"content":     "write file",
	}, http.StatusAccepted)
	run = waitForRunStatus(t, handler, run.ID, agent.RunFailed)

	if got := toolExec.calls.Load(); got != 0 {
		t.Fatalf("tool executor calls = %d, want 0", got)
	}
	if !strings.Contains(run.Error, "action rejected by hook") {
		t.Fatalf("run.Error = %q", run.Error)
	}
	results := executor.RecentResultsByHook("hook-000001", 1)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Trigger != hooks.TriggerBeforeToolCall || results[0].ToolName != "fs.write" {
		t.Fatalf("result = %#v", results[0])
	}
}

func TestServerAfterToolCallHookRecordsResult(t *testing.T) {
	t.Parallel()

	store := hooks.NewInMemoryStore()
	if _, err := store.Add(context.Background(), hooks.Hook{
		Name:    "after-tool-call",
		Enabled: true,
		Trigger: hooks.TriggerAfterToolCall,
		Kind:    hooks.KindCommand,
		Command: "cat >/dev/null",
		Phase:   hooks.HookPhasePost,
	}); err != nil {
		t.Fatalf("hook add: %v", err)
	}
	executor := hooks.NewExecutor(store)

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-hook-post-1",
					Name: "fs.write",
				}},
			}, {
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done",
				},
			}},
		},
		tools: serverToolExecutor{
			results: []contextengine.ToolResult{{
				ToolName:   "fs.write",
				ToolCallID: "call-hook-post-1",
				Summary:    "write finished",
			}},
		},
		hooks: executor,
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-hook-tool-post",
		"content":     "write file",
	}, http.StatusAccepted)
	waitForRunStatus(t, handler, run.ID, agent.RunCompleted)

	results := executor.RecentResultsByHook("hook-000001", 1)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Trigger != hooks.TriggerAfterToolCall || results[0].ToolName != "fs.write" {
		t.Fatalf("result = %#v", results[0])
	}
	if results[0].PayloadPreview["result_status"] != "ok" {
		t.Fatalf("PayloadPreview = %#v", results[0].PayloadPreview)
	}
}

func TestServerSubmitRunWithoutExecuteLeavesQueued(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-queued",
		"content":     "hello",
		"execute":     false,
	}, http.StatusCreated)
	if run.Status != agent.RunQueued {
		t.Fatalf("run.Status = %q", run.Status)
	}
}

func TestServerSubmitRunBlockingPreflightWaitsForInput(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model:     &serverModelClient{},
		preflight: runtimeBlockingPreflightAnalyzer{},
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-server-waiting-input",
		"content":     "把这个文件改一下",
	}, http.StatusAccepted)
	if run.Status != agent.RunWaitingInput {
		t.Fatalf("run.Status = %q", run.Status)
	}
	if run.Preflight == nil || run.Preflight.Prompt == "" {
		t.Fatalf("run.Preflight = %#v", run.Preflight)
	}
	result := getRunResult(t, handler, run.ID)
	if result.Outcome != rt.RunOutcomeNeedsConfirmation {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, rt.RunOutcomeNeedsConfirmation)
	}
}

func TestServerApprovalEndpoints(t *testing.T) {
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
						Content: "after approval",
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
		bus:       eventbus.NewInMemoryBus(),
		skills:    skillSvc,
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-approval",
		"content":     "write file",
	}, http.StatusAccepted)
	if run.Status != agent.RunQueued {
		t.Fatalf("run.Status = %q", run.Status)
	}
	run = waitForRunStatus(t, handler, run.ID, agent.RunWaitingApproval)

	tickets := listApprovals(t, handler, "pending")
	if len(tickets) != 1 {
		t.Fatalf("len(tickets) = %d", len(tickets))
	}
	ticket := getApproval(t, handler, tickets[0].ID)
	if ticket.Status != approval.StatusPending {
		t.Fatalf("ticket.Status = %q", ticket.Status)
	}

	resolveApproval(t, handler, ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	})

	run = waitForRunStatus(t, handler, run.ID, agent.RunCompleted)
	if run.Status != agent.RunCompleted {
		t.Fatalf("run.Status = %q", run.Status)
	}

	events := getEvents(t, handler, "/runtime/audit/events")
	if len(events) == 0 || events[len(events)-1].Type != eventbus.EventRunCompleted {
		t.Fatalf("events = %#v", events)
	}
}

func TestServerApprovalListSupportsPagination(t *testing.T) {
	t.Parallel()

	approvalStore := approval.NewInMemoryStore()
	for _, runID := range []string{"run-1", "run-2", "run-3"} {
		if _, err := approvalStore.Create(context.Background(), approval.Ticket{
			RunID:     runID,
			SessionID: "sess-" + runID,
		}); err != nil {
			t.Fatalf("Create(%s) error = %v", runID, err)
		}
	}

	svc := newRuntimeService(t, runtimeFixture{
		model:     &serverModelClient{},
		approvals: approvalStore,
	})
	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/runtime/approvals?status=pending&limit=1&offset=1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/approvals status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []*rt.ApprovalView `json:"items"`
		Count int                `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(approvals) error = %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Items[0].RunID != "run-2" {
		t.Fatalf("payload.Items[0].RunID = %q, want run-2", payload.Items[0].RunID)
	}
}

func TestServerApprovalCallbackEndpoint(t *testing.T) {
	t.Parallel()

	skillSvc := newSkillService(t, "fs.write", "local_write")
	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-callback-1",
					Name: "fs.write",
				}},
			}},
		},
		tools: serverToolExecutor{
			results: []contextengine.ToolResult{{
				ToolName:   "fs.write",
				ToolCallID: "call-callback-1",
				Content:    "written",
			}},
		},
		policy: policy.NewDefaultEngine(policy.Config{
			RequireApprovalForWrite: true,
		}),
		approvals: approval.NewInMemoryStore(),
		bus:       eventbus.NewInMemoryBus(),
		skills:    skillSvc,
	})
	handler := New(svc, Config{
		ApprovalCallbacks: map[string]controlplane.ApprovalCallbackAuthPolicy{
			"jira": {HeaderName: "X-HopClaw-Approval-Token", Token: "jira-secret"},
		},
	}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-approval-callback",
		"content":     "write file",
	}, http.StatusAccepted)
	run = waitForRunStatus(t, handler, run.ID, agent.RunWaitingApproval)

	tickets := listApprovals(t, handler, "pending")
	if len(tickets) != 1 {
		t.Fatalf("len(tickets) = %d", len(tickets))
	}
	body, err := json.Marshal(controlplane.ApprovalResolveCallbackRequest{
		Provider: "jira",
		TicketID: tickets[0].ID,
		Status:   "approved",
		Scope:    "session",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/callbacks/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HopClaw-Approval-Token", "jira-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST callback status = %d body=%s", rec.Code, rec.Body.String())
	}

	var view rt.ApprovalView
	if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if view.Status != approval.StatusApproved {
		t.Fatalf("view.Status = %q", view.Status)
	}
	if view.ResolvedBy != "provider:jira" {
		t.Fatalf("view.ResolvedBy = %q", view.ResolvedBy)
	}
	if len(view.External) != 1 || view.External[0].Provider != "jira" || view.External[0].Status != "approved" {
		t.Fatalf("view.External = %#v", view.External)
	}
}

func TestServerApprovalCallbackEndpointRejectsMissingProviderToken(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{})
	handler := New(svc, Config{
		ApprovalCallbacks: map[string]controlplane.ApprovalCallbackAuthPolicy{
			"jira": {HeaderName: "X-HopClaw-Approval-Token", Token: "jira-secret"},
		},
	}).Handler()

	body, err := json.Marshal(controlplane.ApprovalResolveCallbackRequest{
		Provider: "jira",
		TicketID: "appr-1",
		Status:   "approved",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/callbacks/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST callback status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServerApprovalCallbackEndpointResolvesByExternalID(t *testing.T) {
	t.Parallel()

	approvalStore := approval.NewInMemoryStore()
	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-callback-external-1",
					Name: "fs.write",
				}},
			}},
		},
		tools: serverToolExecutor{
			results: []contextengine.ToolResult{{
				ToolName:   "fs.write",
				ToolCallID: "call-callback-external-1",
				Content:    "written",
			}},
		},
		policy: policy.NewDefaultEngine(policy.Config{
			RequireApprovalForWrite: true,
		}),
		approvals: approvalStore,
		bus:       eventbus.NewInMemoryBus(),
		skills:    newSkillService(t, "fs.write", "local_write"),
	})
	handler := New(svc, Config{
		ApprovalCallbacks: map[string]controlplane.ApprovalCallbackAuthPolicy{
			"jira": {HeaderName: "X-HopClaw-Approval-Token", Token: "jira-secret"},
		},
	}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-approval-callback-external",
		"content":     "write file",
	}, http.StatusAccepted)
	run = waitForRunStatus(t, handler, run.ID, agent.RunWaitingApproval)

	tickets := listApprovals(t, handler, "pending")
	if len(tickets) != 1 {
		t.Fatalf("len(tickets) = %d", len(tickets))
	}
	if _, err := approvalStore.UpsertExternalRef(context.Background(), tickets[0].ID, approval.ExternalReference{
		Provider:   "jira",
		ExternalID: "jira-123",
		Status:     "pending_remote",
		SyncedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertExternalRef() error = %v", err)
	}

	body, err := json.Marshal(controlplane.ApprovalResolveCallbackRequest{
		Provider:   "jira",
		ExternalID: "jira-123",
		Status:     "approved",
		Scope:      "session",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/callbacks/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HopClaw-Approval-Token", "jira-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST callback status = %d body=%s", rec.Code, rec.Body.String())
	}

	var view rt.ApprovalView
	if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if view.ID != tickets[0].ID {
		t.Fatalf("view.ID = %q, want %q", view.ID, tickets[0].ID)
	}
	if view.Status != approval.StatusApproved {
		t.Fatalf("view.Status = %q", view.Status)
	}
}

func TestServerApprovalCallbackEndpointSupportsHMAC(t *testing.T) {
	t.Parallel()

	skillSvc := newSkillService(t, "fs.write", "local_write")
	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-callback-hmac-1",
					Name: "fs.write",
				}},
			}},
		},
		tools: serverToolExecutor{
			results: []contextengine.ToolResult{{
				ToolName:   "fs.write",
				ToolCallID: "call-callback-hmac-1",
				Content:    "written",
			}},
		},
		policy: policy.NewDefaultEngine(policy.Config{
			RequireApprovalForWrite: true,
		}),
		approvals: approval.NewInMemoryStore(),
		bus:       eventbus.NewInMemoryBus(),
		skills:    skillSvc,
	})
	handler := New(svc, Config{
		ApprovalCallbacks: map[string]controlplane.ApprovalCallbackAuthPolicy{
			"jira": {
				Mode:            "hmac",
				Secret:          "jira-hmac-secret",
				SignatureHeader: "X-HopClaw-Signature",
				TimestampHeader: "X-HopClaw-Timestamp",
				MaxAge:          5 * time.Minute,
			},
		},
	}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-approval-callback-hmac",
		"content":     "write file",
	}, http.StatusAccepted)
	run = waitForRunStatus(t, handler, run.ID, agent.RunWaitingApproval)

	tickets := listApprovals(t, handler, "pending")
	if len(tickets) != 1 {
		t.Fatalf("len(tickets) = %d", len(tickets))
	}
	body, err := json.Marshal(controlplane.ApprovalResolveCallbackRequest{
		Provider: "jira",
		TicketID: tickets[0].ID,
		Status:   "approved",
		Scope:    "session",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	signature := "sha256=" + computeApprovalCallbackHMAC("jira-hmac-secret", timestamp, body)

	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/callbacks/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HopClaw-Timestamp", timestamp)
	req.Header.Set("X-HopClaw-Signature", signature)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST callback status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServerApprovalCallbackEndpointRejectsInvalidHMAC(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{})
	handler := New(svc, Config{
		ApprovalCallbacks: map[string]controlplane.ApprovalCallbackAuthPolicy{
			"jira": {
				Mode:            "hmac",
				Secret:          "jira-hmac-secret",
				SignatureHeader: "X-HopClaw-Signature",
				TimestampHeader: "X-HopClaw-Timestamp",
				MaxAge:          5 * time.Minute,
			},
		},
	}).Handler()

	body, err := json.Marshal(controlplane.ApprovalResolveCallbackRequest{
		Provider: "jira",
		TicketID: "appr-1",
		Status:   "approved",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/callbacks/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HopClaw-Timestamp", strconv.FormatInt(time.Now().UTC().Unix(), 10))
	req.Header.Set("X-HopClaw-Signature", "sha256=bad")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST callback status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServerSyncApprovalsEndpoint(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		syncer: syncerFunc(func(context.Context) error { return nil }),
	})
	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/sync", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST sync status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServerSyncApprovalsEndpointReturnsUnavailableWithoutSyncer(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{})
	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/sync", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST sync status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServerArtifactEndpoints(t *testing.T) {
	t.Parallel()

	artifacts := artifact.NewInMemoryStore()
	blob, err := artifacts.Put(context.Background(), artifact.PutRequest{
		Body:        []byte("artifact payload"),
		ContentType: "text/plain; charset=utf-8",
		Metadata: map[string]any{
			"tool_name": "fs.read",
		},
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	svc := newRuntimeService(t, runtimeFixture{
		model:     &serverModelClient{},
		artifacts: artifacts,
	})
	handler := New(svc, Config{}).Handler()

	got := getArtifact(t, handler, blob.ID)
	if got.ID != blob.ID || got.Metadata["tool_name"] != "fs.read" {
		t.Fatalf("artifact meta = %#v", got)
	}
	body := getArtifactContent(t, handler, blob.ID)
	if body != "artifact payload" {
		t.Fatalf("artifact content = %q", body)
	}
}

func TestServerArtifactListAndPruneEndpoints(t *testing.T) {
	t.Parallel()

	artifacts := artifact.NewInMemoryStore()
	oldBlob, err := artifacts.Put(context.Background(), artifact.PutRequest{
		Body: []byte("old payload"),
		Metadata: map[string]any{
			"run_id":    "run-1",
			"tool_name": "fs.read",
		},
	})
	if err != nil {
		t.Fatalf("Put(old) error = %v", err)
	}
	_, err = artifacts.Put(context.Background(), artifact.PutRequest{
		Body: []byte("other payload"),
		Metadata: map[string]any{
			"run_id":    "run-2",
			"tool_name": "fs.write",
		},
	})
	if err != nil {
		t.Fatalf("Put(other) error = %v", err)
	}

	bus := eventbus.NewInMemoryBus()
	svc := newRuntimeService(t, runtimeFixture{
		model:     &serverModelClient{},
		artifacts: artifacts,
		bus:       bus,
	})
	handler := New(svc, Config{}).Handler()

	items := listArtifacts(t, handler, "/runtime/artifacts?run_id=run-1")
	if len(items) != 1 || items[0].ID != oldBlob.ID {
		t.Fatalf("listArtifacts(run-1) = %#v", items)
	}

	result := pruneArtifacts(t, handler, map[string]any{
		"run_id": "run-1",
	})
	if result.DeletedCount != 1 || len(result.DeletedIDs) != 1 || result.DeletedIDs[0] != oldBlob.ID {
		t.Fatalf("pruneArtifacts() = %#v", result)
	}
	items = listArtifacts(t, handler, "/runtime/artifacts?run_id=run-1")
	if len(items) != 0 {
		t.Fatalf("expected run-1 artifacts to be pruned, got %#v", items)
	}
	events := getEvents(t, handler, "/runtime/events")
	if len(events) == 0 || events[len(events)-1].Type != eventbus.EventArtifactPruned {
		t.Fatalf("events = %#v", events)
	}
}

func TestServerToolsEndpointReturnsRuntimeToolManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
		tools: toolruntime.NewBuiltins(toolruntime.BuiltinsConfig{Root: root}),
	})
	handler := New(svc, Config{}).Handler()

	tools := listTools(t, handler, "/runtime/tools")
	if len(tools) == 0 {
		t.Fatal("expected tool manifests")
	}
	var fsRead *agent.ToolDefinition
	for i := range tools {
		if tools[i].Name == "fs.read" {
			fsRead = &tools[i]
			break
		}
	}
	if fsRead == nil {
		t.Fatalf("tools = %#v", tools)
	}
	if fsRead.SideEffectClass != "read" || len(fsRead.InputSchema) == 0 || len(fsRead.OutputSchema) == 0 {
		t.Fatalf("fs.read tool = %#v", *fsRead)
	}
}

func TestServerToolsEndpointReturnsSkillToolsForSession(t *testing.T) {
	t.Parallel()

	skillSvc := newSkillService(t, "bundle.review", "read")
	svc := newRuntimeService(t, runtimeFixture{
		model:  &serverModelClient{},
		skills: skillSvc,
	})
	handler := New(svc, Config{}).Handler()

	tools := listTools(t, handler, "/runtime/tools?session_key=skill-session")
	var review *agent.ToolDefinition
	for i := range tools {
		if tools[i].Name == "bundle.review" {
			review = &tools[i]
			break
		}
	}
	if review == nil {
		t.Fatalf("tools = %#v", tools)
	}
	if review.SideEffectClass != "read" || review.ExecutionKey != "session:{id}" || len(review.InputSchema) == 0 {
		t.Fatalf("bundle.review tool = %#v", *review)
	}
}

func TestServerAuthTokenProtectsRuntimeEndpoints(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	handler := New(svc, Config{AuthToken: "test-token"}).Handler()

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d body=%s", healthRec.Code, healthRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/runtime/events", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /runtime/events without token status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/runtime/events", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/events with bearer token status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/runtime/events", nil)
	req.Header.Set("X-OpenClaw-Token", "test-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/events with legacy token header status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServerHealthProjectsOperationalWarningsAsDegraded(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	handler := New(svc, Config{
		OperationalWarnings: serverOperationalWarningSourceStub{warnings: []controlplane.OperationalWarning{{
			Component: "channel/slack/delivery",
			Summary:   `Channel "slack" recently failed to deliver replies`,
		}}},
	}).Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.OK {
		t.Fatalf("payload.OK = true, want degraded payload: %+v", payload)
	}
	if payload.State != "degraded" {
		t.Fatalf("payload.State = %q, want degraded", payload.State)
	}
	if payload.Summary != `Channel "slack" recently failed to deliver replies` {
		t.Fatalf("payload.Summary = %q", payload.Summary)
	}
	if len(payload.Warnings) != 1 || payload.Warnings[0] != `Channel "slack" recently failed to deliver replies` {
		t.Fatalf("payload.Warnings = %#v", payload.Warnings)
	}
}

func TestRuntimeHandlerBypassesServerAuthWrapper(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{})
	srv := New(svc, Config{AuthToken: "test-token"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/runtime/events", nil)
	srv.RuntimeHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("RuntimeHandler GET /runtime/events status = %d body=%s", rec.Code, rec.Body.String())
	}

	protectedRec := httptest.NewRecorder()
	protectedReq := httptest.NewRequest(http.MethodGet, "/runtime/events", nil)
	srv.Handler().ServeHTTP(protectedRec, protectedReq)
	if protectedRec.Code != http.StatusUnauthorized {
		t.Fatalf("Handler GET /runtime/events status = %d body=%s", protectedRec.Code, protectedRec.Body.String())
	}
}

func TestRuntimeWebSocketRouteBypassesHTTPAuthWrapper(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	hub := NewWSHub(eventbus.NewInMemoryBus())
	hub.Start()
	t.Cleanup(func() { hub.Stop() })

	handler := New(svc, Config{AuthToken: "test-token", WSHub: hub}).Handler()

	req := httptest.NewRequest(http.MethodGet, RuntimeWebSocketPath, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("GET %s should bypass HTTP auth wrapper, got %d body=%s", RuntimeWebSocketPath, rec.Code, rec.Body.String())
	}
}

func TestServerLandingPageSupportsEnglishAndChinese(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	handler := New(svc, Config{AuthToken: "test-token"}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="landing-shell"`) {
		t.Fatalf("english landing page missing landing shell testid: %s", body)
	}
	if !strings.Contains(body, `data-locale="en"`) {
		t.Fatalf("english landing page missing structured locale marker: %s", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/?lang=zh-CN", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /?lang=zh-CN status = %d body=%s", rec.Code, rec.Body.String())
	}
	body = rec.Body.String()
	if !strings.Contains(body, `data-testid="landing-title"`) {
		t.Fatalf("chinese landing page missing landing title testid: %s", body)
	}
	if !strings.Contains(body, `data-locale="zh-CN"`) {
		t.Fatalf("chinese landing page missing structured locale marker: %s", body)
	}
}

func TestServerLandingPageUsesAcceptLanguage(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / with Accept-Language status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-locale="zh-CN"`) {
		t.Fatalf("landing page did not switch structured locale marker: %s", body)
	}
	if !strings.Contains(body, `data-testid="landing-card-web"`) {
		t.Fatalf("landing page missing structured web card testid: %s", body)
	}
}

func TestServerEventsLimitUsesConfiguredWindow(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	for i := 0; i < 5; i++ {
		if err := bus.Publish(context.Background(), eventbus.Event{
			Type:  eventbus.EventRunCompleted,
			RunID: "run-limit",
		}); err != nil {
			t.Fatalf("Publish() error = %v", err)
		}
	}

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
		bus:   bus,
	})
	handler := New(svc, Config{MaxEventResults: 2}).Handler()

	events := getEvents(t, handler, "/runtime/events")
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
}

func TestReplayEventIDsSkipsDuplicatesOnlyOnce(t *testing.T) {
	t.Parallel()

	replayed := replayEventIDs([]eventbus.Event{
		{ID: "evt-1"},
		{ID: "evt-2"},
	})
	if !shouldSkipReplayedEvent(replayed, "evt-1") {
		t.Fatal("expected evt-1 to be treated as replay duplicate")
	}
	if shouldSkipReplayedEvent(replayed, "evt-1") {
		t.Fatal("evt-1 should only be skipped once")
	}
	if shouldSkipReplayedEvent(replayed, "evt-3") {
		t.Fatal("unexpected skip for non-replayed event")
	}
}

type runtimeFixture struct {
	model      agent.ModelClient
	tools      agent.ToolExecutor
	policy     policy.Engine
	approvals  approval.Store
	artifacts  artifact.Store
	bus        *eventbus.InMemoryBus
	hooks      *hooks.Executor
	skills     *skill.Service
	preflight  agent.PreflightAnalyzer
	classifier rt.InteractionClassifier
	syncer     rt.ApprovalSyncer
	governance rt.GovernanceDeliveryController
}

func newRuntimeService(t *testing.T, fixture runtimeFixture) *rt.Service {
	t.Helper()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	if fixture.bus == nil {
		fixture.bus = eventbus.NewInMemoryBus()
	}
	engine := contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "You are a test runtime.",
		IncludeSkillCatalog:  false,
		DefaultContextWindow: 512,
		DefaultOutputTokens:  64,
	}, fixture.skills)
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 4,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, queue, engine, fixture.model, fixture.tools, nil)
	if fixture.preflight != nil {
		component.WithPreflightAnalyzer(fixture.preflight)
	}
	if fixture.policy != nil {
		component.WithPolicy(fixture.policy)
	}
	if fixture.approvals != nil {
		component.WithApprovals(fixture.approvals)
	}
	if fixture.hooks != nil {
		component.WithHooks(fixture.hooks)
	}
	component.WithEventBus(fixture.bus)
	svc := rt.NewService(component, sessions, runs, fixture.approvals, fixture.bus, fixture.artifacts)
	if fixture.classifier != nil {
		svc.WithClassifier(fixture.classifier)
	}
	if fixture.syncer != nil {
		svc.WithApprovalSyncer(fixture.syncer)
	}
	if fixture.governance != nil {
		svc.WithGovernanceDelivery(fixture.governance)
	}
	return svc
}

type syncerFunc func(context.Context) error

func (f syncerFunc) SyncPendingApprovals(ctx context.Context) error {
	return f(ctx)
}

func newSkillService(t *testing.T, toolName, sideEffect string) *skill.Service {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "writer")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	skillContent := `---
name: writer
description: write files
---
# writer
`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(run.sh) error = %v", err)
	}
	manifest := map[string]any{
		"version": "1",
		"tool": map[string]any{
			"name": toolName,
			"input_schema": map[string]any{
				"type": "object",
			},
			"output_schema": map[string]any{
				"type": "object",
			},
			"side_effect_class": sideEffect,
			"execution_key":     "session:{id}",
		},
		"runtime": map[string]any{
			"entry": "scripts/run.sh",
			"shell": "bash",
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal(manifest) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.manifest.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.manifest.json) error = %v", err)
	}

	svc := skill.NewService(skill.ServiceConfig{
		Roots: []skill.DiscoveryRoot{{Kind: skill.SourceWorkspace, Path: root}},
	})
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	return svc
}

type runtimeBlockingPreflightAnalyzer struct{}

type serverStubInteractionClassifier struct {
	decision rt.InteractionDecision
	err      error
}

type serverRequireApprovalPolicyEngine struct{}

func (serverRequireApprovalPolicyEngine) EvaluateTool(context.Context, policy.ToolContext) (policy.Decision, error) {
	return policy.Decision{
		Action:       policy.ActionRequireApproval,
		Reasons:      []string{"tool requires approval by server test policy"},
		PolicySource: "test.policy/server",
		Summary:      "approval required by server test policy",
	}, nil
}

func (s serverStubInteractionClassifier) Classify(context.Context, rt.InteractionClassifyRequest) (rt.InteractionDecision, error) {
	if s.err != nil {
		return rt.InteractionDecision{}, s.err
	}
	return s.decision, nil
}

func (runtimeBlockingPreflightAnalyzer) Analyze(_ context.Context, req agent.PreflightAnalysisRequest) (agent.PreflightAnalysis, error) {
	if strings.Contains(req.Message, "这个文件") || strings.Contains(strings.ToLower(req.Message), "this file") {
		return agent.PreflightAnalysis{NeedsReference: true}, nil
	}
	return agent.PreflightAnalysis{}, nil
}

func postRun(t *testing.T, handler http.Handler, body map[string]any, wantStatus int) agent.Run {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal(body) error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtime/runs", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("POST /runtime/runs status = %d body=%s", rec.Code, rec.Body.String())
	}
	var run agent.Run
	if err := json.NewDecoder(rec.Body).Decode(&run); err != nil {
		t.Fatalf("Decode(run) error = %v", err)
	}
	return run
}

func postInteract(t *testing.T, handler http.Handler, body map[string]any, wantStatus int) interactResponse {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal(body) error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtime/interact", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("POST /runtime/interact status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp interactResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode(interactResponse) error = %v", err)
	}
	return resp
}

func getRun(t *testing.T, handler http.Handler, id string) agent.Run {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/runtime/runs/"+id, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs/%s status = %d body=%s", id, rec.Code, rec.Body.String())
	}
	var run agent.Run
	if err := json.NewDecoder(rec.Body).Decode(&run); err != nil {
		t.Fatalf("Decode(run) error = %v", err)
	}
	return run
}

func getRunResult(t *testing.T, handler http.Handler, id string) rt.RunResult {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/runtime/runs/"+id+"/result", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs/%s/result status = %d body=%s", id, rec.Code, rec.Body.String())
	}
	var result rt.RunResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("Decode(result) error = %v", err)
	}
	return result
}

func getRunCompletion(t *testing.T, handler http.Handler, id string) rt.RunCompletion {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/runtime/runs/"+id+"/completion", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs/%s/completion status = %d body=%s", id, rec.Code, rec.Body.String())
	}
	var result rt.RunCompletion
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("Decode(completion) error = %v", err)
	}
	return result
}

func getRunVerification(t *testing.T, handler http.Handler, id string) rtverify.RunVerification {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/runtime/runs/"+id+"/verification", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/runs/%s/verification status = %d body=%s", id, rec.Code, rec.Body.String())
	}
	var result rtverify.RunVerification
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("Decode(verification) error = %v", err)
	}
	return result
}

func waitForRunStatus(t *testing.T, handler http.Handler, id string, want agent.RunStatus) agent.Run {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run := getRun(t, handler, id)
		if run.Status == want {
			return run
		}
		if run.Status.Terminal() && run.Status != want {
			t.Fatalf("run %s reached terminal status %q with error %q, want %q", id, run.Status, run.Error, want)
		}
		time.Sleep(10 * time.Millisecond)
	}

	run := getRun(t, handler, id)
	t.Fatalf("run %s status = %q with error %q, want %q", id, run.Status, run.Error, want)
	return agent.Run{}
}

func listApprovals(t *testing.T, handler http.Handler, status string) []*rt.ApprovalView {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/runtime/approvals?status="+status, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/approvals status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []*rt.ApprovalView `json:"items"`
		Count int                `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(approvals) error = %v", err)
	}
	if payload.Count != len(payload.Items) {
		t.Fatalf("approval count = %d, want %d", payload.Count, len(payload.Items))
	}
	return payload.Items
}

func getApproval(t *testing.T, handler http.Handler, id string) rt.ApprovalView {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/runtime/approvals/"+id, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/approvals/%s status = %d body=%s", id, rec.Code, rec.Body.String())
	}
	var ticket rt.ApprovalView
	if err := json.NewDecoder(rec.Body).Decode(&ticket); err != nil {
		t.Fatalf("Decode(ticket) error = %v", err)
	}
	return ticket
}

func resolveApproval(t *testing.T, handler http.Handler, id string, resolution approval.Resolution) rt.ApprovalView {
	t.Helper()
	data, err := json.Marshal(resolution)
	if err != nil {
		t.Fatalf("Marshal(resolution) error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/"+id+"/resolve", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /runtime/approvals/%s/resolve status = %d body=%s", id, rec.Code, rec.Body.String())
	}
	var ticket rt.ApprovalView
	if err := json.NewDecoder(rec.Body).Decode(&ticket); err != nil {
		t.Fatalf("Decode(ticket) error = %v", err)
	}
	return ticket
}

func getEvents(t *testing.T, handler http.Handler, path string) []eventbus.Event {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s", path, rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []eventbus.Event `json:"items"`
		Count int              `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(events) error = %v", err)
	}
	if payload.Count != len(payload.Items) {
		t.Fatalf("event count = %d, want %d", payload.Count, len(payload.Items))
	}
	return payload.Items
}

func getArtifact(t *testing.T, handler http.Handler, id string) artifact.Blob {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/runtime/artifacts/"+id, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/artifacts/%s status = %d body=%s", id, rec.Code, rec.Body.String())
	}
	var blob artifact.Blob
	if err := json.NewDecoder(rec.Body).Decode(&blob); err != nil {
		t.Fatalf("Decode(blob) error = %v", err)
	}
	return blob
}

func getArtifactContent(t *testing.T, handler http.Handler, id string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/runtime/artifacts/"+id+"/content", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/artifacts/%s/content status = %d body=%s", id, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func listTools(t *testing.T, handler http.Handler, path string) []agent.ToolDefinition {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s", path, rec.Code, rec.Body.String())
	}
	var body struct {
		Items []agent.ToolDefinition `json:"items"`
		Count int                    `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal(list tools) error = %v", err)
	}
	if body.Count != len(body.Items) {
		t.Fatalf("tool count = %d, want %d", body.Count, len(body.Items))
	}
	return body.Items
}

func listArtifacts(t *testing.T, handler http.Handler, path string) []artifact.Blob {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s", path, rec.Code, rec.Body.String())
	}
	var body struct {
		Items []artifact.Blob `json:"items"`
		Count int             `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal(list artifacts) error = %v", err)
	}
	if body.Count != len(body.Items) {
		t.Fatalf("artifact count = %d, want %d", body.Count, len(body.Items))
	}
	return body.Items
}

func pruneArtifacts(t *testing.T, handler http.Handler, payload map[string]any) artifact.PruneResult {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal(prune payload) error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtime/artifacts/prune", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /runtime/artifacts/prune status = %d body=%s", rec.Code, rec.Body.String())
	}
	var result artifact.PruneResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal(prune result) error = %v", err)
	}
	return result
}

type serverModelClient struct {
	responses []*agent.ModelResponse
	index     int
}

func (s *serverModelClient) Chat(_ context.Context, _ agent.ChatRequest) (*agent.ModelResponse, error) {
	if s.index >= len(s.responses) {
		return &agent.ModelResponse{}, nil
	}
	resp := s.responses[s.index]
	s.index++
	return resp, nil
}

type serverToolExecutor struct {
	results []contextengine.ToolResult
}

func (s serverToolExecutor) ExecuteBatch(context.Context, *agent.Run, *agent.Session, []agent.ToolCall) ([]contextengine.ToolResult, error) {
	return append([]contextengine.ToolResult(nil), s.results...), nil
}

type serverCountingToolExecutor struct {
	serverToolExecutor
	calls atomic.Int32
}

func (s *serverCountingToolExecutor) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	s.calls.Add(1)
	return s.serverToolExecutor.ExecuteBatch(ctx, run, session, calls)
}
