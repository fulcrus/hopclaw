package runtime

import (
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/resultmodel"
)

type TaskOutcomeView struct {
	TaskID         string                       `json:"task_id"`
	Status         string                       `json:"status,omitempty"`
	Attempt        int                          `json:"attempt,omitempty"`
	Summary        string                       `json:"summary,omitempty"`
	OutputBlocks   []resultmodel.ResultBlock    `json:"output_blocks,omitempty"`
	ToolResults    []resultmodel.ToolResult     `json:"tool_results,omitempty"`
	Artifacts      []resultmodel.ResultArtifact `json:"artifacts,omitempty"`
	Error          *resultmodel.ResultError     `json:"error,omitempty"`
	MergeStrategy  string                       `json:"merge_strategy,omitempty"`
	IdempotencyKey string                       `json:"idempotency_key,omitempty"`
	StartedAt      time.Time                    `json:"started_at,omitempty"`
	FinishedAt     time.Time                    `json:"finished_at,omitempty"`
}

func collectRunTaskOutcomes(run *agent.Run) []TaskOutcomeView {
	if run == nil || run.ExecutionGraph == nil || len(run.ExecutionGraph.Tasks) == 0 {
		return nil
	}
	out := make([]TaskOutcomeView, 0, len(run.ExecutionGraph.Tasks))
	for _, task := range run.ExecutionGraph.Tasks {
		if task.LastOutcome == nil {
			continue
		}
		out = append(out, taskOutcomeViewFromAgent(task.LastOutcome))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func taskOutcomeViewFromAgent(outcome *agent.TaskOutcome) TaskOutcomeView {
	if outcome == nil {
		return TaskOutcomeView{}
	}
	view := TaskOutcomeView{
		TaskID:         strings.TrimSpace(outcome.TaskID),
		Status:         strings.TrimSpace(string(outcome.Status)),
		Attempt:        outcome.Attempt,
		Summary:        strings.TrimSpace(outcome.Summary),
		MergeStrategy:  strings.TrimSpace(string(outcome.MergeStrategy)),
		IdempotencyKey: strings.TrimSpace(outcome.IdempotencyKey),
		StartedAt:      outcome.StartedAt.UTC(),
		FinishedAt:     outcome.FinishedAt.UTC(),
	}
	if len(outcome.OutputBlocks) > 0 {
		view.OutputBlocks = append([]resultmodel.ResultBlock(nil), outcome.OutputBlocks...)
	}
	if len(outcome.ToolResults) > 0 {
		view.ToolResults = make([]resultmodel.ToolResult, 0, len(outcome.ToolResults))
		for _, item := range outcome.ToolResults {
			view.ToolResults = append(view.ToolResults, item.Normalized())
		}
	}
	if len(outcome.Artifacts) > 0 {
		view.Artifacts = append([]resultmodel.ResultArtifact(nil), outcome.Artifacts...)
	}
	if outcome.Error != nil {
		clone := *outcome.Error
		view.Error = &clone
	}
	return view
}

func taskOutcomeToolResults(items []TaskOutcomeView) []resultmodel.ToolResult {
	if len(items) == 0 {
		return nil
	}
	out := make([]resultmodel.ToolResult, 0)
	for _, item := range items {
		for _, toolResult := range item.ToolResults {
			normalized := toolResult.Normalized()
			if normalized.ToolName == "" && normalized.ToolCallID == "" && normalized.Summary == "" && normalized.TranscriptText == "" && normalized.PrimaryArtifactURI() == "" {
				continue
			}
			out = append(out, normalized)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeToolResults(groups ...[]resultmodel.ToolResult) []resultmodel.ToolResult {
	if len(groups) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]resultmodel.ToolResult, 0)
	for _, group := range groups {
		for _, item := range group {
			normalized := item.Normalized()
			key := toolResultKey(normalized)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, normalized)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toolResultKey(result resultmodel.ToolResult) string {
	result = result.Normalized()
	switch {
	case strings.TrimSpace(result.ToolCallID) != "":
		return "call:" + strings.TrimSpace(result.ToolCallID)
	case strings.TrimSpace(result.PrimaryArtifactURI()) != "":
		return "artifact:" + strings.TrimSpace(result.PrimaryArtifactURI())
	case strings.TrimSpace(result.ToolName) != "" || strings.TrimSpace(result.Summary) != "":
		return "summary:" + strings.TrimSpace(result.ToolName) + "|" + strings.TrimSpace(result.Summary)
	default:
		return ""
	}
}

func lastTaskOutcomeSummary(items []TaskOutcomeView) string {
	for i := len(items) - 1; i >= 0; i-- {
		if summary := strings.TrimSpace(items[i].Summary); summary != "" {
			return summary
		}
		for _, block := range items[i].OutputBlocks {
			if text := strings.TrimSpace(block.Content); text != "" {
				return text
			}
		}
	}
	return ""
}
