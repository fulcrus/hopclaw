package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

func TestGetRunVerificationSpreadsheetPassesWithToolEvidence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "sheet-key", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "sheet-key",
		Content:    "update the spreadsheet",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-5 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "spreadsheet.write_range",
			Content:   `{"path":"sheet.csv","range":"A1:B2","created":false}`,
			CreatedAt: now.Add(-2 * time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "Spreadsheet updated successfully.",
			CreatedAt: now,
			Metadata:  map[string]any{meta.KeyRunID: run.ID},
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
			"tool_names": []string{"spreadsheet.write_range"},
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
	if !hasVerificationCheck(verification.Checks, "spreadsheet.result", verifyrt.StatusPassed) {
		t.Fatalf("expected passed spreadsheet check, got %+v", verification.Checks)
	}
}

func TestGetRunVerificationWatchWarnsWithoutFinalPayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()

	session, err := sessions.GetOrCreate(ctx, "watch:market-open", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "watch:market-open",
		Content:    "observe market",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-3 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, nil, nil)
	verification, err := svc.GetRunVerification(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunVerification() error = %v", err)
	}
	if verification.Status != verifyrt.StatusWarning {
		t.Fatalf("verification.Status = %q, want %q", verification.Status, verifyrt.StatusWarning)
	}
	if verification.RequiredWarnings != 1 {
		t.Fatalf("verification.RequiredWarnings = %d, want 1", verification.RequiredWarnings)
	}
	if verification.AdvisoryWarnings != 1 {
		t.Fatalf("verification.AdvisoryWarnings = %d, want 1", verification.AdvisoryWarnings)
	}
	if verification.Summary != "verification finished with 1 required warning and 1 advisory warning" {
		t.Fatalf("verification.Summary = %q", verification.Summary)
	}
	if !hasVerificationCheck(verification.Checks, "watch.notification", verifyrt.StatusWarning) {
		t.Fatalf("expected warning watch check, got %+v", verification.Checks)
	}
}

func TestGetRunVerificationEmailFailureUsesToolOutput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "mail-key", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "mail-key",
		Content:    "send the email",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-4 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleTool,
		Name:      "email.send",
		Content:   `{"success":false,"error":"smtp refused recipient"}`,
		CreatedAt: now.Add(-2 * time.Second),
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := bus.Publish(ctx, eventbus.Event{
		Type:      eventbus.EventToolExecuted,
		RunID:     run.ID,
		SessionID: session.ID,
		Attrs: map[string]any{
			"tool_names": []string{"email.send"},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, bus, artifact.NewInMemoryStore())
	verification, err := svc.GetRunVerification(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunVerification() error = %v", err)
	}
	if verification.Status != verifyrt.StatusWarning {
		t.Fatalf("verification.Status = %q, want %q", verification.Status, verifyrt.StatusWarning)
	}
	if verification.RequiredWarnings != 1 {
		t.Fatalf("verification.RequiredWarnings = %d, want 1", verification.RequiredWarnings)
	}
	if verification.Summary != "verification finished with 1 required warning" {
		t.Fatalf("verification.Summary = %q", verification.Summary)
	}
	if !hasVerificationCheck(verification.Checks, "email.result", verifyrt.StatusFailed) {
		t.Fatalf("expected failed email check, got %+v", verification.Checks)
	}
}

func TestGetRunVerificationDoesNotLeakToolOutputsAcrossRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "mail-shared", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	runOne, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "mail-shared",
		Content:    "send email one",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create(runOne) error = %v", err)
	}
	runTwo, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "mail-shared",
		Content:    "send email two",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create(runTwo) error = %v", err)
	}
	now := time.Now().UTC()
	runOne.Status = agent.RunCompleted
	runOne.StartedAt = now.Add(-10 * time.Second)
	runOne.FinishedAt = now.Add(-8 * time.Second)
	if err := runs.Update(ctx, runOne); err != nil {
		t.Fatalf("Update(runOne) error = %v", err)
	}
	runTwo.Status = agent.RunCompleted
	runTwo.StartedAt = now.Add(-6 * time.Second)
	runTwo.FinishedAt = now
	if err := runs.Update(ctx, runTwo); err != nil {
		t.Fatalf("Update(runTwo) error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "calling email.send",
			CreatedAt: now.Add(-9 * time.Second),
			Metadata:  map[string]any{meta.KeyRunID: runOne.ID},
			ToolCalls: []contextengine.ToolCallRef{{ID: "call-old", Name: "email.send"}},
		},
		contextengine.Message{
			Role:       contextengine.RoleTool,
			Name:       "email.send",
			ToolCallID: "call-old",
			Content:    `{"success":false,"error":"old failure should not leak"}`,
			CreatedAt:  now.Add(-9 * time.Second),
			Metadata: map[string]any{
				meta.KeyRunID: runOne.ID,
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "calling email.send",
			CreatedAt: now.Add(-2 * time.Second),
			Metadata:  map[string]any{meta.KeyRunID: runTwo.ID},
			ToolCalls: []contextengine.ToolCallRef{{ID: "call-new", Name: "email.send"}},
		},
		contextengine.Message{
			Role:       contextengine.RoleTool,
			Name:       "email.send",
			ToolCallID: "call-new",
			Content:    `{"success":true,"message_id":"msg-2"}`,
			CreatedAt:  now.Add(-2 * time.Second),
			Metadata: map[string]any{
				meta.KeyRunID: runTwo.ID,
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "sent second email",
			CreatedAt: now.Add(-time.Second),
			Metadata:  map[string]any{meta.KeyRunID: runTwo.ID},
		},
	)
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := bus.Publish(ctx, eventbus.Event{
		Type:      eventbus.EventToolExecuted,
		RunID:     runTwo.ID,
		SessionID: session.ID,
		Attrs: map[string]any{
			"tool_names": []string{"email.send"},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, bus, nil)
	verification, err := svc.GetRunVerification(ctx, runTwo.ID)
	if err != nil {
		t.Fatalf("GetRunVerification() error = %v", err)
	}
	if verification.Status != verifyrt.StatusPassed {
		t.Fatalf("verification.Status = %q, want %q", verification.Status, verifyrt.StatusPassed)
	}
	if hasVerificationCheck(verification.Checks, "email.result", verifyrt.StatusFailed) {
		t.Fatalf("verification leaked failed email output from another run: %+v", verification.Checks)
	}
}

func TestGetRunVerificationBrowserPassesWithScreenshotEvidence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	artifacts := artifact.NewInMemoryStore()

	session, err := sessions.GetOrCreate(ctx, "browser-key", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "browser-key",
		Content:    "打开页面并截图",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-4 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "browser.navigate",
			Content:   `{"url":"https://example.com","title":"Example Domain"}`,
			CreatedAt: now.Add(-2 * time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "browser.screenshot",
			Content:   `{"artifact_uri":"artifact://local/browser-shot","url":"https://example.com"}`,
			CreatedAt: now.Add(-time.Second),
		},
	)
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	blob, err := artifacts.Put(ctx, artifact.PutRequest{
		Kind:        "tool_output",
		ContentType: "image/png",
		Body:        []byte("png"),
		Metadata: map[string]any{
			meta.KeyRunID:    run.ID,
			meta.KeyToolName: "browser.screenshot",
		},
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := bus.Publish(ctx, eventbus.Event{
		Type:      eventbus.EventToolExecuted,
		RunID:     run.ID,
		SessionID: session.ID,
		Attrs: map[string]any{
			"tool_names":    []string{"browser.navigate", "browser.screenshot"},
			"artifact_uris": []string{blob.URI},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, bus, artifacts)
	verification, err := svc.GetRunVerification(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunVerification() error = %v", err)
	}
	if verification.Status != verifyrt.StatusPassed {
		t.Fatalf("verification.Status = %q, want %q", verification.Status, verifyrt.StatusPassed)
	}
	if !hasVerificationCheck(verification.Checks, "browser.result", verifyrt.StatusPassed) {
		t.Fatalf("expected passed browser check, got %+v", verification.Checks)
	}
}

func TestGetRunVerificationContractWarnsWhenRequiredDeliverableMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()

	session, err := sessions.GetOrCreate(ctx, "contract-sheet", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "contract-sheet",
		Content:    "整理销售数据并产出表格",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-3 * time.Second)
	run.FinishedAt = now
	run.TaskContract = &agent.TaskContract{
		Goal:    "整理销售数据并产出表格",
		JobType: "report",
		ExpectedDeliverables: []agent.TaskContractDeliverable{{
			Kind:     "spreadsheet",
			Required: true,
		}},
		AcceptanceCriteria: []agent.TaskContractAcceptance{{
			ID:       "deliverables_ready",
			Summary:  "Leave the expected spreadsheet deliverable.",
			Required: true,
			DeliverableKinds: []string{
				"spreadsheet",
			},
		}},
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleAssistant,
		Content:   "我已经整理了主要数据点。",
		CreatedAt: now,
		Metadata:  map[string]any{meta.KeyRunID: run.ID},
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, nil, nil)
	verification, err := svc.GetRunVerification(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunVerification() error = %v", err)
	}
	if verification.Status != verifyrt.StatusWarning {
		t.Fatalf("verification.Status = %q, want %q", verification.Status, verifyrt.StatusWarning)
	}
	if !hasVerificationCheck(verification.Checks, "task.contract", verifyrt.StatusWarning) {
		t.Fatalf("expected warning task contract check, got %+v", verification.Checks)
	}
}

func TestGetRunVerificationBrowserPassesWithOpenURLOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "browser-open-key", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "browser-open-key",
		Content:    "open example.com and summarize it",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-3 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "browser.open",
			Content:   `{"session_id":"sess-1","url":"https://example.com"}`,
			CreatedAt: now.Add(-2 * time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "browser.close",
			Content:   `{"ok":true}`,
			CreatedAt: now.Add(-time.Second),
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
			"tool_names": []string{"browser.open", "browser.close"},
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
	if !hasVerificationCheck(verification.Checks, "browser.result", verifyrt.StatusPassed) {
		t.Fatalf("expected passed browser check, got %+v", verification.Checks)
	}
}

func TestGetRunVerificationBrowserPassesWithOpenURLOnlyWithoutEventToolNames(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()

	session, err := sessions.GetOrCreate(ctx, "browser-open-persisted", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "browser-open-persisted",
		Content:    "open example.com and summarize it",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-3 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "browser.open",
			Content:   `{"session_id":"sess-1","url":"https://example.com","title":"Example Domain"}`,
			CreatedAt: now.Add(-2 * time.Second),
			Metadata:  map[string]any{meta.KeyRunID: run.ID},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "页面已打开，标题是 Example Domain。",
			CreatedAt: now,
			Metadata:  map[string]any{meta.KeyRunID: run.ID},
		},
	)
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, nil, nil)
	verification, err := svc.GetRunVerification(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunVerification() error = %v", err)
	}
	if verification.Status != verifyrt.StatusPassed {
		t.Fatalf("verification.Status = %q, want %q", verification.Status, verifyrt.StatusPassed)
	}
	if !hasVerificationCheck(verification.Checks, "browser.result", verifyrt.StatusPassed) {
		t.Fatalf("expected passed browser check, got %+v", verification.Checks)
	}
}

func TestGetRunVerificationBrowserExternalEffectFailsWithoutSubmitEvidence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "browser-submit-missing", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "browser-submit-missing",
		Content:    "打开表单，填写后提交",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-3 * time.Second)
	run.FinishedAt = now
	run.TaskContract = &agent.TaskContract{
		Goal:                   "打开表单，填写后提交",
		JobType:                "general",
		RequiresExternalEffect: true,
		ExpectedDeliverables: []agent.TaskContractDeliverable{{
			Kind:     "browser_evidence",
			Required: false,
		}},
		AcceptanceCriteria: []agent.TaskContractAcceptance{{
			ID:       "external_effect_verified",
			Summary:  "Do not report success without submit evidence.",
			Required: true,
		}},
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "browser.open",
			Content:   `{"session_id":"sess-1","url":"https://httpbin.org/forms/post","title":"forms"}`,
			CreatedAt: now.Add(-2 * time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "browser.type",
			Content:   `{"ok":true}`,
			CreatedAt: now.Add(-time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "browser.click",
			Content:   `{"ok":true}`,
			CreatedAt: now,
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
			"tool_names": []string{"browser.open", "browser.type", "browser.click"},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, bus, nil)
	verification, err := svc.GetRunVerification(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunVerification() error = %v", err)
	}
	if verification.Status != verifyrt.StatusWarning {
		t.Fatalf("verification.Status = %q, want %q", verification.Status, verifyrt.StatusWarning)
	}
	if !hasVerificationCheck(verification.Checks, "task.contract", verifyrt.StatusFailed) {
		t.Fatalf("expected failed task contract check, got %+v", verification.Checks)
	}
}

func TestGetRunVerificationSupportsBlockingSeverityOverrides(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "mail-blocking", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "mail-blocking",
		Content:    "send the email",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-4 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleTool,
		Name:      "email.send",
		Content:   `{"success":false,"error":"smtp refused recipient"}`,
		CreatedAt: now.Add(-2 * time.Second),
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := bus.Publish(ctx, eventbus.Event{
		Type:      eventbus.EventToolExecuted,
		RunID:     run.ID,
		SessionID: session.ID,
		Attrs: map[string]any{
			"tool_names": []string{"email.send"},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, bus, artifact.NewInMemoryStore()).
		WithVerificationPolicy(verifyrt.Policy{
			VerifierSeverities: map[string]verifyrt.IssueSeverity{
				"email.result": verifyrt.SeverityBlocking,
			},
		})
	verification, err := svc.GetRunVerification(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunVerification() error = %v", err)
	}
	if verification.Status != verifyrt.StatusFailed {
		t.Fatalf("verification.Status = %q, want %q", verification.Status, verifyrt.StatusFailed)
	}
	if verification.BlockingFailures != 1 {
		t.Fatalf("verification.BlockingFailures = %d, want 1", verification.BlockingFailures)
	}
	if !verification.ShouldBlockDelivery() {
		t.Fatal("expected blocking severity to prevent delivery")
	}
}

func TestGetRunVerificationBrowserExternalEffectPassesWithResultPageEvidence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "browser-submit-pass", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "browser-submit-pass",
		Content:    "打开表单，填写后提交",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-3 * time.Second)
	run.FinishedAt = now
	run.TaskContract = &agent.TaskContract{
		Goal:                   "打开表单，填写后提交",
		JobType:                "general",
		RequiresExternalEffect: true,
		ExpectedDeliverables: []agent.TaskContractDeliverable{{
			Kind:     "browser_evidence",
			Required: false,
		}},
		AcceptanceCriteria: []agent.TaskContractAcceptance{{
			ID:       "external_effect_verified",
			Summary:  "Do not report success without submit evidence.",
			Required: true,
		}},
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "browser.open",
			Content:   `{"session_id":"sess-1","url":"https://httpbin.org/forms/post","title":"forms"}`,
			CreatedAt: now.Add(-2 * time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "browser.click",
			Content:   `{"ok":true,"url":"https://httpbin.org/post","title":"httpbin.org"}`,
			CreatedAt: now.Add(-time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "browser.snapshot",
			Content:   `{"ok":true,"url":"https://httpbin.org/post","title":"httpbin.org","content":"{\"form\":{\"custname\":\"HopClaw QA\"}}","content_type":"text/html"}`,
			CreatedAt: now,
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
			"tool_names": []string{"browser.open", "browser.click", "browser.snapshot"},
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
}

func TestGetRunVerificationBrowserPassesWithSnapshotArtifactDeliverableWithoutEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	artifacts := artifact.NewInMemoryStore()

	session, err := sessions.GetOrCreate(ctx, "browser-snapshot-artifact", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "browser-snapshot-artifact",
		Content:    "打开页面并提取搜索结果",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-3 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleAssistant,
		Content:   "搜索结果已加载，已提取前几条结果。",
		CreatedAt: now,
		Metadata:  map[string]any{meta.KeyRunID: run.ID},
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := artifacts.Put(ctx, artifact.PutRequest{
		Kind:        "tool_output",
		ContentType: "text/plain; charset=utf-8",
		Body:        []byte("{\"content\":\"<html><title>openai - 搜索</title></html>\"}"),
		Metadata: map[string]any{
			meta.KeyRunID:    run.ID,
			meta.KeyToolName: "browser.snapshot",
			"name":           "browser.snapshot-call_00_test.txt",
		},
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, nil, artifacts)
	verification, err := svc.GetRunVerification(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunVerification() error = %v", err)
	}
	if verification.Status != verifyrt.StatusPassed {
		t.Fatalf("verification.Status = %q, want %q", verification.Status, verifyrt.StatusPassed)
	}
	if !hasVerificationCheck(verification.Checks, "browser.result", verifyrt.StatusPassed) {
		t.Fatalf("expected passed browser check, got %+v", verification.Checks)
	}
}

func TestGetRunVerificationDesktopPassesWithUITreeEvidence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "desktop-key", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "desktop-key",
		Content:    "查看桌面窗口状态",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-4 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleTool,
		Name:      "desktop.capture_tree",
		Content:   `{"app":"Safari","window":"Example","elements":[{"role":"button","name":"Continue"}]}`,
		CreatedAt: now.Add(-time.Second),
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := bus.Publish(ctx, eventbus.Event{
		Type:      eventbus.EventToolExecuted,
		RunID:     run.ID,
		SessionID: session.ID,
		Attrs: map[string]any{
			"tool_names": []string{"desktop.capture_tree"},
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
	if !hasVerificationCheck(verification.Checks, "desktop.result", verifyrt.StatusPassed) {
		t.Fatalf("expected passed desktop check, got %+v", verification.Checks)
	}
}

func hasVerificationCheck(checks []verifyrt.Check, name string, status verifyrt.Status) bool {
	for _, check := range checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
