package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestDocumentCreate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	// document tools are registered by NewBuiltins via builtin.go
	ctx := context.Background()
	run := &agent.Run{ID: "run-doc"}
	sess := &agent.Session{ID: "sess-doc"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	raw := exec("document.create", map[string]any{
		"path":   "test.docx",
		"title":  "Test Document",
		"author": "Unit Test",
		"content": []any{
			map[string]any{"text": "Main Title", "style": "heading1"},
			map[string]any{"text": "This is the first paragraph."},
			map[string]any{"text": "Sub Heading", "style": "heading2"},
			map[string]any{"text": "Second paragraph with more text."},
		},
	})

	var out struct {
		Path           string `json:"path"`
		ParagraphCount int    `json:"paragraph_count"`
		Bytes          int    `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("document.create unmarshal: %v", err)
	}
	if out.Path != "test.docx" {
		t.Fatalf("path = %q, want test.docx", out.Path)
	}
	if out.ParagraphCount != 4 {
		t.Fatalf("paragraph_count = %d, want 4", out.ParagraphCount)
	}
	if out.Bytes == 0 {
		t.Fatalf("bytes = 0, want >0")
	}

	// Verify file exists on disk.
	fi, err := os.Stat(filepath.Join(root, "test.docx"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatalf("file is empty")
	}
}

func TestDocumentCreateNestedPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	ctx := context.Background()
	run := &agent.Run{ID: "run-doc-nested"}
	sess := &agent.Session{ID: "sess-doc-nested"}

	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-document.create",
		Name: "document.create",
		Input: map[string]any{
			"path": "nested/output/test.docx",
			"content": []any{
				map[string]any{"text": "Nested document"},
			},
		},
	}})
	if err != nil {
		t.Fatalf("document.create error: %v", err)
	}
	if results[0].Content == "" {
		t.Fatal("document.create returned empty content")
	}
	if _, err := os.Stat(filepath.Join(root, "nested/output/test.docx")); err != nil {
		t.Fatalf("stat nested docx: %v", err)
	}
}

func TestDocumentRead(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	// document tools are registered by NewBuiltins via builtin.go
	ctx := context.Background()
	run := &agent.Run{ID: "run-doc-read"}
	sess := &agent.Session{ID: "sess-doc-read"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	// Create a document first.
	exec("document.create", map[string]any{
		"path":   "read-test.docx",
		"title":  "Read Test",
		"author": "Tester",
		"content": []any{
			map[string]any{"text": "Hello World", "style": "heading1"},
			map[string]any{"text": "First body paragraph."},
			map[string]any{"text": "Second body paragraph."},
		},
	})

	// Read it back.
	raw := exec("document.read", map[string]any{"path": "read-test.docx"})

	var out struct {
		Path           string   `json:"path"`
		Paragraphs     []string `json:"paragraphs"`
		Text           string   `json:"text"`
		ParagraphCount int      `json:"paragraph_count"`
		WordCount      int      `json:"word_count"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("document.read unmarshal: %v", err)
	}
	if out.ParagraphCount != 3 {
		t.Fatalf("paragraph_count = %d, want 3", out.ParagraphCount)
	}
	if out.Paragraphs[0] != "Hello World" {
		t.Fatalf("paragraphs[0] = %q, want Hello World", out.Paragraphs[0])
	}
	if out.Paragraphs[1] != "First body paragraph." {
		t.Fatalf("paragraphs[1] = %q", out.Paragraphs[1])
	}
	if out.WordCount < 5 {
		t.Fatalf("word_count = %d, want >= 5", out.WordCount)
	}
}

func TestDocumentInfo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	// document tools are registered by NewBuiltins via builtin.go
	ctx := context.Background()
	run := &agent.Run{ID: "run-doc-info"}
	sess := &agent.Session{ID: "sess-doc-info"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	exec("document.create", map[string]any{
		"path":   "info-test.docx",
		"title":  "Info Title",
		"author": "Info Author",
		"content": []any{
			map[string]any{"text": "Heading One", "style": "heading1"},
			map[string]any{"text": "Body text here."},
		},
	})

	raw := exec("document.info", map[string]any{"path": "info-test.docx"})

	var out struct {
		Path           string `json:"path"`
		Title          string `json:"title"`
		Author         string `json:"author"`
		WordCount      int    `json:"word_count"`
		ParagraphCount int    `json:"paragraph_count"`
		FileSize       int64  `json:"file_size"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("document.info unmarshal: %v", err)
	}
	if out.Title != "Info Title" {
		t.Fatalf("title = %q, want Info Title", out.Title)
	}
	if out.Author != "Info Author" {
		t.Fatalf("author = %q, want Info Author", out.Author)
	}
	if out.ParagraphCount != 2 {
		t.Fatalf("paragraph_count = %d, want 2", out.ParagraphCount)
	}
	if out.FileSize == 0 {
		t.Fatalf("file_size = 0, want >0")
	}
}

func TestDocumentSearch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	// document tools are registered by NewBuiltins via builtin.go
	ctx := context.Background()
	run := &agent.Run{ID: "run-doc-search"}
	sess := &agent.Session{ID: "sess-doc-search"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	exec("document.create", map[string]any{
		"path": "search-test.docx",
		"content": []any{
			map[string]any{"text": "The quick brown fox jumps over the lazy dog."},
			map[string]any{"text": "A second paragraph without the keyword."},
			map[string]any{"text": "Another mention of the quick fox here."},
		},
	})

	// Case-insensitive search (default).
	raw := exec("document.search", map[string]any{
		"path":  "search-test.docx",
		"query": "Quick",
	})

	var out struct {
		Query        string           `json:"query"`
		TotalMatches int              `json:"total_matches"`
		Matches      []map[string]any `json:"matches"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("document.search unmarshal: %v", err)
	}
	if out.TotalMatches != 2 {
		t.Fatalf("total_matches = %d, want 2", out.TotalMatches)
	}

	// Case-sensitive search.
	raw = exec("document.search", map[string]any{
		"path":           "search-test.docx",
		"query":          "Quick",
		"case_sensitive": true,
	})
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("document.search (case-sensitive) unmarshal: %v", err)
	}
	if out.TotalMatches != 0 {
		t.Fatalf("total_matches (case-sensitive) = %d, want 0", out.TotalMatches)
	}
}
