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
// text.json tests
// ---------------------------------------------------------------------------

func TestTextJSONParseInline(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-json",
		Name: "text.json",
		Input: map[string]any{
			"input": `{"name":"alice","age":30}`,
		},
	}})
	if err != nil {
		t.Fatalf("text.json error = %v", err)
	}
	var payload struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(payload.Result, &data); err != nil {
		t.Fatalf("Unmarshal result error = %v", err)
	}
	if data["name"] != "alice" {
		t.Fatalf("data.name = %v", data["name"])
	}
}

func TestTextJSONParseFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.json"), []byte(`{"key":"value"}`), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-json-file",
		Name: "text.json",
		Input: map[string]any{
			"file": "data.json",
		},
	}})
	if err != nil {
		t.Fatalf("text.json error = %v", err)
	}
	var payload struct {
		Result json.RawMessage `json:"result"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	var data map[string]any
	if err := json.Unmarshal(payload.Result, &data); err != nil {
		t.Fatalf("Unmarshal result error = %v", err)
	}
	if data["key"] != "value" {
		t.Fatalf("data.key = %v", data["key"])
	}
}

// ---------------------------------------------------------------------------
// text.yaml tests
// ---------------------------------------------------------------------------

func TestTextYAMLParse(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-yaml",
		Name: "text.yaml",
		Input: map[string]any{
			"input": "name: alice\nage: 30\n",
		},
	}})
	if err != nil {
		t.Fatalf("text.yaml error = %v", err)
	}
	var payload struct {
		Result json.RawMessage `json:"result"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	var data map[string]any
	if err := json.Unmarshal(payload.Result, &data); err != nil {
		t.Fatalf("Unmarshal result error = %v", err)
	}
	if data["name"] != "alice" {
		t.Fatalf("data.name = %v", data["name"])
	}
}

// ---------------------------------------------------------------------------
// text.csv tests
// ---------------------------------------------------------------------------

func TestTextCSVParse(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-csv",
		Name: "text.csv",
		Input: map[string]any{
			"input": "name,age\nalice,30\nbob,25\n",
		},
	}})
	if err != nil {
		t.Fatalf("text.csv error = %v", err)
	}
	var payload struct {
		Rows    [][]string `json:"rows"`
		Headers []string   `json:"headers"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if len(payload.Headers) != 2 || payload.Headers[0] != "name" {
		t.Fatalf("headers = %v", payload.Headers)
	}
}

// ---------------------------------------------------------------------------
// text.regex tests
// ---------------------------------------------------------------------------

func TestTextRegexMatch(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-regex",
		Name: "text.regex",
		Input: map[string]any{
			"input":   "hello world 123 foo 456",
			"pattern": `\d+`,
		},
	}})
	if err != nil {
		t.Fatalf("text.regex error = %v", err)
	}
	var payload struct {
		Matches []string `json:"matches"`
		Count   int      `json:"count"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Count != 2 {
		t.Fatalf("count = %d, want 2", payload.Count)
	}
	if payload.Matches[0] != "123" || payload.Matches[1] != "456" {
		t.Fatalf("matches = %v", payload.Matches)
	}
}

func TestTextRegexReplace(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-regex-replace",
		Name: "text.regex",
		Input: map[string]any{
			"input":   "hello world",
			"pattern": `world`,
			"mode":    "replace",
			"replace": "universe",
		},
	}})
	if err != nil {
		t.Fatalf("text.regex error = %v", err)
	}
	var payload struct {
		Result string `json:"result"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Result != "hello universe" {
		t.Fatalf("result = %q, want 'hello universe'", payload.Result)
	}
}

// ---------------------------------------------------------------------------
// text.base64 / text.hex encode-decode tests
// ---------------------------------------------------------------------------

func TestTextBase64Encode(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-b64-encode",
		Name: "text.base64",
		Input: map[string]any{
			"input": "hello world",
		},
	}})
	if err != nil {
		t.Fatalf("text.base64 error = %v", err)
	}
	var payload struct {
		Result string `json:"result"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Result != "aGVsbG8gd29ybGQ=" {
		t.Fatalf("result = %q, want aGVsbG8gd29ybGQ=", payload.Result)
	}
}

func TestTextBase64Decode(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-b64-decode",
		Name: "text.base64",
		Input: map[string]any{
			"input":  "aGVsbG8gd29ybGQ=",
			"decode": true,
		},
	}})
	if err != nil {
		t.Fatalf("text.base64 error = %v", err)
	}
	var payload struct {
		Result string `json:"result"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Result != "hello world" {
		t.Fatalf("decoded = %q, want 'hello world'", payload.Result)
	}
}

func TestTextHexEncode(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-hex-encode",
		Name: "text.hex",
		Input: map[string]any{
			"input": "test",
		},
	}})
	if err != nil {
		t.Fatalf("text.hex error = %v", err)
	}
	var payload struct {
		Result string `json:"result"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Result != "74657374" {
		t.Fatalf("result = %q, want 74657374", payload.Result)
	}
}

func TestTextHexDecode(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-hex-decode",
		Name: "text.hex",
		Input: map[string]any{
			"input":  "74657374",
			"decode": true,
		},
	}})
	if err != nil {
		t.Fatalf("text.hex error = %v", err)
	}
	var payload struct {
		Result string `json:"result"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Result != "test" {
		t.Fatalf("decoded = %q, want 'test'", payload.Result)
	}
}

// ---------------------------------------------------------------------------
// text.count tests
// ---------------------------------------------------------------------------

func TestTextCount(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-count",
		Name: "text.count",
		Input: map[string]any{
			"input": "hello world\nsecond line\n",
		},
	}})
	if err != nil {
		t.Fatalf("text.count error = %v", err)
	}
	var payload struct {
		Characters int `json:"characters"`
		Lines      int `json:"lines"`
		Words      int `json:"words"`
		Bytes      int `json:"bytes"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Lines < 2 {
		t.Fatalf("lines = %d, want >= 2", payload.Lines)
	}
	if payload.Words < 3 {
		t.Fatalf("words = %d, want >= 3", payload.Words)
	}
	if payload.Bytes != 24 { // "hello world\nsecond line\n" = 24 bytes
		t.Fatalf("bytes = %d, want 24", payload.Bytes)
	}
}

// ---------------------------------------------------------------------------
// text.hash tests
// ---------------------------------------------------------------------------

func TestTextHash(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-text-hash",
		Name: "text.hash",
		Input: map[string]any{
			"input":     "hello",
			"algorithm": "sha256",
		},
	}})
	if err != nil {
		t.Fatalf("text.hash error = %v", err)
	}
	var payload struct {
		Hash      string `json:"hash"`
		Algorithm string `json:"algorithm"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Algorithm != "sha256" {
		t.Fatalf("algorithm = %q", payload.Algorithm)
	}
	want := sha256Hex([]byte("hello"))
	if payload.Hash != want {
		t.Fatalf("hash = %q, want %q", payload.Hash, want)
	}
}

// ---------------------------------------------------------------------------
// text.xml tests
// ---------------------------------------------------------------------------

func TestTextXMLParse(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-xml",
		Name: "text.xml",
		Input: map[string]any{
			"input": `<root><item>hello</item></root>`,
		},
	}})
	if err != nil {
		t.Fatalf("text.xml error = %v", err)
	}
	if len(results[0].Content) == 0 {
		t.Fatal("result should not be empty")
	}
}
