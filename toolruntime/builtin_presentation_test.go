package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestPresentationCreate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-pres"}
	sess := &agent.Session{ID: "sess-pres"}

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

	raw := exec("presentation.create", map[string]any{
		"path":   "test.pptx",
		"title":  "Test Presentation",
		"author": "Test Author",
		"slides": []any{
			map[string]any{
				"title":   "First Slide",
				"content": "Hello world",
				"notes":   "Speaker notes here",
			},
			map[string]any{
				"title":   "Second Slide",
				"content": []any{"Line one", "Line two"},
			},
			map[string]any{
				"title": "Empty Slide",
			},
		},
	})

	var createOut struct {
		Path       string `json:"path"`
		SlideCount int    `json:"slide_count"`
		Bytes      int    `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(raw), &createOut); err != nil {
		t.Fatalf("presentation.create unmarshal: %v", err)
	}
	if createOut.SlideCount != 3 {
		t.Fatalf("slide_count = %d, want 3", createOut.SlideCount)
	}
	if createOut.Bytes <= 0 {
		t.Fatalf("bytes = %d, want > 0", createOut.Bytes)
	}

	info, err := os.Stat(filepath.Join(root, "test.pptx"))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("file size = 0")
	}
}

func TestPresentationRead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-pres-read"}
	sess := &agent.Session{ID: "sess-pres-read"}

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

	// Create first, then read.
	exec("presentation.create", map[string]any{
		"path":   "read_test.pptx",
		"title":  "Read Test",
		"author": "Author A",
		"slides": []any{
			map[string]any{
				"title":   "Slide Alpha",
				"content": "Alpha content here",
			},
			map[string]any{
				"title":   "Slide Beta",
				"content": []any{"Beta line 1", "Beta line 2"},
			},
		},
	})

	raw := exec("presentation.read", map[string]any{
		"path": "read_test.pptx",
	})

	var readOut struct {
		Path       string `json:"path"`
		SlideCount int    `json:"slide_count"`
		Slides     []struct {
			Index   int    `json:"index"`
			Title   string `json:"title"`
			Content string `json:"content"`
			Notes   string `json:"notes"`
		} `json:"slides"`
	}
	if err := json.Unmarshal([]byte(raw), &readOut); err != nil {
		t.Fatalf("presentation.read unmarshal: %v", err)
	}
	if readOut.SlideCount != 2 {
		t.Fatalf("slide_count = %d, want 2", readOut.SlideCount)
	}
	if len(readOut.Slides) != 2 {
		t.Fatalf("len(slides) = %d, want 2", len(readOut.Slides))
	}
	if readOut.Slides[0].Title != "Slide Alpha" {
		t.Fatalf("slides[0].title = %q, want %q", readOut.Slides[0].Title, "Slide Alpha")
	}
	if readOut.Slides[0].Content != "Alpha content here" {
		t.Fatalf("slides[0].content = %q, want %q", readOut.Slides[0].Content, "Alpha content here")
	}
	if readOut.Slides[1].Title != "Slide Beta" {
		t.Fatalf("slides[1].title = %q, want %q", readOut.Slides[1].Title, "Slide Beta")
	}
	if readOut.Slides[1].Index != 2 {
		t.Fatalf("slides[1].index = %d, want 2", readOut.Slides[1].Index)
	}
}

func TestPresentationInfo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-pres-info"}
	sess := &agent.Session{ID: "sess-pres-info"}

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

	exec("presentation.create", map[string]any{
		"path":   "info_test.pptx",
		"title":  "Info Title",
		"author": "Info Author",
		"slides": []any{
			map[string]any{"title": "Only Slide", "content": "Some text"},
		},
	})

	raw := exec("presentation.info", map[string]any{
		"path": "info_test.pptx",
	})

	var infoOut struct {
		Path       string `json:"path"`
		SlideCount int    `json:"slide_count"`
		Title      string `json:"title"`
		Author     string `json:"author"`
		FileSize   int64  `json:"file_size"`
	}
	if err := json.Unmarshal([]byte(raw), &infoOut); err != nil {
		t.Fatalf("presentation.info unmarshal: %v", err)
	}
	if infoOut.SlideCount != 1 {
		t.Fatalf("slide_count = %d, want 1", infoOut.SlideCount)
	}
	if infoOut.Title != "Info Title" {
		t.Fatalf("title = %q, want %q", infoOut.Title, "Info Title")
	}
	if infoOut.Author != "Info Author" {
		t.Fatalf("author = %q, want %q", infoOut.Author, "Info Author")
	}
	if infoOut.FileSize <= 0 {
		t.Fatalf("file_size = %d, want > 0", infoOut.FileSize)
	}
}
