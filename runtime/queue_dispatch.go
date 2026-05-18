package runtime

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
)

func (s *Service) shouldDispatchQueuedRunNow(ctx context.Context, run *agent.Run) bool {
	if s == nil || s.agent == nil || run == nil || run.Status != agent.RunQueued {
		return false
	}
	if sessionHasQueueOccupant(ctx, s.runs, run.SessionID, run.ID) {
		return false
	}
	coord := s.agent.Coordinator()
	if coord == nil {
		return true
	}
	nextRunID, ok, err := coord.NextQueuedRun(ctx, run.SessionID)
	if err != nil {
		log.Warn("inspect queued head failed", "run_id", run.ID, "session_id", run.SessionID, "error", err)
		return true
	}
	if !ok || strings.TrimSpace(nextRunID) == "" {
		return true
	}
	return strings.TrimSpace(nextRunID) == strings.TrimSpace(run.ID)
}

func sessionHasQueueOccupant(ctx context.Context, runs agent.RunStore, sessionID, excludeRunID string) bool {
	lister, ok := runs.(agent.RunLister)
	if !ok || strings.TrimSpace(sessionID) == "" {
		return false
	}
	items, err := lister.List(ctx, agent.RunListFilter{SessionID: sessionID})
	if err != nil {
		return false
	}
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.ID) == strings.TrimSpace(excludeRunID) {
			continue
		}
		if runOccupiesQueueSlot(item.Status) {
			return true
		}
	}
	return false
}

func runOccupiesQueueSlot(status agent.RunStatus) bool {
	switch status {
	case agent.RunRunning, agent.RunStreaming, agent.RunWaitingInput, agent.RunWaitingApproval:
		return true
	default:
		return false
	}
}
