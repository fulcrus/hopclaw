package toolruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestCalendarGroupRegistered(t *testing.T) {
	t.Parallel()

	reg := NewLayer2Registry(Layer2Config{Root: t.TempDir()})
	for _, name := range []string{
		"calendar.list_events",
		"calendar.create_event",
		"calendar.update_event",
		"calendar.delete_event",
	} {
		if _, ok := reg.ResolveTool(nil, name); !ok {
			t.Errorf("tool %s not registered", name)
		}
	}
}

func TestCalendarNotConfigured(t *testing.T) {
	t.Parallel()

	reg := NewLayer2Registry(Layer2Config{Root: t.TempDir()})
	ctx := context.Background()
	run := &agent.Run{ID: "run-cal"}
	sess := &agent.Session{ID: "sess-cal"}

	results, err := reg.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-cal-list", Name: "calendar.list_events",
	}})
	if err != nil {
		t.Fatalf("calendar.list_events error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	var payload struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v\nraw: %s", err, results[0].Content)
	}
	if payload.Status != "not_configured" {
		t.Fatalf("expected status 'not_configured', got %q", payload.Status)
	}
	if !strings.Contains(payload.Message, "calendar.list_events") {
		t.Fatalf("expected message to contain tool name, got %q", payload.Message)
	}
}
