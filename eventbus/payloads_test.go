package eventbus

import (
	"reflect"
	"testing"

	"github.com/fulcrus/hopclaw/resultmodel"
)

func TestNewRunPhaseChangedEventRoundTrip(t *testing.T) {
	t.Parallel()

	event := NewRunPhaseChangedEvent("run-1", "sess-1", RunPhaseChangedAttrs{
		Phase:     "executing_tools",
		ToolNames: []string{"fs.read", "web.search"},
		ToolCount: 2,
	}, map[string]any{"policy_source": "policy.test/source"})

	if event.Type != EventRunPhaseChanged {
		t.Fatalf("event.Type = %q", event.Type)
	}
	if event.RunID != "run-1" || event.SessionID != "sess-1" {
		t.Fatalf("event IDs = (%q, %q)", event.RunID, event.SessionID)
	}
	payload, ok := event.RunPhaseChangedPayload()
	if !ok {
		t.Fatal("RunPhaseChangedPayload() ok = false")
	}
	if payload.Phase != "executing_tools" {
		t.Fatalf("payload.Phase = %q", payload.Phase)
	}
	if !reflect.DeepEqual(payload.ToolNames, []string{"fs.read", "web.search"}) {
		t.Fatalf("payload.ToolNames = %#v", payload.ToolNames)
	}
	if payload.ToolCount != 2 {
		t.Fatalf("payload.ToolCount = %d", payload.ToolCount)
	}
	if event.Attrs["policy_source"] != "policy.test/source" {
		t.Fatalf("policy_source = %#v", event.Attrs["policy_source"])
	}
}

func TestNewRunSubmittedEventRoundTrip(t *testing.T) {
	t.Parallel()

	preflight := map[string]any{"state": "ready"}
	agentProfile := map[string]any{"name": "support"}
	taskContract := map[string]any{"goal": "answer question"}
	event := NewRunSubmittedEvent("run-2", "sess-2", RunSubmittedAttrs{
		QueueMode:     "enqueue",
		Model:         "gpt-4.1",
		ExecutionMode: "direct",
		Preflight:     preflight,
		AgentProfile:  agentProfile,
		TaskContract:  taskContract,
	}, nil)

	payload, ok := event.RunSubmittedPayload()
	if !ok {
		t.Fatal("RunSubmittedPayload() ok = false")
	}
	if payload.QueueMode != "enqueue" || payload.Model != "gpt-4.1" || payload.ExecutionMode != "direct" {
		t.Fatalf("payload = %#v", payload)
	}
	if !reflect.DeepEqual(payload.Preflight, preflight) {
		t.Fatalf("payload.Preflight = %#v", payload.Preflight)
	}
	if !reflect.DeepEqual(payload.AgentProfile, agentProfile) {
		t.Fatalf("payload.AgentProfile = %#v", payload.AgentProfile)
	}
	if !reflect.DeepEqual(payload.TaskContract, taskContract) {
		t.Fatalf("payload.TaskContract = %#v", payload.TaskContract)
	}
}

func TestNewToolExecutedEventRoundTrip(t *testing.T) {
	t.Parallel()

	event := NewToolExecutedEvent("run-3", "sess-3", ToolExecutedPayload{
		ToolCount:      2,
		ToolRound:      3,
		ToolNames:      []string{"fs.read", "web.search"},
		ApprovalID:     "appr-1",
		ArtifactCount:  1,
		ArtifactURIs:   []string{"artifact://1"},
		TaskID:         "task-1",
		TaskTitle:      "Research",
		CompletedTasks: 1,
		TotalTasks:     2,
		Results: []ToolExecutionResultPayload{{
			ToolName:      "fs.read",
			ToolCallID:    "call-1",
			Status:        "success",
			Summary:       "read finished",
			ArtifactURI:   "artifact://1",
			ArtifactCount: 1,
			ActionCount:   0,
			ToolResult: map[string]any{
				"tool_name": "fs.read",
			},
		}},
		ExecutionError:            "permission denied",
		Recovered:                 true,
		RecoveryAttempt:           1,
		RecoveryAttemptsRemaining: 2,
	}, map[string]any{"policy_source": "policy.test/tool"})

	payload, ok := event.ToolExecutedPayload()
	if !ok {
		t.Fatal("ToolExecutedPayload() ok = false")
	}
	if payload.ToolRound != 3 || payload.ToolCount != 2 {
		t.Fatalf("payload counts = %#v", payload)
	}
	if !reflect.DeepEqual(payload.ToolNames, []string{"fs.read", "web.search"}) {
		t.Fatalf("payload.ToolNames = %#v", payload.ToolNames)
	}
	if payload.ApprovalID != "appr-1" {
		t.Fatalf("payload.ApprovalID = %q", payload.ApprovalID)
	}
	if len(payload.Results) != 1 {
		t.Fatalf("len(payload.Results) = %d", len(payload.Results))
	}
	if payload.Results[0].ToolName != "fs.read" || payload.Results[0].ToolCallID != "call-1" {
		t.Fatalf("payload.Results[0] = %#v", payload.Results[0])
	}
	if !reflect.DeepEqual(payload.Results[0].ToolResult, map[string]any{"tool_name": "fs.read"}) {
		t.Fatalf("payload.Results[0].ToolResult = %#v", payload.Results[0].ToolResult)
	}
	if event.Attrs["policy_source"] != "policy.test/tool" {
		t.Fatalf("policy_source = %#v", event.Attrs["policy_source"])
	}
	results, ok := event.Attrs["results"].([]map[string]any)
	if !ok || len(results) != 1 {
		t.Fatalf("event.Attrs[results] = %#v", event.Attrs["results"])
	}
	if _, ok := results[0][resultmodel.MetadataKeyToolResult]; !ok {
		t.Fatalf("tool result bundle missing from %#v", results[0])
	}
}

func TestNewDeliveryFailedEventRoundTrip(t *testing.T) {
	t.Parallel()

	event := NewDeliveryFailedEvent("run-4", "sess-4", DeliveryFailedPayload{
		Channel:        "slack",
		TargetID:       "C123",
		ReplyToID:      "ts-1",
		SessionKey:     "slack:C123",
		Attempts:       4,
		Error:          "temporary send failure 4",
		StatusKind:     "chat_reply",
		ContentPreview: "hello",
	}, nil)

	payload, ok := event.DeliveryFailedPayload()
	if !ok {
		t.Fatal("DeliveryFailedPayload() ok = false")
	}
	if payload.Channel != "slack" || payload.TargetID != "C123" || payload.Attempts != 4 {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.StatusKind != "chat_reply" || payload.ContentPreview != "hello" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestRunStatusAndControlPayloadParsers(t *testing.T) {
	t.Parallel()

	statusEvent := NewRunStatusEvent(EventRunFailed, "run-5", "sess-5", RunStatusAttrs{
		Channel: "discord",
		Error:   "boom",
		Summary: "request failed",
	}, nil)
	statusPayload, ok := statusEvent.RunStatusPayload()
	if !ok {
		t.Fatal("RunStatusPayload() ok = false")
	}
	if statusPayload.Channel != "discord" || statusPayload.Error != "boom" || statusPayload.Summary != "request failed" {
		t.Fatalf("statusPayload = %#v", statusPayload)
	}

	controlEvent := NewRunControlEvent(EventRunCancelled, "run-6", "sess-6", RunControlAttrs{
		Channel:    "telegram",
		ApprovalID: "appr-9",
		Status:     "pending",
		Reason:     "operator_cancel",
	}, nil)
	controlPayload, ok := controlEvent.RunControlPayload()
	if !ok {
		t.Fatal("RunControlPayload() ok = false")
	}
	if controlPayload.Channel != "telegram" || controlPayload.ApprovalID != "appr-9" || controlPayload.Reason != "operator_cancel" {
		t.Fatalf("controlPayload = %#v", controlPayload)
	}
}
