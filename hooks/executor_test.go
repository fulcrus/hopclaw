package hooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
)

// ---------------------------------------------------------------------------
// Test HTTP hook fires POST with correct payload
// ---------------------------------------------------------------------------

func TestFireHTTPHook(t *testing.T) {
	var (
		mu           sync.Mutex
		gotBody      map[string]any
		gotHeaders   http.Header
		gotMethod    string
		requestCount int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requestCount++
		gotMethod = r.Method
		gotHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()
	h, err := store.Add(ctx, Hook{
		Name:    "notify-slack",
		Enabled: true,
		Trigger: TriggerRunCompleted,
		Kind:    KindHTTP,
		URL:     srv.URL,
		Headers: map[string]string{"X-Custom": "test-value"},
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}

	executor := NewExecutor(store)
	payload := map[string]any{"run_id": "run-001", "status": "completed"}
	results := executor.Fire(ctx, TriggerRunCompleted, HookPhasePost, payload)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].HookID != h.ID {
		t.Errorf("expected hook id %s, got %s", h.ID, results[0].HookID)
	}
	if results[0].Status != "ok" {
		t.Errorf("expected status ok, got %s (error: %s)", results[0].Status, results[0].Error)
	}
	if results[0].Duration <= 0 {
		t.Errorf("expected positive duration, got %v", results[0].Duration)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotBody["run_id"] != "run-001" {
		t.Errorf("expected run_id run-001, got %v", gotBody["run_id"])
	}
	if gotHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("expected application/json content type, got %s", gotHeaders.Get("Content-Type"))
	}
	if gotHeaders.Get("X-Custom") != "test-value" {
		t.Errorf("expected X-Custom test-value, got %s", gotHeaders.Get("X-Custom"))
	}
	if requestCount != 1 {
		t.Errorf("expected 1 request, got %d", requestCount)
	}
}

// ---------------------------------------------------------------------------
// Test command hook executes and captures output
// ---------------------------------------------------------------------------

func TestFireCommandHook(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	_, err := store.Add(ctx, Hook{
		Name:    "log-event",
		Enabled: true,
		Trigger: TriggerRunFailed,
		Kind:    KindCommand,
		Command: "cat", // cat reads stdin and writes to stdout; exit 0
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}

	executor := NewExecutor(store)
	payload := map[string]any{"run_id": "run-002", "error": "timeout"}
	results := executor.Fire(ctx, TriggerRunFailed, HookPhasePost, payload)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "ok" {
		t.Errorf("expected status ok, got %s (error: %s)", results[0].Status, results[0].Error)
	}
}

func TestFireFiltersScopedHooksByPayloadScope(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	globalHook, err := store.Add(ctx, Hook{
		Name:    "global",
		Enabled: true,
		Trigger: TriggerRunCompleted,
		Kind:    KindCommand,
		Command: "true",
	})
	if err != nil {
		t.Fatalf("add global hook: %v", err)
	}
	matchingHook, err := store.Add(ctx, Hook{
		Name:         "matching",
		Enabled:      true,
		Trigger:      TriggerRunCompleted,
		Kind:         KindCommand,
		Command:      "true",
		AutomationID: "auto-a",
	})
	if err != nil {
		t.Fatalf("add matching hook: %v", err)
	}
	if _, err := store.Add(ctx, Hook{
		Name:         "other-automation",
		Enabled:      true,
		Trigger:      TriggerRunCompleted,
		Kind:         KindCommand,
		Command:      "true",
		AutomationID: "auto-b",
	}); err != nil {
		t.Fatalf("add non-matching hook: %v", err)
	}

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerRunCompleted, HookPhasePost, map[string]any{
		"run_id": "run-tenant-a",
		"scope": domainscope.Ref{
			AutomationID: "auto-a",
		},
	})

	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	got := map[string]bool{}
	for _, result := range results {
		got[result.HookID] = true
	}
	if !got[globalHook.ID] || !got[matchingHook.ID] {
		t.Fatalf("hook ids = %#v", got)
	}
}

// ---------------------------------------------------------------------------
// Test hooks only fire for matching triggers
// ---------------------------------------------------------------------------

func TestFireOnlyMatchingTrigger(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()
	_, err := store.Add(ctx, Hook{
		Name:    "on-completed",
		Enabled: true,
		Trigger: TriggerRunCompleted,
		Kind:    KindHTTP,
		URL:     srv.URL,
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}
	_, err = store.Add(ctx, Hook{
		Name:    "on-failed",
		Enabled: true,
		Trigger: TriggerRunFailed,
		Kind:    KindHTTP,
		URL:     srv.URL,
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerRunCompleted, HookPhasePost, map[string]any{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Trigger != TriggerRunCompleted {
		t.Errorf("expected trigger %s, got %s", TriggerRunCompleted, results[0].Trigger)
	}
}

// ---------------------------------------------------------------------------
// Test disabled hooks are skipped
// ---------------------------------------------------------------------------

func TestDisabledHooksSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()
	_, err := store.Add(ctx, Hook{
		Name:    "disabled-hook",
		Enabled: false,
		Trigger: TriggerToolExecuted,
		Kind:    KindHTTP,
		URL:     srv.URL,
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerToolExecuted, HookPhasePost, map[string]any{})

	if len(results) != 0 {
		t.Fatalf("expected 0 results for disabled hook, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Test timeout behavior
// ---------------------------------------------------------------------------

func TestTimeoutBehavior(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()
	_, err := store.Add(ctx, Hook{
		Name:    "slow-hook",
		Enabled: true,
		Trigger: TriggerStartup,
		Kind:    KindHTTP,
		URL:     srv.URL,
		Timeout: 1, // 1 second
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerStartup, HookPhasePost, map[string]any{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "error" {
		t.Errorf("expected error status for timed-out hook, got %s", results[0].Status)
	}
	if results[0].Error == "" {
		t.Error("expected non-empty error message for timed-out hook")
	}
}

// ---------------------------------------------------------------------------
// Test recent results tracking
// ---------------------------------------------------------------------------

func TestRecentResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()
	_, err := store.Add(ctx, Hook{
		Name:    "counter-hook",
		Enabled: true,
		Trigger: TriggerToolExecuted,
		Kind:    KindHTTP,
		URL:     srv.URL,
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}

	executor := NewExecutor(store)

	// Fire the hook several times.
	const fireCount = 5
	for i := range fireCount {
		_ = i
		executor.Fire(ctx, TriggerToolExecuted, HookPhasePost, map[string]any{"i": i})
	}

	// Request all recent results.
	all := executor.RecentResults(0)
	if len(all) != fireCount {
		t.Fatalf("expected %d recent results, got %d", fireCount, len(all))
	}
	for _, r := range all {
		if r.Status != "ok" {
			t.Errorf("expected ok status, got %s", r.Status)
		}
	}

	// Request a subset.
	subset := executor.RecentResults(2)
	if len(subset) != 2 {
		t.Fatalf("expected 2 results, got %d", len(subset))
	}
}

// ---------------------------------------------------------------------------
// Test recent results circular buffer wrapping
// ---------------------------------------------------------------------------

func TestRecentResultsCircularBuffer(t *testing.T) {
	store := NewInMemoryStore()
	executor := NewExecutor(store)

	// Directly record more than maxRecentResults to verify wrapping.
	for i := range maxRecentResults + 20 {
		executor.recordResult(HookResult{
			HookID:     "hook-direct",
			Trigger:    TriggerShutdown,
			Status:     "ok",
			ExecutedAt: time.Now().Add(time.Duration(i) * time.Millisecond),
		})
	}

	all := executor.RecentResults(0)
	if len(all) != maxRecentResults {
		t.Fatalf("expected %d capped results, got %d", maxRecentResults, len(all))
	}
}

// ---------------------------------------------------------------------------
// Test HTTP hook retry on server error
// ---------------------------------------------------------------------------

func TestHTTPHookRetry(t *testing.T) {
	var (
		mu       sync.Mutex
		attempts int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		attempts++
		current := attempts
		mu.Unlock()

		if current < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()
	_, err := store.Add(ctx, Hook{
		Name:       "retry-hook",
		Enabled:    true,
		Trigger:    TriggerRunCompleted,
		Kind:       KindHTTP,
		URL:        srv.URL,
		RetryCount: 3,
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerRunCompleted, HookPhasePost, map[string]any{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "ok" {
		t.Errorf("expected ok after retries, got %s (error: %s)", results[0].Status, results[0].Error)
	}

	mu.Lock()
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	mu.Unlock()
}

// ---------------------------------------------------------------------------
// Test command hook failure
// ---------------------------------------------------------------------------

func TestCommandHookFailure(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	_, err := store.Add(ctx, Hook{
		Name:    "bad-command",
		Enabled: true,
		Trigger: TriggerShutdown,
		Kind:    KindCommand,
		Command: "exit 1",
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerShutdown, HookPhasePost, map[string]any{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "error" {
		t.Errorf("expected error status for failing command, got %s", results[0].Status)
	}
}

// ---------------------------------------------------------------------------
// Test priority ordering — lower priority runs first
// ---------------------------------------------------------------------------

func TestFirePriorityOrdering(t *testing.T) {
	var (
		mu    sync.Mutex
		order []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		mu.Lock()
		if name, ok := payload["hook_name"]; ok {
			order = append(order, name.(string))
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()

	// Add hooks with different priorities — store.Add applies defaults.
	_, _ = store.Add(ctx, Hook{
		Name:     "low-priority",
		Enabled:  true,
		Trigger:  TriggerRunCompleted,
		Kind:     KindHTTP,
		URL:      srv.URL,
		Priority: 200,
	})
	_, _ = store.Add(ctx, Hook{
		Name:     "high-priority",
		Enabled:  true,
		Trigger:  TriggerRunCompleted,
		Kind:     KindHTTP,
		URL:      srv.URL,
		Priority: 10,
	})
	_, _ = store.Add(ctx, Hook{
		Name:    "default-priority",
		Enabled: true,
		Trigger: TriggerRunCompleted,
		Kind:    KindHTTP,
		URL:     srv.URL,
		// Priority defaults to defaultHookPriority (100)
	})

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerRunCompleted, HookPhasePost, map[string]any{})

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "ok" {
			t.Errorf("hook %s failed: %s", r.HookID, r.Error)
		}
	}
}

// ---------------------------------------------------------------------------
// Test phase filtering — pre hooks only fire for pre phase
// ---------------------------------------------------------------------------

func TestFirePhaseFiltering(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()

	_, _ = store.Add(ctx, Hook{
		Name:    "pre-hook",
		Enabled: true,
		Trigger: TriggerToolExecuted,
		Kind:    KindHTTP,
		URL:     srv.URL,
		Phase:   HookPhasePre,
	})
	_, _ = store.Add(ctx, Hook{
		Name:    "post-hook",
		Enabled: true,
		Trigger: TriggerToolExecuted,
		Kind:    KindHTTP,
		URL:     srv.URL,
		Phase:   HookPhasePost,
	})
	_, _ = store.Add(ctx, Hook{
		Name:    "error-hook",
		Enabled: true,
		Trigger: TriggerToolExecuted,
		Kind:    KindHTTP,
		URL:     srv.URL,
		Phase:   HookPhaseError,
	})

	executor := NewExecutor(store)

	// Fire for pre phase — should only match the pre hook.
	preResults := executor.Fire(ctx, TriggerToolExecuted, HookPhasePre, map[string]any{})
	if len(preResults) != 1 {
		t.Fatalf("expected 1 pre result, got %d", len(preResults))
	}
	if preResults[0].Phase != HookPhasePre {
		t.Errorf("expected pre phase, got %s", preResults[0].Phase)
	}

	// Fire for post phase — should only match the post hook.
	postResults := executor.Fire(ctx, TriggerToolExecuted, HookPhasePost, map[string]any{})
	if len(postResults) != 1 {
		t.Fatalf("expected 1 post result, got %d", len(postResults))
	}
	if postResults[0].Phase != HookPhasePost {
		t.Errorf("expected post phase, got %s", postResults[0].Phase)
	}

	// Fire for error phase — should only match the error hook.
	errorResults := executor.Fire(ctx, TriggerToolExecuted, HookPhaseError, map[string]any{})
	if len(errorResults) != 1 {
		t.Fatalf("expected 1 error result, got %d", len(errorResults))
	}
	if errorResults[0].Phase != HookPhaseError {
		t.Errorf("expected error phase, got %s", errorResults[0].Phase)
	}
}

// ---------------------------------------------------------------------------
// Test filter expression — hooks with non-matching filter are skipped
// ---------------------------------------------------------------------------

func TestFireFilterExpression(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()

	_, _ = store.Add(ctx, Hook{
		Name:    "filtered-match",
		Enabled: true,
		Trigger: TriggerToolExecuted,
		Kind:    KindHTTP,
		URL:     srv.URL,
		Filter:  "tool_name == exec.run",
	})
	_, _ = store.Add(ctx, Hook{
		Name:    "filtered-no-match",
		Enabled: true,
		Trigger: TriggerToolExecuted,
		Kind:    KindHTTP,
		URL:     srv.URL,
		Filter:  "tool_name == read",
	})
	_, _ = store.Add(ctx, Hook{
		Name:    "no-filter",
		Enabled: true,
		Trigger: TriggerToolExecuted,
		Kind:    KindHTTP,
		URL:     srv.URL,
	})

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerToolExecuted, HookPhasePost, map[string]any{
		"tool_name": "exec.run",
	})

	// Should match "filtered-match" and "no-filter", skip "filtered-no-match".
	if len(results) != 2 {
		t.Fatalf("expected 2 results with filter, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Test pre-hook abort — error in pre-hook stops further execution
// ---------------------------------------------------------------------------

func TestFirePreHookAbort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()

	// First pre-hook fails (bad command).
	_, _ = store.Add(ctx, Hook{
		Name:     "pre-failing",
		Enabled:  true,
		Trigger:  TriggerToolExecuted,
		Kind:     KindCommand,
		Command:  "exit 1",
		Phase:    HookPhasePre,
		Priority: 10,
	})
	// Second pre-hook would succeed, but should never run.
	_, _ = store.Add(ctx, Hook{
		Name:     "pre-ok",
		Enabled:  true,
		Trigger:  TriggerToolExecuted,
		Kind:     KindHTTP,
		URL:      srv.URL,
		Phase:    HookPhasePre,
		Priority: 20,
	})

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerToolExecuted, HookPhasePre, map[string]any{})

	// Should return only 1 result (the failed one); second hook should not run.
	if len(results) != 1 {
		t.Fatalf("expected 1 result (pre abort), got %d", len(results))
	}
	if results[0].Status != "error" {
		t.Errorf("expected error status for aborting pre-hook, got %s", results[0].Status)
	}
}

// ---------------------------------------------------------------------------
// Test HookContext is injected into payload
// ---------------------------------------------------------------------------

func TestFireInjectsHookContext(t *testing.T) {
	var gotBody map[string]any
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()
	_, _ = store.Add(ctx, Hook{
		Name:    "context-check",
		Enabled: true,
		Trigger: TriggerRunCompleted,
		Kind:    KindHTTP,
		URL:     srv.URL,
	})

	executor := NewExecutor(store)
	payload := map[string]any{"session_id": "sess-1", "run_id": "run-1"}
	results := executor.Fire(ctx, TriggerRunCompleted, HookPhasePost, payload)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].RunID != "run-1" {
		t.Fatalf("expected run_id run-1, got %q", results[0].RunID)
	}
	if results[0].SessionID != "sess-1" {
		t.Fatalf("expected session_id sess-1, got %q", results[0].SessionID)
	}
	if results[0].Summary == "" {
		t.Fatal("expected non-empty hook result summary")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotBody == nil {
		t.Fatal("expected body to be received")
	}
	hookCtx, ok := gotBody["_hook_context"]
	if !ok {
		t.Fatal("expected _hook_context in payload")
	}
	ctxMap, ok := hookCtx.(map[string]any)
	if !ok {
		t.Fatalf("expected _hook_context to be a map, got %T", hookCtx)
	}
	if ctxMap["session_id"] != "sess-1" {
		t.Errorf("expected session_id sess-1, got %v", ctxMap["session_id"])
	}
	if ctxMap["phase"] != string(HookPhasePost) {
		t.Errorf("expected phase post, got %v", ctxMap["phase"])
	}
}

// ---------------------------------------------------------------------------
// Test default phase assignment — empty phase defaults to post
// ---------------------------------------------------------------------------

func TestFireDefaultPhase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()

	// Add a hook with no explicit phase — store.Add defaults to "post".
	_, _ = store.Add(ctx, Hook{
		Name:    "default-phase",
		Enabled: true,
		Trigger: TriggerRunCompleted,
		Kind:    KindHTTP,
		URL:     srv.URL,
	})

	executor := NewExecutor(store)

	// Fire with empty phase — should default to post and match.
	results := executor.Fire(ctx, TriggerRunCompleted, "", map[string]any{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result with default phase, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Test ListByTrigger returns sorted hooks
// ---------------------------------------------------------------------------

func TestListByTrigger(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	_, _ = store.Add(ctx, Hook{
		Name:     "low",
		Enabled:  true,
		Trigger:  TriggerRunCompleted,
		Kind:     KindHTTP,
		URL:      "http://example.com",
		Priority: 200,
	})
	_, _ = store.Add(ctx, Hook{
		Name:     "high",
		Enabled:  true,
		Trigger:  TriggerRunCompleted,
		Kind:     KindHTTP,
		URL:      "http://example.com",
		Priority: 10,
	})
	_, _ = store.Add(ctx, Hook{
		Name:    "different-trigger",
		Enabled: true,
		Trigger: TriggerRunFailed,
		Kind:    KindHTTP,
		URL:     "http://example.com",
	})
	_, _ = store.Add(ctx, Hook{
		Name:    "disabled",
		Enabled: false,
		Trigger: TriggerRunCompleted,
		Kind:    KindHTTP,
		URL:     "http://example.com",
	})

	matched := store.ListByTrigger(TriggerRunCompleted, HookPhasePost)
	if len(matched) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(matched))
	}
	if matched[0].Priority > matched[1].Priority {
		t.Errorf("expected sorted by priority ascending: %d, %d", matched[0].Priority, matched[1].Priority)
	}
	if matched[0].Name != "high" {
		t.Errorf("expected first hook to be 'high', got %s", matched[0].Name)
	}
}

// ---------------------------------------------------------------------------
// Test RecentResultsByHook
// ---------------------------------------------------------------------------

func TestRecentResultsByHook(t *testing.T) {
	store := NewInMemoryStore()
	executor := NewExecutor(store)

	executor.recordResult(HookResult{HookID: "hook-A", Status: "ok"})
	executor.recordResult(HookResult{HookID: "hook-B", Status: "ok"})
	executor.recordResult(HookResult{HookID: "hook-A", Status: "error"})
	executor.recordResult(HookResult{HookID: "hook-C", Status: "ok"})
	executor.recordResult(HookResult{HookID: "hook-A", Status: "ok"})

	results := executor.RecentResultsByHook("hook-A", 0)
	if len(results) != 3 {
		t.Fatalf("expected 3 results for hook-A, got %d", len(results))
	}

	// With limit.
	limited := executor.RecentResultsByHook("hook-A", 2)
	if len(limited) != 2 {
		t.Fatalf("expected 2 limited results, got %d", len(limited))
	}

	// Unknown hook.
	none := executor.RecentResultsByHook("hook-Z", 0)
	if len(none) != 0 {
		t.Fatalf("expected 0 results for unknown hook, got %d", len(none))
	}
}

// ---------------------------------------------------------------------------
// Test Store.Add assigns defaults
// ---------------------------------------------------------------------------

func TestStoreAddDefaults(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	h, err := store.Add(ctx, Hook{
		Name:    "test",
		Enabled: true,
		Trigger: TriggerStartup,
		Kind:    KindCommand,
		Command: "echo hi",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if h.Priority != defaultHookPriority {
		t.Errorf("expected default priority %d, got %d", defaultHookPriority, h.Priority)
	}
	if h.Phase != HookPhasePost {
		t.Errorf("expected default phase %s, got %s", HookPhasePost, h.Phase)
	}
}

// ---------------------------------------------------------------------------
// Test async hook execution — Fire returns immediately, result recorded later
// ---------------------------------------------------------------------------

func TestAsyncHookExecution(t *testing.T) {
	// Slow server: takes 200ms to respond.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()
	_, err := store.Add(ctx, Hook{
		Name:    "async-hook",
		Enabled: true,
		Trigger: TriggerRunCompleted,
		Kind:    KindHTTP,
		URL:     srv.URL,
		Async:   true,
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerRunCompleted, HookPhasePost, map[string]any{})

	// Async hooks are not included in the synchronous return value.
	if len(results) != 0 {
		t.Fatalf("expected 0 sync results for async hook, got %d", len(results))
	}

	// Wait for the async hook to finish.
	if err := executor.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	// The result should now appear in RecentResults.
	recent := executor.RecentResults(0)
	if len(recent) != 1 {
		t.Fatalf("expected 1 recent result after async completion, got %d", len(recent))
	}
	if recent[0].Status != resultStatusOK {
		t.Errorf("expected ok status, got %s (error: %s)", recent[0].Status, recent[0].Error)
	}
}

// ---------------------------------------------------------------------------
// Test async flag ignored for pre-phase hooks — executes synchronously
// ---------------------------------------------------------------------------

func TestAsyncHookPrePhaseIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()
	_, err := store.Add(ctx, Hook{
		Name:    "pre-async-hook",
		Enabled: true,
		Trigger: TriggerToolExecuted,
		Kind:    KindHTTP,
		URL:     srv.URL,
		Phase:   HookPhasePre,
		Async:   true, // should be ignored for pre-phase
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerToolExecuted, HookPhasePre, map[string]any{})

	// Pre-phase hooks must always execute synchronously, even if Async=true.
	if len(results) != 1 {
		t.Fatalf("expected 1 sync result for pre-phase hook with async flag, got %d", len(results))
	}
	if results[0].Status != resultStatusOK {
		t.Errorf("expected ok status, got %s (error: %s)", results[0].Status, results[0].Error)
	}
}

// ---------------------------------------------------------------------------
// Test webhook HMAC-SHA256 signature
// ---------------------------------------------------------------------------

func TestWebhookHMACSignature(t *testing.T) {
	const secret = "my-webhook-secret"

	var (
		mu        sync.Mutex
		gotSig    string
		gotTS     string
		gotBody   []byte
		gotHeader http.Header
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotHeader = r.Header.Clone()
		gotSig = r.Header.Get(hmacSignatureHeader)
		gotTS = r.Header.Get(hmacTimestampHeader)
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()
	_, err := store.Add(ctx, Hook{
		Name:    "signed-hook",
		Enabled: true,
		Trigger: TriggerRunCompleted,
		Kind:    KindHTTP,
		URL:     srv.URL,
		Secret:  secret,
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerRunCompleted, HookPhasePost, map[string]any{"event": "test"})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != resultStatusOK {
		t.Fatalf("expected ok status, got %s (error: %s)", results[0].Status, results[0].Error)
	}

	mu.Lock()
	defer mu.Unlock()

	// Verify signature header present and prefixed.
	if gotSig == "" {
		t.Fatal("expected X-HopClaw-Signature header to be present")
	}
	if !strings.HasPrefix(gotSig, "sha256=") {
		t.Fatalf("expected signature to start with 'sha256=', got %s", gotSig)
	}

	// Verify timestamp header present.
	if gotTS == "" {
		t.Fatal("expected X-HopClaw-Timestamp header to be present")
	}

	// Recompute HMAC on the "server side" and verify it matches.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(gotTS))
	mac.Write([]byte("."))
	mac.Write(gotBody)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if gotSig != expectedSig {
		t.Errorf("HMAC mismatch:\n  got:      %s\n  expected: %s", gotSig, expectedSig)
	}

	// Verify the other headers still work.
	if gotHeader.Get("Content-Type") != "application/json" {
		t.Errorf("expected application/json content type, got %s", gotHeader.Get("Content-Type"))
	}
}

// ---------------------------------------------------------------------------
// Test Executor shutdown waits for all async hooks
// ---------------------------------------------------------------------------

func TestExecutorShutdown(t *testing.T) {
	const hookCount = 5
	var (
		mu        sync.Mutex
		completed int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		completed++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()
	for i := range hookCount {
		_, err := store.Add(ctx, Hook{
			Name:    fmt.Sprintf("async-shutdown-%d", i),
			Enabled: true,
			Trigger: TriggerRunCompleted,
			Kind:    KindHTTP,
			URL:     srv.URL,
			Async:   true,
		})
		if err != nil {
			t.Fatalf("add hook %d: %v", i, err)
		}
	}

	executor := NewExecutor(store)
	results := executor.Fire(ctx, TriggerRunCompleted, HookPhasePost, map[string]any{})

	// No sync results for async hooks.
	if len(results) != 0 {
		t.Fatalf("expected 0 sync results, got %d", len(results))
	}

	// Shutdown should block until all async hooks complete.
	if err := executor.Shutdown(); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if completed != hookCount {
		t.Errorf("expected %d hooks to complete after shutdown, got %d", hookCount, completed)
	}
}

// ---------------------------------------------------------------------------
// Test async hook results appear in RecentResults after completion
// ---------------------------------------------------------------------------

func TestAsyncHookRecordsResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewInMemoryStore()
	ctx := context.Background()
	h, err := store.Add(ctx, Hook{
		Name:    "async-record",
		Enabled: true,
		Trigger: TriggerToolExecuted,
		Kind:    KindHTTP,
		URL:     srv.URL,
		Async:   true,
	})
	if err != nil {
		t.Fatalf("add hook: %v", err)
	}

	executor := NewExecutor(store)

	// Verify no results before firing.
	before := executor.RecentResults(0)
	if len(before) != 0 {
		t.Fatalf("expected 0 results before firing, got %d", len(before))
	}

	executor.Fire(ctx, TriggerToolExecuted, HookPhasePost, map[string]any{})

	// Wait for async hook to complete.
	if err := executor.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	// Verify the result was recorded.
	after := executor.RecentResults(0)
	if len(after) != 1 {
		t.Fatalf("expected 1 result after async completion, got %d", len(after))
	}
	if after[0].HookID != h.ID {
		t.Errorf("expected hook id %s, got %s", h.ID, after[0].HookID)
	}
	if after[0].Status != resultStatusOK {
		t.Errorf("expected ok status, got %s (error: %s)", after[0].Status, after[0].Error)
	}
	if after[0].Trigger != TriggerToolExecuted {
		t.Errorf("expected trigger %s, got %s", TriggerToolExecuted, after[0].Trigger)
	}
}
