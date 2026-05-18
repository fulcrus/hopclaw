package channels

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEvaluatePolicy(t *testing.T) {
	t.Parallel()

	decision := EvaluatePolicy(PolicyConfig{
		DMPolicy:    "allowlist",
		AllowFrom:   []string{"user-1"},
		GroupPolicy: "open",
	}, PolicyEnvelope{
		ChatType: "direct",
		SenderID: "user-2",
	})
	if decision.Allow {
		t.Fatal("expected direct message to be blocked by allowlist")
	}

	decision = EvaluatePolicy(PolicyConfig{
		GroupPolicy:    "open",
		RequireMention: true,
	}, PolicyEnvelope{
		ChatType:  "group",
		SenderID:  "user-1",
		Mentioned: false,
	})
	if decision.Allow {
		t.Fatal("expected group message without mention to be blocked")
	}
}

func TestSessionKeyForEnvelope(t *testing.T) {
	t.Parallel()

	got := SessionKeyForEnvelope("slack", PolicyConfig{GroupSessionScope: "group_thread_sender"}, PolicyEnvelope{
		ChatType: "group",
		ChatID:   "C1",
		ThreadID: "T1",
		SenderID: "U1",
	}, SessionKeyOptions{DirectPrefix: "dm"})
	if got != "slack:thread:T1:sender:U1" {
		t.Fatalf("SessionKeyForEnvelope() = %q", got)
	}
}

func TestMessageDeduperPersistsAcrossInstances(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "dedupe.json")
	first := NewMessageDeduper(path, time.Hour)
	if first.Seen("message-1") {
		t.Fatal("first seen should be false")
	}
	second := NewMessageDeduper(path, time.Hour)
	if !second.Seen("message-1") {
		t.Fatal("second instance should load persisted state")
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
	if deduper.Seen("message-1") {
		t.Fatal("first seen should be false")
	}
	if deduper.LastError() == nil {
		t.Fatal("LastError() = nil, want persistence failure to be recorded")
	}
}

func TestMessageDeduperDebouncesSubsequentPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dedupe.json")
	deduper := NewMessageDeduper(path, time.Hour)
	deduper.persistDelay = 200 * time.Millisecond

	if deduper.Seen("message-1") {
		t.Fatal("first seen should be false")
	}
	payload := loadDedupePayload(t, path)
	if len(payload) != 1 {
		t.Fatalf("len(payload after first write) = %d, want 1", len(payload))
	}

	if deduper.Seen("message-2") {
		t.Fatal("second distinct message should be false")
	}
	payload = loadDedupePayload(t, path)
	if len(payload) != 1 {
		t.Fatalf("len(payload before debounce flush) = %d, want 1", len(payload))
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		payload = loadDedupePayload(t, path)
		if len(payload) == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected debounced flush to persist second message, payload=%v", payload)
}

func loadDedupePayload(t *testing.T, path string) map[string]int64 {
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

func TestThreadBindingPersistsAcrossInstances(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "bindings.json")
	first := NewPersistentThreadBinding(path)
	first.Bind("slack", "thread-1", "slack:thread:thread-1")

	second := NewPersistentThreadBinding(path)
	sessionKey, ok := second.Resolve("slack", "thread-1")
	if !ok {
		t.Fatal("expected persisted thread binding")
	}
	if sessionKey != "slack:thread:thread-1" {
		t.Fatalf("Resolve() = %q", sessionKey)
	}
}
