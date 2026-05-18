package toolruntime

import (
	"github.com/fulcrus/hopclaw/agent"
)

// ToolMiddleware wraps a ToolExecutor, adding behavior before/after execution.
// This is the single extension point for cross-cutting concerns like
// edit shadow capture, artifact storage, audit logging, and metrics.
type ToolMiddleware func(next agent.ToolExecutor) agent.ToolExecutor

// Chain composes middleware around a core executor. Middleware is applied
// in order: the first middleware in the list is the outermost wrapper.
//
//	Chain(core, mw1, mw2, mw3)
//	→ mw1(mw2(mw3(core)))
//	→ request flows: mw1 → mw2 → mw3 → core → mw3 → mw2 → mw1
func Chain(core agent.ToolExecutor, mws ...ToolMiddleware) agent.ToolExecutor {
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i] != nil {
			core = mws[i](core)
		}
	}
	return core
}
