package hooks

import (
	"context"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// NewInMemoryStore
// ---------------------------------------------------------------------------

func TestNewInMemoryStore(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	hooks, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(hooks) != 0 {
		t.Fatalf("expected empty store, got %d hooks", len(hooks))
	}
}

// ---------------------------------------------------------------------------
// Add
// ---------------------------------------------------------------------------

func TestStoreAdd_AssignsID(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	h, err := store.Add(ctx, Hook{
		Name:    "test-hook",
		Enabled: true,
		Trigger: TriggerStartup,
		Kind:    KindCommand,
		Command: "echo hi",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if h.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestStoreAdd_AssignsDefaults(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	h, err := store.Add(ctx, Hook{
		Name:    "defaults-test",
		Enabled: true,
		Trigger: TriggerStartup,
		Kind:    KindCommand,
		Command: "true",
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
	if h.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestStoreAdd_PreservesExplicitPriority(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	h, err := store.Add(ctx, Hook{
		Name:     "priority-test",
		Enabled:  true,
		Trigger:  TriggerStartup,
		Kind:     KindCommand,
		Command:  "true",
		Priority: 42,
		Phase:    HookPhasePre,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// Priority 42 is non-zero, so Add keeps it.
	if h.Priority != 42 {
		t.Errorf("expected priority 42, got %d", h.Priority)
	}
	if h.Phase != HookPhasePre {
		t.Errorf("expected phase pre, got %s", h.Phase)
	}
}

func TestStoreAdd_ReturnsCopy(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	h, err := store.Add(ctx, Hook{
		Name:    "copy-test",
		Enabled: true,
		Trigger: TriggerStartup,
		Kind:    KindCommand,
		Command: "true",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// Mutating the returned hook should not affect the store.
	h.Name = "mutated"
	stored, err := store.Get(ctx, h.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Name == "mutated" {
		t.Error("Add returned a reference instead of a copy")
	}
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestStoreGet_Exists(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	added, err := store.Add(ctx, Hook{
		Name:    "get-test",
		Enabled: true,
		Trigger: TriggerRunCompleted,
		Kind:    KindHTTP,
		URL:     "http://example.com",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	got, err := store.Get(ctx, added.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "get-test" {
		t.Errorf("expected name get-test, got %s", got.Name)
	}
}

func TestStoreGet_NotFound(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent hook")
	}
}

func TestStoreGet_ReturnsCopy(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	added, _ := store.Add(ctx, Hook{
		Name:    "original",
		Enabled: true,
		Trigger: TriggerStartup,
		Kind:    KindCommand,
		Command: "true",
	})

	got, _ := store.Get(ctx, added.ID)
	got.Name = "mutated"

	got2, _ := store.Get(ctx, added.ID)
	if got2.Name != "original" {
		t.Error("Get returned a reference instead of a copy")
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestStoreUpdate(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	added, _ := store.Add(ctx, Hook{
		Name:    "update-test",
		Enabled: true,
		Trigger: TriggerStartup,
		Kind:    KindCommand,
		Command: "true",
	})

	updated := *added
	updated.Name = "updated-name"
	updated.Enabled = false

	result, err := store.Update(ctx, updated)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if result.Name != "updated-name" {
		t.Errorf("expected name updated-name, got %s", result.Name)
	}
	if result.Enabled {
		t.Error("expected disabled hook")
	}

	// Verify persisted in store.
	got, _ := store.Get(ctx, added.ID)
	if got.Name != "updated-name" {
		t.Errorf("expected stored name updated-name, got %s", got.Name)
	}
}

func TestStoreUpdate_NotFound(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	_, err := store.Update(ctx, Hook{ID: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent hook")
	}
}

// ---------------------------------------------------------------------------
// Remove
// ---------------------------------------------------------------------------

func TestStoreRemove(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	added, _ := store.Add(ctx, Hook{
		Name:    "remove-test",
		Enabled: true,
		Trigger: TriggerStartup,
		Kind:    KindCommand,
		Command: "true",
	})

	if err := store.Remove(ctx, added.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}

	_, err := store.Get(ctx, added.ID)
	if err == nil {
		t.Fatal("expected error after remove")
	}
}

func TestStoreRemove_NotFound(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	err := store.Remove(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent hook")
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestStoreList_ReturnsAll(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, _ = store.Add(ctx, Hook{
			Name:    "list-test",
			Enabled: true,
			Trigger: TriggerStartup,
			Kind:    KindCommand,
			Command: "true",
		})
	}

	hooks, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(hooks) != 3 {
		t.Fatalf("expected 3 hooks, got %d", len(hooks))
	}
}

func TestStoreList_ReturnsCopies(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	added, _ := store.Add(ctx, Hook{
		Name:    "original",
		Enabled: true,
		Trigger: TriggerStartup,
		Kind:    KindCommand,
		Command: "true",
	})

	hooks, _ := store.List(ctx)
	hooks[0].Name = "mutated"

	got, _ := store.Get(ctx, added.ID)
	if got.Name != "original" {
		t.Error("List returned references instead of copies")
	}
}

// ---------------------------------------------------------------------------
// ListByTrigger
// ---------------------------------------------------------------------------

func TestStoreListByTrigger_DefaultPhase(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	_, _ = store.Add(ctx, Hook{
		Name:    "startup-hook",
		Enabled: true,
		Trigger: TriggerStartup,
		Kind:    KindCommand,
		Command: "true",
	})
	_, _ = store.Add(ctx, Hook{
		Name:    "shutdown-hook",
		Enabled: true,
		Trigger: TriggerShutdown,
		Kind:    KindCommand,
		Command: "true",
	})

	// Empty phase should default to post.
	matched := store.ListByTrigger(TriggerStartup, "")
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Trigger != TriggerStartup {
		t.Errorf("expected trigger %s, got %s", TriggerStartup, matched[0].Trigger)
	}
}

func TestStoreListByTrigger_SkipsDisabled(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	_, _ = store.Add(ctx, Hook{
		Name:    "disabled-hook",
		Enabled: false,
		Trigger: TriggerStartup,
		Kind:    KindCommand,
		Command: "true",
	})

	matched := store.ListByTrigger(TriggerStartup, HookPhasePost)
	if len(matched) != 0 {
		t.Fatalf("expected 0 matches for disabled hook, got %d", len(matched))
	}
}

func TestStoreListByTrigger_SortsByPriority(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	_, _ = store.Add(ctx, Hook{
		Name:     "low-priority",
		Enabled:  true,
		Trigger:  TriggerRunCompleted,
		Kind:     KindHTTP,
		URL:      "http://example.com",
		Priority: 200,
	})
	_, _ = store.Add(ctx, Hook{
		Name:     "high-priority",
		Enabled:  true,
		Trigger:  TriggerRunCompleted,
		Kind:     KindHTTP,
		URL:      "http://example.com",
		Priority: 10,
	})

	matched := store.ListByTrigger(TriggerRunCompleted, HookPhasePost)
	if len(matched) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matched))
	}
	if matched[0].Priority > matched[1].Priority {
		t.Errorf("expected ascending priority order, got %d, %d",
			matched[0].Priority, matched[1].Priority)
	}
}

// ---------------------------------------------------------------------------
// Concurrent access
// ---------------------------------------------------------------------------

func TestStoreConcurrentAccess(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ctx := context.Background()

	var wg sync.WaitGroup
	const goroutines = 20

	// Seed one hook.
	added, _ := store.Add(ctx, Hook{
		Name:    "seed",
		Enabled: true,
		Trigger: TriggerStartup,
		Kind:    KindCommand,
		Command: "true",
	})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = store.Add(ctx, Hook{
				Name:    "concurrent",
				Enabled: true,
				Trigger: TriggerStartup,
				Kind:    KindCommand,
				Command: "true",
			})
			_, _ = store.Get(ctx, added.ID)
			_, _ = store.List(ctx)
			_ = store.ListByTrigger(TriggerStartup, HookPhasePost)
		}()
	}
	wg.Wait()

	hooks, _ := store.List(ctx)
	if len(hooks) != goroutines+1 {
		t.Errorf("expected %d hooks, got %d", goroutines+1, len(hooks))
	}
}

// ---------------------------------------------------------------------------
// Hook type methods
// ---------------------------------------------------------------------------

func TestHook_EffectivePriority(t *testing.T) {
	t.Parallel()

	h := Hook{Priority: 0}
	if h.EffectivePriority() != defaultHookPriority {
		t.Errorf("expected %d for zero priority, got %d", defaultHookPriority, h.EffectivePriority())
	}

	h.Priority = 42
	if h.EffectivePriority() != 42 {
		t.Errorf("expected 42, got %d", h.EffectivePriority())
	}
}

func TestHook_EffectivePhase(t *testing.T) {
	t.Parallel()

	h := Hook{Phase: ""}
	if h.EffectivePhase() != HookPhasePost {
		t.Errorf("expected %s for empty phase, got %s", HookPhasePost, h.EffectivePhase())
	}

	h.Phase = HookPhasePre
	if h.EffectivePhase() != HookPhasePre {
		t.Errorf("expected %s, got %s", HookPhasePre, h.EffectivePhase())
	}
}
