package nodes

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRegistryInvokeRoundTrip(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	t.Cleanup(registry.Stop)

	session := NodeSession{NodeID: "mac-1", Platform: "macOS"}
	registry.Register(session, func(msg []byte) error {
		var payload struct {
			ID      int            `json:"id"`
			Type    string         `json:"type"`
			Command string         `json:"command"`
			Params  map[string]any `json:"params"`
		}
		if err := json.Unmarshal(msg, &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if payload.Type != "invoke" || payload.Command != "desktop.screenshot" {
			t.Fatalf("unexpected payload = %#v", payload)
		}
		go registry.HandleResponse(session.NodeID, payload.ID, NodeInvokeResponse{
			OK:   true,
			Data: map[string]any{"path": "/tmp/shot.png"},
		})
		return nil
	})

	resp, err := registry.Invoke(NodeInvokeRequest{
		NodeID:  session.NodeID,
		Command: "desktop.screenshot",
		Timeout: 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if !resp.OK || resp.Data["path"] != "/tmp/shot.png" {
		t.Fatalf("Invoke() response = %#v", resp)
	}
}

func TestRegistryInvokeRejectsDisallowedCommand(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	t.Cleanup(registry.Stop)

	registry.Register(NodeSession{NodeID: "ios-1", Platform: "iOS"}, func([]byte) error {
		t.Fatal("sendFn should not be called for disallowed command")
		return nil
	})

	resp, err := registry.Invoke(NodeInvokeRequest{
		NodeID:  "ios-1",
		Command: "desktop.screenshot",
		Timeout: 50 * time.Millisecond,
	})
	if err == nil || resp != nil {
		t.Fatalf("Invoke() err=%v resp=%#v, want disallowed command error", err, resp)
	}
}

func TestPendingWorkQueueDrainSkipsExpiredAndHonorsPriority(t *testing.T) {
	t.Parallel()

	queue := NewPendingWorkQueue()
	expiredAt := time.Now().Add(-time.Minute)
	queue.Enqueue(WorkItem{Type: WorkItemStatusRequest, Priority: PriorityNormal})
	queue.Enqueue(WorkItem{Type: WorkItemLocationRequest, Priority: PriorityHigh})
	queue.Enqueue(WorkItem{Type: WorkItemStatusRequest, Priority: PriorityDefault, ExpiresAt: &expiredAt})

	items := queue.Drain(10)
	if len(items) != 2 {
		t.Fatalf("Drain() len = %d, want 2", len(items))
	}
	if items[0].Priority != PriorityHigh {
		t.Fatalf("Drain()[0].Priority = %q, want %q", items[0].Priority, PriorityHigh)
	}
	if queue.Len() != 0 {
		t.Fatalf("queue.Len() = %d, want 0", queue.Len())
	}
}

func TestPhoneControlGateStatusAndExpiry(t *testing.T) {
	t.Parallel()

	gate := NewPhoneControlGate()
	if gate.IsAllowed("camera.snap") {
		t.Fatal("camera.snap should be blocked before arming")
	}
	if err := gate.Arm("camera", "ops", 25*time.Millisecond); err != nil {
		t.Fatalf("Arm() error = %v", err)
	}
	if !gate.IsAllowed("camera.snap") {
		t.Fatal("camera.snap should be allowed while armed")
	}

	status := gate.Status()
	camera, ok := status["camera"].(map[string]any)
	if !ok || camera["armed"] != true || camera["armed_by"] != "ops" {
		t.Fatalf("Status()[camera] = %#v", status["camera"])
	}

	time.Sleep(35 * time.Millisecond)
	if gate.IsAllowed("camera.snap") {
		t.Fatal("camera.snap should expire after arm duration")
	}
}
