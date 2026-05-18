package runtime

import (
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"github.com/fulcrus/hopclaw/resultmodel"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

func buildAutomationResult(run *agent.Run, result *RunResult, verification *verifyrt.RunVerification) resultmodel.AutomationResult {
	if result == nil {
		return resultmodel.AutomationResult{}
	}
	out := resultmodel.AutomationResult{
		Source:    resultmodel.AutomationSourceRuntime,
		RunID:     strings.TrimSpace(result.RunID),
		RunStatus: strings.TrimSpace(string(result.Status)),
		Summary:   strings.TrimSpace(result.Summary),
		Output:    strings.TrimSpace(result.Output),
		Actions:   cloneBundleResultActions(result.NextActions),
		Metadata:  buildAutomationResultMetadata(result),
	}
	if run != nil && strings.TrimSpace(run.ID) != "" {
		out.RunID = strings.TrimSpace(run.ID)
		out.RunStatus = strings.TrimSpace(string(run.Status))
	}
	if errText := strings.TrimSpace(result.Error); errText != "" {
		out.Error = &resultmodel.ResultError{Message: errText}
	}
	if verification != nil {
		out.Verification = &resultmodel.ResultVerification{
			Status:  resultmodel.VerificationStatus(strings.TrimSpace(string(verification.Status))),
			Summary: strings.TrimSpace(verification.Summary),
		}
		if verification.Status == verifyrt.StatusFailed && out.Error == nil {
			out.Error = &resultmodel.ResultError{Message: strings.TrimSpace(verification.Summary)}
		}
	}
	if len(result.Deliverables) > 0 {
		out.Artifacts = make([]resultmodel.ResultArtifact, 0, len(result.Deliverables))
		for _, item := range result.Deliverables {
			out.Artifacts = append(out.Artifacts, resultmodel.ResultArtifact{
				Kind:        strings.TrimSpace(item.Kind),
				Name:        strings.TrimSpace(item.Name),
				URI:         strings.TrimSpace(item.URI),
				ContentType: strings.TrimSpace(item.ContentType),
				SizeBytes:   item.SizeBytes,
				PreviewText: strings.TrimSpace(item.PreviewText),
				Metadata:    supportmaps.Clone(item.Metadata),
			})
		}
	}
	out.Status = automationStatusForRun(run, result, verification)
	return out.Normalized()
}

func automationStatusForRun(run *agent.Run, result *RunResult, verification *verifyrt.RunVerification) resultmodel.AutomationStatus {
	status := agent.RunStatus("")
	if run != nil {
		status = run.Status
	}
	if status == "" && result != nil {
		status = result.Status
	}
	switch status {
	case agent.RunCompleted:
		if verification != nil && verification.Status == verifyrt.StatusFailed {
			return resultmodel.AutomationStatusError
		}
		return resultmodel.AutomationStatusOK
	case agent.RunFailed:
		return resultmodel.AutomationStatusError
	case agent.RunCancelled:
		return resultmodel.AutomationStatusSkipped
	default:
		return resultmodel.AutomationStatusPending
	}
}

func buildAutomationResultMetadata(result *RunResult) map[string]any {
	if result == nil {
		return nil
	}
	var out map[string]any
	if result.Outcome != "" {
		out = map[string]any{"outcome": string(result.Outcome)}
	}
	if status := strings.TrimSpace(result.VerificationStatus); status != "" {
		if out == nil {
			out = make(map[string]any, 2)
		}
		out["verification_status"] = status
	}
	if summary := strings.TrimSpace(result.VerificationSummary); summary != "" {
		if out == nil {
			out = make(map[string]any, 2)
		}
		out["verification_summary"] = summary
	}
	return out
}

func cloneBundleResultActions(items []resultmodel.ResultAction) []resultmodel.ResultAction {
	if len(items) == 0 {
		return nil
	}
	out := make([]resultmodel.ResultAction, len(items))
	copy(out, items)
	return out
}
