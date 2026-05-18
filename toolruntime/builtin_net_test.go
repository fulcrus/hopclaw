package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

// ---------------------------------------------------------------------------
// net.http tests
// ---------------------------------------------------------------------------

func TestNetHTTPGet(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer server.Close()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-http-get",
		Name: "net.http",
		Input: map[string]any{
			"url":    server.URL,
			"method": "GET",
		},
	}})
	if err != nil {
		t.Fatalf("net.http error = %v", err)
	}

	var payload struct {
		StatusCode int    `json:"status_code"`
		Body       string `json:"body"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if payload.StatusCode != 200 {
		t.Fatalf("status_code = %d, want 200", payload.StatusCode)
	}
	if !strings.Contains(payload.Body, "ok") {
		t.Fatalf("body = %q", payload.Body)
	}
}

func TestNetHTTPPost(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"123"}`)
	}))
	defer server.Close()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-http-post",
		Name: "net.http",
		Input: map[string]any{
			"url":    server.URL,
			"method": "POST",
			"body":   `{"name":"test"}`,
			"headers": map[string]any{
				"Content-Type": "application/json",
			},
		},
	}})
	if err != nil {
		t.Fatalf("net.http error = %v", err)
	}

	var payload struct {
		StatusCode int `json:"status_code"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.StatusCode != 201 {
		t.Fatalf("status_code = %d, want 201", payload.StatusCode)
	}
}

func TestNetHTTPCustomHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "test-val" {
			t.Fatalf("missing custom header, got %q", r.Header.Get("X-Custom"))
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-http-headers",
		Name: "net.http",
		Input: map[string]any{
			"url":    server.URL,
			"method": "GET",
			"headers": map[string]any{
				"X-Custom": "test-val",
			},
		},
	}})
	if err != nil {
		t.Fatalf("net.http error = %v", err)
	}

	var payload struct {
		StatusCode int `json:"status_code"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.StatusCode != 200 {
		t.Fatalf("status_code = %d", payload.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// net.fetch tests
// ---------------------------------------------------------------------------

func TestNetFetch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><p>Hello, world!</p></body></html>`)
	}))
	defer server.Close()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-fetch",
		Name: "net.fetch",
		Input: map[string]any{
			"url": server.URL,
		},
	}})
	if err != nil {
		t.Fatalf("net.fetch error = %v", err)
	}

	var payload struct {
		Content    string `json:"content"`
		StatusCode int    `json:"status_code"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if !strings.Contains(payload.Content, "Hello") {
		t.Fatalf("content = %q, should contain Hello", payload.Content)
	}
}

// ---------------------------------------------------------------------------
// net.download tests
// ---------------------------------------------------------------------------

func TestNetDownload(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("binary content here"))
	}))
	defer server.Close()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-download",
		Name: "net.download",
		Input: map[string]any{
			"url":  server.URL + "/file.bin",
			"path": "downloaded.bin",
		},
	}})
	if err != nil {
		t.Fatalf("net.download error = %v", err)
	}

	var payload struct {
		Path         string `json:"path"`
		BytesWritten int64  `json:"bytes_written"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.BytesWritten != 19 { // len("binary content here")
		t.Fatalf("bytes_written = %d, want 19", payload.BytesWritten)
	}

	data, err := os.ReadFile(filepath.Join(root, "downloaded.bin"))
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	if string(data) != "binary content here" {
		t.Fatalf("downloaded content = %q", string(data))
	}
}

// ---------------------------------------------------------------------------
// net.dns tests
// ---------------------------------------------------------------------------

func TestNetDNSLookup(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-dns",
		Name: "net.dns",
		Input: map[string]any{
			"host": "localhost",
			"type": "A",
		},
	}})
	if err != nil {
		t.Fatalf("net.dns error = %v", err)
	}

	var payload struct {
		Host    string   `json:"host"`
		Type    string   `json:"type"`
		Records []string `json:"records"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Host != "localhost" {
		t.Fatalf("host = %q", payload.Host)
	}
	// localhost should resolve to 127.0.0.1 on most systems.
	if len(payload.Records) == 0 {
		t.Fatal("expected at least one DNS record for localhost")
	}
}

// ---------------------------------------------------------------------------
// net.ping tests
// ---------------------------------------------------------------------------

func TestNetPingLocalhost(t *testing.T) {
	t.Parallel()

	// Start a TCP server to ping.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Extract host and port from the server URL.
	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.SplitN(addr, ":", 2)
	host := parts[0]
	port := 0
	if len(parts) == 2 {
		fmt.Sscanf(parts[1], "%d", &port)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-ping",
		Name: "net.ping",
		Input: map[string]any{
			"host": host,
			"port": port,
		},
	}})
	if err != nil {
		t.Fatalf("net.ping error = %v", err)
	}

	var payload struct {
		Host      string `json:"host"`
		Reachable bool   `json:"reachable"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if !payload.Reachable {
		t.Fatal("localhost server should be reachable")
	}
}
