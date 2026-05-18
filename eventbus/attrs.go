package eventbus

import (
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

// ---------------------------------------------------------------------------
// Typed event attribute structs
// ---------------------------------------------------------------------------
//
// Prefer these over raw map[string]any when emitting events.
// Each struct has a ToMap() method for backward-compatible assignment to
// Event.Attrs.
//
// Usage:
//
//	event := eventbus.Event{
//	    Type:  eventbus.EventModelRetry,
//	    Attrs: eventbus.ModelRetryAttrs{Model: "gpt-4", ...}.ToMap(),
//	}

// RunPlannedAttrs describes a plan creation event.
type RunPlannedAttrs struct {
	TaskCount        int      `json:"task_count"`
	Strategy         string   `json:"strategy"`
	Replanned        bool     `json:"replanned"`
	CoverageWarnings []string `json:"plan_coverage_warnings,omitempty"`
}

// ToMap converts to map[string]any for Event.Attrs.
func (a RunPlannedAttrs) ToMap() map[string]any {
	m := map[string]any{
		"task_count": a.TaskCount,
		"strategy":   a.Strategy,
		"replanned":  a.Replanned,
	}
	if warnings := normalize.DedupeStrings(cloneStrings(a.CoverageWarnings)); len(warnings) > 0 {
		m["plan_coverage_warnings"] = warnings
	}
	return m
}

// RunPhaseChangedAttrs describes a user-visible execution phase transition.
type RunPhaseChangedAttrs struct {
	Phase     string   `json:"phase"`
	ToolNames []string `json:"tool_names,omitempty"`
	ToolCount int      `json:"tool_count,omitempty"`
}

// ToMap converts to map[string]any for Event.Attrs.
func (a RunPhaseChangedAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if phase := strings.TrimSpace(a.Phase); phase != "" {
		m["phase"] = phase
	}
	if names := cloneStrings(a.ToolNames); len(names) > 0 {
		m["tool_names"] = names
		m["tool_count"] = len(names)
	} else if a.ToolCount > 0 {
		m["tool_count"] = a.ToolCount
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// TaskProgressAttrs describes aggregate task progress for multi-step runs.
type TaskProgressAttrs struct {
	CurrentTask string `json:"current_task,omitempty"`
	Completed   int    `json:"completed_count,omitempty"`
	Total       int    `json:"total_tasks,omitempty"`
}

// ToMap converts to map[string]any for Event.Attrs.
func (a TaskProgressAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if current := strings.TrimSpace(a.CurrentTask); current != "" {
		m["current_task"] = current
	}
	if a.Completed >= 0 {
		m["completed_count"] = a.Completed
	}
	if a.Total > 0 {
		m["total_tasks"] = a.Total
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// ModelRetryAttrs describes a model call retry attempt.
type ModelRetryAttrs struct {
	Model         string `json:"model"`
	Attempt       int    `json:"attempt"`
	MaxAttempts   int    `json:"max_attempts"`
	FailureReason string `json:"failure_reason"`
	DelayMs       int64  `json:"delay_ms"`
	Error         string `json:"error"`
}

// ToMap converts to map[string]any for Event.Attrs.
func (a ModelRetryAttrs) ToMap() map[string]any {
	return map[string]any{
		"model":          a.Model,
		"attempt":        a.Attempt,
		"max_attempts":   a.MaxAttempts,
		"failure_reason": a.FailureReason,
		"delay_ms":       a.DelayMs,
		"error":          a.Error,
	}
}

// ThinkingDegradedAttrs describes a thinking mode degradation.
type ThinkingDegradedAttrs struct {
	Model   string `json:"model"`
	From    string `json:"from"`
	To      string `json:"to"`
	Reason  string `json:"reason"`
	Error   string `json:"error"`
	Attempt int    `json:"attempt"`
}

// ToMap converts to map[string]any for Event.Attrs.
func (a ThinkingDegradedAttrs) ToMap() map[string]any {
	return map[string]any{
		"model":   a.Model,
		"from":    a.From,
		"to":      a.To,
		"reason":  a.Reason,
		"error":   a.Error,
		"attempt": a.Attempt,
	}
}

// ModelRoutedAttrs describes a model routing decision.
type ModelRoutedAttrs struct {
	RequestedModel string `json:"requested_model"`
	SelectedModel  string `json:"selected_model"`
	FailoverFrom   string `json:"failover_from,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// ToMap converts to map[string]any for Event.Attrs.
func (a ModelRoutedAttrs) ToMap() map[string]any {
	m := map[string]any{
		"requested_model": a.RequestedModel,
		"selected_model":  a.SelectedModel,
	}
	if a.FailoverFrom != "" {
		m["failover_from"] = a.FailoverFrom
	}
	if a.Reason != "" {
		m["reason"] = a.Reason
	}
	return m
}

// ModelFailoverAttrs describes a model failover event.
type ModelFailoverAttrs struct {
	FromModel string `json:"from_model"`
	ToModel   string `json:"to_model"`
	Reason    string `json:"reason,omitempty"`
}

// ToMap converts to map[string]any for Event.Attrs.
func (a ModelFailoverAttrs) ToMap() map[string]any {
	m := map[string]any{
		"from_model": a.FromModel,
		"to_model":   a.ToModel,
	}
	if a.Reason != "" {
		m["reason"] = a.Reason
	}
	return m
}

// ToolExecutedAttrs describes a tool execution event.
type ToolExecutedAttrs struct {
	ToolCallID   string   `json:"tool_call_id"`
	ToolName     string   `json:"tool_name"`
	Success      bool     `json:"success"`
	Error        string   `json:"error,omitempty"`
	ArtifactRefs []string `json:"artifact_refs,omitempty"`
}

// ToMap converts to map[string]any for Event.Attrs.
func (a ToolExecutedAttrs) ToMap() map[string]any {
	m := map[string]any{
		"tool_call_id": a.ToolCallID,
		"tool_name":    a.ToolName,
		"success":      a.Success,
	}
	if a.Error != "" {
		m["error"] = a.Error
	}
	if len(a.ArtifactRefs) > 0 {
		m["artifact_refs"] = a.ArtifactRefs
	}
	return m
}

type DeltaAttrs struct {
	Delta string `json:"delta"`
}

func (a DeltaAttrs) ToMap() map[string]any {
	return map[string]any{"delta": a.Delta}
}

type RunSubmittedAttrs struct {
	QueueMode     string         `json:"queue_mode,omitempty"`
	Model         string         `json:"model,omitempty"`
	ExecutionMode string         `json:"execution_mode,omitempty"`
	Preflight     map[string]any `json:"preflight,omitempty"`
	AgentProfile  map[string]any `json:"agent_profile,omitempty"`
	TaskContract  map[string]any `json:"task_contract,omitempty"`
}

func (a RunSubmittedAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if queueMode := strings.TrimSpace(a.QueueMode); queueMode != "" {
		m["queue_mode"] = queueMode
	}
	if model := strings.TrimSpace(a.Model); model != "" {
		m["model"] = model
	}
	if mode := strings.TrimSpace(a.ExecutionMode); mode != "" {
		m["execution_mode"] = mode
	}
	if len(a.Preflight) > 0 {
		m["preflight"] = a.Preflight
	}
	if len(a.AgentProfile) > 0 {
		m["agent_profile"] = a.AgentProfile
	}
	if len(a.TaskContract) > 0 {
		m["task_contract"] = a.TaskContract
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

type RunDispatchAttrs struct {
	Channel       string         `json:"channel,omitempty"`
	Model         string         `json:"model,omitempty"`
	ExecutionMode string         `json:"execution_mode,omitempty"`
	AgentProfile  map[string]any `json:"agent_profile,omitempty"`
	ApprovalID    string         `json:"approval_id,omitempty"`
}

func (a RunDispatchAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if channel := strings.TrimSpace(a.Channel); channel != "" {
		m["channel"] = channel
	}
	if model := strings.TrimSpace(a.Model); model != "" {
		m["model"] = model
	}
	if mode := strings.TrimSpace(a.ExecutionMode); mode != "" {
		m["execution_mode"] = mode
	}
	if len(a.AgentProfile) > 0 {
		m["agent_profile"] = a.AgentProfile
	}
	if approvalID := strings.TrimSpace(a.ApprovalID); approvalID != "" {
		m["approval_id"] = approvalID
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

type RunControlAttrs struct {
	Channel    string `json:"channel,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
	Status     string `json:"status,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

func (a RunControlAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if channel := strings.TrimSpace(a.Channel); channel != "" {
		m["channel"] = channel
	}
	if approvalID := strings.TrimSpace(a.ApprovalID); approvalID != "" {
		m["approval_id"] = approvalID
	}
	if status := strings.TrimSpace(a.Status); status != "" {
		m["status"] = status
	}
	if reason := strings.TrimSpace(a.Reason); reason != "" {
		m["reason"] = reason
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

type RunSteeredAttrs struct {
	Count        int  `json:"count,omitempty"`
	AutoRecovery bool `json:"auto_recovery,omitempty"`
}

func (a RunSteeredAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if a.Count > 0 {
		m["count"] = a.Count
	}
	if a.AutoRecovery {
		m["auto_recovery"] = true
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// ApprovalEventAttrs describes approval lifecycle events.
type ApprovalEventAttrs struct {
	ApprovalID    string                     `json:"approval_id,omitempty"`
	ApprovalKind  string                     `json:"approval_kind,omitempty"`
	Status        string                     `json:"status,omitempty"`
	ResolvedBy    string                     `json:"resolved_by,omitempty"`
	RemainingMs   int64                      `json:"remaining_ms,omitempty"`
	ToolCount     int                        `json:"tool_count,omitempty"`
	Reasons       []string                   `json:"reasons,omitempty"`
	PolicySource  string                     `json:"policy_source,omitempty"`
	PolicySummary string                     `json:"policy_summary,omitempty"`
	ExternalRefs  []ApprovalExternalRefAttrs `json:"external_refs,omitempty"`
}

type ApprovalExternalRefAttrs struct {
	Provider   string `json:"provider,omitempty"`
	ExternalID string `json:"external_id,omitempty"`
	URL        string `json:"url,omitempty"`
	Status     string `json:"status,omitempty"`
	SyncedAt   string `json:"synced_at,omitempty"`
}

// ToMap converts to map[string]any for Event.Attrs.
func (a ApprovalEventAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if id := strings.TrimSpace(a.ApprovalID); id != "" {
		m["approval_id"] = id
	}
	if kind := strings.TrimSpace(a.ApprovalKind); kind != "" {
		m["approval_kind"] = kind
	}
	if status := strings.TrimSpace(a.Status); status != "" {
		m["status"] = status
	}
	if by := strings.TrimSpace(a.ResolvedBy); by != "" {
		m["resolved_by"] = by
	}
	if a.RemainingMs > 0 {
		m["remaining_ms"] = a.RemainingMs
	}
	if a.ToolCount > 0 {
		m["tool_count"] = a.ToolCount
	}
	if reasons := normalize.DedupeStrings(cloneStrings(a.Reasons)); len(reasons) > 0 {
		m["reasons"] = reasons
	}
	if source := strings.TrimSpace(a.PolicySource); source != "" {
		m["policy_source"] = source
	}
	if summary := strings.TrimSpace(a.PolicySummary); summary != "" {
		m["policy_summary"] = summary
	}
	if refs := cloneApprovalExternalRefAttrs(a.ExternalRefs); len(refs) > 0 {
		m["approval_external_refs"] = refs
		m["approval_external_providers"] = approvalExternalProviderNames(refs)
		m["approval_external_statuses"] = approvalExternalStatuses(refs)
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func ApprovalExternalRefAttrsFromTicketRefs(items []approval.ExternalReference) []ApprovalExternalRefAttrs {
	if len(items) == 0 {
		return nil
	}
	out := make([]ApprovalExternalRefAttrs, 0, len(items))
	for _, item := range items {
		ref := ApprovalExternalRefAttrs{
			Provider:   strings.TrimSpace(item.Provider),
			ExternalID: strings.TrimSpace(item.ExternalID),
			URL:        strings.TrimSpace(item.URL),
			Status:     strings.TrimSpace(item.Status),
		}
		if !item.SyncedAt.IsZero() {
			ref.SyncedAt = item.SyncedAt.UTC().Format(time.RFC3339)
		}
		if ref.Provider == "" && ref.ExternalID == "" && ref.URL == "" && ref.Status == "" && ref.SyncedAt == "" {
			continue
		}
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ApprovalExternalReferencesFromValue(value any) []approval.ExternalReference {
	switch typed := value.(type) {
	case nil:
		return nil
	case []approval.ExternalReference:
		return approval.CloneExternalReferences(typed)
	case []ApprovalExternalRefAttrs:
		return approvalExternalReferencesFromAttrItems(typed)
	case []map[string]any:
		return approvalExternalReferencesFromMapItems(typed)
	case []any:
		out := make([]approval.ExternalReference, 0, len(typed))
		for _, item := range typed {
			ref, ok := approvalExternalReferenceFromValue(item)
			if !ok {
				continue
			}
			out = append(out, ref)
		}
		if len(out) == 0 {
			return nil
		}
		return approval.CloneExternalReferences(out)
	default:
		return nil
	}
}

// RunStatusAttrs describes a run lifecycle event.
type RunStatusAttrs struct {
	Channel string `json:"channel,omitempty"`
	Error   string `json:"error,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// ToMap converts to map[string]any for Event.Attrs.
func (a RunStatusAttrs) ToMap() map[string]any {
	if a.Channel == "" && a.Error == "" && a.Summary == "" {
		return nil
	}
	m := map[string]any{}
	if a.Channel != "" {
		m["channel"] = a.Channel
	}
	if a.Error != "" {
		m["error"] = a.Error
	}
	if a.Summary != "" {
		m["summary"] = a.Summary
	}
	return m
}

// GovernanceAttrs describes governance context attached to audit/control events.
type GovernanceAttrs struct {
	Scope                      domainscope.Ref `json:"scope,omitempty"`
	EffectiveConfigSnapshotID  string          `json:"effective_config_snapshot_id,omitempty"`
	PolicyAction               string          `json:"policy_action,omitempty"`
	PolicySource               string          `json:"policy_source,omitempty"`
	PolicySummary              string          `json:"policy_summary,omitempty"`
	PolicyReasons              []string        `json:"policy_reasons,omitempty"`
	PolicyReasonCodes          []string        `json:"policy_reason_codes,omitempty"`
	PolicyLayers               []string        `json:"policy_layers,omitempty"`
	PolicyAuditLabels          []string        `json:"policy_audit_labels,omitempty"`
	PolicyApprovalDefaultScope string          `json:"policy_approval_default_scope,omitempty"`
	PolicyApprovalMaxScope     string          `json:"policy_approval_max_scope,omitempty"`
	PolicyToolNames            []string        `json:"policy_tool_names,omitempty"`
}

// ToMap converts to map[string]any for Event.Attrs.
func (a GovernanceAttrs) ToMap() map[string]any {
	m := map[string]any{}
	scope := a.Scope.Normalize()
	if !scope.IsZero() {
		m["scope"] = scope
	}
	if id := strings.TrimSpace(a.EffectiveConfigSnapshotID); id != "" {
		m["effective_config_snapshot_id"] = id
	}
	if action := strings.TrimSpace(a.PolicyAction); action != "" {
		m["policy_action"] = action
	}
	if source := strings.TrimSpace(a.PolicySource); source != "" {
		m["policy_source"] = source
	}
	if summary := strings.TrimSpace(a.PolicySummary); summary != "" {
		m["policy_summary"] = summary
	}
	if reasons := cloneStrings(a.PolicyReasons); len(reasons) > 0 {
		m["policy_reasons"] = reasons
	}
	if codes := cloneStrings(a.PolicyReasonCodes); len(codes) > 0 {
		m["policy_reason_codes"] = codes
	}
	if layers := cloneStrings(a.PolicyLayers); len(layers) > 0 {
		m["policy_layers"] = layers
	}
	if labels := cloneStrings(a.PolicyAuditLabels); len(labels) > 0 {
		m["policy_audit_labels"] = labels
	}
	if scope := strings.TrimSpace(a.PolicyApprovalDefaultScope); scope != "" {
		m["policy_approval_default_scope"] = scope
	}
	if scope := strings.TrimSpace(a.PolicyApprovalMaxScope); scope != "" {
		m["policy_approval_max_scope"] = scope
	}
	if names := cloneStrings(a.PolicyToolNames); len(names) > 0 {
		m["policy_tool_names"] = names
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneApprovalExternalRefAttrs(items []ApprovalExternalRefAttrs) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		ref := map[string]any{}
		if provider := strings.TrimSpace(item.Provider); provider != "" {
			ref["provider"] = provider
		}
		if externalID := strings.TrimSpace(item.ExternalID); externalID != "" {
			ref["external_id"] = externalID
		}
		if url := strings.TrimSpace(item.URL); url != "" {
			ref["url"] = url
		}
		if status := strings.TrimSpace(item.Status); status != "" {
			ref["status"] = status
		}
		if syncedAt := strings.TrimSpace(item.SyncedAt); syncedAt != "" {
			ref["synced_at"] = syncedAt
		}
		if len(ref) == 0 {
			continue
		}
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func approvalExternalProviderNames(items []map[string]any) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		if provider := strings.TrimSpace(attrStringValue(item["provider"])); provider != "" {
			names = append(names, provider)
		}
	}
	return normalize.DedupeStrings(names)
}

func approvalExternalStatuses(items []map[string]any) []string {
	statuses := make([]string, 0, len(items))
	for _, item := range items {
		provider := strings.TrimSpace(attrStringValue(item["provider"]))
		status := strings.TrimSpace(attrStringValue(item["status"]))
		switch {
		case provider != "" && status != "":
			statuses = append(statuses, provider+":"+status)
		case status != "":
			statuses = append(statuses, status)
		}
	}
	return normalize.DedupeStrings(statuses)
}

func approvalExternalReferencesFromAttrItems(items []ApprovalExternalRefAttrs) []approval.ExternalReference {
	if len(items) == 0 {
		return nil
	}
	out := make([]approval.ExternalReference, 0, len(items))
	for _, item := range items {
		ref, ok := approvalExternalReferenceFromValue(item)
		if !ok {
			continue
		}
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil
	}
	return approval.CloneExternalReferences(out)
}

func approvalExternalReferencesFromMapItems(items []map[string]any) []approval.ExternalReference {
	if len(items) == 0 {
		return nil
	}
	out := make([]approval.ExternalReference, 0, len(items))
	for _, item := range items {
		ref, ok := approvalExternalReferenceFromValue(item)
		if !ok {
			continue
		}
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil
	}
	return approval.CloneExternalReferences(out)
}

func approvalExternalReferenceFromValue(value any) (approval.ExternalReference, bool) {
	switch typed := value.(type) {
	case nil:
		return approval.ExternalReference{}, false
	case approval.ExternalReference:
		ref := approval.CloneExternalReferences([]approval.ExternalReference{typed})
		if len(ref) == 0 {
			return approval.ExternalReference{}, false
		}
		return ref[0], true
	case ApprovalExternalRefAttrs:
		ref := approval.ExternalReference{
			Provider:   strings.TrimSpace(typed.Provider),
			ExternalID: strings.TrimSpace(typed.ExternalID),
			URL:        strings.TrimSpace(typed.URL),
			Status:     strings.TrimSpace(typed.Status),
		}
		if syncedAt := strings.TrimSpace(typed.SyncedAt); syncedAt != "" {
			if parsed, err := time.Parse(time.RFC3339, syncedAt); err == nil {
				ref.SyncedAt = parsed.UTC()
			}
		}
		if ref.Provider == "" && ref.ExternalID == "" && ref.URL == "" && ref.Status == "" && ref.SyncedAt.IsZero() {
			return approval.ExternalReference{}, false
		}
		return ref, true
	case map[string]any:
		return approvalExternalReferenceFromMap(typed)
	case map[string]string:
		ref := map[string]any{}
		for key, item := range typed {
			ref[key] = item
		}
		return approvalExternalReferenceFromMap(ref)
	default:
		return approval.ExternalReference{}, false
	}
}

func approvalExternalReferenceFromMap(item map[string]any) (approval.ExternalReference, bool) {
	ref := approval.ExternalReference{
		Provider:   strings.TrimSpace(attrStringValue(item["provider"])),
		ExternalID: strings.TrimSpace(attrStringValue(item["external_id"])),
		URL:        strings.TrimSpace(attrStringValue(item["url"])),
		Status:     strings.TrimSpace(attrStringValue(item["status"])),
	}
	if syncedAt := strings.TrimSpace(attrStringValue(item["synced_at"])); syncedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, syncedAt); err == nil {
			ref.SyncedAt = parsed.UTC()
		}
	}
	if ref.Provider == "" && ref.ExternalID == "" && ref.URL == "" && ref.Status == "" && ref.SyncedAt.IsZero() {
		return approval.ExternalReference{}, false
	}
	return ref, true
}

func attrStringValue(value any) string {
	return strings.Trim(strings.TrimSpace(normalize.String(value)), `"`)
}

// PlanTaskAttrs describes a plan task lifecycle event.
type PlanTaskAttrs struct {
	TaskID         string `json:"task_id"`
	Title          string `json:"task_title,omitempty"`
	Kind           string `json:"task_kind,omitempty"`
	Goal           string `json:"task_goal,omitempty"`
	Error          string `json:"error,omitempty"`
	ResultSummary  string `json:"result_summary,omitempty"`
	CompletedCount int    `json:"completed_count,omitempty"`
	TotalTasks     int    `json:"total_tasks,omitempty"`
	FinalTask      string `json:"final_task,omitempty"`
}

// ToMap converts to map[string]any for Event.Attrs.
func (a PlanTaskAttrs) ToMap() map[string]any {
	m := map[string]any{"task_id": a.TaskID}
	if a.Title != "" {
		m["task_title"] = a.Title
	}
	if a.Kind != "" {
		m["task_kind"] = a.Kind
	}
	if a.Goal != "" {
		m["task_goal"] = a.Goal
	}
	if a.Error != "" {
		m["error"] = a.Error
	}
	if a.ResultSummary != "" {
		m["result_summary"] = a.ResultSummary
	}
	if a.CompletedCount > 0 || a.TotalTasks > 0 {
		m["completed_count"] = a.CompletedCount
		m["total_tasks"] = a.TotalTasks
	}
	if a.FinalTask != "" {
		m["final_task"] = a.FinalTask
	}
	return m
}

type ArtifactPrunedAttrs struct {
	DeletedCount int       `json:"deleted_count,omitempty"`
	DeletedIDs   []string  `json:"deleted_ids,omitempty"`
	Cutoff       time.Time `json:"cutoff,omitempty"`
	Kind         string    `json:"kind,omitempty"`
	RunID        string    `json:"run_id,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	ToolName     string    `json:"tool_name,omitempty"`
	ToolCallID   string    `json:"tool_call_id,omitempty"`
}

func (a ArtifactPrunedAttrs) ToMap() map[string]any {
	m := map[string]any{
		"deleted_count": a.DeletedCount,
	}
	if ids := cloneStrings(a.DeletedIDs); len(ids) > 0 {
		m["deleted_ids"] = ids
	}
	if !a.Cutoff.IsZero() {
		m["cutoff"] = a.Cutoff
	}
	if kind := strings.TrimSpace(a.Kind); kind != "" {
		m["kind"] = kind
	}
	if runID := strings.TrimSpace(a.RunID); runID != "" {
		m["run_id"] = runID
	}
	if sessionID := strings.TrimSpace(a.SessionID); sessionID != "" {
		m["session_id"] = sessionID
	}
	if toolName := strings.TrimSpace(a.ToolName); toolName != "" {
		m["tool_name"] = toolName
	}
	if toolCallID := strings.TrimSpace(a.ToolCallID); toolCallID != "" {
		m["tool_call_id"] = toolCallID
	}
	return m
}

type PlanSnapshotAttrs struct {
	CompletedCount   int      `json:"completed_count,omitempty"`
	FailedCount      int      `json:"failed_count,omitempty"`
	SkippedCount     int      `json:"skipped_count,omitempty"`
	TotalTasks       int      `json:"total_tasks,omitempty"`
	FinalTask        string   `json:"final_task,omitempty"`
	RunningTaskIDs   []string `json:"running_task_ids,omitempty"`
	CoverageWarnings []string `json:"plan_coverage_warnings,omitempty"`
}

func (a PlanSnapshotAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if a.CompletedCount > 0 {
		m["completed_count"] = a.CompletedCount
	}
	if a.FailedCount > 0 {
		m["failed_count"] = a.FailedCount
	}
	if a.SkippedCount > 0 {
		m["skipped_count"] = a.SkippedCount
	}
	if a.TotalTasks > 0 {
		m["total_tasks"] = a.TotalTasks
	}
	if a.FinalTask != "" {
		m["final_task"] = a.FinalTask
	}
	if running := normalize.DedupeStrings(cloneStrings(a.RunningTaskIDs)); len(running) > 0 {
		m["running_task_ids"] = running
	}
	if warnings := normalize.DedupeStrings(cloneStrings(a.CoverageWarnings)); len(warnings) > 0 {
		m["plan_coverage_warnings"] = warnings
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
