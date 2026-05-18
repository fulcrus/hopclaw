package bundle

import "strings"

const ProtocolVersionV1 = "hopclaw.tool/v1"

type RuntimeType string

const (
	RuntimePrompt     RuntimeType = "prompt"
	RuntimeExecutable RuntimeType = "executable"
	RuntimeSidecar    RuntimeType = "sidecar"
	RuntimeOpenAPI    RuntimeType = "openapi"
	RuntimeMCP        RuntimeType = "mcp"
)

type ResultStatus string

const (
	ResultSuccess        ResultStatus = "success"
	ResultAccepted       ResultStatus = "accepted"
	ResultPartial        ResultStatus = "partial"
	ResultBlocked        ResultStatus = "blocked"
	ResultRetryableError ResultStatus = "retryable_error"
	ResultError          ResultStatus = "error"
)

type ErrorCategory string

const (
	ErrorAuth       ErrorCategory = "auth"
	ErrorPermission ErrorCategory = "permission"
	ErrorValidation ErrorCategory = "validation"
	ErrorNotFound   ErrorCategory = "not_found"
	ErrorConflict   ErrorCategory = "conflict"
	ErrorRateLimit  ErrorCategory = "rate_limit"
	ErrorNetwork    ErrorCategory = "network"
	ErrorTimeout    ErrorCategory = "timeout"
	ErrorUpstream   ErrorCategory = "upstream"
	ErrorInternal   ErrorCategory = "internal"
)

type VerificationStatus string

const (
	VerificationNone    VerificationStatus = "none"
	VerificationPassed  VerificationStatus = "passed"
	VerificationFailed  VerificationStatus = "failed"
	VerificationUnknown VerificationStatus = "unknown"
)

type ExecuteRequest struct {
	ProtocolVersion string         `json:"protocol_version,omitempty"`
	ToolName        string         `json:"tool_name"`
	ToolCallID      string         `json:"tool_call_id,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
	RunID           string         `json:"run_id,omitempty"`
	Input           map[string]any `json:"input,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type Artifact struct {
	URI       string `json:"uri"`
	Name      string `json:"name,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

type Evidence struct {
	Kind   string         `json:"kind"`
	Name   string         `json:"name,omitempty"`
	Detail string         `json:"detail,omitempty"`
	Data   map[string]any `json:"data,omitempty"`
}

type Verification struct {
	Attempted bool               `json:"attempted,omitempty"`
	Status    VerificationStatus `json:"status,omitempty"`
	Strategy  string             `json:"strategy,omitempty"`
	Observed  map[string]any     `json:"observed,omitempty"`
}

type ExecuteError struct {
	Code      string         `json:"code,omitempty"`
	Category  ErrorCategory  `json:"category,omitempty"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

type ExecuteResponse struct {
	ProtocolVersion string         `json:"protocol_version,omitempty"`
	OK              bool           `json:"ok,omitempty"`
	Status          ResultStatus   `json:"status,omitempty"`
	Summary         string         `json:"summary,omitempty"`
	Content         string         `json:"content,omitempty"`
	Data            any            `json:"data,omitempty"`
	Artifacts       []Artifact     `json:"artifacts,omitempty"`
	Evidence        []Evidence     `json:"evidence,omitempty"`
	Verification    *Verification  `json:"verification,omitempty"`
	Error           *ExecuteError  `json:"error,omitempty"`
	Metrics         map[string]any `json:"metrics,omitempty"`
}

func (r ExecuteResponse) Normalized() ExecuteResponse {
	out := r
	if strings.TrimSpace(out.ProtocolVersion) == "" {
		out.ProtocolVersion = ProtocolVersionV1
	}
	out.Status = normalizeResultStatus(out.Status)
	if out.Status == "" {
		switch {
		case out.Error != nil && out.Error.Retryable:
			out.Status = ResultRetryableError
		case out.Error != nil:
			out.Status = ResultError
		case out.Verification != nil && out.Verification.Attempted && out.Verification.Status == VerificationFailed:
			out.Status = ResultError
		case out.Summary != "" || out.Content != "" || out.Data != nil || len(out.Artifacts) > 0 || len(out.Evidence) > 0:
			out.Status = ResultSuccess
		default:
			out.Status = ResultError
		}
	}
	out.OK = out.Status == ResultSuccess || out.Status == ResultAccepted || out.Status == ResultPartial
	if out.Error == nil && !out.OK {
		out.Error = &ExecuteError{
			Category: ErrorInternal,
			Message:  "tool execution failed",
		}
	}
	if out.Verification != nil {
		out.Verification.Status = normalizeVerificationStatus(out.Verification.Status)
	}
	if out.Error != nil {
		out.Error.Category = normalizeErrorCategory(out.Error.Category)
	}
	return out
}

func (r ExecuteResponse) Successful() bool {
	normalized := r.Normalized()
	return normalized.OK
}

func normalizeResultStatus(v ResultStatus) ResultStatus {
	switch ResultStatus(strings.TrimSpace(string(v))) {
	case ResultSuccess:
		return ResultSuccess
	case ResultAccepted:
		return ResultAccepted
	case ResultPartial:
		return ResultPartial
	case ResultBlocked:
		return ResultBlocked
	case ResultRetryableError:
		return ResultRetryableError
	case ResultError:
		return ResultError
	default:
		return ""
	}
}

func normalizeVerificationStatus(v VerificationStatus) VerificationStatus {
	switch VerificationStatus(strings.TrimSpace(string(v))) {
	case "", VerificationNone:
		return VerificationNone
	case VerificationPassed:
		return VerificationPassed
	case VerificationFailed:
		return VerificationFailed
	case VerificationUnknown:
		return VerificationUnknown
	default:
		return VerificationUnknown
	}
}

func normalizeErrorCategory(v ErrorCategory) ErrorCategory {
	switch ErrorCategory(strings.TrimSpace(string(v))) {
	case "", ErrorInternal:
		return ErrorInternal
	case ErrorAuth:
		return ErrorAuth
	case ErrorPermission:
		return ErrorPermission
	case ErrorValidation:
		return ErrorValidation
	case ErrorNotFound:
		return ErrorNotFound
	case ErrorConflict:
		return ErrorConflict
	case ErrorRateLimit:
		return ErrorRateLimit
	case ErrorNetwork:
		return ErrorNetwork
	case ErrorTimeout:
		return ErrorTimeout
	case ErrorUpstream:
		return ErrorUpstream
	default:
		return ErrorInternal
	}
}
