package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func withGatewayClientStub(t *testing.T, fn roundTripFunc) {
	t.Helper()
	old := newGatewayClient
	newGatewayClient = func() (*GatewayClient, error) {
		return &GatewayClient{
			BaseURL: "http://gateway.test",
			HTTP:    &http.Client{Transport: fn},
		}, nil
	}
	t.Cleanup(func() {
		newGatewayClient = old
	})
}

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestMemorySetCmdSupportsAutoKeyGlobalAndLabel(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		gotPath = req.URL.Path
		gotMethod = req.Method
		if err := json.NewDecoder(req.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, `{"key":"mem_auto_1","value":"部署地址是 198.51.100.42"}`), nil
	})

	restore := captureStdout(t)
	cmd := newMemorySetCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--global", "--label", "infra", "部署地址是 198.51.100.42"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/runtime/memory/records" {
		t.Fatalf("path = %s, want /runtime/memory/records", gotPath)
	}
	if _, ok := gotBody["key"]; ok {
		t.Fatalf("unexpected key in body: %#v", gotBody)
	}
	if gotBody["value"] != "部署地址是 198.51.100.42" {
		t.Fatalf("value = %#v", gotBody["value"])
	}
	if gotBody["source"] != "user" {
		t.Fatalf("source = %#v", gotBody["source"])
	}
	if gotBody["namespace"] != "global" {
		t.Fatalf("namespace = %#v", gotBody["namespace"])
	}
	if gotBody["label"] != "infra" {
		t.Fatalf("label = %#v", gotBody["label"])
	}

	output := restore()
	if !strings.Contains(output, "✓ 已记住: mem_auto_1 = 部署地址是 198.51.100.42") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestMemorySetCmdSupportsExplicitKey(t *testing.T) {
	var gotBody map[string]any
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(req.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, `{"key":"deploy_server","value":"198.51.100.42"}`), nil
	})

	cmd := newMemorySetCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"deploy_server", "198.51.100.42"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	if gotBody["key"] != "deploy_server" {
		t.Fatalf("key = %#v", gotBody["key"])
	}
	if gotBody["namespace"] != "project" {
		t.Fatalf("namespace = %#v", gotBody["namespace"])
	}
}

func TestMemoryDeleteCmdCancelsWithoutConfirmation(t *testing.T) {
	var deleteCalled bool
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		deleteCalled = true
		return jsonHTTPResponse(http.StatusOK, `{"ok":true}`), nil
	})

	cmd := newMemoryDeleteCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"deploy_server"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if deleteCalled {
		t.Fatal("expected delete request to be skipped")
	}
	text := out.String()
	if !strings.Contains(text, `Delete memory "deploy_server"? [y/N] `) {
		t.Fatalf("prompt missing from output: %q", text)
	}
	if !strings.Contains(text, "Cancelled.") {
		t.Fatalf("cancel message missing from output: %q", text)
	}
}

func TestMemoryDeleteCmdErrorsInNonInteractiveModeWithoutYes(t *testing.T) {
	var deleteCalled bool
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		deleteCalled = true
		return jsonHTTPResponse(http.StatusOK, `{"ok":true}`), nil
	})

	cmd := newMemoryDeleteCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"deploy_server"})

	err := cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("ExecuteContext() error = %v, want confirmation guidance", err)
	}
	if deleteCalled {
		t.Fatal("expected delete request to be skipped")
	}
	if strings.Contains(out.String(), "Cancelled.") {
		t.Fatalf("output = %q, want hard error instead of cancellation", out.String())
	}
}

func TestMemoryDeleteCmdDeletesAfterConfirmation(t *testing.T) {
	var deleteCalled bool
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		deleteCalled = true
		if req.Method != http.MethodDelete {
			t.Fatalf("method = %s, want DELETE", req.Method)
		}
		if req.URL.Path != "/runtime/memory/deploy_server" {
			t.Fatalf("path = %s, want /runtime/memory/deploy_server", req.URL.Path)
		}
		return jsonHTTPResponse(http.StatusOK, `{"ok":true}`), nil
	})

	restore := captureStdout(t)
	cmd := newMemoryDeleteCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetArgs([]string{"deploy_server"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !deleteCalled {
		t.Fatal("expected delete request")
	}
	if !strings.Contains(out.String(), `Delete memory "deploy_server"? [y/N] `) {
		t.Fatalf("prompt missing from output: %q", out.String())
	}
	if !strings.Contains(restore(), "deleted deploy_server") {
		t.Fatal("expected delete confirmation on stdout")
	}
}

func TestMemorySearchCmdEscapesQuery(t *testing.T) {
	var gotQuery string
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		gotQuery = req.URL.RawQuery
		return jsonHTTPResponse(http.StatusOK, `{"items":[{"key":"deploy_note","value":"hello world"}]}`), nil
	})

	restore := captureStdout(t)
	if err := runMemorySearch(context.Background(), "hello world"); err != nil {
		t.Fatalf("runMemorySearch() error = %v", err)
	}
	if gotQuery != "q=hello+world" {
		t.Fatalf("RawQuery = %q, want %q", gotQuery, "q=hello+world")
	}
	if !strings.Contains(restore(), "deploy_note") {
		t.Fatal("expected search result on stdout")
	}
}

func TestMemoryStatusCmdUsesStatusEndpoint(t *testing.T) {
	var gotPath string
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		gotPath = req.URL.Path
		return jsonHTTPResponse(http.StatusOK, `{"store_type":"in-memory","entry_count":2,"index_ready":true}`), nil
	})

	restore := captureStdout(t)
	if err := runMemoryStatus(context.Background()); err != nil {
		t.Fatalf("runMemoryStatus() error = %v", err)
	}
	if gotPath != "/runtime/memory/status" {
		t.Fatalf("path = %s, want /runtime/memory/status", gotPath)
	}
	output := restore()
	if !strings.Contains(output, "Store type:  in-memory") || !strings.Contains(output, "Entries:     2") || !strings.Contains(output, "Index:       ready") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestMemoryIndexCmdPostsForceFlag(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
		gotBody   memoryIndexRequest
	)
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		gotPath = req.URL.Path
		gotMethod = req.Method
		if err := json.NewDecoder(req.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, `{"status":"rebuilt_forced","indexed":3}`), nil
	})

	restore := captureStdout(t)
	if err := runMemoryIndex(context.Background(), true); err != nil {
		t.Fatalf("runMemoryIndex() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/runtime/memory/index" {
		t.Fatalf("path = %s, want /runtime/memory/index", gotPath)
	}
	if !gotBody.Force {
		t.Fatal("expected force flag in request body")
	}
	if got := strings.TrimSpace(restore()); got != "reindex rebuilt_forced: 3 entries indexed" {
		t.Fatalf("stdout = %q", got)
	}
}
