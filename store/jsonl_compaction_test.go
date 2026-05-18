package store

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendJSONLCompactsLargeSnapshotFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	cfg := jsonlAppendConfig{CompactThresholdBytes: 64}
	first := map[string]any{"id": "sess-1", "content": "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"}
	second := map[string]any{"id": "sess-1", "content": "latest snapshot"}
	if err := appendJSONLWithConfig(path, first, cfg); err != nil {
		t.Fatalf("appendJSONLWithConfig(first) error = %v", err)
	}
	if err := appendJSONLWithConfig(path, second, cfg); err != nil {
		t.Fatalf("appendJSONLWithConfig(second) error = %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lineCount := 0
	var last string
	for scanner.Scan() {
		lineCount++
		last = scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner.Err() = %v", err)
	}
	if lineCount != 1 {
		t.Fatalf("lineCount = %d, want 1", lineCount)
	}
	if !containsAll(last, `"id":"sess-1"`, `"content":"latest snapshot"`) {
		t.Fatalf("unexpected compacted line: %s", last)
	}
}

func containsAll(value string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(value, sub) {
			return false
		}
	}
	return true
}
