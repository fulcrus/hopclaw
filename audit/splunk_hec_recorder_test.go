package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/eventbus"
)

func TestSplunkHECRecorderPostsHECPayload(t *testing.T) {
	t.Parallel()

	type receivedPayload struct {
		Event  eventbus.Event `json:"event"`
		Fields map[string]any `json:"fields"`
	}
	received := make(chan receivedPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Splunk splunk-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var payload receivedPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	recorder, err := NewSplunkHECRecorder(SplunkHECRecorderConfig{
		Name:       "corp-splunk",
		URL:        server.URL,
		Token:      "splunk-token",
		Source:     "hopclaw",
		SourceType: "hopclaw:audit",
		Index:      "main",
	})
	if err != nil {
		t.Fatalf("NewSplunkHECRecorder() error = %v", err)
	}
	if err := recorder.Deliver(context.Background(), eventbus.Event{
		ID:        "evt-2",
		Type:      eventbus.EventRunFailed,
		RunID:     "run-2",
		SessionID: "sess-2",
	}); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	got := <-received
	if got.Event.ID != "evt-2" || got.Event.Type != eventbus.EventRunFailed {
		t.Fatalf("received event = %+v", got.Event)
	}
	if got.Fields["hopclaw_event_id"] != "evt-2" {
		t.Fatalf("fields = %+v", got.Fields)
	}
}
