package pairing

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStorePersistsRecords(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pairing.json")
	store := NewFileStore(path)
	rec := &PairingRecord{
		ID:          "feishu:user-1",
		Channel:     "feishu",
		UserID:      "user-1",
		Status:      StatusPending,
		Code:        "123456",
		CreatedAt:   time.Now().UTC(),
		DisplayName: "Alice",
	}
	if err := store.Save(rec); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reloaded := NewFileStore(path)
	got, err := reloaded.Get("feishu", "user-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Code != "123456" || got.DisplayName != "Alice" {
		t.Fatalf("record = %#v", got)
	}
}

func TestFileStoreListFailsClosedOnCorruptJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pairing.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store := NewFileStore(path)
	if _, err := store.List(); err == nil {
		t.Fatal("expected List() to fail on corrupt JSON")
	}
}
