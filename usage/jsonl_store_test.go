package usage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestJSONLStore(t *testing.T) *JSONLStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	s, err := NewJSONLStore(path)
	if err != nil {
		t.Fatalf("failed to create JSONL store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestJSONLStore_RecordAndQuery(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
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
	if err := store.Record(ctx, rec); err != nil {
		t.Fatalf("record: %v", err)
	}

	results, err := store.Query(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 record, got %d", len(results))
	}
	if results[0].ID == "" {
		t.Error("expected auto-generated ID")
	}
	if results[0].CreatedAt.IsZero() {
		t.Error("expected auto-generated CreatedAt")
	}
	if results[0].Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", results[0].Model)
	}
	if results[0].PromptTokens != 100 {
		t.Errorf("expected 100 prompt tokens, got %d", results[0].PromptTokens)
	}
}

func TestJSONLStore_QueryBySession(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
	ctx := context.Background()

	records := []Record{
		{SessionID: "sess-a", Model: "gpt-4o", TotalTokens: 100},
		{SessionID: "sess-b", Model: "gpt-4o", TotalTokens: 200},
		{SessionID: "sess-a", Model: "gpt-4o", TotalTokens: 300},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	results, err := store.Query(ctx, QueryFilter{SessionID: "sess-a"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 records for sess-a, got %d", len(results))
	}
}

func TestJSONLStore_QueryByModel(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
	ctx := context.Background()

	records := []Record{
		{Model: "gpt-4o", PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
		{Model: "gpt-4o-mini", PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300},
		{Model: "gpt-4o", PromptTokens: 300, CompletionTokens: 150, TotalTokens: 450},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	results, err := store.Query(ctx, QueryFilter{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 records for gpt-4o, got %d", len(results))
	}
}

func TestJSONLStore_QueryByTimeRange(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
	ctx := context.Background()

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	records := []Record{
		{Model: "gpt-4o", TotalTokens: 100, CreatedAt: base},
		{Model: "gpt-4o", TotalTokens: 200, CreatedAt: base.Add(1 * time.Hour)},
		{Model: "gpt-4o", TotalTokens: 300, CreatedAt: base.Add(2 * time.Hour)},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	// Query records between 30m and 90m after base.
	since := base.Add(30 * time.Minute)
	until := base.Add(90 * time.Minute)
	results, err := store.Query(ctx, QueryFilter{Since: since, Until: until})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 record in time range, got %d", len(results))
	}
	if results[0].TotalTokens != 200 {
		t.Errorf("expected 200 total tokens, got %d", results[0].TotalTokens)
	}
}

func TestJSONLStore_Summarize(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
	ctx := context.Background()

	records := []Record{
		{Model: "gpt-4o", PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, CostEstimate: 0.01},
		{Model: "gpt-4o-mini", PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300, CostEstimate: 0.005},
		{Model: "gpt-4o", PromptTokens: 300, CompletionTokens: 150, TotalTokens: 450, CostEstimate: 0.02},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	summary, err := store.Summarize(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.RecordCount != 3 {
		t.Errorf("expected 3 records, got %d", summary.RecordCount)
	}
	if summary.TotalPromptTokens != 600 {
		t.Errorf("expected 600 prompt tokens, got %d", summary.TotalPromptTokens)
	}
	if summary.TotalCompletionTokens != 300 {
		t.Errorf("expected 300 completion tokens, got %d", summary.TotalCompletionTokens)
	}
	if summary.TotalTokens != 900 {
		t.Errorf("expected 900 total tokens, got %d", summary.TotalTokens)
	}

	gpt4oUsage, ok := summary.ByModel["gpt-4o"]
	if !ok {
		t.Fatal("expected gpt-4o in ByModel")
	}
	if gpt4oUsage.CallCount != 2 {
		t.Errorf("expected 2 calls for gpt-4o, got %d", gpt4oUsage.CallCount)
	}
	if gpt4oUsage.PromptTokens != 400 {
		t.Errorf("expected 400 prompt tokens for gpt-4o, got %d", gpt4oUsage.PromptTokens)
	}
}

func TestJSONLStore_SessionSummary(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
	ctx := context.Background()

	base := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)
	records := []Record{
		{
			SessionID: "sess-x", Model: "gpt-4o", RecordType: RecordTypeModelCall,
			PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150,
			CostEstimate: 0.01, Duration: 200 * time.Millisecond,
			CreatedAt: base,
		},
		{
			SessionID: "sess-x", ToolName: "exec.run", RecordType: RecordTypeToolExecution,
			Duration:  500 * time.Millisecond,
			CreatedAt: base.Add(1 * time.Second),
		},
		{
			SessionID: "sess-y", Model: "gpt-4o", RecordType: RecordTypeModelCall,
			PromptTokens: 999, TotalTokens: 999,
			CreatedAt: base.Add(2 * time.Second),
		},
		{
			SessionID: "sess-x", Model: "gpt-4o-mini", RecordType: RecordTypeModelCall,
			PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300,
			CostEstimate: 0.005, Duration: 150 * time.Millisecond,
			CreatedAt: base.Add(3 * time.Second),
		},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	summary, err := store.SessionSummary(ctx, "sess-x")
	if err != nil {
		t.Fatalf("session summary: %v", err)
	}
	if summary.SessionID != "sess-x" {
		t.Errorf("expected session sess-x, got %s", summary.SessionID)
	}
	if summary.ModelCallCount != 2 {
		t.Errorf("expected 2 model calls, got %d", summary.ModelCallCount)
	}
	if summary.ToolExecutionCount != 1 {
		t.Errorf("expected 1 tool execution, got %d", summary.ToolExecutionCount)
	}
	if summary.TotalTokens != 450 {
		t.Errorf("expected 450 total tokens, got %d", summary.TotalTokens)
	}
	if !summary.FirstCallAt.Equal(base) {
		t.Errorf("expected first call at %v, got %v", base, summary.FirstCallAt)
	}
	if !summary.LastCallAt.Equal(base.Add(3 * time.Second)) {
		t.Errorf("expected last call at %v, got %v", base.Add(3*time.Second), summary.LastCallAt)
	}
}

func TestJSONLStore_SummarizeByWorkflowID(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
	ctx := context.Background()

	records := []Record{
		{
			RunID: "run-1", WorkflowID: "wf-1", ParentRunID: "run-root", ContinuationIndex: 0,
			Model: "gpt-4o", PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150,
			RecordType: RecordTypeModelCall,
		},
		{
			RunID: "run-2", WorkflowID: "wf-1", ParentRunID: "run-1", ContinuationIndex: 1,
			ToolName: "exec.run", Duration: 300 * time.Millisecond, RecordType: RecordTypeToolExecution,
		},
		{
			RunID: "run-3", WorkflowID: "wf-2", ParentRunID: "run-root", ContinuationIndex: 0,
			Model: "gpt-4o-mini", PromptTokens: 200, CompletionTokens: 50, TotalTokens: 250,
			RecordType: RecordTypeModelCall,
		},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	summary, err := store.Summarize(ctx, QueryFilter{WorkflowID: "wf-1", RecordType: RecordTypeModelCall})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.RecordCount != 1 || summary.TotalTokens != 150 {
		t.Fatalf("summary = %#v, want one model-call record with 150 tokens", summary)
	}

	results, err := store.Query(ctx, QueryFilter{WorkflowID: "wf-1", RecordType: RecordTypeToolExecution})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].ContinuationIndex != 1 {
		t.Fatalf("results[0].ContinuationIndex = %d, want 1", results[0].ContinuationIndex)
	}
}

func TestJSONLStore_DailySummary(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
	ctx := context.Background()

	day1 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2025, 1, 16, 14, 0, 0, 0, time.UTC)
	day3 := time.Date(2025, 1, 15, 22, 0, 0, 0, time.UTC) // same day as day1

	records := []Record{
		{Model: "gpt-4o", Provider: "openai", PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, CostEstimate: 0.01, CreatedAt: day1},
		{Model: "gpt-4o-mini", Provider: "openai", PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300, CostEstimate: 0.005, CreatedAt: day2},
		{Model: "gpt-4o", Provider: "openai", PromptTokens: 300, CompletionTokens: 150, TotalTokens: 450, CostEstimate: 0.02, CreatedAt: day3},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	daily, err := store.DailySummary(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("daily summary: %v", err)
	}
	if len(daily) != 2 {
		t.Fatalf("expected 2 daily entries, got %d", len(daily))
	}
	if daily[0].Date != "2025-01-15" {
		t.Errorf("expected first date 2025-01-15, got %s", daily[0].Date)
	}
	if daily[0].CallCount != 2 {
		t.Errorf("expected 2 calls on 2025-01-15, got %d", daily[0].CallCount)
	}
	if daily[0].TotalTokens != 600 {
		t.Errorf("expected 600 total tokens on 2025-01-15, got %d", daily[0].TotalTokens)
	}
	if daily[1].Date != "2025-01-16" {
		t.Errorf("expected second date 2025-01-16, got %s", daily[1].Date)
	}
	if daily[1].CallCount != 1 {
		t.Errorf("expected 1 call on 2025-01-16, got %d", daily[1].CallCount)
	}
}

func TestJSONLStore_ProviderSummary(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
	ctx := context.Background()

	records := []Record{
		{Model: "gpt-4o", Provider: "openai", PromptTokens: 100, TotalTokens: 150, CostEstimate: 0.01},
		{Model: "claude-sonnet-4-20250514", Provider: "anthropic", PromptTokens: 200, TotalTokens: 300, CostEstimate: 0.02},
		{Model: "gpt-4o-mini", Provider: "openai", PromptTokens: 300, TotalTokens: 450, CostEstimate: 0.005},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	providers, err := store.ProviderSummary(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("provider summary: %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}
	openai, ok := providers["openai"]
	if !ok {
		t.Fatal("expected openai in providers")
	}
	if openai.CallCount != 2 {
		t.Errorf("expected 2 openai calls, got %d", openai.CallCount)
	}
	if openai.TotalTokens != 600 {
		t.Errorf("expected 600 openai total tokens, got %d", openai.TotalTokens)
	}
	anthropic, ok := providers["anthropic"]
	if !ok {
		t.Fatal("expected anthropic in providers")
	}
	if anthropic.CallCount != 1 {
		t.Errorf("expected 1 anthropic call, got %d", anthropic.CallCount)
	}
}

func TestJSONLStore_Persistence(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	ctx := context.Background()

	// Write records and close.
	store1, err := NewJSONLStore(path)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	records := []Record{
		{Model: "gpt-4o", PromptTokens: 100, TotalTokens: 150, CostEstimate: 0.01},
		{Model: "gpt-4o-mini", PromptTokens: 200, TotalTokens: 300, CostEstimate: 0.005},
	}
	for _, rec := range records {
		if err := store1.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen and verify data persisted.
	store2, err := NewJSONLStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store2.Close()

	results, err := store2.Query(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("query after reopen: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 records after reopen, got %d", len(results))
	}
	if results[0].Model != "gpt-4o" {
		t.Errorf("expected first record model gpt-4o, got %s", results[0].Model)
	}
	if results[1].Model != "gpt-4o-mini" {
		t.Errorf("expected second record model gpt-4o-mini, got %s", results[1].Model)
	}

	// Add more records after reopen.
	if err := store2.Record(ctx, Record{Model: "claude-sonnet-4-20250514", PromptTokens: 300, TotalTokens: 450}); err != nil {
		t.Fatalf("record after reopen: %v", err)
	}
	results, err = store2.Query(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("query after append: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 records after append, got %d", len(results))
	}
}

func TestJSONLStore_PresetIDAndTimestamp(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
	ctx := context.Background()

	fixedTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	rec := Record{
		ID:        "custom-id-456",
		Model:     "gpt-4o",
		CreatedAt: fixedTime,
	}
	if err := store.Record(ctx, rec); err != nil {
		t.Fatalf("record: %v", err)
	}

	results, err := store.Query(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if results[0].ID != "custom-id-456" {
		t.Errorf("expected custom ID, got %s", results[0].ID)
	}
	if !results[0].CreatedAt.Equal(fixedTime) {
		t.Errorf("expected preset timestamp, got %v", results[0].CreatedAt)
	}
}

func TestJSONLStore_QueryWithLimit(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		if err := store.Record(ctx, Record{Model: "gpt-4o", TotalTokens: i}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	results, err := store.Query(ctx, QueryFilter{Limit: 3})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 records with limit, got %d", len(results))
	}
}

func TestJSONLStore_SummarizeWithFilter(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
	ctx := context.Background()

	records := []Record{
		{SessionID: "sess-a", Model: "gpt-4o", PromptTokens: 100, TotalTokens: 100},
		{SessionID: "sess-b", Model: "gpt-4o", PromptTokens: 200, TotalTokens: 200},
		{SessionID: "sess-a", Model: "gpt-4o", PromptTokens: 300, TotalTokens: 300},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	summary, err := store.Summarize(ctx, QueryFilter{SessionID: "sess-a"})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.RecordCount != 2 {
		t.Errorf("expected 2 records for sess-a, got %d", summary.RecordCount)
	}
	if summary.TotalPromptTokens != 400 {
		t.Errorf("expected 400 prompt tokens for sess-a, got %d", summary.TotalPromptTokens)
	}
}

func TestJSONLStore_DailySummaryWithFilter(t *testing.T) {
	t.Parallel()
	store := newTestJSONLStore(t)
	ctx := context.Background()

	day1 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2025, 1, 16, 14, 0, 0, 0, time.UTC)
	day3 := time.Date(2025, 1, 17, 8, 0, 0, 0, time.UTC)

	records := []Record{
		{Model: "gpt-4o", PromptTokens: 100, TotalTokens: 100, CreatedAt: day1},
		{Model: "gpt-4o", PromptTokens: 200, TotalTokens: 200, CreatedAt: day2},
		{Model: "gpt-4o", PromptTokens: 300, TotalTokens: 300, CreatedAt: day3},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	since := time.Date(2025, 1, 16, 0, 0, 0, 0, time.UTC)
	until := time.Date(2025, 1, 16, 23, 59, 59, 0, time.UTC)
	daily, err := store.DailySummary(ctx, QueryFilter{Since: since, Until: until})
	if err != nil {
		t.Fatalf("daily summary: %v", err)
	}
	if len(daily) != 1 {
		t.Fatalf("expected 1 daily entry with filter, got %d", len(daily))
	}
	if daily[0].Date != "2025-01-16" {
		t.Errorf("expected date 2025-01-16, got %s", daily[0].Date)
	}
	if daily[0].PromptTokens != 200 {
		t.Errorf("expected 200 prompt tokens, got %d", daily[0].PromptTokens)
	}
}
