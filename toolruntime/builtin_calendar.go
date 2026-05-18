package toolruntime

// This file provides root-package wrappers for calendar handlers that are
// called by other root-package code (e.g. semantic.deliver's nested calls).
// The canonical implementations live in toolruntime/calendar/.

import (
	"context"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/toolruntime/calendar"
)

// handleCalendarCreateICS wraps the sub-package handler for use by nested
// root-package callers (e.g. handleSemanticScheduleCreate).
func handleCalendarCreateICS(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return calendar.HandleCalendarCreateICS(ctx, b, call)
}
