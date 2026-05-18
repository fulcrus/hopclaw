package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/eventbus"
)

func TestElasticsearchRecorderIndexesByEventID(t *testing.T) {
	t.Parallel()

	received := make(chan eventbus.Event, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/hopclaw-audit/_doc/evt-1" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "ApiKey es-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var event eventbus.Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		received <- event
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	recorder, err := NewElasticsearchRecorder(ElasticsearchRecorderConfig{
		Name:   "corp-es",
		URL:    server.URL,
		Index:  "hopclaw-audit",
		APIKey: "es-key",
	})
	if err != nil {
		t.Fatalf("NewElasticsearchRecorder() error = %v", err)
	}
	if err := recorder.Deliver(context.Background(), eventbus.Event{
		ID:   "evt-1",
		Type: eventbus.EventRunCompleted,
	}); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	got := <-received
	if got.ID != "evt-1" || got.Type != eventbus.EventRunCompleted {
		t.Fatalf("received event = %+v", got)
	}
}
