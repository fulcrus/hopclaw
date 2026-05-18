package toolruntime

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/bundle"
)

func TestEncodeExecuteRequestUsesStandardEnvelope(t *testing.T) {
	call := agent.ToolCall{
		ID:   "call_1",
		Name: "feishu.doc.read",
		Input: map[string]any{
			"document_id": "doc_123",
		},
	}

	payload, err := encodeExecuteRequest(call, &agent.Run{ID: "run_1"}, &agent.Session{ID: "sess_1"})
	if err != nil {
		t.Fatalf("encodeExecuteRequest() error = %v", err)
	}

	var req bundle.ExecuteRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	if req.ProtocolVersion != bundle.ProtocolVersionV1 {
		t.Fatalf("ProtocolVersion = %q", req.ProtocolVersion)
	}
	if req.ToolName != call.Name {
		t.Fatalf("ToolName = %q", req.ToolName)
	}
	if req.ToolCallID != call.ID {
		t.Fatalf("ToolCallID = %q", req.ToolCallID)
	}
	if req.RunID != "run_1" || req.SessionID != "sess_1" {
		t.Fatalf("run/session ids = %q/%q", req.RunID, req.SessionID)
	}
}

func TestDecodeToolResultPayloadStructuredSuccess(t *testing.T) {
	call := agent.ToolCall{ID: "call_1", Name: "feishu.doc.write"}
	body := []byte(`{
		"protocol_version":"hopclaw.tool/v1",
		"status":"success",
		"summary":"Document updated",
		"artifacts":[
			{"uri":"artifact://primary"},
			{"uri":"artifact://secondary"}
		],
		"evidence":[
			{"kind":"verification","name":"doc_readback","detail":"matched"}
		]
	}`)

	result := decodeToolResultPayload(call, body)
	if result.ToolName != call.Name || result.ToolCallID != call.ID {
		t.Fatalf("unexpected tool metadata: %#v", result)
	}
	if result.ArtifactURI != "artifact://primary" {
		t.Fatalf("ArtifactURI = %q", result.ArtifactURI)
	}
	if !strings.Contains(result.Content, "Document updated") {
		t.Fatalf("Content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "[evidence] verification:doc_readback=matched") {
		t.Fatalf("Content missing evidence: %q", result.Content)
	}
	if !strings.Contains(result.Content, "[artifact] artifact://secondary") {
		t.Fatalf("Content missing extra artifact: %q", result.Content)
	}
}

func TestDecodeToolResultPayloadStructuredError(t *testing.T) {
	call := agent.ToolCall{ID: "call_1", Name: "feishu.doc.write"}
	body := []byte(`{
		"protocol_version":"hopclaw.tool/v1",
		"status":"retryable_error",
		"error":{"code":"rate_limit","category":"rate_limit","message":"Too many requests","retryable":true}
	}`)

	result := decodeToolResultPayload(call, body)
	if got := result.Content; got != "error [rate_limit]: Too many requests" {
		t.Fatalf("Content = %q", got)
	}
}

func TestDecodeToolResultPayloadLegacyExternal(t *testing.T) {
	call := agent.ToolCall{ID: "call_1", Name: "legacy.tool"}
	result := decodeToolResultPayload(call, []byte(`{"output":"ok"}`))
	if result.Content != "ok" {
		t.Fatalf("Content = %q", result.Content)
	}
}

func TestDecodeToolResultPayloadLegacyLocal(t *testing.T) {
	call := agent.ToolCall{ID: "call_1", Name: "legacy.tool"}
	result := decodeToolResultPayload(call, []byte(`{"content":"ok","artifact_uri":"artifact://one"}`))
	if result.Content != "ok" {
		t.Fatalf("Content = %q", result.Content)
	}
	if result.ArtifactURI != "artifact://one" {
		t.Fatalf("ArtifactURI = %q", result.ArtifactURI)
	}
}
