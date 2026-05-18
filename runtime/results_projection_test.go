package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/resultmodel"
)

func TestGetRunResultProjectsTranscriptTaskOutcomesAndEventLedger(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "chat:projection", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey:      "chat:projection",
		ExternalEventID: "evt-projection",
		Content:         "prepare the brief",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-2 * time.Second)
	run.FinishedAt = now
	run.ExecutionGraph = &agent.ExecutionGraph{
		RunID:     run.ID,
		SessionID: session.ID,
		Tasks: []agent.ExecutionTask{{
			ID:     "task-brief",
			Status: planner.TaskCompleted,
			LastOutcome: &agent.TaskOutcome{
				TaskID:         "task-brief",
				Status:         planner.TaskCompleted,
				Summary:        "prepared brief from task outcome",
				IdempotencyKey: "task:brief",
				OutputBlocks: []resultmodel.ResultBlock{{
					Kind:    resultmodel.ResultBlockText,
					Content: "prepared brief from task outcome",
				}},
				ToolResults: []resultmodel.ToolResult{{
					ToolName: "brief.generate",
					Summary:  "prepared brief from task outcome",
				}},
				Artifacts: []resultmodel.ResultArtifact{{
					Kind:        "document",
					Name:        "brief.md",
					URI:         "artifact://brief-1",
					ContentType: "text/markdown",
				}},
			},
		}},
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "prepare the brief",
			CreatedAt: now.Add(-1500 * time.Millisecond),
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "Final answer from transcript.",
			CreatedAt: now,
			Metadata:  map[string]any{meta.KeyRunID: run.ID},
		},
	)
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	for _, event := range []eventbus.Event{
		{
			Type:      eventbus.EventToolExecuted,
			RunID:     run.ID,
			SessionID: session.ID,
			Attrs: map[string]any{
				"tool_name":     "brief.generate",
				"artifact_uris": []string{"artifact://brief-1"},
			},
		},
		{
			Type:      eventbus.EventGovernanceDeliveryDelivered,
			RunID:     run.ID,
			SessionID: session.ID,
			Attrs: map[string]any{
				"adapter_name":      "audit-hub",
				"delivery_status":   "delivered",
				"source_event_id":   "evt-source-1",
				"source_event_type": string(eventbus.EventToolExecuted),
			},
		},
	} {
		if err := bus.Publish(ctx, event); err != nil {
			t.Fatalf("Publish(%s) error = %v", event.Type, err)
		}
	}

	svc := NewService(nil, sessions, runs, approval.NewInMemoryStore(), bus, nil)
	result, err := svc.GetRunResult(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunResult() error = %v", err)
	}
	if result.Output != "Final answer from transcript." {
		t.Fatalf("Output = %q, want transcript output", result.Output)
	}
	if len(result.TaskOutcomes) != 1 {
		t.Fatalf("TaskOutcomes = %#v, want 1 task outcome", result.TaskOutcomes)
	}
	if result.TaskOutcomes[0].IdempotencyKey != "task:brief" {
		t.Fatalf("TaskOutcomes[0].IdempotencyKey = %q, want task:brief", result.TaskOutcomes[0].IdempotencyKey)
	}
	if len(result.Deliverables) != 1 || result.Deliverables[0].URI != "artifact://brief-1" {
		t.Fatalf("Deliverables = %#v", result.Deliverables)
	}
	if result.EventLedger == nil || len(result.EventLedger.Events) != 2 {
		t.Fatalf("EventLedger = %#v, want 2 events", result.EventLedger)
	}
	if result.EventLedger.Events[0].EventClass != EventClassEvidence || result.EventLedger.Events[1].EventClass != EventClassDelivery {
		t.Fatalf("EventLedger classes = %#v", result.EventLedger.Events)
	}
	if result.Bundle == nil {
		t.Fatal("expected result bundle")
	}
	if got := result.Bundle.StructuredData["task_outcome_count"]; got != 1 {
		t.Fatalf("bundle.StructuredData[task_outcome_count] = %#v, want 1", got)
	}
	if got := result.Bundle.StructuredData["event_ledger_delivery_count"]; got != 1 {
		t.Fatalf("bundle.StructuredData[event_ledger_delivery_count] = %#v, want 1", got)
	}
}
