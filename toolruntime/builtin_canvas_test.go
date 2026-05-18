package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
)

// ---------------------------------------------------------------------------
// Mock browser server
// ---------------------------------------------------------------------------

// browserAction records a single action sent to the mock browser.
type browserAction struct {
	Action    string         `json:"action"`
	SessionID string         `json:"session_id"`
	Params    map[string]any `json:"params"`
}

// mockBrowserServer is an httptest server that records requests and returns
// configurable responses.
type mockBrowserServer struct {
	mu       sync.Mutex
	actions  []browserAction
	response browsertypes.Response
	server   *httptest.Server
}

func newMockBrowserServer() *mockBrowserServer {
	m := &mockBrowserServer{
		response: browsertypes.Response{OK: true},
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req browsertypes.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		m.actions = append(m.actions, browserAction{
			Action:    req.Action,
			SessionID: req.SessionID,
			Params:    req.Params,
		})
		resp := m.response
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	return m
}

func (m *mockBrowserServer) close() {
	m.server.Close()
}

func (m *mockBrowserServer) setResponse(resp browsertypes.Response) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.response = resp
}

func (m *mockBrowserServer) lastAction() browserAction {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.actions) == 0 {
		return browserAction{}
	}
	return m.actions[len(m.actions)-1]
}

// newCanvasTestBuiltins creates a Builtins instance wired to the mock server.
func newCanvasTestBuiltins(t *testing.T, mock *mockBrowserServer) *Builtins {
	t.Helper()
	b := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	client := browserclient.New(mock.server.URL)
	b.ApplyBindings(BuiltinsBindings{BrowserClient: client})
	return b
}

// execCanvas is a test helper that calls a single canvas tool and returns
// the parsed JSON result.
func execCanvas(t *testing.T, ctx context.Context, b *Builtins, name string, input map[string]any) map[string]any {
	t.Helper()
	results, err := b.ExecuteBatch(ctx, &agent.Run{ID: "r1"}, &agent.Session{ID: "s1"}, []agent.ToolCall{{
		ID: "call-" + name, Name: name, Input: input,
	}})
	if err != nil {
		t.Fatalf("%s returned error: %v", name, err)
	}
	if len(results) != 1 {
		t.Fatalf("%s returned %d results, want 1", name, len(results))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
		t.Fatalf("%s result unmarshal: %v", name, err)
	}
	return out
}

// execCanvasErr is a test helper that calls a canvas tool and expects an error.
func execCanvasErr(t *testing.T, ctx context.Context, b *Builtins, name string, input map[string]any) error {
	t.Helper()
	_, err := b.ExecuteBatch(ctx, &agent.Run{ID: "r1"}, &agent.Session{ID: "s1"}, []agent.ToolCall{{
		ID: "call-" + name, Name: name, Input: input,
	}})
	return err
}

// ---------------------------------------------------------------------------
// Tests — canvas.present
// ---------------------------------------------------------------------------

func TestCanvasPresent(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.present", map[string]any{
		"session_id": "sess-1",
		"html":       "<h1>Hello</h1>",
		"title":      "Greeting",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.present ok = %v, want true", out["ok"])
	}
	if out["title"] != "Greeting" {
		t.Fatalf("canvas.present title = %v, want Greeting", out["title"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionNavigate {
		t.Fatalf("expected navigate action, got %q", last.Action)
	}
	if last.SessionID != "sess-1" {
		t.Fatalf("expected session_id sess-1, got %q", last.SessionID)
	}
	urlStr, _ := last.Params["url"].(string)
	if !strings.HasPrefix(urlStr, "data:text/html") {
		t.Fatalf("expected data URL, got %q", urlStr)
	}
}

func TestCanvasPresentWithTemplate(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.present", map[string]any{
		"session_id": "sess-1",
		"template":   "chart.bar",
		"params": map[string]any{
			"title":  "Sales",
			"labels": []any{"Q1", "Q2", "Q3"},
			"values": []any{10.0, 20.0, 15.0},
		},
	})
	if out["ok"] != true {
		t.Fatalf("canvas.present (template) ok = %v, want true", out["ok"])
	}
	if out["template"] != "chart.bar" {
		t.Fatalf("canvas.present (template) template = %v, want chart.bar", out["template"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionNavigate {
		t.Fatalf("expected navigate action, got %q", last.Action)
	}
	urlStr, _ := last.Params["url"].(string)
	if !strings.Contains(urlStr, "svg") {
		t.Fatalf("expected SVG content in data URL for chart.bar template")
	}
}

func TestCanvasPresentUnknownTemplate(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.present", map[string]any{
		"session_id": "sess-1",
		"template":   "nonexistent",
		"params":     map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
	if !strings.Contains(err.Error(), "unknown canvas template") {
		t.Fatalf("expected 'unknown canvas template' in error, got %q", err.Error())
	}
}

func TestCanvasPresentMissingSessionID(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.present", map[string]any{
		"html": "<p>test</p>",
	})
	if err == nil {
		t.Fatal("expected error for missing session_id")
	}
}

func TestCanvasPresentMissingHTML(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.present", map[string]any{
		"session_id": "sess-1",
	})
	if err == nil {
		t.Fatal("expected error when neither html nor template is provided")
	}
}

func TestCanvasPresentTemplateMissingRequiredParams(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.present", map[string]any{
		"session_id": "sess-1",
		"template":   "chart.bar",
		"params":     map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error for template with missing required params")
	}
	if !strings.Contains(err.Error(), "requires parameter") {
		t.Fatalf("expected 'requires parameter' in error, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests — canvas.eval
// ---------------------------------------------------------------------------

func TestCanvasEval(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(browsertypes.Response{
		OK:   true,
		Data: map[string]any{"result": "42"},
	})
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.eval", map[string]any{
		"session_id": "sess-1",
		"expression": "2 + 40",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.eval ok = %v, want true", out["ok"])
	}
	if out["result"] != "42" {
		t.Fatalf("canvas.eval result = %v, want 42", out["result"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionEval {
		t.Fatalf("expected eval action, got %q", last.Action)
	}
}

func TestCanvasEvalMissingExpression(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.eval", map[string]any{
		"session_id": "sess-1",
	})
	if err == nil {
		t.Fatal("expected error for missing expression")
	}
}

// ---------------------------------------------------------------------------
// Tests — canvas.snapshot
// ---------------------------------------------------------------------------

func TestCanvasSnapshot(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(browsertypes.Response{
		OK:   true,
		Data: map[string]any{"base64": "iVBOR..."},
	})
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.snapshot", map[string]any{
		"session_id": "sess-1",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.snapshot ok = %v, want true", out["ok"])
	}
	if out["base64"] != "iVBOR..." {
		t.Fatalf("canvas.snapshot base64 = %v, want iVBOR...", out["base64"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionScreenshot {
		t.Fatalf("expected screenshot action, got %q", last.Action)
	}
}

func TestCanvasSnapshotWithSelector(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(browsertypes.Response{
		OK:   true,
		Data: map[string]any{"base64": "abc123"},
	})
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	_ = execCanvas(t, ctx, b, "canvas.snapshot", map[string]any{
		"session_id": "sess-1",
		"selector":   "#main",
	})
	last := mock.lastAction()
	if last.Params["selector"] != "#main" {
		t.Fatalf("expected selector #main, got %v", last.Params["selector"])
	}
}

func TestCanvasSnapshotArtifactRefFallback(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(browsertypes.Response{
		OK:          true,
		ArtifactRef: "artifact://screenshot/xyz",
	})
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.snapshot", map[string]any{
		"session_id": "sess-1",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.snapshot ok = %v, want true", out["ok"])
	}
	if out["base64"] != "artifact://screenshot/xyz" {
		t.Fatalf("canvas.snapshot base64 fallback = %v, want artifact://screenshot/xyz", out["base64"])
	}
}

// ---------------------------------------------------------------------------
// Tests — canvas.hide
// ---------------------------------------------------------------------------

func TestCanvasHide(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.hide", map[string]any{
		"session_id": "sess-1",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.hide ok = %v, want true", out["ok"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionNavigate {
		t.Fatalf("expected navigate action, got %q", last.Action)
	}
	if last.Params["url"] != canvasBlankURL {
		t.Fatalf("expected blank URL, got %v", last.Params["url"])
	}
}

// ---------------------------------------------------------------------------
// Tests — canvas.style
// ---------------------------------------------------------------------------

func TestCanvasStyle(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.style", map[string]any{
		"session_id": "sess-1",
		"selector":   ".box",
		"css": map[string]any{
			"color":     "red",
			"font-size": "16px",
		},
	})
	if out["ok"] != true {
		t.Fatalf("canvas.style ok = %v, want true", out["ok"])
	}
	if out["selector"] != ".box" {
		t.Fatalf("canvas.style selector = %v, want .box", out["selector"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionEval {
		t.Fatalf("expected eval action, got %q", last.Action)
	}
	expr, _ := last.Params["expression"].(string)
	if !strings.Contains(expr, "querySelectorAll") {
		t.Fatalf("expected querySelectorAll in JS, got %q", expr)
	}
	if !strings.Contains(expr, "el.style") {
		t.Fatalf("expected el.style assignment in JS, got %q", expr)
	}
}

func TestCanvasStyleJSContainsProperties(t *testing.T) {
	t.Parallel()
	js := canvasStyleJS(".target", map[string]any{
		"color": "blue",
	})
	if !strings.Contains(js, `querySelectorAll(".target")`) {
		t.Fatalf("expected querySelectorAll with selector, got %q", js)
	}
	if !strings.Contains(js, `el.style["color"]="blue"`) {
		t.Fatalf("expected color assignment in JS, got %q", js)
	}
}

func TestCanvasStyleMissingCSS(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.style", map[string]any{
		"session_id": "sess-1",
		"selector":   ".box",
	})
	if err == nil {
		t.Fatal("expected error for missing css")
	}
}

// ---------------------------------------------------------------------------
// Tests — canvas.dom
// ---------------------------------------------------------------------------

func TestCanvasDOMSetHTML(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.dom", map[string]any{
		"session_id": "sess-1",
		"operation":  "set_html",
		"selector":   "#content",
		"value":      "<p>New content</p>",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.dom set_html ok = %v, want true", out["ok"])
	}
	if out["operation"] != "set_html" {
		t.Fatalf("canvas.dom operation = %v, want set_html", out["operation"])
	}
	last := mock.lastAction()
	expr, _ := last.Params["expression"].(string)
	if !strings.Contains(expr, "innerHTML") {
		t.Fatalf("expected innerHTML in JS, got %q", expr)
	}
}

func TestCanvasDOMSetAttr(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.dom", map[string]any{
		"session_id": "sess-1",
		"operation":  "set_attr",
		"selector":   "img",
		"attr_name":  "src",
		"value":      "logo.png",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.dom set_attr ok = %v, want true", out["ok"])
	}
	last := mock.lastAction()
	expr, _ := last.Params["expression"].(string)
	if !strings.Contains(expr, "setAttribute") {
		t.Fatalf("expected setAttribute in JS, got %q", expr)
	}
}

func TestCanvasDOMCreate(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.dom", map[string]any{
		"session_id":      "sess-1",
		"operation":       "create",
		"selector":        "new-el",
		"tag":             "div",
		"parent_selector": "#container",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.dom create ok = %v, want true", out["ok"])
	}
	last := mock.lastAction()
	expr, _ := last.Params["expression"].(string)
	if !strings.Contains(expr, "createElement") {
		t.Fatalf("expected createElement in JS, got %q", expr)
	}
	if !strings.Contains(expr, "appendChild") {
		t.Fatalf("expected appendChild in JS, got %q", expr)
	}
}

func TestCanvasDOMCreateDefaultParent(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.dom", map[string]any{
		"session_id": "sess-1",
		"operation":  "create",
		"selector":   "new-el",
		"tag":        "span",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.dom create (default parent) ok = %v, want true", out["ok"])
	}
	last := mock.lastAction()
	expr, _ := last.Params["expression"].(string)
	// When parent_selector is omitted, it should default to "body".
	if !strings.Contains(expr, `querySelector("body")`) {
		t.Fatalf("expected default parent 'body' in JS, got %q", expr)
	}
}

func TestCanvasDOMSetAttrMissingAttrName(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.dom", map[string]any{
		"session_id": "sess-1",
		"operation":  "set_attr",
		"selector":   "#el",
		"value":      "some-value",
	})
	if err == nil {
		t.Fatal("expected error for set_attr without attr_name")
	}
	if !strings.Contains(err.Error(), "attr_name") {
		t.Fatalf("expected attr_name in error, got %q", err.Error())
	}
}

func TestCanvasDOMCreateMissingTag(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.dom", map[string]any{
		"session_id": "sess-1",
		"operation":  "create",
		"selector":   "new-el",
	})
	if err == nil {
		t.Fatal("expected error for create without tag")
	}
	if !strings.Contains(err.Error(), "tag") {
		t.Fatalf("expected 'tag' in error, got %q", err.Error())
	}
}

func TestCanvasDOMRemove(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.dom", map[string]any{
		"session_id": "sess-1",
		"operation":  "remove",
		"selector":   "#old-el",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.dom remove ok = %v, want true", out["ok"])
	}
	last := mock.lastAction()
	expr, _ := last.Params["expression"].(string)
	if !strings.Contains(expr, ".remove()") {
		t.Fatalf("expected .remove() in JS, got %q", expr)
	}
}

func TestCanvasDOMUnknownOperation(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.dom", map[string]any{
		"session_id": "sess-1",
		"operation":  "flip",
		"selector":   "#el",
	})
	if err == nil {
		t.Fatal("expected error for unknown DOM operation")
	}
	if !strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("expected 'unknown operation' in error, got %q", err.Error())
	}
}

func TestCanvasDOMMissingSelector(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.dom", map[string]any{
		"session_id": "sess-1",
		"operation":  "set_html",
	})
	if err == nil {
		t.Fatal("expected error for missing selector")
	}
}

// ---------------------------------------------------------------------------
// Tests — canvas.click
// ---------------------------------------------------------------------------

func TestCanvasClick(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.click", map[string]any{
		"session_id": "sess-1",
		"selector":   "#btn",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.click ok = %v, want true", out["ok"])
	}
	if out["selector"] != "#btn" {
		t.Fatalf("canvas.click selector = %v, want #btn", out["selector"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionClick {
		t.Fatalf("expected click action, got %q", last.Action)
	}
	if last.Params["selector"] != "#btn" {
		t.Fatalf("expected selector #btn in params, got %v", last.Params["selector"])
	}
}

func TestCanvasClickMissingSelector(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.click", map[string]any{
		"session_id": "sess-1",
	})
	if err == nil {
		t.Fatal("expected error for missing selector")
	}
}

// ---------------------------------------------------------------------------
// Tests — canvas.type
// ---------------------------------------------------------------------------

func TestCanvasType(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.type", map[string]any{
		"session_id": "sess-1",
		"selector":   "#input-name",
		"text":       "John Doe",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.type ok = %v, want true", out["ok"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionType {
		t.Fatalf("expected type action, got %q", last.Action)
	}
	if last.Params["selector"] != "#input-name" {
		t.Fatalf("expected selector #input-name, got %v", last.Params["selector"])
	}
	if last.Params["text"] != "John Doe" {
		t.Fatalf("expected text 'John Doe', got %v", last.Params["text"])
	}
}

func TestCanvasTypeMissingText(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.type", map[string]any{
		"session_id": "sess-1",
		"selector":   "#input",
	})
	if err == nil {
		t.Fatal("expected error for missing text")
	}
}

// ---------------------------------------------------------------------------
// Tests — canvas.scroll
// ---------------------------------------------------------------------------

func TestCanvasScrollBySelector(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.scroll", map[string]any{
		"session_id": "sess-1",
		"selector":   "#footer",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.scroll ok = %v, want true", out["ok"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionScroll {
		t.Fatalf("expected scroll action, got %q", last.Action)
	}
	if last.Params["selector"] != "#footer" {
		t.Fatalf("expected selector #footer, got %v", last.Params["selector"])
	}
}

func TestCanvasScrollByCoords(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.scroll", map[string]any{
		"session_id": "sess-1",
		"x":          0.0,
		"y":          500.0,
	})
	if out["ok"] != true {
		t.Fatalf("canvas.scroll ok = %v, want true", out["ok"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionScroll {
		t.Fatalf("expected scroll action, got %q", last.Action)
	}
	if last.Params["y"] != 500.0 {
		t.Fatalf("expected y=500 in params, got %v", last.Params["y"])
	}
	if last.Params["x"] != 0.0 {
		t.Fatalf("expected x=0 in params, got %v", last.Params["x"])
	}
}

// ---------------------------------------------------------------------------
// Tests — canvas.console
// ---------------------------------------------------------------------------

func TestCanvasConsoleStart(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.console", map[string]any{
		"session_id": "sess-1",
		"operation":  "start",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.console start ok = %v, want true", out["ok"])
	}
	if out["operation"] != "start" {
		t.Fatalf("canvas.console operation = %v, want start", out["operation"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionConsoleStart {
		t.Fatalf("expected console_start action, got %q", last.Action)
	}
}

func TestCanvasConsoleRead(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"messages": []any{"log: hello", "warn: oops"},
		},
	})
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.console", map[string]any{
		"session_id": "sess-1",
		"operation":  "read",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.console read ok = %v, want true", out["ok"])
	}
	msgs, ok := out["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("canvas.console read messages = %v, want 2 items", out["messages"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionConsoleMessages {
		t.Fatalf("expected console_messages action, got %q", last.Action)
	}
}

func TestCanvasConsoleClear(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.console", map[string]any{
		"session_id": "sess-1",
		"operation":  "clear",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.console clear ok = %v, want true", out["ok"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionEval {
		t.Fatalf("expected eval action for clear, got %q", last.Action)
	}
	expr, _ := last.Params["expression"].(string)
	if !strings.Contains(expr, "console.clear") {
		t.Fatalf("expected console.clear in expression, got %q", expr)
	}
}

func TestCanvasConsoleUnknownOperation(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.console", map[string]any{
		"session_id": "sess-1",
		"operation":  "flush",
	})
	if err == nil {
		t.Fatal("expected error for unknown console operation")
	}
}

// ---------------------------------------------------------------------------
// Tests — canvas.wait
// ---------------------------------------------------------------------------

func TestCanvasWaitForSelector(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(browsertypes.Response{
		OK:   true,
		Data: map[string]any{"matched": true},
	})
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.wait", map[string]any{
		"session_id": "sess-1",
		"selector":   "#loaded",
		"timeout_ms": 3000.0,
	})
	if out["ok"] != true {
		t.Fatalf("canvas.wait ok = %v, want true", out["ok"])
	}
	if out["matched"] != true {
		t.Fatalf("canvas.wait matched = %v, want true", out["matched"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionWaitFor {
		t.Fatalf("expected wait_for action, got %q", last.Action)
	}
	if last.Params["selector"] != "#loaded" {
		t.Fatalf("expected selector #loaded, got %v", last.Params["selector"])
	}
}

func TestCanvasWaitForExpression(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(browsertypes.Response{
		OK:   true,
		Data: map[string]any{"matched": true},
	})
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.wait", map[string]any{
		"session_id": "sess-1",
		"expression": "window.ready === true",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.wait ok = %v, want true", out["ok"])
	}
	last := mock.lastAction()
	if last.Params["expression"] != "window.ready === true" {
		t.Fatalf("expected expression in params, got %v", last.Params["expression"])
	}
}

func TestCanvasWaitDefaultTimeout(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(browsertypes.Response{OK: true, Data: map[string]any{"matched": true}})
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	_ = execCanvas(t, ctx, b, "canvas.wait", map[string]any{
		"session_id": "sess-1",
		"selector":   "#el",
	})
	last := mock.lastAction()
	timeout, _ := last.Params["timeout"].(float64)
	if int(timeout) != canvasDefaultWaitTimeoutMS {
		t.Fatalf("expected default timeout %d, got %v", canvasDefaultWaitTimeoutMS, timeout)
	}
}

func TestCanvasWaitCustomTimeoutInt(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(browsertypes.Response{OK: true, Data: map[string]any{"matched": true}})
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	_ = execCanvas(t, ctx, b, "canvas.wait", map[string]any{
		"session_id": "sess-1",
		"selector":   "#el",
		"timeout_ms": 8000,
	})
	last := mock.lastAction()
	timeout, _ := last.Params["timeout"].(float64)
	if int(timeout) != 8000 {
		t.Fatalf("expected custom timeout 8000, got %v", timeout)
	}
}

func TestCanvasWaitMatchedFalse(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(browsertypes.Response{
		OK:   true,
		Data: map[string]any{"matched": false},
	})
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.wait", map[string]any{
		"session_id": "sess-1",
		"selector":   "#missing",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.wait ok = %v, want true", out["ok"])
	}
	if out["matched"] != false {
		t.Fatalf("canvas.wait matched = %v, want false", out["matched"])
	}
}

func TestCanvasWaitMissingCondition(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.wait", map[string]any{
		"session_id": "sess-1",
	})
	if err == nil {
		t.Fatal("expected error when neither selector nor expression provided")
	}
	if !strings.Contains(err.Error(), "either selector or expression is required") {
		t.Fatalf("unexpected error message: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests — canvas.pdf
// ---------------------------------------------------------------------------

func TestCanvasPDF(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(browsertypes.Response{
		OK:          true,
		ArtifactRef: "artifact://pdf/abc123",
	})
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.pdf", map[string]any{
		"session_id": "sess-1",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.pdf ok = %v, want true", out["ok"])
	}
	if out["artifact_ref"] != "artifact://pdf/abc123" {
		t.Fatalf("canvas.pdf artifact_ref = %v, want artifact://pdf/abc123", out["artifact_ref"])
	}
	last := mock.lastAction()
	if last.Action != browsertypes.ActionPDF {
		t.Fatalf("expected pdf action, got %q", last.Action)
	}
}

func TestCanvasPDFMissingSessionID(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	err := execCanvasErr(t, ctx, b, "canvas.pdf", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing session_id")
	}
}

// ---------------------------------------------------------------------------
// Tests — canvas.eval nil result
// ---------------------------------------------------------------------------

func TestCanvasEvalNilResult(t *testing.T) {
	t.Parallel()
	mock := newMockBrowserServer()
	defer mock.close()
	mock.setResponse(browsertypes.Response{
		OK: true,
	})
	b := newCanvasTestBuiltins(t, mock)
	ctx := context.Background()

	out := execCanvas(t, ctx, b, "canvas.eval", map[string]any{
		"session_id": "sess-1",
		"expression": "void 0",
	})
	if out["ok"] != true {
		t.Fatalf("canvas.eval ok = %v, want true", out["ok"])
	}
	if out["result"] != nil {
		t.Fatalf("canvas.eval result = %v, want nil", out["result"])
	}
}

// ---------------------------------------------------------------------------
// Tests — error: no browser client
// ---------------------------------------------------------------------------

func TestCanvasNoBrowserClient(t *testing.T) {
	t.Parallel()
	b := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	ctx := context.Background()

	tools := []string{
		"canvas.present", "canvas.eval", "canvas.snapshot", "canvas.hide",
		"canvas.style", "canvas.dom", "canvas.click", "canvas.type",
		"canvas.scroll", "canvas.console", "canvas.wait", "canvas.pdf",
	}
	for _, tool := range tools {
		tool := tool
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			// Provide minimal input to pass parameter checks and reach browser check.
			input := map[string]any{"session_id": "s1"}
			switch tool {
			case "canvas.present":
				input["html"] = "<p>x</p>"
			case "canvas.eval":
				input["expression"] = "1"
			case "canvas.style":
				input["selector"] = ".x"
				input["css"] = map[string]any{"color": "red"}
			case "canvas.dom":
				input["operation"] = "remove"
				input["selector"] = ".x"
			case "canvas.click":
				input["selector"] = ".x"
			case "canvas.type":
				input["selector"] = ".x"
				input["text"] = "x"
			case "canvas.console":
				input["operation"] = "start"
			case "canvas.wait":
				input["selector"] = ".x"
			}
			err := execCanvasErr(t, ctx, b, tool, input)
			if err == nil {
				t.Fatalf("%s: expected error when no browser client", tool)
			}
			if !strings.Contains(err.Error(), "browser client not available") {
				t.Fatalf("%s: unexpected error: %v", tool, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests — tool count
// ---------------------------------------------------------------------------

func TestCanvasToolDefCount(t *testing.T) {
	t.Parallel()
	const expectedToolCount = 12
	defs := canvasToolDefs(BuiltinsConfig{})
	if len(defs) != expectedToolCount {
		t.Fatalf("canvasToolDefs returned %d tools, want %d", len(defs), expectedToolCount)
	}
	// Verify each has a non-empty name and handler.
	seen := make(map[string]bool)
	for _, d := range defs {
		if d.Manifest.Name == "" {
			t.Fatal("found tool def with empty name")
		}
		if d.Handler == nil {
			t.Fatalf("tool %q has nil handler", d.Manifest.Name)
		}
		if seen[d.Manifest.Name] {
			t.Fatalf("duplicate tool name %q", d.Manifest.Name)
		}
		seen[d.Manifest.Name] = true
	}
}

// ---------------------------------------------------------------------------
// Tests — jsStringLiteral helper
// ---------------------------------------------------------------------------

func TestJsStringLiteral(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{"hello", `"hello"`},
		{`it's "quoted"`, `"it's \"quoted\""`},
		{"line\nnewline", `"line\nnewline"`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("input=%q", tc.input), func(t *testing.T) {
			t.Parallel()
			got := jsStringLiteral(tc.input)
			if got != tc.want {
				t.Fatalf("jsStringLiteral(%q) = %s, want %s", tc.input, got, tc.want)
			}
		})
	}
}
