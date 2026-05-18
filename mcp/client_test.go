package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test timeouts
// ---------------------------------------------------------------------------

const (
	testTimeout = 5 * time.Second
)

// mockServer reads requests from serverReader and writes responses to
// serverWriter, simulating an MCP server for client tests.
type mockServer struct {
	transport *Transport
	tools     []Tool
	mu        sync.Mutex
	calls     []CallToolParams
}

func newMockServer(r io.Reader, w io.Writer) *mockServer {
	return &mockServer{
		transport: NewTransport(r, w),
		tools: []Tool{
			{
				Name:        "echo",
				Description: "echoes input",
				InputSchema: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
			},
			{
				Name:        "fail",
				Description: "always fails",
				InputSchema: map[string]any{"type": "object"},
			},
		},
	}
}

func (m *mockServer) serve(t *testing.T) {
	t.Helper()
	for {
		msg, err := m.transport.Receive()
		if err != nil {
			return
		}
		if msg.IsNotification() {
			continue
		}

		var resp *JSONRPCMessage
		switch msg.Method {
		case MethodInitialize:
			resp = m.handleInitialize(msg.ID)
		case MethodToolsList:
			resp = m.handleToolsList(msg.ID)
		case MethodToolsCall:
			resp = m.handleToolsCall(msg.ID, msg.Params)
		default:
			resp = errorResponse(msg.ID, ErrCodeMethodNotFound, "not found")
		}
		if err := m.transport.Send(resp); err != nil {
			return
		}
	}
}

func (m *mockServer) handleInitialize(id any) *JSONRPCMessage {
	return successResponse(id, InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    ServerCapabilities{Tools: &ToolsCapability{}},
		ServerInfo:      Implementation{Name: "mock-server", Version: "1.0.0"},
	})
}

func (m *mockServer) handleToolsList(id any) *JSONRPCMessage {
	return successResponse(id, ToolListResult{Tools: m.tools})
}

func (m *mockServer) handleToolsCall(id any, params json.RawMessage) *JSONRPCMessage {
	var p CallToolParams
	if err := json.Unmarshal(params, &p); err != nil {
		return errorResponse(id, ErrCodeInvalidParams, "bad params")
	}

	m.mu.Lock()
	m.calls = append(m.calls, p)
	m.mu.Unlock()

	if p.Name == "fail" {
		return successResponse(id, CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: "tool error"}},
			IsError: true,
		})
	}

	text, _ := p.Arguments["text"].(string)
	return successResponse(id, CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: "echo: " + text}},
	})
}

// setupClientServer creates paired in-process pipes wired to a client and
// mock server. The mock server is started in a background goroutine.
func setupClientServer(t *testing.T) *Client {
	t.Helper()

	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	mock := newMockServer(serverReader, serverWriter)
	go mock.serve(t)

	transport := NewTransport(clientReader, clientWriter)
	client := newClientFromTransport(transport)

	t.Cleanup(func() {
		transport.Close()
		clientReader.Close()
		clientWriter.Close()
		serverReader.Close()
		serverWriter.Close()
	})

	return client
}

func TestClientInitialize(t *testing.T) {
	t.Parallel()

	client := setupClientServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	result, err := client.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if result.ServerInfo.Name != "mock-server" {
		t.Errorf("ServerInfo.Name = %q, want %q", result.ServerInfo.Name, "mock-server")
	}
	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("ProtocolVersion = %q, want %q", result.ProtocolVersion, ProtocolVersion)
	}
	if result.Capabilities.Tools == nil {
		t.Error("Capabilities.Tools is nil, want non-nil")
	}
}

func TestClientListTools(t *testing.T) {
	t.Parallel()

	client := setupClientServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("len(tools) = %d, want 2", len(tools))
	}

	found := false
	for _, tool := range tools {
		if tool.Name == "echo" {
			found = true
			if tool.Description != "echoes input" {
				t.Errorf("echo tool description = %q, want %q", tool.Description, "echoes input")
			}
		}
	}
	if !found {
		t.Error("tool 'echo' not found in list")
	}
}

func TestClientCallTool(t *testing.T) {
	t.Parallel()

	client := setupClientServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	result, err := client.CallTool(ctx, "echo", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Error("CallTool() returned IsError = true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(result.Content))
	}
	if result.Content[0].Text != "echo: hello" {
		t.Errorf("Content[0].Text = %q, want %q", result.Content[0].Text, "echo: hello")
	}
}

func TestClientCallToolError(t *testing.T) {
	t.Parallel()

	client := setupClientServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	result, err := client.CallTool(ctx, "fail", nil)
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !result.IsError {
		t.Error("CallTool() returned IsError = false, want true")
	}
	if len(result.Content) == 0 {
		t.Fatal("Content is empty")
	}
	if result.Content[0].Text != "tool error" {
		t.Errorf("Content[0].Text = %q, want %q", result.Content[0].Text, "tool error")
	}
}

func TestBuildServerCommandEnvResolvesRefsAndSanitizesHostEnv(t *testing.T) {
	t.Setenv("HOPCLAW_MCP_SECRET", "mcp-secret")
	t.Setenv("HOPCLAW_MCP_LEAK", "host-only")

	env, err := buildServerCommandEnv(ServerConfig{
		Name:    "demo",
		Command: "demo-server",
		Env: map[string]string{
			"TOKEN": "env:HOPCLAW_MCP_SECRET",
			"MODE":  "literal",
		},
	})
	if err != nil {
		t.Fatalf("buildServerCommandEnv() error = %v", err)
	}
	if got := envSliceValue(env, "TOKEN"); got != "mcp-secret" {
		t.Fatalf("TOKEN = %q, want %q", got, "mcp-secret")
	}
	if got := envSliceValue(env, "MODE"); got != "literal" {
		t.Fatalf("MODE = %q, want %q", got, "literal")
	}
	if got := envSliceValue(env, "HOPCLAW_MCP_LEAK"); got != "" {
		t.Fatalf("unexpected host env leak = %q", got)
	}
	if got := envSliceValue(env, "PATH"); got == "" {
		t.Fatal("PATH should be present in child env")
	}
}

func TestClientContextCancellation(t *testing.T) {
	t.Parallel()

	// Create a server that never responds.
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	_ = serverWriter
	_ = serverReader

	transport := NewTransport(clientReader, clientWriter)
	client := newClientFromTransport(transport)

	t.Cleanup(func() {
		transport.Close()
		clientReader.Close()
		clientWriter.Close()
		serverReader.Close()
		serverWriter.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Drain the server side to prevent write blocking.
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverReader.Read(buf); err != nil {
				return
			}
		}
	}()

	_, err := client.Initialize(ctx)
	if err == nil {
		t.Fatal("Initialize() expected error due to context cancellation")
	}
}

func TestClientSupervisorRestartsAfterCrashAndRecovers(t *testing.T) {
	setClientRestartPolicyForTest(t, 20*time.Millisecond, 50*time.Millisecond, 200*time.Millisecond, 5)

	stateFile := filepath.Join(t.TempDir(), "helper-state.txt")
	client, err := NewClient(helperServerConfig(stateFile, "crash-once"))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := client.ListTools(ctx); err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}

	_, err = client.CallTool(ctx, "echo", map[string]any{"text": "hello"})
	if err == nil {
		t.Fatal("CallTool() expected temporary error after crash")
	}
	if !IsTemporary(err) {
		t.Fatalf("CallTool() error = %v, want temporary", err)
	}

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		result, callErr := client.CallTool(context.Background(), "echo", map[string]any{"text": "recovered"})
		if callErr == nil {
			if len(result.Content) != 1 || result.Content[0].Text != "echo: recovered" {
				t.Fatalf("recovered CallTool() result = %#v", result)
			}
			if got := helperRunCount(t, stateFile); got < 2 {
				t.Fatalf("helper run count = %d, want at least 2", got)
			}
			return
		}
		if !IsTemporary(callErr) {
			t.Fatalf("CallTool() during recovery returned non-temporary error: %v", callErr)
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("timed out waiting for MCP client to recover after crash")
}

func TestClientSupervisorMarksClientDeadAfterMaxRestarts(t *testing.T) {
	setClientRestartPolicyForTest(t, 10*time.Millisecond, 20*time.Millisecond, 100*time.Millisecond, 2)

	stateFile := filepath.Join(t.TempDir(), "helper-state.txt")
	client, err := NewClient(helperServerConfig(stateFile, "exit-immediately"))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		_, initErr := client.Initialize(context.Background())
		if initErr == nil {
			t.Fatal("Initialize() unexpectedly succeeded")
		}
		if IsTemporary(initErr) {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if !strings.Contains(strings.ToLower(initErr.Error()), "dead") {
			t.Fatalf("Initialize() error = %v, want dead-state error", initErr)
		}
		if got := helperRunCount(t, stateFile); got < 3 {
			t.Fatalf("helper run count = %d, want at least 3", got)
		}
		return
	}

	t.Fatal("timed out waiting for MCP client to enter dead state")
}

func TestMCPSupervisorHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_HELPER_PROCESS") != "1" {
		return
	}

	mode := strings.TrimSpace(os.Getenv("HOPCLAW_MCP_HELPER_MODE"))
	stateFile := strings.TrimSpace(os.Getenv("HOPCLAW_MCP_STATE_FILE"))
	run := incrementHelperRun(stateFile)
	if mode == "exit-immediately" {
		os.Exit(2)
	}

	transport := NewTransport(os.Stdin, os.Stdout)
	for {
		msg, err := transport.Receive()
		if err != nil {
			return
		}
		if msg.IsNotification() {
			continue
		}

		switch msg.Method {
		case MethodInitialize:
			if err := transport.Send(successResponse(msg.ID, InitializeResult{
				ProtocolVersion: ProtocolVersion,
				Capabilities:    ServerCapabilities{Tools: &ToolsCapability{}},
				ServerInfo:      Implementation{Name: "helper", Version: strconv.Itoa(run)},
			})); err != nil {
				return
			}
		case MethodToolsList:
			if err := transport.Send(successResponse(msg.ID, ToolListResult{
				Tools: []Tool{{
					Name:        "echo",
					Description: "echo test tool",
					InputSchema: map[string]any{"type": "object"},
				}},
			})); err != nil {
				return
			}
		case MethodToolsCall:
			if mode == "crash-once" && run == 1 {
				os.Exit(3)
			}
			var params CallToolParams
			if err := json.Unmarshal(msg.Params, &params); err != nil {
				_ = transport.Send(errorResponse(msg.ID, ErrCodeInvalidParams, "bad params"))
				continue
			}
			text, _ := params.Arguments["text"].(string)
			if err := transport.Send(successResponse(msg.ID, CallToolResult{
				Content: []ContentBlock{{Type: "text", Text: "echo: " + text}},
			})); err != nil {
				return
			}
		default:
			if err := transport.Send(errorResponse(msg.ID, ErrCodeMethodNotFound, "not found")); err != nil {
				return
			}
		}
	}
}

func helperServerConfig(stateFile, mode string) ServerConfig {
	return ServerConfig{
		Name:    "helper",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestMCPSupervisorHelperProcess", "--"},
		Env: map[string]string{
			"GO_WANT_MCP_HELPER_PROCESS": "1",
			"HOPCLAW_MCP_HELPER_MODE":    mode,
			"HOPCLAW_MCP_STATE_FILE":     stateFile,
		},
	}
}

func setClientRestartPolicyForTest(t *testing.T, initial, max, healthy time.Duration, attempts int) {
	t.Helper()

	oldInitial := clientRestartInitialBackoff
	oldMax := clientRestartMaxBackoff
	oldHealthy := clientRestartHealthyDuration
	oldAttempts := clientRestartMaxAttempts

	clientRestartInitialBackoff = initial
	clientRestartMaxBackoff = max
	clientRestartHealthyDuration = healthy
	clientRestartMaxAttempts = attempts

	t.Cleanup(func() {
		clientRestartInitialBackoff = oldInitial
		clientRestartMaxBackoff = oldMax
		clientRestartHealthyDuration = oldHealthy
		clientRestartMaxAttempts = oldAttempts
	})
}

func incrementHelperRun(path string) int {
	count := 0
	if data, err := os.ReadFile(path); err == nil {
		count, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	count++
	_ = os.WriteFile(path, []byte(strconv.Itoa(count)), 0o644)
	return count
}

func helperRunCount(t *testing.T, path string) int {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("Atoi(%q): %v", string(data), err)
	}
	return count
}

func envSliceValue(env []string, key string) string {
	for _, entry := range env {
		currentKey, value, ok := strings.Cut(entry, "=")
		if ok && currentKey == key {
			return value
		}
	}
	return ""
}
