package agent

import (
	"context"
	"testing"
	"time"
)

func TestMemoryLifecycle_SetRecallDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewGovernedMemoryStore(NewInMemoryKVStore())

	if err := store.Set(ctx, "deploy_server", "10.0.0.1"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	recalled := RecallForContext(entries, "", "")
	if len(recalled.Memories) != 1 {
		t.Fatalf("len(recalled.Memories) = %d, want 1", len(recalled.Memories))
	}
	if recalled.Memories[0].Key != "deploy_server" {
		t.Fatalf("recalled.Memories[0].Key = %q, want %q", recalled.Memories[0].Key, "deploy_server")
	}

	if err := store.Delete(ctx, "deploy_server"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	entries, err = store.List(ctx)
	if err != nil {
		t.Fatalf("List() after delete error = %v", err)
	}
	recalled = RecallForContext(entries, "", "")
	if len(recalled.Memories) != 0 {
		t.Fatalf("len(recalled.Memories) = %d, want 0", len(recalled.Memories))
	}
}

func TestMemoryGovernance_AgentCannotOverwriteUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewGovernedMemoryStore(NewInMemoryKVStore())

	if err := store.Set(ctx, "server", "1.2.3.4"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	entry, result, err := store.AgentUpsert(ctx, MemoryRecord{
		Key:   "server",
		Value: "5.6.7.8",
	})
	if err != nil {
		t.Fatalf("AgentUpsert() error = %v", err)
	}
	if entry != nil {
		t.Fatalf("entry = %#v, want nil when mutation is blocked", entry)
	}
	if result.Action != MutationBlocked {
		t.Fatalf("result.Action = %q, want %q", result.Action, MutationBlocked)
	}

	current, err := store.Get(ctx, "server")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if current == nil || current.Value != "1.2.3.4" {
		t.Fatalf("current = %#v, want original user value", current)
	}
}

func TestMemoryGovernance_UserCorrectSupersedes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewGovernedMemoryStore(NewInMemoryKVStore())

	initial, result, err := store.AgentUpsert(ctx, MemoryRecord{
		Key:   "deploy_server",
		Value: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("AgentUpsert() error = %v", err)
	}
	if result.Action != MutationApplied {
		t.Fatalf("result.Action = %q, want %q", result.Action, MutationApplied)
	}
	if initial == nil {
		t.Fatal("expected initial agent memory entry")
	}

	corrected, err := store.UserCorrect(ctx, "deploy_server", "5.6.7.8")
	if err != nil {
		t.Fatalf("UserCorrect() error = %v", err)
	}
	if corrected.Value != "5.6.7.8" {
		t.Fatalf("corrected.Value = %q, want %q", corrected.Value, "5.6.7.8")
	}
	if corrected.Source != MemorySourceUser {
		t.Fatalf("corrected.Source = %q, want %q", corrected.Source, MemorySourceUser)
	}
	if len(corrected.PreviousValues) == 0 || corrected.PreviousValues[0] != "1.2.3.4" {
		t.Fatalf("corrected.PreviousValues = %#v, want old agent value tracked", corrected.PreviousValues)
	}
	if corrected.CorrectionCount != 1 {
		t.Fatalf("corrected.CorrectionCount = %d, want 1", corrected.CorrectionCount)
	}
}

func TestMemoryRecall_SortsBySourceThenScore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewGovernedMemoryStore(NewInMemoryKVStore())

	if err := store.Set(ctx, "user_pref", "always ask before deploy"); err != nil {
		t.Fatalf("Set(user_pref) error = %v", err)
	}
	if _, err := store.UpsertRecord(ctx, MemoryRecord{
		Key:        "agent_high",
		Value:      "staging deploy target",
		Source:     MemorySourceAgent,
		Score:      0.9,
		LastUsedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertRecord(agent_high) error = %v", err)
	}
	if _, err := store.UpsertRecord(ctx, MemoryRecord{
		Key:        "agent_low",
		Value:      "legacy deploy notes",
		Source:     MemorySourceAgent,
		Score:      0.4,
		LastUsedAt: time.Now().UTC().Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("UpsertRecord(agent_low) error = %v", err)
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	recalled := RecallForContext(entries, "", "")
	if len(recalled.Memories) != 3 {
		t.Fatalf("len(recalled.Memories) = %d, want 3", len(recalled.Memories))
	}
	if recalled.Memories[0].Key != "user_pref" {
		t.Fatalf("recalled.Memories[0].Key = %q, want %q", recalled.Memories[0].Key, "user_pref")
	}
	if recalled.Memories[1].Key != "agent_high" {
		t.Fatalf("recalled.Memories[1].Key = %q, want %q", recalled.Memories[1].Key, "agent_high")
	}
	if recalled.Memories[2].Key != "agent_low" {
		t.Fatalf("recalled.Memories[2].Key = %q, want %q", recalled.Memories[2].Key, "agent_low")
	}
}

func TestMemoryScoring_RecencyDecay(t *testing.T) {
	t.Parallel()

	fresh := ComputeScore(MemoryEntry{
		Source:        MemorySourceAgent,
		EvidenceCount: 3,
		LastUsedAt:    time.Now().UTC(),
	})
	stale := ComputeScore(MemoryEntry{
		Source:        MemorySourceAgent,
		EvidenceCount: 3,
		LastUsedAt:    time.Now().UTC().Add(-180 * 24 * time.Hour),
	})

	if fresh <= stale {
		t.Fatalf("fresh score = %f, stale score = %f, want fresh > stale", fresh, stale)
	}
}

func TestMemoryPromptInjection_MaxFacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewGovernedMemoryStore(NewInMemoryKVStore())

	for i := 0; i < 25; i++ {
		key := "memory_key_" + string(rune('a'+i))
		if err := store.Set(ctx, key, "value"); err != nil {
			t.Fatalf("Set(%q) error = %v", key, err)
		}
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	recalled := RecallForContext(entries, "", "")
	if len(recalled.Memories) != 20 {
		t.Fatalf("len(recalled.Memories) = %d, want 20", len(recalled.Memories))
	}

	facts := InjectMemoryFacts(recalled.Memories)
	if len(facts) != 21 {
		t.Fatalf("len(facts) = %d, want 21 (guide + 20 memories)", len(facts))
	}
	if facts[0].Key != "_memory_guide" {
		t.Fatalf("facts[0].Key = %q, want %q", facts[0].Key, "_memory_guide")
	}
}
