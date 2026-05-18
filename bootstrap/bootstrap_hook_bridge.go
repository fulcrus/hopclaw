package bootstrap

import (
	"context"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/hooks"
)

// eventTriggerMap maps eventbus event types to hook trigger events.
var eventTriggerMap = map[eventbus.EventType]hooks.TriggerEvent{
	eventbus.EventRunCompleted:                     hooks.TriggerRunCompleted,
	eventbus.EventRunFailed:                        hooks.TriggerRunFailed,
	eventbus.EventToolExecuted:                     hooks.TriggerToolExecuted,
	eventbus.EventApprovalRequested:                hooks.TriggerApprovalRequested,
	eventbus.EventApprovalResolved:                 hooks.TriggerApprovalResolved,
	eventbus.EventApprovalTimedOut:                 hooks.TriggerApprovalTimedOut,
	eventbus.EventApprovalGraceWarning:             hooks.TriggerApprovalGraceWarning,
	eventbus.EventGovernanceDeliveryQueued:         hooks.TriggerGovernanceDeliveryQueued,
	eventbus.EventGovernanceDeliveryRetryScheduled: hooks.TriggerGovernanceDeliveryRetryScheduled,
	eventbus.EventGovernanceDeliveryDelivered:      hooks.TriggerGovernanceDeliveryDelivered,
	eventbus.EventGovernanceDeliveryDeadLettered:   hooks.TriggerGovernanceDeliveryDeadLettered,
	eventbus.EventGovernanceDeliveryRedriven:       hooks.TriggerGovernanceDeliveryRedriven,
}

// hookEventSink implements eventbus.Sink and fires hooks on matching events.
type hookEventSink struct {
	executor *hooks.Executor
}

func (s *hookEventSink) Handle(ctx context.Context, event eventbus.Event) error {
	if s == nil || s.executor == nil {
		return nil
	}
	trigger, ok := eventTriggerMap[event.Type]
	if !ok {
		return nil
	}

	payload := make(map[string]any, len(event.Attrs)+3)
	for key, value := range event.Attrs {
		payload[key] = value
	}
	if event.SessionID != "" {
		payload["session_id"] = event.SessionID
	}
	if event.RunID != "" {
		payload["run_id"] = event.RunID
	}
	if event.ID != "" {
		payload["event_id"] = event.ID
	}
	if !event.Time.IsZero() {
		payload["event_time"] = event.Time.UTC()
	}
	payload["event_type"] = string(event.Type)

	phase := hooks.HookPhasePost
	if event.Type == eventbus.EventRunFailed {
		s.executor.Fire(ctx, trigger, hooks.HookPhaseError, payload)
	}
	s.executor.Fire(ctx, trigger, phase, payload)
	return nil
}
