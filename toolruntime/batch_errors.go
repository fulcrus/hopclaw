package toolruntime

import (
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/resultmodel"
)

func batchErrorResult(call agent.ToolCall, err error) contextengine.ToolResult {
	message := "tool execution failed"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = strings.TrimSpace(err.Error())
	}
	return contextengine.ToolResult{
		ToolName:   strings.TrimSpace(call.Name),
		ToolCallID: strings.TrimSpace(call.ID),
		Status:     resultmodel.ToolResultError,
		Error: &resultmodel.ResultError{
			Message: message,
		},
	}.Normalized()
}
