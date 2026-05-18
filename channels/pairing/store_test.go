package pairing

import (
	"testing"
	"time"
)

func TestInMemoryStoreGetNotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	_, err := store.Get("slack", "user-1")
	if err == nil {
		t.Fatal("expected error for nonexistent record")
	}
}

func TestInMemoryStoreSaveAndGet(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	rec := &PairingRecord{
		ID:          "pair-1",
		Channel:     "telegram",
		UserID:      "user-42",
		DisplayName: "Alice",
		Status:      StatusPending,
		Code:        "ABC123",
		CreatedAt:   time.Now().UTC(),
	}

	if err := store.Save(rec); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := store.Get("telegram", "user-42")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got.ID != "pair-1" {
		t.Fatalf("got.ID = %q, want %q", got.ID, "pair-1")
	}
	if got.DisplayName != "Alice" {
		t.Fatalf("got.DisplayName = %q, want %q", got.DisplayName, "Alice")
	}
}

func TestInMemoryStoreSaveReturnsErrorForNil(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	err := store.Save(nil)
	if err == nil {
		t.Fatal("expected error for nil record")
	}
}

func TestInMemoryStoreGetByCode(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	rec := &PairingRecord{
		ID:        "pair-2",
		Channel:   "slack",
		UserID:    "user-99",
		Status:    StatusPending,
		Code:      "XYZ789",
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Save(rec); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := store.GetByCode("XYZ789")
	if err != nil {
		t.Fatalf("GetByCode error: %v", err)
	}
	if got.UserID != "user-99" {
		t.Fatalf("got.UserID = %q, want %q", got.UserID, "user-99")
	}

	_, err = store.GetByCode("NONEXISTENT")
	if err == nil {
		t.Fatal("expected error for nonexistent code")
	}
}

func TestInMemoryStoreList(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()

	list, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("empty store should return 0 records, got %d", len(list))
	}

	_ = store.Save(&PairingRecord{Channel: "slack", UserID: "u1", CreatedAt: time.Now()})
	_ = store.Save(&PairingRecord{Channel: "telegram", UserID: "u2", CreatedAt: time.Now()})

	list, err = store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List() returned %d records, want 2", len(list))
	}
}

func TestInMemoryStoreDelete(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	_ = store.Save(&PairingRecord{Channel: "slack", UserID: "u1", CreatedAt: time.Now()})

	if err := store.Delete("slack", "u1"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	_, err := store.Get("slack", "u1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestInMemoryStoreDeleteNotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	err := store.Delete("slack", "nonexistent")
	if err == nil {
		t.Fatal("expected error for deleting nonexistent record")
	}
}

func TestPairingStatusConstants(t *testing.T) {
	t.Parallel()

	if StatusPending != "pending" {
		t.Fatalf("StatusPending = %q", StatusPending)
	}
	if StatusVerified != "verified" {
		t.Fatalf("StatusVerified = %q", StatusVerified)
	}
	if StatusRevoked != "revoked" {
		t.Fatalf("StatusRevoked = %q", StatusRevoked)
	}
}

func TestInMemoryStoreSaveReturnsCopy(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	rec := &PairingRecord{
		Channel:     "slack",
		UserID:      "u1",
		DisplayName: "original",
		CreatedAt:   time.Now(),
	}
	_ = store.Save(rec)

	// Mutate original.
	rec.DisplayName = "mutated"

	got, _ := store.Get("slack", "u1")
	if got.DisplayName != "original" {
		t.Fatal("Store should save a copy, not a reference")
	}
}
