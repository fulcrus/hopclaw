package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func TestWSHandleChatSendResolvesSessionIDToSessionKey(t *testing.T) {
	t.Parallel()

	gw := newGatewayWithBuiltins(t, t.TempDir())
	handler := NewWSHandler(gw, nil)

	seedRun, err := gw.runtime.Submit(context.Background(), runtimesvc.SubmitRequest{
		SessionKey: "ws:thread",
		Content:    "seed message",
		Model:      "test-model",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	raw, err := handler.handleChatSend(nil, json.RawMessage(`{"session_id":"`+seedRun.SessionID+`","content":"follow up"}`))
	if err != nil {
		t.Fatalf("handleChatSend() error = %v", err)
	}

	var payload chatSendResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	run, err := gw.runtime.GetRun(context.Background(), payload.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.SessionID != seedRun.SessionID {
		t.Fatalf("SessionID = %q, want %q", run.SessionID, seedRun.SessionID)
	}
}

func TestWSHandleChatSendAppliesAuthScopeToRun(t *testing.T) {
	t.Parallel()

	gw := newGatewayWithBuiltins(t, t.TempDir())
	handler := NewWSHandler(gw, nil)

	raw, err := handler.handleChatSend(&wsClient{
		authScope: authScope{
			Subject: "user-gateway-ws",
		},
	}, json.RawMessage(`{"session_key":"ws:scoped","content":"follow up"}`))
	if err != nil {
		t.Fatalf("handleChatSend() error = %v", err)
	}

	var payload chatSendResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	run, err := gw.runtime.GetRun(context.Background(), payload.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if !run.Scope.IsZero() {
		t.Fatalf("run.Scope = %#v", run.Scope)
	}
}

func TestWSHandleChatSendAutoFillsSingleAutomationScope(t *testing.T) {
	t.Parallel()

	gw := newGatewayWithBuiltins(t, t.TempDir())
	handler := NewWSHandler(gw, nil)

	raw, err := handler.handleChatSend(&wsClient{
		authScope: authScope{
			AutomationIDs: []string{"auto-ws"},
			Scoped:        true,
		},
	}, json.RawMessage(`{"session_key":"ws:auto","content":"follow up"}`))
	if err != nil {
		t.Fatalf("handleChatSend() error = %v", err)
	}

	var payload chatSendResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	run, err := gw.runtime.GetRun(context.Background(), payload.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.Scope.AutomationID != "auto-ws" {
		t.Fatalf("run.Scope = %#v", run.Scope)
	}
}

func TestWSHandleChatSendRejectsAmbiguousAutomationScope(t *testing.T) {
	t.Parallel()

	gw := newGatewayWithBuiltins(t, t.TempDir())
	handler := NewWSHandler(gw, nil)

	_, err := handler.handleChatSend(&wsClient{
		authScope: authScope{
			AutomationIDs: []string{"auto-a", "auto-b"},
			Scoped:        true,
		},
	}, json.RawMessage(`{"session_key":"ws:ambiguous","content":"follow up"}`))
	if err == nil {
		t.Fatal("expected ambiguous automation scope error")
	}
}

func TestWSHandleChatAbortCancelsLatestActiveRunWithoutRunID(t *testing.T) {
	t.Parallel()

	gw := newGatewayWithBuiltins(t, t.TempDir())
	handler := NewWSHandler(gw, nil)

	execute := false
	run, err := gw.runtime.Submit(context.Background(), runtimesvc.SubmitRequest{
		SessionKey: "ws:abort",
		Content:    "seed message",
		Execute:    &execute,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	raw, err := handler.handleChatAbort(nil, json.RawMessage(`{"session_id":"`+run.SessionID+`"}`))
	if err != nil {
		t.Fatalf("handleChatAbort() error = %v", err)
	}

	var payload chatAbortResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !payload.OK {
		t.Fatalf("payload = %#v", payload)
	}
	cancelled, err := gw.runtime.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if cancelled.Status != agent.RunCancelled {
		t.Fatalf("cancelled.Status = %q, want %q", cancelled.Status, agent.RunCancelled)
	}
}

func TestWSHandleConfigSetReturnsExplicitUnsupportedError(t *testing.T) {
	t.Parallel()

	handler := NewWSHandler(newTestGatewayFull(t), nil)
	_, err := handler.handleConfigSet(nil, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "/operator/config") {
		t.Fatalf("error = %q", err)
	}
}

func TestWSDoesNotExposeSessionsCompact(t *testing.T) {
	t.Parallel()

	handler := NewWSHandler(newTestGatewayFull(t), nil)
	if _, ok := handler.methods["sessions.compact"]; ok {
		t.Fatal("sessions.compact should not be exposed on gateway websocket")
	}
}
