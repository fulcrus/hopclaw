package cron

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/automation"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/resultmodel"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

const (
	executionTimeout = 10 * time.Minute
	pollInterval     = 3 * time.Second
)

// ---------------------------------------------------------------------------
// Executor
// ---------------------------------------------------------------------------

// executor encapsulates the logic for running a single cron job: submitting
// the run, polling for completion, and optionally delivering the result.
type executor struct {
	runner   *automation.Runner
	verifier RuntimeVerifier
	channels ChannelDeliverer
}

// run executes a single job and returns the outcome.
func (e *executor) run(ctx context.Context, job *Job) CronRunResult {
	sessionKey := job.SessionKey
	if sessionKey == "" {
		sessionKey = "cron:" + job.ID
	}

	if e.runner == nil {
		return CronRunResult{
			Source:    resultmodel.AutomationSourceCron,
			Status:    resultmodel.AutomationStatusError,
			RunStatus: "failed",
			Error:     &resultmodel.ResultError{Message: "automation runner is not configured"},
		}
	}
	automationID := strings.TrimSpace(job.AutomationID)
	if automationID == "" {
		automationID = job.ID
	}
	result, err := e.runner.Run(ctx, automation.SubmitRequest{
		SessionKey:   sessionKey,
		Content:      job.Payload.Content,
		Model:        job.Model,
		AutomationID: automationID,
		Metadata: map[string]any{
			"automation_kind": "cron",
			"automation_id":   job.ID,
			"automation_name": strings.TrimSpace(job.Name),
		},
	})
	if err != nil {
		return CronRunResult{
			Source:    resultmodel.AutomationSourceCron,
			Status:    resultmodel.AutomationStatusError,
			RunStatus: "failed",
			Error:     &resultmodel.ResultError{Message: fmt.Sprintf("submit failed: %v", err)},
		}
	}

	log.Info("cron job submitted",
		"job_id", job.ID,
		"job_name", job.Name,
		"run_id", result.RunID,
	)
	out := cronRunResultFromRuntime(result)
	e.attachVerification(ctx, &out)
	return out
}

func cronRunResultFromRuntime(result *runtimesvc.RunResult) CronRunResult {
	if result == nil {
		return CronRunResult{}
	}
	if result.Canonical.Populated() {
		return result.Canonical.Normalized()
	}
	out := resultmodel.AutomationResult{
		Source:    resultmodel.AutomationSourceRuntime,
		RunID:     strings.TrimSpace(result.RunID),
		RunStatus: strings.TrimSpace(string(result.Status)),
		Summary:   strings.TrimSpace(result.Summary),
		Output:    strings.TrimSpace(result.Output),
		Actions:   append([]resultmodel.ResultAction(nil), result.NextActions...),
	}
	if errText := strings.TrimSpace(result.Error); errText != "" {
		out.Error = &resultmodel.ResultError{Message: errText}
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
			})
		}
	}
	return out.Normalized()
}

func (e *executor) attachVerification(ctx context.Context, result *CronRunResult) {
	if e == nil || e.verifier == nil || result == nil || strings.TrimSpace(result.RunID) == "" {
		return
	}
	verification, err := e.verifier.GetRunVerification(ctx, result.RunID)
	if err != nil {
		result.Verification = &resultmodel.ResultVerification{
			Status:  resultmodel.VerificationStatus(verifyrt.StatusWarning),
			Summary: fmt.Sprintf("verification unavailable: %v", err),
		}
		return
	}
	if verification == nil {
		return
	}
	result.Verification = &resultmodel.ResultVerification{
		Status:  resultmodel.VerificationStatus(strings.TrimSpace(string(verification.Status))),
		Summary: strings.TrimSpace(verification.Summary),
	}
}

// deliver sends the run result to the configured channel.
func (e *executor) deliver(ctx context.Context, job *Job, content string) error {
	if e.channels == nil {
		return fmt.Errorf("channel deliverer is not configured")
	}
	if job.Delivery == nil {
		return nil
	}
	delivery := job.Delivery
	channel := delivery.Channel
	target := delivery.Target
	if channel == "" || target == "" {
		return nil
	}
	return e.channels.DeliverMessage(ctx, *delivery, content)
}

// ---------------------------------------------------------------------------
// Backoff
// ---------------------------------------------------------------------------

// computeBackoff returns the backoff duration for the given number of
// consecutive errors using exponential backoff: 30s, 1m, 2m, 4m, ...
// capped at backoffMax.
func computeBackoff(consecutiveErrors int) time.Duration {
	if consecutiveErrors <= 0 {
		return 0
	}
	d := backoffBase
	for i := 1; i < consecutiveErrors && d < backoffMax; i++ {
		d *= 2
	}
	if d > backoffMax {
		d = backoffMax
	}
	return d
}

func ComputeBackoff(consecutiveErrors int) time.Duration {
	return computeBackoff(consecutiveErrors)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func outcomeFromResult(result CronRunResult) (string, string) {
	normalized := result.Normalized()
	if verificationFailed(normalized.Verification) {
		errMsg := verificationSummary(normalized.Verification)
		if errMsg == "" {
			errMsg = "verification failed"
		}
		return RunStatusError, errMsg
	}
	switch normalized.Status {
	case resultmodel.AutomationStatusOK, resultmodel.AutomationStatusTriggered:
		return RunStatusOK, ""
	case resultmodel.AutomationStatusError:
		errMsg := normalized.ErrorMessage()
		if errMsg == "" {
			errMsg = normalize.FirstNonEmpty(verificationSummary(normalized.Verification), "run failed")
		}
		return RunStatusError, errMsg
	case resultmodel.AutomationStatusSkipped:
		return RunStatusSkipped, "run was cancelled"
	case resultmodel.AutomationStatusPending:
		if normalized.RunStatus != "" {
			return RunStatusError, fmt.Sprintf("unexpected terminal status: %s", normalized.RunStatus)
		}
		return RunStatusError, "unexpected non-terminal automation result"
	default:
		if normalized.RunStatus != "" {
			return RunStatusError, fmt.Sprintf("unexpected terminal status: %s", normalized.RunStatus)
		}
		return RunStatusError, fmt.Sprintf("unexpected terminal status: %s", normalized.Status)
	}
}

func deliveryContent(job *Job, result CronRunResult) string {
	normalized := result.Normalized()
	output := strings.TrimSpace(normalized.Output)
	summary := strings.TrimSpace(normalized.Summary)
	artifacts := compactArtifactURIs(normalized.ArtifactURIs())
	verificationWarning := verificationWarningSummary(normalized.Verification)

	if output != "" {
		base := output
		if len(artifacts) != 0 && !containsAllArtifacts(output, artifacts) {
			var b strings.Builder
			b.WriteString(output)
			b.WriteString("\n\nArtifacts:\n")
			for _, uri := range artifacts {
				b.WriteString("- ")
				b.WriteString(uri)
				b.WriteByte('\n')
			}
			base = strings.TrimSpace(b.String())
		}
		if verificationWarning == "" {
			return base
		}
		return verificationWarning + "\n\n" + base
	}

	lines := make([]string, 0, 2+len(artifacts))
	if verificationWarning != "" {
		lines = append(lines, verificationWarning)
	}
	if summary != "" {
		lines = append(lines, summary)
	} else if job != nil {
		lines = append(lines, fmt.Sprintf("[cron:%s] job %q completed successfully", job.ID, job.Name))
	}
	if len(artifacts) > 0 {
		lines = append(lines, "Artifacts:")
		for _, uri := range artifacts {
			lines = append(lines, "- "+uri)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func verificationFailureContent(job *Job, result CronRunResult) string {
	normalized := result.Normalized()
	summary := normalize.FirstNonEmpty(verificationSummary(normalized.Verification), normalized.ErrorMessage(), "verification failed")
	if job == nil {
		return "Scheduled task failed verification: " + summary
	}
	return fmt.Sprintf("[cron:%s] job %q failed verification: %s", job.ID, job.Name, summary)
}

func compactArtifactURIs(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func containsAllArtifacts(content string, uris []string) bool {
	for _, uri := range uris {
		if !strings.Contains(content, uri) {
			return false
		}
	}
	return true
}

func verificationFailed(result *resultmodel.ResultVerification) bool {
	return result != nil && strings.TrimSpace(string(result.Status)) == string(verifyrt.StatusFailed)
}

func verificationWarningSummary(result *resultmodel.ResultVerification) string {
	if result == nil || strings.TrimSpace(string(result.Status)) != string(verifyrt.StatusWarning) {
		return ""
	}
	return "Verification warning: " + normalize.FirstNonEmpty(strings.TrimSpace(result.Summary), "quality checks finished with warnings")
}

func verificationSummary(result *resultmodel.ResultVerification) string {
	if result == nil {
		return ""
	}
	return strings.TrimSpace(result.Summary)
}

func verificationStatus(result *resultmodel.ResultVerification) string {
	if result == nil {
		return ""
	}
	return strings.TrimSpace(string(result.Status))
}
