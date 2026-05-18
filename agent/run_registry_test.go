package agent

import (
	"context"
	"testing"
	"time"
)

func TestSweepRunStateRemovesStaleTerminalEntries(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{DefaultModel: "test-model"}, sessions, runs, NewInMemoryCoordinator(), nil, nil, nil, nil)

	run, err := runs.Create(context.Background(), "sess-1", IncomingMessage{SessionKey: "sess-1"}, AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = RunCancelled
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	staleAt := time.Now().UTC().Add(-runRegistryTTL - time.Minute)
	component.executing.Store(run.ID, runExecutionEntry{claimedAt: staleAt})
	component.cancels.Store(run.ID, runCancelEntry{
		cancel:    func() {},
		claimedAt: staleAt,
	})

	component.sweepRunState(time.Now().UTC())

	if _, ok := component.executing.Load(run.ID); ok {
		t.Fatal("expected stale terminal execution claim to be removed")
	}
	if _, ok := component.cancels.Load(run.ID); ok {
		t.Fatal("expected stale terminal cancel entry to be removed")
	}
}

func TestSweepRunStateKeepsStaleRunningEntries(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{DefaultModel: "test-model"}, sessions, runs, NewInMemoryCoordinator(), nil, nil, nil, nil)

	run, err := runs.Create(context.Background(), "sess-2", IncomingMessage{SessionKey: "sess-2"}, AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = RunRunning
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	staleAt := time.Now().UTC().Add(-runRegistryTTL - time.Minute)
	component.executing.Store(run.ID, runExecutionEntry{claimedAt: staleAt})
	component.cancels.Store(run.ID, runCancelEntry{
		cancel:    func() {},
		claimedAt: staleAt,
	})

	component.sweepRunState(time.Now().UTC())

	if _, ok := component.executing.Load(run.ID); !ok {
		t.Fatal("expected running execution claim to be preserved")
	}
	if _, ok := component.cancels.Load(run.ID); !ok {
		t.Fatal("expected running cancel entry to be preserved")
	}
}
