package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

// ---------------------------------------------------------------------------
// archive.zip / archive.unzip tests
// ---------------------------------------------------------------------------

func TestArchiveZipAndUnzip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Create files to archive.
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "hello.txt"), []byte("hello zip"), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "world.txt"), []byte("world zip"), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	// Create ZIP archive.
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-zip",
		Name: "archive.zip",
		Input: map[string]any{
			"output": "archive.zip",
			"paths":  []any{"src"},
		},
	}})
	if err != nil {
		t.Fatalf("archive.zip error = %v", err)
	}

	var zipPayload struct {
		Path       string `json:"path"`
		FileCount  int    `json:"file_count"`
		TotalBytes int64  `json:"total_bytes"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &zipPayload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if zipPayload.FileCount != 2 {
		t.Fatalf("file_count = %d, want 2", zipPayload.FileCount)
	}
	if zipPayload.TotalBytes != 18 { // "hello zip" (9) + "world zip" (9) = 18
		t.Fatalf("total_bytes = %d, want 18", zipPayload.TotalBytes)
	}

	// Verify archive file exists.
	if _, err := os.Stat(filepath.Join(root, "archive.zip")); err != nil {
		t.Fatalf("archive file not found: %v", err)
	}

	// Extract ZIP archive.
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-unzip",
		Name: "archive.unzip",
		Input: map[string]any{
			"path":   "archive.zip",
			"output": "extracted",
		},
	}})
	if err != nil {
		t.Fatalf("archive.unzip error = %v", err)
	}

	var unzipPayload struct {
		FileCount  int   `json:"file_count"`
		TotalBytes int64 `json:"total_bytes"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &unzipPayload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if unzipPayload.FileCount != 2 {
		t.Fatalf("unzip file_count = %d, want 2", unzipPayload.FileCount)
	}

	// Verify extracted content.
	data, err := os.ReadFile(filepath.Join(root, "extracted", "src", "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	if string(data) != "hello zip" {
		t.Fatalf("extracted content = %q", string(data))
	}
}

// ---------------------------------------------------------------------------
// archive.tar / archive.untar tests
// ---------------------------------------------------------------------------

func TestArchiveTarAndUntar(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tarsrc"), 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "tarsrc", "data.txt"), []byte("tar content"), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	// Create tar.gz archive.
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-tar",
		Name: "archive.tar",
		Input: map[string]any{
			"output":   "archive.tar.gz",
			"paths":    []any{"tarsrc"},
			"compress": true,
		},
	}})
	if err != nil {
		t.Fatalf("archive.tar error = %v", err)
	}

	var tarPayload struct {
		FileCount  int   `json:"file_count"`
		TotalBytes int64 `json:"total_bytes"`
		Compressed bool  `json:"compressed"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &tarPayload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if tarPayload.FileCount != 1 {
		t.Fatalf("file_count = %d, want 1", tarPayload.FileCount)
	}
	if !tarPayload.Compressed {
		t.Fatal("compressed should be true")
	}

	// Extract tar.gz archive.
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-untar",
		Name: "archive.untar",
		Input: map[string]any{
			"path":   "archive.tar.gz",
			"output": "tarextracted",
		},
	}})
	if err != nil {
		t.Fatalf("archive.untar error = %v", err)
	}

	var untarPayload struct {
		FileCount  int   `json:"file_count"`
		TotalBytes int64 `json:"total_bytes"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &untarPayload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if untarPayload.FileCount != 1 {
		t.Fatalf("untar file_count = %d, want 1", untarPayload.FileCount)
	}

	data, err := os.ReadFile(filepath.Join(root, "tarextracted", "tarsrc", "data.txt"))
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	if string(data) != "tar content" {
		t.Fatalf("extracted content = %q", string(data))
	}
}

func TestArchiveTarUncompressed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), []byte("plain tar"), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-tar-plain",
		Name: "archive.tar",
		Input: map[string]any{
			"output":   "plain.tar",
			"paths":    []any{"plain.txt"},
			"compress": false,
		},
	}})
	if err != nil {
		t.Fatalf("archive.tar error = %v", err)
	}

	var payload struct {
		Compressed bool `json:"compressed"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Compressed {
		t.Fatal("compressed should be false")
	}
}

// ---------------------------------------------------------------------------
// archive.list tests
// ---------------------------------------------------------------------------

func TestArchiveListZip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "listme.txt"), []byte("list content"), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	// Create a zip to list.
	builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-zip-list",
		Name: "archive.zip",
		Input: map[string]any{
			"output": "list.zip",
			"paths":  []any{"listme.txt"},
		},
	}})

	// List the zip.
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-list",
		Name: "archive.list",
		Input: map[string]any{
			"path": "list.zip",
		},
	}})
	if err != nil {
		t.Fatalf("archive.list error = %v", err)
	}

	var payload struct {
		Format  string           `json:"format"`
		Count   int              `json:"count"`
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if payload.Format != "zip" {
		t.Fatalf("format = %q, want zip", payload.Format)
	}
	if payload.Count < 1 {
		t.Fatalf("count = %d, want >= 1", payload.Count)
	}
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

func TestArchiveZipMissingPaths(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-zip-empty",
		Name: "archive.zip",
		Input: map[string]any{
			"output": "out.zip",
			"paths":  []any{},
		},
	}})
	if err == nil {
		t.Fatal("expected error when paths is empty")
	}
}
