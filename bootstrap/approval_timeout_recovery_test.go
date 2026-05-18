package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
)

func TestForceCancelTimedOutApprovalWithoutRuntimeCancelsTicketAndPublishesEvents(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	ticket, err := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-timeout",
		SessionID: "session-timeout",
		ToolCalls: []approval.ToolCall{{ID: "call-1", Name: "fs.write"}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	warnings := newStartupWarningCollector()
	recordApprovalTimeoutRunWarning(warnings, ticket.RunID, errors.New("approval timeout test warning"))
	bus := eventbus.NewInMemoryBus()

	if err := forceCancelTimedOutApproval(context.Background(), nil, store, bus, warnings, ticket); err != nil {
		t.Fatalf("forceCancelTimedOutApproval() error = %v", err)
	}

	resolved, err := store.Get(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if resolved.Status != approval.StatusCancelled {
		t.Fatalf("ticket status = %q, want %q", resolved.Status, approval.StatusCancelled)
	}
	if items := warnings.OperationalWarnings(); len(items) != 0 {
		t.Fatalf("OperationalWarnings() = %#v, want empty", items)
	}

	events := bus.Snapshot()
	if countEventType(events, eventbus.EventApprovalResolved) != 1 {
		t.Fatalf("approval.resolved events = %d, want 1", countEventType(events, eventbus.EventApprovalResolved))
	}
	if countEventType(events, eventbus.EventApprovalTimedOut) != 1 {
		t.Fatalf("approval.timed_out events = %d, want 1", countEventType(events, eventbus.EventApprovalTimedOut))
	}
}

func countEventType(events []eventbus.Event, kind eventbus.EventType) int {
	count := 0
	for _, event := range events {
		if event.Type == kind {
			count++
		}
	}
	return count
}
