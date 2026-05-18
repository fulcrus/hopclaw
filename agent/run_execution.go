package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/metrics"
	"github.com/fulcrus/hopclaw/logging"
)

type runDispatchOptions struct {
	eventType    eventbus.EventType
	eventAttrs   map[string]any
	allowWatch   bool
	clearError   bool
	setStartedAt bool
}

func (a *AgentComponent) withRunCancellation(ctx context.Context, runID string, fn func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.storeRunCancel(runID, cancel)
	defer a.deleteRunCancel(runID)

	return fn(ctx)
}

func (a *AgentComponent) claimQueuedExecution(ctx context.Context, run *Run) (func(), bool, error) {
	queueStarted := false
	if a.queue != nil {
		if err := a.queue.StartRun(ctx, run.SessionID, run.ID); err != nil {
			if isQueueActiveRunError(err) {
				return nil, false, nil
			}
			return nil, false, err
		}
		queueStarted = true
	}

	claimedRun, claimed, err := a.runs.ClaimQueuedRun(ctx, run.ID)
	if err != nil {
		if queueStarted && a.queue != nil {
			logging.LogIfErr(ctx, a.queue.FinishRun(ctx, run.SessionID, run.ID),
				"queue finish run failed after claim error", slog.String("run_id", run.ID))
		}
		return nil, false, err
	}
	if !claimed {
		if queueStarted && a.queue != nil {
			logging.LogIfErr(ctx, a.queue.FinishRun(ctx, run.SessionID, run.ID),
				"queue finish run failed after unclaimed run", slog.String("run_id", run.ID))
		}
		return nil, false, nil
	}
	if claimedRun != nil {
		*run = *cloneRun(claimedRun)
	}

	if !queueStarted || a.queue == nil {
		return func() {}, true, nil
	}
	sessionID := run.SessionID
	runID := run.ID
	return func() {
		cleanupCtx := context.Background()
		logging.LogIfErr(cleanupCtx, a.queue.FinishRun(cleanupCtx, sessionID, runID),
			"queue finish run failed", slog.String("run_id", runID))
	}, true, nil
}

func isQueueActiveRunError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "already has active run")
}

func (a *AgentComponent) dispatchRunExecution(ctx context.Context, run *Run, opts runDispatchOptions) error {
	ctx = logging.WithSessionID(ctx, run.SessionID)
	ctx = logging.WithRunID(ctx, run.ID)
	if !hasTraceID(logging.FieldsFromContext(ctx)) {
		ctx = logging.WithTraceID(ctx, "run-"+run.ID)
	}
	metrics.RunsInFlight.Inc()
	defer metrics.RunsInFlight.Dec()

	if err := a.beforeAgentStartHook(ctx, run); err != nil {
		return a.handleRunExecutionError(ctx, &run, err)
	}

	if err := a.markPreflightReady(ctx, run); err != nil {
		return err
	}

	transitionRun(run, "", PhasePreparing)
	if opts.clearError {
		transitionRun(run, "", "", withRunError(""))
	}
	if opts.setStartedAt && run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if err := a.runs.Update(ctx, run); err != nil {
		return err
	}
	if opts.eventType != "" {
		channel := a.resolveRunChannel(ctx, run, nil)
		logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewRunDispatchEvent(
			opts.eventType,
			run.ID,
			run.SessionID,
			eventbus.RunDispatchAttrs{Channel: channel},
			opts.eventAttrs,
		)), "emit event failed", slog.String("kind", string(opts.eventType)))
	}

	if opts.allowWatch && run.ExecutionMode == ExecutionModeWatch && a.watchFlow != nil {
		session, unlock, err := a.sessions.LoadForExecution(ctx, run.SessionID)
		if err != nil {
			return err
		}
		defer unlock()
		a.observeSessionRevision(run, session)
		return a.executeWatchMode(ctx, run, session)
	}
	session, unlock, err := a.sessions.LoadForExecution(ctx, run.SessionID)
	if err != nil {
		return err
	}
	a.observeSessionRevision(run, session)
	return a.executeLoop(ctx, run, session, unlock)
}

func hasTraceID(attrs []slog.Attr) bool {
	for _, attr := range attrs {
		if attr.Key == logging.AttrKeyTraceID {
			return true
		}
	}
	return false
}
