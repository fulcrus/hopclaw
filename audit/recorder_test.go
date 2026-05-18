package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/eventbus"
)

func TestInMemoryRecorderCapturesEvents(t *testing.T) {
	t.Parallel()

	recorder := NewInMemoryRecorder()
	err := recorder.Handle(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-1",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(recorder.Snapshot()) != 1 {
		t.Fatalf("len(Snapshot()) = %d", len(recorder.Snapshot()))
	}
}

func TestJSONLRecorderWritesFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "audit.jsonl")
	recorder := &JSONLRecorder{Path: path}
	if err := recorder.Handle(context.Background(), eventbus.Event{
		Type:      eventbus.EventApprovalResolved,
		RunID:     "run-2",
		SessionID: "sess-2",
		Attrs: map[string]any{
			"status": "approved",
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `"type":"approval.resolved"`) {
		t.Fatalf("audit file = %q", string(data))
	}
}
