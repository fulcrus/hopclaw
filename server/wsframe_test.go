package server

import (
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// RequestFrame marshaling
// ---------------------------------------------------------------------------

func TestRequestFrame_Marshal(t *testing.T) {
	t.Parallel()

	params, _ := json.Marshal(map[string]string{"key": "value"})
	frame := RequestFrame{
		Type:   frameTypeRequest,
		ID:     "req-1",
		Method: WSMethodStatus,
		Params: params,
	}

	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded RequestFrame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != frameTypeRequest {
		t.Errorf("Type = %q, want %q", decoded.Type, frameTypeRequest)
	}
	if decoded.ID != "req-1" {
		t.Errorf("ID = %q, want req-1", decoded.ID)
	}
	if decoded.Method != WSMethodStatus {
		t.Errorf("Method = %q, want %q", decoded.Method, WSMethodStatus)
	}
}

func TestRequestFrame_OmitsEmptyParams(t *testing.T) {
	t.Parallel()

	frame := RequestFrame{
		Type:   frameTypeRequest,
		ID:     "req-2",
		Method: WSMethodRunsList,
	}

	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["params"]; ok {
		t.Error("expected params to be omitted when nil")
	}
}

// ---------------------------------------------------------------------------
// ResponseFrame marshaling
// ---------------------------------------------------------------------------

func TestResponseFrame_Marshal_Success(t *testing.T) {
	t.Parallel()

	payload, _ := json.Marshal(map[string]string{"status": "online"})
	frame := ResponseFrame{
		Type:    frameTypeResponse,
		ID:      "req-1",
		OK:      true,
		Payload: payload,
	}

	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ResponseFrame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.OK {
		t.Error("expected OK = true")
	}
	if decoded.Error != nil {
		t.Error("expected nil Error for success response")
	}
}

func TestResponseFrame_Marshal_Error(t *testing.T) {
	t.Parallel()

	frame := ResponseFrame{
		Type: frameTypeResponse,
		ID:   "req-3",
		OK:   false,
		Error: &WSError{
			Code:    WSErrNotFound,
			Message: "session not found",
		},
	}

	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ResponseFrame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.OK {
		t.Error("expected OK = false")
	}
	if decoded.Error == nil {
		t.Fatal("expected non-nil Error")
	}
	if decoded.Error.Code != WSErrNotFound {
		t.Errorf("Error.Code = %q, want %q", decoded.Error.Code, WSErrNotFound)
	}
	if decoded.Error.Message != "session not found" {
		t.Errorf("Error.Message = %q", decoded.Error.Message)
	}
}

// ---------------------------------------------------------------------------
// EventFrame marshaling
// ---------------------------------------------------------------------------

func TestEventFrame_Marshal(t *testing.T) {
	t.Parallel()

	frame := EventFrame{
		Type:    frameTypeEvent,
		Event:   "run.completed",
		Payload: map[string]string{"run_id": "run-1"},
		Seq:     42,
	}

	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded EventFrame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != frameTypeEvent {
		t.Errorf("Type = %q, want %q", decoded.Type, frameTypeEvent)
	}
	if decoded.Event != "run.completed" {
		t.Errorf("Event = %q, want run.completed", decoded.Event)
	}
	if decoded.Seq != 42 {
		t.Errorf("Seq = %d, want 42", decoded.Seq)
	}
}

func TestEventFrame_OmitsZeroSeq(t *testing.T) {
	t.Parallel()

	frame := EventFrame{
		Type:  frameTypeEvent,
		Event: "status.changed",
	}

	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["seq"]; ok {
		// Seq is int64 with omitempty; zero should be omitted.
		var seq int64
		if err := json.Unmarshal(raw["seq"], &seq); err == nil && seq == 0 {
			t.Error("expected seq to be omitted when zero")
		}
	}
}

// ---------------------------------------------------------------------------
// WSError marshaling
// ---------------------------------------------------------------------------

func TestWSError_Marshal(t *testing.T) {
	t.Parallel()

	wsErr := WSError{
		Code:    WSErrInternal,
		Message: "something went wrong",
		Details: map[string]string{"trace": "abc123"},
	}

	data, err := json.Marshal(wsErr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded WSError
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Code != WSErrInternal {
		t.Errorf("Code = %q, want %q", decoded.Code, WSErrInternal)
	}
	if decoded.Message != "something went wrong" {
		t.Errorf("Message = %q", decoded.Message)
	}
}

func TestWSError_OmitsNilDetails(t *testing.T) {
	t.Parallel()

	wsErr := WSError{
		Code:    WSErrUnauthorized,
		Message: "invalid token",
	}

	data, err := json.Marshal(wsErr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["details"]; ok {
		t.Error("expected details to be omitted when nil")
	}
}

// ---------------------------------------------------------------------------
// ConnectParams marshaling
// ---------------------------------------------------------------------------

func TestConnectParams_Marshal(t *testing.T) {
	t.Parallel()

	params := ConnectParams{
		MinProtocol: 1,
		MaxProtocol: 1,
		Client: ConnectClientInfo{
			ID:          "client-1",
			DisplayName: "Test Client",
			Version:     "0.1.0",
			Platform:    "darwin",
			Mode:        WSClientModeBackend,
		},
		Auth: &ConnectAuth{
			Token: "test-token",
		},
		Role:   "operator",
		Scopes: []string{"read", "write"},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ConnectParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Client.ID != "client-1" {
		t.Errorf("Client.ID = %q", decoded.Client.ID)
	}
	if decoded.Auth == nil {
		t.Fatal("expected non-nil Auth")
	}
	if decoded.Auth.Token != "test-token" {
		t.Errorf("Auth.Token = %q", decoded.Auth.Token)
	}
	if decoded.Role != "operator" {
		t.Errorf("Role = %q", decoded.Role)
	}
	if len(decoded.Scopes) != 2 {
		t.Errorf("Scopes len = %d, want 2", len(decoded.Scopes))
	}
}

func TestConnectParams_OmitsOptionalFields(t *testing.T) {
	t.Parallel()

	params := ConnectParams{
		MinProtocol: 1,
		MaxProtocol: 1,
		Client: ConnectClientInfo{
			ID: "client-2",
		},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["auth"]; ok {
		t.Error("expected auth to be omitted when nil")
	}
}

// ---------------------------------------------------------------------------
// HelloOK marshaling
// ---------------------------------------------------------------------------

func TestHelloOK_Marshal(t *testing.T) {
	t.Parallel()

	hello := HelloOK{
		Type:     "hello-ok",
		Protocol: wsProtocolVersion,
		Server: HelloServer{
			Version: "1.0.0",
			ConnID:  "conn-abc",
		},
		Features: HelloFeatures{
			Methods: []string{WSMethodStatus.String(), WSMethodRunsSubmit.String()},
			Events:  []string{"run.completed", "run.failed"},
		},
		Policy: HelloPolicy{
			MaxPayload:     wsMaxPayloadBytes,
			MaxBuffered:    wsMaxBufferedBytes,
			TickIntervalMs: wsTickInterval.Milliseconds(),
		},
	}

	data, err := json.Marshal(hello)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded HelloOK
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Protocol != wsProtocolVersion {
		t.Errorf("Protocol = %d, want %d", decoded.Protocol, wsProtocolVersion)
	}
	if decoded.Server.Version != "1.0.0" {
		t.Errorf("Server.Version = %q", decoded.Server.Version)
	}
	if decoded.Server.ConnID != "conn-abc" {
		t.Errorf("Server.ConnID = %q", decoded.Server.ConnID)
	}
	if len(decoded.Features.Methods) != 2 {
		t.Errorf("Features.Methods len = %d, want 2", len(decoded.Features.Methods))
	}
	if decoded.Policy.MaxPayload != wsMaxPayloadBytes {
		t.Errorf("Policy.MaxPayload = %d, want %d", decoded.Policy.MaxPayload, wsMaxPayloadBytes)
	}
}

// ---------------------------------------------------------------------------
// Constants sanity checks
// ---------------------------------------------------------------------------

func TestWSFrameConstants(t *testing.T) {
	t.Parallel()

	if frameTypeRequest != "req" {
		t.Errorf("frameTypeRequest = %q, want req", frameTypeRequest)
	}
	if frameTypeResponse != "res" {
		t.Errorf("frameTypeResponse = %q, want res", frameTypeResponse)
	}
	if frameTypeEvent != "event" {
		t.Errorf("frameTypeEvent = %q, want event", frameTypeEvent)
	}
	if wsProtocolVersion != 1 {
		t.Errorf("wsProtocolVersion = %d, want 1", wsProtocolVersion)
	}
	if WSMethodConnect != "connect" {
		t.Errorf("WSMethodConnect = %q, want connect", WSMethodConnect)
	}
	if WSClientModeBackend != "backend" {
		t.Errorf("WSClientModeBackend = %q, want backend", WSClientModeBackend)
	}
}

func TestWSErrorCodeConstants(t *testing.T) {
	t.Parallel()

	codes := map[string]string{
		"invalid_request": WSErrInvalidRequest,
		"not_found":       WSErrNotFound,
		"unauthorized":    WSErrUnauthorized,
		"rate_limited":    WSErrRateLimited,
		"internal_error":  WSErrInternal,
	}
	for expected, got := range codes {
		if got != expected {
			t.Errorf("error code = %q, want %q", got, expected)
		}
	}
}
