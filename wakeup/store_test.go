package wakeup

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStore_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "wakeup.json")
	store := NewStore(path)
	now := time.Now().UTC().Truncate(time.Second)
	trigger := Trigger{
		ID:         "wake-1",
		Name:       "daily brief",
		Schedule:   "0 9 * * *",
		SessionKey: "webchat",
		Message:    "good morning",
		Enabled:    true,
		CreatedAt:  now,
		NextRunAt:  now.Add(time.Hour),
	}
	if err := store.Add(trigger); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	items := loaded.List()
	if len(items) != 1 {
		t.Fatalf("len(List()) = %d, want 1", len(items))
	}
	if items[0].ID != trigger.ID {
		t.Fatalf("id = %q, want %q", items[0].ID, trigger.ID)
	}
	if items[0].SessionKey != trigger.SessionKey {
		t.Fatalf("session_key = %q, want %q", items[0].SessionKey, trigger.SessionKey)
	}
}
