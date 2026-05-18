package agent

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/hooks"
	"github.com/fulcrus/hopclaw/internal/metrics"
	"github.com/fulcrus/hopclaw/logging"
)

type runFinalPlanAction string

const (
	runFinalPlanNone   runFinalPlanAction = ""
	runFinalPlanCancel runFinalPlanAction = "cancel"
	runFinalPlanFail   runFinalPlanAction = "fail"
)

type runFinalization struct {
	status        RunStatus
	errorText     string
	eventType     eventbus.EventType
	eventAttrs    map[string]any
	planAction    runFinalPlanAction
	planReason    string
	planErr       error
	cancelContext bool
	finishQueue   bool
	hookPhase     hooks.HookPhase
	hookSummary   string
	hookErr       error
	session       *Session
}

func (a *AgentComponent) finalizeRun(ctx context.Context, run *Run, spec runFinalization) error {
	if a == nil || run == nil {
		return nil
	}
	if run.Status.Terminal() {
		return nil
	}
	transitionRun(run, spec.status, PhaseFinalize,
		withRunPendingTools(nil),
		withRunApproval(""),
		withRunError(strings.TrimSpace(spec.errorText)),
		withRunFinishedAt(time.Now().UTC()),
	)

	switch spec.planAction {
	case runFinalPlanFail:
		if spec.planErr != nil {
			if err := a.failRunningPlanTasks(ctx, run, spec.planErr); err != nil {
				return err
			}
		}
	case runFinalPlanCancel:
		if err := a.cancelPlanTasks(ctx, run, strings.TrimSpace(spec.planReason)); err != nil {
			return err
		}
	}

	if err := a.runs.Update(ctx, run); err != nil {
		return err
	}
	switch spec.status {
	case RunCompleted:
		metrics.RunsTotal.WithLabelValues("completed").Inc()
	case RunFailed:
		metrics.RunsTotal.WithLabelValues("failed").Inc()
	case RunCancelled:
		metrics.RunsTotal.WithLabelValues("cancelled").Inc()
	}

	if spec.cancelContext {
		if cancelFn, ok := a.loadAndDeleteRunCancel(run.ID); ok {
			cancelFn()
		}
	}
	if spec.finishQueue && a.queue != nil {
		logging.LogIfErr(ctx, a.queue.FinishRun(ctx, run.SessionID, run.ID),
			"queue finish run failed", slog.String("run_id", run.ID))
	}
	if spec.eventType != "" {
		channel := a.resolveRunChannel(ctx, run, spec.session)
		extraAttrs := buildGovernanceEventAttrs(run)
		var event eventbus.Event
		switch spec.eventType {
		case eventbus.EventRunCompleted:
			payload, _ := (eventbus.Event{Attrs: spec.eventAttrs}).RunStatusPayload()
			if payload.Channel == "" {
				payload.Channel = channel
			}
			event = eventbus.NewRunCompletedEvent(run.ID, run.SessionID, payload, extraAttrs)
		case eventbus.EventRunFailed:
			payload, _ := (eventbus.Event{Attrs: spec.eventAttrs}).RunStatusPayload()
			if payload.Channel == "" {
				payload.Channel = channel
			}
			event = eventbus.NewRunFailedEvent(run.ID, run.SessionID, payload, extraAttrs)
		case eventbus.EventRunCancelled:
			payload, _ := (eventbus.Event{Attrs: spec.eventAttrs}).RunControlPayload()
			if payload.Channel == "" {
				payload.Channel = channel
			}
			event = eventbus.NewRunCancelledEvent(run.ID, run.SessionID, payload, extraAttrs)
		default:
			event = eventbus.Event{
				Type:      spec.eventType,
				RunID:     run.ID,
				SessionID: run.SessionID,
				Attrs:     mergeEventAttrs(spec.eventAttrs, eventbus.RunStatusAttrs{Channel: channel}.ToMap(), extraAttrs),
			}
		}
		logging.LogIfErr(ctx, a.emit(ctx, event), "emit event failed", slog.String("kind", string(spec.eventType)))
	}
	if spec.hookPhase != "" {
		a.afterAgentEndHook(ctx, run, spec.session, spec.hookPhase, strings.TrimSpace(spec.hookSummary), spec.hookErr)
	}
	return nil
}

func (a *AgentComponent) MarkBackgroundFailure(ctx context.Context, runID string, runErr error) error {
	if a == nil || strings.TrimSpace(runID) == "" || runErr == nil {
		return nil
	}
	run, err := a.runs.Get(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status.Terminal() || run.Status == RunWaitingApproval {
		return nil
	}
	summary := compactRunSummary(runErr.Error())
	return a.finalizeRun(ctx, run, runFinalization{
		status:        RunFailed,
		errorText:     runErr.Error(),
		eventType:     eventbus.EventRunFailed,
		eventAttrs:    eventbus.RunStatusAttrs{Error: runErr.Error(), Summary: summary}.ToMap(),
		planAction:    runFinalPlanFail,
		planErr:       runErr,
		cancelContext: true,
		finishQueue:   true,
		hookPhase:     hooks.HookPhaseError,
		hookSummary:   summary,
		hookErr:       runErr,
	})
}

func (a *AgentComponent) SupersedeWaitingInputRun(ctx context.Context, runID, reason string) error {
	if a == nil || strings.TrimSpace(runID) == "" {
		return nil
	}
	run, err := a.runs.Get(ctx, runID)
	if err != nil {
		return err
	}
	if run == nil || run.Status != RunWaitingInput {
		return nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = RunReasonClarificationSuperseded
	}
	return a.finalizeRun(ctx, run, runFinalization{
		status:        RunCancelled,
		errorText:     reason,
		eventType:     eventbus.EventRunCancelled,
		eventAttrs:    eventbus.RunControlAttrs{Reason: "preflight_clarification_superseded"}.ToMap(),
		planAction:    runFinalPlanCancel,
		planReason:    reason,
		cancelContext: true,
		finishQueue:   true,
		hookPhase:     hooks.HookPhaseError,
		hookSummary:   reason,
		hookErr:       errors.New(reason),
	})
}
