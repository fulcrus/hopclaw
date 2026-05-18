package runtime

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
)

type gatedQueueModelClient struct {
	started chan struct{}
	release chan struct{}
	calls   int
}

func (m *gatedQueueModelClient) Chat(_ context.Context, _ agent.ChatRequest) (*agent.ModelResponse, error) {
	m.calls++
	if m.calls == 1 {
		select {
		case <-m.started:
		default:
			close(m.started)
		}
		<-m.release
	}
	return &agent.ModelResponse{
		Message: contextengine.Message{
			Role:    contextengine.RoleAssistant,
			Content: "done",
		},
	}, nil
}

func TestSubmitQueuesSecondRunUntilActiveRunFinishes(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	bus := eventbus.NewInMemoryBus()
	model := &gatedQueueModelClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, queue, newContextEngine(), model, nil, nil).
		WithEventBus(bus)
	svc := NewService(component, sessions, runs, nil, bus, nil)

	first, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "queue-two-runs",
		Content:    "first request",
	})
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	<-model.started

	second, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "queue-two-runs",
		Content:    "second request",
	})
	if err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}
	if second.Status != agent.RunQueued {
		t.Fatalf("second.Status = %q, want queued", second.Status)
	}

	firstSub := newRunEventSubscription(t, svc)
	defer firstSub.Close()
	secondSub := newRunEventSubscription(t, svc)
	defer secondSub.Close()

	close(model.release)

	waitForRunEventStatus(t, svc, firstSub, first.ID, agent.RunCompleted)
	waitForRunEventStatus(t, svc, secondSub, second.ID, agent.RunCompleted)
}

func TestDispatchNextQueuedRunUsesCoordinatorHeadForInterruptAndCoalesce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		thirdMode agent.QueueMode
	}{
		{name: "interrupt", thirdMode: agent.QueueInterrupt},
		{name: "coalesce", thirdMode: agent.QueueCoalesce},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sessions := agent.NewInMemorySessionStore()
			runs := agent.NewInMemoryRunStore()
			queue := agent.NewInMemoryCoordinator()
			bus := eventbus.NewInMemoryBus()
			component := agent.NewComponent(agent.AgentConfig{
				DefaultModel: "test-model",
				QueueMode:    agent.QueueEnqueue,
			}, sessions, runs, queue, newContextEngine(), mockModelClient{}, nil, nil).
				WithEventBus(bus)
			svc := NewService(component, sessions, runs, nil, bus, nil)

			session, err := sessions.GetOrCreate(context.Background(), "queue-head-order", "test-model")
			if err != nil {
				t.Fatalf("GetOrCreate() error = %v", err)
			}

			first := mustCreateQueuedRun(t, runs, session.ID, "first", agent.QueueEnqueue)
			second := mustCreateQueuedRun(t, runs, session.ID, "second", agent.QueueEnqueue)
			third := mustCreateQueuedRun(t, runs, session.ID, "third", tt.thirdMode)

			if err := queue.EnqueueSessionRun(context.Background(), session.ID, first.ID, agent.QueueEnqueue); err != nil {
				t.Fatalf("EnqueueSessionRun(first) error = %v", err)
			}
			if err := queue.StartRun(context.Background(), session.ID, first.ID); err != nil {
				t.Fatalf("StartRun(first) error = %v", err)
			}
			if _, ok, err := runs.ClaimQueuedRun(context.Background(), first.ID); err != nil {
				t.Fatalf("ClaimQueuedRun(first) error = %v", err)
			} else if !ok {
				t.Fatal("ClaimQueuedRun(first) = false, want true")
			}
			if err := queue.EnqueueSessionRun(context.Background(), session.ID, second.ID, agent.QueueEnqueue); err != nil {
				t.Fatalf("EnqueueSessionRun(second) error = %v", err)
			}
			if err := queue.EnqueueSessionRun(context.Background(), session.ID, third.ID, tt.thirdMode); err != nil {
				t.Fatalf("EnqueueSessionRun(third) error = %v", err)
			}

			first.Status = agent.RunCompleted
			if err := runs.Update(context.Background(), first); err != nil {
				t.Fatalf("Update(first) error = %v", err)
			}
			if err := queue.FinishRun(context.Background(), session.ID, first.ID); err != nil {
				t.Fatalf("FinishRun(first) error = %v", err)
			}

			thirdSub := newRunEventSubscription(t, svc)
			defer thirdSub.Close()

			if err := svc.dispatchNextQueuedRun(context.Background(), session.ID, first.ID); err != nil {
				t.Fatalf("dispatchNextQueuedRun() error = %v", err)
			}

			waitForRunEventStatus(t, svc, thirdSub, third.ID, agent.RunCompleted)
			secondAfter, err := svc.GetRun(context.Background(), second.ID)
			if err != nil {
				t.Fatalf("GetRun(second) error = %v", err)
			}
			if secondAfter.Status != agent.RunQueued {
				t.Fatalf("second.Status = %q, want queued", secondAfter.Status)
			}
		})
	}
}

func mustCreateQueuedRun(t *testing.T, runs *agent.InMemoryRunStore, sessionID, content string, mode agent.QueueMode) *agent.Run {
	t.Helper()

	run, err := runs.Create(context.Background(), sessionID, agent.IncomingMessage{
		Content: content,
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    mode,
	})
	if err != nil {
		t.Fatalf("Create(%s) error = %v", content, err)
	}
	return run
}
