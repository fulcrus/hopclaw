package repl

import (
	"fmt"
	"strings"
)

type Phase string

const (
	PhaseIdle              Phase = "idle"
	PhaseThinking          Phase = "thinking"
	PhasePlanning          Phase = "planning"
	PhaseExecutingTools    Phase = "executing_tools"
	PhaseWaitingApproval   Phase = "waiting_approval"
	PhaseProcessingResults Phase = "processing_results"
	PhaseDelivering        Phase = "delivering"
	PhaseError             Phase = "error"
	PhaseCompleted         Phase = "completed"
	PhaseCancelled         Phase = "cancelled"
	PhasePaused            Phase = "paused"
)

func (p Phase) String() string {
	if p == "" {
		return string(PhaseIdle)
	}
	return string(p)
}

func formatPhaseLine(phase Phase, toolName string) string {
	toolName = strings.TrimSpace(toolName)
	switch phase {
	case PhaseThinking:
		return "* Thinking"
	case PhasePlanning:
		return "* Planning"
	case PhaseExecutingTools:
		if toolName != "" {
			return fmt.Sprintf("* Running tools: %s", toolName)
		}
		return "* Running tools"
	case PhaseWaitingApproval:
		if toolName != "" {
			return fmt.Sprintf("* Waiting approval: %s", toolName)
		}
		return "* Waiting approval"
	case PhaseProcessingResults:
		return "* Processing results"
	case PhaseDelivering:
		return "* Delivering response"
	case PhaseCompleted:
		return "* Completed"
	case PhaseCancelled:
		return "* Cancelled"
	case PhaseError:
		return "* Error"
	case PhasePaused:
		return "* Paused"
	default:
		return "* Idle"
	}
}
