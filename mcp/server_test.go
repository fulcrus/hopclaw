package mcp

import (
	"context"
	"encoding/json"
	"io"
	"testing"
)

// ---------------------------------------------------------------------------
// stubToolProvider implements ToolProvider for server tests.
// ---------------------------------------------------------------------------

type stubToolProvider struct {
	tools []Tool
}

func (s *stubToolProvider) ListTools(_ context.Context) ([]Tool, error) {
	return s.tools, nil
}

func (s *stubToolProvider) CallTool(_ context.Context, name string, args map[string]any) (*CallToolResult, error) {
	text, _ := args["text"].(string)
	return &CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: name + ": " + text}},
	}, nil
}

// roundTrip sends a request through one pipe end and reads the server's
// response from the other.
func roundTrip(t *testing.T, clientTransport *Transport, method string, id any, params any) *JSONRPCMessage {
	t.Helper()

	var rawParams json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		rawParams = data
	}

	msg := &JSONRPCMessage{
		JSONRPC: JSONRPC,
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}
	if err := clientTransport.Send(msg); err != nil {
		t.Fatalf("send: %v", err)
	}

	resp, err := clientTransport.Receive()
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	return resp
}

// setupServer starts a server in a background goroutine with in-process pipes
// and returns a transport connected to the client side of the pipes.
func setupServer(t *testing.T, provider ToolProvider) *Transport {
	t.Helper()

	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	srv := NewServer(provider, Implementation{Name: "test-server", Version: "0.1.0"})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		clientReader.Close()
		clientWriter.Close()
		serverReader.Close()
		serverWriter.Close()
	})

	go func() {
		srv.Serve(ctx, serverReader, serverWriter)
	}()

	return NewTransport(clientReader, clientWriter)
}

func TestServerInitialize(t *testing.T) {
	t.Parallel()

	provider := &stubToolProvider{}
	clientTransport := setupServer(t, provider)

	resp := roundTrip(t, clientTransport, MethodInitialize, float64(1), InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientInfo:      Implementation{Name: "test-client", Version: "0.1.0"},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.ServerInfo.Name != "test-server" {
		t.Errorf("ServerInfo.Name = %q, want %q", result.ServerInfo.Name, "test-server")
	}
	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("ProtocolVersion = %q, want %q", result.ProtocolVersion, ProtocolVersion)
	}
}

func TestServerListTools(t *testing.T) {
	t.Parallel()

	provider := &stubToolProvider{
		tools: []Tool{
			{Name: "alpha", Description: "first tool", InputSchema: map[string]any{"type": "object"}},
			{Name: "beta", Description: "second tool", InputSchema: map[string]any{"type": "object"}},
		},
	}
	clientTransport := setupServer(t, provider)

	// Initialize first.
	roundTrip(t, clientTransport, MethodInitialize, float64(1), InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientInfo:      Implementation{Name: "test", Version: "0.1.0"},
	})

	resp := roundTrip(t, clientTransport, MethodToolsList, float64(2), nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}

	var result ToolListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Tools) != 2 {
		t.Fatalf("len(Tools) = %d, want 2", len(result.Tools))
	}
}

func TestServerCallTool(t *testing.T) {
	t.Parallel()

	provider := &stubToolProvider{}
	clientTransport := setupServer(t, provider)

	// Initialize first.
	roundTrip(t, clientTransport, MethodInitialize, float64(1), InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientInfo:      Implementation{Name: "test", Version: "0.1.0"},
	})

	resp := roundTrip(t, clientTransport, MethodToolsCall, float64(2), CallToolParams{
		Name:      "greet",
		Arguments: map[string]any{"text": "world"},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}

	var result CallToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(result.Content))
	}
	expected := "greet: world"
	if result.Content[0].Text != expected {
		t.Errorf("Content[0].Text = %q, want %q", result.Content[0].Text, expected)
	}
}

func TestServerUnknownMethod(t *testing.T) {
	t.Parallel()

	provider := &stubToolProvider{}
	clientTransport := setupServer(t, provider)

	resp := roundTrip(t, clientTransport, "unknown/method", float64(1), nil)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != ErrCodeMethodNotFound {
		t.Errorf("Error.Code = %d, want %d", resp.Error.Code, ErrCodeMethodNotFound)
	}
}
