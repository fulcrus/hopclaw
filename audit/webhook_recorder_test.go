package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/eventbus"
)

func TestWebhookRecorderPostsEvents(t *testing.T) {
	t.Parallel()

	secret := "audit-secret"
	received := make(chan eventbus.Event, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer audit-token" {
			t.Fatalf("Authorization = %q", got)
		}
		timestamp := strings.TrimSpace(r.Header.Get("X-HopClaw-Timestamp"))
		signature := strings.TrimSpace(r.Header.Get("X-HopClaw-Signature"))
		body, err := readTestRequestBody(r)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		expected := "sha256=" + computeTestRecorderHMAC(secret, timestamp, body)
		if signature != expected {
			t.Fatalf("signature = %q, want %q", signature, expected)
		}
		var event eventbus.Event
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		received <- event
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	recorder, err := NewWebhookRecorder(WebhookRecorderConfig{
		Name:   "audit-hub",
		URL:    server.URL,
		Secret: secret,
		Headers: map[string]string{
			"Authorization": "Bearer audit-token",
		},
	})
	if err != nil {
		t.Fatalf("NewWebhookRecorder() error = %v", err)
	}

	event := eventbus.Event{Type: eventbus.EventRunCompleted, RunID: "run-1", SessionID: "sess-1"}
	if err := recorder.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	got := <-received
	if got.Type != event.Type || got.RunID != event.RunID {
		t.Fatalf("received event = %+v", got)
	}
}

func TestWebhookRecorderSwallowsTransportErrors(t *testing.T) {
	t.Parallel()

	recorder, err := NewWebhookRecorder(WebhookRecorderConfig{
		Name: "audit-hub",
		URL:  "http://127.0.0.1:1/events",
	})
	if err != nil {
		t.Fatalf("NewWebhookRecorder() error = %v", err)
	}
	if err := recorder.Handle(context.Background(), eventbus.Event{Type: eventbus.EventRunCompleted}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
}

func TestMultiSinkFansOut(t *testing.T) {
	t.Parallel()

	left := NewInMemoryRecorder()
	right := NewInMemoryRecorder()
	sink := MultiSink{Sinks: []eventbus.Sink{left, right}}

	if err := sink.Handle(context.Background(), eventbus.Event{Type: eventbus.EventApprovalResolved}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(left.Snapshot()) != 1 || len(right.Snapshot()) != 1 {
		t.Fatalf("fanout snapshots = %d,%d", len(left.Snapshot()), len(right.Snapshot()))
	}
}

func readTestRequestBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func computeTestRecorderHMAC(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
