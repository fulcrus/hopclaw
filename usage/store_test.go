package usage

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryStore_RecordAndQuery(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
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

func TestInMemoryStore_QueryByModel(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
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

func TestInMemoryStore_QueryBySession(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
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

func TestInMemoryStore_QueryByTimeRange(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
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

func TestInMemoryStore_QueryWithLimit(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
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

func TestInMemoryStore_Summarize(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
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

	miniUsage, ok := summary.ByModel["gpt-4o-mini"]
	if !ok {
		t.Fatal("expected gpt-4o-mini in ByModel")
	}
	if miniUsage.CallCount != 1 {
		t.Errorf("expected 1 call for gpt-4o-mini, got %d", miniUsage.CallCount)
	}
}

func TestInMemoryStore_SummarizeWithFilter(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
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

func TestInMemoryStore_SummarizeByWorkflowID(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	records := []Record{
		{
			RunID: "run-1", WorkflowID: "wf-1", Model: "gpt-4o",
			PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150,
			RecordType: RecordTypeModelCall,
		},
		{
			RunID: "run-2", WorkflowID: "wf-1", ToolName: "exec.run",
			Duration: 2 * time.Second, RecordType: RecordTypeToolExecution,
		},
		{
			RunID: "run-3", WorkflowID: "wf-2", Model: "gpt-4o",
			PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300,
			RecordType: RecordTypeModelCall,
		},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	summary, err := store.Summarize(ctx, QueryFilter{
		WorkflowID: "wf-1",
		RecordType: RecordTypeModelCall,
	})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.RecordCount != 1 {
		t.Fatalf("summary.RecordCount = %d, want 1", summary.RecordCount)
	}
	if summary.TotalTokens != 150 {
		t.Fatalf("summary.TotalTokens = %d, want 150", summary.TotalTokens)
	}

	results, err := store.Query(ctx, QueryFilter{
		WorkflowID: "wf-1",
		RecordType: RecordTypeToolExecution,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].ToolName != "exec.run" {
		t.Fatalf("results[0].ToolName = %q, want exec.run", results[0].ToolName)
	}
}

func TestInMemoryStore_MaxRecordsEviction(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	// Insert more records than maxRecords.
	overflowCount := 50
	totalInserts := maxRecords + overflowCount
	for i := 0; i < totalInserts; i++ {
		rec := Record{
			Model:       "gpt-4o",
			TotalTokens: i,
		}
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	results, err := store.Query(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != maxRecords {
		t.Fatalf("expected %d records after eviction, got %d", maxRecords, len(results))
	}

	// Oldest records should have been evicted; first record should have
	// TotalTokens == overflowCount (the first overflowCount records were dropped).
	if results[0].TotalTokens != overflowCount {
		t.Errorf("expected first record TotalTokens %d, got %d", overflowCount, results[0].TotalTokens)
	}
}

func TestInMemoryStore_PresetIDAndTimestamp(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	fixedTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	rec := Record{
		ID:        "custom-id-123",
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
	if results[0].ID != "custom-id-123" {
		t.Errorf("expected custom ID, got %s", results[0].ID)
	}
	if !results[0].CreatedAt.Equal(fixedTime) {
		t.Errorf("expected preset timestamp, got %v", results[0].CreatedAt)
	}
}

// ---------------------------------------------------------------------------
// DailySummary tests
// ---------------------------------------------------------------------------

func TestInMemoryStore_DailySummary(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
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

	// First entry should be 2025-01-15 (sorted ascending).
	if daily[0].Date != "2025-01-15" {
		t.Errorf("expected first date 2025-01-15, got %s", daily[0].Date)
	}
	if daily[0].CallCount != 2 {
		t.Errorf("expected 2 calls on 2025-01-15, got %d", daily[0].CallCount)
	}
	if daily[0].PromptTokens != 400 {
		t.Errorf("expected 400 prompt tokens on 2025-01-15, got %d", daily[0].PromptTokens)
	}
	if daily[0].TotalTokens != 600 {
		t.Errorf("expected 600 total tokens on 2025-01-15, got %d", daily[0].TotalTokens)
	}

	// Check ByModel breakdown for day 1.
	gpt4o, ok := daily[0].ByModel["gpt-4o"]
	if !ok {
		t.Fatal("expected gpt-4o in day 1 ByModel")
	}
	if gpt4o.CallCount != 2 {
		t.Errorf("expected 2 gpt-4o calls on day 1, got %d", gpt4o.CallCount)
	}

	// Second entry should be 2025-01-16.
	if daily[1].Date != "2025-01-16" {
		t.Errorf("expected second date 2025-01-16, got %s", daily[1].Date)
	}
	if daily[1].CallCount != 1 {
		t.Errorf("expected 1 call on 2025-01-16, got %d", daily[1].CallCount)
	}
}

func TestInMemoryStore_DailySummaryWithFilter(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
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

	// Filter to only include day2.
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

func TestInMemoryStore_DailySummaryEmpty(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	daily, err := store.DailySummary(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("daily summary: %v", err)
	}
	if len(daily) != 0 {
		t.Fatalf("expected 0 daily entries for empty store, got %d", len(daily))
	}
}

// ---------------------------------------------------------------------------
// ProviderSummary tests
// ---------------------------------------------------------------------------

func TestInMemoryStore_ProviderSummary(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	records := []Record{
		{Model: "gpt-4o", Provider: "openai", PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, CostEstimate: 0.01},
		{Model: "claude-sonnet-4-20250514", Provider: "anthropic", PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300, CostEstimate: 0.02},
		{Model: "gpt-4o-mini", Provider: "openai", PromptTokens: 300, CompletionTokens: 150, TotalTokens: 450, CostEstimate: 0.005},
		{Model: "claude-sonnet-4-20250514", Provider: "anthropic", PromptTokens: 400, CompletionTokens: 200, TotalTokens: 600, CostEstimate: 0.03},
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
	if openai.PromptTokens != 400 {
		t.Errorf("expected 400 openai prompt tokens, got %d", openai.PromptTokens)
	}
	if openai.TotalTokens != 600 {
		t.Errorf("expected 600 openai total tokens, got %d", openai.TotalTokens)
	}

	// Check ByModel breakdown.
	gpt4o, ok := openai.ByModel["gpt-4o"]
	if !ok {
		t.Fatal("expected gpt-4o in openai ByModel")
	}
	if gpt4o.CallCount != 1 {
		t.Errorf("expected 1 gpt-4o call under openai, got %d", gpt4o.CallCount)
	}
	gpt4oMini, ok := openai.ByModel["gpt-4o-mini"]
	if !ok {
		t.Fatal("expected gpt-4o-mini in openai ByModel")
	}
	if gpt4oMini.CallCount != 1 {
		t.Errorf("expected 1 gpt-4o-mini call under openai, got %d", gpt4oMini.CallCount)
	}

	anthropic, ok := providers["anthropic"]
	if !ok {
		t.Fatal("expected anthropic in providers")
	}
	if anthropic.CallCount != 2 {
		t.Errorf("expected 2 anthropic calls, got %d", anthropic.CallCount)
	}
	if anthropic.TotalTokens != 900 {
		t.Errorf("expected 900 anthropic total tokens, got %d", anthropic.TotalTokens)
	}
}

func TestInMemoryStore_ProviderSummaryEmptyProvider(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	// Records with empty Provider should be grouped under "unknown".
	records := []Record{
		{Model: "gpt-4o", Provider: "", PromptTokens: 100, TotalTokens: 100},
		{Model: "gpt-4o", Provider: "", PromptTokens: 200, TotalTokens: 200},
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
	unknown, ok := providers["unknown"]
	if !ok {
		t.Fatal("expected 'unknown' provider for empty provider field")
	}
	if unknown.CallCount != 2 {
		t.Errorf("expected 2 unknown calls, got %d", unknown.CallCount)
	}
}

func TestInMemoryStore_ProviderSummaryWithFilter(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	records := []Record{
		{Model: "gpt-4o", Provider: "openai", PromptTokens: 100, TotalTokens: 100, SessionID: "sess-a"},
		{Model: "gpt-4o", Provider: "openai", PromptTokens: 200, TotalTokens: 200, SessionID: "sess-b"},
		{Model: "claude-sonnet-4-20250514", Provider: "anthropic", PromptTokens: 300, TotalTokens: 300, SessionID: "sess-a"},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	providers, err := store.ProviderSummary(ctx, QueryFilter{SessionID: "sess-a"})
	if err != nil {
		t.Fatalf("provider summary: %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers for sess-a, got %d", len(providers))
	}
	openai := providers["openai"]
	if openai.CallCount != 1 {
		t.Errorf("expected 1 openai call for sess-a, got %d", openai.CallCount)
	}
}

// ---------------------------------------------------------------------------
// Cost estimation tests
// ---------------------------------------------------------------------------

func TestEstimateCost_KnownModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model            string
		promptTokens     int
		completionTokens int
		wantNonZero      bool
	}{
		{"gpt-4o", 1000, 500, true},
		{"gpt-4o-mini", 1000, 500, true},
		{"claude-sonnet-4-20250514", 1000, 500, true},
		{"claude-haiku-4-5-20251001", 1000, 500, true},
		{"claude-opus-4-20250515", 1000, 500, true},
		{"o1", 1000, 500, true},
		{"o3-mini", 1000, 500, true},
	}

	for _, tt := range tests {
		cost := EstimateCost(tt.model, tt.promptTokens, tt.completionTokens)
		if tt.wantNonZero && cost <= 0 {
			t.Errorf("EstimateCost(%q, %d, %d) = %f, want > 0", tt.model, tt.promptTokens, tt.completionTokens, cost)
		}
	}
}

func TestEstimateCost_UnknownModel(t *testing.T) {
	t.Parallel()
	cost := EstimateCost("unknown-model-xyz", 1000, 500)
	if cost != 0 {
		t.Errorf("expected 0 for unknown model, got %f", cost)
	}
}

func TestEstimateCost_Gpt4o_Exact(t *testing.T) {
	t.Parallel()
	// gpt-4o: $2.50/1M prompt, $10.00/1M completion
	// 1M prompt + 1M completion = $2.50 + $10.00 = $12.50
	cost := EstimateCost("gpt-4o", 1_000_000, 1_000_000)
	expected := 12.50
	if cost != expected {
		t.Errorf("expected cost %f for gpt-4o with 1M+1M tokens, got %f", expected, cost)
	}
}

func TestEstimateCost_ZeroTokens(t *testing.T) {
	t.Parallel()
	cost := EstimateCost("gpt-4o", 0, 0)
	if cost != 0 {
		t.Errorf("expected 0 cost for zero tokens, got %f", cost)
	}
}
