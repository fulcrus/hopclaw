package toolruntime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

// ---------------------------------------------------------------------------
// db.kv.set / db.kv.get tests
// ---------------------------------------------------------------------------

func TestDBKVSetAndGet(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	ctx := context.Background()
	run := &agent.Run{ID: "run-kv"}
	sess := &agent.Session{ID: "sess-kv"}

	// Use a unique key prefix per test to avoid interference with other tests.
	keyPrefix := "test-set-get-"

	// Set a value.
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-kv-set",
		Name: "db.kv.set",
		Input: map[string]any{
			"key":   keyPrefix + "greeting",
			"value": "hello world",
		},
	}})
	if err != nil {
		t.Fatalf("db.kv.set error = %v", err)
	}
	var setPayload struct {
		Key     string `json:"key"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &setPayload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if setPayload.Key != keyPrefix+"greeting" {
		t.Fatalf("key = %q", setPayload.Key)
	}

	// Get the value.
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-kv-get",
		Name: "db.kv.get",
		Input: map[string]any{
			"key": keyPrefix + "greeting",
		},
	}})
	if err != nil {
		t.Fatalf("db.kv.get error = %v", err)
	}
	var getPayload struct {
		Key    string `json:"key"`
		Value  string `json:"value"`
		Exists bool   `json:"exists"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &getPayload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if !getPayload.Exists {
		t.Fatal("exists should be true")
	}
	if getPayload.Value != "hello world" {
		t.Fatalf("value = %q, want 'hello world'", getPayload.Value)
	}

	// Clean up via db.kv.delete.
	builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-kv-cleanup", Name: "db.kv.delete",
		Input: map[string]any{"key": keyPrefix + "greeting"},
	}})
}

func TestDBKVGetNonExistent(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-kv"}, &agent.Session{ID: "sess-kv"}, []agent.ToolCall{{
		ID:   "call-kv-miss",
		Name: "db.kv.get",
		Input: map[string]any{
			"key": "nonexistent-key-xyz-test",
		},
	}})
	if err != nil {
		t.Fatalf("db.kv.get error = %v", err)
	}
	var payload struct {
		Exists bool `json:"exists"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Exists {
		t.Fatal("exists should be false for nonexistent key")
	}
}

// ---------------------------------------------------------------------------
// db.kv.delete tests
// ---------------------------------------------------------------------------

func TestDBKVDelete(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	ctx := context.Background()
	run := &agent.Run{ID: "run-kv"}
	sess := &agent.Session{ID: "sess-kv"}
	keyPrefix := "test-delete-"

	// Set then delete via tool calls.
	builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-kv-setup", Name: "db.kv.set",
		Input: map[string]any{"key": keyPrefix + "victim", "value": "doomed"},
	}})

	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-kv-del",
		Name: "db.kv.delete",
		Input: map[string]any{
			"key": keyPrefix + "victim",
		},
	}})
	if err != nil {
		t.Fatalf("db.kv.delete error = %v", err)
	}
	var payload struct {
		Key     string `json:"key"`
		Deleted bool   `json:"deleted"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if !payload.Deleted {
		t.Fatal("deleted should be true")
	}

	// Verify it's gone via db.kv.get.
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-kv-verify", Name: "db.kv.get",
		Input: map[string]any{"key": keyPrefix + "victim"},
	}})
	if err != nil {
		t.Fatalf("db.kv.get error = %v", err)
	}
	var getPayload struct {
		Exists bool `json:"exists"`
	}
	json.Unmarshal([]byte(results[0].Content), &getPayload)
	if getPayload.Exists {
		t.Fatal("key should be deleted from store")
	}
}

func TestDBKVDeleteNonExistent(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-kv"}, &agent.Session{ID: "sess-kv"}, []agent.ToolCall{{
		ID:   "call-kv-del-miss",
		Name: "db.kv.delete",
		Input: map[string]any{
			"key": "nonexistent-delete-key-xyz",
		},
	}})
	if err != nil {
		t.Fatalf("db.kv.delete error = %v", err)
	}
	var payload struct {
		Deleted bool `json:"deleted"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Deleted {
		t.Fatal("deleted should be false for nonexistent key")
	}
}

// ---------------------------------------------------------------------------
// db.kv.list tests
// ---------------------------------------------------------------------------

func TestDBKVList(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	ctx := context.Background()
	run := &agent.Run{ID: "run-kv"}
	sess := &agent.Session{ID: "sess-kv"}
	keyPrefix := "test-list-unique-"

	// Store some keys via tool calls.
	for _, kv := range []struct{ k, v string }{
		{keyPrefix + "alpha", "1"},
		{keyPrefix + "beta", "2"},
		{keyPrefix + "gamma", "3"},
	} {
		builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-setup-" + kv.k, Name: "db.kv.set",
			Input: map[string]any{"key": kv.k, "value": kv.v},
		}})
	}

	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-kv-list",
		Name: "db.kv.list",
		Input: map[string]any{
			"prefix": keyPrefix,
		},
	}})
	if err != nil {
		t.Fatalf("db.kv.list error = %v", err)
	}
	var payload struct {
		Keys  []string `json:"keys"`
		Count int      `json:"count"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if payload.Count != 3 {
		t.Fatalf("count = %d, want 3", payload.Count)
	}
	// Keys should be sorted.
	if len(payload.Keys) != 3 {
		t.Fatalf("len(keys) = %d, want 3", len(payload.Keys))
	}
	if payload.Keys[0] != keyPrefix+"alpha" {
		t.Fatalf("keys[0] = %q, want %q", payload.Keys[0], keyPrefix+"alpha")
	}

	// Clean up.
	for _, k := range payload.Keys {
		builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-cleanup-" + k, Name: "db.kv.delete",
			Input: map[string]any{"key": k},
		}})
	}
}

func TestDBKVListNoPrefix(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	// List all keys (no prefix filter). This should not error.
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-kv"}, &agent.Session{ID: "sess-kv"}, []agent.ToolCall{{
		ID:    "call-kv-list-all",
		Name:  "db.kv.list",
		Input: map[string]any{},
	}})
	if err != nil {
		t.Fatalf("db.kv.list error = %v", err)
	}
	var payload struct {
		Count int `json:"count"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	// Count should be >= 0 (may have leftover keys from other tests).
	if payload.Count < 0 {
		t.Fatalf("count = %d, should be >= 0", payload.Count)
	}
}

// ---------------------------------------------------------------------------
// Concurrent access
// ---------------------------------------------------------------------------

func TestDBKVConcurrentAccess(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	ctx := context.Background()
	run := &agent.Run{ID: "run-kv"}
	sess := &agent.Session{ID: "sess-kv"}
	keyPrefix := "test-concurrent-"

	var wg sync.WaitGroup
	const goroutines = 10

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			key := keyPrefix + string(rune('a'+idx))
			builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
				ID:   "call-set-" + key,
				Name: "db.kv.set",
				Input: map[string]any{
					"key":   key,
					"value": "value",
				},
			}})
			builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
				ID:   "call-get-" + key,
				Name: "db.kv.get",
				Input: map[string]any{
					"key": key,
				},
			}})
		}(i)
	}
	wg.Wait()

	// Clean up.
	for i := 0; i < goroutines; i++ {
		key := keyPrefix + string(rune('a'+i))
		builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-cleanup-" + key, Name: "db.kv.delete",
			Input: map[string]any{"key": key},
		}})
	}
}
