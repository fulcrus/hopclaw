package agent

import (
	"context"
	"strings"
	"time"
)

const (
	runRegistryTTL           = 24 * time.Hour
	runRegistrySweepInterval = 5 * time.Minute
)

type runCancelEntry struct {
	cancel    context.CancelFunc
	claimedAt time.Time
}

type runExecutionEntry struct {
	claimedAt time.Time
}

func (a *AgentComponent) storeRunCancel(runID string, cancel context.CancelFunc) {
	if a == nil || strings.TrimSpace(runID) == "" || cancel == nil {
		return
	}
	a.maybeSweepRunState(time.Now().UTC())
	a.cancels.Store(runID, runCancelEntry{
		cancel:    cancel,
		claimedAt: time.Now().UTC(),
	})
}

func (a *AgentComponent) deleteRunCancel(runID string) {
	if a == nil || strings.TrimSpace(runID) == "" {
		return
	}
	a.cancels.Delete(runID)
}

func (a *AgentComponent) loadAndDeleteRunCancel(runID string) (context.CancelFunc, bool) {
	if a == nil || strings.TrimSpace(runID) == "" {
		return nil, false
	}
	value, ok := a.cancels.LoadAndDelete(runID)
	if !ok || value == nil {
		return nil, false
	}
	switch typed := value.(type) {
	case runCancelEntry:
		if typed.cancel == nil {
			return nil, false
		}
		return typed.cancel, true
	case context.CancelFunc:
		return typed, true
	default:
		return nil, false
	}
}

func (a *AgentComponent) maybeSweepRunState(now time.Time) {
	if a == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nextSweep := a.runStateSweepAt.Load()
	if nextSweep != 0 && now.UnixNano() < nextSweep {
		return
	}
	if !a.runStateSweepAt.CompareAndSwap(nextSweep, now.Add(runRegistrySweepInterval).UnixNano()) {
		return
	}
	a.sweepRunState(now)
}

func (a *AgentComponent) sweepRunState(now time.Time) {
	if a == nil {
		return
	}
	staleBefore := now.Add(-runRegistryTTL)
	a.executing.Range(func(key, value any) bool {
		runID := strings.TrimSpace(toString(key))
		if runID == "" {
			return true
		}
		at := claimedAtExecutionEntry(value)
		if !at.IsZero() && at.After(staleBefore) {
			return true
		}
		if a.keepRunExecutionState(runID) {
			return true
		}
		a.executing.Delete(runID)
		return true
	})
	a.cancels.Range(func(key, value any) bool {
		runID := strings.TrimSpace(toString(key))
		if runID == "" {
			return true
		}
		at := claimedAtCancelEntry(value)
		if !at.IsZero() && at.After(staleBefore) {
			return true
		}
		if a.keepRunCancelState(runID) {
			return true
		}
		a.cancels.Delete(runID)
		return true
	})
}

func (a *AgentComponent) keepRunExecutionState(runID string) bool {
	if a == nil || a.runs == nil {
		return false
	}
	run, err := a.runs.Get(context.Background(), runID)
	if err != nil || run == nil {
		return false
	}
	return run.Status == RunRunning
}

func (a *AgentComponent) keepRunCancelState(runID string) bool {
	return a.keepRunExecutionState(runID)
}

func claimedAtExecutionEntry(value any) time.Time {
	switch typed := value.(type) {
	case runExecutionEntry:
		return typed.claimedAt
	default:
		return time.Time{}
	}
}

func claimedAtCancelEntry(value any) time.Time {
	switch typed := value.(type) {
	case runCancelEntry:
		return typed.claimedAt
	default:
		return time.Time{}
	}
}

func toString(value any) string {
	text, _ := value.(string)
	return text
}
