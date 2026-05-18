package eventbus

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileEventLogWriteAndReplay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	log, err := NewFileEventLog(FileEventLogConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	now := time.Now().UTC()
	events := []Event{
		{ID: "e1", Type: EventRunStarted, RunID: "r1", Time: now, Attrs: map[string]any{"k": "v1"}},
		{ID: "e2", Type: EventRunCompleted, RunID: "r1", Time: now.Add(time.Second), Attrs: map[string]any{"k": "v2"}},
		{ID: "e3", Type: EventToolExecuted, RunID: "r2", Time: now.Add(2 * time.Second)},
	}

	ctx := context.Background()
	for _, e := range events {
		if err := log.Handle(ctx, e); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}

	got, err := log.Replay()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[0].ID != "e1" || got[1].ID != "e2" || got[2].ID != "e3" {
		t.Fatalf("unexpected event IDs: %v, %v, %v", got[0].ID, got[1].ID, got[2].ID)
	}
	if got[0].Attrs["k"] != "v1" {
		t.Fatalf("attrs not preserved: %v", got[0].Attrs)
	}
}

func TestFileEventLogReplaySince(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	log, err := NewFileEventLog(FileEventLogConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	for i := range 5 {
		_ = log.Handle(ctx, Event{
			ID:   fmt.Sprintf("e%d", i),
			Type: EventRunStarted,
			Time: now.Add(time.Duration(i) * time.Second),
		})
	}

	// Replay since e2 → should get e3, e4.
	got, err := log.ReplaySince("e2", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events after e2, got %d", len(got))
	}
	if got[0].ID != "e3" || got[1].ID != "e4" {
		t.Fatalf("unexpected: %s, %s", got[0].ID, got[1].ID)
	}

	// With limit.
	got, err = log.ReplaySince("e1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].ID != "e2" {
		t.Fatalf("expected e2, got %s", got[0].ID)
	}
}

func TestFileEventLogRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	// Small max to trigger rotation quickly.
	log, err := NewFileEventLog(FileEventLogConfig{Path: path, MaxFileSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	// Write enough events to trigger rotation.
	for i := range 10 {
		_ = log.Handle(ctx, Event{
			ID:    fmt.Sprintf("event-%d", i),
			Type:  EventRunStarted,
			RunID: "r1",
			Time:  now,
			Attrs: map[string]any{"idx": i},
		})
	}

	// Old file should exist.
	if _, err := os.Stat(path + ".old"); os.IsNotExist(err) {
		t.Fatal("expected rotated .old file")
	}

	// Current file should still be readable.
	got, err := log.Replay()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected some events in current file after rotation")
	}
}

func TestFileEventLogReplayNonExistent(t *testing.T) {
	log := &FileEventLog{path: "/nonexistent/events.jsonl"}
	got, err := log.Replay()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 events for nonexistent file, got %d", len(got))
	}
}

func TestFileEventLogAsSink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	log, err := NewFileEventLog(FileEventLogConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	// Wire as a Sink on InMemoryBus.
	bus := NewInMemoryBus()
	bus.Subscribe(log)

	ctx := context.Background()
	_ = bus.Publish(ctx, Event{ID: "pub1", Type: EventRunStarted})
	_ = bus.Publish(ctx, Event{ID: "pub2", Type: EventRunCompleted})

	// Events should be persisted.
	got, err := log.Replay()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 persisted events, got %d", len(got))
	}
	if got[0].ID != "pub1" || got[1].ID != "pub2" {
		t.Fatalf("wrong IDs: %s, %s", got[0].ID, got[1].ID)
	}
}
