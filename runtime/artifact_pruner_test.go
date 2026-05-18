package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/eventbus"
)

func TestArtifactPrunerPeriodicPrune(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	artifacts := artifact.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()
	sub := bus.SubscribeChannel(8)
	defer sub.Close()

	comp := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), mockModelClient{}, nil, nil)

	svc := NewService(comp, sessions, runs, approvals, bus, artifacts).
		WithClock(clock).
		WithArtifactRetention(time.Nanosecond)

	if _, err := artifacts.Put(context.Background(), artifact.PutRequest{Kind: "old", Body: []byte("x")}); err != nil {
		t.Fatalf("artifacts.Put() error = %v", err)
	}

	pruner := NewArtifactPruner(svc, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pruner.Start(ctx)
	defer pruner.Stop()
	clock.WaitForTickers(1)
	clock.Advance(50 * time.Millisecond)

	timeout := time.After(2 * time.Second)
	for {
		select {
		case event, ok := <-sub.Events():
			if !ok {
				t.Fatal("artifact pruner event subscription closed")
			}
			if event.Type != eventbus.EventArtifactPruned {
				continue
			}
			items, err := artifacts.List(context.Background(), artifact.ListFilter{})
			if err != nil {
				t.Fatalf("artifacts.List() error = %v", err)
			}
			if len(items) != 0 {
				t.Fatalf("artifact count after prune = %d, want 0", len(items))
			}
			return
		case <-timeout:
			t.Fatal("artifact pruner did not emit prune event")
		}
	}
}

func TestArtifactPrunerStop(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	artifacts := artifact.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()

	comp := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), mockModelClient{}, nil, nil)

	svc := NewService(comp, sessions, runs, approvals, bus, artifacts).
		WithArtifactRetention(24 * time.Hour)

	pruner := NewArtifactPruner(svc, 50*time.Millisecond)

	ctx := context.Background()
	pruner.Start(ctx)

	// Stop should not block or panic.
	pruner.Stop()
	if pruner.cancel != nil {
		t.Fatal("expected Stop to clear pruner cancel function")
	}

	// Double-stop is safe.
	pruner.Stop()

	// Restart after stop should re-arm the loop.
	pruner.Start(ctx)
	if pruner.cancel == nil {
		t.Fatal("expected Start to install a cancel function")
	}
	pruner.Stop()
}
