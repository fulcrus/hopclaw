package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
)

type staticWatchWorkflow struct {
	result       *WatchWorkflowResult
	err          error
	req          WatchWorkflowRequest
	cancelResult *WatchWorkflowCancelResult
	cancelErr    error
	cancelReq    WatchWorkflowCancelRequest
}

func (w *staticWatchWorkflow) Create(_ context.Context, req WatchWorkflowRequest) (*WatchWorkflowResult, error) {
	w.req = req
	if w.err != nil {
		return nil, w.err
	}
	return w.result, nil
}

func (w *staticWatchWorkflow) Cancel(_ context.Context, req WatchWorkflowCancelRequest) (*WatchWorkflowCancelResult, error) {
	w.cancelReq = req
	if w.cancelErr != nil {
		return nil, w.cancelErr
	}
	return w.cancelResult, nil
}

func TestExecuteRunWatchModeCreatesWatchWorkflow(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: `{"supported":true,"name":"NVIDIA monitor","source_url":"https://example.com/nvda","interval":"1h","summary":"已为你创建监控：每小时检查一次这个页面。","confidence":0.96}`,
			},
		}},
	}
	workflow := &staticWatchWorkflow{
		result: &WatchWorkflowResult{
			WatchID:   "watch-1",
			Name:      "NVIDIA monitor",
			SourceURL: "https://example.com/nvda",
			Interval:  "1h",
			Summary:   "已为你创建监控：每小时检查一次这个页面。",
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: ExecutionModeWatch}}).
		WithWatchWorkflow(workflow)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-watch-mode",
		ExternalEventID: "evt-watch-mode",
		Content:         "请持续监控这个页面的变化：https://example.com/nvda",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", run.Status)
	}
	if run.Plan != nil {
		t.Fatalf("run.Plan = %#v, want nil for watch workflow", run.Plan)
	}
	if workflow.req.SourceURL != "https://example.com/nvda" {
		t.Fatalf("workflow.req.SourceURL = %q", workflow.req.SourceURL)
	}
	session, err := sessions.GetByKey(context.Background(), "chat-watch-mode")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if len(session.Messages) < 2 || session.Messages[len(session.Messages)-1].Content != "已为你创建监控：每小时检查一次这个页面。" {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
}

func TestExecuteRunWatchModeCreatesFileWatchWorkflow(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: `{"supported":true,"name":"Report file monitor","source_kind":"file","source_path":"/tmp/report.csv","interval":"30m","summary":"已为你创建文件监控。","confidence":0.95}`,
			},
		}},
	}
	workflow := &staticWatchWorkflow{
		result: &WatchWorkflowResult{
			WatchID:    "watch-file",
			Name:       "Report file monitor",
			SourceKind: "file",
			SourcePath: "/tmp/report.csv",
			Interval:   "30m",
			Summary:    "已为你创建文件监控。",
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: ExecutionModeWatch}}).
		WithWatchWorkflow(workflow)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-watch-file-mode",
		ExternalEventID: "evt-watch-file-mode",
		Content:         "持续监控 /tmp/report.csv 的变化",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if workflow.req.SourceKind != "file" {
		t.Fatalf("workflow.req.SourceKind = %q", workflow.req.SourceKind)
	}
	if workflow.req.SourcePath != "/tmp/report.csv" {
		t.Fatalf("workflow.req.SourcePath = %q", workflow.req.SourcePath)
	}
}

func TestExecuteRunWatchModeNeedsConcreteTarget(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: `{"supported":false,"need_confirmation":true,"summary":"要开始持续监控，我还需要一个明确的网址。","question":"请把你要监控的网页链接发给我。","confidence":0.91}`,
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
		SessionKey:      "chat-watch-mode-gap",
		ExternalEventID: "evt-watch-mode-gap",
		Content:         "帮我持续监控页面变化",
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
	if run.Preflight == nil || run.Preflight.Question != "请把你要监控的网页链接发给我。" {
		t.Fatalf("run.Preflight = %#v", run.Preflight)
	}
}

func TestExecuteRunWatchModeCancelsLatestWatch(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: `{"action":"cancel","supported":true,"summary":"已取消监控任务 ` + "`watch-2`" + `。","confidence":0.95}`,
			},
		}},
	}
	workflow := &staticWatchWorkflow{
		cancelResult: &WatchWorkflowCancelResult{
			RemovedWatchIDs: []string{"watch-2"},
			Summary:         "已取消监控任务 `watch-2`。",
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: ExecutionModeWatch}}).
		WithWatchWorkflow(workflow)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-watch-cancel",
		ExternalEventID: "evt-watch-cancel",
		Content:         "取消刚才那个监控提醒",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", run.Status)
	}
	if workflow.cancelReq.SessionKey != "chat-watch-cancel" {
		t.Fatalf("workflow.cancelReq.SessionKey = %q", workflow.cancelReq.SessionKey)
	}
	if workflow.cancelReq.RemoveAll {
		t.Fatalf("workflow.cancelReq.RemoveAll = true, want false")
	}
	if workflow.req.SourceURL != "" {
		t.Fatalf("workflow.Create should not run, got req = %#v", workflow.req)
	}
	session, err := sessions.GetByKey(context.Background(), "chat-watch-cancel")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if got := session.Messages[len(session.Messages)-1].Content; got != "已取消监控任务 `watch-2`。" {
		t.Fatalf("final assistant message = %q", got)
	}
}

func TestExecuteRunWatchModePassesStructuredCancelTargetAndScope(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: `{"action":"cancel","supported":true,"target_ref":"example.com","remove_all":true,"summary":"已取消 example.com 相关监控。","confidence":0.95}`,
			},
		}},
	}
	workflow := &staticWatchWorkflow{
		cancelResult: &WatchWorkflowCancelResult{
			RemovedWatchIDs: []string{"watch-1", "watch-2"},
			Summary:         "已取消 example.com 相关监控。",
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: ExecutionModeWatch}}).
		WithWatchWorkflow(workflow)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-watch-cancel-all",
		ExternalEventID: "evt-watch-cancel-all",
		Content:         "停掉所有和 example.com 相关的监控",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if workflow.cancelReq.TargetRef != "example.com" {
		t.Fatalf("workflow.cancelReq.TargetRef = %q, want %q", workflow.cancelReq.TargetRef, "example.com")
	}
	if !workflow.cancelReq.RemoveAll {
		t.Fatalf("workflow.cancelReq.RemoveAll = false, want true")
	}
}

type inspectWatchContextModelClient struct {
	minRemaining time.Duration
	response     *ModelResponse
}

func (m *inspectWatchContextModelClient) Chat(ctx context.Context, req ChatRequest) (*ModelResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, errors.New("missing deadline")
	}
	if remaining := time.Until(deadline); remaining < m.minRemaining {
		return nil, errors.New("watch intake deadline too short")
	}
	return m.response, nil
}

func TestClassifyWatchRequestUsesDetachedTimeoutContext(t *testing.T) {
	t.Parallel()

	component := &AgentComponent{
		model: &inspectWatchContextModelClient{
			minRemaining: 15 * time.Second,
			response: &ModelResponse{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: `{"action":"create","supported":true,"name":"Example monitor","source_url":"https://example.com","interval":"1h","summary":"ok","confidence":0.95}`,
				},
			},
		},
	}
	run := &Run{Model: "test-model"}
	session := &Session{
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:    contextengine.RoleUser,
				Content: "监控 https://example.com 每小时检查一次",
			}},
		},
	}
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	decision, err := component.classifyWatchRequest(parent, run, session)
	if err != nil {
		t.Fatalf("classifyWatchRequest() error = %v", err)
	}
	if !decision.Supported {
		t.Fatalf("decision = %#v, want supported", decision)
	}
	if decision.SourceURL != "https://example.com" {
		t.Fatalf("decision.SourceURL = %q", decision.SourceURL)
	}
}
