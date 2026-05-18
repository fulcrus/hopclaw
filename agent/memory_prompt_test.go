package agent

import (
	"strings"
	"testing"
)

func TestInjectMemoryFacts(t *testing.T) {
	t.Parallel()

	memories := []MemoryEntry{
		{
			Key:    "server",
			Value:  "198.51.100.42",
			Source: MemorySourceUser,
			Label:  "Deploy Server",
		},
		{
			Key:    "db_port",
			Value:  "5432",
			Source: MemorySourceAgent,
		},
	}

	facts := InjectMemoryFacts(memories)
	if len(facts) != 3 {
		t.Fatalf("len(facts) = %d, want 3", len(facts))
	}
	if facts[0].Key != "_memory_guide" {
		t.Fatalf("facts[0].Key = %q, want _memory_guide", facts[0].Key)
	}
	if !strings.Contains(facts[0].Content, "Use the available memory tools") {
		t.Fatalf("guide content = %q", facts[0].Content)
	}
	if strings.Contains(facts[0].Content, "Use memory.set to save useful information.") {
		t.Fatalf("guide content should not require specific memory tool names: %q", facts[0].Content)
	}
	foundProtectedUserFact := false
	for _, fact := range facts {
		if fact.Key == "memory:server" && strings.Contains(fact.Content, "DO NOT overwrite") {
			foundProtectedUserFact = true
		}
	}
	if !foundProtectedUserFact {
		t.Fatal("expected user memory to be marked as non-overwritable")
	}
}

func TestInjectMemoryFactsEmpty(t *testing.T) {
	t.Parallel()

	if facts := InjectMemoryFacts(nil); facts != nil {
		t.Fatalf("InjectMemoryFacts(nil) = %#v, want nil", facts)
	}
}

func TestInjectMemoryRecallFactsIncludesConflictHints(t *testing.T) {
	t.Parallel()

	facts := InjectMemoryRecallFacts(RecallResult{
		Memories: []MemoryEntry{{
			Key:    "deploy_server",
			Value:  "10.0.0.1",
			Source: MemorySourceAgent,
		}},
		Conflicts: []MemoryConflict{{
			Kind: ConflictMemoryVsMemory,
			EntryA: MemoryEntry{
				Key:   "deploy_server",
				Field: "host",
				Value: "10.0.0.1",
			},
			EntryB: &MemoryEntry{
				Key:   "deploy_server",
				Field: "host",
				Value: "10.0.0.2",
			},
			Message: "conflicting values for similar fields: host=10.0.0.1 vs host=10.0.0.2",
		}},
	})

	if len(facts) < 4 {
		t.Fatalf("len(facts) = %d, want at least 4", len(facts))
	}
	foundConflictGuide := false
	foundConflictDetail := false
	for _, fact := range facts {
		if fact.Key == "_memory_conflicts" && strings.Contains(fact.Content, "Potential memory conflicts") {
			foundConflictGuide = true
		}
		if strings.HasPrefix(fact.Key, "memory_conflict:") && strings.Contains(fact.Content, "Potential memory conflict:") {
			foundConflictDetail = true
		}
	}
	if !foundConflictGuide {
		t.Fatal("expected conflict guide fact")
	}
	if !foundConflictDetail {
		t.Fatal("expected conflict detail fact")
	}
}
