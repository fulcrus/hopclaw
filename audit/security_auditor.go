package audit

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("audit")

// ---------------------------------------------------------------------------
// SecurityRisk
// ---------------------------------------------------------------------------

// SecurityRisk is a unified risk finding that combines results from all audit
// sub-systems (injection, path safety, content).
type SecurityRisk struct {
	Category string `json:"category"` // "injection", "path_safety", "content"
	Type     string `json:"type"`
	Detail   string `json:"detail"`
	Severity string `json:"severity"` // "high", "medium", "low"
}

// ---------------------------------------------------------------------------
// SecurityEvent
// ---------------------------------------------------------------------------

// SecurityEvent captures the tool call context and all risks found during an
// audit.
type SecurityEvent struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input,omitempty"`
	Risks     []SecurityRisk `json:"risks"`
}

// ---------------------------------------------------------------------------
// EventPublisher interface
// ---------------------------------------------------------------------------

// EventPublisher is the subset of eventbus.Bus needed by the auditor.
type EventPublisher interface {
	Publish(ctx context.Context, event eventbus.Event) error
}

// ---------------------------------------------------------------------------
// SecurityAuditor
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// CustomPattern
// ---------------------------------------------------------------------------

// CustomPattern is a compiled user-defined detection pattern.
type CustomPattern struct {
	name     string
	re       *regexp.Regexp
	severity string
	category string
}

// SecurityAuditor aggregates all security checks (injection detection, path
// safety, content validation) and publishes findings as events.
type SecurityAuditor struct {
	pathChecker    *PathChecker
	injDetector    *InjectionDetector
	contentVal     *ContentValidator
	bus            EventPublisher
	dangerousTools map[string]struct{}
	customPatterns []CustomPattern
}

// SecurityAuditorOption configures a SecurityAuditor.
type SecurityAuditorOption func(*SecurityAuditor)

// WithEventPublisher attaches an event bus for publishing security events.
func WithEventPublisher(bus EventPublisher) SecurityAuditorOption {
	return func(a *SecurityAuditor) {
		a.bus = bus
	}
}

// WithPathChecker overrides the default PathChecker.
func WithPathChecker(pc *PathChecker) SecurityAuditorOption {
	return func(a *SecurityAuditor) {
		a.pathChecker = pc
	}
}

// WithInjectionDetector overrides the default InjectionDetector.
func WithInjectionDetector(id *InjectionDetector) SecurityAuditorOption {
	return func(a *SecurityAuditor) {
		a.injDetector = id
	}
}

// WithContentValidator overrides the default ContentValidator.
func WithContentValidator(cv *ContentValidator) SecurityAuditorOption {
	return func(a *SecurityAuditor) {
		a.contentVal = cv
	}
}

// WithDangerousTools sets the list of tool names that always produce a
// high-severity risk when invoked.
func WithDangerousTools(tools []string) SecurityAuditorOption {
	return func(a *SecurityAuditor) {
		a.dangerousTools = make(map[string]struct{}, len(tools))
		for _, t := range tools {
			name := strings.TrimSpace(strings.ToLower(t))
			if name != "" {
				a.dangerousTools[name] = struct{}{}
			}
		}
	}
}

// WithCustomPatterns compiles and registers user-defined detection patterns.
// Invalid regex patterns are silently skipped.
func WithCustomPatterns(patterns []CustomPatternConfig) SecurityAuditorOption {
	return func(a *SecurityAuditor) {
		a.customPatterns = make([]CustomPattern, 0, len(patterns))
		invalidCount := 0
		for _, p := range patterns {
			compiled, err := regexp.Compile(p.Pattern)
			if err != nil {
				invalidCount++
				continue
			}
			severity := strings.TrimSpace(strings.ToLower(p.Severity))
			if severity == "" {
				severity = severityMedium
			}
			category := strings.TrimSpace(strings.ToLower(p.Category))
			if category == "" {
				category = "content"
			}
			a.customPatterns = append(a.customPatterns, CustomPattern{
				name:     p.Name,
				re:       compiled,
				severity: severity,
				category: category,
			})
		}
		if invalidCount > 0 {
			log.Warn("invalid custom audit patterns skipped", "count", invalidCount)
		}
	}
}

// CustomPatternConfig is the uncompiled configuration for a user-defined
// detection pattern, suitable for passing from config layer.
type CustomPatternConfig struct {
	Name     string
	Pattern  string
	Severity string
	Category string
}

// NewSecurityAuditor creates an auditor with sensible defaults, optionally
// overridden by the supplied options.
func NewSecurityAuditor(opts ...SecurityAuditorOption) *SecurityAuditor {
	a := &SecurityAuditor{
		pathChecker: NewPathChecker(nil),
		injDetector: NewInjectionDetector(),
		contentVal:  NewContentValidator(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// IsDangerousTool returns true if the given tool name is in the dangerous
// tools list.
func (a *SecurityAuditor) IsDangerousTool(toolName string) bool {
	if len(a.dangerousTools) == 0 {
		return false
	}
	_, ok := a.dangerousTools[strings.TrimSpace(strings.ToLower(toolName))]
	return ok
}

// AuditToolCall runs all security checks against a tool call and publishes
// events for any findings. Returns the aggregated risks.
func (a *SecurityAuditor) AuditToolCall(ctx context.Context, toolName string, input map[string]any) []SecurityRisk {
	var risks []SecurityRisk

	// 0. Dangerous tool check.
	if a.IsDangerousTool(toolName) {
		risks = append(risks, SecurityRisk{
			Category: "dangerous_tool",
			Type:     "dangerous_tool",
			Detail:   fmt.Sprintf("tool %q is marked as dangerous", toolName),
			Severity: severityHigh,
		})
	}

	// 1. Injection detection.
	for _, ir := range a.injDetector.Scan(toolName, input) {
		risks = append(risks, SecurityRisk{
			Category: "injection",
			Type:     ir.Type,
			Detail:   formatRisk(ir),
			Severity: ir.Severity,
		})
	}

	// 2. Path safety checks on string fields that look like file paths.
	for _, value := range extractStringValues(input) {
		if !looksLikePath(value) {
			continue
		}
		if err := a.pathChecker.CheckPath(value); err != nil {
			risks = append(risks, SecurityRisk{
				Category: "path_safety",
				Type:     "path_violation",
				Detail:   err.Error(),
				Severity: severityHigh,
			})
		}
	}

	// 3. Content validation on string fields.
	for _, value := range extractStringValues(input) {
		if looksLikeURL(value) {
			for _, cr := range a.contentVal.ValidateURL(value) {
				risks = append(risks, SecurityRisk{
					Category: "content",
					Type:     cr.Type,
					Detail:   cr.Detail,
					Severity: cr.Severity,
				})
			}
		}
		for _, cr := range a.contentVal.ValidateContent(value) {
			risks = append(risks, SecurityRisk{
				Category: "content",
				Type:     cr.Type,
				Detail:   cr.Detail,
				Severity: cr.Severity,
			})
		}
	}

	// 4. Custom pattern matching against all string inputs.
	if len(a.customPatterns) > 0 {
		for _, value := range extractStringValues(input) {
			for _, cp := range a.customPatterns {
				if cp.re.MatchString(value) {
					risks = append(risks, SecurityRisk{
						Category: cp.category,
						Type:     cp.name,
						Detail:   fmt.Sprintf("custom pattern %q matched", cp.name),
						Severity: cp.severity,
					})
				}
			}
		}
	}

	// Publish events if any risks were found.
	if len(risks) > 0 {
		a.publishSecurityEvents(ctx, toolName, input, risks)
	}

	return risks
}

// HasHighSeverity returns true if any risk in the slice has high severity.
func HasHighSeverity(risks []SecurityRisk) bool {
	for _, r := range risks {
		if r.Severity == severityHigh {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (a *SecurityAuditor) publishSecurityEvents(ctx context.Context, toolName string, input map[string]any, risks []SecurityRisk) {
	if a.bus == nil {
		return
	}

	items := make([]eventbus.SecurityRiskItemAttrs, 0, len(risks))
	for _, risk := range risks {
		items = append(items, eventbus.SecurityRiskItemAttrs{
			Category: risk.Category,
			Type:     risk.Type,
			Detail:   risk.Detail,
			Severity: risk.Severity,
		})
	}
	logging.LogIfErr(ctx, a.bus.Publish(ctx, eventbus.NewSecurityRiskDetectedEvent(
		"",
		"",
		eventbus.SecurityRiskDetectedAttrs{
			ToolName:  toolName,
			RiskCount: len(risks),
			Severity:  highestSecuritySeverity(items),
			Risks:     items,
		},
		nil,
	)), "publish audit event failed")

	// Publish typed events for specific categories.
	for _, risk := range risks {
		var eventType eventbus.EventType
		switch risk.Category {
		case "path_safety":
			eventType = eventbus.EventSecurityPathViolation
		case "injection":
			eventType = eventbus.EventSecurityInjectionAttempt
		default:
			continue
		}
		payload := eventbus.SecurityFindingAttrs{
			ToolName: toolName,
			Type:     risk.Type,
			Detail:   risk.Detail,
			Severity: risk.Severity,
		}
		var event eventbus.Event
		switch eventType {
		case eventbus.EventSecurityPathViolation:
			event = eventbus.NewSecurityPathViolationEvent("", "", payload, nil)
		case eventbus.EventSecurityInjectionAttempt:
			event = eventbus.NewSecurityInjectionAttemptEvent("", "", payload, nil)
		default:
			continue
		}
		logging.LogIfErr(ctx, a.bus.Publish(ctx, event), "publish audit event failed")
	}
}

func highestSecuritySeverity(risks []eventbus.SecurityRiskItemAttrs) string {
	best := ""
	bestRank := -1
	for _, risk := range risks {
		rank := securitySeverityRank(risk.Severity)
		if rank > bestRank {
			bestRank = rank
			best = strings.TrimSpace(risk.Severity)
		}
	}
	return best
}

func securitySeverityRank(severity string) int {
	switch strings.TrimSpace(strings.ToLower(severity)) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// extractStringValues returns all string values from a map, including nested
// maps one level deep.
func extractStringValues(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	var out []string
	for _, v := range m {
		switch val := v.(type) {
		case string:
			out = append(out, val)
		case map[string]any:
			for _, inner := range val {
				if s, ok := inner.(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// looksLikePath returns true if the string resembles a file path.
func looksLikePath(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return true
	}
	if strings.Contains(s, string('/')) && !strings.Contains(s, " ") {
		return true
	}
	return false
}

// looksLikeURL returns true if the string resembles a URL.
func looksLikeURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "file://") ||
		strings.HasPrefix(s, "ftp://")
}
