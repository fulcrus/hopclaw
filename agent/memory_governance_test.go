package agent

import (
	"context"
	"testing"
)

func TestAgentUpsertBlocksUserMemory(t *testing.T) {
	store := newInMemoryKVStore()
	governed := NewGovernedMemoryStore(store)

	if err := governed.Set(context.Background(), "server", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}

	_, result, err := governed.AgentUpsert(context.Background(), MemoryRecord{
		Key:   "server",
		Value: "5.6.7.8",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != MutationBlocked {
		t.Fatalf("expected blocked, got %s", result.Action)
	}

	entry, err := governed.Get(context.Background(), "server")
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil {
		t.Fatal("expected entry")
	}
	if entry.Value != "1.2.3.4" {
		t.Fatalf("expected original value, got %s", entry.Value)
	}
}

func TestUserCorrectSupersedes(t *testing.T) {
	store := newInMemoryKVStore()
	governed := NewGovernedMemoryStore(store)

	if err := governed.Set(context.Background(), "server", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}

	entry, err := governed.UserCorrect(context.Background(), "server", "5.6.7.8")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Value != "5.6.7.8" {
		t.Fatalf("expected new value, got %s", entry.Value)
	}
	if len(entry.PreviousValues) == 0 || entry.PreviousValues[0] != "1.2.3.4" {
		t.Fatalf("expected PreviousValues to contain old value, got %#v", entry.PreviousValues)
	}
	if entry.CorrectionCount != 1 {
		t.Fatalf("expected CorrectionCount 1, got %d", entry.CorrectionCount)
	}
}

func newInMemoryKVStore() *InMemoryKVStore {
	return NewInMemoryKVStore()
}
