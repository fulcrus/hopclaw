package hooks

import (
	"fmt"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Hook event catalog
// ---------------------------------------------------------------------------

// EventCategory groups hook triggers by the product area they observe.
type EventCategory string

const (
	EventCategoryRun        EventCategory = "run"
	EventCategoryTool       EventCategory = "tool"
	EventCategorySession    EventCategory = "session"
	EventCategorySystem     EventCategory = "system"
	EventCategoryAgent      EventCategory = "agent"
	EventCategoryMessage    EventCategory = "message"
	EventCategoryModel      EventCategory = "model"
	EventCategoryApproval   EventCategory = "approval"
	EventCategoryGovernance EventCategory = "governance"
)

// EventSpec describes the semantics of a supported hook trigger.
type EventSpec struct {
	Trigger       TriggerEvent  `json:"trigger"`
	Description   string        `json:"description"`
	Category      EventCategory `json:"category"`
	AllowedPhases []HookPhase   `json:"allowed_phases"`
	CanBlock      bool          `json:"can_block"`
	SupportsAsync bool          `json:"supports_async"`
}

var eventCatalog = map[TriggerEvent]EventSpec{
	TriggerRunCompleted: {
		Trigger:       TriggerRunCompleted,
		Description:   "Fires when a run completes successfully and delivery is ready.",
		Category:      EventCategoryRun,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerRunFailed: {
		Trigger:       TriggerRunFailed,
		Description:   "Fires when a run fails and an error receipt is produced.",
		Category:      EventCategoryRun,
		AllowedPhases: []HookPhase{HookPhaseError},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerToolExecuted: {
		Trigger:       TriggerToolExecuted,
		Description:   "Generic tool lifecycle hook for cross-cutting tool observability and policy.",
		Category:      EventCategoryTool,
		AllowedPhases: []HookPhase{HookPhasePre, HookPhasePost, HookPhaseError},
		CanBlock:      true,
		SupportsAsync: true,
	},
	TriggerSessionCreated: {
		Trigger:       TriggerSessionCreated,
		Description:   "Fires after a new session is created.",
		Category:      EventCategorySession,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerStartup: {
		Trigger:       TriggerStartup,
		Description:   "Fires after HopClaw startup completes.",
		Category:      EventCategorySystem,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerShutdown: {
		Trigger:       TriggerShutdown,
		Description:   "Fires during shutdown for cleanup and notifications.",
		Category:      EventCategorySystem,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerBeforeModelResolve: {
		Trigger:       TriggerBeforeModelResolve,
		Description:   "Fires before model routing and resolution begin.",
		Category:      EventCategoryModel,
		AllowedPhases: []HookPhase{HookPhasePre},
		CanBlock:      true,
		SupportsAsync: false,
	},
	TriggerBeforePromptBuild: {
		Trigger:       TriggerBeforePromptBuild,
		Description:   "Fires before the final model prompt is assembled.",
		Category:      EventCategoryModel,
		AllowedPhases: []HookPhase{HookPhasePre},
		CanBlock:      true,
		SupportsAsync: false,
	},
	TriggerBeforeAgentStart: {
		Trigger:       TriggerBeforeAgentStart,
		Description:   "Fires before agent execution starts.",
		Category:      EventCategoryAgent,
		AllowedPhases: []HookPhase{HookPhasePre},
		CanBlock:      true,
		SupportsAsync: false,
	},
	TriggerAfterAgentEnd: {
		Trigger:       TriggerAfterAgentEnd,
		Description:   "Fires after agent execution ends, including degraded exits.",
		Category:      EventCategoryAgent,
		AllowedPhases: []HookPhase{HookPhasePost, HookPhaseError},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerBeforeToolCall: {
		Trigger:       TriggerBeforeToolCall,
		Description:   "Fires before an individual tool call executes.",
		Category:      EventCategoryTool,
		AllowedPhases: []HookPhase{HookPhasePre},
		CanBlock:      true,
		SupportsAsync: false,
	},
	TriggerAfterToolCall: {
		Trigger:       TriggerAfterToolCall,
		Description:   "Fires after an individual tool call finishes or fails.",
		Category:      EventCategoryTool,
		AllowedPhases: []HookPhase{HookPhasePost, HookPhaseError},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerMessageReceived: {
		Trigger:       TriggerMessageReceived,
		Description:   "Fires when an inbound user or channel message is accepted.",
		Category:      EventCategoryMessage,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerMessageSending: {
		Trigger:       TriggerMessageSending,
		Description:   "Fires before an outbound message is dispatched to a channel.",
		Category:      EventCategoryMessage,
		AllowedPhases: []HookPhase{HookPhasePre},
		CanBlock:      true,
		SupportsAsync: false,
	},
	TriggerMessageSent: {
		Trigger:       TriggerMessageSent,
		Description:   "Fires after an outbound message is sent to a channel.",
		Category:      EventCategoryMessage,
		AllowedPhases: []HookPhase{HookPhasePost, HookPhaseError},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerSessionEnd: {
		Trigger:       TriggerSessionEnd,
		Description:   "Fires when a session is finalized or explicitly ended.",
		Category:      EventCategorySession,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerApprovalRequested: {
		Trigger:       TriggerApprovalRequested,
		Description:   "Fires when a run or tool action enters approval-required state.",
		Category:      EventCategoryApproval,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerApprovalResolved: {
		Trigger:       TriggerApprovalResolved,
		Description:   "Fires when an approval ticket is approved or denied.",
		Category:      EventCategoryApproval,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerApprovalTimedOut: {
		Trigger:       TriggerApprovalTimedOut,
		Description:   "Fires when an approval ticket expires before a decision arrives.",
		Category:      EventCategoryApproval,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerApprovalGraceWarning: {
		Trigger:       TriggerApprovalGraceWarning,
		Description:   "Fires shortly before an approval ticket reaches timeout.",
		Category:      EventCategoryApproval,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerGovernanceDeliveryQueued: {
		Trigger:       TriggerGovernanceDeliveryQueued,
		Description:   "Fires when a governance record is enqueued for reliable delivery.",
		Category:      EventCategoryGovernance,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerGovernanceDeliveryRetryScheduled: {
		Trigger:       TriggerGovernanceDeliveryRetryScheduled,
		Description:   "Fires when governance delivery is retried and the next attempt is scheduled.",
		Category:      EventCategoryGovernance,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerGovernanceDeliveryDelivered: {
		Trigger:       TriggerGovernanceDeliveryDelivered,
		Description:   "Fires when a governance record is delivered successfully to an external adapter.",
		Category:      EventCategoryGovernance,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerGovernanceDeliveryDeadLettered: {
		Trigger:       TriggerGovernanceDeliveryDeadLettered,
		Description:   "Fires when governance delivery exhausts retries and enters dead-letter state.",
		Category:      EventCategoryGovernance,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
	TriggerGovernanceDeliveryRedriven: {
		Trigger:       TriggerGovernanceDeliveryRedriven,
		Description:   "Fires when an operator or automation redrives a governance delivery entry.",
		Category:      EventCategoryGovernance,
		AllowedPhases: []HookPhase{HookPhasePost},
		CanBlock:      false,
		SupportsAsync: true,
	},
}

// EventSpecs returns the supported hook event catalog in stable order.
func EventSpecs() []EventSpec {
	out := make([]EventSpec, 0, len(eventCatalog))
	for _, spec := range eventCatalog {
		spec.AllowedPhases = append([]HookPhase(nil), spec.AllowedPhases...)
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Trigger < out[j].Trigger
	})
	return out
}

// LookupEventSpec returns metadata for a supported hook trigger.
func LookupEventSpec(trigger TriggerEvent) (EventSpec, bool) {
	spec, ok := eventCatalog[trigger]
	if !ok {
		return EventSpec{}, false
	}
	spec.AllowedPhases = append([]HookPhase(nil), spec.AllowedPhases...)
	return spec, true
}

// ValidateHookDefinition validates a stored hook definition against the
// canonical hook catalog and execution semantics.
func ValidateHookDefinition(h Hook) error {
	trigger := TriggerEvent(strings.TrimSpace(string(h.Trigger)))
	if trigger == "" {
		return fmt.Errorf("trigger is required")
	}
	spec, ok := LookupEventSpec(trigger)
	if !ok {
		return fmt.Errorf("unsupported hook trigger %q", trigger)
	}
	phase := h.EffectivePhase()
	if err := validateHookPhaseForSpec(spec, phase); err != nil {
		return err
	}
	if h.Async && phase == HookPhasePre {
		return fmt.Errorf("hook %q cannot run async in %q phase", trigger, phase)
	}
	switch h.Kind {
	case KindHTTP:
		if strings.TrimSpace(h.URL) == "" {
			return fmt.Errorf("url is required for http hooks")
		}
	case KindCommand:
		if strings.TrimSpace(h.Command) == "" {
			return fmt.Errorf("command is required for command hooks")
		}
	case "":
		return fmt.Errorf("kind is required")
	default:
		return fmt.Errorf("unsupported hook kind %q", h.Kind)
	}
	return nil
}

// ValidateHookInvocation validates a concrete fire request after effective
// trigger and phase defaults have been resolved.
func ValidateHookInvocation(trigger TriggerEvent, phase HookPhase) error {
	spec, ok := LookupEventSpec(trigger)
	if !ok {
		return fmt.Errorf("unsupported hook trigger %q", trigger)
	}
	return validateHookPhaseForSpec(spec, phase)
}

func validateHookPhaseForSpec(spec EventSpec, phase HookPhase) error {
	for _, allowed := range spec.AllowedPhases {
		if allowed == phase {
			return nil
		}
	}
	allowed := make([]string, 0, len(spec.AllowedPhases))
	for _, item := range spec.AllowedPhases {
		allowed = append(allowed, string(item))
	}
	return fmt.Errorf("hook trigger %q does not support phase %q (allowed: %s)", spec.Trigger, phase, strings.Join(allowed, ", "))
}
