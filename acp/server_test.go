package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
)

// ---------------------------------------------------------------------------
// Mock gateway client
// ---------------------------------------------------------------------------

type mockGatewayClient struct {
	mu       sync.Mutex // guards calls and configured responses
	sessions []SessionInfo
	runs     map[string][]RunEvent

	submitCalled           int
	cancelCalled           int
	cancelledRuns          []string
	lastMessage            string
	lastImages             []string
	lastContentBlocks      []contextengine.ContentBlock
	lastStructuredCommand  *StructuredCommand
	lastStructuredApproval *StructuredApproval
	resolvedApprovals      []approval.Resolution
	resolvedApprovalIDs    []string
}

func newMockGatewayClient() *mockGatewayClient {
	return &mockGatewayClient{
		runs: make(map[string][]RunEvent),
	}
}

func (m *mockGatewayClient) SubmitRun(_ context.Context, sessionKey, message string, images []string) (string, <-chan RunEvent, error) {
	return m.SubmitRunWithOptions(context.Background(), sessionKey, message, images, PromptOptions{})
}

func (m *mockGatewayClient) SubmitRunWithOptions(_ context.Context, sessionKey, message string, images []string, options PromptOptions) (string, <-chan RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.submitCalled++
	m.lastMessage = message
	m.lastImages = append([]string(nil), images...)
	m.lastContentBlocks = append([]contextengine.ContentBlock(nil), options.ContentBlocks...)
	if options.StructuredCommand != nil {
		cloned := *options.StructuredCommand
		m.lastStructuredCommand = &cloned
	} else {
		m.lastStructuredCommand = nil
	}
	if options.StructuredApproval != nil {
		cloned := *options.StructuredApproval
		m.lastStructuredApproval = &cloned
	} else {
		m.lastStructuredApproval = nil
	}

	runID := fmt.Sprintf("run-%d", m.submitCalled)
	events, ok := m.runs[sessionKey]
	if !ok {
		events = []RunEvent{
			{Type: "text_delta", Text: "hello"},
			{Type: "complete", StopReason: StopEndTurn},
		}
	}

	ch := make(chan RunEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return runID, ch, nil
}

func (m *mockGatewayClient) CancelRun(_ context.Context, _ string, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelCalled++
	m.cancelledRuns = append(m.cancelledRuns, runID)
	return nil
}

func (m *mockGatewayClient) ListSessions(_ context.Context, limit, offset int) ([]SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		return nil, fmt.Errorf("no sessions configured")
	}
	result := m.sessions
	if offset > 0 && offset < len(result) {
		result = result[offset:]
	}
	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockGatewayClient) ResolveSession(_ context.Context, key string) (*SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &SessionInfo{
		SessionID:  key,
		SessionKey: key,
		Status:     SessionIdle,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

func (m *mockGatewayClient) ResetSession(_ context.Context, _ string) error {
	return nil
}

func (m *mockGatewayClient) ResolveApproval(_ context.Context, requestID string, resolution approval.Resolution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolvedApprovalIDs = append(m.resolvedApprovalIDs, requestID)
	m.resolvedApprovals = append(m.resolvedApprovals, resolution)
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// pipePair creates a connected reader/writer pair for test transport.
func pipePair() (clientR io.Reader, clientW io.Writer, serverR io.Reader, serverW io.Writer) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	return cr, cw, sr, sw
}

// sendRequest writes a JSON-RPC request to the transport and returns the ID.
func sendRequest(t *testing.T, tr *Transport, id int, method string, params any) {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("failed to marshal params: %v", err)
	}
	msg := &JSONRPCMessage{
		JSONRPC: jsonrpcVersion,
		ID:      float64(id),
		HasID:   true,
		Method:  method,
		Params:  data,
	}
	if err := tr.Send(msg); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
}

// receiveResponse reads a JSON-RPC message from the transport.
func receiveResponse(t *testing.T, tr *Transport) *JSONRPCMessage {
	t.Helper()
	msg, err := tr.Receive()
	if err != nil {
		t.Fatalf("failed to receive response: %v", err)
	}
	return msg
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestServerInitialize(t *testing.T) {
	t.Parallel()

	clientR, clientW, serverR, serverW := pipePair()
	gw := newMockGatewayClient()
	srv := NewServer(gw, ServerConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.Serve(ctx, serverR, serverW)
	}()

	client := NewTransport(clientR, clientW)
	defer client.Close()

	sendRequest(t, client, 1, "initialize", InitializeParams{
		ProtocolVersion: protocolVersion,
		ClientInfo: Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		},
	})

	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("initialize returned error: %s", resp.Error.Message)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result.ProtocolVersion != protocolVersion {
		t.Fatalf("expected protocol version %q, got %q", protocolVersion, result.ProtocolVersion)
	}
	if result.ServerInfo.Name != serverName {
		t.Fatalf("expected server name %q, got %q", serverName, result.ServerInfo.Name)
	}

	cancel()
}

func TestServerNewSession(t *testing.T) {
	t.Parallel()

	clientR, clientW, serverR, serverW := pipePair()
	gw := newMockGatewayClient()
	srv := NewServer(gw, ServerConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.Serve(ctx, serverR, serverW)
	}()

	client := NewTransport(clientR, clientW)
	defer client.Close()

	sendRequest(t, client, 1, "acp/newSession", NewSessionParams{
		SessionID: "test-session-1",
	})

	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("newSession returned error: %s", resp.Error.Message)
	}

	var info SessionInfo
	if err := json.Unmarshal(resp.Result, &info); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if info.SessionID != "test-session-1" {
		t.Fatalf("expected session ID %q, got %q", "test-session-1", info.SessionID)
	}
	if info.Status != SessionIdle {
		t.Fatalf("expected status %q, got %q", SessionIdle, info.Status)
	}

	cancel()
}

func TestServerPromptAndStream(t *testing.T) {
	t.Parallel()

	clientR, clientW, serverR, serverW := pipePair()
	gw := newMockGatewayClient()
	srv := NewServer(gw, ServerConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.Serve(ctx, serverR, serverW)
	}()

	client := NewTransport(clientR, clientW)
	defer client.Close()

	// Create session first.
	sendRequest(t, client, 1, "acp/newSession", NewSessionParams{
		SessionID: "stream-session",
	})
	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("newSession returned error: %s", resp.Error.Message)
	}

	// Send prompt.
	sendRequest(t, client, 2, "acp/prompt", PromptParams{
		SessionID: "stream-session",
		Message:   "hello",
	})

	// Receive the acceptance response.
	resp = receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("prompt returned error: %s", resp.Error.Message)
	}

	// Collect streaming notifications.
	var updates []SessionUpdateNotification
	for i := 0; i < 2; i++ {
		msg := receiveResponse(t, client)
		if msg.Method != "acp/sessionUpdate" {
			t.Fatalf("expected sessionUpdate notification, got method %q", msg.Method)
		}
		var update SessionUpdateNotification
		if err := json.Unmarshal(msg.Params, &update); err != nil {
			t.Fatalf("failed to unmarshal session update: %v", err)
		}
		updates = append(updates, update)
	}

	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}
	if updates[0].Status != SessionStreaming {
		t.Fatalf("expected first update status %q, got %q", SessionStreaming, updates[0].Status)
	}
	if updates[0].TextDelta != "hello" {
		t.Fatalf("expected text delta %q, got %q", "hello", updates[0].TextDelta)
	}
	if updates[1].Status != SessionCompleted {
		t.Fatalf("expected last update status %q, got %q", SessionCompleted, updates[1].Status)
	}
	if updates[1].StopReason != StopEndTurn {
		t.Fatalf("expected stop reason %q, got %q", StopEndTurn, updates[1].StopReason)
	}

	cancel()
}

func TestInProcessClientCloseWithUnreadNotifications(t *testing.T) {
	t.Parallel()

	client, err := NewInProcessClient(context.Background(), NewServer(newMockGatewayClient(), ServerConfig{}))
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}

	if _, err := client.NewSession(context.Background(), NewSessionParams{SessionID: "close-session"}); err != nil {
		_ = client.Close()
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := client.Prompt(context.Background(), PromptParams{
		SessionID: "close-session",
		Message:   "hello",
	}); err != nil {
		_ = client.Close()
		t.Fatalf("Prompt() error = %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, ok := <-client.Notifications(); ok {
		t.Fatal("Notifications() should be closed after Close")
	}
}

func TestServerPromptAcceptsImageOnlyInput(t *testing.T) {
	t.Parallel()

	clientR, clientW, serverR, serverW := pipePair()
	gw := newMockGatewayClient()
	srv := NewServer(gw, ServerConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.Serve(ctx, serverR, serverW)
	}()

	client := NewTransport(clientR, clientW)
	defer client.Close()

	sendRequest(t, client, 1, "acp/newSession", NewSessionParams{
		SessionID: "image-session",
	})
	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("newSession returned error: %s", resp.Error.Message)
	}

	sendRequest(t, client, 2, "acp/prompt", PromptParams{
		SessionID: "image-session",
		Images:    []string{"data:image/png;base64,ZmFrZQ=="},
	})

	resp = receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("prompt returned error: %s", resp.Error.Message)
	}

	msg := receiveResponse(t, client)
	if msg.Method != "acp/sessionUpdate" {
		t.Fatalf("expected sessionUpdate notification, got method %q", msg.Method)
	}

	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.lastMessage != "" {
		t.Fatalf("lastMessage = %q, want empty string for image-only prompt", gw.lastMessage)
	}
	if len(gw.lastImages) != 1 || gw.lastImages[0] != "data:image/png;base64,ZmFrZQ==" {
		t.Fatalf("lastImages = %#v", gw.lastImages)
	}
}

func TestServerPromptAcceptsContentBlocksOnlyInput(t *testing.T) {
	t.Parallel()

	clientR, clientW, serverR, serverW := pipePair()
	gw := newMockGatewayClient()
	srv := NewServer(gw, ServerConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.Serve(ctx, serverR, serverW)
	}()

	client := NewTransport(clientR, clientW)
	defer client.Close()

	sendRequest(t, client, 1, "acp/newSession", NewSessionParams{SessionID: "blocks-session"})
	if resp := receiveResponse(t, client); resp.Error != nil {
		t.Fatalf("newSession returned error: %s", resp.Error.Message)
	}

	blocks := []contextengine.ContentBlock{
		{Type: contextengine.ContentBlockText, Text: "inspect this"},
		{Type: contextengine.ContentBlockFile, Label: "spec.md", Path: "/tmp/spec.md"},
	}
	sendRequest(t, client, 2, "acp/prompt", PromptParams{
		SessionID:     "blocks-session",
		ContentBlocks: blocks,
	})

	if resp := receiveResponse(t, client); resp.Error != nil {
		t.Fatalf("prompt returned error: %s", resp.Error.Message)
	}
	if msg := receiveResponse(t, client); msg.Method != "acp/sessionUpdate" {
		t.Fatalf("expected sessionUpdate notification, got method %q", msg.Method)
	}

	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.lastMessage != "" {
		t.Fatalf("lastMessage = %q, want empty string for block-only prompt", gw.lastMessage)
	}
	if len(gw.lastContentBlocks) != len(blocks) {
		t.Fatalf("lastContentBlocks = %#v, want %#v", gw.lastContentBlocks, blocks)
	}
}

func TestServerPromptForwardsStructuredRetryAndRunID(t *testing.T) {
	t.Parallel()

	clientR, clientW, serverR, serverW := pipePair()
	gw := newMockGatewayClient()
	srv := NewServer(gw, ServerConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.Serve(ctx, serverR, serverW)
	}()

	client := NewTransport(clientR, clientW)
	defer client.Close()

	sendRequest(t, client, 1, "acp/newSession", NewSessionParams{SessionID: "retry-session"})
	if resp := receiveResponse(t, client); resp.Error != nil {
		t.Fatalf("newSession returned error: %s", resp.Error.Message)
	}

	sendRequest(t, client, 2, "acp/prompt", PromptParams{
		SessionID: "retry-session",
		StructuredCommand: &StructuredCommand{
			Kind:  "retry",
			RunID: "run-source-1",
		},
	})

	if resp := receiveResponse(t, client); resp.Error != nil {
		t.Fatalf("prompt returned error: %s", resp.Error.Message)
	}
	msg := receiveResponse(t, client)
	if msg.Method != "acp/sessionUpdate" {
		t.Fatalf("expected sessionUpdate notification, got method %q", msg.Method)
	}
	var update SessionUpdateNotification
	if err := json.Unmarshal(msg.Params, &update); err != nil {
		t.Fatalf("failed to unmarshal session update: %v", err)
	}
	if update.RunID != "run-1" {
		t.Fatalf("update.RunID = %q, want run-1", update.RunID)
	}

	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.lastStructuredCommand == nil {
		t.Fatal("lastStructuredCommand = nil, want structured retry")
	}
	if gw.lastStructuredCommand.Kind != "retry" || gw.lastStructuredCommand.RunID != "run-source-1" {
		t.Fatalf("lastStructuredCommand = %#v", gw.lastStructuredCommand)
	}
}

func TestServerCancel(t *testing.T) {
	t.Parallel()

	clientR, clientW, serverR, serverW := pipePair()
	gw := newMockGatewayClient()
	srv := NewServer(gw, ServerConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.Serve(ctx, serverR, serverW)
	}()

	client := NewTransport(clientR, clientW)
	defer client.Close()

	// Create session.
	sendRequest(t, client, 1, "acp/newSession", NewSessionParams{
		SessionID: "cancel-session",
	})
	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("newSession returned error: %s", resp.Error.Message)
	}

	cancelInvoked := false
	srv.sessions.SetActiveRun("cancel-session", "run-123", func() {
		cancelInvoked = true
	})

	sendRequest(t, client, 2, "acp/cancel", CancelParams{
		SessionID: "cancel-session",
	})
	resp = receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("cancel returned error: %s", resp.Error.Message)
	}
	if gw.cancelCalled != 1 {
		t.Fatalf("cancelCalled = %d, want 1", gw.cancelCalled)
	}
	if len(gw.cancelledRuns) != 1 || gw.cancelledRuns[0] != "run-123" {
		t.Fatalf("cancelledRuns = %#v, want [run-123]", gw.cancelledRuns)
	}
	if !cancelInvoked {
		t.Fatal("expected active run cancel function to be invoked")
	}

	cancel()
}

func TestServerSetConfigOptionPersistsValue(t *testing.T) {
	t.Parallel()

	clientR, clientW, serverR, serverW := pipePair()
	gw := newMockGatewayClient()
	srv := NewServer(gw, ServerConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.Serve(ctx, serverR, serverW)
	}()

	client := NewTransport(clientR, clientW)
	defer client.Close()

	sendRequest(t, client, 1, "acp/newSession", NewSessionParams{
		SessionID: "cfg-session",
	})
	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("newSession returned error: %s", resp.Error.Message)
	}

	sendRequest(t, client, 2, "acp/setConfigOption", SetConfigOptionParams{
		SessionID: "cfg-session",
		Key:       ConfigReasoningLevel,
		Value:     "high",
	})
	resp = receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("setConfigOption returned error: %s", resp.Error.Message)
	}

	options := srv.sessions.ConfigOptions("cfg-session")
	if got := options[ConfigReasoningLevel]; got != "high" {
		t.Fatalf("ConfigOptions()[%q] = %q, want %q", ConfigReasoningLevel, got, "high")
	}
}

func TestServerSendsCommandsUpdateOnSessionLifecycle(t *testing.T) {
	t.Parallel()

	clientR, clientW, serverR, serverW := pipePair()
	gw := newMockGatewayClient()
	srv := NewServer(gw, ServerConfig{
		CommandProvider: func(context.Context) ([]Command, error) {
			return []Command{
				{Name: "review-pr", Description: "Review a PR", Shortcut: "/review-pr"},
				{Name: "help", Description: "Override help"},
			}, nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.Serve(ctx, serverR, serverW)
	}()

	client := NewTransport(clientR, clientW)
	defer client.Close()

	sendRequest(t, client, 1, "acp/newSession", NewSessionParams{
		SessionID: "commands-session",
	})
	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("newSession returned error: %s", resp.Error.Message)
	}

	msg := receiveResponse(t, client)
	assertCommandsUpdate(t, msg, []string{"review-pr", "help", "status"})

	sendRequest(t, client, 2, "acp/prompt", PromptParams{
		SessionID: "commands-session",
		Message:   "hello",
	})
	resp = receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("prompt returned error: %s", resp.Error.Message)
	}

	var commandUpdates int
	var statuses []SessionStatus
	for len(statuses) < 2 || commandUpdates < 3 {
		msg = receiveResponse(t, client)
		switch msg.Method {
		case "acp/sessionUpdate":
			var update SessionUpdateNotification
			if err := json.Unmarshal(msg.Params, &update); err != nil {
				t.Fatalf("failed to unmarshal session update: %v", err)
			}
			statuses = append(statuses, update.Status)
		case "acp/commandsUpdate":
			commandUpdates++
			assertCommandsUpdate(t, msg, []string{"review-pr", "help", "status"})
		default:
			t.Fatalf("unexpected notification method %q", msg.Method)
		}
	}

	if commandUpdates != 3 {
		t.Fatalf("commandUpdates = %d, want 3", commandUpdates)
	}
	if len(statuses) != 2 {
		t.Fatalf("len(statuses) = %d, want 2", len(statuses))
	}
	if statuses[0] != SessionStreaming || statuses[1] != SessionCompleted {
		t.Fatalf("statuses = %#v, want [%q %q]", statuses, SessionStreaming, SessionCompleted)
	}
}

func assertCommandsUpdate(t *testing.T, msg *JSONRPCMessage, wantNames []string) {
	t.Helper()

	if msg.Method != "acp/commandsUpdate" {
		t.Fatalf("expected commandsUpdate notification, got method %q", msg.Method)
	}

	var payload struct {
		Commands []Command `json:"commands"`
	}
	if err := json.Unmarshal(msg.Params, &payload); err != nil {
		t.Fatalf("failed to unmarshal commands update: %v", err)
	}
	if len(payload.Commands) == 0 {
		t.Fatal("commandsUpdate contained no commands")
	}
	names := make([]string, 0, len(payload.Commands))
	for _, command := range payload.Commands {
		names = append(names, command.Name)
	}
	for _, want := range wantNames {
		found := false
		for _, name := range names {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("commands = %#v, want %q present", names, want)
		}
	}
	for _, command := range payload.Commands {
		if command.Name == "help" && command.Description != "Override help" {
			t.Fatalf("help command description = %q, want override from provider", command.Description)
		}
	}
}

func TestServerListSessions(t *testing.T) {
	t.Parallel()

	clientR, clientW, serverR, serverW := pipePair()
	gw := newMockGatewayClient()
	gw.sessions = []SessionInfo{
		{SessionID: "s1", SessionKey: "k1", Status: SessionIdle, CreatedAt: time.Now().UTC()},
		{SessionID: "s2", SessionKey: "k2", Status: SessionStreaming, CreatedAt: time.Now().UTC()},
	}
	srv := NewServer(gw, ServerConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.Serve(ctx, serverR, serverW)
	}()

	client := NewTransport(clientR, clientW)
	defer client.Close()

	sendRequest(t, client, 1, "acp/listSessions", ListSessionsParams{Limit: 10})
	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("listSessions returned error: %s", resp.Error.Message)
	}

	var result struct {
		Sessions []SessionInfo `json:"sessions"`
		Count    int           `json:"count"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result.Count != 2 {
		t.Fatalf("expected 2 sessions, got %d", result.Count)
	}

	cancel()
}

func TestPermissionHandlerSafe(t *testing.T) {
	t.Parallel()

	if NeedsPermission("fs.read") {
		t.Fatal("fs.read should not need permission")
	}
	if NeedsPermission("fs.grep") {
		t.Fatal("fs.grep should not need permission")
	}
	if NeedsPermission("env.info") {
		t.Fatal("env.info should not need permission")
	}
	if NeedsPermission("skill.list") {
		t.Fatal("skill.list should not need permission")
	}
}

func TestPermissionHandlerDangerous(t *testing.T) {
	t.Parallel()

	if !NeedsPermission("exec.shell") {
		t.Fatal("exec.shell should need permission")
	}
	if !NeedsPermission("fs.write") {
		t.Fatal("fs.write should need permission")
	}
	if !NeedsPermission("fs.delete") {
		t.Fatal("fs.delete should need permission")
	}
	if !NeedsPermission("db.execute") {
		t.Fatal("db.execute should need permission")
	}
	// Unknown tools should also need permission.
	if !NeedsPermission("unknown.tool") {
		t.Fatal("unknown tool should need permission")
	}
}

func TestSessionStoreReap(t *testing.T) {
	t.Parallel()

	store := &SessionStore{
		sessions: make(map[string]*session),
		done:     make(chan struct{}),
	}
	// Do not start the reap loop; we will invoke the reap logic directly.

	now := time.Now().UTC()
	store.sessions["active"] = &session{
		ID:            "active",
		GatewayKey:    "gw-active",
		CreatedAt:     now,
		LastTouchedAt: now,
	}
	store.sessions["stale"] = &session{
		ID:            "stale",
		GatewayKey:    "gw-stale",
		CreatedAt:     now.Add(-48 * time.Hour),
		LastTouchedAt: now.Add(-48 * time.Hour),
	}

	// Simulate a reap tick.
	store.mu.Lock()
	for id, sess := range store.sessions {
		if now.Sub(sess.LastTouchedAt) > sessionIdleTTL {
			if sess.Cancel != nil {
				sess.Cancel()
			}
			delete(store.sessions, id)
		}
	}
	store.mu.Unlock()

	if _, ok := store.sessions["active"]; !ok {
		t.Fatal("active session should not have been reaped")
	}
	if _, ok := store.sessions["stale"]; ok {
		t.Fatal("stale session should have been reaped")
	}
}
