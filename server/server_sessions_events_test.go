package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

func TestServerGetSessionMessages(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done",
				},
			}},
		},
	})
	handler := New(svc, Config{}).Handler()

	run := postRun(t, handler, map[string]any{
		"session_key": "chat-session-export",
		"content":     "hello",
	}, http.StatusAccepted)
	waitForRunStatus(t, handler, run.ID, agent.RunCompleted)

	req := httptest.NewRequest(http.MethodGet, "/runtime/sessions/"+run.SessionID+"/messages", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/sessions/%s/messages status = %d body=%s", run.SessionID, rec.Code, rec.Body.String())
	}

	var messages []contextengine.Message
	if err := json.NewDecoder(rec.Body).Decode(&messages); err != nil {
		t.Fatalf("Decode(messages) error = %v", err)
	}
	if len(messages) < 2 {
		t.Fatalf("len(messages) = %d, want at least 2", len(messages))
	}
	if messages[0].Role != contextengine.RoleUser || messages[0].TextContent() != "hello" {
		t.Fatalf("messages[0] = %#v", messages[0])
	}
	if messages[len(messages)-1].Role != contextengine.RoleAssistant || messages[len(messages)-1].TextContent() != "done" {
		t.Fatalf("last message = %#v", messages[len(messages)-1])
	}
}
