package feishu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMessageDeduperPersistsAcrossInstances(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "feishu-dedup.json")
	first := NewMessageDeduper(path, time.Hour)
	if first.Seen("account:msg-1") {
		t.Fatal("first seen should be false")
	}
	if !first.Seen("account:msg-1") {
		t.Fatal("second seen on same instance should be true")
	}

	second := NewMessageDeduper(path, time.Hour)
	if !second.Seen("account:msg-1") {
		t.Fatal("second instance should load persisted dedupe state")
	}
}

func TestMessageDeduperRecordsPersistenceErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blocker := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(blocker) error = %v", err)
	}

	deduper := NewMessageDeduper(filepath.Join(blocker, "dedupe.json"), time.Hour)
	if deduper.Seen("account:msg-2") {
		t.Fatal("first seen should be false")
	}
	if deduper.LastError() == nil {
		t.Fatal("LastError() = nil, want persistence failure to be recorded")
	}
}

func TestMessageDeduperDebouncesSubsequentPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feishu-dedup.json")
	deduper := NewMessageDeduper(path, time.Hour)
	deduper.persistDelay = 200 * time.Millisecond

	if deduper.Seen("account:msg-1") {
		t.Fatal("first seen should be false")
	}
	payload := loadFeishuDedupePayload(t, path)
	if len(payload) != 1 {
		t.Fatalf("len(payload after first write) = %d, want 1", len(payload))
	}

	if deduper.Seen("account:msg-2") {
		t.Fatal("second distinct message should be false")
	}
	payload = loadFeishuDedupePayload(t, path)
	if len(payload) != 1 {
		t.Fatalf("len(payload before debounce flush) = %d, want 1", len(payload))
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		payload = loadFeishuDedupePayload(t, path)
		if len(payload) == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected debounced flush to persist second message, payload=%v", payload)
}

func loadFeishuDedupePayload(t *testing.T, path string) map[string]int64 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	payload := map[string]int64{}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", path, err)
	}
	return payload
}
