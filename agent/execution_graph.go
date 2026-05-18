package agent

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	planpkg "github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/resultmodel"
)

type MergeStrategy string

const (
	MergeStrategyTaskOrder  MergeStrategy = "task_order"
	MergeStrategySerialOnly MergeStrategy = "serial_only"
)

type SideEffectScope string

const (
	SideEffectScopeReadOnly   SideEffectScope = "read_only"
	SideEffectScopeWorkspace  SideEffectScope = "workspace_write"
	SideEffectScopeSessionAll SideEffectScope = "session_exclusive"
)

type TaskOutcome struct {
	TaskID         string                       `json:"task_id"`
	Status         planpkg.TaskStatus           `json:"status,omitempty"`
	Attempt        int                          `json:"attempt,omitempty"`
	Summary        string                       `json:"summary,omitempty"`
	OutputBlocks   []resultmodel.ResultBlock    `json:"output_blocks,omitempty"`
	ToolResults    []resultmodel.ToolResult     `json:"tool_results,omitempty"`
	Artifacts      []resultmodel.ResultArtifact `json:"artifacts,omitempty"`
	Error          *resultmodel.ResultError     `json:"error,omitempty"`
	MergeStrategy  MergeStrategy                `json:"merge_strategy,omitempty"`
	IdempotencyKey string                       `json:"idempotency_key,omitempty"`
	StartedAt      time.Time                    `json:"started_at,omitempty"`
	FinishedAt     time.Time                    `json:"finished_at,omitempty"`
}

type ExecutionTask struct {
	ID              string             `json:"id"`
	Title           string             `json:"title,omitempty"`
	Kind            planpkg.TaskKind   `json:"kind,omitempty"`
	Goal            string             `json:"goal,omitempty"`
	DependsOn       []string           `json:"depends_on,omitempty"`
	ResourceKeys    []string           `json:"resource_keys,omitempty"`
	SideEffectScope SideEffectScope    `json:"side_effect_scope,omitempty"`
	MergeStrategy   MergeStrategy      `json:"merge_strategy,omitempty"`
	IdempotencyKey  string             `json:"idempotency_key,omitempty"`
	Status          planpkg.TaskStatus `json:"status,omitempty"`
	AttemptCount    int                `json:"attempt_count,omitempty"`
	LastOutcome     *TaskOutcome       `json:"last_outcome,omitempty"`
}

type ExecutionGraph struct {
	RunID          string          `json:"run_id,omitempty"`
	SessionID      string          `json:"session_id,omitempty"`
	Scope          string          `json:"scope,omitempty"`
	MergeStrategy  MergeStrategy   `json:"merge_strategy,omitempty"`
	SingleSession  bool            `json:"single_session,omitempty"`
	SessionLocking bool            `json:"session_locking,omitempty"`
	Tasks          []ExecutionTask `json:"tasks,omitempty"`
}

func ensureExecutionGraph(run *Run) {
	if run == nil || run.Plan == nil {
		return
	}
	run.ExecutionGraph = buildExecutionGraph(run, run.ExecutionGraph)
}

func clearExecutionGraph(run *Run) {
	if run == nil {
		return
	}
	run.ExecutionGraph = nil
}

func executionGraphTask(graph *ExecutionGraph, id string) *ExecutionTask {
	if graph == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	for i := range graph.Tasks {
		if graph.Tasks[i].ID == id {
			return &graph.Tasks[i]
		}
	}
	return nil
}

func executionMetadataForTask(run *Run, task *planpkg.Task) *ExecutionTask {
	if task == nil {
		return nil
	}
	if run != nil {
		ensureExecutionGraph(run)
		if run.ExecutionGraph != nil {
			if existing := executionGraphTask(run.ExecutionGraph, task.ID); existing != nil {
				cloned := normalizeExecutionTask(*existing)
				return &cloned
			}
		}
	}
	derived := executionTaskFromPlanTask(run, task)
	return &derived
}

func selectExecutionBatch(run *Run, ready []*planpkg.Task) []*planpkg.Task {
	if run == nil || len(ready) <= 1 {
		return ready
	}
	ensureExecutionGraph(run)
	if run.ExecutionGraph == nil || run.Plan == nil || run.Plan.Strategy == planpkg.StrategySerial {
		return ready
	}
	selected := make([]*planpkg.Task, 0, len(ready))
	selectedMeta := make([]ExecutionTask, 0, len(ready))
	for _, task := range ready {
		if task == nil {
			continue
		}
		meta := executionTaskFromPlanTask(run, task)
		if existing := executionGraphTask(run.ExecutionGraph, task.ID); existing != nil {
			meta = normalizeExecutionTask(*existing)
		}
		conflict := false
		for _, other := range selectedMeta {
			if executionTasksConflict(meta, other) {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		selected = append(selected, task)
		selectedMeta = append(selectedMeta, meta)
		if meta.MergeStrategy == MergeStrategySerialOnly {
			break
		}
	}
	if len(selected) == 0 {
		return ready[:1]
	}
	return selected
}

func markExecutionTasksRunning(graph *ExecutionGraph, ids ...string) {
	if graph == nil || len(ids) == 0 {
		return
	}
	for _, id := range ids {
		task := executionGraphTask(graph, id)
		if task == nil {
			continue
		}
		task.Status = planpkg.TaskRunning
		task.AttemptCount++
	}
}

func requeueExecutionTasksForRetry(graph *ExecutionGraph, ids ...string) {
	if graph == nil || len(ids) == 0 {
		return
	}
	for _, id := range ids {
		task := executionGraphTask(graph, id)
		if task == nil || task.Status != planpkg.TaskRunning {
			continue
		}
		task.Status = planpkg.TaskQueued
	}
}

func applyTaskResultToExecutionGraph(graph *ExecutionGraph, result TaskExecutionResult) {
	if graph == nil || strings.TrimSpace(result.TaskID) == "" {
		return
	}
	task := executionGraphTask(graph, result.TaskID)
	if task == nil {
		return
	}
	task.Status = result.Status
	if result.Outcome != nil {
		task.LastOutcome = cloneTaskOutcome(result.Outcome)
		return
	}
	if result.Status == planpkg.TaskCompleted || result.Status == planpkg.TaskFailed || result.Status == planpkg.TaskSkipped || result.Status == planpkg.TaskCancelled {
		task.LastOutcome = &TaskOutcome{
			TaskID:         result.TaskID,
			Status:         result.Status,
			Attempt:        result.Attempt,
			Summary:        strings.TrimSpace(result.Summary),
			MergeStrategy:  task.MergeStrategy,
			IdempotencyKey: task.IdempotencyKey,
		}
		if result.Error != "" && result.Status != planpkg.TaskCompleted {
			task.LastOutcome.Error = &resultmodel.ResultError{Message: strings.TrimSpace(result.Error)}
		}
	}
}

func syncExecutionGraphWithPlan(run *Run) {
	if run == nil || run.Plan == nil {
		return
	}
	ensureExecutionGraph(run)
	if run.ExecutionGraph == nil {
		return
	}
	for _, planTask := range run.Plan.Tasks {
		task := executionGraphTask(run.ExecutionGraph, planTask.ID)
		if task == nil {
			continue
		}
		task.Status = planTask.Status
		if planTask.Status != planpkg.TaskSkipped && planTask.Status != planpkg.TaskCancelled {
			continue
		}
		if task.LastOutcome == nil {
			task.LastOutcome = &TaskOutcome{
				TaskID:         planTask.ID,
				Status:         planTask.Status,
				Summary:        strings.TrimSpace(planTask.Error),
				MergeStrategy:  task.MergeStrategy,
				IdempotencyKey: task.IdempotencyKey,
			}
		}
		if strings.TrimSpace(planTask.Error) != "" && task.LastOutcome.Error == nil {
			task.LastOutcome.Error = &resultmodel.ResultError{Message: strings.TrimSpace(planTask.Error)}
		}
	}
}

func executionTasksConflict(a, b ExecutionTask) bool {
	if a.ID == "" || b.ID == "" || a.ID == b.ID {
		return false
	}
	if a.MergeStrategy == MergeStrategySerialOnly || b.MergeStrategy == MergeStrategySerialOnly {
		return true
	}
	if a.SideEffectScope == SideEffectScopeSessionAll || b.SideEffectScope == SideEffectScopeSessionAll {
		return true
	}
	if executionTaskHasUnknownMutableScope(a) || executionTaskHasUnknownMutableScope(b) {
		return true
	}
	if !executionTasksShareResourceKey(a.ResourceKeys, b.ResourceKeys) {
		return false
	}
	return a.SideEffectScope != SideEffectScopeReadOnly || b.SideEffectScope != SideEffectScopeReadOnly
}

func executionTaskHasUnknownMutableScope(task ExecutionTask) bool {
	return task.SideEffectScope != SideEffectScopeReadOnly && len(task.ResourceKeys) == 0
}

func executionTasksShareResourceKey(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, key := range left {
		seen[strings.TrimSpace(key)] = struct{}{}
	}
	for _, key := range right {
		if _, ok := seen[strings.TrimSpace(key)]; ok {
			return true
		}
	}
	return false
}

func buildExecutionGraph(run *Run, existing *ExecutionGraph) *ExecutionGraph {
	if run == nil || run.Plan == nil {
		return nil
	}
	planpkg.NormalizeExecution(run.Plan)
	graph := &ExecutionGraph{
		RunID:          strings.TrimSpace(run.ID),
		SessionID:      strings.TrimSpace(run.SessionID),
		Scope:          "single_session",
		MergeStrategy:  MergeStrategyTaskOrder,
		SingleSession:  true,
		SessionLocking: true,
		Tasks:          make([]ExecutionTask, 0, len(run.Plan.Tasks)),
	}
	existingByID := make(map[string]ExecutionTask, len(run.Plan.Tasks))
	if existing != nil {
		for _, task := range existing.Tasks {
			id := strings.TrimSpace(task.ID)
			if id == "" {
				continue
			}
			existingByID[id] = normalizeExecutionTask(task)
		}
		if existing.MergeStrategy != "" {
			graph.MergeStrategy = existing.MergeStrategy
		}
	}
	for _, planTask := range run.Plan.Tasks {
		task := executionTaskFromPlanTask(run, &planTask)
		if previous, ok := existingByID[task.ID]; ok {
			task.AttemptCount = previous.AttemptCount
			task.LastOutcome = cloneTaskOutcome(previous.LastOutcome)
			if len(previous.ResourceKeys) > 0 {
				task.ResourceKeys = append([]string(nil), previous.ResourceKeys...)
			}
			if previous.SideEffectScope != "" {
				task.SideEffectScope = previous.SideEffectScope
			}
			if previous.MergeStrategy != "" {
				task.MergeStrategy = previous.MergeStrategy
			}
			if strings.TrimSpace(previous.IdempotencyKey) != "" {
				task.IdempotencyKey = strings.TrimSpace(previous.IdempotencyKey)
			}
		}
		graph.Tasks = append(graph.Tasks, task)
	}
	return graph
}

func executionTaskFromPlanTask(run *Run, task *planpkg.Task) ExecutionTask {
	if task == nil {
		return ExecutionTask{}
	}
	out := ExecutionTask{
		ID:              strings.TrimSpace(task.ID),
		Title:           strings.TrimSpace(task.Title),
		Kind:            task.Kind,
		Goal:            strings.TrimSpace(task.Goal),
		DependsOn:       cloneStrings(task.DependsOn),
		ResourceKeys:    executionTaskResourceKeys(task),
		SideEffectScope: executionTaskSideEffectScope(task),
		MergeStrategy:   executionTaskMergeStrategy(task),
		IdempotencyKey:  executionTaskIdempotencyKey(run, task),
		Status:          task.Status,
	}
	if out.Status == "" {
		out.Status = planpkg.TaskQueued
	}
	return normalizeExecutionTask(out)
}

func normalizeExecutionTask(task ExecutionTask) ExecutionTask {
	task.ID = strings.TrimSpace(task.ID)
	task.Title = strings.TrimSpace(task.Title)
	task.Goal = strings.TrimSpace(task.Goal)
	task.DependsOn = normalizeExecutionStrings(task.DependsOn)
	task.ResourceKeys = normalizeExecutionStrings(task.ResourceKeys)
	task.IdempotencyKey = strings.TrimSpace(task.IdempotencyKey)
	if task.MergeStrategy == "" {
		task.MergeStrategy = MergeStrategyTaskOrder
	}
	if task.SideEffectScope == "" {
		task.SideEffectScope = SideEffectScopeReadOnly
	}
	if task.Status == "" {
		task.Status = planpkg.TaskQueued
	}
	task.LastOutcome = cloneTaskOutcome(task.LastOutcome)
	return task
}

func normalizeExecutionStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func executionTaskResourceKeys(task *planpkg.Task) []string {
	if task == nil {
		return nil
	}
	keys := make([]string, 0, len(task.Outputs)+2)
	for _, output := range task.Outputs {
		if trimmed := strings.TrimSpace(output); trimmed != "" {
			keys = append(keys, "output:"+trimmed)
		}
	}
	if len(keys) == 0 && task.Kind != planpkg.TaskResearch && task.Kind != planpkg.TaskReview && task.Kind != planpkg.TaskTranslate {
		keys = append(keys, "session:mutable_state")
	}
	return normalizeExecutionStrings(keys)
}

func executionTaskSideEffectScope(task *planpkg.Task) SideEffectScope {
	if task == nil {
		return SideEffectScopeReadOnly
	}
	switch task.Kind {
	case planpkg.TaskResearch, planpkg.TaskReview, planpkg.TaskTranslate:
		return SideEffectScopeReadOnly
	case planpkg.TaskTransform, planpkg.TaskWrite, planpkg.TaskExecute:
		return SideEffectScopeWorkspace
	default:
		return SideEffectScopeSessionAll
	}
}

func executionTaskMergeStrategy(task *planpkg.Task) MergeStrategy {
	if task == nil {
		return MergeStrategyTaskOrder
	}
	switch task.Kind {
	case planpkg.TaskDeliver:
		return MergeStrategySerialOnly
	default:
		return MergeStrategyTaskOrder
	}
}

func executionTaskIdempotencyKey(run *Run, task *planpkg.Task) string {
	if task == nil {
		return ""
	}
	parts := []string{
		strings.TrimSpace(task.ID),
		string(task.Kind),
		strings.TrimSpace(task.Goal),
	}
	if run != nil {
		parts = append(parts, strings.TrimSpace(run.ID), strings.TrimSpace(run.SessionID))
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "\n")))
	return fmt.Sprintf("task:%s", hex.EncodeToString(sum[:]))
}

func cloneExecutionGraph(in *ExecutionGraph) *ExecutionGraph {
	if in == nil {
		return nil
	}
	out := *in
	out.Tasks = cloneExecutionTasks(in.Tasks)
	return &out
}

func cloneExecutionTasks(in []ExecutionTask) []ExecutionTask {
	if in == nil {
		return nil
	}
	out := make([]ExecutionTask, len(in))
	for i, task := range in {
		out[i] = task
		out[i].DependsOn = cloneStrings(task.DependsOn)
		out[i].ResourceKeys = cloneStrings(task.ResourceKeys)
		out[i].LastOutcome = cloneTaskOutcome(task.LastOutcome)
	}
	return out
}

func cloneTaskOutcome(in *TaskOutcome) *TaskOutcome {
	if in == nil {
		return nil
	}
	out := *in
	out.OutputBlocks = cloneOutcomeBlocks(in.OutputBlocks)
	out.ToolResults = cloneOutcomeToolResults(in.ToolResults)
	out.Artifacts = cloneOutcomeArtifacts(in.Artifacts)
	if in.Error != nil {
		copied := *in.Error
		copied.Metadata = cloneMap(in.Error.Metadata)
		out.Error = &copied
	}
	return &out
}

func cloneOutcomeBlocks(in []resultmodel.ResultBlock) []resultmodel.ResultBlock {
	if in == nil {
		return nil
	}
	out := make([]resultmodel.ResultBlock, len(in))
	copy(out, in)
	return out
}

func cloneOutcomeToolResults(in []resultmodel.ToolResult) []resultmodel.ToolResult {
	if in == nil {
		return nil
	}
	out := make([]resultmodel.ToolResult, len(in))
	for i := range in {
		out[i] = in[i].Normalized()
	}
	return out
}

func cloneOutcomeArtifacts(in []resultmodel.ResultArtifact) []resultmodel.ResultArtifact {
	if in == nil {
		return nil
	}
	out := make([]resultmodel.ResultArtifact, len(in))
	for i, item := range in {
		out[i] = item
		out[i].Metadata = cloneMap(item.Metadata)
	}
	return out
}
