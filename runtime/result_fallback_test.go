package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	planpkg "github.com/fulcrus/hopclaw/planner"
	resultmodel "github.com/fulcrus/hopclaw/resultmodel"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

func TestGetRunResultFallsBackToPlanSummaryAfterSessionMessagesDisappear(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()

	session, err := sessions.GetOrCreate(ctx, "result-fallback", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "result-fallback",
		Content:    "打开页面后截图给我，再告诉我页面标题",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-3 * time.Second)
	run.FinishedAt = now
	goal := "打开页面后截图给我，再告诉我页面标题"
	run.Plan = &planpkg.Plan{
		Goal:     goal,
		Strategy: planpkg.StrategySerial,
		Tasks: []planpkg.Task{{
			ID:            "task_1",
			Kind:          planpkg.TaskExecute,
			Goal:          goal,
			Status:        planpkg.TaskCompleted,
			ResultSummary: "我已经打开页面并截图，页面标题是 Example Domain。",
		}},
		FinalTask: "task_1",
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, nil, nil)
	result, err := svc.GetRunResult(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunResult() error = %v", err)
	}
	if got := strings.TrimSpace(result.Output); got != "我已经打开页面并截图，页面标题是 Example Domain。" {
		t.Fatalf("Output = %q", got)
	}
}

func TestGetRunVerificationUsesEventToolOutputsWhenSessionMessagesDisappear(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "verification-fallback", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "verification-fallback",
		Content:    "抓取页面信息，写到 docs/tmp/example-brief.md",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-5 * time.Second)
	run.FinishedAt = now
	goal := "抓取页面信息，写到 docs/tmp/example-brief.md"
	run.TaskContract = &agent.TaskContract{
		Goal:    goal,
		JobType: "report",
		ExpectedDeliverables: []agent.TaskContractDeliverable{
			{Kind: "summary", Required: true},
			{Kind: "browser_evidence", Required: false},
			{Kind: "document", Required: true},
		},
		AcceptanceCriteria: []agent.TaskContractAcceptance{
			{ID: "visible_result", Summary: "Produce a user-visible result or summary, not just internal steps.", Required: true},
			{ID: "deliverables_ready", Summary: "Leave the expected deliverables or evidence for the requested work.", Required: true, DeliverableKinds: []string{"summary", "document"}},
		},
	}
	run.Plan = &planpkg.Plan{
		Goal:     goal,
		Strategy: planpkg.StrategySerial,
		Tasks: []planpkg.Task{{
			ID:            "task_1",
			Kind:          planpkg.TaskExecute,
			Goal:          goal,
			Status:        planpkg.TaskCompleted,
			ResultSummary: "页面信息抓取完成，报告已写入 docs/tmp/example-brief.md。",
		}},
		FinalTask: "task_1",
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if err := bus.Publish(ctx, eventbus.Event{
		Type:      eventbus.EventToolExecuted,
		RunID:     run.ID,
		SessionID: session.ID,
		Attrs: map[string]any{
			"tool_names": []string{"browser.eval", "fs.write"},
			"results": []any{
				map[string]any{
					"tool_name": "browser.eval",
					resultmodel.MetadataKeyToolResult: resultmodel.ToolResult{
						ToolName: "browser.eval",
						Content:  `{"url":"https://httpbin.org/forms/post","title":"Pizza Form","content":"order form"}`,
					}.MarshalMetadata(),
				},
				map[string]any{
					"tool_name": "fs.write",
					resultmodel.MetadataKeyToolResult: resultmodel.ToolResult{
						ToolName: "fs.write",
						Content:  `{"path":"docs/tmp/example-brief.md","bytes_written":1469}`,
					}.MarshalMetadata(),
				},
			},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, bus, nil)
	verification, err := svc.GetRunVerification(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunVerification() error = %v", err)
	}
	if verification.Status != verifyrt.StatusPassed {
		t.Fatalf("verification.Status = %q, want %q", verification.Status, verifyrt.StatusPassed)
	}
	if !hasVerificationCheck(verification.Checks, "task.contract", verifyrt.StatusPassed) {
		t.Fatalf("expected passed task contract check, got %+v", verification.Checks)
	}
	if !hasVerificationCheck(verification.Checks, "browser.result", verifyrt.StatusPassed) {
		t.Fatalf("expected passed browser check, got %+v", verification.Checks)
	}
}

func TestGetRunVerificationMergesSessionAndEventToolOutputs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "verification-merge", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "verification-merge",
		Content:    "抓取页面信息，写到 docs/tmp/example-brief.md",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-5 * time.Second)
	run.FinishedAt = now
	goal := "抓取页面信息，写到 docs/tmp/example-brief.md"
	run.TaskContract = &agent.TaskContract{
		Goal:          goal,
		JobType:       "report",
		TargetSummary: "docs/tmp/example-brief.md",
		ExpectedDeliverables: []agent.TaskContractDeliverable{
			{Kind: "summary", Required: true},
			{Kind: "browser_evidence", Required: false},
			{Kind: "document", Required: true},
		},
		AcceptanceCriteria: []agent.TaskContractAcceptance{
			{ID: "visible_result", Summary: "Produce a user-visible result or summary, not just internal steps.", Required: true},
			{ID: "deliverables_ready", Summary: "Leave the expected deliverables or evidence for the requested work.", Required: true, DeliverableKinds: []string{"summary", "document"}},
		},
	}
	run.Plan = &planpkg.Plan{
		Goal:     goal,
		Strategy: planpkg.StrategySerial,
		Tasks: []planpkg.Task{{
			ID:            "task_1",
			Kind:          planpkg.TaskExecute,
			Goal:          goal,
			Status:        planpkg.TaskCompleted,
			ResultSummary: "页面信息抓取完成，报告已写入 docs/tmp/example-brief.md。",
		}},
		FinalTask: "task_1",
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "browser.snapshot",
			Content:   `{"url":"https://example.com","title":"Example Domain"}`,
			CreatedAt: now.Add(-2 * time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "页面信息抓取完成，报告已写入 docs/tmp/example-brief.md。",
			CreatedAt: now,
			Metadata:  map[string]any{"run_id": run.ID},
		},
	)
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := bus.Publish(ctx, eventbus.Event{
		Type:      eventbus.EventToolExecuted,
		RunID:     run.ID,
		SessionID: session.ID,
		Attrs: map[string]any{
			"tool_names": []string{"browser.snapshot", "fs.write"},
			"results": []any{
				map[string]any{
					"tool_name": "browser.snapshot",
					resultmodel.MetadataKeyToolResult: resultmodel.ToolResult{
						ToolName: "browser.snapshot",
						Content:  `{"url":"https://example.com","title":"Example Domain"}`,
					}.MarshalMetadata(),
				},
				map[string]any{
					"tool_name": "fs.write",
					resultmodel.MetadataKeyToolResult: resultmodel.ToolResult{
						ToolName: "fs.write",
						Content:  `{"path":"docs/tmp/example-brief.md","bytes_written":974}`,
					}.MarshalMetadata(),
				},
			},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, bus, nil)
	verification, err := svc.GetRunVerification(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunVerification() error = %v", err)
	}
	if verification.Status != verifyrt.StatusPassed {
		t.Fatalf("verification.Status = %q, want %q", verification.Status, verifyrt.StatusPassed)
	}
	if !hasVerificationCheck(verification.Checks, "task.contract", verifyrt.StatusPassed) {
		t.Fatalf("expected passed task contract check, got %+v", verification.Checks)
	}
	if !hasVerificationCheck(verification.Checks, "browser.result", verifyrt.StatusPassed) {
		t.Fatalf("expected passed browser check, got %+v", verification.Checks)
	}
}
