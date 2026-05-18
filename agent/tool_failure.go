package agent

import (
	"context"
	"errors"
	"strings"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/resultmodel"
)

const toolExecutionRecoveryHint = "Retry with corrected input, choose a different tool, or continue with a partial answer that clearly states what remains unfinished."

func shouldRecoverToolExecutionError(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, ErrHookRejected)
}

func buildToolExecutionFailureResults(calls []ToolCall, err error, attempt, remaining int) []contextengine.ToolResult {
	if len(calls) == 0 || err == nil {
		return nil
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "tool execution failed"
	}
	recoveryHint := toolExecutionRecoveryHint
	if remaining == 0 {
		recoveryHint = "No recovery attempts remain after this one. Use a materially different strategy or return a partial answer that clearly states what could not be completed."
	}
	results := make([]contextengine.ToolResult, 0, len(calls))
	for _, call := range calls {
		results = append(results, contextengine.ToolResult{
			ToolName:       call.Name,
			ToolCallID:     call.ID,
			Status:         resultmodel.ToolResultError,
			TranscriptText: "error: " + message,
			Summary:        "tool execution failed",
			Error: &resultmodel.ResultError{
				Code:    "tool_execution_error",
				Message: message,
			},
			Structured: map[string]any{
				"tool_execution_error":        true,
				"tool_name":                   strings.TrimSpace(call.Name),
				"tool_call_id":                strings.TrimSpace(call.ID),
				"error":                       message,
				"recovery_attempt":            attempt,
				"recovery_attempts_remaining": remaining,
				"recovery_hint":               recoveryHint,
			},
			Actions: []resultmodel.ResultAction{{
				Kind:   resultmodel.ResultActionFollowUp,
				Label:  "retry with a different strategy",
				Target: recoveryHint,
			}},
		})
	}
	return results
}
