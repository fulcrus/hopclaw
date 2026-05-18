package channels

import (
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

type ProjectedRunEventKind string

const (
	ProjectedRunEventNone         ProjectedRunEventKind = ""
	ProjectedRunEventPhase        ProjectedRunEventKind = "phase"
	ProjectedRunEventToolProgress ProjectedRunEventKind = "tool_progress"
	ProjectedRunEventPlanProgress ProjectedRunEventKind = "plan_progress"
	ProjectedRunEventApproval     ProjectedRunEventKind = "approval_waiting"
	ProjectedRunEventResumed      ProjectedRunEventKind = "resumed"
	ProjectedRunEventCancelled    ProjectedRunEventKind = "cancelled"
	ProjectedRunEventTerminal     ProjectedRunEventKind = "terminal"
	ProjectedRunEventStreaming    ProjectedRunEventKind = "streaming"
)

type ProjectedRunEvent struct {
	Kind       ProjectedRunEventKind
	StatusKind string
	Content    string
	Phase      string
	ToolRounds int
	ToolNames  []string
	ActiveTask string
	Completed  int
	Total      int
}

type RunEventProjector struct{}

func NewRunEventProjector() *RunEventProjector {
	return &RunEventProjector{}
}

func (p *RunEventProjector) ProjectLive(event eventbus.Event, snapshot RunProgressSnapshot) (ProjectedRunEvent, bool) {
	switch event.Type {
	case eventbus.EventRunPhaseChanged:
		phase, names := ExtractRunPhaseChange(event)
		return ProjectedRunEvent{
			Kind:      ProjectedRunEventPhase,
			Phase:     phase,
			ToolNames: names,
		}, true
	case eventbus.EventToolExecuted:
		rounds, names := ExtractToolProgress(event)
		return ProjectedRunEvent{
			Kind:       ProjectedRunEventToolProgress,
			ToolRounds: rounds,
			ToolNames:  names,
		}, true
	case eventbus.EventTaskProgress:
		activeTask, completed, total := ExtractTaskProgress(event)
		return ProjectedRunEvent{
			Kind:       ProjectedRunEventPlanProgress,
			ActiveTask: activeTask,
			Completed:  completed,
			Total:      total,
		}, true
	case eventbus.EventPlanTaskStarted:
		activeTask, completed, total := ExtractPlanProgress(event)
		return ProjectedRunEvent{
			Kind:       ProjectedRunEventPlanProgress,
			ActiveTask: activeTask,
			Completed:  completed,
			Total:      total,
		}, true
	case eventbus.EventPlanTaskCompleted:
		_, completed, total := ExtractPlanProgress(event)
		return ProjectedRunEvent{
			Kind:      ProjectedRunEventPlanProgress,
			Completed: completed,
			Total:     total,
		}, true
	case eventbus.EventPlanTaskFailed, eventbus.EventPlanTaskSkipped:
		_, completed, total := ExtractPlanProgress(event)
		return ProjectedRunEvent{
			Kind:      ProjectedRunEventPlanProgress,
			Completed: completed,
			Total:     total,
		}, true
	case eventbus.EventPlanSnapshotUpdated:
		payload, _ := event.PlanSnapshotUpdatedPayload()
		return ProjectedRunEvent{
			Kind:      ProjectedRunEventPlanProgress,
			Completed: payload.CompletedCount,
			Total:     payload.TotalTasks,
		}, true
	case eventbus.EventRunWaitingApproval:
		return ProjectedRunEvent{
			Kind:       ProjectedRunEventApproval,
			StatusKind: meta.StatusKindApprovalWaiting.String(),
			Content:    BridgeApprovalMessage(snapshot.Target.InputContent),
		}, true
	case eventbus.EventRunResumed:
		return ProjectedRunEvent{Kind: ProjectedRunEventResumed}, true
	case eventbus.EventRunCancelled:
		if IsSilentRunCancellation(event) {
			return ProjectedRunEvent{}, false
		}
		return ProjectedRunEvent{
			Kind:       ProjectedRunEventCancelled,
			StatusKind: meta.StatusKindCancelled.String(),
			Content:    BridgeCancelledMessage(snapshot.Target.InputContent),
		}, true
	case eventbus.EventModelTextDelta:
		payload, _ := event.ModelTextDeltaPayload()
		return ProjectedRunEvent{
			Kind:    ProjectedRunEventStreaming,
			Content: payload.Delta,
		}, true
	case eventbus.EventModelStreamComplete:
		// No-op: terminal events are already handled by ProjectTerminal.
		return ProjectedRunEvent{}, false
	default:
		return ProjectedRunEvent{}, false
	}
}

func IsSilentRunCancellation(event eventbus.Event) bool {
	if event.Type != eventbus.EventRunCancelled {
		return false
	}
	if payload, ok := event.RunControlPayload(); ok {
		return strings.TrimSpace(payload.Reason) == "preflight_clarification_superseded"
	}
	payload, ok := event.RunCancelledPayload()
	return ok && strings.TrimSpace(payload.Reason) == "preflight_clarification_superseded"
}

func (p *RunEventProjector) ProjectTerminal(session *agent.Session, run *agent.Run, result *runtimesvc.RunResult, verification *verifyrt.RunVerification, event eventbus.Event) (ProjectedRunEvent, bool) {
	if session == nil || run == nil {
		return ProjectedRunEvent{}, false
	}
	switch event.Type {
	case eventbus.EventRunCompleted:
		content := strings.TrimSpace(BridgeCompletedResultContent(session, event.RunID, run.InputEventID, result, verification, event))
		if content == "" {
			return ProjectedRunEvent{}, false
		}
		statusKind := meta.StatusKindCompleted.String()
		if result != nil && strings.TrimSpace(string(result.Outcome)) != "" {
			switch result.Outcome {
			case runtimesvc.RunOutcomePartial:
				statusKind = meta.StatusKindPartial.String()
			case runtimesvc.RunOutcomeFailed:
				statusKind = meta.StatusKindVerificationFailed.String()
			default:
				statusKind = string(result.Outcome)
			}
		} else if verification != nil {
			switch verification.Status {
			case verifyrt.StatusFailed:
				if verification.ShouldBlockDelivery() {
					statusKind = meta.StatusKindVerificationFailed.String()
				} else {
					statusKind = meta.StatusKindPartial.String()
				}
			case verifyrt.StatusWarning:
				statusKind = meta.StatusKindPartial.String()
			}
		}
		return ProjectedRunEvent{
			Kind:       ProjectedRunEventTerminal,
			StatusKind: statusKind,
			Content:    content,
		}, true
	case eventbus.EventRunFailed:
		raw := ""
		if payload, ok := event.RunFailedPayload(); ok {
			raw = payload.Error
		}
		if strings.TrimSpace(raw) == "" && result != nil {
			raw = result.Error
		}
		content := strings.TrimSpace(BridgeFailureMessage(BridgeInputContent(session, run.InputEventID), raw))
		if content == "" {
			return ProjectedRunEvent{}, false
		}
		return ProjectedRunEvent{
			Kind:       ProjectedRunEventTerminal,
			StatusKind: meta.StatusKindFailed.String(),
			Content:    content,
		}, true
	default:
		return ProjectedRunEvent{}, false
	}
}
