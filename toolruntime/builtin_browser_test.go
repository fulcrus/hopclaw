package toolruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	"github.com/fulcrus/hopclaw/browserapi/types"
)

func TestBrowserOpenIncludesInitialURLInResult(t *testing.T) {
	t.Parallel()

	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(types.Response{
		OK:        true,
		SessionID: "sess-1",
		Data: map[string]any{
			"session_id": "sess-1",
		},
	})

	b := newCanvasTestBuiltins(t, mock)
	out := execCanvas(t, context.Background(), b, "browser.open", map[string]any{
		"url": "https://example.com",
	})

	if out["session_id"] != "sess-1" {
		t.Fatalf("browser.open session_id = %v, want sess-1", out["session_id"])
	}
	if out["url"] != "https://example.com" {
		t.Fatalf("browser.open url = %v, want https://example.com", out["url"])
	}
	last := mock.lastAction()
	if last.Action != types.ActionCreateSession {
		t.Fatalf("browser.open action = %q, want %q", last.Action, types.ActionCreateSession)
	}
	if got, _ := last.Params["url"].(string); got != "https://example.com" {
		t.Fatalf("browser.open sent url = %q, want https://example.com", got)
	}
}

func TestBrowserSnapshotIncludesPageMetadata(t *testing.T) {
	t.Parallel()

	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(types.Response{
		OK: true,
		Data: map[string]any{
			"html":         "<html><body>Example</body></html>",
			"url":          "https://example.com",
			"title":        "Example Domain",
			"content_type": "text/html",
		},
	})

	b := newCanvasTestBuiltins(t, mock)
	out := execCanvas(t, context.Background(), b, "browser.snapshot", map[string]any{
		"session_id": "sess-1",
	})

	if out["content"] != "<html><body>Example</body></html>" {
		t.Fatalf("browser.snapshot content = %v", out["content"])
	}
	if out["html"] != "<html><body>Example</body></html>" {
		t.Fatalf("browser.snapshot html = %v", out["html"])
	}
	if out["url"] != "https://example.com" {
		t.Fatalf("browser.snapshot url = %v, want https://example.com", out["url"])
	}
	if out["title"] != "Example Domain" {
		t.Fatalf("browser.snapshot title = %v, want Example Domain", out["title"])
	}
	if out["content_type"] != "text/html" {
		t.Fatalf("browser.snapshot content_type = %v, want text/html", out["content_type"])
	}
	last := mock.lastAction()
	if last.Action != types.ActionSnapshot {
		t.Fatalf("browser.snapshot action = %q, want %q", last.Action, types.ActionSnapshot)
	}
}

func TestBrowserClickIncludesPageMetadata(t *testing.T) {
	t.Parallel()

	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(types.Response{
		OK: true,
		Data: map[string]any{
			"selector": "#submit",
			"clicked":  true,
			"url":      "https://httpbin.org/post",
			"title":    "httpbin.org",
		},
	})

	b := newCanvasTestBuiltins(t, mock)
	out := execCanvas(t, context.Background(), b, "browser.click", map[string]any{
		"session_id": "sess-1",
		"selector":   "#submit",
	})

	if out["ok"] != true {
		t.Fatalf("browser.click ok = %v, want true", out["ok"])
	}
	if out["url"] != "https://httpbin.org/post" {
		t.Fatalf("browser.click url = %v, want https://httpbin.org/post", out["url"])
	}
	if out["title"] != "httpbin.org" {
		t.Fatalf("browser.click title = %v, want httpbin.org", out["title"])
	}
	last := mock.lastAction()
	if last.Action != types.ActionClick {
		t.Fatalf("browser.click action = %q, want %q", last.Action, types.ActionClick)
	}
}

func TestBrowserTypeIncludesSelectorAndText(t *testing.T) {
	t.Parallel()

	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(types.Response{
		OK: true,
		Data: map[string]any{
			"selector": "#email",
			"text":     "demo@example.com",
		},
	})

	b := newCanvasTestBuiltins(t, mock)
	out := execCanvas(t, context.Background(), b, "browser.type", map[string]any{
		"session_id": "sess-1",
		"selector":   "#email",
		"text":       "demo@example.com",
	})

	if out["ok"] != true {
		t.Fatalf("browser.type ok = %v, want true", out["ok"])
	}
	if out["selector"] != "#email" {
		t.Fatalf("browser.type selector = %v, want #email", out["selector"])
	}
	if out["text"] != "demo@example.com" {
		t.Fatalf("browser.type text = %v, want demo@example.com", out["text"])
	}
}

func TestBrowserSnapshotAriaUsesReadableTranscriptAndRefSummary(t *testing.T) {
	t.Parallel()

	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(types.Response{
		OK: true,
		Data: map[string]any{
			"text":          "RootWebArea\n  [e13] button \"显示时间选择器\"\n  [e14] textbox \"Delivery instructions: \"\n  [e15] button \"Submit order\"\n",
			"element_count": 3,
			"tree": map[string]any{
				"role": "document",
				"children": []any{
					map[string]any{"ref": "e13", "role": "button", "name": "显示时间选择器"},
					map[string]any{"ref": "e14", "role": "textbox", "name": "Delivery instructions: "},
					map[string]any{"ref": "e15", "role": "button", "name": "Submit order"},
				},
			},
		},
	})

	b := newCanvasTestBuiltins(t, mock)
	results, err := b.ExecuteBatch(context.Background(), &agent.Run{ID: "r1"}, &agent.Session{ID: "s1"}, []agent.ToolCall{{
		ID:   "call-browser.snapshot_aria",
		Name: "browser.snapshot_aria",
		Input: map[string]any{
			"session_id": "sess-1",
		},
	}})
	if err != nil {
		t.Fatalf("browser.snapshot_aria returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if strings.Contains(results[0].TranscriptText, "\\n") {
		t.Fatalf("browser.snapshot_aria transcript should be readable, got %q", results[0].TranscriptText)
	}
	if !strings.Contains(results[0].TranscriptText, `[e15] button "Submit order" action=click submit_candidate=true`) {
		t.Fatalf("browser.snapshot_aria transcript = %q, want submit ref summary", results[0].TranscriptText)
	}
	if !strings.Contains(results[0].TranscriptText, `[e13] button "显示时间选择器" action=click`) {
		t.Fatalf("browser.snapshot_aria transcript = %q, want non-submit button summary", results[0].TranscriptText)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
		t.Fatalf("browser.snapshot_aria result unmarshal: %v", err)
	}
	refs, ok := out["refs"].([]any)
	if !ok || len(refs) != 3 {
		t.Fatalf("browser.snapshot_aria refs = %#v, want 3 entries", out["refs"])
	}
	last := mock.lastAction()
	if last.Action != types.ActionSnapshotAria {
		t.Fatalf("browser.snapshot_aria action = %q, want %q", last.Action, types.ActionSnapshotAria)
	}
}

func TestAriaSnapshotTranscriptPrioritizesMainContentRefs(t *testing.T) {
	t.Parallel()

	tree := map[string]any{
		"role": "document",
		"children": []any{
			map[string]any{
				"role": "banner",
				"children": []any{
					map[string]any{"ref": "e1", "role": "link", "name": "登录"},
				},
			},
			map[string]any{
				"role": "main",
				"name": "搜索结果",
				"children": []any{
					map[string]any{"ref": "e2", "role": "link", "name": "OpenAI"},
					map[string]any{"ref": "e3", "role": "link", "name": "API Platform"},
				},
			},
		},
	}

	refs := flattenAriaRefs(tree)
	if len(refs) != 3 {
		t.Fatalf("flattenAriaRefs len = %d, want 3", len(refs))
	}
	if refs[0].Ref != "e2" || refs[1].Ref != "e3" || refs[2].Ref != "e1" {
		t.Fatalf("flattenAriaRefs order = %#v, want main-content refs first", refs)
	}

	transcript := buildAriaSnapshotTranscript(refs, "")
	if strings.Index(transcript, `[e2] link "OpenAI"`) > strings.Index(transcript, `[e1] link "登录"`) {
		t.Fatalf("transcript = %q, want main-content refs before banner refs", transcript)
	}
}

func TestBrowserScreenshotLabeledUsesExtendedRequestTimeout(t *testing.T) {
	t.Parallel()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req types.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if req.Action != types.ActionScreenshotLabeled {
			t.Fatalf("unexpected action %q", req.Action)
		}
		time.Sleep(80 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(types.Response{
			OK: true,
			Data: map[string]any{
				"element_count": 2,
				"elements": map[string]any{
					"e1": map[string]any{"tag": "a", "text": "OpenAI"},
					"e2": map[string]any{"tag": "a", "text": "API Platform"},
				},
			},
		})
	}))
	defer host.Close()

	b := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	b.ApplyBindings(BuiltinsBindings{
		BrowserClient: browserclient.NewWithConfig(browserclient.Config{
			BaseURL: host.URL,
			Timeout: 20 * time.Millisecond,
		}),
	})

	out := execCanvas(t, context.Background(), b, "browser.screenshot_labeled", map[string]any{
		"session_id": "sess-1",
	})
	if out["ok"] != true {
		t.Fatalf("browser.screenshot_labeled ok = %v, want true", out["ok"])
	}
	if out["element_count"] != float64(2) {
		t.Fatalf("browser.screenshot_labeled element_count = %v, want 2", out["element_count"])
	}
}

func TestBrowserSnapshotUsesExtendedRequestTimeout(t *testing.T) {
	t.Parallel()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req types.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if req.Action != types.ActionSnapshot {
			t.Fatalf("unexpected action %q", req.Action)
		}
		time.Sleep(80 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(types.Response{
			OK: true,
			Data: map[string]any{
				"html":         "<html><body>OpenAI</body></html>",
				"url":          "https://www.bing.com/search?q=openai",
				"title":        "openai - Search",
				"content_type": "text/html",
			},
		})
	}))
	defer host.Close()

	b := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	b.ApplyBindings(BuiltinsBindings{
		BrowserClient: browserclient.NewWithConfig(browserclient.Config{
			BaseURL: host.URL,
			Timeout: 20 * time.Millisecond,
		}),
	})

	out := execCanvas(t, context.Background(), b, "browser.snapshot", map[string]any{
		"session_id": "sess-1",
	})
	if out["ok"] != true {
		t.Fatalf("browser.snapshot ok = %v, want true", out["ok"])
	}
	if out["title"] != "openai - Search" {
		t.Fatalf("browser.snapshot title = %v, want openai - Search", out["title"])
	}
}

func TestBrowserClickAriaIncludesRefAndPageMetadata(t *testing.T) {
	t.Parallel()

	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(types.Response{
		OK: true,
		Data: map[string]any{
			"ref":     "e13",
			"clicked": true,
			"url":     "https://httpbin.org/post",
			"title":   "httpbin.org",
		},
	})

	b := newCanvasTestBuiltins(t, mock)
	out := execCanvas(t, context.Background(), b, "browser.click_aria", map[string]any{
		"session_id": "sess-1",
		"ref":        "e13",
	})

	if out["ok"] != true {
		t.Fatalf("browser.click_aria ok = %v, want true", out["ok"])
	}
	if out["ref"] != "e13" {
		t.Fatalf("browser.click_aria ref = %v, want e13", out["ref"])
	}
	if out["clicked"] != true {
		t.Fatalf("browser.click_aria clicked = %v, want true", out["clicked"])
	}
	if out["url"] != "https://httpbin.org/post" {
		t.Fatalf("browser.click_aria url = %v, want https://httpbin.org/post", out["url"])
	}
	if out["title"] != "httpbin.org" {
		t.Fatalf("browser.click_aria title = %v, want httpbin.org", out["title"])
	}
	last := mock.lastAction()
	if last.Action != types.ActionClickAria {
		t.Fatalf("browser.click_aria action = %q, want %q", last.Action, types.ActionClickAria)
	}
}

func TestBrowserTypeAriaIncludesRefAndTypedText(t *testing.T) {
	t.Parallel()

	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(types.Response{
		OK: true,
		Data: map[string]any{
			"ref":   "e3",
			"text":  "demo@example.com",
			"typed": true,
		},
	})

	b := newCanvasTestBuiltins(t, mock)
	out := execCanvas(t, context.Background(), b, "browser.type_aria", map[string]any{
		"session_id": "sess-1",
		"ref":        "e3",
		"text":       "demo@example.com",
	})

	if out["ok"] != true {
		t.Fatalf("browser.type_aria ok = %v, want true", out["ok"])
	}
	if out["ref"] != "e3" {
		t.Fatalf("browser.type_aria ref = %v, want e3", out["ref"])
	}
	if out["text"] != "demo@example.com" {
		t.Fatalf("browser.type_aria text = %v, want demo@example.com", out["text"])
	}
	if out["typed"] != true {
		t.Fatalf("browser.type_aria typed = %v, want true", out["typed"])
	}
	last := mock.lastAction()
	if last.Action != types.ActionTypeAria {
		t.Fatalf("browser.type_aria action = %q, want %q", last.Action, types.ActionTypeAria)
	}
}
