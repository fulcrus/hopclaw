package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

// ErrRateLimited is returned when a session exceeds its submit rate limit.
var ErrRateLimited = errors.New("rate limited: too many requests for this session")

const metadataKeyAgent = "agent"

var (
	clarificationURLPattern      = regexp.MustCompile(`https?://[^\s]+`)
	clarificationEmailPattern    = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	clarificationPathPattern     = regexp.MustCompile(`(?:[A-Za-z]:\\[\w.\-\\]+|(?:\.\.?/|/|[A-Za-z0-9._-]+/)[\w.\-~/]+)`)
	clarificationResidualPattern = regexp.MustCompile(`[\s,，。.!！?？:：;；"'()\[\]{}<>]+`)
	clarificationCronPattern     = regexp.MustCompile(`^(?:@(?:yearly|annually|monthly|weekly|daily|hourly|reboot)|(?:[\d*/,\-]+\s+){4,5}[\d*/,\-]+)$`)
	clarificationAtPattern       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:[ T]\d{1,2}:\d{2}(?::\d{2})?)?$|^\d{1,2}:\d{2}(?::\d{2})?$`)
	clarificationChannelPattern  = regexp.MustCompile(`^(?:[#@][\p{L}\p{N}._/\-]+|[a-z0-9._-]+:[#@]?[\p{L}\p{N}._/\-]+)$`)
	clarificationHostPattern     = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+(?::\d{1,5})?(?:/[^\s]*)?$`)
	clarificationDeployIDPattern = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9._/\-]{1,63}$`)
	clarificationResidualTokens  = regexp.MustCompile(`[\p{L}\p{N}_-]+`)
)

type SubmitRequest struct {
	SessionID       string                       `json:"session_id,omitempty"` // optional: caller-specified session ID; empty = auto-generate
	SessionKey      string                       `json:"session_key"`
	ParentRunID     string                       `json:"parent_run_id,omitempty"`
	ExternalEventID string                       `json:"external_event_id,omitempty"`
	Content         string                       `json:"content"`
	Input           string                       `json:"input,omitempty"` // alias for content
	ContentBlocks   []contextengine.ContentBlock `json:"content_blocks,omitempty"`
	Images          []string                     `json:"images,omitempty"`
	Model           string                       `json:"model,omitempty"`
	AutomationID    string                       `json:"automation_id,omitempty"`
	Metadata        map[string]any               `json:"metadata,omitempty"`
	SemanticSignal  *agent.SemanticSignal        `json:"semantic_signal,omitempty"`
	Execute         *bool                        `json:"execute,omitempty"`
}

type submitOptions struct {
	skipRateLimit  bool
	skipAgentRoute bool
}

func (s *Service) Submit(ctx context.Context, req SubmitRequest) (*agent.Run, error) {
	return s.submit(ctx, req, submitOptions{})
}

func (s *Service) submit(ctx context.Context, req SubmitRequest, opts submitOptions) (*agent.Run, error) {
	if s.agent == nil {
		return nil, fmt.Errorf("agent component is required")
	}

	// Per-session rate limit check.
	if !opts.skipRateLimit && s.rateLimiter != nil && !s.rateLimiter.Allow(req.SessionKey) {
		return nil, ErrRateLimited
	}

	// Resolve agent profile from session key (e.g. "agent:sales:user123").
	sessionKey := req.SessionKey
	model := req.Model
	metadata := req.Metadata
	if !opts.skipAgentRoute {
		router := s.AgentRouter()
		if router != nil {
			agentName, innerKey, profile := router.Resolve(sessionKey)
			if profile != nil {
				sessionKey = innerKey
				if profile.Model != "" && model == "" {
					model = profile.Model
				}
				metadata = injectAgentProfileMetadata(metadata, profile, agentName)
			}
		}
	}
	scope := agent.ScopeFilter{}
	if waiting, session := s.findWaitingInputRun(ctx, sessionKey, scope); waiting != nil && session != nil {
		forceFollowUp := shouldForceWaitingInputFollowUp(req.Metadata, waiting.ID)
		merged, clarified, ok := mergeWaitingInputFollowUp(session, waiting, req.Content, forceFollowUp)
		if ok {
			if metadata == nil {
				metadata = make(map[string]any, 4)
			}
			metadata["preflight_followup_for_run"] = waiting.ID
			metadata["preflight_followup"] = true
			metadata[agent.MetadataKeyClarificationSourceRunID] = waiting.ID
			metadata[agent.MetadataKeyClarificationText] = strings.TrimSpace(req.Content)
			if len(clarified) > 0 {
				metadata[agent.MetadataKeyClarificationSlots] = clarified
			}
			req.Content = merged
			if strings.TrimSpace(req.ParentRunID) == "" {
				req.ParentRunID = waiting.ID
			}
			if err := s.supersedeWaitingInputRun(ctx, waiting); err != nil {
				return nil, err
			}
		}
	}
	req.ParentRunID = normalize.FirstNonEmpty(strings.TrimSpace(req.ParentRunID), inferredParentRunID(req.Metadata))

	run, err := s.agent.Submit(ctx, agent.IncomingMessage{
		SessionID:       req.SessionID,
		SessionKey:      sessionKey,
		ParentRunID:     strings.TrimSpace(req.ParentRunID),
		ExternalEventID: req.ExternalEventID,
		Content:         req.Content,
		ContentBlocks:   append([]contextengine.ContentBlock(nil), req.ContentBlocks...),
		Images:          append([]string(nil), req.Images...),
		Model:           model,
		AutomationID:    req.AutomationID,
		Metadata:        metadata,
		SemanticSignal:  agent.CloneSemanticSignal(req.SemanticSignal),
	})
	if err != nil {
		return nil, err
	}
	if err := s.bindEffectiveConfigSnapshot(ctx, run); err != nil {
		return nil, err
	}
	if err := s.captureSubmissionMemory(ctx, sessionKey, req.Content, run); err != nil {
		log.Warn("capture submission memory failed", "run_id", run.ID, "session_key", sessionKey, "error", err)
	}
	if (req.Execute == nil || *req.Execute) && s.shouldDispatchQueuedRunNow(ctx, run) {
		if err := s.dispatchRun(ctx, run.ID, false); err != nil {
			return nil, err
		}
	}
	return s.runs.Get(ctx, run.ID)
}

func inferredParentRunID(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, key := range []string{
		"parent_run_id",
		"verification_repair_for_run_id",
		"preflight_followup_for_run",
		agent.MetadataKeyClarificationSourceRunID,
	} {
		if value, ok := metadata[key]; ok {
			if runID := strings.TrimSpace(fmt.Sprint(value)); runID != "" && runID != "<nil>" {
				return runID
			}
		}
	}
	return ""
}

func (s *Service) findWaitingInputRun(ctx context.Context, sessionKey string, scope agent.ScopeFilter) (*agent.Run, *agent.Session) {
	if strings.TrimSpace(sessionKey) == "" {
		return nil, nil
	}
	sessionMeta, err := s.getSessionMetadataByKeyScoped(ctx, sessionKey, scope)
	if err != nil || sessionMeta == nil {
		return nil, nil
	}
	lister, ok := s.runs.(agent.RunLister)
	if !ok {
		return nil, nil
	}
	runs, err := lister.List(ctx, agent.RunListFilter{SessionID: sessionMeta.ID, Limit: 16})
	if err != nil {
		return nil, nil
	}
	for _, run := range runs {
		if run != nil && run.Status == agent.RunWaitingInput {
			session, getErr := s.getSessionByKeyScoped(ctx, sessionKey, scope)
			if getErr != nil || session == nil {
				return run, nil
			}
			return run, session
		}
	}
	return nil, nil
}

func (s *Service) getSessionMetadataByKeyScoped(ctx context.Context, sessionKey string, scope agent.ScopeFilter) (*agent.Session, error) {
	if s == nil || s.sessions == nil || strings.TrimSpace(sessionKey) == "" {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	return agent.LoadSessionMetadataByKey(ctx, s.sessions, sessionKey, scope)
}

func (s *Service) getSessionByKeyScoped(ctx context.Context, sessionKey string, scope agent.ScopeFilter) (*agent.Session, error) {
	if s == nil || s.sessions == nil || strings.TrimSpace(sessionKey) == "" {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	return agent.LoadSessionByKey(ctx, s.sessions, sessionKey, scope)
}

func mergeWaitingInputFollowUp(session *agent.Session, run *agent.Run, followUp string, force bool) (string, map[string]string, bool) {
	followUp = strings.TrimSpace(followUp)
	if session == nil || run == nil || followUp == "" {
		return "", nil, false
	}
	if !force && !shouldAutoMergeWaitingInputFollowUp(run, followUp) {
		return "", nil, false
	}
	original := strings.TrimSpace(waitingInputOriginalTask(session, run))
	if original == "" {
		return "", nil, false
	}
	clarified := extractClarificationSlots(run, followUp)
	var parts []string
	parts = append(parts,
		"The previous task was waiting for missing input. Continue the same task with the new clarification instead of starting from scratch.",
		"Original task: "+original,
		"User clarification: "+followUp,
	)
	if len(clarified) > 0 {
		parts = append(parts, "Resolved inputs:")
		for _, line := range formatClarificationSlotLines(clarified) {
			parts = append(parts, "- "+line)
		}
	}
	parts = append(parts, "If the task is still ambiguous, ask one concise follow-up question. Otherwise continue.")
	return strings.Join(parts, "\n"), clarified, true
}

func waitingInputOriginalTask(session *agent.Session, run *agent.Run) string {
	if session == nil || run == nil {
		return ""
	}
	if run.InputEventID != "" {
		for i := len(session.Messages) - 1; i >= 0; i-- {
			msg := session.Messages[i]
			if msg.Role != contextengine.RoleUser {
				continue
			}
			if eventID, _ := msg.Metadata["message_id"].(string); strings.TrimSpace(eventID) == strings.TrimSpace(run.InputEventID) {
				return msg.Content
			}
		}
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role == contextengine.RoleUser && strings.TrimSpace(msg.Content) != "" {
			return msg.Content
		}
	}
	return ""
}

func shouldAutoMergeWaitingInputFollowUp(run *agent.Run, followUp string) bool {
	followUp = strings.TrimSpace(followUp)
	if run == nil || followUp == "" {
		return false
	}
	if len(parseStructuredClarificationSlots(run, followUp)) > 0 {
		return true
	}
	return clarificationReplyContainsOnlyResolvedInputs(run, followUp)
}

func shouldForceWaitingInputFollowUp(metadata map[string]any, runID string) bool {
	if len(metadata) == 0 {
		return false
	}
	if enabled, _ := metadata["preflight_followup"].(bool); !enabled {
		return false
	}
	if runID == "" {
		return true
	}
	value, ok := metadata["preflight_followup_for_run"]
	if !ok {
		return true
	}
	return strings.TrimSpace(fmt.Sprint(value)) == strings.TrimSpace(runID)
}

func clarificationReplyContainsOnlyResolvedInputs(run *agent.Run, followUp string) bool {
	clarified := extractClarificationSlots(run, followUp)
	if len(clarified) == 0 {
		return false
	}

	residual := strings.ToLower(strings.TrimSpace(followUp))
	for _, value := range clarified {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		residual = strings.ReplaceAll(residual, strings.ToLower(value), " ")
	}
	for id := range clarified {
		for _, alias := range clarificationSlotAliases(run, id) {
			alias = strings.TrimSpace(alias)
			if alias == "" {
				continue
			}
			residual = strings.ReplaceAll(residual, strings.ToLower(alias), " ")
		}
	}
	residual = clarificationResidualPattern.ReplaceAllString(residual, "")
	if residual == "" {
		return true
	}
	tokens := clarificationResidualTokens.FindAllString(residual, -1)
	if len(tokens) != 1 {
		return false
	}
	return utf8.RuneCountInString(tokens[0]) <= 4
}

func (s *Service) supersedeWaitingInputRun(ctx context.Context, run *agent.Run) error {
	if run == nil {
		return nil
	}
	if s.agent == nil {
		return fmt.Errorf("agent component is required")
	}
	return s.agent.SupersedeWaitingInputRun(ctx, run.ID, agent.RunReasonClarificationSuperseded)
}

func (s *Service) ResumeRun(ctx context.Context, runID string) (*agent.Run, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	switch run.Status {
	case agent.RunCancelled:
		return nil, fmt.Errorf("%w: %s", agent.ErrRunCancelled, runID)
	case agent.RunCompleted, agent.RunFailed:
		return nil, fmt.Errorf("run %s is already %s", runID, run.Status)
	}
	if err := s.dispatchRun(ctx, runID, true); err != nil {
		return nil, err
	}
	return s.runs.Get(ctx, runID)
}

func extractClarificationSlots(run *agent.Run, followUp string) map[string]string {
	followUp = strings.TrimSpace(followUp)
	if run == nil || followUp == "" {
		return nil
	}
	out := parseStructuredClarificationSlots(run, followUp)
	appendSlot := func(id, value string) {
		id = strings.TrimSpace(id)
		value = strings.TrimSpace(value)
		if id == "" || value == "" {
			return
		}
		if out == nil {
			out = make(map[string]string)
		}
		out[id] = value
	}
	pending := pendingClarificationIDs(run)
	for _, id := range pending {
		if _, ok := out[id]; ok {
			continue
		}
		switch id {
		case "source_target":
			if value := extractSourceReference(followUp); value != "" {
				appendSlot(id, value)
			}
		case "delivery_target":
			if value := extractDeliveryTargetReference(followUp); value != "" {
				appendSlot(id, value)
			}
		case "schedule":
			if value := extractScheduleReference(followUp); value != "" {
				appendSlot(id, value)
			}
		case "deployment_target":
			if value := extractDeploymentTargetReference(followUp); value != "" {
				appendSlot(id, value)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func pendingClarificationIDs(run *agent.Run) []string {
	if run == nil {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, 4)
	if run.TaskContract != nil {
		for _, item := range run.TaskContract.MissingInfo {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	if len(out) > 0 {
		return out
	}
	if run.Preflight == nil {
		return nil
	}
	for _, check := range run.Preflight.Checks {
		id := strings.TrimSpace(check.ID)
		switch id {
		case "reference_gap":
			id = "source_target"
		case "source_target", "delivery_target", "schedule", "deployment_target":
		default:
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseStructuredClarificationSlots(run *agent.Run, text string) map[string]string {
	out := parseStructuredClarificationJSON(run, text)
	for id, value := range parseStructuredClarificationLines(run, text) {
		if out == nil {
			out = make(map[string]string)
		}
		out[id] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseStructuredClarificationJSON(run *agent.Run, text string) map[string]string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil
	}
	out := make(map[string]string)
	for key, value := range raw {
		id := clarificationSlotID(run, key)
		if id == "" || value == nil {
			continue
		}
		if trimmedValue := strings.TrimSpace(fmt.Sprint(value)); trimmedValue != "" {
			out[id] = trimmedValue
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseStructuredClarificationLines(run *agent.Run, text string) map[string]string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make(map[string]string)
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		normalized := strings.ReplaceAll(line, "：", ":")
		parts := strings.SplitN(normalized, ":", 2)
		if len(parts) != 2 {
			continue
		}
		label := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if value == "" {
			continue
		}
		if id := clarificationSlotID(run, label); id != "" {
			out[id] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func extractSourceReference(text string) string {
	text = strings.TrimSpace(text)
	for _, pattern := range []*regexp.Regexp{
		clarificationURLPattern,
		clarificationPathPattern,
		clarificationEmailPattern,
	} {
		if match := pattern.FindString(text); strings.TrimSpace(match) != "" {
			return strings.TrimSpace(match)
		}
	}
	if strings.Contains(text, "artifact://") || strings.Contains(strings.ToLower(text), "github.com/") {
		return text
	}
	return ""
}

func extractDeliveryTargetReference(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	switch {
	case clarificationEmailPattern.MatchString(text):
		return text
	case clarificationURLPattern.MatchString(text):
		return text
	case clarificationChannelPattern.MatchString(text):
		return text
	default:
		return ""
	}
}

func extractScheduleReference(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if _, err := time.Parse(time.RFC3339, text); err == nil {
		return text
	}
	if clarificationAtPattern.MatchString(text) || clarificationCronPattern.MatchString(text) {
		return text
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "every ") {
		if _, err := time.ParseDuration(strings.TrimSpace(text[len("every "):])); err == nil {
			return text
		}
	}
	return ""
}

func extractDeploymentTargetReference(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if clarificationURLPattern.MatchString(text) {
		return text
	}
	if clarificationHostPattern.MatchString(text) || clarificationDeployIDPattern.MatchString(text) {
		return text
	}
	return ""
}

func formatClarificationSlotLines(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	order := []string{"source_target", "delivery_target", "schedule", "deployment_target"}
	out := make([]string, 0, len(values))
	appendLine := func(id string) {
		if value := strings.TrimSpace(values[id]); value != "" {
			out = append(out, clarificationSlotTemplateLabel(id)+": "+value)
		}
	}
	for _, id := range order {
		appendLine(id)
	}
	for id, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || containsString(order, id) {
			continue
		}
		out = append(out, clarificationSlotTemplateLabel(id)+": "+value)
	}
	return out
}

func clarificationSlotTemplateLabel(id string) string {
	label := clarificationSlotLabel(id)
	if label == "" || label == id {
		return id
	}
	return id + " (" + label + ")"
}

func clarificationSlotLabel(id string) string {
	switch strings.TrimSpace(id) {
	case "source_target":
		return "Target"
	case "delivery_target":
		return "Destination"
	case "schedule":
		return "Schedule"
	case "deployment_target":
		return "Deployment target"
	default:
		return "Clarification"
	}
}

func clarificationSlotID(run *agent.Run, label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	if id := canonicalClarificationSlotID(label); id != "" {
		return id
	}
	normalized := normalizeClarificationLabel(label)
	if normalized == "" {
		return ""
	}
	for _, slot := range clarificationPendingSlots(run) {
		if normalizeClarificationLabel(slot.Label) == normalized {
			return slot.ID
		}
	}
	return ""
}

func canonicalClarificationSlotID(label string) string {
	lower := strings.ToLower(strings.TrimSpace(label))
	for _, id := range []string{"source_target", "delivery_target", "schedule", "deployment_target"} {
		if lower == id {
			return id
		}
		if strings.HasPrefix(lower, id+" ") || strings.HasPrefix(lower, id+"(") || strings.HasPrefix(lower, id+"[") {
			return id
		}
	}
	return ""
}

func clarificationPendingSlots(run *agent.Run) []agent.RunClarificationSlot {
	if run == nil {
		return nil
	}
	if run.Preflight != nil && len(run.Preflight.ClarificationSlots) > 0 {
		return append([]agent.RunClarificationSlot(nil), run.Preflight.ClarificationSlots...)
	}
	if run.TaskContract == nil || len(run.TaskContract.MissingInfo) == 0 {
		return nil
	}
	out := make([]agent.RunClarificationSlot, 0, len(run.TaskContract.MissingInfo))
	for _, item := range run.TaskContract.MissingInfo {
		out = append(out, agent.RunClarificationSlot{
			ID:        strings.TrimSpace(item.ID),
			Label:     strings.TrimSpace(item.Label),
			InputMode: strings.TrimSpace(item.InputMode),
		})
	}
	return out
}

func clarificationSlotAliases(run *agent.Run, id string) []string {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	aliases := []string{id, clarificationSlotTemplateLabel(id), clarificationSlotLabel(id)}
	for _, slot := range clarificationPendingSlots(run) {
		if strings.TrimSpace(slot.ID) != id {
			continue
		}
		aliases = append(aliases, slot.Label)
	}
	return aliases
}

func normalizeClarificationLabel(label string) string {
	label = strings.TrimSpace(strings.ToLower(label))
	if label == "" {
		return ""
	}
	replacer := strings.NewReplacer("：", ":", "(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ", "/", " ")
	label = replacer.Replace(label)
	return strings.Join(strings.Fields(label), " ")
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
