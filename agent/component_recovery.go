package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/logging"
	planpkg "github.com/fulcrus/hopclaw/planner"
)

const maxAutomaticPlanRecoveryAttempts = 1
const maxAutomaticToolLoopRecoveryAttempts = 1

func (a *AgentComponent) applySessionDirectives(ctx context.Context, run *Run, session *Session) (bool, error) {
	if a == nil || a.directives == nil || run == nil || session == nil {
		return false, nil
	}
	items, err := a.directives.Drain(ctx, session.ID, SessionDirectiveSteer)
	if err != nil || len(items) == 0 {
		return false, err
	}
	latest := time.Now().UTC()
	for _, item := range items {
		createdAt := item.CreatedAt
		if createdAt.IsZero() {
			createdAt = latest
		}
		session.Messages = append(session.Messages, contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   item.Content,
			CreatedAt: createdAt,
			Metadata: map[string]any{
				meta.KeyRunID:       run.ID,
				meta.KeyIngressKind: string(item.Kind),
				"synthetic_msg":     true,
			},
		})
		if createdAt.After(latest) {
			latest = createdAt
		}
	}
	session.UpdatedAt = latest
	if err := a.saveSession(ctx, run, session); err != nil {
		return false, err
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewRunSteeredEvent(
		run.ID,
		session.ID,
		eventbus.RunSteeredAttrs{Count: len(items)},
		nil,
	)), "emit event failed", slog.String("kind", string(eventbus.EventRunSteered)))
	return true, nil
}

func (a *AgentComponent) tryRecoverFailedPlan(ctx context.Context, run *Run, session *Session, attempts *int, failureReason string) (bool, error) {
	if a == nil || run == nil || session == nil || attempts == nil {
		return false, nil
	}
	if *attempts >= maxAutomaticPlanRecoveryAttempts {
		return false, nil
	}
	trimmed := strings.TrimSpace(failureReason)
	if trimmed == "" {
		trimmed = "the previous plan failed"
	}
	if err := a.injectRecoveryDirective(ctx, run, session, buildPlanRecoveryDirective(trimmed)); err != nil {
		return false, err
	}
	*attempts++
	run.Plan = nil
	clearExecutionGraph(run)
	transitionRun(run, "", PhasePreparing, withRunError(""))
	if err := a.runs.Update(ctx, run); err != nil {
		return false, err
	}
	return true, nil
}

func (a *AgentComponent) tryRecoverRepeatedToolLoop(ctx context.Context, run *Run, session *Session, attempts *int, calls []ToolCall, repeatCount int) (bool, error) {
	if a == nil || run == nil || session == nil || attempts == nil {
		return false, nil
	}
	if *attempts >= maxAutomaticToolLoopRecoveryAttempts || len(calls) == 0 {
		return false, nil
	}
	if err := a.context.Compact(ctx, &session.Session, contextengine.CompactEmergency); err != nil {
		return false, err
	}
	if err := a.injectRecoveryDirective(ctx, run, session, buildToolLoopRecoveryDirective(calls, repeatCount)); err != nil {
		return false, err
	}
	*attempts++
	return true, nil
}

func (a *AgentComponent) tryRecoverStalledToolProgress(ctx context.Context, run *Run, session *Session, attempts *int, calls []ToolCall, results []contextengine.ToolResult) (bool, error) {
	if a == nil || run == nil || session == nil || attempts == nil {
		return false, nil
	}
	if *attempts >= maxAutomaticToolLoopRecoveryAttempts || len(calls) == 0 {
		return false, nil
	}
	if err := a.context.Compact(ctx, &session.Session, contextengine.CompactEmergency); err != nil {
		return false, err
	}
	if err := a.injectRecoveryDirective(ctx, run, session, buildToolNoProgressDirective(calls, results)); err != nil {
		return false, err
	}
	*attempts++
	return true, nil
}

func (a *AgentComponent) injectRecoveryDirective(ctx context.Context, run *Run, session *Session, content string) error {
	if a == nil || run == nil || session == nil {
		return nil
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	now := time.Now().UTC()
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   content,
		CreatedAt: now,
		Metadata: map[string]any{
			meta.KeyRunID:       run.ID,
			meta.KeyIngressKind: string(SessionDirectiveSteer),
			"synthetic_msg":     true,
			"auto_recovery":     true,
		},
	})
	session.UpdatedAt = now
	if err := a.saveSession(ctx, run, session); err != nil {
		return err
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewRunSteeredEvent(
		run.ID,
		session.ID,
		eventbus.RunSteeredAttrs{Count: 1, AutoRecovery: true},
		nil,
	)), "emit event failed", slog.String("kind", string(eventbus.EventRunSteered)))
	return nil
}

func buildPlanRecoveryDirective(failureReason string) string {
	failureReason = strings.TrimSpace(failureReason)
	if failureReason == "" {
		failureReason = "the previous plan failed"
	}
	return "Continue the current user request. The previous plan did not complete because " + failureReason + ". Re-plan with fewer, higher-confidence steps. Reuse evidence already gathered, avoid repeating failed tool calls, and choose the shortest path that can still produce a verifiable result."
}

func buildToolLoopRecoveryDirective(calls []ToolCall, repeatCount int) string {
	if len(calls) == 0 {
		return "Continue the current user request. Stop repeating the same tool call. Either choose a different high-confidence tool or finish with the best verifiable answer you can produce from the evidence already gathered."
	}
	name := strings.TrimSpace(calls[0].Name)
	if name == "" {
		name = "the same tool"
	}
	if repeatCount < 2 {
		repeatCount = 2
	}
	return fmt.Sprintf("Continue the current user request. You have repeated `%s` %d times with no meaningful progress. Do not repeat the same tool call with unchanged arguments again. Either choose a different high-confidence action or deliver the best verifiable answer you can from the evidence already gathered.", name, repeatCount)
}

func buildToolNoProgressDirective(calls []ToolCall, results []contextengine.ToolResult) string {
	name := "the recent tool batch"
	if len(calls) > 0 {
		if trimmed := strings.TrimSpace(calls[0].Name); trimmed != "" {
			name = trimmed
		}
	}
	outcome := compactProgressText(resultsSummaryForDirective(results))
	if outcome == "" {
		outcome = "the same outcome"
	}
	return fmt.Sprintf("Continue the current user request. Recent tool work with `%s` is not producing new evidence and keeps returning %q. Do not keep probing the same path. Either switch to a different high-confidence action, ask for missing information, or deliver the best verifiable answer you can from the evidence already gathered.", name, outcome)
}

func resultsSummaryForDirective(results []contextengine.ToolResult) string {
	if len(results) == 0 {
		return ""
	}
	parts := make([]string, 0, len(results))
	for _, result := range results {
		normalized := result.Normalized()
		summary := strings.TrimSpace(normalized.Summary)
		if summary == "" {
			summary = strings.TrimSpace(normalized.TranscriptText)
		}
		if normalized.ArtifactURI != "" {
			if summary != "" {
				summary += " "
			}
			summary += normalized.ArtifactURI
		}
		if summary == "" {
			summary = strings.TrimSpace(normalized.ToolName)
		}
		if summary != "" {
			parts = append(parts, summary)
		}
	}
	return strings.Join(parts, "; ")
}

func summarizePlanFailure(plan *planpkg.Plan) string {
	if plan == nil {
		return ""
	}
	planpkg.NormalizeExecution(plan)
	failed := make([]string, 0, 2)
	skipped := 0
	for _, task := range plan.Tasks {
		switch task.Status {
		case planpkg.TaskFailed:
			label := planTaskLabel(&task)
			if label == "" {
				label = task.ID
			}
			if errText := strings.TrimSpace(task.Error); errText != "" {
				failed = append(failed, label+": "+errText)
			} else {
				failed = append(failed, label)
			}
		case planpkg.TaskSkipped:
			skipped++
		}
		if len(failed) == 2 {
			break
		}
	}
	parts := make([]string, 0, 3)
	if len(failed) > 0 {
		parts = append(parts, "failed tasks: "+strings.Join(failed, "; "))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d dependent task(s) were skipped", skipped))
	}
	if len(parts) == 0 {
		return "the previous plan terminated with failures"
	}
	return strings.Join(parts, "; ")
}

func batchHasWaitingApproval(results []TaskExecutionResult) bool {
	for _, r := range results {
		if r.Status == planpkg.TaskRunning {
			return true
		}
	}
	return false
}
