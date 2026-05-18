package acp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestACP_InitializeHandshake(t *testing.T) {
	t.Parallel()

	_, client := startProtocolFlowServer(t, newMockGatewayClient())

	initializeProtocolFlow(t, client)

	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("initialize returned error: %#v", resp.Error)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Unmarshal(initialize result) error = %v", err)
	}
	if result.ProtocolVersion != protocolVersion {
		t.Fatalf("result.ProtocolVersion = %q, want %q", result.ProtocolVersion, protocolVersion)
	}
	if result.Capabilities == nil {
		t.Fatal("expected capabilities in initialize result")
	}
	if streaming, _ := result.Capabilities["streaming"].(bool); !streaming {
		t.Fatalf("capabilities = %#v, want streaming=true", result.Capabilities)
	}
	if permissions, _ := result.Capabilities["permissions"].(bool); !permissions {
		t.Fatalf("capabilities = %#v, want permissions=true", result.Capabilities)
	}
	if commands, _ := result.Capabilities["commands"].(bool); !commands {
		t.Fatalf("capabilities = %#v, want commands=true", result.Capabilities)
	}
}

func TestACP_NewSessionAndPrompt(t *testing.T) {
	t.Parallel()

	gw := newMockGatewayClient()
	_, client := startProtocolFlowServer(t, gw)

	initializeProtocolFlow(t, client)
	if resp := receiveResponse(t, client); resp.Error != nil {
		t.Fatalf("initialize returned error: %#v", resp.Error)
	}

	sendRequest(t, client, 2, "acp/newSession", NewSessionParams{
		SessionID: "flow-session",
	})
	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("newSession returned error: %#v", resp.Error)
	}

	var info SessionInfo
	if err := json.Unmarshal(resp.Result, &info); err != nil {
		t.Fatalf("Unmarshal(newSession result) error = %v", err)
	}
	if info.SessionID != "flow-session" {
		t.Fatalf("info.SessionID = %q, want %q", info.SessionID, "flow-session")
	}

	sendRequest(t, client, 3, "acp/prompt", PromptParams{
		SessionID: "flow-session",
		Message:   "hello world",
	})
	resp = receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("prompt returned error: %#v", resp.Error)
	}

	var accepted map[string]string
	if err := json.Unmarshal(resp.Result, &accepted); err != nil {
		t.Fatalf("Unmarshal(prompt result) error = %v", err)
	}
	if accepted["status"] != "accepted" {
		t.Fatalf("accepted[status] = %q, want %q", accepted["status"], "accepted")
	}

	streaming := receiveSessionUpdateNotification(t, client)
	completed := receiveSessionUpdateNotification(t, client)
	if streaming.Status != SessionStreaming {
		t.Fatalf("streaming.Status = %q, want %q", streaming.Status, SessionStreaming)
	}
	if streaming.RunID != "run-1" {
		t.Fatalf("streaming.RunID = %q, want %q", streaming.RunID, "run-1")
	}
	if streaming.TextDelta != "hello" {
		t.Fatalf("streaming.TextDelta = %q, want %q", streaming.TextDelta, "hello")
	}
	if completed.Status != SessionCompleted {
		t.Fatalf("completed.Status = %q, want %q", completed.Status, SessionCompleted)
	}
	if completed.StopReason != StopEndTurn {
		t.Fatalf("completed.StopReason = %q, want %q", completed.StopReason, StopEndTurn)
	}

	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.submitCalled != 1 {
		t.Fatalf("submitCalled = %d, want 1", gw.submitCalled)
	}
	if gw.lastMessage != "hello world" {
		t.Fatalf("lastMessage = %q, want %q", gw.lastMessage, "hello world")
	}
}

func TestACP_CancelActiveRun(t *testing.T) {
	t.Parallel()

	gw := newMockGatewayClient()
	srv, client := startProtocolFlowServer(t, gw)

	initializeProtocolFlow(t, client)
	if resp := receiveResponse(t, client); resp.Error != nil {
		t.Fatalf("initialize returned error: %#v", resp.Error)
	}

	sendRequest(t, client, 2, "acp/newSession", NewSessionParams{
		SessionID: "cancel-flow-session",
	})
	if resp := receiveResponse(t, client); resp.Error != nil {
		t.Fatalf("newSession returned error: %#v", resp.Error)
	}

	sendRequest(t, client, 3, "acp/prompt", PromptParams{
		SessionID: "cancel-flow-session",
		Message:   "please run",
	})
	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("prompt returned error: %#v", resp.Error)
	}
	_ = receiveSessionUpdateNotification(t, client)
	_ = receiveSessionUpdateNotification(t, client)

	cancelInvoked := false
	srv.sessions.SetActiveRun("cancel-flow-session", "run-cancel-flow", func() {
		cancelInvoked = true
	})

	sendRequest(t, client, 4, "acp/cancel", CancelParams{
		SessionID: "cancel-flow-session",
	})
	resp = receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("cancel returned error: %#v", resp.Error)
	}

	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Unmarshal(cancel result) error = %v", err)
	}
	if result["status"] != "cancelled" {
		t.Fatalf("result[status] = %q, want %q", result["status"], "cancelled")
	}
	if gw.cancelCalled != 1 {
		t.Fatalf("cancelCalled = %d, want 1", gw.cancelCalled)
	}
	if len(gw.cancelledRuns) != 1 || gw.cancelledRuns[0] != "run-cancel-flow" {
		t.Fatalf("cancelledRuns = %#v, want [run-cancel-flow]", gw.cancelledRuns)
	}
	if !cancelInvoked {
		t.Fatal("expected active run cancel function to be invoked")
	}
}

func TestACP_ListSessions(t *testing.T) {
	t.Parallel()

	_, client := startProtocolFlowServer(t, newMockGatewayClient())

	initializeProtocolFlow(t, client)
	if resp := receiveResponse(t, client); resp.Error != nil {
		t.Fatalf("initialize returned error: %#v", resp.Error)
	}

	sendRequest(t, client, 2, "acp/newSession", NewSessionParams{SessionID: "list-session-1"})
	if resp := receiveResponse(t, client); resp.Error != nil {
		t.Fatalf("newSession(list-session-1) returned error: %#v", resp.Error)
	}
	sendRequest(t, client, 3, "acp/newSession", NewSessionParams{SessionID: "list-session-2"})
	if resp := receiveResponse(t, client); resp.Error != nil {
		t.Fatalf("newSession(list-session-2) returned error: %#v", resp.Error)
	}

	sendRequest(t, client, 4, "acp/listSessions", ListSessionsParams{Limit: 10})
	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("listSessions returned error: %#v", resp.Error)
	}

	var result struct {
		Sessions []SessionInfo `json:"sessions"`
		Count    int           `json:"count"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Unmarshal(listSessions result) error = %v", err)
	}
	if result.Count != 2 {
		t.Fatalf("result.Count = %d, want 2", result.Count)
	}
	if len(result.Sessions) != 2 {
		t.Fatalf("len(result.Sessions) = %d, want 2", len(result.Sessions))
	}
}

func TestACP_UnknownMethod(t *testing.T) {
	t.Parallel()

	_, client := startProtocolFlowServer(t, newMockGatewayClient())

	sendRequest(t, client, 1, "acp/unknownMethod", map[string]string{"foo": "bar"})
	resp := receiveResponse(t, client)
	if resp.Error == nil {
		t.Fatal("expected unknown method to return an error")
	}
	if resp.Error.Code != errCodeMethodNotFound {
		t.Fatalf("resp.Error.Code = %d, want %d", resp.Error.Code, errCodeMethodNotFound)
	}
}

func TestACP_SetConfigOption(t *testing.T) {
	t.Parallel()

	srv, client := startProtocolFlowServer(t, newMockGatewayClient())

	initializeProtocolFlow(t, client)
	if resp := receiveResponse(t, client); resp.Error != nil {
		t.Fatalf("initialize returned error: %#v", resp.Error)
	}

	sendRequest(t, client, 2, "acp/newSession", NewSessionParams{
		SessionID: "config-flow-session",
	})
	if resp := receiveResponse(t, client); resp.Error != nil {
		t.Fatalf("newSession returned error: %#v", resp.Error)
	}

	sendRequest(t, client, 3, "acp/setConfigOption", SetConfigOptionParams{
		SessionID: "config-flow-session",
		Key:       ConfigReasoningLevel,
		Value:     "high",
	})
	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("setConfigOption returned error: %#v", resp.Error)
	}

	options := srv.sessions.ConfigOptions("config-flow-session")
	if got := options[ConfigReasoningLevel]; got != "high" {
		t.Fatalf("ConfigOptions()[%q] = %q, want %q", ConfigReasoningLevel, got, "high")
	}
}

func startProtocolFlowServer(t *testing.T, gw *mockGatewayClient) (*Server, *Transport) {
	t.Helper()

	clientR, clientW, serverR, serverW := pipePair()
	srv := NewServer(gw, ServerConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	go func() {
		_ = srv.Serve(ctx, serverR, serverW)
	}()

	client := NewTransport(clientR, clientW)
	t.Cleanup(func() {
		_ = client.Close()
	})

	return srv, client
}

func initializeProtocolFlow(t *testing.T, client *Transport) {
	t.Helper()

	sendRequest(t, client, 1, "initialize", InitializeParams{
		ProtocolVersion: protocolVersion,
		ClientInfo: Implementation{
			Name:    "protocol-flow-client",
			Version: "1.0.0",
		},
	})
}

func receiveSessionUpdateNotification(t *testing.T, client *Transport) SessionUpdateNotification {
	t.Helper()

	msg := receiveResponse(t, client)
	if msg.Method != "acp/sessionUpdate" {
		t.Fatalf("expected sessionUpdate notification, got method %q", msg.Method)
	}

	var update SessionUpdateNotification
	if err := json.Unmarshal(msg.Params, &update); err != nil {
		t.Fatalf("Unmarshal(sessionUpdate) error = %v", err)
	}
	return update
}
