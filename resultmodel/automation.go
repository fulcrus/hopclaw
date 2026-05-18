package resultmodel

import (
	"strings"

	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
)

type AutomationSource string

const (
	AutomationSourceRuntime AutomationSource = "runtime"
	AutomationSourceCron    AutomationSource = "cron"
	AutomationSourceWatch   AutomationSource = "watch"
)

type AutomationStatus string

const (
	AutomationStatusPending   AutomationStatus = "pending"
	AutomationStatusOK        AutomationStatus = "ok"
	AutomationStatusError     AutomationStatus = "error"
	AutomationStatusSkipped   AutomationStatus = "skipped"
	AutomationStatusPrimed    AutomationStatus = "primed"
	AutomationStatusUnchanged AutomationStatus = "unchanged"
	AutomationStatusTriggered AutomationStatus = "triggered"
)

// AutomationResult is the canonical execution/automation outcome model shared
// by runtime, cron, and watch surfaces.
type AutomationResult struct {
	Source       AutomationSource    `json:"source,omitempty"`
	Status       AutomationStatus    `json:"status,omitempty"`
	RunID        string              `json:"run_id,omitempty"`
	RunStatus    string              `json:"run_status,omitempty"`
	Summary      string              `json:"summary,omitempty"`
	Output       string              `json:"output,omitempty"`
	Fingerprint  string              `json:"fingerprint,omitempty"`
	Error        *ResultError        `json:"error,omitempty"`
	Verification *ResultVerification `json:"verification,omitempty"`
	Artifacts    []ResultArtifact    `json:"artifacts,omitempty"`
	Actions      []ResultAction      `json:"actions,omitempty"`
	Metadata     map[string]any      `json:"metadata,omitempty"`
}

func (r AutomationResult) Normalized() AutomationResult {
	out := r
	out.Source = AutomationSource(strings.TrimSpace(string(out.Source)))
	out.Status = normalizeAutomationStatus(out.Status)
	out.RunID = strings.TrimSpace(out.RunID)
	out.RunStatus = strings.TrimSpace(out.RunStatus)
	out.Summary = strings.TrimSpace(out.Summary)
	out.Output = strings.TrimSpace(out.Output)
	out.Fingerprint = strings.TrimSpace(out.Fingerprint)
	out.Error = cloneError(out.Error)
	out.Verification = cloneVerification(out.Verification)
	out.Artifacts = cloneArtifacts(out.Artifacts)
	out.Actions = cloneActions(out.Actions)
	out.Metadata = supportmaps.Clone(out.Metadata)
	if verificationFailed(out.Verification) {
		if out.Error == nil {
			summary := strings.TrimSpace(out.Verification.Summary)
			if summary == "" {
				summary = "verification failed"
			}
			out.Error = &ResultError{Message: summary}
		}
		if out.Status != AutomationStatusSkipped {
			out.Status = AutomationStatusError
		}
	}

	if out.Status == "" {
		out.Status = inferAutomationStatus(out)
	}
	if out.Summary == "" {
		switch {
		case out.Output != "":
			out.Summary = compact(out.Output, summaryMaxChars)
		case out.Error != nil && strings.TrimSpace(out.Error.Message) != "":
			out.Summary = compact(out.Error.Message, summaryMaxChars)
		case out.RunStatus != "":
			out.Summary = strings.TrimSpace(out.RunStatus)
		}
	}
	return out
}

func CloneAutomationResult(in *AutomationResult) *AutomationResult {
	if in == nil {
		return nil
	}
	cloned := in.Normalized()
	return &cloned
}

func (r AutomationResult) Populated() bool {
	if strings.TrimSpace(string(r.Source)) != "" {
		return true
	}
	if strings.TrimSpace(string(r.Status)) != "" {
		return true
	}
	if strings.TrimSpace(r.RunID) != "" || strings.TrimSpace(r.RunStatus) != "" {
		return true
	}
	if strings.TrimSpace(r.Summary) != "" || strings.TrimSpace(r.Output) != "" || strings.TrimSpace(r.Fingerprint) != "" {
		return true
	}
	if r.Error != nil || r.Verification != nil {
		return true
	}
	if len(r.Artifacts) != 0 || len(r.Actions) != 0 || len(r.Metadata) != 0 {
		return true
	}
	return false
}

func (r AutomationResult) ErrorMessage() string {
	if normalized := r.Normalized(); normalized.Error != nil {
		return strings.TrimSpace(normalized.Error.Message)
	}
	return ""
}

func (r AutomationResult) ArtifactURIs() []string {
	normalized := r.Normalized()
	if len(normalized.Artifacts) == 0 {
		return nil
	}
	out := make([]string, 0, len(normalized.Artifacts))
	seen := make(map[string]struct{}, len(normalized.Artifacts))
	for _, item := range normalized.Artifacts {
		uri := strings.TrimSpace(item.URI)
		if uri == "" {
			continue
		}
		if _, ok := seen[uri]; ok {
			continue
		}
		seen[uri] = struct{}{}
		out = append(out, uri)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeAutomationStatus(status AutomationStatus) AutomationStatus {
	switch AutomationStatus(strings.TrimSpace(string(status))) {
	case AutomationStatusPending,
		AutomationStatusOK,
		AutomationStatusError,
		AutomationStatusSkipped,
		AutomationStatusPrimed,
		AutomationStatusUnchanged,
		AutomationStatusTriggered:
		return AutomationStatus(strings.TrimSpace(string(status)))
	default:
		return ""
	}
}

func inferAutomationStatus(result AutomationResult) AutomationStatus {
	if result.Error != nil && strings.TrimSpace(result.Error.Message) != "" {
		return AutomationStatusError
	}
	switch strings.ToLower(strings.TrimSpace(result.RunStatus)) {
	case "completed":
		return AutomationStatusOK
	case "failed":
		return AutomationStatusError
	case "cancelled":
		return AutomationStatusSkipped
	case "queued", "running", "streaming", "waiting_input", "waiting_approval":
		return AutomationStatusPending
	}
	if result.Output != "" || result.Summary != "" || len(result.Artifacts) > 0 {
		return AutomationStatusOK
	}
	return AutomationStatusPending
}

func verificationFailed(result *ResultVerification) bool {
	return result != nil && strings.EqualFold(strings.TrimSpace(string(result.Status)), "failed")
}
