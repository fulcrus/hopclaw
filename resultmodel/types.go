package resultmodel

import (
	"encoding/json"
	"fmt"
	"strings"

	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
)

const (
	MetadataKeyToolResult = "tool_result"
	summaryMaxChars       = 240
)

type ToolResultStatus string

const (
	ToolResultOK      ToolResultStatus = "ok"
	ToolResultPartial ToolResultStatus = "partial"
	ToolResultError   ToolResultStatus = "error"
)

type ResultBlockKind string

const (
	ResultBlockText    ResultBlockKind = "text"
	ResultBlockJSON    ResultBlockKind = "json"
	ResultBlockTable   ResultBlockKind = "table"
	ResultBlockWarning ResultBlockKind = "warning"
	ResultBlockDiff    ResultBlockKind = "diff"
	ResultBlockPreview ResultBlockKind = "preview"
)

type ResultActionKind string

const (
	ResultActionOpenArtifact ResultActionKind = "open_artifact"
	ResultActionFollowUp     ResultActionKind = "followup"
	ResultActionApproveRetry ResultActionKind = "approve_retry"
	ResultActionOpenURL      ResultActionKind = "open_url"
	ResultActionContinueWith ResultActionKind = "continue_with"
)

type VerificationStatus string

type ResultBlock struct {
	Kind    ResultBlockKind `json:"kind"`
	Title   string          `json:"title,omitempty"`
	Content string          `json:"content,omitempty"`
	Data    any             `json:"data,omitempty"`
}

type ResultArtifact struct {
	Kind        string         `json:"kind,omitempty"`
	Name        string         `json:"name,omitempty"`
	URI         string         `json:"uri,omitempty"`
	ContentType string         `json:"content_type,omitempty"`
	SizeBytes   int64          `json:"size_bytes,omitempty"`
	PreviewText string         `json:"preview_text,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type ResultAction struct {
	Kind   ResultActionKind `json:"kind"`
	Label  string           `json:"label,omitempty"`
	Target string           `json:"target,omitempty"`
	Params map[string]any   `json:"params,omitempty"`
}

type ResultVerification struct {
	Status  VerificationStatus `json:"status,omitempty"`
	Summary string             `json:"summary,omitempty"`
}

type ResultError struct {
	Code      string         `json:"code,omitempty"`
	Message   string         `json:"message,omitempty"`
	Retryable bool           `json:"retryable,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type ToolResult struct {
	ToolName       string              `json:"tool_name,omitempty"`
	ToolCallID     string              `json:"tool_call_id,omitempty"`
	Status         ToolResultStatus    `json:"status,omitempty"`
	TranscriptText string              `json:"transcript_text,omitempty"`
	Summary        string              `json:"summary,omitempty"`
	Structured     map[string]any      `json:"structured,omitempty"`
	Blocks         []ResultBlock       `json:"blocks,omitempty"`
	Artifacts      []ResultArtifact    `json:"artifacts,omitempty"`
	Actions        []ResultAction      `json:"actions,omitempty"`
	Verification   *ResultVerification `json:"verification,omitempty"`
	Error          *ResultError        `json:"error,omitempty"`
	Metadata       map[string]any      `json:"metadata,omitempty"`
	Content        string              `json:"content,omitempty"`
	ArtifactURI    string              `json:"artifact_uri,omitempty"`
}

func (r ToolResult) Normalized() ToolResult {
	out := r
	out.ToolName = strings.TrimSpace(out.ToolName)
	out.ToolCallID = strings.TrimSpace(out.ToolCallID)
	out.TranscriptText = strings.TrimSpace(out.TranscriptText)
	out.Summary = strings.TrimSpace(out.Summary)
	out.Content = strings.TrimSpace(out.Content)
	out.ArtifactURI = strings.TrimSpace(out.ArtifactURI)
	out.Blocks = cloneBlocks(out.Blocks)
	out.Artifacts = cloneArtifacts(out.Artifacts)
	out.Actions = cloneActions(out.Actions)
	out.Structured = supportmaps.Clone(out.Structured)
	out.Metadata = supportmaps.Clone(out.Metadata)
	out.Error = cloneError(out.Error)
	out.Verification = cloneVerification(out.Verification)

	if out.ArtifactURI != "" && out.PrimaryArtifactURI() == "" {
		out.Artifacts = append([]ResultArtifact{{
			Kind: "artifact",
			URI:  out.ArtifactURI,
		}}, out.Artifacts...)
	}
	if out.ArtifactURI == "" {
		out.ArtifactURI = out.PrimaryArtifactURI()
	}

	if out.TranscriptText == "" {
		switch {
		case out.Content != "":
			out.TranscriptText = out.Content
		case out.Error != nil && strings.TrimSpace(out.Error.Message) != "":
			out.TranscriptText = "error: " + strings.TrimSpace(out.Error.Message)
		case out.Summary != "":
			out.TranscriptText = out.Summary
		}
	}
	if out.Summary == "" {
		out.Summary = deriveSummary(out)
	}
	if out.Content == "" {
		out.Content = out.TranscriptText
	}
	if out.Status == "" {
		switch {
		case out.Error != nil && strings.TrimSpace(out.Error.Message) != "":
			out.Status = ToolResultError
		default:
			out.Status = ToolResultOK
		}
	}
	return out
}

func (r ToolResult) PrimaryArtifactURI() string {
	for _, item := range r.Artifacts {
		if uri := strings.TrimSpace(item.URI); uri != "" {
			return uri
		}
	}
	return strings.TrimSpace(r.ArtifactURI)
}

func (r ToolResult) Transcript() string {
	return strings.TrimSpace(r.Normalized().TranscriptText)
}

func (r ToolResult) MarshalMetadata() map[string]any {
	normalized := r.Normalized()
	payload, err := json.Marshal(normalized)
	if err != nil {
		return map[string]any{
			"tool_name":       normalized.ToolName,
			"tool_call_id":    normalized.ToolCallID,
			"status":          normalized.Status,
			"transcript_text": normalized.TranscriptText,
			"summary":         normalized.Summary,
			"artifact_uri":    normalized.ArtifactURI,
		}
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		return map[string]any{
			"tool_name":       normalized.ToolName,
			"tool_call_id":    normalized.ToolCallID,
			"status":          normalized.Status,
			"transcript_text": normalized.TranscriptText,
			"summary":         normalized.Summary,
			"artifact_uri":    normalized.ArtifactURI,
		}
	}
	return out
}

func DecodeToolResultMetadata(metadata map[string]any) (ToolResult, bool) {
	if len(metadata) == 0 {
		return ToolResult{}, false
	}
	raw, ok := metadata[MetadataKeyToolResult]
	if !ok || raw == nil {
		return ToolResult{}, false
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return ToolResult{}, false
	}
	var result ToolResult
	if err := json.Unmarshal(body, &result); err != nil {
		return ToolResult{}, false
	}
	normalized := result.Normalized()
	if normalized.ToolName == "" && normalized.ToolCallID == "" && normalized.TranscriptText == "" && normalized.PrimaryArtifactURI() == "" {
		return ToolResult{}, false
	}
	return normalized, true
}

func UnavailableToolResult(toolName, toolCallID string, reasons []string, hints []string) ToolResult {
	message := "tool is unavailable"
	if len(reasons) > 0 {
		message = fmt.Sprintf("tool is unavailable: %s", strings.Join(reasons, "; "))
	}
	result := ToolResult{
		ToolName:       strings.TrimSpace(toolName),
		ToolCallID:     strings.TrimSpace(toolCallID),
		Status:         ToolResultError,
		TranscriptText: message,
		Summary:        "tool unavailable",
		Error: &ResultError{
			Code:    "tool_unavailable",
			Message: message,
		},
		Structured: map[string]any{
			"available": false,
			"reasons":   append([]string(nil), reasons...),
			"hints":     append([]string(nil), hints...),
		},
	}
	if len(hints) > 0 {
		result.Actions = append(result.Actions, ResultAction{
			Kind:   ResultActionFollowUp,
			Label:  "review install hints",
			Target: strings.Join(hints, "\n"),
		})
	}
	return result.Normalized()
}

func deriveSummary(result ToolResult) string {
	switch {
	case result.TranscriptText != "":
		return compact(result.TranscriptText, summaryMaxChars)
	case len(result.Artifacts) > 0:
		return fmt.Sprintf("%d artifact(s) ready", len(result.Artifacts))
	case result.Error != nil && strings.TrimSpace(result.Error.Message) != "":
		return compact(result.Error.Message, summaryMaxChars)
	default:
		return strings.TrimSpace(string(result.Status))
	}
}

func compact(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	return strings.TrimSpace(text[:maxChars]) + "..."
}

func cloneBlocks(in []ResultBlock) []ResultBlock {
	if len(in) == 0 {
		return nil
	}
	out := make([]ResultBlock, 0, len(in))
	out = append(out, in...)
	return out
}

func cloneArtifacts(in []ResultArtifact) []ResultArtifact {
	if len(in) == 0 {
		return nil
	}
	out := make([]ResultArtifact, 0, len(in))
	for _, item := range in {
		cloned := item
		cloned.Metadata = supportmaps.Clone(item.Metadata)
		out = append(out, cloned)
	}
	return out
}

func cloneActions(in []ResultAction) []ResultAction {
	if len(in) == 0 {
		return nil
	}
	out := make([]ResultAction, 0, len(in))
	for _, item := range in {
		cloned := item
		cloned.Params = supportmaps.Clone(item.Params)
		out = append(out, cloned)
	}
	return out
}

func cloneVerification(in *ResultVerification) *ResultVerification {
	if in == nil {
		return nil
	}
	cloned := *in
	return &cloned
}

func cloneError(in *ResultError) *ResultError {
	if in == nil {
		return nil
	}
	cloned := *in
	cloned.Metadata = supportmaps.Clone(in.Metadata)
	return &cloned
}
