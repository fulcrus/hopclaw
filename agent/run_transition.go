package agent

import (
	"log/slog"
	"strings"
	"time"

	domainrun "github.com/fulcrus/hopclaw/internal/domain/runstate"
)

type RunTransitionOption func(*runTransitionSpec)

type runTransitionSpec struct {
	errorSet               bool
	errorText              string
	approvalSet            bool
	approvalID             string
	pendingToolsSet        bool
	pendingTools           []ToolCall
	finishedAtSet          bool
	finishedAt             time.Time
	lastSessionRevisionSet bool
	lastSessionRevision    int64
}

func transitionRun(run *Run, status RunStatus, phase RunPhase, opts ...RunTransitionOption) {
	if run == nil {
		return
	}
	if status != "" && status != run.Status {
		if !domainrun.ValidTransition(run.Status, status) {
			slog.Warn("invalid run status transition attempted",
				slog.String("run_id", run.ID),
				slog.String("from", string(run.Status)),
				slog.String("to", string(status)),
			)
			return
		}
		prev := run.Status
		run.Status = status
		slog.Debug("run status transition",
			slog.String("run_id", run.ID),
			slog.String("from", string(prev)),
			slog.String("to", string(status)),
			slog.String("phase", string(phase)),
		)
	}
	if phase != "" {
		run.Phase = phase
	}
	var spec runTransitionSpec
	for _, opt := range opts {
		if opt != nil {
			opt(&spec)
		}
	}
	if spec.errorSet {
		run.Error = strings.TrimSpace(spec.errorText)
	}
	if spec.approvalSet {
		run.ApprovalID = strings.TrimSpace(spec.approvalID)
	}
	if spec.pendingToolsSet {
		run.PendingTools = cloneToolCalls(spec.pendingTools)
	}
	if spec.finishedAtSet {
		run.FinishedAt = spec.finishedAt.UTC()
	}
	if spec.lastSessionRevisionSet {
		run.LastSessionRevision = spec.lastSessionRevision
	}
}

func withRunError(text string) RunTransitionOption {
	return func(spec *runTransitionSpec) {
		spec.errorSet = true
		spec.errorText = text
	}
}

func withRunApproval(id string) RunTransitionOption {
	return func(spec *runTransitionSpec) {
		spec.approvalSet = true
		spec.approvalID = id
	}
}

func withRunPendingTools(calls []ToolCall) RunTransitionOption {
	return func(spec *runTransitionSpec) {
		spec.pendingToolsSet = true
		spec.pendingTools = cloneToolCalls(calls)
	}
}

func withRunFinishedAt(at time.Time) RunTransitionOption {
	return func(spec *runTransitionSpec) {
		spec.finishedAtSet = true
		spec.finishedAt = at
	}
}

func withRunLastSessionRevision(revision int64) RunTransitionOption {
	return func(spec *runTransitionSpec) {
		spec.lastSessionRevisionSet = true
		spec.lastSessionRevision = revision
	}
}
