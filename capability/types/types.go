package types

import "time"

// Kind classifies how a capability operates.
type Kind string

const (
	KindService Kind = "service" // local or remote service-level capability
	KindSession Kind = "session" // interactive, stateful (browser, desktop, channel)
	KindJob     Kind = "job"     // async, fire-and-forget (crawler, render, transcode)
)

// Manifest declares what a capability provides.
type Manifest struct {
	Name                 string          `json:"name"` // e.g. "browser", "channel.feishu"
	Kind                 Kind            `json:"kind"`
	SessionScoped        bool            `json:"session_scoped"`
	JobScoped            bool            `json:"job_scoped"`
	Operations           []OperationSpec `json:"operations"`
	ArtifactKinds        []string        `json:"artifact_kinds,omitempty"`
	Events               []string        `json:"events,omitempty"`
	ResourceRequirements map[string]any  `json:"resource_requirements,omitempty"`
	ApprovalPolicy       string          `json:"approval_policy,omitempty"` // "none", "always", "policy"
}

// OperationSpec describes a single invokable operation.
type OperationSpec struct {
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	InputSchema     map[string]any `json:"input_schema,omitempty"`
	OutputSchema    map[string]any `json:"output_schema,omitempty"`
	SideEffectClass string         `json:"side_effect_class,omitempty"` // "read", "local_write", "external_write"
	Idempotent      bool           `json:"idempotent,omitempty"`
	SessionOptional bool           `json:"session_optional,omitempty"` // for session capabilities, allow invoke without a session id
}

// CapabilityStatus reports the runtime health of a registered capability.
type CapabilityStatus string

const (
	StatusReady       CapabilityStatus = "ready"
	StatusUnavailable CapabilityStatus = "unavailable"
	StatusDegraded    CapabilityStatus = "degraded"
)

// Health is the result of a capability health check.
type Health struct {
	Status  CapabilityStatus `json:"status"`
	Message string           `json:"message,omitempty"`
}

// Report is the operator-facing view of a registered capability.
type Report struct {
	Manifest Manifest `json:"manifest"`
	Health   Health   `json:"health"`
}

// SessionHandle represents an open capability session.
type SessionHandle struct {
	ID         string         `json:"id"`
	Capability string         `json:"capability"`
	CreatedAt  time.Time      `json:"created_at"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// JobHandle represents a running capability job.
type JobHandle struct {
	ID         string    `json:"id"`
	Capability string    `json:"capability"`
	Status     string    `json:"status"`             // queued, running, completed, failed, cancelled
	Progress   float64   `json:"progress,omitempty"` // 0.0 to 1.0
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Error      string    `json:"error,omitempty"`
}

// InvokeRequest is the generic envelope for calling a capability operation.
type InvokeRequest struct {
	Operation string         `json:"operation"`
	SessionID string         `json:"session_id,omitempty"`
	JobID     string         `json:"job_id,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
}

// InvokeResult is the generic envelope for an operation response.
type InvokeResult struct {
	OK             bool           `json:"ok"`
	Status         string         `json:"status,omitempty"`
	Summary        string         `json:"summary,omitempty"`
	TranscriptText string         `json:"transcript_text,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
	Blocks         []ResultBlock  `json:"blocks,omitempty"`
	Artifacts      []ArtifactRef  `json:"artifacts,omitempty"`
	Actions        []ResultAction `json:"actions,omitempty"`
	Error          string         `json:"error,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	ArtifactRef    string         `json:"artifact_ref,omitempty"`
}

type ResultBlock struct {
	Kind    string         `json:"kind,omitempty"`
	Title   string         `json:"title,omitempty"`
	Content string         `json:"content,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

type ArtifactRef struct {
	Kind        string         `json:"kind,omitempty"`
	Name        string         `json:"name,omitempty"`
	URI         string         `json:"uri,omitempty"`
	ContentType string         `json:"content_type,omitempty"`
	SizeBytes   int64          `json:"size_bytes,omitempty"`
	PreviewText string         `json:"preview_text,omitempty"`
	Body        []byte         `json:"body,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type ResultAction struct {
	Kind   string         `json:"kind,omitempty"`
	Label  string         `json:"label,omitempty"`
	Target string         `json:"target,omitempty"`
	Params map[string]any `json:"params,omitempty"`
}
