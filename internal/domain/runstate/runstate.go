package runstate

import "strings"

type QueueMode string

const (
	QueueEnqueue   QueueMode = "enqueue"
	QueueInterrupt QueueMode = "interrupt"
	QueueCoalesce  QueueMode = "coalesce"
	QueueReject    QueueMode = "reject"
)

func (m QueueMode) Normalize() QueueMode {
	switch QueueMode(strings.TrimSpace(string(m))) {
	case QueueInterrupt, QueueCoalesce, QueueReject:
		return m
	default:
		return QueueEnqueue
	}
}

type Status string

const (
	RunQueued          Status = "queued"
	RunWaitingInput    Status = "waiting_input"
	RunRunning         Status = "running"
	RunWaitingApproval Status = "waiting_approval"
	RunStreaming       Status = "streaming"
	RunCompleted       Status = "completed"
	RunFailed          Status = "failed"
	RunCancelled       Status = "cancelled"
)

func (s Status) Terminal() bool {
	switch s {
	case RunCompleted, RunFailed, RunCancelled:
		return true
	default:
		return false
	}
}

func (s Status) Waiting() bool {
	switch s {
	case RunWaitingInput, RunWaitingApproval:
		return true
	default:
		return false
	}
}

func (s Status) Active() bool {
	switch s {
	case RunQueued, RunRunning, RunStreaming, RunWaitingInput, RunWaitingApproval:
		return true
	default:
		return false
	}
}

// AllowedTransitions defines the explicit state machine for run status.
// Any transition not in this map is illegal.
var AllowedTransitions = map[Status][]Status{
	RunQueued:          {RunRunning, RunWaitingInput, RunWaitingApproval, RunCancelled},
	RunWaitingInput:    {RunRunning, RunCancelled},
	RunRunning:         {RunWaitingApproval, RunWaitingInput, RunStreaming, RunCompleted, RunFailed, RunCancelled},
	RunWaitingApproval: {RunQueued, RunRunning, RunCompleted, RunFailed, RunCancelled},
	RunStreaming:       {RunRunning, RunCompleted, RunFailed, RunCancelled},
	// Terminal states allow no further transitions.
	RunCompleted: {},
	RunFailed:    {},
	RunCancelled: {},
}

// ValidTransition returns true if transitioning from → to is allowed.
func ValidTransition(from, to Status) bool {
	allowed, ok := AllowedTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

type Phase string

const (
	PhasePreparing       Phase = "preparing"
	PhaseWaitingModel    Phase = "waiting_model"
	PhaseExecutingTools  Phase = "executing_tools"
	PhaseWaitingApproval Phase = "waiting_approval"
	PhaseCommitting      Phase = "committing"
	PhaseFinalize        Phase = "finalize"
)

func (p Phase) Active() bool {
	switch p {
	case PhasePreparing, PhaseWaitingModel, PhaseExecutingTools, PhaseWaitingApproval, PhaseCommitting:
		return true
	default:
		return false
	}
}

type ExecutionMode string

const (
	ExecutionModeDirect   ExecutionMode = "direct"
	ExecutionModePlanned  ExecutionMode = "planned"
	ExecutionModeWatch    ExecutionMode = "watch"
	ExecutionModeWorkflow ExecutionMode = "workflow"
)

func (m ExecutionMode) Normalize() ExecutionMode {
	switch ExecutionMode(strings.TrimSpace(string(m))) {
	case ExecutionModePlanned, ExecutionModeWatch, ExecutionModeWorkflow:
		return m
	default:
		return ExecutionModeDirect
	}
}
