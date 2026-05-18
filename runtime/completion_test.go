package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	"github.com/fulcrus/hopclaw/internal/meta"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

func TestGetRunCompletionUnifiesResultVerificationAndDeliveryReceipts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, store := newGovernanceDeliveryTestService()

	session, err := svc.sessions.GetOrCreate(ctx, "chat:completion", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := svc.runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey:      "chat:completion",
		ExternalEventID: "evt-completion",
		Content:         "generate report",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-3 * time.Second)
	run.FinishedAt = now
	if err := svc.runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "generate report",
			CreatedAt: now.Add(-2 * time.Second),
			Metadata: map[string]any{
				meta.KeyChannel:   "slack",
				meta.KeyMessageID: "msg-completion",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleTool,
			Name:      "report.generate",
			Content:   `{"summary":"report ready"}`,
			CreatedAt: now.Add(-time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "Report finished.",
			CreatedAt: now,
			Metadata:  map[string]any{meta.KeyRunID: run.ID},
		},
	)
	if err := svc.sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	bus, ok := svc.events.(*eventbus.InMemoryBus)
	if !ok {
		t.Fatal("expected in-memory bus")
	}
	if err := bus.Publish(ctx, eventbus.Event{
		Type:      eventbus.EventToolExecuted,
		RunID:     run.ID,
		SessionID: session.ID,
		Attrs: map[string]any{
			"tool_names":    []string{"report.generate"},
			"artifact_uris": []string{"artifact://report-1"},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	mustEnqueueGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "delivery-1",
		AdapterName: "audit-hub",
		Status:      controlgov.DeliveryStatusDelivered,
		Attempts:    1,
		MaxAttempts: 3,
		CreatedAt:   now.Add(-500 * time.Millisecond),
		UpdatedAt:   now,
		DeliveredAt: now,
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-delivery-1",
			EventType: eventbus.EventGovernanceDeliveryDelivered,
			RunID:     run.ID,
			SessionID: session.ID,
			Summary:   "audit record delivered",
		},
	})

	completion, err := svc.GetRunCompletion(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunCompletion() error = %v", err)
	}
	if completion.Result == nil {
		t.Fatal("expected result in completion view")
	}
	if completion.Verification == nil {
		t.Fatal("expected verification in completion view")
	}
	if completion.Bundle == nil {
		t.Fatal("expected bundle in completion view")
	}
	if completion.Delivery == nil {
		t.Fatal("expected delivery plan in completion view")
	}
	if completion.RunID != run.ID || completion.Result.RunID != run.ID {
		t.Fatalf("run ids = %#v / %#v, want %q", completion.RunID, completion.Result.RunID, run.ID)
	}
	if completion.Outcome != RunOutcomeCompleted || completion.Result.Outcome != RunOutcomeCompleted {
		t.Fatalf("outcome = %q / %q, want %q", completion.Outcome, completion.Result.Outcome, RunOutcomeCompleted)
	}
	if completion.Verification.Status != verifyrt.StatusPassed {
		t.Fatalf("verification.Status = %q, want %q", completion.Verification.Status, verifyrt.StatusPassed)
	}
	if len(completion.Receipts) != 1 {
		t.Fatalf("len(completion.Receipts) = %d, want 1", len(completion.Receipts))
	}
	if len(completion.Result.Receipts) != 1 || len(completion.Bundle.Receipts) != 1 {
		t.Fatalf("result/bundle receipts = %d/%d, want 1/1", len(completion.Result.Receipts), len(completion.Bundle.Receipts))
	}
	if completion.Receipts[0].AdapterName != "audit-hub" || completion.Receipts[0].Status != "delivered" {
		t.Fatalf("completion.Receipts[0] = %#v", completion.Receipts[0])
	}
	if completion.Receipts[0].IdempotencyKey == "" {
		t.Fatalf("completion.Receipts[0].IdempotencyKey = %q, want non-empty", completion.Receipts[0].IdempotencyKey)
	}
	if completion.Result.Delivery == nil || completion.Result.Delivery.Summary == "" {
		t.Fatalf("result.Delivery = %#v", completion.Result.Delivery)
	}
	if len(completion.Result.Delivery.Receipts) != 1 || len(completion.Delivery.Receipts) != 1 {
		t.Fatalf("delivery envelope receipts = %d/%d, want 1/1", len(completion.Result.Delivery.Receipts), len(completion.Delivery.Receipts))
	}
	if completion.Result.Delivery.Receipts[0].IdempotencyKey != completion.Receipts[0].IdempotencyKey {
		t.Fatalf("delivery receipt idempotency key = %q, want %q", completion.Result.Delivery.Receipts[0].IdempotencyKey, completion.Receipts[0].IdempotencyKey)
	}
	if got := completion.Bundle.StructuredData["delivery_receipt_count"]; got != 1 {
		t.Fatalf("bundle.StructuredData[delivery_receipt_count] = %#v, want 1", got)
	}
}
