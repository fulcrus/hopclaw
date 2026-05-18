package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
)

func TestGetRunEventLedgerClassifiesEvidenceAuditAndDelivery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "chat:event-ledger", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey:      "chat:event-ledger",
		ExternalEventID: "evt-ledger",
		Content:         "produce a report",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = agent.RunCompleted
	run.StartedAt = time.Now().UTC().Add(-2 * time.Second)
	run.FinishedAt = time.Now().UTC()
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	events := []eventbus.Event{
		{
			ID:        "evt-001",
			Type:      eventbus.EventToolExecuted,
			RunID:     run.ID,
			SessionID: session.ID,
			Attrs: map[string]any{
				"tool_name": "report.generate",
				"results": []map[string]any{{
					"summary":      "report generated",
					"artifact_uri": "artifact://report-1",
				}},
			},
		},
		{
			ID:        "evt-002",
			Type:      eventbus.EventApprovalRequested,
			RunID:     run.ID,
			SessionID: session.ID,
			Attrs: map[string]any{
				"summary": "approval requested",
			},
		},
		{
			ID:        "evt-003",
			Type:      eventbus.EventGovernanceDeliveryDelivered,
			RunID:     run.ID,
			SessionID: session.ID,
			Attrs: map[string]any{
				"adapter_name":      "audit-hub",
				"delivery_status":   "delivered",
				"source_event_id":   "evt-002",
				"source_event_type": string(eventbus.EventApprovalRequested),
			},
		},
	}
	for _, event := range events {
		if err := bus.Publish(ctx, event); err != nil {
			t.Fatalf("Publish(%s) error = %v", event.ID, err)
		}
	}

	svc := NewService(nil, sessions, runs, approval.NewInMemoryStore(), bus, nil)
	ledger, err := svc.GetRunEventLedger(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunEventLedger() error = %v", err)
	}
	if ledger == nil || len(ledger.Events) != 3 {
		t.Fatalf("ledger = %#v", ledger)
	}
	if ledger.Events[0].EventClass != EventClassEvidence {
		t.Fatalf("ledger.Events[0].EventClass = %q, want %q", ledger.Events[0].EventClass, EventClassEvidence)
	}
	if ledger.Events[1].EventClass != EventClassAudit {
		t.Fatalf("ledger.Events[1].EventClass = %q, want %q", ledger.Events[1].EventClass, EventClassAudit)
	}
	if ledger.Events[2].EventClass != EventClassDelivery {
		t.Fatalf("ledger.Events[2].EventClass = %q, want %q", ledger.Events[2].EventClass, EventClassDelivery)
	}
	if ledger.Events[0].Summary != "report generated" {
		t.Fatalf("ledger.Events[0].Summary = %q, want %q", ledger.Events[0].Summary, "report generated")
	}

	result, err := svc.GetRunResult(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunResult() error = %v", err)
	}
	if result.EventLedger == nil || len(result.EventLedger.Events) != 3 {
		t.Fatalf("result.EventLedger = %#v", result.EventLedger)
	}
}
