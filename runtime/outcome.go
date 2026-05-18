package runtime

import (
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

type RunOutcome string

const (
	RunOutcomeInProgress        RunOutcome = "in_progress"
	RunOutcomeNeedsConfirmation RunOutcome = "needs_confirmation"
	RunOutcomeCompleted         RunOutcome = "completed"
	RunOutcomePartial           RunOutcome = "partial"
	RunOutcomeFailed            RunOutcome = "failed"
	RunOutcomeCancelled         RunOutcome = "cancelled"
)

func DeriveRunOutcome(run *agent.Run, result *RunResult, verification *verifyrt.RunVerification) RunOutcome {
	if run == nil {
		return ""
	}
	if run.WorkflowState != nil && run.WorkflowState.TerminalOutcome == agent.WorkflowTerminalOutcomeFailed {
		return RunOutcomeFailed
	}
	switch run.Status {
	case agent.RunWaitingInput, agent.RunWaitingApproval:
		return RunOutcomeNeedsConfirmation
	case agent.RunQueued, agent.RunRunning, agent.RunStreaming:
		return RunOutcomeInProgress
	case agent.RunCancelled:
		return RunOutcomeCancelled
	case agent.RunFailed:
		return RunOutcomeFailed
	case agent.RunCompleted:
		if verification != nil {
			switch {
			case verification.RequiredFailures > 0:
				return RunOutcomeFailed
			case verification.RequiredWarnings > 0:
				if resultHasUsableContent(result) {
					return RunOutcomePartial
				}
				return RunOutcomeFailed
			case verification.Status == verifyrt.StatusFailed:
				return RunOutcomeFailed
			case verification.Status == verifyrt.StatusWarning && !resultHasUsableContent(result):
				return RunOutcomeFailed
			}
		}
		return RunOutcomeCompleted
	default:
		return RunOutcomeInProgress
	}
}

func resultHasUsableContent(result *RunResult) bool {
	if result == nil {
		return false
	}
	if strings.TrimSpace(result.Output) != "" {
		return true
	}
	if len(result.Deliverables) > 0 {
		return true
	}
	summary := strings.TrimSpace(result.Summary)
	if summary == "" {
		return false
	}
	switch summary {
	case strings.TrimSpace(string(result.Status)), strings.TrimSpace(result.Error):
		return false
	default:
		return true
	}
}
