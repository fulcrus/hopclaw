package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/planner"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func containsAnySubstring(text string, wants ...string) bool {
	for _, want := range wants {
		if strings.Contains(text, want) {
			return true
		}
	}
	return false
}

func TestApprovalViewToPermissionRequestCarriesGovernanceMetadata(t *testing.T) {
	view := &runtimesvc.ApprovalView{
		Ticket: approval.Ticket{
			ID:        "apr-442",
			SessionID: "sess-1",
			Reasons:   []string{"requires operator approval"},
			ToolCalls: []approval.ToolCall{{
				Name:  "exec.shell",
				Input: map[string]any{"cmd": "rm -rf /tmp/logs"},
				ResourceScope: approval.ResourceScope{
					CommandPrefixes: []string{"rm -rf /tmp/logs"},
				},
			}},
			Metadata: map[string]any{
				"harness_requires_external_side_effect": true,
			},
		},
		ResourceScopeSummary: "commands=rm -rf /tmp/logs",
		Governance: &runtimesvc.GovernanceReceipt{
			Summary: "remote write requires approval | automation=nightly",
			Scope:   controlplane.ScopeRef{AutomationID: "nightly"},
			Policy: &controlplane.GovernanceDecision{
				Action:  controlplane.GovernanceDecisionRequireApproval,
				Summary: "remote write requires approval",
				ApprovalPolicy: &controlplane.GovernanceDecisionApprovalPolicy{
					DefaultScope: approval.ScopeOnce,
					MaxScope:     approval.ScopeSession,
				},
			},
		},
	}

	req := approvalViewToPermissionRequest(view)
	if req == nil {
		t.Fatal("approvalViewToPermissionRequest() = nil")
	}
	if req.RequestID != "apr-442" {
		t.Fatalf("RequestID = %q, want %q", req.RequestID, "apr-442")
	}
	if req.ToolName != "exec.shell" {
		t.Fatalf("ToolName = %q, want %q", req.ToolName, "exec.shell")
	}
	if !strings.Contains(req.Input, `"cmd":"rm -rf /tmp/logs"`) {
		t.Fatalf("Input = %q, want marshaled tool input", req.Input)
	}
	if req.PolicyAction != string(controlplane.GovernanceDecisionRequireApproval) {
		t.Fatalf("PolicyAction = %q, want %q", req.PolicyAction, controlplane.GovernanceDecisionRequireApproval)
	}
	if req.PolicySummary != "remote write requires approval" {
		t.Fatalf("PolicySummary = %q", req.PolicySummary)
	}
	if req.ScopeSummary != "automation=nightly" {
		t.Fatalf("ScopeSummary = %q, want %q", req.ScopeSummary, "automation=nightly")
	}
	if req.ResourceScopeSummary != "commands=rm -rf /tmp/logs" {
		t.Fatalf("ResourceScopeSummary = %q", req.ResourceScopeSummary)
	}
	if req.DefaultGrantScope != "once" || req.MaxGrantScope != "session" {
		t.Fatalf("grant scopes = (%q, %q), want (once, session)", req.DefaultGrantScope, req.MaxGrantScope)
	}
	if !req.RequiresExternalSideEffect {
		t.Fatal("RequiresExternalSideEffect = false, want true")
	}
}

func TestRootCommandFallsBackToInteractiveREPL(t *testing.T) {
	var runRequest struct {
		SessionKey string `json:"session_key"`
		Content    string `json:"content"`
		Model      string `json:"model"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
		case "/operator/skills":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/models":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"providers": []map[string]any{{
					"name":          "openai",
					"default_model": "gpt-4o",
					"models": []map[string]any{{
						"model":          "gpt-4o",
						"context_window": 128000,
						"capabilities":   []string{"reasoning"},
					}},
					"capability_matrix": map[string]any{
						"model":              "gpt-4o",
						"context_window":     128000,
						"supports_reasoning": true,
					},
				}},
			})
		case "/runtime/sessions":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/runtime/events":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/runtime/runs", "/runtime/interact":
			if r.URL.Path == "/runtime/runs" && r.Method == http.MethodGet {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&runRequest); err != nil {
				t.Fatalf("decode interactive submit body: %v", err)
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"decision": map[string]any{"reply_act": "task_accept"},
				"run": map[string]any{
					"id":         "run-1",
					"session_id": "sess-1",
					"status":     "running",
					"model":      "gpt-4o",
				},
				"submit_request": map[string]any{
					"session_key": runRequest.SessionKey,
					"content":     runRequest.Content,
				},
			})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-1",
				Type:      eventbus.EventModelTextDelta,
				RunID:     "run-1",
				SessionID: "sess-1",
				Time:      time.Now().UTC(),
				Attrs:     map[string]any{"delta": "hello from gateway\n"},
			})
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-1b",
				Type:      eventbus.EventModelStreamComplete,
				RunID:     "run-1",
				SessionID: "sess-1",
				Time:      time.Now().UTC(),
				Attrs: map[string]any{
					"prompt_tokens":     9,
					"completion_tokens": 3,
					"total_tokens":      12,
				},
			})
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-2",
				Type:      eventbus.EventRunCompleted,
				RunID:     "run-1",
				SessionID: "sess-1",
				Time:      time.Now().UTC(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := writeInteractiveConfig(t, server.URL)
	t.Setenv("HOPCLAW_CONFIG", configPath)
	t.Setenv("HOME", t.TempDir())

	root := newRootCmd()
	var stdout strings.Builder
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"--model", "gpt-4o", "hello", "world"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if runRequest.Content != "hello world" {
		t.Fatalf("run content = %q, want %q", runRequest.Content, "hello world")
	}
	if runRequest.SessionKey == "" || runRequest.SessionKey == "default" {
		t.Fatalf("run session key = %q, want generated non-default session key", runRequest.SessionKey)
	}
	if runRequest.Model != "gpt-4o" {
		t.Fatalf("run model = %q, want %q", runRequest.Model, "gpt-4o")
	}
	if got := stdout.String(); !strings.Contains(got, "hello from gateway") {
		t.Fatalf("stdout = %q, want streamed gateway output", got)
	}
	if got := stdout.String(); !containsAnySubstring(got, "tokens: 9 in · 3 out · 12 total", "Tokens    9 in / 3 out") {
		t.Fatalf("stdout = %q, want completion usage summary", got)
	}
	if got := stdout.String(); !containsAnySubstring(got, "[card] Task Completed", "* Completed") {
		t.Fatalf("stdout = %q, want completion status", got)
	}
}

func TestRootCommandPipeModeRunsSingleInputAndExits(t *testing.T) {
	var runRequest struct {
		SessionKey string `json:"session_key"`
		Content    string `json:"content"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
		case "/operator/skills":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/models":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"providers": []any{}})
		case "/runtime/sessions":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/runtime/events":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/runtime/runs", "/runtime/interact":
			if r.URL.Path == "/runtime/runs" && r.Method == http.MethodGet {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&runRequest); err != nil {
				t.Fatalf("decode interactive submit body: %v", err)
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"decision": map[string]any{"reply_act": "task_accept"},
				"run": map[string]any{
					"id":         "run-pipe",
					"session_id": "sess-pipe",
					"status":     "running",
				},
				"submit_request": map[string]any{
					"session_key": runRequest.SessionKey,
					"content":     runRequest.Content,
				},
			})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-pipe-1",
				Type:      eventbus.EventModelTextDelta,
				RunID:     "run-pipe",
				SessionID: "sess-pipe",
				Time:      time.Now().UTC(),
				Attrs:     map[string]any{"delta": "pipe ok\n"},
			})
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-pipe-2",
				Type:      eventbus.EventRunCompleted,
				RunID:     "run-pipe",
				SessionID: "sess-pipe",
				Time:      time.Now().UTC(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := writeInteractiveConfig(t, server.URL)
	t.Setenv("HOPCLAW_CONFIG", configPath)
	t.Setenv("HOME", t.TempDir())

	root := newRootCmd()
	var stdout strings.Builder
	root.SetIn(strings.NewReader("hello from stdin\n"))
	root.SetOut(&stdout)
	root.SetErr(&stdout)

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if runRequest.Content != "hello from stdin" {
		t.Fatalf("run content = %q, want %q", runRequest.Content, "hello from stdin")
	}
	if strings.Contains(stdout.String(), "> ") {
		t.Fatalf("stdout = %q, want one-shot pipe mode without prompt loop", stdout.String())
	}
	if !strings.Contains(stdout.String(), "pipe ok") {
		t.Fatalf("stdout = %q, want streamed pipe output", stdout.String())
	}
}

func TestRootCommandOneShotSplitsAssistantStdoutAndStatusStderr(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
		case "/operator/skills":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/models":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"providers": []any{}})
		case "/runtime/sessions":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/runtime/events":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/runtime/runs", "/runtime/interact":
			if r.URL.Path == "/runtime/runs" && r.Method == http.MethodGet {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
				return
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"decision": map[string]any{"reply_act": "task_accept"},
				"run": map[string]any{
					"id":         "run-split",
					"session_id": "sess-split",
					"status":     "running",
				},
				"submit_request": map[string]any{
					"session_key": "ignored",
					"content":     "hello",
				},
			})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-split-1",
				Type:      eventbus.EventModelTextDelta,
				RunID:     "run-split",
				SessionID: "sess-split",
				Time:      time.Now().UTC(),
				Attrs:     map[string]any{"delta": "assistant only\n"},
			})
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-split-2",
				Type:      eventbus.EventModelStreamComplete,
				RunID:     "run-split",
				SessionID: "sess-split",
				Time:      time.Now().UTC(),
				Attrs: map[string]any{
					"prompt_tokens":     9,
					"completion_tokens": 3,
					"total_tokens":      12,
				},
			})
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-split-3",
				Type:      eventbus.EventRunCompleted,
				RunID:     "run-split",
				SessionID: "sess-split",
				Time:      time.Now().UTC(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := writeInteractiveConfig(t, server.URL)
	t.Setenv("HOPCLAW_CONFIG", configPath)
	t.Setenv("HOME", t.TempDir())

	root := newRootCmd()
	var stdout strings.Builder
	var stderr strings.Builder
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"hello"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := stdout.String(); !strings.Contains(got, "assistant only") {
		t.Fatalf("stdout = %q, want assistant body", got)
	}
	for _, unwanted := range []string{"tokens:", "HopClaw", "[LOCAL]", "[system]"} {
		if strings.Contains(stdout.String(), unwanted) {
			t.Fatalf("stdout leaked status %q: %q", unwanted, stdout.String())
		}
	}
	for _, want := range []string{"* Thinking", "[card] Task Completed"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr missing %q: %q", want, stderr.String())
		}
	}
	if strings.Contains(stderr.String(), "[task]") {
		t.Fatalf("stderr should not contain task snapshots in one-shot mode: %q", stderr.String())
	}
	if !containsAnySubstring(stderr.String(), "tokens: 9 in · 3 out · 12 total", "Tokens    9 in / 3 out") {
		t.Fatalf("stderr missing usage summary: %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "HopClaw") || strings.Contains(stderr.String(), "[LOCAL]") {
		t.Fatalf("stderr should not contain banner or dock in one-shot mode: %q", stderr.String())
	}
}

func TestMapRunDetailIncludesSupervisorBlocks(t *testing.T) {
	view := &runtimesvc.RunListView{
		ID:            "run-graph",
		SessionID:     "sess-1",
		Status:        agent.RunRunning,
		Phase:         agent.PhaseExecutingTools,
		ExecutionMode: agent.ExecutionModeWorkflow,
		Delegation: &agent.DelegationContract{
			SideEffectClass: "workspace_write",
		},
		Governance: &runtimesvc.GovernanceReceipt{
			Policy: &controlplane.GovernanceDecision{
				Action:  controlplane.GovernanceDecisionRequireApproval,
				Summary: "requires approval for remote write",
			},
		},
		SemanticSignal: &agent.SemanticSignal{
			Language: agent.LanguageProfile{
				Family:           "es",
				Script:           "Latn",
				MainSemanticPath: true,
			},
			RequiresCurrentInfo: true,
			SuggestedDomains:    []string{"browser", "fs"},
			JobType:             "report",
			TargetSummary:       "docs/tmp/resumen.md",
			TriageReady:         true,
			TaskContractReady:   true,
			Reason:              "fresh_page_state",
		},
		ExecutionGraph: &agent.ExecutionGraph{
			SingleSession:  true,
			SessionLocking: true,
			Tasks: []agent.ExecutionTask{{
				ID:              "task-1",
				Title:           "compare deliveries",
				Goal:            "compare receipts",
				Status:          planner.TaskRunning,
				AttemptCount:    2,
				MergeStrategy:   agent.MergeStrategyTaskOrder,
				SideEffectScope: agent.SideEffectScopeWorkspace,
				ResourceKeys:    []string{"file:plan.md", "webhook:ops"},
			}},
		},
		WorkflowState: &agent.WorkflowState{
			ContinuationIndex: 2,
			TotalRoundsUsed:   7,
			Yielded:           true,
			YieldReason:       "budget_soft_limit",
		},
	}

	detail := mapRunDetail(view, nil, "")
	if detail == nil {
		t.Fatal("mapRunDetail() = nil")
	}
	if detail.ScopeDetails == nil || detail.ScopeDetails.SideEffectScope != "workspace_write" || !detail.ScopeDetails.Destructive {
		t.Fatalf("ScopeDetails = %#v", detail.ScopeDetails)
	}
	if detail.Semantic == nil || detail.Semantic.Language != "es-Latn" || !detail.Semantic.RequiresCurrentInfo {
		t.Fatalf("Semantic = %#v", detail.Semantic)
	}
	if detail.Semantic.TargetSummary != "docs/tmp/resumen.md" || !detail.Semantic.TaskContractReady {
		t.Fatalf("Semantic = %#v", detail.Semantic)
	}
	if detail.Workflow == nil || detail.Workflow.Mode != "workflow" || detail.Workflow.ContinuationIndex != 2 {
		t.Fatalf("Workflow = %#v", detail.Workflow)
	}
	if detail.Delegation == nil || !detail.Delegation.Enabled || detail.Delegation.ParallelTasks != 1 {
		t.Fatalf("Delegation = %#v", detail.Delegation)
	}
	if detail.ExecutionGraph == nil || len(detail.ExecutionGraph.Tasks) != 1 || detail.ExecutionGraph.Tasks[0].Title != "compare deliveries" {
		t.Fatalf("ExecutionGraph = %#v", detail.ExecutionGraph)
	}
}

func TestRootCommandOneShotReturnsErrorOnRunFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
		case "/operator/skills":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/models":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"providers": []any{}})
		case "/runtime/sessions":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/runtime/events":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/runtime/runs", "/runtime/interact":
			if r.URL.Path == "/runtime/runs" && r.Method == http.MethodGet {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
				return
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"decision": map[string]any{"reply_act": "task_accept"},
				"run": map[string]any{
					"id":         "run-failed",
					"session_id": "sess-failed",
					"status":     "running",
				},
				"submit_request": map[string]any{
					"session_key": "ignored",
					"content":     "hello",
				},
			})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-failed-1",
				Type:      eventbus.EventRunFailed,
				RunID:     "run-failed",
				SessionID: "sess-failed",
				Time:      time.Now().UTC(),
				Attrs:     map[string]any{"error": "gateway unreachable"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := writeInteractiveConfig(t, server.URL)
	t.Setenv("HOPCLAW_CONFIG", configPath)
	t.Setenv("HOME", t.TempDir())

	root := newRootCmd()
	var stdout strings.Builder
	var stderr strings.Builder
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"hello"})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "gateway unreachable") {
		t.Fatalf("Execute() error = %v, want gateway failure", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no assistant output on failure", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[error] gateway unreachable") {
		t.Fatalf("stderr = %q, want rendered error", stderr.String())
	}
}

func TestRootCommandDisplaysModelFailoverNotification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
		case "/operator/skills":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/models":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"providers": []any{}})
		case "/runtime/sessions":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/runtime/events":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/runtime/runs", "/runtime/interact":
			if r.URL.Path == "/runtime/runs" && r.Method == http.MethodGet {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
				return
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"decision": map[string]any{"reply_act": "task_accept"},
				"run": map[string]any{
					"id":         "run-failover",
					"session_id": "sess-failover",
					"status":     "running",
				},
				"submit_request": map[string]any{
					"session_key": "ignored",
					"content":     "hello",
				},
			})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-failover-1",
				Type:      eventbus.EventModelFailover,
				RunID:     "run-failover",
				SessionID: "sess-failover",
				Time:      time.Now().UTC(),
				Attrs: map[string]any{
					"from_model": "gpt-4o",
					"to_model":   "fallback/model-b",
					"reason":     "rate limit",
				},
			})
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-failover-2",
				Type:      eventbus.EventModelTextDelta,
				RunID:     "run-failover",
				SessionID: "sess-failover",
				Time:      time.Now().UTC(),
				Attrs:     map[string]any{"delta": "done\n"},
			})
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-failover-3",
				Type:      eventbus.EventRunCompleted,
				RunID:     "run-failover",
				SessionID: "sess-failover",
				Time:      time.Now().UTC(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := writeInteractiveConfig(t, server.URL)
	t.Setenv("HOPCLAW_CONFIG", configPath)
	t.Setenv("HOME", t.TempDir())

	root := newRootCmd()
	var stdout strings.Builder
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"hello"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "[model failover] gpt-4o → fallback/model-b") {
		t.Fatalf("stdout = %q, want failover notification", output)
	}
	if !strings.Contains(output, "done") {
		t.Fatalf("stdout = %q, want streamed output", output)
	}
}

func TestRootCommandRejectsLocalAndRemoteFlagsTogether(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	root := newRootCmd()
	root.SetIn(strings.NewReader(""))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"--local", "--remote", "http://127.0.0.1:16280", "hello"})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--local and --remote cannot be used together") {
		t.Fatalf("Execute() error = %v, want mutually-exclusive flags error", err)
	}
}

func TestRootCommandUsesSavedNamedTarget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setupTargetTestKeychain(t)

	var runRequest struct {
		SessionKey string `json:"session_key"`
		Content    string `json:"content"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
		case "/operator/skills":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/models":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"providers": []any{}})
		case "/runtime/sessions":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/runtime/events":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/runtime/runs", "/runtime/interact":
			if r.URL.Path == "/runtime/runs" && r.Method == http.MethodGet {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&runRequest); err != nil {
				t.Fatalf("decode interactive submit body: %v", err)
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"decision": map[string]any{"reply_act": "task_accept"},
				"run": map[string]any{
					"id":         "run-remote",
					"session_id": "sess-remote",
					"status":     "running",
				},
				"submit_request": map[string]any{
					"session_key": runRequest.SessionKey,
					"content":     runRequest.Content,
				},
			})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-remote-1",
				Type:      eventbus.EventModelTextDelta,
				RunID:     "run-remote",
				SessionID: "sess-remote",
				Time:      time.Now().UTC(),
				Attrs:     map[string]any{"delta": "named remote ok\n"},
			})
			writeSSEEvent(t, w, eventbus.Event{
				ID:        "evt-remote-2",
				Type:      eventbus.EventRunCompleted,
				RunID:     "run-remote",
				SessionID: "sess-remote",
				Time:      time.Now().UTC(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := addSavedTargetProfile(savedTargetProfile{
		Name:    "prod",
		Kind:    targetKindRemote,
		BaseURL: server.URL,
	}); err != nil {
		t.Fatalf("addSavedTargetProfile() error = %v", err)
	}

	root := newRootCmd()
	var stdout strings.Builder
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"--remote", "prod", "hello"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if runRequest.Content != "hello" {
		t.Fatalf("run content = %q, want %q", runRequest.Content, "hello")
	}
	if runRequest.SessionKey == "" || runRequest.SessionKey == "default" {
		t.Fatalf("run session key = %q, want generated non-default session key", runRequest.SessionKey)
	}
	if !strings.Contains(stdout.String(), "named remote ok") {
		t.Fatalf("stdout = %q, want streamed named-remote output", stdout.String())
	}
}

func TestDashboardCommandUsesSavedTarget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := setupTargetTestKeychain(t)
	if err := store.Set(keychain.DefaultService(), managedTargetSecretKey("prod"), "dashboard-secret"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := addSavedTargetProfile(savedTargetProfile{
		Name:     "prod",
		Kind:     targetKindRemote,
		BaseURL:  "https://prod.example.com",
		AuthType: targetAuthTypeBearer,
		AuthRef:  "keychain:" + managedTargetSecretKey("prod"),
	}); err != nil {
		t.Fatalf("addSavedTargetProfile() error = %v", err)
	}

	root := newRootCmd()
	var stdout strings.Builder
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"dashboard", "--remote", "prod"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	text := stdout.String()
	if !strings.Contains(text, "Dashboard:  https://prod.example.com/dashboard/") {
		t.Fatalf("stdout = %q, want remote dashboard URL", text)
	}
	if !strings.Contains(text, "Auth token:") {
		t.Fatalf("stdout = %q, want masked auth token hint", text)
	}
}

func TestFetchSkillCommandsSkipsNonUserInvocableSkills(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/operator/skills" {
			http.NotFound(w, r)
			return
		}
		writeJSONResponse(t, w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"name": "review-pr", "summary": "Review a PR", "user_invocable": true},
				{"name": "internal-audit", "summary": "Background audit", "user_invocable": false},
				{"name": "legacy-skill", "summary": "No explicit flag"},
			},
		})
	}))
	defer server.Close()

	client := &GatewayClient{
		BaseURL: server.URL,
		HTTP:    &http.Client{Timeout: time.Second},
	}

	commands, err := fetchSkillCommands(t.Context(), client)
	if err != nil {
		t.Fatalf("fetchSkillCommands() error = %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("len(commands) = %d, want 2", len(commands))
	}
	if commands[0].Name != "review-pr" || commands[1].Name != "legacy-skill" {
		t.Fatalf("commands = %#v", commands)
	}
}

func TestApplyInteractiveLoggingOverridesSuppressesPrivateLocalInfoLogs(t *testing.T) {
	originalVerbose := flagVerbose
	flagVerbose = false
	t.Cleanup(func() {
		flagVerbose = originalVerbose
	})

	cfg := config.Config{}
	cfg.Logging.Level = "info"
	cfg.Logging.SubsystemLevels = map[string]string{"agent": "info"}
	cfg.Logging.ConsoleCapture = true

	applyInteractiveLoggingOverrides(&cfg, localSessionInteractiveTarget())

	if cfg.Logging.Level != "warn" {
		t.Fatalf("cfg.Logging.Level = %q, want warn", cfg.Logging.Level)
	}
	if cfg.Logging.SubsystemLevels != nil {
		t.Fatalf("cfg.Logging.SubsystemLevels = %#v, want nil", cfg.Logging.SubsystemLevels)
	}
	if cfg.Logging.ConsoleCapture {
		t.Fatal("cfg.Logging.ConsoleCapture = true, want false")
	}
}

func TestApplyInteractiveRuntimeProfileDefaultsPromotesPrivateLocalDesktop(t *testing.T) {
	cfg := config.Config{}

	applyInteractiveRuntimeProfileDefaults(&cfg, localSessionInteractiveTarget())

	if cfg.Runtime.Profile != config.RuntimeProfileTrustedDesktop {
		t.Fatalf("cfg.Runtime.Profile = %q, want %q", cfg.Runtime.Profile, config.RuntimeProfileTrustedDesktop)
	}
}

func TestApplyInteractiveRuntimeProfileDefaultsPreservesExplicitProductionProfile(t *testing.T) {
	cfg := config.Config{
		Runtime: config.RuntimeConfig{
			Profile: config.RuntimeProfileProduction,
		},
	}

	applyInteractiveRuntimeProfileDefaults(&cfg, localSessionInteractiveTarget())

	if cfg.Runtime.Profile != config.RuntimeProfileProduction {
		t.Fatalf("cfg.Runtime.Profile = %q, want %q", cfg.Runtime.Profile, config.RuntimeProfileProduction)
	}
}

func TestBuildMemoryUsageItemsUsesUserFacingLabels(t *testing.T) {
	items, _ := buildMemoryUsageItems([]agent.MemoryEntry{
		{Key: "reply_style", Source: "user"},
		{Key: "deploy_root", ProjectID: "proj-1"},
		{Key: "incident", SessionKey: "conv-1"},
		{Key: "todo", Namespace: "task"},
	}, "conv-1", "proj-1")

	got := map[string]replpkg.MemoryUsageItem{}
	for _, item := range items {
		got[item.Key] = item
	}

	if got["reply_style"].Scope != "saved" || got["reply_style"].Reason != "pinned" {
		t.Fatalf("reply_style = %+v", got["reply_style"])
	}
	if got["deploy_root"].Scope != "project" || got["deploy_root"].Reason != "project context" {
		t.Fatalf("deploy_root = %+v", got["deploy_root"])
	}
	if got["incident"].Scope != "conversation" || got["incident"].Reason != "recent" {
		t.Fatalf("incident = %+v", got["incident"])
	}
	if got["todo"].Scope != "task" || got["todo"].Reason != "task context" {
		t.Fatalf("todo = %+v", got["todo"])
	}
}

func TestCompactInteractiveToolSummaryHidesStructuredPayloads(t *testing.T) {
	if got := compactInteractiveToolSummary(`{"command":"ls -la","stdout":"ok"}`); got != "" {
		t.Fatalf("compactInteractiveToolSummary() = %q, want empty", got)
	}
	if got := compactInteractiveToolSummary("Listed 112 files in the current directory."); got != "Listed 112 files in the current directory." {
		t.Fatalf("compactInteractiveToolSummary() = %q", got)
	}
}

func TestInteractiveToolHintExtractsCommandAndCounts(t *testing.T) {
	shell := eventbus.ToolExecutionResultPayload{
		ToolName: "exec.shell",
		ToolResult: map[string]any{
			"content": `{"command":"ls -la","dir":"/tmp","stdout":"","stderr":"","exit_code":0}`,
		},
	}
	if got := interactiveToolHint(shell); got != "ls -la" {
		t.Fatalf("interactiveToolHint(exec.shell) = %q, want %q", got, "ls -la")
	}

	list := eventbus.ToolExecutionResultPayload{
		ToolName: "fs.list",
		ToolResult: map[string]any{
			"content": `{"path":".","count":112,"entries":[]}`,
		},
	}
	if got := interactiveToolHint(list); got != "112 entries in ." {
		t.Fatalf("interactiveToolHint(fs.list) = %q, want %q", got, "112 entries in .")
	}

	read := eventbus.ToolExecutionResultPayload{
		ToolName: "fs.read",
		ToolResult: map[string]any{
			"content": `{"path":"docs/HopClaw 实施版总方案.md","content":"..."}`,
		},
	}
	if got := interactiveToolHint(read); got != "read docs/HopClaw 实施版总方案.md" {
		t.Fatalf("interactiveToolHint(fs.read) = %q", got)
	}
}

func TestFirstToolSummarySuppressesQuietSuccessTools(t *testing.T) {
	event := eventbus.NewToolExecutedEvent("run-1", "sess-1", eventbus.ToolExecutedPayload{
		Results: []eventbus.ToolExecutionResultPayload{{
			ToolName: "fs.read",
			Status:   "ok",
			ToolResult: map[string]any{
				"content": `{"path":"README.md","content":"..."}`,
			},
		}},
	}, nil)

	if got := firstToolSummary(event); got != "" {
		t.Fatalf("firstToolSummary(fs.read ok) = %q, want empty", got)
	}
}

func TestFirstToolSummaryKeepsQuietToolErrorsVisible(t *testing.T) {
	event := eventbus.NewToolExecutedEvent("run-1", "sess-1", eventbus.ToolExecutedPayload{
		Results: []eventbus.ToolExecutionResultPayload{{
			ToolName: "exec.run",
			Status:   "error",
			Error:    `exec.run "go" failed: exit status 1`,
		}},
	}, nil)

	if got := firstToolSummary(event); got != `exec.run "go" failed: exit status 1` {
		t.Fatalf("firstToolSummary(exec.run error) = %q", got)
	}
}

func TestExternalInteractiveGatewayReadinessProjectsNamedLocalRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"ok":      true,
				"summary": "gateway healthy",
			})
		case "/runtime/runs":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := (&externalInteractiveGateway{
		client: &GatewayClient{BaseURL: server.URL, HTTP: server.Client()},
		target: interactiveTarget{Kind: interactiveTargetLocal, Name: "local-dev", BaseURL: server.URL},
	}).ReadinessSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ReadinessSnapshot() error = %v", err)
	}
	if snapshot == nil {
		t.Fatal("ReadinessSnapshot() = nil")
	}

	var runtimeCategory *replpkg.ReadinessCategory
	for i := range snapshot.Categories {
		if snapshot.Categories[i].ID == "remote_target" {
			runtimeCategory = &snapshot.Categories[i]
			break
		}
	}
	if runtimeCategory == nil {
		t.Fatal("remote_target category not found")
	}
	if runtimeCategory.Label != "Local runtime local-dev" {
		t.Fatalf("runtime category label = %q", runtimeCategory.Label)
	}
	if runtimeCategory.Summary != "gateway local runtime reachable" {
		t.Fatalf("runtime category summary = %q", runtimeCategory.Summary)
	}
}

func TestInteractiveBackendDelegatesGovernanceAutomationAndMemoryExtensions(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/governance/deliveries":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"items": []map[string]any{{
					"id":              "gdel-1",
					"adapter_name":    "audit-hub",
					"status":          "dead_letter",
					"attempts":        3,
					"max_attempts":    3,
					"last_error":      "timeout",
					"next_attempt_at": now.Add(10 * time.Minute).Format(time.RFC3339),
					"can_redrive":     true,
					"record": map[string]any{
						"run_id":     "run-1",
						"summary":    "delivery failed",
						"event_id":   "evt-1",
						"event_type": "governance.delivery.dead_letter",
					},
				}},
				"count": 1,
			})
		case "/runtime/governance/deliveries/gdel-1/redrive":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"count":   1,
				"updated": 1,
				"skipped": 0,
			})
		case automationPath + "/cron/cron-1":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"item": map[string]any{
					"id":       "cron-1",
					"kind":     "cron",
					"name":     "Daily report",
					"enabled":  true,
					"schedule": "0 9 * * 1-5",
					"delivery": map[string]any{
						"channel": "slack",
					},
				},
			})
		case "/runtime/memory":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"key":             "service_url",
						"value":           "https://old.example.com",
						"source":          "project",
						"conflict_with":   "https://new.example.com",
						"conflict_source": "agent",
					},
					{
						"key":                  "deploy_env",
						"value":                "staging",
						"pending_write":        true,
						"pending_write_source": "agent",
						"pending_write_value":  "production",
					},
				},
				"count": 2,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	backend := newExternalInteractiveBackend(&GatewayClient{
		BaseURL: server.URL,
		HTTP:    server.Client(),
	}, interactiveTarget{})

	deliveries, err := backend.ListGovernanceDeliveries(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("ListGovernanceDeliveries() error = %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("len(deliveries) = %d, want 1", len(deliveries))
	}
	if deliveries[0].Summary != "delivery failed" {
		t.Fatalf("delivery summary = %q", deliveries[0].Summary)
	}

	redrive, err := backend.RedriveDelivery(context.Background(), "gdel-1")
	if err != nil {
		t.Fatalf("RedriveDelivery() error = %v", err)
	}
	if redrive == nil || redrive.Redriven != 1 || redrive.Failed != 0 {
		t.Fatalf("redrive result = %#v", redrive)
	}

	automation, err := backend.GetAutomationDetail(context.Background(), "cron", "cron-1")
	if err != nil {
		t.Fatalf("GetAutomationDetail() error = %v", err)
	}
	if automation == nil {
		t.Fatal("GetAutomationDetail() = nil")
	}
	if automation.Status != "needs_input" {
		t.Fatalf("automation status = %q, want needs_input", automation.Status)
	}
	if automation.SetupContract == nil || len(automation.SetupContract.Slots) != 1 {
		t.Fatalf("automation setup contract = %#v, want one missing delivery slot", automation.SetupContract)
	}

	conflicts, err := backend.ListMemoryConflicts(context.Background())
	if err != nil {
		t.Fatalf("ListMemoryConflicts() error = %v", err)
	}
	if len(conflicts) != 1 || conflicts[0].Key != "service_url" {
		t.Fatalf("conflicts = %#v", conflicts)
	}

	pending, err := backend.ListPendingMemoryWrites(context.Background())
	if err != nil {
		t.Fatalf("ListPendingMemoryWrites() error = %v", err)
	}
	if len(pending) != 1 || pending[0].Key != "deploy_env" {
		t.Fatalf("pending writes = %#v", pending)
	}

	if err := backend.ResolveMemoryConflict(context.Background(), "service_url", "keep"); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("ResolveMemoryConflict() error = %v, want unsupported error", err)
	}
}

func writeInteractiveConfig(t *testing.T, baseURL string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "server:\n  address: " + strings.TrimPrefix(baseURL, "http://") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func writeSSEEvent(t *testing.T, w http.ResponseWriter, event any) {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal sse event: %v", err)
	}
	if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
		t.Fatalf("write sse event: %v", err)
	}
}
