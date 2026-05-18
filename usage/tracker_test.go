package usage

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// NewTracker
// ---------------------------------------------------------------------------

func TestNewTracker(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	tracker := NewTracker(store)

	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}
	if tracker.Store() != store {
		t.Fatal("expected Store() to return the provided store")
	}
}

// ---------------------------------------------------------------------------
// TrackModelCall
// ---------------------------------------------------------------------------

func TestTrackModelCall_RecordsToStore(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	tracker := NewTracker(store)
	ctx := context.Background()

	rec := Record{
		RunID:            "run-1",
		SessionID:        "sess-1",
		Model:            "gpt-4o",
		Provider:         "openai",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}
	if err := tracker.TrackModelCall(ctx, rec); err != nil {
		t.Fatalf("TrackModelCall: %v", err)
	}

	results, err := store.Query(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 record, got %d", len(results))
	}
	if results[0].Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", results[0].Model)
	}
}

func TestTrackModelCall_AutoEstimatesCost(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	tracker := NewTracker(store)
	ctx := context.Background()

	rec := Record{
		Model:            "gpt-4o",
		PromptTokens:     1_000_000,
		CompletionTokens: 1_000_000,
		TotalTokens:      2_000_000,
		// CostEstimate is zero, should be auto-computed.
	}
	if err := tracker.TrackModelCall(ctx, rec); err != nil {
		t.Fatalf("TrackModelCall: %v", err)
	}

	results, err := store.Query(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 record, got %d", len(results))
	}
	if results[0].CostEstimate <= 0 {
		t.Errorf("expected auto-estimated cost > 0, got %f", results[0].CostEstimate)
	}
}

func TestTrackModelCall_PreservesCostEstimate(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	tracker := NewTracker(store)
	ctx := context.Background()

	rec := Record{
		Model:            "gpt-4o",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CostEstimate:     0.42, // pre-set
	}
	if err := tracker.TrackModelCall(ctx, rec); err != nil {
		t.Fatalf("TrackModelCall: %v", err)
	}

	results, err := store.Query(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if results[0].CostEstimate != 0.42 {
		t.Errorf("expected cost 0.42, got %f", results[0].CostEstimate)
	}
}

// ---------------------------------------------------------------------------
// TrackToolExecution
// ---------------------------------------------------------------------------

func TestTrackToolExecution_SetsRecordType(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	tracker := NewTracker(store)
	ctx := context.Background()

	rec := Record{
		SessionID: "sess-1",
		ToolName:  "exec.run",
	}
	if err := tracker.TrackToolExecution(ctx, rec); err != nil {
		t.Fatalf("TrackToolExecution: %v", err)
	}

	results, err := store.Query(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 record, got %d", len(results))
	}
	if results[0].RecordType != RecordTypeToolExecution {
		t.Errorf("expected record type %q, got %q", RecordTypeToolExecution, results[0].RecordType)
	}
	if results[0].ToolName != "exec.run" {
		t.Errorf("expected tool name exec.run, got %s", results[0].ToolName)
	}
}

// ---------------------------------------------------------------------------
// WithEventPublisher
// ---------------------------------------------------------------------------

func TestWithEventPublisher_Chainable(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	tracker := NewTracker(store)

	// WithEventPublisher should return the same tracker (fluent API).
	result := tracker.WithEventPublisher(nil)
	if result != tracker {
		t.Fatal("expected WithEventPublisher to return the same tracker")
	}
}

// ---------------------------------------------------------------------------
// Store accessor
// ---------------------------------------------------------------------------

func TestStore_ReturnsUnderlyingStore(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	tracker := NewTracker(store)

	if tracker.Store() != store {
		t.Fatal("Store() should return the store passed to NewTracker")
	}
}
