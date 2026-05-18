package verify

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	domainrun "github.com/fulcrus/hopclaw/internal/domain/runstate"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

const (
	contractDeliverableSummary         = "summary"
	contractDeliverableDocument        = "document"
	contractDeliverableSpreadsheet     = "spreadsheet"
	contractDeliverablePresentation    = "presentation"
	contractDeliverableBrowserEvidence = "browser_evidence"
	contractDeliverableDesktopEvidence = "desktop_evidence"
	contractDeliverableMessageDelivery = "message_delivery"
	contractDeliverableWatchAlert      = "watch_alert"
	contractDeliverableDeployment      = "deployment"

	contractAcceptanceVisibleResult  = "visible_result"
	contractAcceptanceDeliverables   = "deliverables_ready"
	contractAcceptanceExternalEffect = "external_effect_verified"
	contractAcceptanceApproval       = "approval_before_side_effect"
)

func Evaluate(input Input, opts ...Option) RunVerification {
	return EvaluateWithOptions(input, opts...)
}

type Option func(*Policy)

func WithPolicy(policy Policy) Option {
	return func(target *Policy) {
		if target == nil {
			return
		}
		*target = policy.Normalized()
	}
}

func EvaluateWithOptions(input Input, opts ...Option) RunVerification {
	policy := DefaultPolicy()
	for _, opt := range opts {
		if opt != nil {
			opt(&policy)
		}
	}

	checks := make([]Check, 0, len(defaultVerifiers()))
	for _, verifier := range defaultVerifiers() {
		if !verifier.Applies(input) {
			continue
		}
		check := verifier.Verify(input)
		if strings.TrimSpace(check.Name) == "" {
			check.Name = verifier.Name()
		}
		check.Severity = policy.SeverityFor(check.Name, check.Status)
		checks = append(checks, check)
	}

	out := RunVerification{
		RunID:  strings.TrimSpace(input.RunID),
		Checks: checks,
	}
	sawPassed := false
	for _, check := range checks {
		requirement := check.Requirement
		if requirement == "" {
			requirement = RequirementRequired
		}
		switch check.Status {
		case StatusPassed:
			sawPassed = true
			continue
		case StatusSkipped:
			continue
		}
		switch normalizeIssueSeverity(check.Severity) {
		case SeverityInfo:
			out.Infos++
		case SeverityError:
			if requirement == RequirementAdvisory {
				out.AdvisoryFailures++
				out.Failures++
				continue
			}
			out.RequiredFailures++
			out.Failures++
		case SeverityBlocking:
			out.BlockingFailures++
			if requirement == RequirementAdvisory {
				out.AdvisoryFailures++
				out.Failures++
				continue
			}
			out.RequiredFailures++
			out.Failures++
		case SeverityWarning, "":
			if requirement == RequirementAdvisory {
				out.AdvisoryWarnings++
			} else {
				out.RequiredWarnings++
			}
			out.Warnings++
		}
	}
	out.Status = aggregateStatus(out, sawPassed)
	out.Summary = summarizeVerification(out)
	return out
}

func runCompleted(status string) bool {
	return domainrun.Status(strings.TrimSpace(status)) == domainrun.RunCompleted
}

func aggregateStatus(result RunVerification, sawPassed bool) Status {
	switch {
	case result.Failures > 0:
		return StatusFailed
	case result.Warnings > 0:
		return StatusWarning
	case sawPassed || result.Infos > 0:
		return StatusPassed
	default:
		return StatusSkipped
	}
}

func summarizeVerification(result RunVerification) string {
	switch result.Status {
	case StatusFailed:
		if result.BlockingFailures > 0 {
			if result.BlockingFailures == 1 {
				return "verification blocked delivery: 1 blocking check did not pass"
			}
			return fmt.Sprintf("verification blocked delivery: %d blocking checks did not pass", result.BlockingFailures)
		}
		if result.RequiredFailures == 1 {
			return "verification failed: 1 required check failed"
		}
		return fmt.Sprintf("verification failed: %d required checks failed", result.RequiredFailures)
	case StatusWarning:
		parts := make([]string, 0, 4)
		if result.RequiredWarnings > 0 {
			parts = append(parts, describeCount(result.RequiredWarnings, "required warning", "required warnings"))
		}
		if result.AdvisoryFailures > 0 {
			parts = append(parts, describeCount(result.AdvisoryFailures, "advisory failure", "advisory failures"))
		}
		if result.AdvisoryWarnings > 0 {
			parts = append(parts, describeCount(result.AdvisoryWarnings, "advisory warning", "advisory warnings"))
		}
		if len(parts) == 0 {
			if result.Warnings == 1 {
				return "verification finished with 1 warning"
			}
			return fmt.Sprintf("verification finished with %d warnings", result.Warnings)
		}
		return "verification finished with " + strings.Join(parts, " and ")
	case StatusPassed:
		if result.Infos == 1 {
			return "verification passed with 1 informational note"
		}
		if result.Infos > 1 {
			return fmt.Sprintf("verification passed with %d informational notes", result.Infos)
		}
		return "verification passed"
	default:
		return "verification skipped"
	}
}

func describeCount(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func deliverableMatches(ref Deliverable, exts ...string) bool {
	ct := strings.ToLower(strings.TrimSpace(ref.ContentType))
	uri := strings.ToLower(strings.TrimSpace(ref.URI))
	if ct != "" {
		for _, ext := range exts {
			ext = strings.ToLower(strings.TrimSpace(ext))
			switch ext {
			case ".csv":
				if strings.Contains(ct, "csv") {
					return true
				}
			case ".tsv":
				if strings.Contains(ct, "tab-separated-values") {
					return true
				}
			case ".xlsx", ".xls":
				if strings.Contains(ct, "sheet") || strings.Contains(ct, "excel") {
					return true
				}
			case ".ods":
				if strings.Contains(ct, "opendocument.spreadsheet") {
					return true
				}
			case ".docx", ".doc":
				if strings.Contains(ct, "wordprocessingml") || strings.Contains(ct, "msword") {
					return true
				}
			case ".odt":
				if strings.Contains(ct, "opendocument.text") {
					return true
				}
			case ".rtf":
				if strings.Contains(ct, "rtf") {
					return true
				}
			case ".pptx", ".ppt":
				if strings.Contains(ct, "presentationml") || strings.Contains(ct, "powerpoint") {
					return true
				}
			case ".png", ".jpg", ".jpeg", ".webp":
				if strings.HasPrefix(ct, "image/") {
					return true
				}
			case ".html":
				if strings.Contains(ct, "html") {
					return true
				}
			case ".json":
				if strings.Contains(ct, "json") {
					return true
				}
			}
		}
	}
	ext := strings.ToLower(filepath.Ext(uri))
	for _, want := range exts {
		if ext == strings.ToLower(strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func matchingTools(names []string, prefix string) []string {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || !strings.HasPrefix(name, prefix) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func parseJSONObject(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	return payload
}

func hasKey(payload map[string]any, key string) bool {
	if len(payload) == 0 {
		return false
	}
	value, ok := payload[key]
	return ok && value != nil
}

func hasBool(payload map[string]any, key string, want bool) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	got, ok := value.(bool)
	return ok && got == want
}

func summarizeDeliverableEvidence(ref Deliverable) string {
	parts := make([]string, 0, 2)
	if uri := strings.TrimSpace(ref.URI); uri != "" {
		parts = append(parts, uri)
	}
	if toolName := strings.TrimSpace(ref.ToolName); toolName != "" {
		parts = append(parts, "via "+toolName)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func summarizeStructuredEvidence(payload map[string]any, keys ...string) string {
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := normalize.String(payload[key])
		if value == "" {
			continue
		}
		parts = append(parts, key+"="+compactEvidence(value))
	}
	return strings.Join(parts, " ")
}

func dedupeEvidence(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
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

func compactEvidence(text string) string {
	const maxEvidenceLen = 120
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= maxEvidenceLen {
		return text
	}
	return strings.TrimSpace(text[:maxEvidenceLen]) + "..."
}

func hasAnyKey(payload map[string]any, keys ...string) bool {
	for _, key := range keys {
		if hasKey(payload, key) {
			return true
		}
	}
	return false
}
