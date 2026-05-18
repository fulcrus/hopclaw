package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/durablefact"
)

func TestGovernedMemoryStoreUpsertMergesPreviousValues(t *testing.T) {
	ctx := context.Background()
	store := NewGovernedMemoryStore(NewInMemoryKVStore())
	entry, err := store.UpsertRecord(ctx, MemoryRecord{
		Namespace: memoryNamespaceProfile,
		ScopeKey:  "user",
		Field:     "name",
		Label:     "User Name",
		Value:     "Alice",
		Source:    "test",
		Score:     0.9,
		Evidence: []MemoryRecordEvidence{{
			Source:  "test",
			Summary: "first",
			Value:   "Alice",
		}},
	})
	if err != nil {
		t.Fatalf("UpsertRecord(first) error = %v", err)
	}
	if entry.Namespace != memoryNamespaceProfile || entry.ScopeKey != "user" || entry.Field != "name" {
		t.Fatalf("unexpected entry metadata: %#v", entry)
	}
	entry, err = store.UpsertRecord(ctx, MemoryRecord{
		Key:       entry.Key,
		Namespace: memoryNamespaceProfile,
		ScopeKey:  "user",
		Field:     "name",
		Label:     "User Name",
		Value:     "Bob",
		Source:    "test",
		Score:     0.95,
		Evidence: []MemoryRecordEvidence{{
			Source:  "test",
			Summary: "second",
			Value:   "Bob",
		}},
	})
	if err != nil {
		t.Fatalf("UpsertRecord(second) error = %v", err)
	}
	if entry.Value != "Bob" {
		t.Fatalf("entry.Value = %q", entry.Value)
	}
	if len(entry.PreviousValues) == 0 || entry.PreviousValues[0] != "Alice" {
		t.Fatalf("entry.PreviousValues = %#v", entry.PreviousValues)
	}
	if entry.EvidenceCount != 2 {
		t.Fatalf("entry.EvidenceCount = %d", entry.EvidenceCount)
	}
}

func TestGovernedMemoryStoreUpsertPreservesExtendedFields(t *testing.T) {
	ctx := context.Background()
	store := NewGovernedMemoryStore(NewInMemoryKVStore())
	lastUsedAt := time.Now().UTC().Truncate(time.Second)

	entry, err := store.UpsertRecord(ctx, MemoryRecord{
		Namespace:       memoryNamespaceProject,
		ScopeKey:        "repo",
		Field:           "status",
		Label:           "Project Status",
		Value:           "stable",
		Source:          MemorySourceAgent,
		Score:           0.84,
		State:           MemorySuperseded,
		SupersededBy:    "project.repo.status_v2",
		SessionKey:      "sess-hopclaw",
		ProjectID:       "github.com/fulcrus/hopclaw",
		MediaRefs:       []string{"artifact://local/screenshot-1", "artifact://local/screenshot-1"},
		UsedCount:       3,
		LastUsedAt:      lastUsedAt,
		CorrectionCount: 1,
	})
	if err != nil {
		t.Fatalf("UpsertRecord() error = %v", err)
	}

	if entry.Score != 0.84 {
		t.Fatalf("entry.Score = %v, want 0.84", entry.Score)
	}
	if entry.State != MemorySuperseded {
		t.Fatalf("entry.State = %q, want %q", entry.State, MemorySuperseded)
	}
	if entry.SupersededBy != "project.repo.status_v2" {
		t.Fatalf("entry.SupersededBy = %q", entry.SupersededBy)
	}
	if len(entry.MediaRefs) != 1 || entry.MediaRefs[0] != "artifact://local/screenshot-1" {
		t.Fatalf("entry.MediaRefs = %#v", entry.MediaRefs)
	}
	if entry.UsedCount != 3 {
		t.Fatalf("entry.UsedCount = %d", entry.UsedCount)
	}
	if !entry.LastUsedAt.Equal(lastUsedAt) {
		t.Fatalf("entry.LastUsedAt = %v, want %v", entry.LastUsedAt, lastUsedAt)
	}
	if entry.CorrectionCount != 1 {
		t.Fatalf("entry.CorrectionCount = %d", entry.CorrectionCount)
	}
	if entry.SessionKey != "sess-hopclaw" || entry.ProjectID != "github.com/fulcrus/hopclaw" {
		t.Fatalf("entry session/project = %q/%q", entry.SessionKey, entry.ProjectID)
	}
}

func TestGovernedMemoryStoreTouchMemoryVerificationPreservesRecord(t *testing.T) {
	ctx := context.Background()
	store := NewGovernedMemoryStore(NewInMemoryKVStore())

	entry, err := store.UpsertRecord(ctx, MemoryRecord{
		Namespace:             memoryNamespaceProject,
		ScopeKey:              "repo",
		Field:                 "deploy_target",
		Label:                 "Deploy Target",
		Value:                 "staging",
		Source:                MemorySourceAgent,
		Tags:                  []string{"deploy", "staging"},
		UsedCount:             2,
		VerificationPassCount: 1,
		Evidence: []MemoryRecordEvidence{{
			Source:  "runtime",
			Summary: "used staging in the last deployment",
			Value:   "staging",
		}},
	})
	if err != nil {
		t.Fatalf("UpsertRecord() error = %v", err)
	}

	updated, err := store.TouchMemoryVerification(ctx, entry.Key, false)
	if err != nil {
		t.Fatalf("TouchMemoryVerification() error = %v", err)
	}
	if updated.UsedCount != 3 {
		t.Fatalf("updated.UsedCount = %d, want 3", updated.UsedCount)
	}
	if updated.VerificationPassCount != 1 || updated.VerificationFailCount != 1 {
		t.Fatalf("verification counts = %d/%d, want 1/1", updated.VerificationPassCount, updated.VerificationFailCount)
	}
	if updated.EvidenceCount != 1 {
		t.Fatalf("updated.EvidenceCount = %d, want 1", updated.EvidenceCount)
	}
	if updated.State != MemoryActive {
		t.Fatalf("updated.State = %q, want %q", updated.State, MemoryActive)
	}
	if updated.LastUsedAt.IsZero() {
		t.Fatal("expected LastUsedAt to be updated")
	}
}

func TestGovernedMemoryStoreListFiltered(t *testing.T) {
	ctx := context.Background()
	store := NewGovernedMemoryStore(NewInMemoryKVStore())
	if _, err := store.UpsertRecord(ctx, MemoryRecord{
		Namespace: memoryNamespaceProfile,
		ScopeKey:  "user",
		Field:     "reply_language",
		Value:     "zh-CN",
	}); err != nil {
		t.Fatalf("UpsertRecord(profile) error = %v", err)
	}
	if _, err := store.UpsertRecord(ctx, MemoryRecord{
		Namespace: memoryNamespaceProject,
		ScopeKey:  "repo",
		Field:     "name",
		Value:     "HopClaw",
	}); err != nil {
		t.Fatalf("UpsertRecord(project) error = %v", err)
	}
	results, err := store.ListFiltered(ctx, MemoryFilter{Namespace: memoryNamespaceProfile, ManagedOnly: true})
	if err != nil {
		t.Fatalf("ListFiltered() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	if results[0].Namespace != memoryNamespaceProfile || !strings.Contains(results[0].Key, "profile") {
		t.Fatalf("unexpected result = %#v", results[0])
	}
}

func TestGovernedMemoryStoreDelegatesDurableViews(t *testing.T) {
	ctx := context.Background()
	store := NewGovernedMemoryStore(newTestSQLiteStore(t))
	if _, err := store.UpsertRecord(ctx, MemoryRecord{
		Namespace: memoryNamespaceProfile,
		ScopeKey:  "user",
		Field:     "timezone",
		Value:     "Asia/Shanghai",
	}); err != nil {
		t.Fatalf("UpsertRecord() error = %v", err)
	}

	contextViews, err := store.ListContextViews(ctx, durablefact.Filter{Namespace: memoryNamespaceProfile})
	if err != nil {
		t.Fatalf("ListContextViews() error = %v", err)
	}
	if len(contextViews) != 1 || contextViews[0].Field != "timezone" {
		t.Fatalf("unexpected context views: %#v", contextViews)
	}

	operatorViews, err := store.ListOperatorViews(ctx, durablefact.Filter{Namespace: memoryNamespaceProfile})
	if err != nil {
		t.Fatalf("ListOperatorViews() error = %v", err)
	}
	if len(operatorViews) != 1 || operatorViews[0].ViewType != durablefact.ViewTypeContext {
		t.Fatalf("unexpected operator views: %#v", operatorViews)
	}
}
