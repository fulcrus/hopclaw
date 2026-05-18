package approval

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsStoreTracksLifecycleAndDelegates(t *testing.T) {
	store := &MetricsStore{Inner: NewInMemoryStore()}
	before := testutil.ToFloat64(metrics.ApprovalsPending)

	ticket, err := store.Create(context.Background(), Ticket{
		RunID:     "run-metrics",
		SessionID: "session-metrics",
		ToolCalls: []ToolCall{{ID: "call-1", Name: "fs.write"}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if afterCreate := testutil.ToFloat64(metrics.ApprovalsPending); afterCreate <= before {
		t.Fatalf("pending gauge after create = %v, want > %v", afterCreate, before)
	}

	if _, err := store.Get(context.Background(), ticket.ID); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := store.GetByRun(context.Background(), ticket.RunID); err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if _, err := store.UpsertExternalRef(context.Background(), ticket.ID, ExternalReference{
		Provider:   "jira",
		ExternalID: "JIRA-1",
	}); err != nil {
		t.Fatalf("UpsertExternalRef() error = %v", err)
	}
	if _, err := store.GetByExternal(context.Background(), "jira", "JIRA-1"); err != nil {
		t.Fatalf("GetByExternal() error = %v", err)
	}
	items, err := store.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}

	if _, err := store.Resolve(context.Background(), ticket.ID, Resolution{Status: StatusApproved}); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if afterResolve := testutil.ToFloat64(metrics.ApprovalsPending); afterResolve != before {
		t.Fatalf("pending gauge after resolve = %v, want %v", afterResolve, before)
	}
}
