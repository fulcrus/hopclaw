package hooks

import (
	"strings"
	"time"

	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
)

// ---------------------------------------------------------------------------
// Trigger events
// ---------------------------------------------------------------------------

// TriggerEvent identifies the event that causes a hook to fire.
type TriggerEvent string

const (
	TriggerRunCompleted   TriggerEvent = "run.completed"
	TriggerRunFailed      TriggerEvent = "run.failed"
	TriggerToolExecuted   TriggerEvent = "tool.executed"
	TriggerSessionCreated TriggerEvent = "session.created"
	TriggerStartup        TriggerEvent = "startup"
	TriggerShutdown       TriggerEvent = "shutdown"

	// Lifecycle hooks for model resolution and prompt building.
	TriggerBeforeModelResolve TriggerEvent = "before.model_resolve"
	TriggerBeforePromptBuild  TriggerEvent = "before.prompt_build"

	// Agent lifecycle hooks.
	TriggerBeforeAgentStart TriggerEvent = "before.agent_start"
	TriggerAfterAgentEnd    TriggerEvent = "after.agent_end"

	// Tool call hooks — fire before/after individual tool invocations.
	TriggerBeforeToolCall TriggerEvent = "before.tool_call"
	TriggerAfterToolCall  TriggerEvent = "after.tool_call"

	// Message lifecycle hooks.
	TriggerMessageReceived TriggerEvent = "message.received"
	TriggerMessageSending  TriggerEvent = "message.sending"
	TriggerMessageSent     TriggerEvent = "message.sent"

	// Session end hook.
	TriggerSessionEnd TriggerEvent = "session.end"

	// Approval lifecycle hooks.
	TriggerApprovalRequested    TriggerEvent = "approval.requested"
	TriggerApprovalResolved     TriggerEvent = "approval.resolved"
	TriggerApprovalTimedOut     TriggerEvent = "approval.timed_out"
	TriggerApprovalGraceWarning TriggerEvent = "approval.grace_warning"

	// Governance delivery lifecycle hooks.
	TriggerGovernanceDeliveryQueued         TriggerEvent = "governance.delivery.queued"
	TriggerGovernanceDeliveryRetryScheduled TriggerEvent = "governance.delivery.retry_scheduled"
	TriggerGovernanceDeliveryDelivered      TriggerEvent = "governance.delivery.delivered"
	TriggerGovernanceDeliveryDeadLettered   TriggerEvent = "governance.delivery.dead_lettered"
	TriggerGovernanceDeliveryRedriven       TriggerEvent = "governance.delivery.redriven"
)

// ---------------------------------------------------------------------------
// Hook kinds
// ---------------------------------------------------------------------------

// HookKind describes how a hook is executed.
type HookKind string

const (
	KindHTTP    HookKind = "http"    // POST to a URL
	KindCommand HookKind = "command" // execute a shell command
)

// ---------------------------------------------------------------------------
// Hook phase
// ---------------------------------------------------------------------------

// HookPhase controls when a hook fires relative to the triggering action.
type HookPhase string

const (
	HookPhasePre   HookPhase = "pre"   // before the triggering action
	HookPhasePost  HookPhase = "post"  // after the triggering action (default)
	HookPhaseError HookPhase = "error" // only on error
)

// defaultHookPriority is the priority assigned to hooks that do not specify one.
// Lower values run first.
const defaultHookPriority = 100

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

// Hook is a user-registered callback that fires when a matching event occurs.
type Hook struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Enabled     bool              `json:"enabled"`
	Source      string            `json:"source,omitempty"`
	SourceRef   string            `json:"source_ref,omitempty"`
	Trigger     TriggerEvent      `json:"trigger"`
	Kind        HookKind          `json:"kind"`
	Priority    int               `json:"priority"`          // lower = runs first, default 100
	Phase       HookPhase         `json:"phase,omitempty"`   // pre, post, error
	Filter      string            `json:"filter,omitempty"`  // JSONPath-like filter expression
	URL         string            `json:"url,omitempty"`     // for http hooks
	Command     string            `json:"command,omitempty"` // for command hooks
	Headers     map[string]string `json:"headers,omitempty"` // for http hooks
	Timeout     int               `json:"timeout,omitempty"` // seconds; 0 uses defaultTimeout
	RetryCount  int               `json:"retry_count,omitempty"`
	Async       bool              `json:"async,omitempty" yaml:"async,omitempty"`   // if true, execute without blocking the caller
	Secret      string            `json:"secret,omitempty" yaml:"secret,omitempty"` // HMAC-SHA256 signing key for webhook payloads
	AutomationID string           `json:"automation_id,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// EffectivePriority returns the hook's priority, defaulting to defaultHookPriority
// when Priority is zero.
func (h *Hook) EffectivePriority() int {
	if h.Priority == 0 {
		return defaultHookPriority
	}
	return h.Priority
}

// EffectivePhase returns the hook's phase, defaulting to HookPhasePost when empty.
func (h *Hook) EffectivePhase() HookPhase {
	if h.Phase == "" {
		return HookPhasePost
	}
	return h.Phase
}

func (h Hook) Scope() domainscope.Ref {
	return domainscope.Ref{AutomationID: strings.TrimSpace(h.AutomationID)}.Normalize()
}

func (h Hook) MatchesScope(scope domainscope.Ref) bool {
	hookScope := h.Scope()
	scope = scope.Normalize()
	if hookScope.IsZero() {
		return true
	}
	if scope.IsZero() {
		return false
	}
	if hookScope.AutomationID != "" && hookScope.AutomationID != scope.AutomationID {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Hook context
// ---------------------------------------------------------------------------

// HookContext provides contextual information when a hook fires.
type HookContext struct {
	SessionID   string         `json:"session_id,omitempty"`
	RunID       string         `json:"run_id,omitempty"`
	ToolName    string         `json:"tool_name,omitempty"`
	Phase       HookPhase      `json:"phase"`
	TriggerTime time.Time      `json:"trigger_time"`
	Payload     map[string]any `json:"payload,omitempty"`
}

// ---------------------------------------------------------------------------
// Hook result
// ---------------------------------------------------------------------------

// HookResult records the outcome of a single hook execution.
type HookResult struct {
	HookID         string         `json:"hook_id"`
	HookName       string         `json:"hook_name,omitempty"`
	HookKind       HookKind       `json:"hook_kind,omitempty"`
	TargetLabel    string         `json:"target_label,omitempty"`
	Trigger        TriggerEvent   `json:"trigger"`
	Phase          HookPhase      `json:"phase,omitempty"`
	Status         string         `json:"status"` // "ok" or "error"
	Duration       time.Duration  `json:"duration"`
	AttemptCount   int            `json:"attempt_count,omitempty"`
	Async          bool           `json:"async,omitempty"`
	SessionID      string         `json:"session_id,omitempty"`
	RunID          string         `json:"run_id,omitempty"`
	ToolName       string         `json:"tool_name,omitempty"`
	PayloadPreview map[string]any `json:"payload_preview,omitempty"`
	Summary        string         `json:"summary,omitempty"`
	Error          string         `json:"error,omitempty"`
	ExecutedAt     time.Time      `json:"executed_at"`

	replayPayload map[string]any `json:"-"`
}
