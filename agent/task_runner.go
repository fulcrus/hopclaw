package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	planpkg "github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/resultmodel"
)

// TaskExecutionResult captures the outcome of a single plan task execution.
type TaskExecutionResult struct {
	TaskID     string
	Status     planpkg.TaskStatus
	Summary    string
	Error      string
	Artifacts  []string
	Output     string
	Attempt    int
	Outcome    *TaskOutcome
	TokensUsed int
	StartedAt  time.Time
	FinishedAt time.Time
	// Messages produced during task execution. Populated only for parallel
	// tasks so the caller can merge them back into the main session.
	Messages []contextengine.Message
	// RequiresSerial indicates this task could not safely continue in parallel
	// mode and must be retried through the normal serial execution path.
	RequiresSerial bool
}

type dependencyPromptToolResult struct {
	ToolName   string                       `json:"tool_name,omitempty"`
	ToolCallID string                       `json:"tool_call_id,omitempty"`
	Status     resultmodel.ToolResultStatus `json:"status,omitempty"`
	Summary    string                       `json:"summary,omitempty"`
	Structured map[string]any               `json:"structured,omitempty"`
	Artifacts  []resultmodel.ResultArtifact `json:"artifacts,omitempty"`
	Error      *resultmodel.ResultError     `json:"error,omitempty"`
}

type dependencyPromptOutcome struct {
	TaskID         string                       `json:"task_id"`
	Status         planpkg.TaskStatus           `json:"status,omitempty"`
	Attempt        int                          `json:"attempt,omitempty"`
	Summary        string                       `json:"summary,omitempty"`
	OutputBlocks   []resultmodel.ResultBlock    `json:"output_blocks,omitempty"`
	Artifacts      []resultmodel.ResultArtifact `json:"artifacts,omitempty"`
	ToolResults    []dependencyPromptToolResult `json:"tool_results,omitempty"`
	Error          *resultmodel.ResultError     `json:"error,omitempty"`
	MergeStrategy  MergeStrategy                `json:"merge_strategy,omitempty"`
	IdempotencyKey string                       `json:"idempotency_key,omitempty"`
}

type stagedTaskRunRequest struct {
	mode       stagedExecutionMode
	session    *Session
	lease      *sessionLease
	task       *planpkg.Task
	execTask   *ExecutionTask
	depResults []TaskExecutionResult
	onText     func(context.Context, *Run, *Session, contextengine.Message) error
}

// runSingleTask executes a single plan task on a detached session snapshot
// through the same staged prepare/model/tool pipeline used by the serial run
// path. Parallel tasks therefore share the same turn construction and tool
// state machine; only persistence/approval semantics differ.
func (a *AgentComponent) runSingleTask(
	ctx context.Context,
	run *Run,
	session *Session,
	task *planpkg.Task,
	execTask *ExecutionTask,
	depResults []TaskExecutionResult,
) TaskExecutionResult {
	return a.runTaskWithStagedTurns(ctx, run, stagedTaskRunRequest{
		mode:       stagedExecutionModeDetached,
		session:    session,
		task:       task,
		execTask:   execTask,
		depResults: depResults,
		onText:     a.commitDetachedAssistantText,
	})
}

func (a *AgentComponent) runSingleTaskSerial(
	ctx context.Context,
	run *Run,
	lease *sessionLease,
	task *planpkg.Task,
	execTask *ExecutionTask,
	depResults []TaskExecutionResult,
) TaskExecutionResult {
	return a.runTaskWithStagedTurns(ctx, run, stagedTaskRunRequest{
		mode:       stagedExecutionModeSerial,
		lease:      lease,
		task:       task,
		execTask:   execTask,
		depResults: depResults,
		onText:     a.commitAssistantText,
	})
}

func (a *AgentComponent) runTaskWithStagedTurns(
	ctx context.Context,
	run *Run,
	req stagedTaskRunRequest,
) TaskExecutionResult {
	result := TaskExecutionResult{
		StartedAt: time.Now().UTC(),
	}
	if req.task != nil {
		result.TaskID = req.task.ID
	}
	if req.execTask != nil {
		result.Attempt = req.execTask.AttemptCount
	}
	if req.task == nil {
		result.Status = planpkg.TaskFailed
		result.Error = "task is required"
		result.FinishedAt = time.Now().UTC()
		return result
	}

	switch req.mode {
	case stagedExecutionModeSerial:
		if req.lease == nil || req.lease.session == nil {
			result.Status = planpkg.TaskFailed
			result.Error = "session lease is required"
			result.FinishedAt = time.Now().UTC()
			return result
		}
	case stagedExecutionModeDetached:
		if req.session == nil {
			result.Status = planpkg.TaskFailed
			result.Error = "session is required"
			result.FinishedAt = time.Now().UTC()
			return result
		}
	default:
		result.Status = planpkg.TaskFailed
		result.Error = "unsupported staged execution mode"
		result.FinishedAt = time.Now().UTC()
		return result
	}

	toolState := stagedToolLoopState{}
	roundBudget := a.toolRoundBudget(run, req.task, req.task.Goal)
	prepare := buildTaskTurnPrepareOptions(req.task, req.depResults)
	var allToolResults []contextengine.ToolResult

	for round := 0; round < roundBudget; round++ {
		if err := ctx.Err(); err != nil {
			result.Status = planpkg.TaskCancelled
			result.Error = "context cancelled"
			result.FinishedAt = time.Now().UTC()
			return result
		}

		outcome, err := a.executeStagedPreparedTurn(ctx, run, stagedPreparedTurnRequest{
			mode:      req.mode,
			lease:     req.lease,
			session:   req.session,
			toolState: &toolState,
			prepare:   prepare,
			onText:    req.onText,
		})
		if len(outcome.toolResults) > 0 {
			allToolResults = append(allToolResults, outcome.toolResults...)
		}
		if outcome.session != nil {
			switch req.mode {
			case stagedExecutionModeSerial:
				if req.lease != nil {
					req.lease.session = outcome.session
				}
			case stagedExecutionModeDetached:
				req.session = outcome.session
			}
		}
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, ErrRunCancelled) {
				result.Status = planpkg.TaskCancelled
				result.Error = "context cancelled"
				result.FinishedAt = time.Now().UTC()
				finalizeTaskOutcome(&result, req.execTask, allToolResults)
				return result
			}
			result.Status = planpkg.TaskFailed
			result.Error = err.Error()
			result.FinishedAt = time.Now().UTC()
			finalizeTaskOutcome(&result, req.execTask, allToolResults)
			return result
		}
		if outcome.requiresSerial {
			result.Status = planpkg.TaskFailed
			result.Error = ErrParallelApprovalRequired.Error()
			result.RequiresSerial = true
			result.FinishedAt = time.Now().UTC()
			finalizeTaskOutcome(&result, req.execTask, allToolResults)
			return result
		}
		if outcome.waitingApproval {
			result.Status = planpkg.TaskRunning
			result.Error = "waiting_approval"
			result.FinishedAt = time.Now().UTC()
			finalizeTaskOutcome(&result, req.execTask, allToolResults)
			return result
		}
		if outcome.completed {
			session := req.session
			if req.mode == stagedExecutionModeSerial && req.lease != nil {
				session = req.lease.session
			}
			if output, ok := taskRunOutput(session, run.ID); ok {
				result.Status = planpkg.TaskCompleted
				result.Output = output
				result.Summary = summarizePlanTaskResult(output)
				result.FinishedAt = time.Now().UTC()
				finalizeTaskOutcome(&result, req.execTask, allToolResults)
				return result
			}
			result.Status = planpkg.TaskFailed
			result.Error = "completed task turn missing assistant output"
			result.FinishedAt = time.Now().UTC()
			finalizeTaskOutcome(&result, req.execTask, allToolResults)
			return result
		}
		if outcome.retry {
			continue
		}
	}

	result.Status = planpkg.TaskFailed
	result.Error = ErrTooManyToolRounds.Error()
	result.FinishedAt = time.Now().UTC()
	finalizeTaskOutcome(&result, req.execTask, allToolResults)
	return result
}

func buildTaskTurnPrepareOptions(task *planpkg.Task, depResults []TaskExecutionResult) turnPrepareOptions {
	return turnPrepareOptions{
		prepareSystemPrompt: func(run *Run, session *Session, basePrompt string) string {
			return composeTaskSystemPrompt(run, basePrompt, task, depResults)
		},
		guidanceInput: task.Goal,
		selectTools: func(run *Run, tools []ToolDefinition, guidanceInput string) []ToolDefinition {
			filtered := filterToolsForTask(tools, task)
			return prepareToolsForModel(filtered, guidanceInput)
		},
	}
}

func taskRunOutput(session *Session, runID string) (string, bool) {
	if session == nil {
		return "", false
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role != contextengine.RoleAssistant {
			continue
		}
		if !messageMatchesRunID(msg.Metadata, runID) {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		return content, true
	}

	return "", false
}

// composeTaskSystemPrompt builds a system prompt for an individual task,
// including dependency results from the aggregator.
func composeTaskSystemPrompt(run *Run, basePrompt string, task *planpkg.Task, depResults []TaskExecutionResult) string {
	base := strings.TrimSpace(basePrompt)
	if run == nil || run.Plan == nil || task == nil {
		return base
	}

	parts := make([]string, 0, 4)
	if base != "" {
		parts = append(parts, base)
	}

	var planLines []string
	planLines = append(planLines, "You are executing a structured task plan.")
	if goal := strings.TrimSpace(run.Plan.Goal); goal != "" {
		planLines = append(planLines, "Overall goal: "+goal)
	}
	planLines = append(planLines, "Current task: "+planTaskLabel(task))
	if task.Kind != "" {
		planLines = append(planLines, "Current task kind: "+string(task.Kind))
	}
	if task.Goal != "" && strings.TrimSpace(task.Goal) != planTaskLabel(task) {
		planLines = append(planLines, "Current task objective: "+strings.TrimSpace(task.Goal))
	}
	if len(task.Outputs) > 0 {
		planLines = append(planLines, "Expected outputs: "+strings.Join(task.Outputs, ", "))
	}
	if len(task.RequiredCapabilities) > 0 {
		planLines = append(planLines, "Prefer these capabilities when relevant: "+strings.Join(task.RequiredCapabilities, ", "))
	}
	planLines = append(planLines, "Do not claim task completion unless there is concrete output, tool evidence, or artifacts that later verification can inspect.")
	if len(task.VerificationHints) > 0 {
		planLines = append(planLines, "Expected verification domains: "+strings.Join(task.VerificationHints, ", "))
		planLines = append(planLines, "Before finishing, leave clear evidence for those domains and mention the resulting files, artifacts, or delivery status.")
	}
	planLines = append(planLines, "If two similar tool attempts do not change the evidence, stop repeating the same path. Re-plan locally, ask for the missing input, or return the best verifiable partial result instead of looping.")
	if looksLikeSearchResultsExtractionRequest(strings.Join(nonEmptyPlanStrings(run.Plan.Goal, planTaskLabel(task), task.Goal), "\n")) {
		planLines = append(planLines,
			"For search-results tasks, once the results are visible and the first requested items are captured, stop using tools and produce the answer.",
			"If current page context already includes a loaded search-results page, use that page as the target instead of asking the user to restate the query.",
			"Prefer browser.wait plus one evidence pass such as browser.snapshot or browser.screenshot. If titles or links are still unclear, use browser.snapshot_aria once for a targeted structure check, not as a repeated fallback.",
			"Avoid browser.element_text or browser.element_attr on broad search-result containers.",
			"Avoid browser.screenshot_labeled on large search-result pages unless labeled UI evidence is explicitly requested.",
		)
	}

	if len(depResults) > 0 {
		if payload := dependencyOutcomePromptPayload(depResults); payload != "" {
			planLines = append(planLines, "Dependency task outcomes are attached as structured JSON. Reuse only these outcomes/artifacts; do not depend on free-text transcript recall.")
			parts = append(parts, "<task_dependency_outcomes>\n"+payload+"\n</task_dependency_outcomes>")
		}
	}

	if run.Plan.FinalTask != "" {
		if task.ID == run.Plan.FinalTask {
			planLines = append(planLines, "This is the final task. Produce the user-facing answer.")
		} else {
			planLines = append(planLines, "This is not the final task. Produce concise intermediate output that later tasks can reuse, and do not wrap up the overall request yet.")
		}
	}
	parts = append(parts, "<execution_plan>\n"+strings.Join(planLines, "\n")+"\n</execution_plan>")
	return strings.Join(parts, "\n\n")
}

// toContextRunWithPrompt is like toContextRun but with a pre-composed prompt.
func toContextRunWithPrompt(run *Run, systemPrompt string) *contextengine.Run {
	if run == nil {
		return &contextengine.Run{SystemPrompt: systemPrompt}
	}
	return &contextengine.Run{
		ID:              run.ID,
		SystemPrompt:    systemPrompt,
		Goal:            goalForContextRun(run),
		TargetSummary:   targetSummaryForContextRun(run),
		Model:           run.Model,
		JobType:         jobTypeForContextRun(run),
		DetectedDomains: detectedDomainsForContextRun(run),
		AllowedSkills:   allowedSkillsForRun(run),
		MaxOutputTokens: maxOutputTokensForRun(run),
	}
}

func finalizeTaskOutcome(result *TaskExecutionResult, execTask *ExecutionTask, toolResults []contextengine.ToolResult) {
	if result == nil {
		return
	}
	outcome := &TaskOutcome{
		TaskID:      strings.TrimSpace(result.TaskID),
		Status:      result.Status,
		Attempt:     result.Attempt,
		Summary:     strings.TrimSpace(result.Summary),
		ToolResults: cloneOutcomeToolResults(toolResults),
		Artifacts:   collectTaskOutcomeArtifacts(toolResults),
		StartedAt:   result.StartedAt,
		FinishedAt:  result.FinishedAt,
	}
	if execTask != nil {
		outcome.MergeStrategy = execTask.MergeStrategy
		outcome.IdempotencyKey = strings.TrimSpace(execTask.IdempotencyKey)
	}
	if outcome.Summary == "" {
		switch result.Status {
		case planpkg.TaskCompleted:
			outcome.Summary = summarizePlanTaskResult(result.Output)
		default:
			outcome.Summary = summarizePlanTaskResult(result.Error)
		}
	}
	if text := strings.TrimSpace(result.Output); text != "" {
		outcome.OutputBlocks = []resultmodel.ResultBlock{{
			Kind:    resultmodel.ResultBlockText,
			Title:   "assistant_output",
			Content: text,
		}}
	}
	if result.Error != "" && result.Status != planpkg.TaskCompleted {
		outcome.Error = &resultmodel.ResultError{
			Message:   strings.TrimSpace(result.Error),
			Retryable: result.RequiresSerial || result.Status == planpkg.TaskRunning,
		}
	}
	result.Outcome = outcome
	result.Artifacts = taskOutcomeArtifactURIs(outcome.Artifacts)
}

func collectTaskOutcomeArtifacts(results []contextengine.ToolResult) []resultmodel.ResultArtifact {
	if len(results) == 0 {
		return nil
	}
	out := make([]resultmodel.ResultArtifact, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, item := range results {
		normalized := item.Normalized()
		for _, artifact := range normalized.Artifacts {
			uri := strings.TrimSpace(artifact.URI)
			if uri == "" {
				continue
			}
			if _, ok := seen[uri]; ok {
				continue
			}
			seen[uri] = struct{}{}
			out = append(out, artifact)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return cloneOutcomeArtifacts(out)
}

func taskOutcomeArtifactURIs(artifacts []resultmodel.ResultArtifact) []string {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if uri := strings.TrimSpace(artifact.URI); uri != "" {
			out = append(out, uri)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func dependencyOutcomePromptPayload(depResults []TaskExecutionResult) string {
	if len(depResults) == 0 {
		return ""
	}
	payload := struct {
		Dependencies []dependencyPromptOutcome `json:"dependencies"`
	}{
		Dependencies: make([]dependencyPromptOutcome, 0, len(depResults)),
	}
	for _, dep := range depResults {
		outcome := dependencyPromptOutcome{
			TaskID: dep.TaskID,
			Status: dep.Status,
		}
		if dep.Outcome != nil {
			outcome.Status = dep.Outcome.Status
			outcome.Attempt = dep.Outcome.Attempt
			outcome.Summary = strings.TrimSpace(dep.Outcome.Summary)
			outcome.OutputBlocks = cloneOutcomeBlocks(dep.Outcome.OutputBlocks)
			outcome.Artifacts = cloneOutcomeArtifacts(dep.Outcome.Artifacts)
			outcome.Error = cloneDependencyOutcomeError(dep.Outcome.Error)
			outcome.MergeStrategy = dep.Outcome.MergeStrategy
			outcome.IdempotencyKey = strings.TrimSpace(dep.Outcome.IdempotencyKey)
			outcome.ToolResults = compactDependencyToolResults(dep.Outcome.ToolResults)
		} else {
			outcome.Attempt = dep.Attempt
			outcome.Summary = strings.TrimSpace(dep.Summary)
			if text := strings.TrimSpace(dep.Output); text != "" {
				outcome.OutputBlocks = []resultmodel.ResultBlock{{
					Kind:    resultmodel.ResultBlockText,
					Title:   "assistant_output",
					Content: text,
				}}
			}
			for _, uri := range dep.Artifacts {
				if strings.TrimSpace(uri) == "" {
					continue
				}
				outcome.Artifacts = append(outcome.Artifacts, resultmodel.ResultArtifact{
					Kind: "artifact",
					URI:  strings.TrimSpace(uri),
				})
			}
			if dep.Error != "" {
				outcome.Error = &resultmodel.ResultError{Message: strings.TrimSpace(dep.Error)}
			}
		}
		payload.Dependencies = append(payload.Dependencies, outcome)
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return ""
	}
	return string(body)
}

func compactDependencyToolResults(items []resultmodel.ToolResult) []dependencyPromptToolResult {
	if len(items) == 0 {
		return nil
	}
	out := make([]dependencyPromptToolResult, 0, len(items))
	for _, item := range items {
		normalized := item.Normalized()
		entry := dependencyPromptToolResult{
			ToolName:   normalized.ToolName,
			ToolCallID: normalized.ToolCallID,
			Status:     normalized.Status,
			Summary:    strings.TrimSpace(normalized.Summary),
			Structured: cloneMap(normalized.Structured),
			Artifacts:  cloneOutcomeArtifacts(normalized.Artifacts),
			Error:      cloneDependencyOutcomeError(normalized.Error),
		}
		out = append(out, entry)
	}
	return out
}

func cloneDependencyOutcomeError(in *resultmodel.ResultError) *resultmodel.ResultError {
	if in == nil {
		return nil
	}
	out := *in
	out.Metadata = cloneMap(in.Metadata)
	return &out
}
