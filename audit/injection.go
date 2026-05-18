package audit

import (
	"fmt"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	injectionTypeCommand = "command_injection"
	injectionTypeSQL     = "sql_injection"
	injectionTypePath    = "path_injection"

	severityHigh   = "high"
	severityMedium = "medium"
	severityLow    = "low"

	// maxInputTruncate is the maximum length of the input field in an
	// InjectionRisk report. Longer values are truncated.
	maxInputTruncate = 200
)

// ---------------------------------------------------------------------------
// InjectionRisk
// ---------------------------------------------------------------------------

// InjectionRisk describes a single injection pattern detected in tool input.
type InjectionRisk struct {
	Type     string `json:"type"`     // e.g. "command_injection"
	Pattern  string `json:"pattern"`  // which pattern matched
	Input    string `json:"input"`    // suspicious input (truncated)
	Severity string `json:"severity"` // "high", "medium", "low"
}

// ---------------------------------------------------------------------------
// InjectionDetector
// ---------------------------------------------------------------------------

// InjectionDetector scans tool inputs for command, SQL, and path injection
// patterns.
type InjectionDetector struct {
	commandPatterns []injectionPattern
	sqlPatterns     []injectionPattern
	pathPatterns    []injectionPattern
}

type injectionPattern struct {
	re       *regexp.Regexp
	label    string
	severity string
}

// NewInjectionDetector creates a detector pre-loaded with common injection
// patterns for commands, SQL, and paths.
func NewInjectionDetector() *InjectionDetector {
	return &InjectionDetector{
		commandPatterns: defaultCommandPatterns(),
		sqlPatterns:     defaultSQLPatterns(),
		pathPatterns:    defaultPathPatterns(),
	}
}

// Scan inspects every string-valued field in the input map and returns any
// injection risks found. Shell execution tools (exec.shell, exec.run,
// exec.script) are exempt from command injection checks because pipes,
// chains, and subshells are legitimate shell syntax for those tools.
func (d *InjectionDetector) Scan(toolName string, input map[string]any) []InjectionRisk {
	if len(input) == 0 {
		return nil
	}

	// exec tools intentionally run shell commands; command injection
	// patterns (pipes, &&, subshells) are normal usage, not attacks.
	skipCommand := isShellExecTool(toolName)

	var risks []InjectionRisk
	for key, raw := range input {
		value, ok := raw.(string)
		if !ok {
			continue
		}
		// Skip command injection checks on content/body fields — these
		// commonly contain HTML/text with pipe chars that are not attacks.
		fieldSkipCommand := skipCommand || isContentField(key)
		risks = append(risks, d.scanValueFiltered(toolName, key, value, fieldSkipCommand)...)
	}
	return risks
}

// isContentField returns true for fields that hold user/web content rather
// than executable commands. These are exempt from command injection patterns.
func isContentField(fieldName string) bool {
	switch strings.ToLower(fieldName) {
	case "content", "body", "html", "text", "input", "data",
		"message", "description", "result", "output", "page",
		"source", "markdown", "raw", "value", "payload":
		return true
	}
	return false
}

func isShellExecTool(name string) bool {
	return name == "exec.shell" || name == "exec.run" || name == "exec.script"
}

func (d *InjectionDetector) scanValueFiltered(_, _ string, value string, skipCommand bool) []InjectionRisk {
	var risks []InjectionRisk
	if !skipCommand {
		risks = append(risks, matchPatterns(value, d.commandPatterns, injectionTypeCommand)...)
	}
	risks = append(risks, matchPatterns(value, d.sqlPatterns, injectionTypeSQL)...)
	risks = append(risks, matchPatterns(value, d.pathPatterns, injectionTypePath)...)
	return risks
}

func matchPatterns(value string, patterns []injectionPattern, riskType string) []InjectionRisk {
	var risks []InjectionRisk
	for _, p := range patterns {
		if p.re.MatchString(value) {
			risks = append(risks, InjectionRisk{
				Type:     riskType,
				Pattern:  p.label,
				Input:    truncate(value, maxInputTruncate),
				Severity: p.severity,
			})
		}
	}
	return risks
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ---------------------------------------------------------------------------
// Default patterns
// ---------------------------------------------------------------------------

func defaultCommandPatterns() []injectionPattern {
	return []injectionPattern{
		{re: regexp.MustCompile(`;\s*\w+`), label: "semicolon_command", severity: severityHigh},
		{re: regexp.MustCompile(`&&\s*\w+`), label: "and_chain_command", severity: severityHigh},
		{re: regexp.MustCompile(`\|\|\s*\w+`), label: "or_chain_command", severity: severityHigh},
		{re: regexp.MustCompile(`\$\([^)]+\)`), label: "dollar_subshell", severity: severityHigh},
		{re: regexp.MustCompile("`[^`]+`"), label: "backtick_execution", severity: severityHigh},
		{re: regexp.MustCompile(`\|\s*(rm|dd|mkfs|shutdown|reboot|kill|pkill)\b`), label: "pipe_to_dangerous_command", severity: severityHigh},
	}
}

func defaultSQLPatterns() []injectionPattern {
	return []injectionPattern{
		{re: regexp.MustCompile(`(?i)'\s*OR\s+1\s*=\s*1`), label: "or_1_equals_1", severity: severityHigh},
		{re: regexp.MustCompile(`(?i)UNION\s+SELECT`), label: "union_select", severity: severityHigh},
		{re: regexp.MustCompile(`(?i)DROP\s+TABLE`), label: "drop_table", severity: severityHigh},
		{re: regexp.MustCompile(`(?i);\s*DELETE\s`), label: "semicolon_delete", severity: severityHigh},
		{re: regexp.MustCompile(`(?i)--\s`), label: "sql_comment_injection", severity: severityMedium},
	}
}

func defaultPathPatterns() []injectionPattern {
	return []injectionPattern{
		{re: regexp.MustCompile(`\.\.[\\/]`), label: "dot_dot_traversal", severity: severityHigh},
		{re: regexp.MustCompile(`%00`), label: "null_byte_encoded", severity: severityHigh},
	}
}

// formatRisk returns a human-readable summary of an InjectionRisk.
func formatRisk(r InjectionRisk) string {
	return fmt.Sprintf("[%s] %s: pattern=%q severity=%s", r.Type, strings.TrimSpace(r.Input), r.Pattern, r.Severity)
}
