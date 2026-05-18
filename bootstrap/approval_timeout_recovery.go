package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func forceCancelTimedOutApproval(
	ctx context.Context,
	runtime *runtimesvc.Service,
	store approval.Store,
	bus eventbus.Bus,
	warnings *startupWarningCollector,
	ticket *approval.Ticket,
) error {
	if store == nil || ticket == nil || strings.TrimSpace(ticket.ID) == "" {
		return fmt.Errorf("timed-out approval ticket is required")
	}

	current := ticket
	ticketID := strings.TrimSpace(ticket.ID)
	runID := strings.TrimSpace(ticket.RunID)
	forceApplied := false

	if current.Status == approval.StatusPending {
		resolved, err := store.Resolve(ctx, ticketID, approval.Resolution{
			Status:     approval.StatusCancelled,
			ResolvedBy: "system_timeout",
			Note:       "approval timed out",
		})
		if err != nil {
			if errors.Is(err, approval.ErrAlreadyResolved) {
				clearApprovalTimeoutRunWarning(warnings, runID)
				return nil
			}
			return err
		}
		if resolved != nil {
			current = resolved
			runID = strings.TrimSpace(resolved.RunID)
		}
		forceApplied = true
	}

	if forceApplied {
		publishApprovalLifecycleEvent(ctx, bus, eventbus.EventApprovalResolved, current, 0)
		publishApprovalLifecycleEvent(ctx, bus, eventbus.EventApprovalTimedOut, current, 0)
	}

	if runtime == nil || runID == "" {
		clearApprovalTimeoutRunWarning(warnings, runID)
		return nil
	}

	run, err := runtime.GetRun(ctx, runID)
	if err != nil {
		wrapped := fmt.Errorf("lookup run %s after timed-out approval %s force-cancel: %w", runID, ticketID, err)
		recordApprovalTimeoutRunWarning(warnings, runID, wrapped)
		return wrapped
	}
	if run == nil || run.Status != agent.RunWaitingApproval {
		clearApprovalTimeoutRunWarning(warnings, runID)
		return nil
	}

	if _, err := runtime.CancelRun(ctx, runID); err != nil && !errors.Is(err, agent.ErrRunCancelled) {
		wrapped := fmt.Errorf("cancel run %s after timed-out approval %s force-cancel: %w", runID, ticketID, err)
		recordApprovalTimeoutRunWarning(warnings, runID, wrapped)
		return wrapped
	}

	clearApprovalTimeoutRunWarning(warnings, runID)
	return nil
}
