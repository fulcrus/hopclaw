package runtime

import (
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	"github.com/fulcrus/hopclaw/resultmodel"
)

func collectExecutionTraces(results []resultmodel.ToolResult) []capprofile.ExecutionTrace {
	if len(results) == 0 {
		return nil
	}
	out := make([]capprofile.ExecutionTrace, 0, len(results))
	for _, result := range results {
		normalizedResult := result.Normalized()
		trace, ok := capprofile.DecodeExecutionTrace(normalizedResult.Metadata)
		if !ok {
			continue
		}
		if trace.ToolName == "" {
			trace.ToolName = normalizedResult.ToolName
		}
		if trace.ToolCallID == "" {
			trace.ToolCallID = normalizedResult.ToolCallID
		}
		out = append(out, trace.Normalized())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
