package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	domainrun "github.com/fulcrus/hopclaw/internal/domain/runstate"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
	"github.com/fulcrus/hopclaw/modelrouter"
	"github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/policy"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolspec"
	"github.com/fulcrus/hopclaw/triage"
)

var (
	ErrRunRejected       = errors.New("run rejected by queue mode")
	ErrRunCancelled      = errors.New("run is cancelled")
	ErrTooManyToolRounds = errors.New("too many tool rounds")
	ErrToolExecutorNil   = errors.New("tool executor is required for tool calls")
	ErrModelClientNil    = errors.New("model client is required")
	ErrContextEngineNil  = errors.New("context engine is required")
	ErrApprovalStoreNil  = errors.New("approval store is required for waiting approvals")
	ErrArtifactStoreNil  = errors.New("artifact store is not configured")
	ErrToolDenied        = errors.New("tool denied by policy")
	ErrToolCallLoop      = errors.New("tool call loop detected")

	ErrSessionListUnsupported      = errors.New("session store does not support listing")
	ErrSessionReadUnsupported      = errors.New("session store does not support get")
	ErrSessionKeyLookupUnsupported = errors.New("session store does not support get by key")
	ErrSessionDeleteUnsupported    = errors.New("session store does not support deletion")
	ErrSessionPruneUnsupported     = errors.New("session store does not support pruning")
)

// maxToolRepeatBeforeBreak is the number of consecutive identical tool call
// signatures allowed before the agent aborts with a loop error.
const maxToolRepeatBeforeBreak = 3

// maxToolNoProgressBeforeBreak is the number of consecutive unchanged tool
// outcomes allowed before the agent treats the loop as stalled. A value of 2
// means the third identical outcome triggers recovery/failure handling.
const maxToolNoProgressBeforeBreak = 2

// QueueMode controls how a newly submitted run interacts with existing work in
// the same session queue.
type QueueMode = domainrun.QueueMode

const (
	QueueEnqueue   QueueMode = domainrun.QueueEnqueue
	QueueInterrupt QueueMode = domainrun.QueueInterrupt
	QueueCoalesce  QueueMode = domainrun.QueueCoalesce
	QueueReject    QueueMode = domainrun.QueueReject
)

// RunStatus reports the persisted lifecycle state of a run.
type RunStatus = domainrun.Status

const (
	RunQueued          RunStatus = domainrun.RunQueued
	RunWaitingInput    RunStatus = domainrun.RunWaitingInput
	RunRunning         RunStatus = domainrun.RunRunning
	RunWaitingApproval RunStatus = domainrun.RunWaitingApproval
	RunStreaming       RunStatus = domainrun.RunStreaming
	RunCompleted       RunStatus = domainrun.RunCompleted
	RunFailed          RunStatus = domainrun.RunFailed
	RunCancelled       RunStatus = domainrun.RunCancelled
)

// RunPhase describes the current execution stage within a run's lifecycle.
type RunPhase = domainrun.Phase

const (
	PhasePreparing       RunPhase = domainrun.PhasePreparing
	PhaseWaitingModel    RunPhase = domainrun.PhaseWaitingModel
	PhaseExecutingTools  RunPhase = domainrun.PhaseExecutingTools
	PhaseWaitingApproval RunPhase = domainrun.PhaseWaitingApproval
	PhaseCommitting      RunPhase = domainrun.PhaseCommitting
	PhaseFinalize        RunPhase = domainrun.PhaseFinalize
)

const (
	MetadataKeyAgentProfileName         = "agent_profile_name"
	MetadataKeyAgentProfileModel        = "agent_profile_model"
	MetadataKeyAgentProfileSystemPrompt = "agent_profile_system_prompt"
	MetadataKeyAgentProfileTools        = "agent_profile_tools"
	MetadataKeyAgentProfileSkills       = "agent_profile_skills"
	MetadataKeyAgentProfileMaxTokens    = "agent_profile_max_tokens"
	MetadataKeyAgentProfileSource       = "agent_profile_source"
	MetadataKeyEpisodeBoundaryReason    = "episode_boundary_reason"
	MetadataKeyClarificationSlots       = "clarification_slots"
	MetadataKeyClarificationText        = "clarification_text"
	MetadataKeyClarificationSourceRunID = "clarification_for_run"
)

const RunReasonClarificationSuperseded = "clarification_superseded"

// ExecutionMode selects the orchestration path used to satisfy a run.
type ExecutionMode = domainrun.ExecutionMode

const (
	ExecutionModeDirect   ExecutionMode = domainrun.ExecutionModeDirect
	ExecutionModePlanned  ExecutionMode = domainrun.ExecutionModePlanned
	ExecutionModeWatch    ExecutionMode = domainrun.ExecutionModeWatch
	ExecutionModeWorkflow ExecutionMode = domainrun.ExecutionModeWorkflow
)

// IncomingMessage captures a user submission and the routing metadata needed to
// create or resume a run.
type IncomingMessage struct {
	SessionID       string // optional: caller-specified session ID; empty = auto-generate
	SessionKey      string
	ParentRunID     string
	ExternalEventID string
	Content         string
	ContentBlocks   []contextengine.ContentBlock
	Images          []string // base64-encoded image data (data:mime;base64,... or raw base64)
	Model           string
	AutomationID    string
	Metadata        map[string]any
	SemanticSignal  *SemanticSignal
}

// Session stores the durable conversation transcript and routing metadata for a
// stable session key.
type Session struct {
	// ID is the agent-level session identifier and the authoritative session ID.
	ID           string          `json:"id"`
	Key          string          `json:"key"`
	Model        string          `json:"model"`
	Revision     int64           `json:"revision"`
	MessageCount int             `json:"message_count,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	Scope        domainscope.Ref `json:"scope,omitempty"`
	Metadata     map[string]any  `json:"metadata,omitempty"`
	// Embedded context session. Its inner ID is context-engine-local; always use
	// the top-level Session.ID as the authoritative agent session identifier.
	contextengine.Session
}

// EffectiveAgentProfile records the final agent profile applied to a run after
// overlays and policy are resolved.
type EffectiveAgentProfile struct {
	Name          string   `json:"name"`
	Model         string   `json:"model,omitempty"`
	SystemPrompt  string   `json:"system_prompt,omitempty"`
	AllowedTools  []string `json:"allowed_tools,omitempty"`
	AllowedSkills []string `json:"allowed_skills,omitempty"`
	MaxTokens     int      `json:"max_tokens,omitempty"`
	Source        string   `json:"source,omitempty"`
}

// TaskContract summarizes the parsed intent, deliverables, and missing inputs
// for a run.
type TaskContract struct {
	Goal                   string                     `json:"goal"`
	JobType                string                     `json:"job_type,omitempty"`
	TargetSummary          string                     `json:"target_summary,omitempty"`
	SuggestedDomains       []string                   `json:"suggested_domains,omitempty"`
	CapabilityHints        []string                   `json:"capability_hints,omitempty"`
	ExpectedDeliverables   []TaskContractDeliverable  `json:"expected_deliverables,omitempty"`
	AcceptanceCriteria     []TaskContractAcceptance   `json:"acceptance_criteria,omitempty"`
	MissingInfo            []TaskContractMissingInfo  `json:"missing_info,omitempty"`
	ResolvedInfo           []TaskContractResolvedInfo `json:"resolved_info,omitempty"`
	RequiresExternalEffect bool                       `json:"requires_external_effect,omitempty"`
	RequiresApproval       bool                       `json:"requires_approval,omitempty"`
	Confidence             float64                    `json:"confidence,omitempty"`
	Source                 string                     `json:"source,omitempty"`
	GeneratedAt            time.Time                  `json:"generated_at,omitempty"`
}

// TaskContractDeliverable describes one expected output from a task contract.
type TaskContractDeliverable struct {
	Kind     string `json:"kind"`
	Summary  string `json:"summary,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// TaskContractAcceptance defines one acceptance criterion used to evaluate a
// task contract's outputs.
type TaskContractAcceptance struct {
	ID               string   `json:"id"`
	Summary          string   `json:"summary"`
	Required         bool     `json:"required,omitempty"`
	DeliverableKinds []string `json:"deliverable_kinds,omitempty"`
	EvidenceHints    []string `json:"evidence_hints,omitempty"`
}

// TaskContractMissingInfo captures a required input the agent still needs from
// the caller.
type TaskContractMissingInfo struct {
	ID          string   `json:"id"`
	Label       string   `json:"label,omitempty"`
	Summary     string   `json:"summary"`
	Question    string   `json:"question,omitempty"`
	InputMode   string   `json:"input_mode,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Hints       []string `json:"hints,omitempty"`
}

// TaskContractResolvedInfo records caller inputs that were already satisfied
// before execution continues.
type TaskContractResolvedInfo struct {
	ID        string `json:"id"`
	Label     string `json:"label,omitempty"`
	Value     string `json:"value,omitempty"`
	Source    string `json:"source,omitempty"`
	InputMode string `json:"input_mode,omitempty"`
}

// DelegationContract constrains the domain, tool, and approval budget for
// delegated work.
type DelegationContract struct {
	Goal                string    `json:"goal,omitempty"`
	AllowedDomains      []string  `json:"allowed_domains,omitempty"`
	AllowedTools        []string  `json:"allowed_tools,omitempty"`
	SideEffectClass     string    `json:"side_effect_class,omitempty"`
	MaxTurns            int       `json:"max_turns,omitempty"`
	MaxBudgetTokens     int       `json:"max_budget_tokens,omitempty"`
	RequiresApproval    bool      `json:"requires_approval,omitempty"`
	VerificationPlanRef string    `json:"verification_plan_ref,omitempty"`
	Source              string    `json:"source,omitempty"`
	GeneratedAt         time.Time `json:"generated_at,omitempty"`
}

// SessionSummary is a lightweight representation of a session for list endpoints.
type SessionSummary struct {
	ID           string          `json:"id"`
	Key          string          `json:"key"`
	Model        string          `json:"model"`
	Revision     int64           `json:"revision"`
	MessageCount int             `json:"message_count"`
	Scope        domainscope.Ref `json:"scope,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// ToSummary returns a list-safe projection of the session and normalizes the
// visible message count.
func (s *Session) ToSummary() SessionSummary {
	messageCount := 0
	if s != nil {
		messageCount = s.TotalMessageCount()
	}
	return SessionSummary{
		ID:           s.ID,
		Key:          s.Key,
		Model:        s.Model,
		Revision:     s.Revision,
		MessageCount: messageCount,
		Scope:        s.Scope.Normalize(),
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
	}
}

// TotalMessageCount returns the larger of the stored message count and the
// in-memory transcript length.
func (s *Session) TotalMessageCount() int {
	if s == nil {
		return 0
	}
	if len(s.Messages) > s.MessageCount {
		return len(s.Messages)
	}
	return s.MessageCount
}

// Run stores the durable execution state for one submitted task.
type Run struct {
	ID                  string                 `json:"id"`
	SessionID           string                 `json:"session_id"`
	Scope               domainscope.Ref        `json:"scope,omitempty"`
	ParentRunID         string                 `json:"parent_run_id,omitempty"`
	InputEventID        string                 `json:"input_event_id,omitempty"`
	Status              RunStatus              `json:"status"`
	QueueMode           QueueMode              `json:"queue_mode"`
	Phase               RunPhase               `json:"phase"`
	ExecutionMode       ExecutionMode          `json:"execution_mode,omitempty"`
	Model               string                 `json:"model"`
	EffectiveProfile    *EffectiveAgentProfile `json:"effective_agent_profile,omitempty"`
	LastSessionRevision int64                  `json:"last_session_revision,omitempty"`
	ToolRounds          int                    `json:"tool_rounds"`
	ToolRecoveryCount   int                    `json:"tool_recovery_count,omitempty"`
	SemanticSignal      *SemanticSignal        `json:"semantic_signal,omitempty"`
	Triage              *RunTriageTrace        `json:"triage,omitempty"`
	TaskContract        *TaskContract          `json:"task_contract,omitempty"`
	Delegation          *DelegationContract    `json:"delegation,omitempty"`
	Governance          *domaingov.Evaluation  `json:"governance,omitempty"`
	ApprovalID          string                 `json:"approval_id,omitempty"`
	PendingTools        []ToolCall             `json:"pending_tools,omitempty"`
	Preflight           *RunPreflightReport    `json:"preflight,omitempty"`
	Error               string                 `json:"error,omitempty"`
	Plan                *planner.Plan          `json:"plan,omitempty"`
	ExecutionGraph      *ExecutionGraph        `json:"execution_graph,omitempty"`
	WorkflowState       *WorkflowState         `json:"workflow_state,omitempty"`
	StartedAt           time.Time              `json:"started_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
	FinishedAt          time.Time              `json:"finished_at,omitempty"`
}

// TurnSnapshot captures the prepared model input for one execution turn.
type TurnSnapshot struct {
	SessionID        string
	RunID            string
	SessionRevision  int64
	Model            string
	SystemPrompt     string
	Messages         []contextengine.Message
	Tools            []ToolDefinition
	Budget           contextengine.Budget
	EffectiveProfile *EffectiveAgentProfile
}

// RunTriageTrace records why a run was routed to a particular execution mode.
type RunTriageTrace struct {
	Source              string        `json:"source,omitempty"`
	Mode                ExecutionMode `json:"mode,omitempty"`
	NeedsReference      bool          `json:"needs_reference,omitempty"`
	NeedsReferenceSet   bool          `json:"needs_reference_set,omitempty"`
	NeedsConfirmation   bool          `json:"needs_confirmation,omitempty"`
	NeedsConfirmSet     bool          `json:"needs_confirmation_set,omitempty"`
	RequiresCurrentInfo bool          `json:"requires_current_info,omitempty"`
	Reason              string        `json:"reason,omitempty"`
	Error               string        `json:"error,omitempty"`
	Confidence          float64       `json:"confidence,omitempty"`
	SuggestedDomains    []string      `json:"suggested_domains,omitempty"`
	CacheHit            bool          `json:"cache_hit,omitempty"`
	GeneratedAt         time.Time     `json:"generated_at,omitempty"`
}

// RunPreflightState reports whether a run can proceed or still needs user
// confirmation.
type RunPreflightState string

const (
	RunPreflightReady             RunPreflightState = "ready"
	RunPreflightAutoPreparing     RunPreflightState = "auto_preparing"
	RunPreflightNeedsConfirmation RunPreflightState = "needs_confirmation"
)

// RunPreflightCheck describes one blocking or advisory check emitted by
// preflight analysis.
type RunPreflightCheck struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	State    RunPreflightState `json:"state"`
	Detail   string            `json:"detail,omitempty"`
	Blocking bool              `json:"blocking,omitempty"`
}

// RunClarificationSlot defines one missing field the user can fill to unblock
// execution.
type RunClarificationSlot struct {
	ID          string   `json:"id"`
	Label       string   `json:"label,omitempty"`
	Question    string   `json:"question,omitempty"`
	InputMode   string   `json:"input_mode,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Hints       []string `json:"hints,omitempty"`
}

// RunPreflightReport captures the user-facing result of preflight analysis for
// a run.
type RunPreflightReport struct {
	State              RunPreflightState      `json:"state"`
	Summary            string                 `json:"summary,omitempty"`
	Prompt             string                 `json:"prompt,omitempty"`
	Question           string                 `json:"question,omitempty"`
	ReplyHints         []string               `json:"reply_hints,omitempty"`
	ReplyTemplate      string                 `json:"reply_template,omitempty"`
	ContinueHint       string                 `json:"continue_hint,omitempty"`
	SuggestedDomains   []string               `json:"suggested_domains,omitempty"`
	DetectedDomains    []string               `json:"detected_domains,omitempty"`
	Blocking           bool                   `json:"blocking,omitempty"`
	Checks             []RunPreflightCheck    `json:"checks,omitempty"`
	ClarificationSlots []RunClarificationSlot `json:"clarification_slots,omitempty"`
	GeneratedAt        time.Time              `json:"generated_at"`
}

// ToolCall represents one tool invocation requested by the model.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ModelUsageInfo captures token usage from a model API response.
type ModelUsageInfo struct {
	PromptTokens             int `json:"prompt_tokens"`
	CompletionTokens         int `json:"completion_tokens"`
	TotalTokens              int `json:"total_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// ModelResponse holds one model turn, including assistant text, tool calls,
// and optional usage data.
type ModelResponse struct {
	Message   contextengine.Message
	ToolCalls []ToolCall
	Usage     *ModelUsageInfo
}

// IsText reports whether the response contains assistant text and no tool
// calls.
func (r ModelResponse) IsText() bool {
	return len(r.ToolCalls) == 0 && r.Message.Content != ""
}

// IsToolCalls reports whether the response requests at least one tool call.
func (r ModelResponse) IsToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// ThinkingMode controls the model's thinking/reasoning behavior.
type ThinkingMode string

const (
	ThinkingDefault  ThinkingMode = ""         // use model default
	ThinkingExtended ThinkingMode = "extended" // request extended thinking
	ThinkingRegular  ThinkingMode = "regular"  // regular mode (no extended thinking)
)

// ChatRequest is the normalized model request assembled from a prepared turn.
type ChatRequest struct {
	SessionID    string
	RunID        string
	Model        string
	SystemPrompt string
	Messages     []contextengine.Message
	Tools        []ToolDefinition
	Budget       contextengine.Budget
	Temperature  float64      // model sampling temperature; 0 = deterministic
	ThinkingMode ThinkingMode // controls thinking/reasoning behavior
}

// ToolAvailabilityStatus aliases toolspec.ToolAvailabilityStatus for
// agent-facing tool readiness states.
type ToolAvailabilityStatus = toolspec.ToolAvailabilityStatus

// ToolAvailabilityCheck aliases toolspec.ToolAvailabilityCheck for individual
// availability findings.
type ToolAvailabilityCheck = toolspec.ToolAvailabilityCheck

// ToolAvailability aliases toolspec.ToolAvailability for aggregate tool
// availability metadata.
type ToolAvailability = toolspec.ToolAvailability

// ToolPresentation aliases toolspec.ToolPresentation for user-facing tool
// labels and descriptions.
type ToolPresentation = toolspec.ToolPresentation

// ToolDefinition aliases toolspec.ToolDefinition for the canonical tool
// specification exposed to models.
type ToolDefinition = toolspec.ToolDefinition

// ResolvedTool aliases toolspec.ResolvedTool for executable tool resolutions.
type ResolvedTool = toolspec.ResolvedTool

const (
	AvailabilityReady        = toolspec.AvailabilityReady
	AvailabilityDegraded     = toolspec.AvailabilityDegraded
	AvailabilityBlocked      = toolspec.AvailabilityBlocked
	AvailabilityDiscoverable = toolspec.AvailabilityDiscoverable
)

// AgentConfig controls default prompts, queueing, and tool-execution limits for
// an AgentComponent.
type AgentConfig struct {
	SystemPrompt            string
	DefaultModel            string
	MaxRunDuration          time.Duration
	MaxToolRounds           int
	MaxToolRecoveryAttempts int
	MaxClarificationRounds  int
	QueueMode               QueueMode
	DedupeWindow            time.Duration
	Retry                   RetryConfig
}

// SessionStore persists sessions and appends new user input before execution
// begins.
type SessionStore interface {
	// GetOrCreate looks up a session by sessionKey. If not found, creates one.
	// sessionID is optional: if non-empty, the new session uses it as ID; otherwise auto-generated.
	GetOrCreate(ctx context.Context, sessionKey string, defaultModel string, sessionID ...string) (*Session, error)
	AppendUserMessage(ctx context.Context, sessionID string, msg IncomingMessage) error
	LoadForExecution(ctx context.Context, sessionID string) (*Session, func(), error)
	Save(ctx context.Context, session *Session) error
}

// SessionEpisodeManager is an optional SessionStore capability for automatic
// episode boundary management.
type SessionEpisodeManager interface {
	EnsureActiveEpisode(ctx context.Context, sessionID string, reason string) (episodeID string, err error)
	StartNewEpisode(ctx context.Context, sessionID string, reason string) (episodeID string, err error)
}

// ExecutionSnapshot is the hot-path execution view of a session. Phase 1 only
// populates the session metadata plus the hot message tail after the current
// execution watermark.
type ExecutionSnapshot struct {
	Session *Session
}

// ExecutionSnapshotStore is an optional interface for SessionStore
// implementations that can load a hot execution snapshot without materializing
// the full persisted transcript.
type ExecutionSnapshotStore interface {
	LoadExecutionSnapshot(ctx context.Context, sessionID string) (*ExecutionSnapshot, func(), error)
}

// SessionLister is an optional interface for SessionStore implementations
// that support listing sessions. Used by session.list tool.
type SessionLister interface {
	List(ctx context.Context) ([]*Session, error)
}

// ScopedSessionLister is an optional interface for SessionStore
// implementations that can apply scope filters in the storage layer.
type ScopedSessionLister interface {
	ListScoped(ctx context.Context, filter SessionListFilter) ([]*Session, error)
}

// SessionReader is an optional interface for SessionStore implementations
// that support read-only session access. Used by session.history tool.
type SessionReader interface {
	Get(ctx context.Context, sessionID string) (*Session, error)
}

// ScopedSessionReader is an optional interface for SessionStore
// implementations that can enforce scope filters in the storage layer.
type ScopedSessionReader interface {
	GetScoped(ctx context.Context, sessionID string, scope ScopeFilter) (*Session, error)
}

// SessionMetadataReader is an optional interface for lightweight session reads
// that do not require loading message history.
type SessionMetadataReader interface {
	GetMetadata(ctx context.Context, sessionID string) (*Session, error)
}

// ScopedSessionMetadataReader is an optional interface for metadata reads that
// can enforce scope filters in the storage layer.
type ScopedSessionMetadataReader interface {
	GetMetadataScoped(ctx context.Context, sessionID string, scope ScopeFilter) (*Session, error)
}

// SessionKeyReader is an optional interface for SessionStore implementations
// that support lookup by stable external session key (for example, channel:chat).
type SessionKeyReader interface {
	GetByKey(ctx context.Context, sessionKey string) (*Session, error)
}

// ScopedSessionKeyReader is an optional interface for SessionStore
// implementations that can enforce scope filters when looking up by session key.
type ScopedSessionKeyReader interface {
	GetByKeyScoped(ctx context.Context, sessionKey string, scope ScopeFilter) (*Session, error)
}

// SessionKeyMetadataReader is an optional interface for lightweight lookup by
// stable session key without loading full message history.
type SessionKeyMetadataReader interface {
	GetByKeyMetadata(ctx context.Context, sessionKey string) (*Session, error)
}

// ScopedSessionKeyMetadataReader is an optional interface for metadata lookup
// by stable session key with scope enforcement in the storage layer.
type ScopedSessionKeyMetadataReader interface {
	GetByKeyMetadataScoped(ctx context.Context, sessionKey string, scope ScopeFilter) (*Session, error)
}

// SessionRecentMessageReader is an optional interface for fetching only the
// tail of a session transcript when read paths do not need full history.
type SessionRecentMessageReader interface {
	RecentMessages(ctx context.Context, sessionID string, limit int) ([]contextengine.Message, error)
}

// SessionDeleter is an optional interface for SessionStore implementations
// that support deleting sessions. Used by the DELETE /runtime/sessions/{id} endpoint.
type SessionDeleter interface {
	DeleteSession(ctx context.Context, sessionID string) error
}

// SessionQueryStore is the preferred optional read contract for SessionStore
// implementations that want to expose the full query surface through one
// stable capability boundary. Legacy fine-grained interfaces remain supported
// as compatibility fallbacks.
type SessionQueryStore interface {
	ScopedSessionLister
	ScopedSessionReader
	ScopedSessionMetadataReader
	ScopedSessionKeyReader
	ScopedSessionKeyMetadataReader
	SessionRecentMessageReader
}

// SessionMaintenanceStore is the preferred optional mutation/retention
// contract for SessionStore implementations that support delete and prune.
// Legacy SessionDeleter / ScopedSessionPruner remain supported as fallbacks.
type SessionMaintenanceStore interface {
	SessionDeleter
	ScopedSessionPruner
}

// RunStore persists runs and coordinates queue-claim semantics for execution.
type RunStore interface {
	Seen(ctx context.Context, externalEventID string, within time.Duration) bool
	FindByExternalEvent(ctx context.Context, externalEventID string) (*Run, error)
	Get(ctx context.Context, runID string) (*Run, error)
	Create(ctx context.Context, sessionID string, msg IncomingMessage, cfg AgentConfig) (*Run, error)
	ClaimQueuedRun(ctx context.Context, runID string) (*Run, bool, error)
	Update(ctx context.Context, run *Run) error
}

// RunLister is an optional interface for RunStore implementations
// that support listing runs. Used by the operator API.
type RunLister interface {
	List(ctx context.Context, filter RunListFilter) ([]*Run, error)
}

// ScopedRunReader is an optional interface for RunStore implementations that
// can enforce scope filters in the storage layer.
type ScopedRunReader interface {
	GetScoped(ctx context.Context, runID string, scope ScopeFilter) (*Run, error)
}

// ScopeFilter constrains reads to explicit automation scopes when present.
type ScopeFilter struct {
	AutomationIDs []string `json:"automation_ids,omitempty"`
	Deny          bool     `json:"deny,omitempty"`
}

// Normalize trims, de-duplicates, and canonicalizes the filter.
func (f ScopeFilter) Normalize() ScopeFilter {
	out := ScopeFilter{Deny: f.Deny}
	if len(f.AutomationIDs) == 0 {
		return out
	}
	seen := make(map[string]struct{}, len(f.AutomationIDs))
	out.AutomationIDs = make([]string, 0, len(f.AutomationIDs))
	for _, raw := range f.AutomationIDs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out.AutomationIDs = append(out.AutomationIDs, trimmed)
	}
	if len(out.AutomationIDs) == 0 {
		out.AutomationIDs = nil
	}
	return out
}

// IsZero reports whether the filter is effectively empty.
func (f ScopeFilter) IsZero() bool {
	normalized := f.Normalize()
	return !normalized.Deny && len(normalized.AutomationIDs) == 0
}

// Matches reports whether the provided scope satisfies the filter.
func (f ScopeFilter) Matches(scope domainscope.Ref) bool {
	normalized := f.Normalize()
	if normalized.Deny {
		return false
	}
	if len(normalized.AutomationIDs) == 0 {
		return true
	}
	scope = scope.Normalize()
	for _, allowed := range normalized.AutomationIDs {
		if scope.AutomationID == allowed {
			return true
		}
	}
	return false
}

// SessionListFilter controls which sessions are returned by SessionLister.List.
type SessionListFilter struct {
	Scope ScopeFilter
	Limit int // 0 = no limit
}

// RunListFilter controls which runs are returned by RunLister.List.
type RunListFilter struct {
	SessionID string    // empty = all sessions
	Status    RunStatus // empty = all statuses
	Scope     ScopeFilter
	Limit     int // 0 = no limit
}

// SessionPruner removes sessions older than the supplied cutoff.
type SessionPruner interface {
	PruneSessions(ctx context.Context, before time.Time) (int, error)
}

// ScopedSessionPruner prunes old sessions while preserving explicitly retained
// session IDs.
type ScopedSessionPruner interface {
	PruneSessionsExcept(ctx context.Context, before time.Time, excludeSessionIDs []string) (int, error)
}

// RunPruner removes runs older than the supplied cutoff.
type RunPruner interface {
	PruneRuns(ctx context.Context, before time.Time) (int, error)
}

// Coordinator serializes run execution within a session queue.
type Coordinator interface {
	EnqueueSessionRun(ctx context.Context, sessionID, runID string, mode QueueMode) error
	NextQueuedRun(ctx context.Context, sessionID string) (runID string, ok bool, err error)
	StartRun(ctx context.Context, sessionID, runID string) error
	FinishRun(ctx context.Context, sessionID, runID string) error
}

// ModelClient executes one fully prepared chat request against a model
// provider.
type ModelClient interface {
	Chat(ctx context.Context, req ChatRequest) (*ModelResponse, error)
}

// StreamCallback receives incremental model output during streaming.
// Implementations must be safe for concurrent use.
type StreamCallback interface {
	OnTextDelta(ctx context.Context, delta string)
	OnReasoningDelta(ctx context.Context, delta string)
	OnToolCallStart(ctx context.Context, toolCallID, toolName string)
	OnToolCallDelta(ctx context.Context, toolCallID, argDelta string)
	OnComplete(ctx context.Context)
	OnError(ctx context.Context, err error)
}

// StreamingModelClient extends ModelClient with streaming support.
type StreamingModelClient interface {
	ModelClient
	ChatStream(ctx context.Context, req ChatRequest, cb StreamCallback) (*ModelResponse, error)
}

// ToolExecutor runs one or more tool calls for a run and returns the ordered
// tool results.
type ToolExecutor interface {
	ExecuteBatch(ctx context.Context, run *Run, session *Session, calls []ToolCall) ([]contextengine.ToolResult, error)
}

// ToolDefinitionProvider exposes the model-visible tools for a session.
type ToolDefinitionProvider interface {
	ToolDefinitions(session *Session) []ToolDefinition
}

// ToolResolver resolves a tool name to its executable implementation for a
// session.
type ToolResolver interface {
	ResolveTool(session *Session, name string) (*ResolvedTool, bool)
}

// RuntimeContextProvider derives runtime facts, secrets, and capabilities for a
// session/run pair.
type RuntimeContextProvider interface {
	Current(ctx context.Context, session *Session, run *Run) (skill.RuntimeContext, error)
}

// ModelRouter aliases modelrouter.Router for model routing decisions.
type ModelRouter = modelrouter.Router

// PolicyEngine aliases policy.Engine for tool-approval and access decisions.
type PolicyEngine = policy.Engine

// ApprovalStore aliases approval.Store for run approval persistence.
type ApprovalStore = approval.Store

// EventBus aliases eventbus.Bus for run lifecycle publication.
type EventBus = eventbus.Bus

// Planner produces a task plan for runs that require structured execution.
type Planner interface {
	Plan(ctx context.Context, req PlanningRequest) (*planner.Plan, error)
}

// ExecutionModeSelector chooses the orchestration mode for an incoming request.
type ExecutionModeSelector interface {
	Select(ctx context.Context, req ExecutionModeRequest) (ExecutionModeDecision, error)
}

// PlanningRequest is the planner input assembled from the current turn state.
type PlanningRequest struct {
	Model           string
	LatestMessage   string
	SessionSummary  string
	RecentMessages  []contextengine.Message
	AvailableTools  []ToolDefinition
	TaskContract    *TaskContract
	Delegation      *DelegationContract
	PinnedFacts     []string
	SessionState    string
	RecalledContext string
}

// ExecutionModeRequest is the selector input used to choose a run mode.
type ExecutionModeRequest struct {
	Model            string
	Message          string
	SessionSummary   string
	PlannerAvailable bool
}

// ExecutionModeDecision records the selected execution mode and its confidence.
type ExecutionModeDecision struct {
	Mode       ExecutionMode `json:"mode"`
	Reason     string        `json:"reason,omitempty"`
	Confidence float64       `json:"confidence,omitempty"`
}

// PreflightAnalysisRequest is the analyzer input used to decide whether a run
// needs clarification before execution.
type PreflightAnalysisRequest struct {
	Model          string
	Message        string
	SessionSummary string
	ExecutionMode  ExecutionMode
	SemanticSignal *SemanticSignal `json:"semantic_signal,omitempty"`
}

// PreflightAnalysis captures the structured output of preflight analysis before
// it is turned into a user-facing report.
type PreflightAnalysis struct {
	NeedsReference           bool     `json:"needs_reference,omitempty"`
	NeedsConfirmation        bool     `json:"needs_confirmation,omitempty"`
	SuggestedDomains         []string `json:"suggested_domains,omitempty"`
	DetectedDomains          []string `json:"detected_domains,omitempty"`
	BrowserContextOnly       bool     `json:"browser_context_only,omitempty"`
	Reason                   string   `json:"reason,omitempty"`
	Confidence               float64  `json:"confidence,omitempty"`
	NeedsReferenceSet        bool     `json:"-"`
	NeedsConfirmSet          bool     `json:"-"`
	DomainsSpecified         bool     `json:"-"`
	DetectedDomainsSpecified bool     `json:"-"`
}

// PreflightAnalyzer evaluates whether a request can proceed or must wait for
// user confirmation.
type PreflightAnalyzer interface {
	Analyze(ctx context.Context, req PreflightAnalysisRequest) (PreflightAnalysis, error)
}

// TaskContractAnalysisRequest is the analyzer input used to derive a task
// contract from a request.
type TaskContractAnalysisRequest struct {
	Model            string          `json:"model,omitempty"`
	Message          string          `json:"message"`
	SessionSummary   string          `json:"session_summary,omitempty"`
	ExecutionMode    ExecutionMode   `json:"execution_mode,omitempty"`
	SuggestedDomains []string        `json:"suggested_domains,omitempty"`
	PreflightState   string          `json:"preflight_state,omitempty"`
	SemanticSignal   *SemanticSignal `json:"semantic_signal,omitempty"`
}

// TaskContractAnalysis captures structured signals used to build a TaskContract.
type TaskContractAnalysis struct {
	JobType                string   `json:"job_type,omitempty"`
	TargetSummary          string   `json:"target_summary,omitempty"`
	SuggestedDomains       []string `json:"suggested_domains,omitempty"`
	CapabilityHints        []string `json:"capability_hints,omitempty"`
	DeliverableKinds       []string `json:"deliverable_kinds,omitempty"`
	MissingInfoIDs         []string `json:"missing_info_ids,omitempty"`
	BrowserContextOnly     bool     `json:"browser_context_only,omitempty"`
	MissingInfoSpecified   bool     `json:"-"`
	RequiresExternalEffect *bool    `json:"requires_external_effect,omitempty"`
	RequiresApproval       *bool    `json:"requires_approval,omitempty"`
	Reason                 string   `json:"reason,omitempty"`
	Confidence             float64  `json:"confidence,omitempty"`
}

// TaskContractAnalyzer derives structured task metadata from a request.
type TaskContractAnalyzer interface {
	Analyze(ctx context.Context, req TaskContractAnalysisRequest) (TaskContractAnalysis, error)
}

// RunTriageAnalyzer classifies a run before execution begins.
type RunTriageAnalyzer interface {
	AnalyzeRun(ctx context.Context, req triage.RunRequest) (triage.RunDecision, error)
}

// WatchWorkflow creates watch jobs that re-trigger work from external changes.
type WatchWorkflow interface {
	Create(ctx context.Context, req WatchWorkflowRequest) (*WatchWorkflowResult, error)
}

// WatchCancelWorkflow removes existing watch jobs that match a cancellation
// request.
type WatchCancelWorkflow interface {
	Cancel(ctx context.Context, req WatchWorkflowCancelRequest) (*WatchWorkflowCancelResult, error)
}

// WatchWorkflowRequest collects the source and scheduling fields required to
// create a watch.
type WatchWorkflowRequest struct {
	RunID            string
	SessionKey       string
	Name             string
	SourceKind       string
	SourceURL        string
	SourcePath       string
	SourceSessionKey string
	CalendarQuery    string
	MailboxFolder    string
	MailboxQuery     string
	WebhookID        string
	WebhookSenderID  string
	InboxLimit       int
	Interval         string
	Prompt           string
	Model            string
	FireOnStart      bool
}

// WatchWorkflowResult returns the persisted watch metadata after creation.
type WatchWorkflowResult struct {
	WatchID          string
	Name             string
	SourceKind       string
	SourceURL        string
	SourcePath       string
	SourceSessionKey string
	CalendarQuery    string
	MailboxFolder    string
	MailboxQuery     string
	WebhookID        string
	WebhookSenderID  string
	InboxLimit       int
	Interval         string
	Summary          string
}

// WatchWorkflowCancelRequest identifies watches to remove for a session or run.
type WatchWorkflowCancelRequest struct {
	RunID      string
	SessionKey string
	Query      string
	TargetRef  string
	RemoveAll  bool
}

// WatchWorkflowCancelResult reports which watches were removed and why.
type WatchWorkflowCancelResult struct {
	RemovedWatchIDs []string
	Summary         string
}

// StaticRuntimeContextProvider always returns the configured runtime context.
type StaticRuntimeContextProvider struct {
	RuntimeContext skill.RuntimeContext
}

// Current returns the provider's static runtime context and never fails.
func (p StaticRuntimeContextProvider) Current(context.Context, *Session, *Run) (skill.RuntimeContext, error) {
	return p.RuntimeContext, nil
}

// AutoRuntimeContextProvider detects runtime context from the environment.
// DetectFunc is called once on first use; the result is cached via sync.Once.
type AutoRuntimeContextProvider struct {
	Base       skill.RuntimeContext // Merged on top of detected context.
	DetectFunc func(string) skill.RuntimeContext
	WorkDir    string
	once       sync.Once
	detected   skill.RuntimeContext
}

// Current detects runtime context once, merges Base overrides, and returns the
// cached result on later calls.
func (p *AutoRuntimeContextProvider) Current(_ context.Context, _ *Session, _ *Run) (skill.RuntimeContext, error) {
	p.once.Do(func() {
		detect := p.DetectFunc
		if detect == nil {
			p.detected = p.Base
			return
		}
		ctx := detect(p.WorkDir)
		// Merge base overrides.
		if p.Base.SecretPresence != nil {
			if ctx.SecretPresence == nil {
				ctx.SecretPresence = make(map[string]skill.SecretStatus)
			}
			for k, v := range p.Base.SecretPresence {
				ctx.SecretPresence[k] = v
			}
		}
		if p.Base.ConfigTruth != nil {
			if ctx.ConfigTruth == nil {
				ctx.ConfigTruth = make(map[string]skill.ConfigStatus)
			}
			for k, v := range p.Base.ConfigTruth {
				ctx.ConfigTruth[k] = v
			}
		}
		if p.Base.Managed != nil {
			if ctx.Managed == nil {
				ctx.Managed = make(map[string]skill.ManagedEntry)
			}
			for k, v := range p.Base.Managed {
				ctx.Managed[k] = v
			}
		}
		if len(p.Base.ModuleCapabilities) > 0 {
			ctx.ModuleCapabilities = append([]string(nil), p.Base.ModuleCapabilities...)
		}
		p.detected = ctx
	})
	return p.detected, nil
}
