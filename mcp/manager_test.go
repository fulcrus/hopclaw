package mcp

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// inProcessClient creates a Client wired to a mock server via in-process pipes.
// ---------------------------------------------------------------------------

func inProcessClient(t *testing.T, tools []Tool) *Client {
	t.Helper()

	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	mock := &mockServer{
		transport: NewTransport(serverReader, serverWriter),
		tools:     tools,
	}
	go mock.serve(t)

	transport := NewTransport(clientReader, clientWriter)
	client := newClientFromTransport(transport)
	client.proc.stdin = clientWriter
	client.proc.stdout = clientReader

	t.Cleanup(func() {
		transport.Close()
		clientReader.Close()
		clientWriter.Close()
		serverReader.Close()
		serverWriter.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := client.ListTools(ctx); err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}

	return client
}

// setupManagerWithMockClients creates a Manager and manually injects mock
// clients, bypassing the process spawning in Start().
func setupManagerWithMockClients(t *testing.T) *Manager {
	t.Helper()

	manager := NewManager([]ServerConfig{
		{Name: "server-a"},
		{Name: "server-b"},
	})

	clientA := inProcessClient(t, []Tool{
		{Name: "echo", Description: "echoes input", InputSchema: map[string]any{"type": "object"}},
		{Name: "list", Description: "lists items", InputSchema: map[string]any{"type": "object"}},
	})
	clientB := inProcessClient(t, []Tool{
		{Name: "search", Description: "searches things", InputSchema: map[string]any{"type": "object"}},
	})

	manager.mu.Lock()
	manager.clients["server-a"] = clientA
	manager.clients["server-b"] = clientB
	manager.mu.Unlock()

	return manager
}

func TestManagerTools(t *testing.T) {
	t.Parallel()

	manager := setupManagerWithMockClients(t)
	tools := manager.Tools()

	if len(tools) != 3 {
		t.Fatalf("len(Tools) = %d, want 3", len(tools))
	}

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	expected := []string{
		"server-a__echo",
		"server-a__list",
		"server-b__search",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected tool %q not found", name)
		}
	}
}

func TestManagerCallToolRouting(t *testing.T) {
	t.Parallel()

	manager := setupManagerWithMockClients(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	result, err := manager.CallTool(ctx, "server-a__echo", map[string]any{"text": "routed"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(result.Content))
	}
	if result.Content[0].Text != "echo: routed" {
		t.Errorf("Content[0].Text = %q, want %q", result.Content[0].Text, "echo: routed")
	}
}

func TestManagerCallToolUnprefixed(t *testing.T) {
	t.Parallel()

	manager := setupManagerWithMockClients(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// "search" is unique to server-b, so it should resolve without prefix.
	result, err := manager.CallTool(ctx, "search", map[string]any{"text": "query"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(result.Content))
	}
}

func TestManagerCallToolNotFound(t *testing.T) {
	t.Parallel()

	manager := setupManagerWithMockClients(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	_, err := manager.CallTool(ctx, "nonexistent", nil)
	if err == nil {
		t.Fatal("CallTool() expected error for nonexistent tool")
	}
}

func TestManagerStatus(t *testing.T) {
	t.Parallel()

	manager := setupManagerWithMockClients(t)
	status := manager.Status()

	if len(status) != 2 {
		t.Fatalf("len(Status) = %d, want 2", len(status))
	}

	sa := status["server-a"]
	if !sa.Connected {
		t.Error("server-a should be connected")
	}
	if sa.Tools != 2 {
		t.Errorf("server-a Tools = %d, want 2", sa.Tools)
	}

	sb := status["server-b"]
	if !sb.Connected {
		t.Error("server-b should be connected")
	}
	if sb.Tools != 1 {
		t.Errorf("server-b Tools = %d, want 1", sb.Tools)
	}
}

func TestManagerAddServerKeepsExistingServersCallable(t *testing.T) {
	previousFactory := newManagerClient
	newManagerClient = func(cfg ServerConfig) (*Client, error) {
		switch cfg.Name {
		case "server-a":
			return inProcessClient(t, []Tool{{
				Name:        "echo",
				Description: "echoes input",
				InputSchema: map[string]any{"type": "object"},
			}}), nil
		case "server-b":
			return inProcessClient(t, []Tool{{
				Name:        "search",
				Description: "searches things",
				InputSchema: map[string]any{"type": "object"},
			}}), nil
		default:
			return nil, nil
		}
	}
	defer func() {
		newManagerClient = previousFactory
	}()

	manager := NewManager(nil)
	t.Cleanup(func() {
		_ = manager.Stop()
	})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	if err := manager.AddServer(ctx, ServerConfig{Name: "server-a", Command: "server-a"}); err != nil {
		t.Fatalf("AddServer(server-a) error = %v", err)
	}
	if err := manager.AddServer(ctx, ServerConfig{Name: "server-b", Command: "server-b"}); err != nil {
		t.Fatalf("AddServer(server-b) error = %v", err)
	}

	if _, err := manager.CallTool(ctx, "server-a__echo", map[string]any{"text": "alpha"}); err != nil {
		t.Fatalf("CallTool(server-a__echo) error = %v", err)
	}
	if _, err := manager.CallTool(ctx, "server-b__search", map[string]any{"text": "beta"}); err != nil {
		t.Fatalf("CallTool(server-b__search) error = %v", err)
	}

	configs := manager.ServerConfigs()
	if len(configs) != 2 {
		t.Fatalf("len(ServerConfigs) = %d, want 2", len(configs))
	}
}

func TestManagerRemoveServerReturnsNotFoundAndKeepsOtherServersRunning(t *testing.T) {
	previousFactory := newManagerClient
	newManagerClient = func(cfg ServerConfig) (*Client, error) {
		switch cfg.Name {
		case "server-a", "server-b":
			return inProcessClient(t, []Tool{{
				Name:        "echo",
				Description: "echoes input",
				InputSchema: map[string]any{"type": "object"},
			}}), nil
		default:
			return nil, nil
		}
	}
	defer func() {
		newManagerClient = previousFactory
	}()

	manager := NewManager(nil)
	t.Cleanup(func() {
		_ = manager.Stop()
	})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	if err := manager.AddServer(ctx, ServerConfig{Name: "server-a", Command: "server-a"}); err != nil {
		t.Fatalf("AddServer(server-a) error = %v", err)
	}
	if err := manager.AddServer(ctx, ServerConfig{Name: "server-b", Command: "server-b"}); err != nil {
		t.Fatalf("AddServer(server-b) error = %v", err)
	}
	if err := manager.RemoveServer("server-a"); err != nil {
		t.Fatalf("RemoveServer(server-a) error = %v", err)
	}

	if _, err := manager.CallTool(ctx, "server-a__echo", map[string]any{"text": "alpha"}); err == nil || !strings.Contains(err.Error(), `mcp server "server-a" not found`) {
		t.Fatalf("CallTool(server-a__echo) error = %v, want not found", err)
	}
	if _, err := manager.CallTool(ctx, "server-b__echo", map[string]any{"text": "beta"}); err != nil {
		t.Fatalf("CallTool(server-b__echo) error = %v", err)
	}

	configs := manager.ServerConfigs()
	if len(configs) != 1 || configs[0].Name != "server-b" {
		t.Fatalf("ServerConfigs() = %#v, want [server-b]", configs)
	}
}

func TestManagerAddServerKeepsServerConfigsSorted(t *testing.T) {
	previousFactory := newManagerClient
	newManagerClient = func(ServerConfig) (*Client, error) {
		return inProcessClient(t, []Tool{{
			Name:        "echo",
			Description: "echoes input",
			InputSchema: map[string]any{"type": "object"},
		}}), nil
	}
	defer func() {
		newManagerClient = previousFactory
	}()

	manager := NewManager(nil)
	t.Cleanup(func() {
		_ = manager.Stop()
	})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	if err := manager.AddServer(ctx, ServerConfig{Name: "server-b", Command: "server-b"}); err != nil {
		t.Fatalf("AddServer(server-b) error = %v", err)
	}
	if err := manager.AddServer(ctx, ServerConfig{Name: "server-a", Command: "server-a"}); err != nil {
		t.Fatalf("AddServer(server-a) error = %v", err)
	}

	configs := manager.ServerConfigs()
	got := []string{configs[0].Name, configs[1].Name}
	want := []string{"server-a", "server-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("server config order = %#v, want %#v", got, want)
	}
}

// Ensure json.RawMessage is used (compile-time check).
var _ json.RawMessage
