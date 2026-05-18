package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
)

func (s *Service) ResolveApproval(ctx context.Context, id string, resolution approval.Resolution) (*approval.Ticket, error) {
	if s.agent == nil {
		if s == nil || s.approvals == nil {
			return nil, fmt.Errorf("agent component is required")
		}
		ticket, err := s.approvals.Resolve(ctx, id, resolution)
		if err != nil {
			return ticket, err
		}
		s.applyApprovalGrant(ticket)
		return ticket, nil
	}
	ticket, err := s.agent.ResolveApproval(ctx, id, resolution)
	if err != nil {
		return ticket, err
	}
	s.applyApprovalGrant(ticket)
	if ticket.Status == approval.StatusApproved && ticket.RunID != "" {
		run, runErr := s.runs.Get(ctx, ticket.RunID)
		if runErr != nil {
			return ticket, runErr
		}
		if run.Status != agent.RunQueued {
			return ticket, nil
		}
		if releaseGateApprovalTicket(ticket) {
			ctx = releaseGateDispatchContext(ctx)
		}
		// The previous dispatch goroutine for this run (which set
		// RunWaitingApproval and returned) may still be unwinding its
		// deferred cleanup. dispatchRun's de-dup gate would otherwise
		// silently drop our follow-on dispatch and leave the run stuck
		// in RunQueued. Wait briefly for the slot to clear; the dispatch
		// itself remains a no-op if a real concurrent dispatcher is
		// holding the slot for legitimate reasons.
		if err := s.waitDispatchSlot(ctx, ticket.RunID); err != nil {
			return ticket, err
		}
		if err := s.dispatchRun(ctx, ticket.RunID, true); err != nil {
			return ticket, err
		}
	}
	return ticket, nil
}

// waitDispatchSlot blocks for a short, bounded period until the runID is no
// longer marked as actively dispatching, or until the context is cancelled.
// Returns nil even when the wait times out; the subsequent dispatchRun may
// still be a no-op in that case, which preserves the dedup semantics that
// concurrent dispatchRun callers rely on.
func (s *Service) waitDispatchSlot(ctx context.Context, runID string) error {
	if s == nil || runID == "" {
		return nil
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, busy := s.dispatching.Load(runID); !busy {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Millisecond):
		}
	}
	return nil
}

func (s *Service) applyApprovalGrant(ticket *approval.Ticket) {
	if s == nil || s.grantStore == nil || ticket == nil || ticket.Status != approval.StatusApproved {
		return
	}
	for _, call := range ticket.ToolCalls {
		scope := call.ResourceScope
		if scope.Empty() {
			scope = approval.ResourceScopeFromToolCall(call.Name, call.Input)
		}
		s.grantStore.GrantScoped(ticket.SessionID, call.Name, ticket.Scope, scope)
	}
}
