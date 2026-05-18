package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
)

type streamingConversationModelClient struct {
	response    *ModelResponse
	deltas      []string
	lastRequest ChatRequest
}

func (s *streamingConversationModelClient) Chat(_ context.Context, req ChatRequest) (*ModelResponse, error) {
	s.lastRequest = req
	return s.response, nil
}

func (s *streamingConversationModelClient) ChatStream(ctx context.Context, req ChatRequest, cb StreamCallback) (*ModelResponse, error) {
	s.lastRequest = req
	for _, delta := range s.deltas {
		if cb != nil {
			cb.OnTextDelta(ctx, delta)
		}
	}
	if cb != nil {
		cb.OnComplete(ctx)
	}
	return s.response, nil
}

func TestConversationTurnPersistsTranscriptAndDisablesTools(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "I am HopClaw.",
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil)

	result, err := component.ConversationTurn(context.Background(), ConversationTurnRequest{
		SessionKey:             "conversation-turn-test",
		Content:                "who are you?",
		Mode:                   ConversationTurnChat,
		AdditionalSystemPrompt: "Answer in one sentence.",
		Metadata: map[string]any{
			meta.KeyChannel: "cli",
		},
	})
	if err != nil {
		t.Fatalf("ConversationTurn() error = %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if result.Message.Content != "I am HopClaw." {
		t.Fatalf("Message.Content = %q, want %q", result.Message.Content, "I am HopClaw.")
	}
	if len(model.lastRequest.Tools) != 0 {
		t.Fatalf("last request tools = %#v, want none", model.lastRequest.Tools)
	}
	if model.lastRequest.SystemPrompt == "" || !containsAll(model.lastRequest.SystemPrompt, "<conversation_envelope>", "Answer in one sentence.") {
		t.Fatalf("system prompt = %q", model.lastRequest.SystemPrompt)
	}

	session, err := sessions.GetByKey(context.Background(), "conversation-turn-test")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if got := session.Messages[0].Metadata[meta.KeyInteractionEnvelope]; got != "conversation" {
		t.Fatalf("user envelope = %#v, want conversation", got)
	}
	if got := session.Messages[1].Metadata[meta.KeyInteractionEnvelope]; got != "conversation" {
		t.Fatalf("assistant envelope = %#v, want conversation", got)
	}
	if got := session.Messages[0].Metadata[meta.KeyInteractionTurnID]; got == nil || got == "" {
		t.Fatalf("missing interaction turn id in user metadata: %#v", session.Messages[0].Metadata)
	}
	if got := session.Messages[1].Metadata[meta.KeyInteractionTurnID]; got == nil || got == "" {
		t.Fatalf("missing interaction turn id in assistant metadata: %#v", session.Messages[1].Metadata)
	}
}

func TestConversationTurnStreamsRunlessEventsWithProvidedTurnID(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	model := &streamingConversationModelClient{
		deltas: []string{"I am ", "HopClaw.\n"},
		response: &ModelResponse{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "I am HopClaw.\n",
			},
			Usage: &ModelUsageInfo{
				PromptTokens:     5,
				CompletionTokens: 4,
				TotalTokens:      9,
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithEventBus(bus)

	result, err := component.ConversationTurn(context.Background(), ConversationTurnRequest{
		SessionKey: "conversation-turn-stream-test",
		Content:    "who are you?",
		Mode:       ConversationTurnChat,
		Metadata: map[string]any{
			meta.KeyInteractionTurnID: "turn-explicit",
		},
	})
	if err != nil {
		t.Fatalf("ConversationTurn() error = %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}

	events := bus.Snapshot()
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(events))
	}
	if events[0].Type != eventbus.EventModelTextDelta || events[1].Type != eventbus.EventModelTextDelta {
		t.Fatalf("unexpected event types = %#v", []eventbus.EventType{events[0].Type, events[1].Type})
	}
	if events[2].Type != eventbus.EventModelStreamComplete {
		t.Fatalf("events[2].Type = %q, want %q", events[2].Type, eventbus.EventModelStreamComplete)
	}
	for _, event := range events {
		if strings.TrimSpace(event.RunID) != "" {
			t.Fatalf("event.RunID = %q, want empty for runless turn", event.RunID)
		}
		if got := event.Attrs[meta.KeyInteractionTurnID]; got != "turn-explicit" {
			t.Fatalf("interaction_turn_id = %#v, want %q", got, "turn-explicit")
		}
		if got := event.Attrs[meta.KeyInteractionEnvelope]; got != "conversation" {
			t.Fatalf("interaction_envelope = %#v, want %q", got, "conversation")
		}
	}
	payload, ok := events[2].ModelStreamCompletePayload()
	if !ok {
		t.Fatalf("ModelStreamCompletePayload() ok = false")
	}
	if payload.TotalTokens != 9 {
		t.Fatalf("payload.TotalTokens = %d, want 9", payload.TotalTokens)
	}
}

func TestConversationTurnAutoCompactsBeforeModelCallWhenPrepareSignalsThreshold(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	engine := &autoCompactionSignalEngine{}
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "compacted reply",
			},
		}},
	}

	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), engine, model, nil, nil)

	result, err := component.ConversationTurn(ctx, ConversationTurnRequest{
		SessionKey: "conversation-auto-compact",
		Content:    "hello there",
	})
	if err != nil {
		t.Fatalf("ConversationTurn() error = %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if engine.compactCalls != 1 {
		t.Fatalf("compactCalls = %d, want 1", engine.compactCalls)
	}
	if engine.prepareCalls < 2 {
		t.Fatalf("prepareCalls = %d, want at least 2", engine.prepareCalls)
	}

	session, err := sessions.GetByKey(ctx, "conversation-auto-compact")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if session.Summary != "auto compacted" {
		t.Fatalf("session.Summary = %q, want auto compacted", session.Summary)
	}
}

func containsAll(text string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(text, sub) {
			return false
		}
	}
	return true
}
