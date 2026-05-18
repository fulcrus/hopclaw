package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
)

func newRunEventSubscription(t *testing.T, svc *Service) *eventbus.Subscription {
	t.Helper()
	sub := svc.SubscribeEvents(64)
	if sub == nil {
		t.Fatal("expected in-memory event bus subscription")
	}
	return sub
}

func waitForRunEventStatus(t *testing.T, svc *Service, sub *eventbus.Subscription, runID string, want agent.RunStatus) *agent.Run {
	t.Helper()
	if svc == nil || sub == nil {
		t.Fatal("service and subscription are required")
	}
	run, err := svc.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun(%s) error = %v", runID, err)
	}
	if run.Status == want {
		return run
	}

	timeout := time.After(2 * time.Second)
	for {
		select {
		case event, ok := <-sub.Events():
			if !ok {
				t.Fatalf("event subscription closed while waiting for run %s", runID)
			}
			if event.RunID != runID {
				continue
			}
			run, err = svc.GetRun(context.Background(), runID)
			if err != nil {
				t.Fatalf("GetRun(%s) error = %v", runID, err)
			}
			if run.Status == want {
				return run
			}
		case <-timeout:
			run, err = svc.GetRun(context.Background(), runID)
			if err != nil {
				t.Fatalf("GetRun(%s) error = %v", runID, err)
			}
			t.Fatalf("run %s status = %q, want %q, error=%q", runID, run.Status, want, run.Error)
		}
	}
}
