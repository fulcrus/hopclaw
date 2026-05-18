package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
)

type conformanceInteractionModelClient struct {
	response    *agent.ModelResponse
	deltas      []string
	lastRequest agent.ChatRequest
	calls       int
}

func (m *conformanceInteractionModelClient) Chat(_ context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	m.calls++
	m.lastRequest = req
	if m.response != nil {
		return m.response, nil
	}
	return testModelResponse("ok"), nil
}

func (m *conformanceInteractionModelClient) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	m.calls++
	m.lastRequest = req
	for _, delta := range m.deltas {
		if cb != nil && strings.TrimSpace(delta) != "" {
			cb.OnTextDelta(ctx, delta)
		}
	}
	if cb != nil {
		cb.OnComplete(ctx)
	}
	if m.response != nil {
		return m.response, nil
	}
	return testModelResponse("ok"), nil
}

func newInteractiveServiceWithStreamingBus(model agent.ModelClient) (*Service, *eventbus.InMemoryBus) {
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	approvals := approval.NewInMemoryStore()
	artifacts := artifact.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, queue, newContextEngine(), model, nil, nil).
		WithPreflightAnalyzer(testPreflightAnalyzer{}).
		WithEventBus(bus)
	return NewService(component, sessions, runs, approvals, bus, artifacts), bus
}

func TestInteractExecutionEnvelopeConformanceRunlessReplies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		reply     string
		replyAct  ReplyAct
		speechAct IncomingSpeechAct
		reason    string
		envelope  string
	}{
		{
			name:      "chat_reply_stays_runless",
			content:   "who are you?",
			reply:     "I am HopClaw.",
			replyAct:  ReplyActChatReply,
			speechAct: SpeechActMetaQuestion,
			reason:    "meta_question",
			envelope:  "conversation",
		},
		{
			name:      "clarification_prompt_stays_runless",
			content:   "帮我改一下",
			reply:     "你想改哪个文件？",
			replyAct:  ReplyActClarificationPrompt,
			speechAct: SpeechActUnknown,
			reason:    "low_confidence_action",
			envelope:  "clarification",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model := &conformanceInteractionModelClient{
				deltas:   splitConversationReply(tt.reply),
				response: testModelResponse(tt.reply),
			}
			svc, bus := newInteractiveServiceWithStreamingBus(model)
			svc.WithClassifier(stubInteractionClassifier{
				decision: InteractionDecision{
					SpeechAct:   tt.speechAct,
					TargetScope: TargetScopeNone,
					ReplyAct:    tt.replyAct,
					Confidence:  0.98,
					Reason:      tt.reason,
				},
			})

			result, err := svc.Interact(context.Background(), InteractionRequest{
				SessionKey: "execution-envelope-" + tt.name,
				Content:    tt.content,
				Metadata: map[string]any{
					meta.KeyChannel: "cli",
					"chat_type":     "direct",
				},
			})
			if err != nil {
				t.Fatalf("Interact() error = %v", err)
			}
			if result.Decision.ReplyAct != tt.replyAct {
				t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, tt.replyAct)
			}
			if result.Run != nil {
				t.Fatalf("Run = %#v, want nil for runless envelope", result.Run)
			}
			if result.SubmitRequest != nil {
				t.Fatalf("SubmitRequest = %#v, want nil for runless envelope", result.SubmitRequest)
			}
			if result.ReplyMessage != tt.reply {
				t.Fatalf("ReplyMessage = %q, want %q", result.ReplyMessage, tt.reply)
			}
			if model.calls != 1 {
				t.Fatalf("model calls = %d, want 1", model.calls)
			}
			if len(model.lastRequest.Tools) != 0 {
				t.Fatalf("last request tools = %#v, want none", model.lastRequest.Tools)
			}

			session, err := svc.GetSession(context.Background(), result.Context.SessionID)
			if err != nil {
				t.Fatalf("GetSession() error = %v", err)
			}
			if len(session.Messages) != 2 {
				t.Fatalf("session.Messages = %#v, want two transcript messages", session.Messages)
			}
			userTurnID := strings.TrimSpace(asString(session.Messages[0].Metadata[meta.KeyInteractionTurnID]))
			assistantTurnID := strings.TrimSpace(asString(session.Messages[1].Metadata[meta.KeyInteractionTurnID]))
			if userTurnID == "" || assistantTurnID == "" {
				t.Fatalf("interaction turn ids = %q / %q, want both populated", userTurnID, assistantTurnID)
			}
			if userTurnID != assistantTurnID {
				t.Fatalf("interaction turn ids = %q / %q, want same turn", userTurnID, assistantTurnID)
			}
			if got := session.Messages[0].Metadata[meta.KeyInteractionEnvelope]; got != tt.envelope {
				t.Fatalf("user envelope = %#v, want %q", got, tt.envelope)
			}
			if got := session.Messages[1].Metadata[meta.KeyInteractionEnvelope]; got != tt.envelope {
				t.Fatalf("assistant envelope = %#v, want %q", got, tt.envelope)
			}

			lister, ok := svc.runs.(agent.RunLister)
			if !ok {
				t.Fatal("run store does not implement RunLister")
			}
			runs, err := lister.List(context.Background(), agent.RunListFilter{
				SessionID: result.Context.SessionID,
				Limit:     8,
			})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(runs) != 0 {
				t.Fatalf("runs = %#v, want none for runless envelope", runs)
			}

			events := bus.Snapshot()
			if len(events) != len(model.deltas)+1 {
				t.Fatalf("len(events) = %d, want %d", len(events), len(model.deltas)+1)
			}
			for i, event := range events {
				if strings.TrimSpace(event.RunID) != "" {
					t.Fatalf("event[%d].RunID = %q, want empty for runless envelope", i, event.RunID)
				}
				if event.SessionID != result.Context.SessionID {
					t.Fatalf("event[%d].SessionID = %q, want %q", i, event.SessionID, result.Context.SessionID)
				}
				if got := strings.TrimSpace(asString(event.Attrs[meta.KeyInteractionTurnID])); got != userTurnID {
					t.Fatalf("event[%d] interaction_turn_id = %q, want %q", i, got, userTurnID)
				}
				if got := event.Attrs[meta.KeyInteractionEnvelope]; got != tt.envelope {
					t.Fatalf("event[%d] interaction_envelope = %#v, want %q", i, got, tt.envelope)
				}
			}
			for i := range model.deltas {
				if events[i].Type != eventbus.EventModelTextDelta {
					t.Fatalf("events[%d].Type = %q, want %q", i, events[i].Type, eventbus.EventModelTextDelta)
				}
			}
			if events[len(events)-1].Type != eventbus.EventModelStreamComplete {
				t.Fatalf("events[%d].Type = %q, want %q", len(events)-1, events[len(events)-1].Type, eventbus.EventModelStreamComplete)
			}
		})
	}
}

func TestInteractExecutionEnvelopeConformanceTaskAcceptCreatesTrackedRun(t *testing.T) {
	t.Parallel()

	model := &conformanceInteractionModelClient{
		deltas:   splitConversationReply("completed"),
		response: testModelResponse("completed"),
	}
	svc, bus := newInteractiveServiceWithStreamingBus(model)
	svc.WithClassifier(stubInteractionClassifier{
		decision: InteractionDecision{
			SpeechAct:   SpeechActNewTask,
			TargetScope: TargetScopeNewRun,
			ReplyAct:    ReplyActTaskAccept,
			Confidence:  0.99,
			Reason:      "explicit_new_task",
		},
	})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "execution-envelope-task-accept",
		Content:    "write a short report",
		Metadata: map[string]any{
			meta.KeyChannel: "cli",
			"chat_type":     "direct",
		},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActTaskAccept {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActTaskAccept)
	}
	if result.Run == nil {
		t.Fatal("expected tracked run for task_accept")
	}
	if result.SubmitRequest == nil {
		t.Fatal("expected submit request for task_accept")
	}
	run := waitForRunStatus(t, svc, result.Run.ID, agent.RunCompleted)
	if strings.TrimSpace(run.ID) == "" {
		t.Fatal("expected persisted run id")
	}

	lister, ok := svc.runs.(agent.RunLister)
	if !ok {
		t.Fatal("run store does not implement RunLister")
	}
	runs, err := lister.List(context.Background(), agent.RunListFilter{
		SessionID: run.SessionID,
		Limit:     8,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1 tracked run", len(runs))
	}
	if runs[0].ID != run.ID {
		t.Fatalf("runs[0].ID = %q, want %q", runs[0].ID, run.ID)
	}

	events := bus.Snapshot()
	if len(events) == 0 {
		t.Fatal("expected tracked run events")
	}
	var foundSubmitted bool
	var foundCompleted bool
	var foundRunScopedModelDelta bool
	for _, event := range events {
		if event.RunID == run.ID {
			switch event.Type {
			case eventbus.EventRunSubmitted:
				foundSubmitted = true
			case eventbus.EventRunCompleted:
				foundCompleted = true
			case eventbus.EventModelTextDelta:
				foundRunScopedModelDelta = true
			}
		}
		if event.Type == eventbus.EventModelTextDelta && strings.TrimSpace(event.RunID) == "" {
			t.Fatalf("model delta event %#v is unexpectedly runless for tracked execution", event)
		}
	}
	if !foundSubmitted {
		t.Fatalf("events = %#v, want run.submitted for %q", events, run.ID)
	}
	if !foundCompleted {
		t.Fatalf("events = %#v, want run.completed for %q", events, run.ID)
	}
	if !foundRunScopedModelDelta {
		t.Fatalf("events = %#v, want run-scoped model streaming for %q", events, run.ID)
	}
}

func splitConversationReply(reply string) []string {
	runes := []rune(strings.TrimSpace(reply))
	if len(runes) == 0 {
		return []string{"ok"}
	}
	if len(runes) == 1 {
		return []string{string(runes)}
	}
	mid := len(runes) / 2
	return []string{string(runes[:mid]), string(runes[mid:])}
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
