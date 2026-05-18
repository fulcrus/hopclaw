package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/internal/usererror"
	"github.com/fulcrus/hopclaw/resultmodel"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

// DeliverableRef is a stable pointer to an output artifact produced by a run.
type DeliverableRef struct {
	Kind        string         `json:"kind"`
	Name        string         `json:"name,omitempty"`
	URI         string         `json:"uri,omitempty"`
	ToolName    string         `json:"tool_name,omitempty"`
	ContentType string         `json:"content_type,omitempty"`
	SizeBytes   int64          `json:"size_bytes,omitempty"`
	PreviewText string         `json:"preview_text,omitempty"`
	CreatedAt   string         `json:"created_at,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// RunResult is a derived, delivery-friendly projection of a run.
type RunResult struct {
	RunID               string                       `json:"run_id"`
	SessionID           string                       `json:"session_id,omitempty"`
	Status              agent.RunStatus              `json:"status"`
	Outcome             RunOutcome                   `json:"outcome,omitempty"`
	Error               string                       `json:"error,omitempty"`
	EventLedger         *EventLedger                 `json:"event_ledger,omitempty"`
	TaskOutcomes        []TaskOutcomeView            `json:"task_outcomes,omitempty"`
	Governance          *GovernanceReceipt           `json:"governance,omitempty"`
	TaskContract        *agent.TaskContract          `json:"task_contract,omitempty"`
	Delegation          *agent.DelegationContract    `json:"delegation,omitempty"`
	Summary             string                       `json:"summary,omitempty"`
	Output              string                       `json:"output,omitempty"`
	VerificationStatus  string                       `json:"verification_status,omitempty"`
	VerificationSummary string                       `json:"verification_summary,omitempty"`
	Deliverables        []DeliverableRef             `json:"deliverables,omitempty"`
	Delivery            *DeliveryPlan                `json:"delivery,omitempty"`
	Receipts            []DeliveryReceipt            `json:"receipts,omitempty"`
	Bundle              *ResultBundle                `json:"bundle,omitempty"`
	NextActions         []resultmodel.ResultAction   `json:"next_actions,omitempty"`
	ExecutionTraces     []capprofile.ExecutionTrace  `json:"execution_traces,omitempty"`
	Canonical           resultmodel.AutomationResult `json:"canonical,omitempty"`
}

func (s *Service) GetRunResult(ctx context.Context, id string) (*RunResult, error) {
	state, err := s.buildRunCompletionState(ctx, id)
	if err != nil {
		return nil, err
	}
	return state.result, nil
}

type runResultBase struct {
	run         *agent.Run
	session     *agent.Session
	toolResults []resultmodel.ToolResult
	result      *RunResult
}

func (s *Service) getRunResultBase(ctx context.Context, id string) (*runResultBase, error) {
	run, err := s.GetRun(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.buildRunResultBase(ctx, run)
}

func (s *Service) buildRunResultBase(ctx context.Context, run *agent.Run) (*runResultBase, error) {
	if run == nil {
		return nil, fmt.Errorf("run is required")
	}
	events := s.EventSnapshotContext(ctx)
	result := &RunResult{
		RunID:        run.ID,
		SessionID:    run.SessionID,
		Status:       run.Status,
		Error:        strings.TrimSpace(run.Error),
		TaskContract: cloneRunTaskContract(run.TaskContract),
		Delegation:   cloneRunDelegationContract(run.Delegation),
	}
	result.EventLedger = buildRunEventLedger(run, events)
	result.TaskOutcomes = collectRunTaskOutcomes(run)

	var transcriptToolResults []resultmodel.ToolResult
	session, err := s.lookupRunSession(ctx, run.SessionID)
	if err != nil {
		return nil, err
	}
	if session != nil {
		transcriptToolResults = collectToolResultsFromMessagesForRun(session.Messages, run.ID)
		if len(transcriptToolResults) == 0 {
			transcriptToolResults = collectLegacyToolResultsForRun(session.Messages, run)
		}
		result.Output = deriveRunOutput(session.Messages, run.ID, transcriptToolResults, run.Status)
		result.NextActions = collectResultActions(transcriptToolResults)
	}
	taskOutcomeResults := taskOutcomeToolResults(result.TaskOutcomes)
	ledgerToolResults := collectToolResultsFromLedger(result.EventLedger)
	toolResults := mergeToolResults(transcriptToolResults, taskOutcomeResults, ledgerToolResults)
	if len(result.NextActions) == 0 {
		result.NextActions = collectResultActions(toolResults)
	}
	if strings.TrimSpace(result.Output) == "" {
		result.Output = lastTaskOutcomeSummary(result.TaskOutcomes)
	}
	if strings.TrimSpace(result.Output) == "" {
		result.Output = fallbackRunOutput(run, events, toolResults, result.Status)
	}
	result.ExecutionTraces = collectExecutionTraces(toolResults)

	deliverables, err := s.collectRunDeliverables(ctx, run, toolResults, result.TaskOutcomes, result.EventLedger)
	if err != nil {
		return nil, err
	}
	result.Deliverables = deliverables
	result.Summary = summarizeRunResultForLocale(result, toolResults, runResultLocale(session, run))
	return &runResultBase{
		run:         run,
		session:     session,
		toolResults: toolResults,
		result:      result,
	}, nil
}

func cloneRunTaskContract(contract *agent.TaskContract) *agent.TaskContract {
	if contract == nil {
		return nil
	}
	cloned := *contract
	if len(contract.SuggestedDomains) > 0 {
		cloned.SuggestedDomains = append([]string(nil), contract.SuggestedDomains...)
	}
	if len(contract.CapabilityHints) > 0 {
		cloned.CapabilityHints = append([]string(nil), contract.CapabilityHints...)
	}
	if len(contract.ExpectedDeliverables) > 0 {
		cloned.ExpectedDeliverables = append([]agent.TaskContractDeliverable(nil), contract.ExpectedDeliverables...)
	}
	if len(contract.AcceptanceCriteria) > 0 {
		cloned.AcceptanceCriteria = make([]agent.TaskContractAcceptance, len(contract.AcceptanceCriteria))
		for i, item := range contract.AcceptanceCriteria {
			cloned.AcceptanceCriteria[i] = item
			if len(item.DeliverableKinds) > 0 {
				cloned.AcceptanceCriteria[i].DeliverableKinds = append([]string(nil), item.DeliverableKinds...)
			}
			if len(item.EvidenceHints) > 0 {
				cloned.AcceptanceCriteria[i].EvidenceHints = append([]string(nil), item.EvidenceHints...)
			}
		}
	}
	if len(contract.MissingInfo) > 0 {
		cloned.MissingInfo = make([]agent.TaskContractMissingInfo, len(contract.MissingInfo))
		for i, item := range contract.MissingInfo {
			cloned.MissingInfo[i] = item
			if len(item.Hints) > 0 {
				cloned.MissingInfo[i].Hints = append([]string(nil), item.Hints...)
			}
		}
	}
	if len(contract.ResolvedInfo) > 0 {
		cloned.ResolvedInfo = append([]agent.TaskContractResolvedInfo(nil), contract.ResolvedInfo...)
	}
	return &cloned
}

func cloneRunDelegationContract(contract *agent.DelegationContract) *agent.DelegationContract {
	if contract == nil {
		return nil
	}
	cloned := *contract
	if len(contract.AllowedDomains) > 0 {
		cloned.AllowedDomains = append([]string(nil), contract.AllowedDomains...)
	}
	if len(contract.AllowedTools) > 0 {
		cloned.AllowedTools = append([]string(nil), contract.AllowedTools...)
	}
	return &cloned
}

func applyRunOutcome(result *RunResult, run *agent.Run, verification *verifyrt.RunVerification) {
	if result == nil || run == nil {
		return
	}
	if verification != nil {
		result.VerificationStatus = string(verification.Status)
		result.VerificationSummary = strings.TrimSpace(verification.Summary)
	}
	result.Outcome = DeriveRunOutcome(run, result, verification)
}

func (s *Service) lookupRunSession(ctx context.Context, sessionID string) (*agent.Session, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	session, err := agent.LoadSession(ctx, s.sessions, sessionID, agent.ScopeFilter{})
	if err != nil {
		return nil, fmt.Errorf("get session %s for run result: %w", sessionID, err)
	}
	return session, nil
}

func deriveRunOutput(messages []contextengine.Message, runID string, toolResults []resultmodel.ToolResult, status agent.RunStatus) string {
	if len(messages) == 0 || strings.TrimSpace(runID) == "" {
		if isTerminalRunStatus(status) {
			return summarizeToolResults(toolResults)
		}
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != contextengine.RoleAssistant {
			continue
		}
		if !messageMatchesRun(msg, runID) {
			continue
		}
		content := strings.TrimSpace(msg.TextContent())
		if content != "" {
			return content
		}
	}
	if isTerminalRunStatus(status) {
		return summarizeToolResults(toolResults)
	}
	return ""
}

func fallbackRunOutput(run *agent.Run, events []eventbus.Event, toolResults []resultmodel.ToolResult, status agent.RunStatus) string {
	if !isTerminalRunStatus(status) {
		return ""
	}
	if text := summarizeToolResults(toolResults); text != "" {
		return text
	}
	if text := planResultSummary(run); text != "" {
		return text
	}
	if run == nil {
		return ""
	}
	return runStatusSummaryFromEvents(events, run.ID)
}

func runToolCallIDs(messages []contextengine.Message, runID string) map[string]struct{} {
	if len(messages) == 0 || strings.TrimSpace(runID) == "" {
		return nil
	}
	out := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Role != contextengine.RoleAssistant || !messageMatchesRun(msg, runID) {
			continue
		}
		for _, call := range msg.ToolCalls {
			if trimmed := strings.TrimSpace(call.ID); trimmed != "" {
				out[trimmed] = struct{}{}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectToolMessagesForRun(messages []contextengine.Message, runID string) []contextengine.Message {
	if len(messages) == 0 || strings.TrimSpace(runID) == "" {
		return nil
	}
	toolCallIDs := runToolCallIDs(messages, runID)
	out := make([]contextengine.Message, 0)
	for _, msg := range messages {
		if msg.Role != contextengine.RoleTool {
			continue
		}
		if messageMatchesRun(msg, runID) {
			out = append(out, msg)
			continue
		}
		if len(toolCallIDs) == 0 {
			continue
		}
		if _, ok := toolCallIDs[strings.TrimSpace(msg.ToolCallID)]; ok {
			out = append(out, msg)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectLegacyToolMessagesInRunWindow(messages []contextengine.Message, run *agent.Run) []contextengine.Message {
	if len(messages) == 0 || run == nil {
		return nil
	}
	start := run.StartedAt
	end := run.FinishedAt
	out := make([]contextengine.Message, 0)
	for _, msg := range messages {
		if msg.Role != contextengine.RoleTool {
			continue
		}
		if messageMatchesRun(msg, run.ID) {
			continue
		}
		if !messageInRunWindow(msg.CreatedAt, start, end) {
			continue
		}
		out = append(out, msg)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func messageMatchesRun(msg contextengine.Message, runID string) bool {
	if strings.TrimSpace(runID) == "" || msg.Metadata == nil {
		return false
	}
	value, ok := msg.Metadata[meta.KeyRunID]
	if !ok || value == nil {
		return false
	}
	return strings.TrimSpace(fmt.Sprint(value)) == runID
}

func (s *Service) collectRunDeliverables(ctx context.Context, run *agent.Run, results []resultmodel.ToolResult, taskOutcomes []TaskOutcomeView, ledger *EventLedger) ([]DeliverableRef, error) {
	if run == nil {
		return nil, nil
	}
	out := make([]DeliverableRef, 0)
	seen := make(map[string]int)

	appendRef := func(ref DeliverableRef) {
		if key := deliverableKey(ref); key != "" {
			if idx, ok := seen[key]; ok {
				out[idx] = mergeDeliverableRef(out[idx], ref)
				return
			}
			seen[key] = len(out)
		}
		out = append(out, ref)
	}

	for _, result := range results {
		for _, artifactRef := range result.Artifacts {
			appendRef(DeliverableRef{
				Kind:        normalize.FirstNonEmpty(strings.TrimSpace(artifactRef.Kind), "artifact"),
				Name:        strings.TrimSpace(artifactRef.Name),
				URI:         strings.TrimSpace(artifactRef.URI),
				ToolName:    strings.TrimSpace(result.ToolName),
				ContentType: strings.TrimSpace(artifactRef.ContentType),
				SizeBytes:   artifactRef.SizeBytes,
				PreviewText: strings.TrimSpace(artifactRef.PreviewText),
				Metadata:    cloneMetadata(artifactRef.Metadata),
			})
		}
	}

	for _, outcome := range taskOutcomes {
		for _, artifactRef := range outcome.Artifacts {
			appendRef(DeliverableRef{
				Kind:        normalize.FirstNonEmpty(strings.TrimSpace(artifactRef.Kind), "artifact"),
				Name:        strings.TrimSpace(artifactRef.Name),
				URI:         strings.TrimSpace(artifactRef.URI),
				ContentType: strings.TrimSpace(artifactRef.ContentType),
				SizeBytes:   artifactRef.SizeBytes,
				PreviewText: strings.TrimSpace(artifactRef.PreviewText),
				Metadata:    cloneMetadata(artifactRef.Metadata),
			})
		}
	}

	for _, ref := range collectDeliverablesFromLedger(ledger) {
		appendRef(ref)
	}

	if s.artifacts != nil {
		blobs, err := s.artifacts.List(ctx, artifact.ListFilter{RunID: run.ID})
		if err != nil {
			return nil, fmt.Errorf("list artifacts for run %s: %w", run.ID, err)
		}
		for _, blob := range blobs {
			if blob == nil {
				continue
			}
			appendRef(DeliverableRef{
				Kind:        "artifact",
				Name:        deliverableNameFromURI(blob.URI, metadataString(blob.Metadata, "name")),
				URI:         strings.TrimSpace(blob.URI),
				ToolName:    metadataString(blob.Metadata, meta.KeyToolName),
				ContentType: strings.TrimSpace(blob.ContentType),
				SizeBytes:   blob.Size,
				PreviewText: s.previewArtifactText(ctx, blob),
				CreatedAt:   formatTimeRFC3339(blob.CreatedAt),
				Metadata:    cloneMetadata(blob.Metadata),
			})
		}
	}

	return out, nil
}

func collectArtifactURIs(attrs map[string]any) []string {
	if len(attrs) == 0 {
		return nil
	}
	var out []string
	appendString := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" && value != "<nil>" {
			out = append(out, value)
		}
	}
	switch items := attrs["artifact_uris"].(type) {
	case []string:
		for _, item := range items {
			appendString(item)
		}
	case []any:
		for _, item := range items {
			appendString(fmt.Sprint(item))
		}
	}
	switch items := attrs["results"].(type) {
	case []map[string]any:
		for _, item := range items {
			if value, ok := item["artifact_uri"]; ok && value != nil {
				appendString(fmt.Sprint(value))
			}
		}
	case []any:
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if value, ok := item["artifact_uri"]; ok && value != nil {
				appendString(fmt.Sprint(value))
			}
		}
	}
	return out
}

func summarizeRunResult(result *RunResult, toolResults []resultmodel.ToolResult) string {
	return summarizeRunResultForLocale(result, toolResults, "")
}

func summarizeRunResultForLocale(result *RunResult, toolResults []resultmodel.ToolResult, locale string) string {
	if result == nil {
		return ""
	}
	if !isTerminalRunStatus(result.Status) && strings.TrimSpace(result.Output) == "" {
		if text := compactSummary(humanizedResultError(result.Error, locale)); text != "" {
			return text
		}
		return strings.TrimSpace(string(result.Status))
	}
	if text := compactSummary(result.Output); text != "" {
		return text
	}
	if text := summarizeToolResults(toolResults); text != "" {
		return text
	}
	if text := compactSummary(humanizedResultError(result.Error, locale)); text != "" {
		return text
	}
	if text := compactSummary(lastTaskOutcomeSummary(result.TaskOutcomes)); text != "" {
		return text
	}
	if len(result.Deliverables) > 0 {
		return fmt.Sprintf("%d deliverable(s) ready", len(result.Deliverables))
	}
	return strings.TrimSpace(string(result.Status))
}

func humanizedResultError(raw string, locale string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return usererror.HumanizeText(raw, locale)
}

func runResultLocale(session *agent.Session, run *agent.Run) string {
	if session == nil {
		return ""
	}
	input := runResultInputContent(session, run)
	if input == "" {
		return ""
	}
	return string(usererror.InferLocale(input))
}

func runResultInputContent(session *agent.Session, run *agent.Run) string {
	if session == nil {
		return ""
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role != contextengine.RoleUser {
			continue
		}
		if run != nil && strings.TrimSpace(run.InputEventID) != "" {
			if mid, _ := msg.Metadata[meta.KeyMessageID].(string); strings.TrimSpace(mid) == strings.TrimSpace(run.InputEventID) {
				return strings.TrimSpace(msg.Content)
			}
			continue
		}
		if text := strings.TrimSpace(msg.Content); text != "" {
			return text
		}
	}
	return ""
}

func isTerminalRunStatus(status agent.RunStatus) bool {
	switch status {
	case agent.RunCompleted, agent.RunFailed, agent.RunCancelled:
		return true
	default:
		return false
	}
}

func compactSummary(text string) string {
	return compactText(text, resultSummaryMaxLen)
}

func deliverableKey(ref DeliverableRef) string {
	if uri := strings.TrimSpace(ref.URI); uri != "" {
		return uri
	}
	if toolName := strings.TrimSpace(ref.ToolName); toolName != "" {
		return ref.Kind + "|" + toolName
	}
	return ""
}

const (
	resultSummaryMaxLen         = 240
	deliverablePreviewMaxLen    = 280
	deliverablePreviewReadLimit = 32 * 1024
)

func compactText(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	return strings.TrimSpace(text[:maxLen]) + "..."
}

func collectToolResultsFromMessagesForRun(messages []contextengine.Message, runID string) []resultmodel.ToolResult {
	if len(messages) == 0 {
		return nil
	}
	runToolMessages := messages
	if strings.TrimSpace(runID) != "" {
		runToolMessages = collectToolMessagesForRun(messages, runID)
	}
	out := make([]resultmodel.ToolResult, 0)
	for _, msg := range runToolMessages {
		if result, ok := resultmodel.DecodeToolResultMetadata(msg.Metadata); ok {
			out = append(out, result)
			continue
		}
		content := strings.TrimSpace(msg.TextContent())
		if content == "" && strings.TrimSpace(msg.Name) == "" {
			continue
		}
		out = append(out, resultmodel.ToolResult{
			ToolName:       strings.TrimSpace(msg.Name),
			ToolCallID:     strings.TrimSpace(msg.ToolCallID),
			TranscriptText: content,
			Content:        content,
		}.Normalized())
	}
	return out
}

func collectToolResultsFromEvents(events []eventbus.Event, runID string) []resultmodel.ToolResult {
	if len(events) == 0 || strings.TrimSpace(runID) == "" {
		return nil
	}
	out := make([]resultmodel.ToolResult, 0)
	for _, event := range events {
		if event.Type != eventbus.EventToolExecuted || event.RunID != runID {
			continue
		}
		if payload, ok := event.ToolExecutedPayload(); ok {
			for _, raw := range payload.Results {
				if result, ok := resultmodel.DecodeToolResultMetadata(map[string]any{
					resultmodel.MetadataKeyToolResult: raw.ToolResult,
				}); ok {
					out = append(out, result)
					continue
				}
				result := resultmodel.ToolResult{
					ToolName:    strings.TrimSpace(raw.ToolName),
					ToolCallID:  strings.TrimSpace(raw.ToolCallID),
					Summary:     strings.TrimSpace(raw.Summary),
					ArtifactURI: strings.TrimSpace(raw.ArtifactURI),
				}.Normalized()
				if result.ToolName == "" && result.ToolCallID == "" && result.Summary == "" && result.ArtifactURI == "" {
					continue
				}
				out = append(out, result)
			}
			continue
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectToolOutputsFromEvents(events []eventbus.Event, runID string) []string {
	results := collectToolResultsFromEvents(events, runID)
	if len(results) == 0 {
		return nil
	}
	out := make([]string, 0, len(results))
	for _, result := range results {
		normalized := result.Normalized()
		for _, text := range []string{normalized.Content, normalized.TranscriptText, normalized.Summary} {
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				out = append(out, trimmed)
				break
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func eventResults(raw any) []any {
	switch typed := raw.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func planResultSummary(run *agent.Run) string {
	if run == nil || run.Plan == nil {
		return ""
	}
	finalID := strings.TrimSpace(run.Plan.FinalTask)
	if finalID != "" {
		for _, task := range run.Plan.Tasks {
			if strings.TrimSpace(task.ID) != finalID {
				continue
			}
			if summary := strings.TrimSpace(task.ResultSummary); summary != "" {
				return summary
			}
		}
	}
	for i := len(run.Plan.Tasks) - 1; i >= 0; i-- {
		if summary := strings.TrimSpace(run.Plan.Tasks[i].ResultSummary); summary != "" {
			return summary
		}
	}
	return ""
}

func runStatusSummaryFromEvents(events []eventbus.Event, runID string) string {
	if len(events) == 0 || strings.TrimSpace(runID) == "" {
		return ""
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.RunID != runID {
			continue
		}
		switch event.Type {
		case eventbus.EventRunCompleted, eventbus.EventRunFailed, eventbus.EventRunCancelled:
			if payload, ok := event.RunStatusPayload(); ok {
				if summary := strings.TrimSpace(payload.Summary); summary != "" {
					return summary
				}
			}
		}
	}
	return ""
}

func collectLegacyToolResultsForRun(messages []contextengine.Message, run *agent.Run) []resultmodel.ToolResult {
	runToolMessages := collectLegacyToolMessagesInRunWindow(messages, run)
	if len(runToolMessages) == 0 {
		return nil
	}
	out := make([]resultmodel.ToolResult, 0, len(runToolMessages))
	for _, msg := range runToolMessages {
		if result, ok := resultmodel.DecodeToolResultMetadata(msg.Metadata); ok {
			out = append(out, result)
			continue
		}
		content := strings.TrimSpace(msg.TextContent())
		if content == "" && strings.TrimSpace(msg.Name) == "" {
			continue
		}
		out = append(out, resultmodel.ToolResult{
			ToolName:       strings.TrimSpace(msg.Name),
			ToolCallID:     strings.TrimSpace(msg.ToolCallID),
			TranscriptText: content,
			Content:        content,
		}.Normalized())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func messageInRunWindow(createdAt, start, end time.Time) bool {
	if createdAt.IsZero() {
		return false
	}
	if !start.IsZero() && createdAt.Before(start) {
		return false
	}
	if !end.IsZero() && createdAt.After(end) {
		return false
	}
	return true
}

func summarizeToolResults(results []resultmodel.ToolResult) string {
	for i := len(results) - 1; i >= 0; i-- {
		result := results[i].Normalized()
		for _, text := range []string{result.Summary, result.TranscriptText} {
			if trimmed := compactSummary(text); trimmed != "" && isUserFacingToolResultSummary(trimmed) {
				return trimmed
			}
		}
	}
	return ""
}

func isUserFacingToolResultSummary(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	switch {
	case strings.HasPrefix(text, "{"),
		strings.HasPrefix(text, "["),
		strings.HasPrefix(text, "<"),
		strings.HasPrefix(lower, "```"),
		strings.HasPrefix(lower, "error:"),
		strings.HasPrefix(lower, "curl "),
		strings.HasPrefix(lower, "cd "),
		strings.HasPrefix(lower, "$ "),
		strings.HasPrefix(lower, "<!doctype"),
		strings.HasPrefix(lower, "<html"):
		return false
	}
	for _, marker := range []string{
		`"command":`,
		`"stdout":`,
		`"stderr":`,
		`"exit_code":`,
		`"tool_name":`,
		`"tool_call_id":`,
		`"artifact_uri":`,
		`"tool_execution_error":`,
		`"content_type":`,
		`"url":`,
		`"html":`,
		`"selector":`,
		"&&",
	} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func collectResultActions(results []resultmodel.ToolResult) []resultmodel.ResultAction {
	if len(results) == 0 {
		return nil
	}
	out := make([]resultmodel.ResultAction, 0)
	seen := make(map[string]struct{})
	for _, result := range results {
		for _, action := range result.Actions {
			key := strings.TrimSpace(string(action.Kind)) + "|" + strings.TrimSpace(action.Label) + "|" + strings.TrimSpace(action.Target)
			if key == "||" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, action)
		}
	}
	return out
}

func mergeDeliverableRef(base, incoming DeliverableRef) DeliverableRef {
	base.Kind = normalize.FirstNonEmpty(base.Kind, incoming.Kind)
	base.Name = normalize.FirstNonEmpty(incoming.Name, base.Name)
	base.URI = normalize.FirstNonEmpty(base.URI, incoming.URI)
	base.ToolName = normalize.FirstNonEmpty(base.ToolName, incoming.ToolName)
	base.ContentType = normalize.FirstNonEmpty(incoming.ContentType, base.ContentType)
	if base.SizeBytes == 0 && incoming.SizeBytes > 0 {
		base.SizeBytes = incoming.SizeBytes
	}
	base.PreviewText = normalize.FirstNonEmpty(incoming.PreviewText, base.PreviewText)
	base.CreatedAt = normalize.FirstNonEmpty(incoming.CreatedAt, base.CreatedAt)
	base.Metadata = mergeMetadata(base.Metadata, incoming.Metadata)
	return base
}

func mergeMetadata(base, incoming map[string]any) map[string]any {
	if len(base) == 0 && len(incoming) == 0 {
		return nil
	}
	merged := cloneMetadata(base)
	if merged == nil {
		merged = make(map[string]any, len(incoming))
	}
	for key, value := range incoming {
		if _, ok := merged[key]; ok {
			continue
		}
		merged[key] = value
	}
	return merged
}

func cloneMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func deliverableNameFromURI(uri string, fallback string) string {
	name := strings.TrimSpace(fallback)
	if name != "" {
		return name
	}
	return deliveryAttachmentLabel(DeliverableRef{URI: uri})
}

func formatTimeRFC3339(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func (s *Service) previewArtifactText(ctx context.Context, blob *artifact.Blob) string {
	if s == nil || s.artifacts == nil || blob == nil {
		return ""
	}
	if blob.Size <= 0 || blob.Size > deliverablePreviewReadLimit {
		return ""
	}
	if !isPreviewableContentType(blob.ContentType) {
		return ""
	}
	body, _, err := s.artifacts.Read(ctx, blob.URI)
	if err != nil {
		return ""
	}
	return compactText(string(body), deliverablePreviewMaxLen)
}

func isPreviewableContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(contentType, "text/"):
		return true
	case strings.Contains(contentType, "json"):
		return true
	case strings.Contains(contentType, "xml"):
		return true
	case strings.Contains(contentType, "yaml"):
		return true
	case strings.Contains(contentType, "csv"):
		return true
	default:
		return false
	}
}
