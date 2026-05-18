package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/usage"
)

// ---------------------------------------------------------------------------
// handleUsageSummary
// ---------------------------------------------------------------------------

func TestUsageSummaryNilStore(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/usage/summary", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: status = %d", rec.Code)
	}
}

func TestUsageSummaryEmpty(t *testing.T) {
	t.Parallel()

	store := usage.NewInMemoryStore()
	gw := newTestGatewayFull(t)
	gw.SetUsageStore(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/usage/summary", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty summary: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload usageSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Summary == nil {
		t.Fatal("expected non-nil summary")
	}
}

func TestUsageSummaryWithRecords(t *testing.T) {
	t.Parallel()

	store := usage.NewInMemoryStore()
	_ = store.Record(context.Background(), usage.Record{
		Model:            "gpt-4",
		Provider:         "openai",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CreatedAt:        time.Now(),
	})

	gw := newTestGatewayFull(t)
	gw.SetUsageStore(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/usage/summary", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("summary: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload usageSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Summary.TotalPromptTokens != 100 {
		t.Fatalf("total_prompt_tokens = %d, want 100", payload.Summary.TotalPromptTokens)
	}
}

func TestUsageSummaryWithTimeFilter(t *testing.T) {
	t.Parallel()

	store := usage.NewInMemoryStore()
	past := time.Now().Add(-48 * time.Hour)
	_ = store.Record(context.Background(), usage.Record{
		Model:        "gpt-4",
		Provider:     "openai",
		PromptTokens: 200,
		CreatedAt:    past,
	})
	_ = store.Record(context.Background(), usage.Record{
		Model:        "gpt-4",
		Provider:     "openai",
		PromptTokens: 300,
		CreatedAt:    time.Now(),
	})

	gw := newTestGatewayFull(t)
	gw.SetUsageStore(store)
	handler := gw.Handler()

	since := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	rec := doRequest(t, handler, http.MethodGet, "/operator/usage/summary?since="+since, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered summary: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// handleUsageSessionSummary
// ---------------------------------------------------------------------------

func TestUsageSessionSummaryNilStore(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/usage/session/sess-1", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: status = %d", rec.Code)
	}
}

func TestUsageSessionSummarySuccess(t *testing.T) {
	t.Parallel()

	store := usage.NewInMemoryStore()
	_ = store.Record(context.Background(), usage.Record{
		SessionID:        "sess-abc",
		Model:            "gpt-4",
		Provider:         "openai",
		PromptTokens:     500,
		CompletionTokens: 250,
		TotalTokens:      750,
		CreatedAt:        time.Now(),
	})

	gw := newTestGatewayFull(t)
	gw.SetUsageStore(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/usage/session/sess-abc", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("session summary: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload sessionCostResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Session == nil {
		t.Fatal("expected non-nil session summary")
	}
}

// ---------------------------------------------------------------------------
// handleUsageDailySummary
// ---------------------------------------------------------------------------

func TestUsageDailySummaryNilStore(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/usage/daily", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: status = %d", rec.Code)
	}
}

func TestUsageDailySummaryEmpty(t *testing.T) {
	t.Parallel()

	store := usage.NewInMemoryStore()
	gw := newTestGatewayFull(t)
	gw.SetUsageStore(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/usage/daily", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty daily: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload dailyUsageResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

// ---------------------------------------------------------------------------
// handleUsageProviderSummary
// ---------------------------------------------------------------------------

func TestUsageProviderSummaryNilStore(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/usage/providers", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: status = %d", rec.Code)
	}
}

func TestUsageProviderSummaryEmpty(t *testing.T) {
	t.Parallel()

	store := usage.NewInMemoryStore()
	gw := newTestGatewayFull(t)
	gw.SetUsageStore(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/usage/providers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty providers: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload providerUsageResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}
