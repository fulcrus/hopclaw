package acp

import (
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
)

// ---------------------------------------------------------------------------
// Session status
// ---------------------------------------------------------------------------

// SessionStatus represents the current state of an ACP session.
type SessionStatus string

const (
	SessionIdle      SessionStatus = "idle"
	SessionStreaming SessionStatus = "streaming"
	SessionToolUse   SessionStatus = "tool_use"
	SessionCompleted SessionStatus = "completed"
	SessionError     SessionStatus = "error"
)

// ---------------------------------------------------------------------------
// Stop reasons
// ---------------------------------------------------------------------------

// StopReason indicates why an agent run stopped.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	StopCancelled StopReason = "cancelled"
	StopError     StopReason = "error"
)

// ---------------------------------------------------------------------------
// Config option keys
// ---------------------------------------------------------------------------

// ConfigOptionKey identifies a runtime configuration option exposed to ACP
// clients.
type ConfigOptionKey string

const (
	ConfigThoughtLevel   ConfigOptionKey = "thought_level"
	ConfigVerboseLevel   ConfigOptionKey = "verbose_level"
	ConfigReasoningLevel ConfigOptionKey = "reasoning_level"
	ConfigResponseUsage  ConfigOptionKey = "response_usage"
	ConfigElevatedLevel  ConfigOptionKey = "elevated_level"
)

// ---------------------------------------------------------------------------
// Request parameter types
// ---------------------------------------------------------------------------

// InitializeParams is sent by the client as the first request to negotiate
// protocol version and capabilities.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocol_version"`
	ClientInfo      Implementation `json:"client_info"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
}

// Implementation identifies a client or server by name and version.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// NewSessionParams requests creation of a new agent session.
type NewSessionParams struct {
	SessionID  string         `json:"session_id,omitempty"`
	SessionKey string         `json:"session_key,omitempty"`
	Meta       map[string]any `json:"_meta,omitempty"`
}

// LoadSessionParams requests loading an existing session by gateway key.
type LoadSessionParams struct {
	SessionKey string `json:"session_key"`
	SessionID  string `json:"session_id,omitempty"`
}

// PromptParams submits a user message to an active session.
type PromptParams struct {
	SessionID          string                       `json:"session_id"`
	Message            string                       `json:"message"`
	ContentBlocks      []contextengine.ContentBlock `json:"content_blocks,omitempty"`
	Images             []string                     `json:"images,omitempty"`
	Model              string                       `json:"model,omitempty"`
	StructuredCommand  *StructuredCommand           `json:"structured_command,omitempty"`
	StructuredApproval *StructuredApproval          `json:"structured_approval,omitempty"`
}

type PromptOptions struct {
	Model              string
	ContentBlocks      []contextengine.ContentBlock
	StructuredCommand  *StructuredCommand
	StructuredApproval *StructuredApproval
}

// StructuredCommand carries a pre-parsed terminal action such as retry.
type StructuredCommand struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id,omitempty"`
}

// StructuredApproval carries a pre-parsed approval action.
type StructuredApproval struct {
	Action string `json:"action"`
}

// CancelParams requests cancellation of the active run in a session.
type CancelParams struct {
	SessionID string `json:"session_id"`
}

// ListSessionsParams controls pagination when listing sessions.
type ListSessionsParams struct {
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// SetSessionModeParams changes the operating mode of a session.
type SetSessionModeParams struct {
	SessionID string `json:"session_id"`
	Mode      string `json:"mode"`
}

// SetConfigOptionParams updates a runtime configuration option.
type SetConfigOptionParams struct {
	SessionID string          `json:"session_id"`
	Key       ConfigOptionKey `json:"key"`
	Value     string          `json:"value"`
}

// ---------------------------------------------------------------------------
// Response / notification types
// ---------------------------------------------------------------------------

// InitializeResult is returned by the server after a successful handshake.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocol_version"`
	ServerInfo      Implementation `json:"server_info"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
}

// SessionInfo describes a session for listing or creation responses.
type SessionInfo struct {
	SessionID  string        `json:"session_id"`
	SessionKey string        `json:"session_key"`
	Status     SessionStatus `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
}

// SessionUpdateNotification carries streaming updates from the server.
type SessionUpdateNotification struct {
	SessionID     string             `json:"session_id"`
	RunID         string             `json:"run_id,omitempty"`
	Status        SessionStatus      `json:"status"`
	StopReason    StopReason         `json:"stop_reason,omitempty"`
	TextDelta     string             `json:"text_delta,omitempty"`
	ToolName      string             `json:"tool_name,omitempty"`
	ToolInput     string             `json:"tool_input,omitempty"`
	ToolOutput    string             `json:"tool_output,omitempty"`
	Usage         *UsageInfo         `json:"usage,omitempty"`
	ModelFailover *ModelFailoverInfo `json:"model_failover,omitempty"`
	Error         string             `json:"error,omitempty"`
	Runless       bool               `json:"runless,omitempty"`
}

// UsageInfo reports token consumption for a run.
type UsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ModelFailoverInfo describes a visible model failover for the current run.
type ModelFailoverInfo struct {
	OriginalModel string `json:"original_model,omitempty"`
	FallbackModel string `json:"fallback_model,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

// PermissionRequest asks the client to approve a tool execution.
type PermissionRequest struct {
	RequestID                  string `json:"request_id"`
	SessionID                  string `json:"session_id"`
	ToolName                   string `json:"tool_name"`
	Description                string `json:"description"`
	Input                      string `json:"input"`
	GovernanceSummary          string `json:"governance_summary,omitempty"`
	PolicySummary              string `json:"policy_summary,omitempty"`
	PolicyAction               string `json:"policy_action,omitempty"`
	ScopeSummary               string `json:"scope_summary,omitempty"`
	ResourceScopeSummary       string `json:"resource_scope_summary,omitempty"`
	DefaultGrantScope          string `json:"default_grant_scope,omitempty"`
	MaxGrantScope              string `json:"max_grant_scope,omitempty"`
	RequiresExternalSideEffect bool   `json:"requires_external_side_effect,omitempty"`
}

// PermissionResponse is the client's answer to a PermissionRequest.
type PermissionResponse struct {
	RequestID string `json:"request_id"`
	Approved  bool   `json:"approved"`
	Scope     string `json:"scope,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// Command describes an available slash command for the client UI.
type Command struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Shortcut    string `json:"shortcut,omitempty"`
}

func (p InitializeParams) Validate(capabilities map[string]any) *protocolMessageError {
	if strings.TrimSpace(p.ProtocolVersion) == "" {
		return newInvalidParamsError("protocol_version is required", map[string]any{"field": "protocol_version"}, nil)
	}
	if p.ProtocolVersion != protocolVersion {
		return newProtocolVersionUnsupportedError(p.ProtocolVersion)
	}
	if strings.TrimSpace(p.ClientInfo.Name) == "" {
		return newInvalidParamsError("client_info.name is required", map[string]any{"field": "client_info.name"}, nil)
	}
	if strings.TrimSpace(p.ClientInfo.Version) == "" {
		return newInvalidParamsError("client_info.version is required", map[string]any{"field": "client_info.version"}, nil)
	}
	if badPath, reason := invalidCapabilityShape("", p.Capabilities); badPath != "" {
		return newInvalidParamsError(
			"capabilities must be nested booleans or objects",
			map[string]any{"field": badPath, "reason": reason},
			nil,
		)
	}
	for _, path := range unsupportedRequestedCapabilities(p.Capabilities, capabilities) {
		return newCapabilityUnsupportedError(path)
	}
	return nil
}

func (p LoadSessionParams) Validate() *protocolMessageError {
	if strings.TrimSpace(p.SessionKey) == "" {
		return newInvalidParamsError("session_key is required", map[string]any{"field": "session_key"}, nil)
	}
	return nil
}

func (p PromptParams) Validate() *protocolMessageError {
	if strings.TrimSpace(p.SessionID) == "" {
		return newInvalidParamsError("session_id is required", map[string]any{"field": "session_id"}, nil)
	}
	if strings.TrimSpace(p.Message) == "" && len(p.Images) == 0 && len(p.ContentBlocks) == 0 && p.StructuredCommand == nil && p.StructuredApproval == nil {
		return newInvalidParamsError("message, content_blocks, images, or structured action is required", nil, nil)
	}
	if p.StructuredCommand != nil && strings.TrimSpace(p.StructuredCommand.Kind) == "" {
		return newInvalidParamsError("structured_command.kind is required", map[string]any{"field": "structured_command.kind"}, nil)
	}
	if p.StructuredApproval != nil && strings.TrimSpace(p.StructuredApproval.Action) == "" {
		return newInvalidParamsError("structured_approval.action is required", map[string]any{"field": "structured_approval.action"}, nil)
	}
	return nil
}

func (p CancelParams) Validate() *protocolMessageError {
	if strings.TrimSpace(p.SessionID) == "" {
		return newInvalidParamsError("session_id is required", map[string]any{"field": "session_id"}, nil)
	}
	return nil
}

func (p ListSessionsParams) Validate() *protocolMessageError {
	if p.Limit < 0 {
		return newInvalidParamsError("limit must be >= 0", map[string]any{"field": "limit"}, nil)
	}
	if p.Offset < 0 {
		return newInvalidParamsError("offset must be >= 0", map[string]any{"field": "offset"}, nil)
	}
	return nil
}

func (p SetSessionModeParams) Validate() *protocolMessageError {
	if strings.TrimSpace(p.SessionID) == "" {
		return newInvalidParamsError("session_id is required", map[string]any{"field": "session_id"}, nil)
	}
	if strings.TrimSpace(p.Mode) == "" {
		return newInvalidParamsError("mode is required", map[string]any{"field": "mode"}, nil)
	}
	return nil
}

func (p SetConfigOptionParams) Validate() *protocolMessageError {
	if strings.TrimSpace(p.SessionID) == "" {
		return newInvalidParamsError("session_id is required", map[string]any{"field": "session_id"}, nil)
	}
	if strings.TrimSpace(string(p.Key)) == "" {
		return newInvalidParamsError("key is required", map[string]any{"field": "key"}, nil)
	}
	return nil
}

func (p PermissionResponse) Validate() *protocolMessageError {
	if strings.TrimSpace(p.RequestID) == "" {
		return newInvalidParamsError("request_id is required", map[string]any{"field": "request_id"}, nil)
	}
	scope, err := approval.NormalizeScope(approval.Scope(strings.TrimSpace(p.Scope)))
	if err != nil {
		return newInvalidParamsError("scope is invalid", map[string]any{"field": "scope"}, err)
	}
	if p.Approved {
		switch scope {
		case "", approval.ScopeOnce, approval.ScopeSession, approval.ScopeAlways:
			return nil
		default:
			return newInvalidParamsError("approved responses must use once, session, or always scope", map[string]any{"field": "scope"}, nil)
		}
	}
	switch scope {
	case "", approval.ScopeDeny:
		return nil
	default:
		return newInvalidParamsError("denied responses must omit scope or use deny", map[string]any{"field": "scope"}, nil)
	}
}
