package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"
	"time"
)

type rawProtocolHarness struct {
	server *Server
	reader *bufio.Reader
	writer io.Writer
}

func startRawProtocolHarness(t *testing.T, gw *mockGatewayClient, cfg ServerConfig) *rawProtocolHarness {
	t.Helper()

	clientR, clientW, serverR, serverW := pipePair()
	srv := NewServer(gw, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	go func() {
		_ = srv.Serve(ctx, serverR, serverW)
	}()

	return &rawProtocolHarness{
		server: srv,
		reader: bufio.NewReader(clientR),
		writer: clientW,
	}
}

func (h *rawProtocolHarness) sendLine(t *testing.T, line string) {
	t.Helper()
	if len(line) == 0 || line[len(line)-1] != '\n' {
		line += "\n"
	}
	if _, err := io.WriteString(h.writer, line); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

func (h *rawProtocolHarness) readLine(t *testing.T) string {
	t.Helper()

	type result struct {
		line string
		err  error
	}

	done := make(chan result, 1)
	go func() {
		line, err := h.reader.ReadString('\n')
		done <- result{line: line, err: err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("read response: %v", res.err)
		}
		return res.line
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for protocol line")
		return ""
	}
}

func assertJSONLineEqual(t *testing.T, got, want string) {
	t.Helper()

	var gotValue any
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		t.Fatalf("unmarshal got JSON: %v\n%s", err, got)
	}

	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("unmarshal want JSON: %v\n%s", err, want)
	}

	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON mismatch\ngot:  %s\nwant: %s", got, want)
	}
}

func parseJSONLine(t *testing.T, line string) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatalf("unmarshal protocol line: %v\n%s", err, line)
	}
	return payload
}

func TestACPProtocolGoldenInitialize(t *testing.T) {
	t.Parallel()

	h := startRawProtocolHarness(t, newMockGatewayClient(), ServerConfig{})
	h.sendLine(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocol_version":"2024-11-05","client_info":{"name":"golden-client","version":"1.0.0"}}}`)

	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"id":1,
		"result":{
			"protocol_version":"2024-11-05",
			"server_info":{"name":"hopclaw","version":"0.1.0"},
			"capabilities":{
				"streaming":true,
				"permissions":true,
				"commands":true,
				"prompt":{
					"message":true,
					"images":true,
					"content_blocks":true,
					"structured_command":true,
					"structured_approval":true,
					"model":true
				},
				"sessions":{
					"new":true,
					"load":true,
					"list":true,
					"cancel":true,
					"set_mode":true,
					"set_config_option":true
				},
				"notifications":{
					"session_update":true,
					"permission_request":true,
					"commands_update":false
				},
				"protocol_versions":["2024-11-05"]
			}
		}
	}`)
}

func TestACPProtocolGoldenPromptFlow(t *testing.T) {
	t.Parallel()

	h := startRawProtocolHarness(t, newMockGatewayClient(), ServerConfig{})
	h.sendLine(t, `{"jsonrpc":"2.0","id":1,"method":"acp/newSession","params":{"session_id":"golden-prompt"}}`)
	if payload := parseJSONLine(t, h.readLine(t)); payload["error"] != nil {
		t.Fatalf("newSession returned error: %#v", payload["error"])
	}

	h.sendLine(t, `{"jsonrpc":"2.0","id":2,"method":"acp/prompt","params":{"session_id":"golden-prompt","message":"hello world"}}`)

	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"id":2,
		"result":{"session_id":"golden-prompt","status":"accepted"}
	}`)
	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"method":"acp/sessionUpdate",
		"params":{"session_id":"golden-prompt","run_id":"run-1","status":"streaming","text_delta":"hello"}
	}`)
	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"method":"acp/sessionUpdate",
		"params":{"session_id":"golden-prompt","run_id":"run-1","status":"completed","stop_reason":"end_turn"}
	}`)
}

func TestACPProtocolGoldenCancelFlow(t *testing.T) {
	t.Parallel()

	gw := newMockGatewayClient()
	h := startRawProtocolHarness(t, gw, ServerConfig{})
	h.sendLine(t, `{"jsonrpc":"2.0","id":1,"method":"acp/newSession","params":{"session_id":"golden-cancel"}}`)
	if payload := parseJSONLine(t, h.readLine(t)); payload["error"] != nil {
		t.Fatalf("newSession returned error: %#v", payload["error"])
	}

	h.server.sessions.SetActiveRun("golden-cancel", "run-123", func() {})
	h.sendLine(t, `{"jsonrpc":"2.0","id":2,"method":"acp/cancel","params":{"session_id":"golden-cancel"}}`)

	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"id":2,
		"result":{"session_id":"golden-cancel","status":"cancelled"}
	}`)

	if gw.cancelCalled != 1 {
		t.Fatalf("cancelCalled = %d, want 1", gw.cancelCalled)
	}
}

func TestACPProtocolGoldenPermissionFlow(t *testing.T) {
	t.Parallel()

	gw := newMockGatewayClient()
	gw.runs["golden-permission"] = []RunEvent{
		{
			Type: "permission_request",
			Permission: &PermissionRequest{
				RequestID:                  "perm-1",
				SessionID:                  "golden-permission",
				ToolName:                   "web.fetch",
				Description:                "Fetch remote content",
				Input:                      "https://example.com",
				RequiresExternalSideEffect: true,
			},
		},
	}

	h := startRawProtocolHarness(t, gw, ServerConfig{})
	h.sendLine(t, `{"jsonrpc":"2.0","id":1,"method":"acp/newSession","params":{"session_id":"golden-permission"}}`)
	if payload := parseJSONLine(t, h.readLine(t)); payload["error"] != nil {
		t.Fatalf("newSession returned error: %#v", payload["error"])
	}

	h.sendLine(t, `{"jsonrpc":"2.0","id":2,"method":"acp/prompt","params":{"session_id":"golden-permission","message":"fetch it"}}`)
	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"id":2,
		"result":{"session_id":"golden-permission","status":"accepted"}
	}`)
	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"method":"acp/permissionRequest",
		"params":{
			"request_id":"perm-1",
			"session_id":"golden-permission",
			"tool_name":"web.fetch",
			"description":"Fetch remote content",
			"input":"https://example.com",
			"requires_external_side_effect":true
		}
	}`)

	h.sendLine(t, `{"jsonrpc":"2.0","id":3,"method":"acp/permissionResponse","params":{"request_id":"perm-1","approved":true,"scope":"session"}}`)
	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"id":3,
		"result":{"request_id":"perm-1","status":"resolved"}
	}`)
	if len(gw.resolvedApprovalIDs) != 1 || gw.resolvedApprovalIDs[0] != "perm-1" {
		t.Fatalf("resolvedApprovalIDs = %#v, want [perm-1]", gw.resolvedApprovalIDs)
	}
	if len(gw.resolvedApprovals) != 1 || gw.resolvedApprovals[0].Status != "approved" || gw.resolvedApprovals[0].Scope != "session" {
		t.Fatalf("resolvedApprovals = %#v, want approved session resolution", gw.resolvedApprovals)
	}
}

func TestACPRejectsUnknownTopLevelFieldAndContinues(t *testing.T) {
	t.Parallel()

	h := startRawProtocolHarness(t, newMockGatewayClient(), ServerConfig{})
	h.sendLine(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocol_version":"2024-11-05","client_info":{"name":"bad-client","version":"1.0.0"}},"unexpected":true}`)
	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"id":1,
		"error":{
			"code":-32600,
			"message":"invalid JSON-RPC message",
			"data":{
				"code":"acp.invalid_request",
				"details":{
					"field":"unexpected",
					"reason":"json: unknown field \"unexpected\""
				}
			}
		}
	}`)

	h.sendLine(t, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocol_version":"2024-11-05","client_info":{"name":"good-client","version":"1.0.0"}}}`)
	second := parseJSONLine(t, h.readLine(t))
	if second["error"] != nil {
		t.Fatalf("initialize after malformed request returned error: %#v", second["error"])
	}
}

func TestACPRejectsUnknownParamField(t *testing.T) {
	t.Parallel()

	h := startRawProtocolHarness(t, newMockGatewayClient(), ServerConfig{})
	h.sendLine(t, `{"jsonrpc":"2.0","id":1,"method":"acp/prompt","params":{"session_id":"missing","message":"hello","unexpected":true}}`)
	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"id":1,
		"error":{
			"code":-32602,
			"message":"invalid params",
			"data":{
				"code":"acp.invalid_params",
				"details":{
					"field":"unexpected",
					"reason":"json: unknown field \"unexpected\""
				}
			}
		}
	}`)
}

func TestACPParseErrorRespondsWithNullID(t *testing.T) {
	t.Parallel()

	h := startRawProtocolHarness(t, newMockGatewayClient(), ServerConfig{})
	h.sendLine(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocol_version":"2024-11-05"`)
	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"id":null,
		"error":{
			"code":-32700,
			"message":"invalid JSON",
			"data":{"code":"acp.parse_error"}
		}
	}`)
}

func TestACPRejectsUnsupportedCapability(t *testing.T) {
	t.Parallel()

	h := startRawProtocolHarness(t, newMockGatewayClient(), ServerConfig{})
	h.sendLine(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocol_version":"2024-11-05","client_info":{"name":"cap-client","version":"1.0.0"},"capabilities":{"notifications":{"commands_update":true}}}}`)
	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"id":1,
		"error":{
			"code":-32602,
			"message":"unsupported required capability \"notifications.commands_update\"",
			"data":{
				"code":"acp.capability_unsupported",
				"details":{"capability":"notifications.commands_update"}
			}
		}
	}`)
}

func TestACPRejectsPermissionResponseWithoutApproved(t *testing.T) {
	t.Parallel()

	h := startRawProtocolHarness(t, newMockGatewayClient(), ServerConfig{})
	h.sendLine(t, `{"jsonrpc":"2.0","id":1,"method":"acp/permissionResponse","params":{"request_id":"perm-1"}}`)
	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"id":1,
		"error":{
			"code":-32602,
			"message":"approved is required",
			"data":{
				"code":"acp.invalid_params",
				"details":{"field":"approved"}
			}
		}
	}`)
}

func TestACPRejectsApprovedPermissionResponseWithDenyScope(t *testing.T) {
	t.Parallel()

	h := startRawProtocolHarness(t, newMockGatewayClient(), ServerConfig{})
	h.sendLine(t, `{"jsonrpc":"2.0","id":1,"method":"acp/permissionResponse","params":{"request_id":"perm-1","approved":true,"scope":"deny"}}`)
	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"id":1,
		"error":{
			"code":-32602,
			"message":"approved responses must use once, session, or always scope",
			"data":{
				"code":"acp.invalid_params",
				"details":{"field":"scope"}
			}
		}
	}`)
}

func TestACPRejectsDeniedPermissionResponseWithGrantScope(t *testing.T) {
	t.Parallel()

	h := startRawProtocolHarness(t, newMockGatewayClient(), ServerConfig{})
	h.sendLine(t, `{"jsonrpc":"2.0","id":1,"method":"acp/permissionResponse","params":{"request_id":"perm-1","approved":false,"scope":"session"}}`)
	assertJSONLineEqual(t, h.readLine(t), `{
		"jsonrpc":"2.0",
		"id":1,
		"error":{
			"code":-32602,
			"message":"denied responses must omit scope or use deny",
			"data":{
				"code":"acp.invalid_params",
				"details":{"field":"scope"}
			}
		}
	}`)
}

func TestACPInitializeAdvertisesCommandsUpdateWhenConfigured(t *testing.T) {
	t.Parallel()

	clientR, clientW, serverR, serverW := pipePair()
	gw := newMockGatewayClient()
	srv := NewServer(gw, ServerConfig{
		CommandProvider: func(context.Context) ([]Command, error) {
			return []Command{{Name: "sync", Description: "Sync knowledge"}}, nil
		},
	})

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
			Name:    "commands-client",
			Version: "1.0.0",
		},
	})

	resp := receiveResponse(t, client)
	if resp.Error != nil {
		t.Fatalf("initialize returned error: %#v", resp.Error)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}

	notifications, _ := result.Capabilities["notifications"].(map[string]any)
	commandsUpdate, _ := notifications["commands_update"].(bool)
	if !commandsUpdate {
		t.Fatalf("notifications.commands_update = %v, want true", notifications["commands_update"])
	}

	update := receiveResponse(t, client)
	if update.Method != "acp/commandsUpdate" {
		t.Fatalf("expected commandsUpdate notification, got %q", update.Method)
	}
}
