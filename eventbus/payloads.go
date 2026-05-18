package eventbus

import (
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/resultmodel"
)

type DeliveryFailedPayload struct {
	Channel        string `json:"channel,omitempty"`
	TargetID       string `json:"target_id,omitempty"`
	ReplyToID      string `json:"reply_to_id,omitempty"`
	SessionKey     string `json:"session_key,omitempty"`
	Attempts       int    `json:"attempts,omitempty"`
	Error          string `json:"error,omitempty"`
	StatusKind     string `json:"status_kind,omitempty"`
	ContentPreview string `json:"content_preview,omitempty"`
}

func (p DeliveryFailedPayload) ToMap() map[string]any {
	out := map[string]any{}
	if channel := strings.TrimSpace(p.Channel); channel != "" {
		out["channel"] = channel
	}
	if targetID := strings.TrimSpace(p.TargetID); targetID != "" {
		out["target_id"] = targetID
	}
	if replyToID := strings.TrimSpace(p.ReplyToID); replyToID != "" {
		out["reply_to_id"] = replyToID
	}
	if sessionKey := strings.TrimSpace(p.SessionKey); sessionKey != "" {
		out["session_key"] = sessionKey
	}
	if p.Attempts > 0 {
		out["attempts"] = p.Attempts
	}
	if errText := strings.TrimSpace(p.Error); errText != "" {
		out["error"] = errText
	}
	if statusKind := strings.TrimSpace(p.StatusKind); statusKind != "" {
		out["status_kind"] = statusKind
	}
	if preview := strings.TrimSpace(p.ContentPreview); preview != "" {
		out["content_preview"] = preview
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type ToolExecutionResultPayload struct {
	ToolName      string         `json:"tool_name,omitempty"`
	ToolCallID    string         `json:"tool_call_id,omitempty"`
	Status        string         `json:"status,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	Error         string         `json:"error,omitempty"`
	ArtifactURI   string         `json:"artifact_uri,omitempty"`
	ArtifactURIs  []string       `json:"artifact_uris,omitempty"`
	ArtifactCount int            `json:"artifact_count,omitempty"`
	ActionCount   int            `json:"action_count,omitempty"`
	ToolResult    map[string]any `json:"tool_result,omitempty"`
}

func (p ToolExecutionResultPayload) ToMap() map[string]any {
	out := map[string]any{}
	if toolName := strings.TrimSpace(p.ToolName); toolName != "" {
		out["tool_name"] = toolName
	}
	if toolCallID := strings.TrimSpace(p.ToolCallID); toolCallID != "" {
		out["tool_call_id"] = toolCallID
	}
	if status := strings.TrimSpace(p.Status); status != "" {
		out["status"] = status
	}
	if summary := strings.TrimSpace(p.Summary); summary != "" {
		out["summary"] = summary
	}
	if errText := strings.TrimSpace(p.Error); errText != "" {
		out["error"] = errText
	}
	if artifactURI := strings.TrimSpace(p.ArtifactURI); artifactURI != "" {
		out["artifact_uri"] = artifactURI
	}
	if artifactURIs := cloneStrings(p.ArtifactURIs); len(artifactURIs) > 0 {
		out["artifact_uris"] = artifactURIs
	}
	if p.ArtifactCount > 0 {
		out["artifact_count"] = p.ArtifactCount
	}
	if p.ActionCount > 0 {
		out["action_count"] = p.ActionCount
	}
	if bundle := cloneMap(p.ToolResult); len(bundle) > 0 {
		out[resultmodel.MetadataKeyToolResult] = bundle
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type ToolExecutedPayload struct {
	ToolCount                 int                          `json:"tool_count,omitempty"`
	ToolRound                 int                          `json:"tool_round,omitempty"`
	ToolNames                 []string                     `json:"tool_names,omitempty"`
	ApprovalID                string                       `json:"approval_id,omitempty"`
	ArtifactCount             int                          `json:"artifact_count,omitempty"`
	ArtifactURIs              []string                     `json:"artifact_uris,omitempty"`
	TaskID                    string                       `json:"task_id,omitempty"`
	TaskTitle                 string                       `json:"task_title,omitempty"`
	TaskKind                  string                       `json:"task_kind,omitempty"`
	TaskGoal                  string                       `json:"task_goal,omitempty"`
	RunningTaskIDs            []string                     `json:"running_task_ids,omitempty"`
	CompletedTasks            int                          `json:"completed_tasks,omitempty"`
	TotalTasks                int                          `json:"total_tasks,omitempty"`
	ExecutionError            string                       `json:"execution_error,omitempty"`
	Recovered                 bool                         `json:"recovered,omitempty"`
	RecoveryAttempt           int                          `json:"recovery_attempt,omitempty"`
	RecoveryAttemptsRemaining int                          `json:"recovery_attempts_remaining,omitempty"`
	Results                   []ToolExecutionResultPayload `json:"results,omitempty"`
}

func (p ToolExecutedPayload) ToMap() map[string]any {
	out := map[string]any{}
	if p.ToolCount > 0 {
		out["tool_count"] = p.ToolCount
	}
	if p.ToolRound > 0 {
		out["tool_round"] = p.ToolRound
	}
	if names := cloneStrings(p.ToolNames); len(names) > 0 {
		out["tool_names"] = names
	}
	if approvalID := strings.TrimSpace(p.ApprovalID); approvalID != "" {
		out["approval_id"] = approvalID
	}
	if p.ArtifactCount > 0 {
		out["artifact_count"] = p.ArtifactCount
	}
	if uris := cloneStrings(p.ArtifactURIs); len(uris) > 0 {
		out["artifact_uris"] = uris
	}
	if taskID := strings.TrimSpace(p.TaskID); taskID != "" {
		out["task_id"] = taskID
	}
	if taskTitle := strings.TrimSpace(p.TaskTitle); taskTitle != "" {
		out["task_title"] = taskTitle
	}
	if taskKind := strings.TrimSpace(p.TaskKind); taskKind != "" {
		out["task_kind"] = taskKind
	}
	if taskGoal := strings.TrimSpace(p.TaskGoal); taskGoal != "" {
		out["task_goal"] = taskGoal
	}
	if ids := cloneStrings(p.RunningTaskIDs); len(ids) > 0 {
		out["running_task_ids"] = ids
	}
	if p.CompletedTasks > 0 {
		out["completed_tasks"] = p.CompletedTasks
	}
	if p.TotalTasks > 0 {
		out["total_tasks"] = p.TotalTasks
	}
	if executionError := strings.TrimSpace(p.ExecutionError); executionError != "" {
		out["execution_error"] = executionError
	}
	if p.Recovered {
		out["recovered"] = true
	}
	if p.RecoveryAttempt > 0 {
		out["recovery_attempt"] = p.RecoveryAttempt
	}
	if p.RecoveryAttemptsRemaining > 0 {
		out["recovery_attempts_remaining"] = p.RecoveryAttemptsRemaining
	}
	if len(p.Results) > 0 {
		results := make([]map[string]any, 0, len(p.Results))
		for _, result := range p.Results {
			if item := result.ToMap(); len(item) > 0 {
				results = append(results, item)
			}
		}
		if len(results) > 0 {
			out["results"] = results
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func NewRunSubmittedEvent(runID, sessionID string, payload RunSubmittedAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventRunSubmitted, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewRunPhaseChangedEvent(runID, sessionID string, payload RunPhaseChangedAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventRunPhaseChanged, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewTaskProgressEvent(runID, sessionID string, payload TaskProgressAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventTaskProgress, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewRunDispatchEvent(eventType EventType, runID, sessionID string, payload RunDispatchAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(eventType, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewRunStatusEvent(eventType EventType, runID, sessionID string, payload RunStatusAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(eventType, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewRunControlEvent(eventType EventType, runID, sessionID string, payload RunControlAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(eventType, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewPlanTaskEvent(eventType EventType, runID, sessionID string, payload PlanTaskAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(eventType, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewToolExecutedEvent(runID, sessionID string, payload ToolExecutedPayload, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventToolExecuted, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewDeliveryFailedEvent(runID, sessionID string, payload DeliveryFailedPayload, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventDeliveryFailed, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func (e Event) RunSubmittedPayload() (RunSubmittedAttrs, bool) {
	var payload RunSubmittedAttrs
	ok := false
	if queueMode := strings.TrimSpace(normalize.String(e.Attrs["queue_mode"])); queueMode != "" {
		payload.QueueMode = queueMode
		ok = true
	}
	if model := strings.TrimSpace(normalize.String(e.Attrs["model"])); model != "" {
		payload.Model = model
		ok = true
	}
	if executionMode := strings.TrimSpace(normalize.String(e.Attrs["execution_mode"])); executionMode != "" {
		payload.ExecutionMode = executionMode
		ok = true
	}
	if preflight := cloneMap(anyMap(e.Attrs["preflight"])); len(preflight) > 0 {
		payload.Preflight = preflight
		ok = true
	}
	if agentProfile := cloneMap(anyMap(e.Attrs["agent_profile"])); len(agentProfile) > 0 {
		payload.AgentProfile = agentProfile
		ok = true
	}
	if taskContract := cloneMap(anyMap(e.Attrs["task_contract"])); len(taskContract) > 0 {
		payload.TaskContract = taskContract
		ok = true
	}
	return payload, ok
}

func (e Event) RunPhaseChangedPayload() (RunPhaseChangedAttrs, bool) {
	var payload RunPhaseChangedAttrs
	ok := false
	if phase := strings.TrimSpace(normalize.String(e.Attrs["phase"])); phase != "" {
		payload.Phase = phase
		ok = true
	}
	if toolNames := stringSliceValue(e.Attrs["tool_names"]); len(toolNames) > 0 {
		payload.ToolNames = toolNames
		ok = true
	}
	if toolCount, found := intValue(e.Attrs["tool_count"]); found {
		payload.ToolCount = toolCount
		ok = true
	}
	return payload, ok
}

func (e Event) TaskProgressPayload() (TaskProgressAttrs, bool) {
	var payload TaskProgressAttrs
	ok := false
	if currentTask := strings.TrimSpace(normalize.String(e.Attrs["current_task"])); currentTask != "" {
		payload.CurrentTask = currentTask
		ok = true
	}
	if completed, found := intValue(e.Attrs["completed_count"]); found {
		payload.Completed = completed
		ok = true
	}
	if total, found := intValue(e.Attrs["total_tasks"]); found {
		payload.Total = total
		ok = true
	}
	return payload, ok
}

func (e Event) PlanTaskPayload() (PlanTaskAttrs, bool) {
	var payload PlanTaskAttrs
	ok := false
	if taskID := strings.TrimSpace(normalize.String(e.Attrs["task_id"])); taskID != "" {
		payload.TaskID = taskID
		ok = true
	}
	if title := strings.TrimSpace(normalize.String(e.Attrs["task_title"])); title != "" {
		payload.Title = title
		ok = true
	}
	if kind := strings.TrimSpace(normalize.String(e.Attrs["task_kind"])); kind != "" {
		payload.Kind = kind
		ok = true
	}
	if goal := strings.TrimSpace(normalize.String(e.Attrs["task_goal"])); goal != "" {
		payload.Goal = goal
		ok = true
	}
	if errText := strings.TrimSpace(normalize.String(e.Attrs["error"])); errText != "" {
		payload.Error = errText
		ok = true
	}
	if summary := strings.TrimSpace(normalize.String(e.Attrs["result_summary"])); summary != "" {
		payload.ResultSummary = summary
		ok = true
	}
	if completedCount, found := intValue(e.Attrs["completed_count"]); found {
		payload.CompletedCount = completedCount
		ok = true
	}
	if totalTasks, found := intValue(e.Attrs["total_tasks"]); found {
		payload.TotalTasks = totalTasks
		ok = true
	}
	if finalTask := strings.TrimSpace(normalize.String(e.Attrs["final_task"])); finalTask != "" {
		payload.FinalTask = finalTask
		ok = true
	}
	return payload, ok
}

func (e Event) RunStatusPayload() (RunStatusAttrs, bool) {
	var payload RunStatusAttrs
	ok := false
	if channel := strings.TrimSpace(normalize.String(e.Attrs["channel"])); channel != "" {
		payload.Channel = channel
		ok = true
	}
	if errText := strings.TrimSpace(normalize.String(e.Attrs["error"])); errText != "" {
		payload.Error = errText
		ok = true
	}
	if summary := strings.TrimSpace(normalize.String(e.Attrs["summary"])); summary != "" {
		payload.Summary = summary
		ok = true
	}
	return payload, ok
}

func (e Event) RunControlPayload() (RunControlAttrs, bool) {
	var payload RunControlAttrs
	ok := false
	if channel := strings.TrimSpace(normalize.String(e.Attrs["channel"])); channel != "" {
		payload.Channel = channel
		ok = true
	}
	if approvalID := strings.TrimSpace(normalize.String(e.Attrs["approval_id"])); approvalID != "" {
		payload.ApprovalID = approvalID
		ok = true
	}
	if status := strings.TrimSpace(normalize.String(e.Attrs["status"])); status != "" {
		payload.Status = status
		ok = true
	}
	if reason := strings.TrimSpace(normalize.String(e.Attrs["reason"])); reason != "" {
		payload.Reason = reason
		ok = true
	}
	return payload, ok
}

func (e Event) RunDispatchPayload() (RunDispatchAttrs, bool) {
	var payload RunDispatchAttrs
	ok := false
	if channel := strings.TrimSpace(normalize.String(e.Attrs["channel"])); channel != "" {
		payload.Channel = channel
		ok = true
	}
	if model := strings.TrimSpace(normalize.String(e.Attrs["model"])); model != "" {
		payload.Model = model
		ok = true
	}
	if executionMode := strings.TrimSpace(normalize.String(e.Attrs["execution_mode"])); executionMode != "" {
		payload.ExecutionMode = executionMode
		ok = true
	}
	if agentProfile := cloneMap(anyMap(e.Attrs["agent_profile"])); len(agentProfile) > 0 {
		payload.AgentProfile = agentProfile
		ok = true
	}
	if approvalID := strings.TrimSpace(normalize.String(e.Attrs["approval_id"])); approvalID != "" {
		payload.ApprovalID = approvalID
		ok = true
	}
	return payload, ok
}

func (e Event) ToolExecutedPayload() (ToolExecutedPayload, bool) {
	var payload ToolExecutedPayload
	ok := false
	if toolCount, found := intValue(e.Attrs["tool_count"]); found {
		payload.ToolCount = toolCount
		ok = true
	}
	if toolRound, found := intValue(e.Attrs["tool_round"]); found {
		payload.ToolRound = toolRound
		ok = true
	}
	if toolNames := stringSliceValue(e.Attrs["tool_names"]); len(toolNames) > 0 {
		payload.ToolNames = toolNames
		ok = true
	}
	if approvalID := strings.TrimSpace(normalize.String(e.Attrs["approval_id"])); approvalID != "" {
		payload.ApprovalID = approvalID
		ok = true
	}
	if artifactCount, found := intValue(e.Attrs["artifact_count"]); found {
		payload.ArtifactCount = artifactCount
		ok = true
	}
	if artifactURIs := stringSliceValue(e.Attrs["artifact_uris"]); len(artifactURIs) > 0 {
		payload.ArtifactURIs = artifactURIs
		ok = true
	}
	if taskID := strings.TrimSpace(normalize.String(e.Attrs["task_id"])); taskID != "" {
		payload.TaskID = taskID
		ok = true
	}
	if taskTitle := strings.TrimSpace(normalize.String(e.Attrs["task_title"])); taskTitle != "" {
		payload.TaskTitle = taskTitle
		ok = true
	}
	if taskKind := strings.TrimSpace(normalize.String(e.Attrs["task_kind"])); taskKind != "" {
		payload.TaskKind = taskKind
		ok = true
	}
	if taskGoal := strings.TrimSpace(normalize.String(e.Attrs["task_goal"])); taskGoal != "" {
		payload.TaskGoal = taskGoal
		ok = true
	}
	if runningTaskIDs := stringSliceValue(e.Attrs["running_task_ids"]); len(runningTaskIDs) > 0 {
		payload.RunningTaskIDs = runningTaskIDs
		ok = true
	}
	if completedTasks, found := intValue(e.Attrs["completed_tasks"]); found {
		payload.CompletedTasks = completedTasks
		ok = true
	}
	if totalTasks, found := intValue(e.Attrs["total_tasks"]); found {
		payload.TotalTasks = totalTasks
		ok = true
	}
	if executionError := strings.TrimSpace(normalize.String(e.Attrs["execution_error"])); executionError != "" {
		payload.ExecutionError = executionError
		ok = true
	}
	if recovered, found := boolValue(e.Attrs["recovered"]); found {
		payload.Recovered = recovered
		ok = true
	}
	if recoveryAttempt, found := intValue(e.Attrs["recovery_attempt"]); found {
		payload.RecoveryAttempt = recoveryAttempt
		ok = true
	}
	if remaining, found := intValue(e.Attrs["recovery_attempts_remaining"]); found {
		payload.RecoveryAttemptsRemaining = remaining
		ok = true
	}
	if rawResults := mapSliceValue(e.Attrs["results"]); len(rawResults) > 0 {
		payload.Results = make([]ToolExecutionResultPayload, 0, len(rawResults))
		for _, item := range rawResults {
			result := ToolExecutionResultPayload{
				ToolName:     strings.TrimSpace(normalize.String(item["tool_name"])),
				ToolCallID:   strings.TrimSpace(normalize.String(item["tool_call_id"])),
				Status:       strings.TrimSpace(normalize.String(item["status"])),
				Summary:      strings.TrimSpace(normalize.String(item["summary"])),
				Error:        strings.TrimSpace(normalize.String(item["error"])),
				ArtifactURI:  strings.TrimSpace(normalize.String(item["artifact_uri"])),
				ArtifactURIs: stringSliceValue(item["artifact_uris"]),
				ToolResult:   cloneMap(anyMap(item[resultmodel.MetadataKeyToolResult])),
			}
			if artifactCount, found := intValue(item["artifact_count"]); found {
				result.ArtifactCount = artifactCount
			}
			if actionCount, found := intValue(item["action_count"]); found {
				result.ActionCount = actionCount
			}
			payload.Results = append(payload.Results, result)
		}
		ok = true
	}
	return payload, ok
}

func (e Event) DeliveryFailedPayload() (DeliveryFailedPayload, bool) {
	var payload DeliveryFailedPayload
	ok := false
	if channel := strings.TrimSpace(normalize.String(e.Attrs["channel"])); channel != "" {
		payload.Channel = channel
		ok = true
	}
	if targetID := strings.TrimSpace(normalize.String(e.Attrs["target_id"])); targetID != "" {
		payload.TargetID = targetID
		ok = true
	}
	if replyToID := strings.TrimSpace(normalize.String(e.Attrs["reply_to_id"])); replyToID != "" {
		payload.ReplyToID = replyToID
		ok = true
	}
	if sessionKey := strings.TrimSpace(normalize.String(e.Attrs["session_key"])); sessionKey != "" {
		payload.SessionKey = sessionKey
		ok = true
	}
	if attempts, found := intValue(e.Attrs["attempts"]); found {
		payload.Attempts = attempts
		ok = true
	}
	if errText := strings.TrimSpace(normalize.String(e.Attrs["error"])); errText != "" {
		payload.Error = errText
		ok = true
	}
	if statusKind := strings.TrimSpace(normalize.String(e.Attrs["status_kind"])); statusKind != "" {
		payload.StatusKind = statusKind
		ok = true
	}
	if preview := strings.TrimSpace(normalize.String(e.Attrs["content_preview"])); preview != "" {
		payload.ContentPreview = preview
		ok = true
	}
	return payload, ok
}

func newEventWithAttrs(eventType EventType, runID, sessionID string, attrs map[string]any) Event {
	return Event{
		Type:      eventType,
		RunID:     strings.TrimSpace(runID),
		SessionID: strings.TrimSpace(sessionID),
		Time:      time.Now().UTC(),
		Attrs:     attrs,
	}
}

func mergePayloadAttrs(base, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := cloneMap(base)
	if out == nil {
		out = make(map[string]any, len(extra))
	}
	for key, value := range extra {
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func anyMap(value any) map[string]any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return cloneMap(typed)
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = item
		}
		return out
	default:
		return nil
	}
}

func mapSliceValue(value any) []map[string]any {
	switch typed := value.(type) {
	case nil:
		return nil
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneMap(item))
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped := anyMap(item); len(mapped) > 0 {
				out = append(out, mapped)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func stringSliceValue(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return cloneStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(normalize.String(item)); text != "" {
				out = append(out, text)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func intValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float32:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func boolValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err != nil {
			return false, false
		}
		return parsed, true
	default:
		return false, false
	}
}
