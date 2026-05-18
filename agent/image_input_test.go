package agent

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/contextengine"
)

func TestExecuteRunIncludesImageContentBlocksInModelRequest(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "done",
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-image-model",
		Content:    "describe this screenshot",
		Images:     []string{"data:image/png;base64,ZmFrZS1wbmc="},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	var userMsg *contextengine.Message
	for i := range model.lastRequest.Messages {
		msg := &model.lastRequest.Messages[i]
		if msg.Role == contextengine.RoleUser {
			userMsg = msg
			break
		}
	}
	if userMsg == nil {
		t.Fatalf("model.lastRequest.Messages = %#v, want user message", model.lastRequest.Messages)
	}
	if len(userMsg.ContentBlocks) != 2 {
		t.Fatalf("len(ContentBlocks) = %d, want 2", len(userMsg.ContentBlocks))
	}
	if userMsg.ContentBlocks[0].Type != contextengine.ContentBlockText || userMsg.ContentBlocks[0].Text != "describe this screenshot" {
		t.Fatalf("text block = %#v", userMsg.ContentBlocks[0])
	}
	if userMsg.ContentBlocks[1].Type != contextengine.ContentBlockImage {
		t.Fatalf("image block type = %#v", userMsg.ContentBlocks[1])
	}
	if userMsg.ContentBlocks[1].MediaType != "image/png" {
		t.Fatalf("image media type = %q, want image/png", userMsg.ContentBlocks[1].MediaType)
	}
	if userMsg.ContentBlocks[1].Data != "ZmFrZS1wbmc=" {
		t.Fatalf("image data = %q, want base64 payload", userMsg.ContentBlocks[1].Data)
	}
}
