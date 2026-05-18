package verify

import (
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type toolExecutionFailure struct {
	ToolName string
	Message  string
}

type toolExecutionVerifier struct{}

func (toolExecutionVerifier) Name() string { return "tool.execution" }
func (toolExecutionVerifier) Applies(input Input) bool {
	return len(collectToolExecutionFailures(input.ToolOutputs)) > 0
}

func (toolExecutionVerifier) Verify(input Input) Check {
	failures := collectToolExecutionFailures(input.ToolOutputs)
	check := Check{Name: "tool.execution", Domain: "core", Tools: uniqueToolExecutionFailureNames(failures)}
	if !runCompleted(input.Status) {
		check.Status = StatusSkipped
		check.Summary = "tool execution recovery skipped because the run did not complete"
		return check
	}
	check.Status = StatusWarning
	if len(failures) == 1 {
		check.Summary = "run completed after 1 tool execution failure"
	} else {
		check.Summary = fmt.Sprintf("run completed after %d tool execution failures", len(failures))
	}
	check.Issues = make([]Issue, 0, len(failures))
	for _, failure := range failures {
		message := firstNonEmptyTrimmed(failure.Message, "tool execution failed")
		if failure.ToolName != "" {
			message = fmt.Sprintf("%s: %s", failure.ToolName, message)
		}
		check.Issues = append(check.Issues, Issue{
			Code:     IssueCodeToolExecutionFailed,
			Severity: SeverityWarning,
			Message:  message,
		})
		check.Evidence = append(check.Evidence, message)
	}
	return check
}

func collectToolExecutionFailures(outputs []string) []toolExecutionFailure {
	out := make([]toolExecutionFailure, 0)
	for _, raw := range outputs {
		payload := parseJSONObject(raw)
		if len(payload) == 0 || !hasBool(payload, "tool_execution_error", true) {
			continue
		}
		out = append(out, toolExecutionFailure{
			ToolName: normalize.String(payload["tool_name"]),
			Message:  firstNonEmptyTrimmed(normalize.String(payload["error"]), normalize.String(payload["message"])),
		})
	}
	return out
}

func uniqueToolExecutionFailureNames(failures []toolExecutionFailure) []string {
	out := make([]string, 0, len(failures))
	seen := make(map[string]struct{}, len(failures))
	for _, failure := range failures {
		name := strings.TrimSpace(failure.ToolName)
		if name == "" {
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
