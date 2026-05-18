package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	planpkg "github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/policy"
	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
)

func TestExecuteRunAdvancesPlanTasksSerially(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	model := &recordingModelClient{
		responses: []*ModelResponse{
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "Collected the top headlines and grouped them by topic.",
				},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "Here is the final table.",
				},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 4,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:     "Search news and deliver a table.",
			Strategy: planpkg.StrategyMixed,
			Tasks: []planpkg.Task{
				{
					ID:    "research",
					Kind:  planpkg.TaskResearch,
					Title: "Search headlines",
					Goal:  "Search today's headline news and gather candidates.",
				},
				{
					ID:        "deliver",
					Kind:      planpkg.TaskDeliver,
					Title:     "Deliver the table",
					Goal:      "Format the researched news as a final markdown table.",
					DependsOn: []string{"research"},
				},
			},
			FinalTask: "deliver",
		}}).
		WithEventBus(bus)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-plan-executor",
		ExternalEventID: "evt-plan-executor",
		Content:         "search today's headlines and give me a table",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunCompleted {
		t.Fatalf("run.Status = %q", got.Status)
	}
	if got.Plan == nil {
		t.Fatal("expected plan on completed run")
	}
	if got.Plan.ActiveTask != "" {
		t.Fatalf("plan.ActiveTask = %q, want empty", got.Plan.ActiveTask)
	}
	if len(got.Plan.Tasks) != 2 {
		t.Fatalf("len(plan.Tasks) = %d", len(got.Plan.Tasks))
	}
	if got.Plan.Tasks[0].Status != planpkg.TaskCompleted {
		t.Fatalf("task 0 state = %q", got.Plan.Tasks[0].Status)
	}
	if got.Plan.Tasks[1].Status != planpkg.TaskCompleted {
		t.Fatalf("task 1 state = %q", got.Plan.Tasks[1].Status)
	}
	if !strings.Contains(got.Plan.Tasks[0].ResultSummary, "Collected") {
		t.Fatalf("task 0 summary = %q", got.Plan.Tasks[0].ResultSummary)
	}
	if len(model.requests) != 2 {
		t.Fatalf("len(model.requests) = %d, want 2", len(model.requests))
	}
	if !strings.Contains(model.requests[0].SystemPrompt, "Current task: Search headlines") {
		t.Fatalf("first prompt missing current task: %q", model.requests[0].SystemPrompt)
	}
	if !strings.Contains(model.requests[1].SystemPrompt, "Current task: Deliver the table") {
		t.Fatalf("second prompt missing current task: %q", model.requests[1].SystemPrompt)
	}
	if !strings.Contains(model.requests[1].SystemPrompt, "<task_dependency_outcomes>") {
		t.Fatalf("second prompt missing structured dependency outcomes: %q", model.requests[1].SystemPrompt)
	}
	if !strings.Contains(model.requests[1].SystemPrompt, "\"task_id\": \"research\"") {
		t.Fatalf("second prompt missing dependency task id: %q", model.requests[1].SystemPrompt)
	}

	started := 0
	completed := 0
	for _, event := range bus.Snapshot() {
		switch event.Type {
		case eventbus.EventPlanTaskStarted:
			started++
		case eventbus.EventPlanTaskCompleted:
			completed++
		}
	}
	if started != 2 {
		t.Fatalf("plan task started events = %d, want 2", started)
	}
	if completed != 2 {
		t.Fatalf("plan task completed events = %d, want 2", completed)
	}
}

func TestExecuteRunMarksPlanTaskFailed(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
		Retry:         RetryConfig{MaxAttempts: 2, MinDelay: 10 * time.Millisecond, MaxDelay: 50 * time.Millisecond},
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), errorModelClient{err: errors.New("upstream unavailable")}, nil, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:     "Do two things",
			Strategy: planpkg.StrategySerial,
			Tasks: []planpkg.Task{
				{ID: "research", Kind: planpkg.TaskResearch, Goal: "Research the topic"},
				{ID: "deliver", Kind: planpkg.TaskDeliver, Goal: "Deliver the answer", DependsOn: []string{"research"}},
			},
			FinalTask: "deliver",
		}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-plan-fail",
		ExternalEventID: "evt-plan-fail",
		Content:         "do the work",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err == nil {
		t.Fatal("expected ExecuteRun() to fail")
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunFailed {
		t.Fatalf("run.Status = %q", got.Status)
	}
	if got.Plan == nil {
		t.Fatal("expected plan on failed run")
	}
	if got.Plan.Tasks[0].Status != planpkg.TaskFailed {
		t.Fatalf("first task state = %q", got.Plan.Tasks[0].Status)
	}
	if got.Plan.Tasks[1].Status != planpkg.TaskCancelled && got.Plan.Tasks[1].Status != planpkg.TaskSkipped {
		t.Fatalf("second task state = %q, want cancelled or skipped", got.Plan.Tasks[1].Status)
	}
	if !strings.Contains(got.Plan.Tasks[0].Error, "upstream unavailable") {
		t.Fatalf("first task error = %q", got.Plan.Tasks[0].Error)
	}
}

func TestExecuteRunPlanTaskRecoversFromToolExecutionError(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &recordingModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.read",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "The file read failed, so I am returning a partial result with the limitation noted.",
				},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, stubToolExecutor{
		err: errors.New("disk offline"),
	}, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:     "Inspect one file and respond.",
			Strategy: planpkg.StrategySerial,
			Tasks: []planpkg.Task{
				{
					ID:    "inspect",
					Kind:  planpkg.TaskResearch,
					Title: "Inspect file",
					Goal:  "Read the file and report back.",
				},
			},
			FinalTask: "inspect",
		}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-plan-tool-recover",
		ExternalEventID: "evt-plan-tool-recover",
		Content:         "inspect the file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunCompleted {
		t.Fatalf("run.Status = %q", got.Status)
	}
	if got.Plan == nil || len(got.Plan.Tasks) != 1 {
		t.Fatalf("plan = %#v", got.Plan)
	}
	if got.Plan.Tasks[0].Status != planpkg.TaskCompleted {
		t.Fatalf("task status = %q", got.Plan.Tasks[0].Status)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-plan-tool-recover", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	foundFailureResult := false
	for _, msg := range session.Messages {
		if msg.Role != contextengine.RoleTool {
			continue
		}
		result, ok := resultmodel.DecodeToolResultMetadata(msg.Metadata)
		if ok && result.Error != nil && result.Error.Message == "disk offline" {
			foundFailureResult = true
			break
		}
	}
	if !foundFailureResult {
		t.Fatalf("expected recovered tool failure result in session messages: %#v", session.Messages)
	}
}

func TestExecuteRunParallelTasksComplete(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	model := &recordingModelClient{
		responses: []*ModelResponse{
			// Two parallel tasks produce responses concurrently.
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "Gathered data from source A.",
				},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "Gathered data from source B.",
				},
			},
			// Final task depends on both.
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "Here is the combined result.",
				},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 4,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:     "Gather from two sources and combine.",
			Strategy: planpkg.StrategyMixed,
			Tasks: []planpkg.Task{
				{
					ID:    "gather_a",
					Kind:  planpkg.TaskResearch,
					Title: "Gather A",
					Goal:  "Gather data from source A.",
				},
				{
					ID:    "gather_b",
					Kind:  planpkg.TaskResearch,
					Title: "Gather B",
					Goal:  "Gather data from source B.",
				},
				{
					ID:        "combine",
					Kind:      planpkg.TaskDeliver,
					Title:     "Combine results",
					Goal:      "Combine data from A and B into final output.",
					DependsOn: []string{"gather_a", "gather_b"},
				},
			},
			FinalTask: "combine",
		}}).
		WithEventBus(bus)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-parallel",
		ExternalEventID: "evt-parallel",
		Content:         "gather and combine",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", got.Status)
	}
	if got.Plan == nil {
		t.Fatal("expected plan on completed run")
	}
	for i, task := range got.Plan.Tasks {
		if task.Status != planpkg.TaskCompleted {
			t.Fatalf("task %d (%s) state = %q, want completed", i, task.ID, task.Status)
		}
	}

	// Verify events: 3 started, 3 completed.
	started := 0
	completed := 0
	for _, event := range bus.Snapshot() {
		switch event.Type {
		case eventbus.EventPlanTaskStarted:
			started++
		case eventbus.EventPlanTaskCompleted:
			completed++
		}
	}
	if started != 3 {
		t.Fatalf("plan task started events = %d, want 3", started)
	}
	if completed != 3 {
		t.Fatalf("plan task completed events = %d, want 3", completed)
	}

	// Verify messages from parallel tasks were merged back to session.
	session, err := sessions.Get(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("session Get() error = %v", err)
	}
	// Should have: user message + (parallel task A + B messages) + combine task message.
	// Each task produces at least 1 assistant message.
	assistantCount := 0
	for _, msg := range session.Messages {
		if msg.Role == contextengine.RoleAssistant {
			assistantCount++
		}
	}
	if assistantCount < 3 {
		t.Fatalf("assistant messages = %d, want >= 3", assistantCount)
	}
}

func TestExecuteReadyBatchParallelRetriesOnRevisionConflict(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &gatedScriptedModelClient{
		responses: []*ModelResponse{
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "stale parallel result a",
				},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "stale parallel result b",
				},
			},
		},
		blockIndex: 0,
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 4,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-parallel-batch-conflict",
		Content:    "run parallel tasks",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	run.Status = RunRunning
	run.Plan = &planpkg.Plan{
		Goal:     "Run two independent tasks.",
		Strategy: planpkg.StrategyParallel,
		Tasks: []planpkg.Task{
			{ID: "a", Kind: planpkg.TaskResearch, Goal: "A", Status: planpkg.TaskQueued},
			{ID: "b", Kind: planpkg.TaskResearch, Goal: "B", Status: planpkg.TaskQueued},
		},
	}
	planpkg.NormalizeExecution(run.Plan)
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("RunStore.Update() error = %v", err)
	}

	session, unlock, err := sessions.LoadForExecution(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	lease := &sessionLease{session: session, unlock: unlock}
	ready := planpkg.ReadyTasks(run.Plan)
	aggregator := NewTaskResultAggregator()

	outcomeCh := make(chan batchExecutionOutcome, 1)
	errCh := make(chan error, 1)
	go func() {
		outcome, err := component.executeReadyBatch(context.Background(), run, lease, ready, aggregator)
		if err != nil {
			errCh <- err
			return
		}
		outcomeCh <- outcome
	}()

	select {
	case <-model.started:
	case <-time.After(2 * time.Second):
		t.Fatal("parallel batch did not reach model call")
	}

	appendDone := make(chan error, 1)
	go func() {
		appendDone <- sessions.AppendUserMessage(context.Background(), run.SessionID, IncomingMessage{
			Content: "new user context during parallel batch",
		})
	}()

	select {
	case err := <-appendDone:
		if err != nil {
			t.Fatalf("AppendUserMessage() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AppendUserMessage() blocked during parallel batch")
	}

	close(model.release)

	select {
	case err := <-errCh:
		t.Fatalf("executeReadyBatch() error = %v", err)
	case outcome := <-outcomeCh:
		if !outcome.retry {
			t.Fatal("executeReadyBatch() retry = false, want true")
		}
		if len(outcome.results) != 0 {
			t.Fatalf("len(outcome.results) = %d, want 0 on retry", len(outcome.results))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("executeReadyBatch() did not return")
	}

	gotRun, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	for _, task := range gotRun.Plan.Tasks {
		if task.Status != planpkg.TaskQueued {
			t.Fatalf("task %s status = %q, want queued", task.ID, task.Status)
		}
	}
	gotSession, err := sessions.Get(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("sessions.Get() error = %v", err)
	}
	for _, msg := range gotSession.Messages {
		if strings.Contains(msg.Content, "stale parallel result") {
			t.Fatalf("stale parallel batch message merged unexpectedly: %#v", msg)
		}
	}
}

func TestExecuteRunParallelTasksRetryOnRevisionConflict(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &gatedScriptedModelClient{
		responses: []*ModelResponse{
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "stale gather result 1",
				},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "stale gather result 2",
				},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "fresh gather result 1",
				},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "fresh gather result 2",
				},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "final answer after parallel retry",
				},
			},
		},
		blockIndex: 0,
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 4,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:     "Gather from two sources and combine.",
			Strategy: planpkg.StrategyMixed,
			Tasks: []planpkg.Task{
				{
					ID:    "gather_a",
					Kind:  planpkg.TaskResearch,
					Title: "Gather A",
					Goal:  "Gather data from source A.",
				},
				{
					ID:    "gather_b",
					Kind:  planpkg.TaskResearch,
					Title: "Gather B",
					Goal:  "Gather data from source B.",
				},
				{
					ID:        "combine",
					Kind:      planpkg.TaskDeliver,
					Title:     "Combine results",
					Goal:      "Combine data from A and B into final output.",
					DependsOn: []string{"gather_a", "gather_b"},
				},
			},
			FinalTask: "combine",
		}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-parallel-retry",
		Content:    "gather and combine",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ExecuteRun(context.Background(), run)
	}()

	select {
	case <-model.started:
	case <-time.After(2 * time.Second):
		t.Fatal("ExecuteRun did not reach first parallel model call")
	}

	appendDone := make(chan error, 1)
	go func() {
		appendDone <- sessions.AppendUserMessage(context.Background(), run.SessionID, IncomingMessage{
			Content: "use the newly arrived context",
		})
	}()

	select {
	case err := <-appendDone:
		if err != nil {
			t.Fatalf("AppendUserMessage() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AppendUserMessage() blocked during parallel execution")
	}

	close(model.release)

	if err := <-errCh; err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	gotRun, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if gotRun.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", gotRun.Status)
	}
	gotSession, err := sessions.Get(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("sessions.Get() error = %v", err)
	}
	if gotSession.Messages[len(gotSession.Messages)-1].Content != "final answer after parallel retry" {
		t.Fatalf("final assistant message = %#v", gotSession.Messages[len(gotSession.Messages)-1])
	}
	for _, msg := range gotSession.Messages {
		if strings.Contains(msg.Content, "stale gather result") {
			t.Fatalf("stale parallel message committed unexpectedly: %#v", msg)
		}
	}
}

func TestExecuteRunRecoversByReplanningAfterPlanFailure(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &recordingModelClient{
		responses: []*ModelResponse{
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "Recovered with a shorter plan and completed the request.",
				},
			},
		},
	}
	planner := &sequencePlanner{
		plans: []*planpkg.Plan{
			{
				Goal:     "Attempt a brittle plan first.",
				Strategy: planpkg.StrategySerial,
				Tasks: []planpkg.Task{
					{ID: "research", Kind: planpkg.TaskResearch, Goal: "Initial failing task"},
					{ID: "deliver", Kind: planpkg.TaskDeliver, Goal: "Deliver result", DependsOn: []string{"research"}},
				},
				FinalTask: "deliver",
			},
			{
				Goal:     "Fallback to a compact plan.",
				Strategy: planpkg.StrategySerial,
				Tasks: []planpkg.Task{
					{ID: "deliver", Kind: planpkg.TaskDeliver, Goal: "Deliver a concise final answer"},
				},
				FinalTask: "deliver",
			},
		},
	}

	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
		Retry:         RetryConfig{MaxAttempts: 1},
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(planner)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-plan-recover",
		ExternalEventID: "evt-plan-recover",
		Content:         "complete the task end-to-end",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	// Fail the first plan's task, then allow the replanned run to complete.
	component.model = &errorThenModelClient{
		firstErr: errors.New("upstream unavailable"),
		next:     model,
	}

	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", got.Status)
	}
	if got.Plan == nil {
		t.Fatal("expected final plan to be present")
	}
	if got.Plan.Goal != "Fallback to a compact plan." {
		t.Fatalf("plan.Goal = %q", got.Plan.Goal)
	}
	if planner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", planner.calls)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-plan-recover", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	foundDirective := false
	for _, msg := range session.Messages {
		if msg.Role != contextengine.RoleUser {
			continue
		}
		if flag, _ := msg.Metadata["auto_recovery"].(bool); flag && strings.Contains(msg.Content, "previous plan did not complete") {
			foundDirective = true
			break
		}
	}
	if !foundDirective {
		t.Fatal("expected auto recovery directive in session messages")
	}
}

func TestExecuteRunPlanTaskRecoversFromRepeatedToolLoop(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &recordingModelClient{
		responses: []*ModelResponse{
			{ToolCalls: []ToolCall{{ID: "call-1", Name: "fs.read", Input: map[string]any{"path": "README.md"}}}},
			{ToolCalls: []ToolCall{{ID: "call-2", Name: "fs.read", Input: map[string]any{"path": "README.md"}}}},
			{ToolCalls: []ToolCall{{ID: "call-3", Name: "fs.read", Input: map[string]any{"path": "README.md"}}}},
			{ToolCalls: []ToolCall{{ID: "call-4", Name: "fs.read", Input: map[string]any{"path": "README.md"}}}},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "Recovered from the loop and delivered the final answer.",
				},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 6,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, stubToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:    "fs.read",
			Content:     "readme contents",
			ArtifactURI: "artifact://fs.read/readme",
		}},
	}, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:     "Read the readme and answer.",
			Strategy: planpkg.StrategySerial,
			Tasks: []planpkg.Task{
				{
					ID:    "inspect",
					Kind:  planpkg.TaskResearch,
					Title: "Inspect file",
					Goal:  "Read the readme and summarize it.",
				},
			},
			FinalTask: "inspect",
		}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-plan-tool-loop-recover",
		ExternalEventID: "evt-plan-tool-loop-recover",
		Content:         "inspect the readme and summarize it",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunCompleted {
		t.Fatalf("run.Status = %q", got.Status)
	}
	if got.Plan == nil || got.Plan.Tasks[0].Status != planpkg.TaskCompleted {
		t.Fatalf("plan = %#v", got.Plan)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-plan-tool-loop-recover", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	foundDirective := false
	for _, msg := range session.Messages {
		if msg.Role != contextengine.RoleUser {
			continue
		}
		if flag, _ := msg.Metadata["auto_recovery"].(bool); flag && strings.Contains(msg.Content, "Do not repeat the same tool call") {
			foundDirective = true
			break
		}
	}
	if !foundDirective {
		t.Fatal("expected auto recovery directive after repeated tool loop")
	}
}

func TestExecuteRunPlanTaskRecoversFromToolNoProgress(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &recordingModelClient{
		responses: []*ModelResponse{
			{ToolCalls: []ToolCall{{ID: "call-1", Name: "fs.read", Input: map[string]any{"path": "a.txt"}}}},
			{ToolCalls: []ToolCall{{ID: "call-2", Name: "fs.read", Input: map[string]any{"path": "b.txt"}}}},
			{ToolCalls: []ToolCall{{ID: "call-3", Name: "fs.read", Input: map[string]any{"path": "c.txt"}}}},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "The reads all returned the same blocker, so I am returning the best partial answer.",
				},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 6,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, stubToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName: "fs.read",
			Content:  "permission denied",
		}},
	}, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:     "Inspect candidate files and summarize.",
			Strategy: planpkg.StrategySerial,
			Tasks: []planpkg.Task{
				{
					ID:    "inspect",
					Kind:  planpkg.TaskResearch,
					Title: "Inspect files",
					Goal:  "Read candidate files and summarize what is available.",
				},
			},
			FinalTask: "inspect",
		}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-plan-tool-no-progress",
		ExternalEventID: "evt-plan-tool-no-progress",
		Content:         "inspect candidate files and summarize",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunCompleted {
		t.Fatalf("run.Status = %q", got.Status)
	}
	if got.Plan == nil || got.Plan.Tasks[0].Status != planpkg.TaskCompleted {
		t.Fatalf("plan = %#v", got.Plan)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-plan-tool-no-progress", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	foundDirective := false
	for _, msg := range session.Messages {
		if msg.Role != contextengine.RoleUser {
			continue
		}
		if flag, _ := msg.Metadata["auto_recovery"].(bool); flag && strings.Contains(msg.Content, "not producing new evidence") {
			foundDirective = true
			break
		}
	}
	if !foundDirective {
		t.Fatal("expected auto recovery directive after stalled tool progress")
	}
}

func TestExecuteRunPlanTaskFallsBackToBestEffortAnswerAfterToolBudget(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &recordingModelClient{
		responses: []*ModelResponse{
			{ToolCalls: []ToolCall{{ID: "call-1", Name: "fs.read", Input: map[string]any{"path": "bootstrap/bootstrap.go"}}}},
			{ToolCalls: []ToolCall{{ID: "call-2", Name: "fs.read", Input: map[string]any{"path": "bootstrap/bootstrap_services.go"}}}},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "Top risks: bootstrap/bootstrap.go startup wiring is monolithic; bootstrap/bootstrap_services.go mixes setup concerns; bootstrap/managed_helpers.go uses short helper timeouts.",
				},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, stubToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName: "fs.read",
			Content:  "evidence block",
		}},
	}, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:     "Review startup path and summarize risks.",
			Strategy: planpkg.StrategySerial,
			Tasks: []planpkg.Task{
				{
					ID:    "inspect",
					Kind:  planpkg.TaskResearch,
					Title: "Inspect startup path",
					Goal:  "Review runtime startup path and summarize risks.",
				},
			},
			FinalTask: "inspect",
		}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-plan-best-effort",
		ExternalEventID: "evt-plan-best-effort",
		Content:         "review the startup path and identify risks",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", got.Status)
	}
	session, err := sessions.GetOrCreate(context.Background(), "chat-plan-best-effort", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	last := session.Messages[len(session.Messages)-1]
	if !strings.Contains(last.Content, "Top risks:") {
		t.Fatalf("last assistant message = %#v", last)
	}
	if len(model.requests) < 3 {
		t.Fatalf("len(model.requests) = %d, want at least 3", len(model.requests))
	}
	finalReq := model.requests[len(model.requests)-1]
	if len(finalReq.Tools) != 0 {
		t.Fatalf("final request tools = %#v, want no tools", finalReq.Tools)
	}
	if finalReq.RunID != run.ID {
		t.Fatalf("final request run id = %q, want %q", finalReq.RunID, run.ID)
	}
}

func TestExecuteRunParallelFailFastSkipsDependents(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
		Retry:         RetryConfig{MaxAttempts: 2, MinDelay: 10 * time.Millisecond, MaxDelay: 50 * time.Millisecond},
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), errorModelClient{err: errors.New("model error")}, nil, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:          "Parallel tasks with failure.",
			Strategy:      planpkg.StrategyMixed,
			FailurePolicy: planpkg.FailFast,
			Tasks: []planpkg.Task{
				{ID: "a", Kind: planpkg.TaskResearch, Goal: "Task A"},
				{ID: "b", Kind: planpkg.TaskResearch, Goal: "Task B"},
				{ID: "c", Kind: planpkg.TaskDeliver, Goal: "Final", DependsOn: []string{"a", "b"}},
			},
			FinalTask: "c",
		}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-parallel-fail",
		ExternalEventID: "evt-parallel-fail",
		Content:         "do parallel work",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	_ = component.ExecuteRun(context.Background(), run)

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunFailed {
		t.Fatalf("run.Status = %q, want failed", got.Status)
	}
	// Tasks a and b should be failed; task c should be skipped.
	for _, task := range got.Plan.Tasks {
		switch task.ID {
		case "a", "b":
			if task.Status != planpkg.TaskFailed && task.Status != planpkg.TaskCancelled {
				t.Fatalf("task %s state = %q, want failed or cancelled", task.ID, task.Status)
			}
		case "c":
			if task.Status != planpkg.TaskSkipped {
				t.Fatalf("task c state = %q, want skipped", task.Status)
			}
		}
	}
}

func TestTaskResultAggregatorSnapshot(t *testing.T) {
	t.Parallel()

	plan := &planpkg.Plan{
		Goal:     "Test plan",
		Strategy: planpkg.StrategyMixed,
		Tasks: []planpkg.Task{
			{ID: "a", Kind: planpkg.TaskResearch, Goal: "A", Status: planpkg.TaskCompleted, ResultSummary: "done"},
			{ID: "b", Kind: planpkg.TaskResearch, Goal: "B", Status: planpkg.TaskRunning},
			{ID: "c", Kind: planpkg.TaskDeliver, Goal: "C", Status: planpkg.TaskSkipped, Error: "dep failed"},
		},
		FinalTask: "c",
	}
	agg := NewTaskResultAggregator()
	agg.CompleteTask(TaskExecutionResult{TaskID: "a", Status: planpkg.TaskCompleted, Summary: "done"})
	agg.StartTask("b")

	snap := agg.Snapshot(plan)
	if snap.Total != 3 {
		t.Fatalf("snap.Total = %d, want 3", snap.Total)
	}
	if snap.Completed != 1 {
		t.Fatalf("snap.Completed = %d, want 1", snap.Completed)
	}
	if snap.SkippedCount != 1 {
		t.Fatalf("snap.SkippedCount = %d, want 1", snap.SkippedCount)
	}
	if len(snap.RunningTasks) != 1 {
		t.Fatalf("len(snap.RunningTasks) = %d, want 1", len(snap.RunningTasks))
	}
	if snap.RunningTasks[0].ID != "b" {
		t.Fatalf("running task ID = %q, want b", snap.RunningTasks[0].ID)
	}
}

type staticPlanner struct {
	plan *planpkg.Plan
}

func (s staticPlanner) Plan(context.Context, PlanningRequest) (*planpkg.Plan, error) {
	return clonePlan(s.plan), nil
}

type sequencePlanner struct {
	plans []*planpkg.Plan
	calls int
}

func (s *sequencePlanner) Plan(context.Context, PlanningRequest) (*planpkg.Plan, error) {
	if s.calls >= len(s.plans) {
		return clonePlan(s.plans[len(s.plans)-1]), nil
	}
	plan := clonePlan(s.plans[s.calls])
	s.calls++
	return plan, nil
}

type recordingModelClient struct {
	mu        sync.Mutex
	responses []*ModelResponse
	requests  []ChatRequest
}

func (r *recordingModelClient) Chat(_ context.Context, req ChatRequest) (*ModelResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	if len(r.responses) == 0 {
		return &ModelResponse{}, nil
	}
	resp := r.responses[0]
	r.responses = r.responses[1:]
	return resp, nil
}

// TestPlanTaskApprovalStopsLoop verifies that when a serial plan task
// triggers approval, executeLoop returns instead of re-executing the
// running task. After approval resolution and ResumeRun, the task and
// plan complete successfully.
func TestPlanTaskApprovalStopsLoop(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	approvalStore := approval.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()
	model := &recordingModelClient{
		responses: []*ModelResponse{
			// First call: model decides to install a skill.
			{
				ToolCalls: []ToolCall{{
					ID:    "call-1",
					Name:  "skill.install",
					Input: map[string]any{"name": "web_search"},
				}},
			},
			// Second call (after resume): model produces the answer.
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "Here are today's hot events.",
				},
			},
		},
	}
	bound := skill.BoundSkill{
		Package: &skill.SkillPackage{
			Prompt: skill.PromptSkill{Name: "installer"},
			Trust:  skill.TrustCommunity,
			ToolManifests: []skill.ToolManifest{{
				Name:            "skill.install",
				SideEffectClass: "local_write",
			}},
		},
		Eligibility: skill.EligibilityResult{Eligible: true},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 4,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model,
		stubToolExecutor{
			results: []contextengine.ToolResult{{
				ToolName:   "skill.install",
				ToolCallID: "call-1",
				Content:    "installed web_search",
			}},
		}, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:     "Search hot events.",
			Strategy: planpkg.StrategySerial,
			Tasks: []planpkg.Task{
				{
					ID:    "search",
					Kind:  planpkg.TaskResearch,
					Title: "Search and format hot events",
					Goal:  "Search today's hot events and format as a table.",
				},
			},
			FinalTask: "search",
		}}).
		WithPolicy(policy.NewDefaultEngine(policy.Config{
			RequireApprovalForWrite:  true,
			RequireApprovalCommunity: true,
		})).
		WithApprovals(approvalStore).
		WithEventBus(bus)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-plan-approval",
		ExternalEventID: "evt-plan-approval",
		Content:         "搜索今天的热点事件，整理成表格",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	// Set up skill snapshot so policy evaluation works.
	session, unlock, err := sessions.LoadForExecution(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	session.SkillSnapshot = skill.SessionSkillSnapshot{
		Fingerprint: "skills-1",
		Skills:      map[string]skill.BoundSkill{"installer": bound},
		Ordered:     []skill.BoundSkill{bound},
	}
	if err := sessions.Save(context.Background(), session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	// ExecuteRun should return without error (paused for approval).
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run, err = runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if run.Status != RunWaitingApproval {
		t.Fatalf("run.Status = %q, want waiting_approval", run.Status)
	}
	if run.Plan == nil {
		t.Fatal("expected plan on run")
	}
	if run.Plan.Tasks[0].Status != planpkg.TaskRunning {
		t.Fatalf("task state = %q, want running", run.Plan.Tasks[0].Status)
	}

	// Resolve approval.
	ticket, err := approvalStore.GetByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if _, err := component.ResolveApproval(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	// Resume the run.
	if err := component.ResumeRun(context.Background(), run.ID); err != nil {
		t.Fatalf("ResumeRun() error = %v", err)
	}
	run, err = runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", run.Status)
	}
	if run.Plan.Tasks[0].Status != planpkg.TaskCompleted {
		t.Fatalf("task state = %q, want completed", run.Plan.Tasks[0].Status)
	}
	if !strings.Contains(run.Plan.Tasks[0].ResultSummary, "hot events") {
		t.Fatalf("task summary = %q", run.Plan.Tasks[0].ResultSummary)
	}
}

func TestParallelPlanTaskFallsBackToSerialForApproval(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	approvalStore := approval.NewInMemoryStore()
	model := &taskAwareModelClient{}
	tool := skill.BoundTool{
		Manifest: skill.ToolManifest{
			Name:             "fs.write",
			Description:      "Write a file",
			SideEffectClass:  "local_write",
			RequiresApproval: true,
		},
		Eligibility: skill.EligibilityResult{Eligible: true},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 4,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, stubRuntimeToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.write",
			ToolCallID: "call-write",
			Content:    "wrote output.md",
		}},
		definitions: []ToolDefinition{{
			Name:             "fs.write",
			SideEffectClass:  "local_write",
			RequiresApproval: true,
		}},
		boundTools: map[string]skill.BoundTool{
			"fs.write": tool,
		},
	}, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:     "Do two independent tasks.",
			Strategy: planpkg.StrategyParallel,
			Tasks: []planpkg.Task{
				{
					ID:    "needs-approval",
					Kind:  planpkg.TaskWrite,
					Title: "Needs approval",
					Goal:  "Write the final result into output.md.",
				},
				{
					ID:    "safe",
					Kind:  planpkg.TaskResearch,
					Title: "Safe task",
					Goal:  "Summarize the current progress.",
				},
			},
			FinalTask: "safe",
		}}).
		WithPolicy(policy.NewDefaultEngine(policy.Config{
			RequireApprovalForWrite: true,
		})).
		WithApprovals(approvalStore)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-parallel-approval",
		ExternalEventID: "evt-parallel-approval",
		Content:         "run the parallel tasks",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	run, err = runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if run.Status != RunWaitingApproval {
		t.Fatalf("run.Status = %q, want waiting_approval", run.Status)
	}
	if run.Plan == nil {
		t.Fatal("expected plan on run")
	}
	if run.Plan.Tasks[0].Status != planpkg.TaskRunning {
		t.Fatalf("approval task state = %q, want running", run.Plan.Tasks[0].Status)
	}
	ticket, err := approvalStore.GetByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if ticket.Status != approval.StatusPending {
		t.Fatalf("ticket.Status = %q, want pending", ticket.Status)
	}

	if model.count("Needs approval") != 2 {
		t.Fatalf("approval task model call count = %d, want 2", model.count("Needs approval"))
	}
}

func TestPlannedSerialTaskRetriesOnRevisionConflictAfterToolPhase(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.read",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "task finished with latest context",
				},
			},
		},
	}
	tools := &blockingToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.read",
			ToolCallID: "call-1",
			Content:    "stale task result",
		}},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 4,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:     "Read and summarize file",
			Strategy: planpkg.StrategySerial,
			Tasks: []planpkg.Task{{
				ID:    "read",
				Kind:  planpkg.TaskResearch,
				Title: "Read file",
				Goal:  "Read the file and summarize it",
			}},
			FinalTask: "read",
		}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-plan-conflict",
		Content:    "read file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ExecuteRun(context.Background(), run)
	}()

	<-tools.started
	if err := sessions.AppendUserMessage(context.Background(), run.SessionID, IncomingMessage{
		Content: "use this new detail too",
	}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}
	close(tools.release)

	if err := <-errCh; err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	run, err = runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", run.Status)
	}
	if run.Plan == nil || run.Plan.Tasks[0].Status != planpkg.TaskCompleted {
		t.Fatalf("task status = %#v", run.Plan)
	}

	session, err := sessions.Get(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("Get(session) error = %v", err)
	}
	for _, msg := range session.Messages {
		if msg.Role == contextengine.RoleTool {
			t.Fatalf("unexpected stale tool result committed: %#v", msg)
		}
	}
	if last := session.Messages[len(session.Messages)-1].Content; last != "task finished with latest context" {
		t.Fatalf("final message = %q", last)
	}
}

func TestPlannedSerialTaskCancelDuringToolExecutionDropsResult(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.read",
				}},
			},
		},
	}
	tools := &blockingToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.read",
			ToolCallID: "call-1",
			Content:    "should never be committed",
		}},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil).
		WithPlanner(staticPlanner{plan: &planpkg.Plan{
			Goal:     "Read a file",
			Strategy: planpkg.StrategySerial,
			Tasks: []planpkg.Task{{
				ID:    "read",
				Kind:  planpkg.TaskResearch,
				Title: "Read file",
				Goal:  "Read the file",
			}},
			FinalTask: "read",
		}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-plan-cancel",
		Content:    "read file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ExecuteRun(context.Background(), run)
	}()

	<-tools.started
	if _, err := component.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	close(tools.release)

	if err := <-errCh; err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	run, err = runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if run.Status != RunCancelled {
		t.Fatalf("run.Status = %q, want cancelled", run.Status)
	}

	session, err := sessions.Get(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("Get(session) error = %v", err)
	}
	for _, msg := range session.Messages {
		if msg.Role == contextengine.RoleTool {
			t.Fatalf("unexpected tool result after cancel: %#v", msg)
		}
	}
}

type errorModelClient struct {
	err error
}

func (e errorModelClient) Chat(context.Context, ChatRequest) (*ModelResponse, error) {
	return nil, e.err
}

type errorThenModelClient struct {
	firstErr error
	next     ModelClient
	done     bool
}

func (e *errorThenModelClient) Chat(ctx context.Context, req ChatRequest) (*ModelResponse, error) {
	if !e.done {
		e.done = true
		return nil, e.firstErr
	}
	return e.next.Chat(ctx, req)
}

type taskAwareModelClient struct {
	mu     sync.Mutex
	counts map[string]int
}

func (m *taskAwareModelClient) Chat(_ context.Context, req ChatRequest) (*ModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counts == nil {
		m.counts = make(map[string]int)
	}

	switch {
	case strings.Contains(req.SystemPrompt, "Current task: Needs approval"):
		m.counts["Needs approval"]++
		return &ModelResponse{
			ToolCalls: []ToolCall{{
				ID:    "call-write",
				Name:  "fs.write",
				Input: map[string]any{"path": "output.md", "content": "done"},
			}},
		}, nil
	case strings.Contains(req.SystemPrompt, "Current task: Safe task"):
		m.counts["Safe task"]++
		return &ModelResponse{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "Safe task finished.",
			},
		}, nil
	default:
		return &ModelResponse{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "final answer",
			},
		}, nil
	}
}

func (m *taskAwareModelClient) count(task string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[task]
}
