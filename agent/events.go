package agent

import (
	"context"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	planpkg "github.com/fulcrus/hopclaw/planner"
)

func buildToolExecutedPayload(run *Run, calls []ToolCall, results []contextengine.ToolResult, approvalID string, toolRound int) eventbus.ToolExecutedPayload {
	payload := eventbus.ToolExecutedPayload{
		ToolCount:  len(calls),
		ToolRound:  toolRound,
		ApprovalID: approvalID,
	}
	payload.ToolNames = make([]string, 0, len(calls))
	for _, call := range calls {
		if call.Name == "" {
			continue
		}
		payload.ToolNames = append(payload.ToolNames, call.Name)
	}

	payload.ArtifactURIs = make([]string, 0, len(results))
	payload.Results = make([]eventbus.ToolExecutionResultPayload, 0, len(results))
	for _, result := range results {
		normalized := result.Normalized()
		item := eventbus.ToolExecutionResultPayload{
			ToolName:      normalized.ToolName,
			ToolCallID:    normalized.ToolCallID,
			Status:        string(normalized.Status),
			Summary:       normalized.Summary,
			ArtifactCount: len(normalized.Artifacts),
			ActionCount:   len(normalized.Actions),
		}
		if normalized.Error != nil && normalized.Error.Message != "" {
			item.Error = normalized.Error.Message
		}
		if normalized.ArtifactURI != "" {
			item.ArtifactURI = normalized.ArtifactURI
			payload.ArtifactURIs = append(payload.ArtifactURIs, normalized.ArtifactURI)
		}
		if len(normalized.Artifacts) > 1 {
			uris := make([]string, 0, len(normalized.Artifacts))
			for _, artifact := range normalized.Artifacts {
				if artifact.URI == "" {
					continue
				}
				uris = append(uris, artifact.URI)
				payload.ArtifactURIs = append(payload.ArtifactURIs, artifact.URI)
			}
			if len(uris) > 0 {
				item.ArtifactURIs = uris
			}
		}
		if bundle := normalized.MarshalMetadata(); len(bundle) > 0 {
			item.ToolResult = bundle
		}
		payload.Results = append(payload.Results, item)
	}
	payload.ArtifactCount = len(payload.ArtifactURIs)
	if run != nil && run.Plan != nil {
		planpkg.NormalizeExecution(run.Plan)
		if running := planpkg.Running(run.Plan); len(running) > 0 {
			first := running[0]
			payload.TaskID = first.ID
			payload.TaskTitle = planTaskLabel(first)
			payload.TaskKind = string(first.Kind)
			payload.TaskGoal = first.Goal
			if len(running) > 1 {
				ids := make([]string, len(running))
				for i, t := range running {
					ids[i] = t.ID
				}
				payload.RunningTaskIDs = ids
			}
		} else if task := planpkg.Active(run.Plan); task != nil {
			payload.TaskID = task.ID
			payload.TaskTitle = planTaskLabel(task)
			payload.TaskKind = string(task.Kind)
			payload.TaskGoal = task.Goal
		}
		payload.CompletedTasks = planpkg.CompletedCount(run.Plan)
		payload.TotalTasks = planpkg.TotalCount(run.Plan)
	}
	return payload
}

func buildPlanSnapshotPayload(snapshot PlanExecutionSnapshot) eventbus.PlanSnapshotAttrs {
	attrs := eventbus.PlanSnapshotAttrs{
		CompletedCount:   snapshot.Completed,
		FailedCount:      snapshot.FailedCount,
		SkippedCount:     snapshot.SkippedCount,
		TotalTasks:       snapshot.Total,
		FinalTask:        snapshot.FinalTask,
		CoverageWarnings: cloneStrings(snapshot.CoverageWarnings),
	}
	if len(snapshot.RunningTasks) > 0 {
		ids := make([]string, len(snapshot.RunningTasks))
		for i, t := range snapshot.RunningTasks {
			ids[i] = t.ID
		}
		attrs.RunningTaskIDs = ids
	}
	return attrs
}

// buildPlanSnapshotAttrs creates event attributes from a PlanExecutionSnapshot
// for the plan.snapshot.updated event.
func buildPlanSnapshotAttrs(snapshot PlanExecutionSnapshot) map[string]any {
	return buildPlanSnapshotPayload(snapshot).ToMap()
}

func snapshotPlanExecution(plan *planpkg.Plan) PlanExecutionSnapshot {
	snapshot := PlanExecutionSnapshot{
		Total: planpkg.TotalCount(plan),
	}
	if plan == nil {
		return snapshot
	}
	planpkg.NormalizeExecution(plan)
	snapshot.Goal = plan.Goal
	snapshot.FinalTask = plan.FinalTask
	snapshot.CoverageWarnings = cloneStrings(plan.CoverageWarnings)
	for _, task := range plan.Tasks {
		item := TaskProgressItem{
			ID:     task.ID,
			Title:  planTaskLabel(&task),
			Status: string(task.Status),
		}
		switch task.Status {
		case planpkg.TaskRunning:
			snapshot.RunningTasks = append(snapshot.RunningTasks, item)
		case planpkg.TaskCompleted:
			snapshot.Succeeded = append(snapshot.Succeeded, item)
			snapshot.Completed++
		case planpkg.TaskFailed:
			snapshot.Failed = append(snapshot.Failed, item)
			snapshot.FailedCount++
		case planpkg.TaskSkipped:
			snapshot.Skipped = append(snapshot.Skipped, item)
			snapshot.SkippedCount++
		}
	}
	return snapshot
}

func currentPlanTaskLabel(plan *planpkg.Plan, preferred *planpkg.Task) string {
	if preferred != nil {
		return planTaskLabel(preferred)
	}
	if plan == nil {
		return ""
	}
	planpkg.NormalizeExecution(plan)
	if running := planpkg.Running(plan); len(running) > 0 {
		return planTaskLabel(running[0])
	}
	if active := planpkg.Active(plan); active != nil {
		return planTaskLabel(active)
	}
	return ""
}

func (a *AgentComponent) emitTaskProgress(ctx context.Context, run *Run, preferred *planpkg.Task) error {
	if a == nil || run == nil || run.Plan == nil {
		return nil
	}
	return a.emit(ctx, eventbus.NewTaskProgressEvent(
		run.ID,
		run.SessionID,
		eventbus.TaskProgressAttrs{
			CurrentTask: currentPlanTaskLabel(run.Plan, preferred),
			Completed:   planpkg.CompletedCount(run.Plan),
			Total:       planpkg.TotalCount(run.Plan),
		},
		buildGovernanceEventAttrs(run),
	))
}
