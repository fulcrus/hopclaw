package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	automationsetup "github.com/fulcrus/hopclaw/automation/setup"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/meta"
)

// ---------------------------------------------------------------------------
// Speech Act & Reply Act enums
// ---------------------------------------------------------------------------

// IncomingSpeechAct classifies what the user's message means in context.
type IncomingSpeechAct string

const (
	SpeechActCommand            IncomingSpeechAct = "command"
	SpeechActApprovalReply      IncomingSpeechAct = "approval_reply"
	SpeechActClarificationReply IncomingSpeechAct = "clarification_reply"
	SpeechActTaskFollowup       IncomingSpeechAct = "task_followup"
	SpeechActStatusQuery        IncomingSpeechAct = "status_query"
	SpeechActNewTask            IncomingSpeechAct = "new_task"
	SpeechActNegativeFeedback   IncomingSpeechAct = "negative_feedback"
	SpeechActCasualChat         IncomingSpeechAct = "casual_chat"
	SpeechActMetaQuestion       IncomingSpeechAct = "meta_question"
	SpeechActUnknown            IncomingSpeechAct = "unknown"
)

// ReplyAct describes how the system should respond.
type ReplyAct string

const (
	ReplyActChatReply           ReplyAct = "chat_reply"
	ReplyActActionAck           ReplyAct = "action_ack"
	ReplyActStatusReply         ReplyAct = "status_reply"
	ReplyActClarificationPrompt ReplyAct = "clarification_prompt"
	ReplyActResumeAck           ReplyAct = "resume_ack"
	ReplyActTaskAccept          ReplyAct = "task_accept"
	ReplyActTaskResult          ReplyAct = "task_result"
	ReplyActTaskFailure         ReplyAct = "task_failure"
)

// TargetScope indicates what the interaction targets.
type TargetScope string

const (
	TargetScopeActiveRun TargetScope = "active_run"
	TargetScopeNewRun    TargetScope = "new_run"
	TargetScopeSession   TargetScope = "session"
	TargetScopeNone      TargetScope = "none"
)

// TextControlCommand is the shared canonical set of text-only slash controls.
type TextControlCommand string

const (
	TextControlCommandStatus TextControlCommand = "status"
	TextControlCommandCancel TextControlCommand = "cancel"
	TextControlCommandBind   TextControlCommand = "bind"
	TextControlCommandUnbind TextControlCommand = "unbind"
)

// ---------------------------------------------------------------------------
// Request / Context / Decision / Result
// ---------------------------------------------------------------------------

// InteractionRequest is the normalized input for Interact.
type InteractionRequest struct {
	SessionKey      string                       `json:"session_key"`
	ParentRunID     string                       `json:"parent_run_id,omitempty"`
	Content         string                       `json:"content"`
	ExternalEventID string                       `json:"external_event_id,omitempty"`
	ContentBlocks   []contextengine.ContentBlock `json:"content_blocks,omitempty"`
	Images          []string                     `json:"images,omitempty"`
	Model           string                       `json:"model,omitempty"`
	AutomationID    string                       `json:"automation_id,omitempty"`
	Metadata        map[string]any               `json:"metadata,omitempty"`

	// Pre-parsed structured signals. Callers (bridge, UI) set these when they
	// detect unambiguous structured input so the runtime fast-path can skip
	// natural-language classification.
	StructuredCommand  *StructuredCommand  `json:"structured_command,omitempty"`
	StructuredApproval *StructuredApproval `json:"structured_approval,omitempty"`
}

// StructuredCommand carries a pre-parsed slash-command or button action.
type StructuredCommand struct {
	Kind  string `json:"kind"`             // "status", "cancel", "bind", "unbind", "retry"
	RunID string `json:"run_id,omitempty"` // optional source run for structured retry / rerun
}

// StructuredApproval carries a pre-parsed approval/deny action.
type StructuredApproval struct {
	Action string `json:"action"` // "approve", "deny", "always"
}

// InteractionContextSnapshot captures the session/run state at classification time.
type InteractionContextSnapshot struct {
	SessionID       string                  `json:"session_id,omitempty"`
	SessionState    InteractionSessionState `json:"session_state"`
	ActiveRunID     string                  `json:"active_run_id,omitempty"`
	ActiveRunStatus agent.RunStatus         `json:"active_run_status,omitempty"`
	ActiveRunPhase  agent.RunPhase          `json:"active_run_phase,omitempty"`
	WaitingApproval bool                    `json:"waiting_approval"`
	WaitingInput    bool                    `json:"waiting_input"`
	HasActiveRun    bool                    `json:"has_active_run"`
	PendingTicketID string                  `json:"pending_ticket_id,omitempty"`
}

// InteractionDecision is the output of classification + policy.
type InteractionDecision struct {
	SpeechAct   IncomingSpeechAct `json:"speech_act"`
	TargetScope TargetScope       `json:"target_scope"`
	ReplyAct    ReplyAct          `json:"reply_act"`
	Confidence  float64           `json:"confidence"`
	Reason      string            `json:"reason,omitempty"`
}

// InteractionResult is the final output of Interact.
type InteractionResult struct {
	Decision      InteractionDecision        `json:"decision"`
	Context       InteractionContextSnapshot `json:"context"`
	ReplyMessage  string                     `json:"reply_message,omitempty"`
	SetupContract *automationsetup.Contract  `json:"setup_contract,omitempty"`

	// Run is set when a run was created, resumed, cancelled, or is the active
	// run for status queries.
	Run *agent.Run `json:"run,omitempty"`

	// SubmitRequest is set (along with Run) when the decision is task_accept,
	// so the caller has the full request that was used for submission.
	SubmitRequest *SubmitRequest `json:"submit_request,omitempty"`

	// ApprovalResolved is true when an approval ticket was resolved.
	ApprovalResolved bool `json:"approval_resolved,omitempty"`

	// ApprovalStatus records the resolved approval status when ApprovalResolved is true.
	ApprovalStatus approval.Status `json:"approval_status,omitempty"`

	// RunCancelled is true when a run was cancelled via command.
	RunCancelled bool `json:"run_cancelled,omitempty"`

	// SteerEnqueued is true when a steering directive was pushed.
	SteerEnqueued bool `json:"steer_enqueued,omitempty"`

	// Error detail for task_failure results.
	Error string `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// InteractionClassifier — semantic classification interface
// ---------------------------------------------------------------------------

// InteractionClassifyRequest is the input for semantic classification.
type InteractionClassifyRequest struct {
	SessionKey      string                  `json:"session_key"`
	Message         string                  `json:"message"`
	Model           string                  `json:"model,omitempty"`
	SessionState    InteractionSessionState `json:"session_state,omitempty"`
	ActiveRun       *agent.Run              `json:"active_run,omitempty"`
	PendingApproval *approval.Ticket        `json:"pending_approval,omitempty"`
	WaitingInput    bool                    `json:"waiting_input"`
	WaitingApproval bool                    `json:"waiting_approval"`
	RecentMessages  []contextengine.Message `json:"recent_messages,omitempty"`
}

// InteractionClassifier classifies ambiguous user messages using semantic
// analysis (typically LLM-based). Implementations must be safe for concurrent use.
type InteractionClassifier interface {
	Classify(ctx context.Context, req InteractionClassifyRequest) (InteractionDecision, error)
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	interactRecentMessageLimit = 4
	interactClassifierTimeout  = 4 * time.Second
	defaultMinActionConfidence = 0.4
)

// ---------------------------------------------------------------------------
// Interact — unified entry point
// ---------------------------------------------------------------------------

// Interact interprets a user message in context and returns a classified,
// policy-checked result. For decisions that require runtime side-effects
// (submit, approve, cancel, steer), those effects are executed before
// returning. Callers handle delivery/notification based on the result.
func (s *Service) Interact(ctx context.Context, req InteractionRequest) (*InteractionResult, error) {
	content := effectiveInteractionContent(req.Content, req.ContentBlocks)
	if content == "" && len(req.ContentBlocks) == 0 && len(req.Images) == 0 && req.StructuredCommand == nil && req.StructuredApproval == nil {
		return nil, fmt.Errorf("interaction content, content_blocks, images, or structured action is required")
	}

	// Rate limit check (same as Submit).
	if s.rateLimiter != nil && !s.rateLimiter.Allow(req.SessionKey) {
		return nil, ErrRateLimited
	}

	// Resolve agent profile.
	sessionKey := req.SessionKey
	model := req.Model
	metadata := req.Metadata
	if router := s.AgentRouter(); router != nil {
		agentName, innerKey, profile := router.Resolve(sessionKey)
		if profile != nil {
			sessionKey = innerKey
			if profile.Model != "" && model == "" {
				model = profile.Model
			}
			metadata = injectAgentProfileMetadata(metadata, profile, agentName)
		}
	}

	scope := agent.ScopeFilter{}.Normalize()

	// --- Step 1: Hydrate context ---
	snapshot := s.hydrateInteractionContext(ctx, sessionKey, scope)

	// --- Step 2: Deterministic fast path ---
	if decision, ok := classifyDeterministic(req, snapshot); ok {
		result := &InteractionResult{Decision: decision, Context: snapshot}
		s.executeInteraction(ctx, result, req, nil, sessionKey, content, model, metadata)
		return result, nil
	}

	// --- Step 3: Unified semantic ingress ---
	if s.ingressClassifier != nil {
		classification, err := s.classifyIngress(ctx, req, content, snapshot, sessionKey, model)
		if err != nil {
			log.Warn("interaction ingress unavailable, falling back to no-run conversation", "error", err, "session_key", sessionKey)
			result := &InteractionResult{
				Decision: ingressUnavailableDecision(snapshot),
				Context:  snapshot,
			}
			s.executeInteraction(ctx, result, req, nil, sessionKey, content, model, metadata)
			return result, nil
		}
		if result, handled := s.executeAutomationIntentPlan(ctx, req, snapshot, sessionKey, model, metadata, classification.AutomationPlan); handled {
			return result, nil
		}
		decision := applyInteractionPolicyWithThreshold(classification.Decision, snapshot, s.minActionConfidence)
		result := &InteractionResult{Decision: decision, Context: snapshot}
		s.executeInteraction(ctx, result, req, classification.SemanticSignal, sessionKey, content, model, metadata)
		return result, nil
	}

	// --- Step 4: Legacy compatibility path ---
	if result, ok := s.tryAutomationIntent(ctx, req, content, snapshot, sessionKey, model, metadata); ok {
		return result, nil
	}

	// --- Step 5: Legacy semantic classifier ---
	decision := s.classifySemantic(ctx, req, content, snapshot, sessionKey, model)

	// --- Step 6: Policy check ---
	decision = applyInteractionPolicyWithThreshold(decision, snapshot, s.minActionConfidence)

	// --- Step 7: Execute ---
	result := &InteractionResult{Decision: decision, Context: snapshot}
	s.executeInteraction(ctx, result, req, nil, sessionKey, content, model, metadata)
	return result, nil
}

// ---------------------------------------------------------------------------
// Context hydration
// ---------------------------------------------------------------------------

func (s *Service) hydrateInteractionContext(ctx context.Context, sessionKey string, scope agent.ScopeFilter) InteractionContextSnapshot {
	snap := InteractionContextSnapshot{SessionState: InteractionSessionStateIdle}

	if strings.TrimSpace(sessionKey) == "" {
		return snap
	}
	session, err := s.getSessionMetadataByKeyScoped(ctx, sessionKey, scope)
	if err != nil || session == nil {
		return snap
	}
	snap.SessionID = session.ID

	// Find active run.
	lister, ok := s.runs.(agent.RunLister)
	if !ok {
		return snap
	}
	runs, err := lister.List(ctx, agent.RunListFilter{SessionID: session.ID, Limit: 16})
	if err != nil {
		return snap
	}

	for _, run := range runs {
		if run == nil {
			continue
		}
		switch run.Status {
		case agent.RunRunning, agent.RunStreaming:
			snap.HasActiveRun = true
			snap.ActiveRunID = run.ID
			snap.ActiveRunStatus = run.Status
			snap.ActiveRunPhase = run.Phase
			snap.SessionState = InteractionSessionStateRunning
		case agent.RunWaitingApproval:
			snap.HasActiveRun = true
			snap.ActiveRunID = run.ID
			snap.ActiveRunStatus = run.Status
			snap.ActiveRunPhase = run.Phase
			snap.WaitingApproval = true
			snap.SessionState = InteractionSessionStateWaitingApproval
			if run.ApprovalID != "" {
				snap.PendingTicketID = run.ApprovalID
			}
		case agent.RunWaitingInput:
			snap.HasActiveRun = true
			snap.ActiveRunID = run.ID
			snap.ActiveRunStatus = run.Status
			snap.ActiveRunPhase = run.Phase
			snap.WaitingInput = true
			snap.SessionState = InteractionSessionStateWaitingInput
		case agent.RunQueued:
			if !snap.HasActiveRun {
				snap.HasActiveRun = true
				snap.ActiveRunID = run.ID
				snap.ActiveRunStatus = run.Status
				snap.ActiveRunPhase = run.Phase
				snap.SessionState = InteractionSessionStateRunning
			}
		}
		// First match of a higher-priority state wins.
		if snap.SessionState == InteractionSessionStateRunning || snap.SessionState == InteractionSessionStateWaitingApproval || snap.SessionState == InteractionSessionStateWaitingInput {
			break
		}
	}

	// Check for recently completed/failed if still idle.
	if snap.SessionState == InteractionSessionStateIdle && len(runs) > 0 {
		for _, run := range runs {
			if run == nil {
				continue
			}
			age := time.Since(run.FinishedAt)
			switch run.Status {
			case agent.RunCompleted:
				if age < 5*time.Minute {
					snap.SessionState = InteractionSessionStateCompletedRecently
					snap.ActiveRunID = run.ID
					snap.ActiveRunStatus = run.Status
				}
			case agent.RunFailed:
				if age < 5*time.Minute {
					snap.SessionState = InteractionSessionStateFailedRecently
					snap.ActiveRunID = run.ID
					snap.ActiveRunStatus = run.Status
				}
			}
			break // only check the most recent
		}
	}

	// Hydrate pending approval ticket ID if waiting.
	if snap.WaitingApproval && snap.PendingTicketID == "" && s.approvals != nil {
		if ticket, err := s.FindPendingApproval(ctx, session.ID); err == nil && ticket != nil {
			snap.PendingTicketID = ticket.ID
		}
	}

	return snap
}

// ---------------------------------------------------------------------------
// Deterministic fast path
// ---------------------------------------------------------------------------

func classifyDeterministic(req InteractionRequest, snap InteractionContextSnapshot) (InteractionDecision, bool) {
	// Priority 1: Pre-parsed structured approval.
	if req.StructuredApproval != nil {
		action := normalizeStructuredApprovalAction(req.StructuredApproval.Action)
		if action == "" {
			action = "approve"
		}
		if snap.WaitingApproval {
			return InteractionDecision{
				SpeechAct:   SpeechActApprovalReply,
				TargetScope: TargetScopeActiveRun,
				ReplyAct:    ReplyActResumeAck,
				Confidence:  1.0,
				Reason:      "structured_approval_" + action,
			}, true
		}
		// Approval reply but nothing waiting — downgrade to chat.
		return InteractionDecision{
			SpeechAct:   SpeechActApprovalReply,
			TargetScope: TargetScopeNone,
			ReplyAct:    ReplyActChatReply,
			Confidence:  0.8,
			Reason:      "approval_reply_no_pending",
		}, true
	}

	// Priority 2: Pre-parsed structured command.
	if req.StructuredCommand != nil {
		switch req.StructuredCommand.Kind {
		case "status":
			return InteractionDecision{
				SpeechAct:   SpeechActStatusQuery,
				TargetScope: TargetScopeSession,
				ReplyAct:    ReplyActStatusReply,
				Confidence:  1.0,
				Reason:      "structured_command_status",
			}, true
		case "cancel":
			return InteractionDecision{
				SpeechAct:   SpeechActCommand,
				TargetScope: TargetScopeActiveRun,
				ReplyAct:    ReplyActActionAck,
				Confidence:  1.0,
				Reason:      "structured_command_cancel",
			}, true
		case "retry":
			return InteractionDecision{
				SpeechAct:   SpeechActNewTask,
				TargetScope: TargetScopeNewRun,
				ReplyAct:    ReplyActTaskAccept,
				Confidence:  1.0,
				Reason:      "structured_command_retry",
			}, true
		default:
			return InteractionDecision{
				SpeechAct:   SpeechActCommand,
				TargetScope: TargetScopeSession,
				ReplyAct:    ReplyActActionAck,
				Confidence:  1.0,
				Reason:      "structured_command_" + req.StructuredCommand.Kind,
			}, true
		}
	}

	// Priority 3: Text-based approval reply when waiting.
	if snap.WaitingApproval {
		if action := parseApprovalSelection(req.Content); action != "" {
			return InteractionDecision{
				SpeechAct:   SpeechActApprovalReply,
				TargetScope: TargetScopeActiveRun,
				ReplyAct:    ReplyActResumeAck,
				Confidence:  1.0,
				Reason:      "numbered_approval_" + action,
			}, true
		}
		if action := parseApprovalText(req.Content); action != "" {
			return InteractionDecision{
				SpeechAct:   SpeechActApprovalReply,
				TargetScope: TargetScopeActiveRun,
				ReplyAct:    ReplyActResumeAck,
				Confidence:  1.0,
				Reason:      "deprecated_text_approval_" + action,
			}, true
		}
	}

	// Priority 4: Text-based slash command.
	if cmd := parseCommandText(req.Content); cmd != "" {
		switch cmd {
		case "status":
			return InteractionDecision{
				SpeechAct:   SpeechActStatusQuery,
				TargetScope: TargetScopeSession,
				ReplyAct:    ReplyActStatusReply,
				Confidence:  1.0,
				Reason:      "text_command_status",
			}, true
		case "cancel":
			return InteractionDecision{
				SpeechAct:   SpeechActCommand,
				TargetScope: TargetScopeActiveRun,
				ReplyAct:    ReplyActActionAck,
				Confidence:  1.0,
				Reason:      "text_command_cancel",
			}, true
		default:
			return InteractionDecision{
				SpeechAct:   SpeechActCommand,
				TargetScope: TargetScopeSession,
				ReplyAct:    ReplyActActionAck,
				Confidence:  1.0,
				Reason:      "text_command_" + cmd,
			}, true
		}
	}

	// Priority 5: Clarification reply when waiting for input.
	if snap.WaitingInput {
		return InteractionDecision{
			SpeechAct:   SpeechActClarificationReply,
			TargetScope: TargetScopeActiveRun,
			ReplyAct:    ReplyActResumeAck,
			Confidence:  0.9,
			Reason:      "waiting_input_followup",
		}, true
	}

	// Priority 6: Attachment-only submissions are unambiguous new work even
	// when the caller omits plain text and relies on content_blocks.
	if strings.TrimSpace(req.Content) == "" && (len(req.Images) > 0 || hasAttachmentContentBlocks(req.ContentBlocks)) {
		return InteractionDecision{
			SpeechAct:   SpeechActNewTask,
			TargetScope: TargetScopeNewRun,
			ReplyAct:    ReplyActTaskAccept,
			Confidence:  1.0,
			Reason:      "attachment_only_prompt",
		}, true
	}

	// Everything else defers to semantic classification or default fallback.
	return InteractionDecision{}, false
}

func effectiveInteractionContent(content string, blocks []contextengine.ContentBlock) string {
	if trimmed := strings.TrimSpace(content); trimmed != "" {
		return trimmed
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != contextengine.ContentBlockText {
			continue
		}
		if text := strings.TrimSpace(block.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func hasAttachmentContentBlocks(blocks []contextengine.ContentBlock) bool {
	for _, block := range blocks {
		switch block.Type {
		case contextengine.ContentBlockText:
			continue
		case contextengine.ContentBlockImage, contextengine.ContentBlockFile:
			return true
		default:
			if strings.TrimSpace(string(block.Type)) != "" {
				return true
			}
		}
	}
	return false
}

// parseApprovalSelection matches numbered approval replies:
// 1=approve, 2=deny, 3=always.
func parseApprovalSelection(text string) string {
	switch strings.TrimSpace(text) {
	case "1":
		return "approve"
	case "2":
		return "deny"
	case "3":
		return "always"
	default:
		return ""
	}
}

// parseApprovalText is a deprecated fallback for legacy natural-language
// approval replies. Structured callbacks and numbered replies are the primary
// approval path.
func parseApprovalText(text string) string {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "y", "yes":
		return "approve"
	case "n", "no":
		return "deny"
	case "a", "always":
		return "always"
	default:
		return ""
	}
}

func normalizeStructuredApprovalAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "approve", "deny", "always":
		return strings.ToLower(strings.TrimSpace(action))
	default:
		return ""
	}
}

// parseCommandText returns the command kind or "" if the text is not a
// recognisable slash command. Matches canonical English command identifiers.
// No non-English aliases — this is an international product.
func parseCommandText(text string) string {
	if cmd, ok := ParseTextControlCommand(text); ok {
		return string(cmd)
	}
	return ""
}

// ParseTextControlCommand matches the shared canonical text control commands.
func ParseTextControlCommand(text string) (TextControlCommand, bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	switch {
	case lower == "/status" || lower == "/progress":
		return TextControlCommandStatus, true
	case lower == "/cancel" || lower == "/abort":
		return TextControlCommandCancel, true
	case lower == "/bind" || strings.HasPrefix(lower, "/bind "):
		return TextControlCommandBind, true
	case lower == "/unbind":
		return TextControlCommandUnbind, true
	default:
		return "", false
	}
}

// ---------------------------------------------------------------------------
// Semantic classifier fallback
// ---------------------------------------------------------------------------

func (s *Service) classifySemantic(ctx context.Context, req InteractionRequest, message string, snap InteractionContextSnapshot, sessionKey, model string) InteractionDecision {
	if s.classifier == nil {
		// No classifier available — default based on context.
		return defaultClassification(req, snap)
	}

	classifyReq := InteractionClassifyRequest{
		SessionKey:      sessionKey,
		Message:         message,
		Model:           model,
		SessionState:    snap.SessionState,
		WaitingInput:    snap.WaitingInput,
		WaitingApproval: snap.WaitingApproval,
	}

	// Load active run for the classifier.
	if snap.ActiveRunID != "" {
		if run, err := s.runs.Get(ctx, snap.ActiveRunID); err == nil {
			classifyReq.ActiveRun = run
		}
	}

	// Load pending approval ticket.
	if snap.PendingTicketID != "" && s.approvals != nil {
		if ticket, err := s.approvals.Get(ctx, snap.PendingTicketID); err == nil {
			classifyReq.PendingApproval = ticket
		}
	}

	// Load recent messages.
	if snap.SessionID != "" {
		classifyReq.RecentMessages = s.loadRecentMessages(ctx, snap.SessionID, interactRecentMessageLimit)
	}

	classifyCtx, cancel := context.WithTimeout(ctx, interactClassifierTimeout)
	defer cancel()

	decision, err := s.classifier.Classify(classifyCtx, classifyReq)
	if err != nil {
		log.Warn("interaction classifier failed, using default", "error", err, "session_key", sessionKey)
		return defaultClassification(req, snap)
	}
	if decision.SpeechAct == "" || decision.ReplyAct == "" {
		return defaultClassification(req, snap)
	}
	return decision
}

func defaultClassification(req InteractionRequest, snap InteractionContextSnapshot) InteractionDecision {
	if len(req.Images) > 0 || hasAttachmentContentBlocks(req.ContentBlocks) {
		return InteractionDecision{
			SpeechAct:   SpeechActNewTask,
			TargetScope: TargetScopeNewRun,
			ReplyAct:    ReplyActTaskAccept,
			Confidence:  0.6,
			Reason:      "default_attachment_prompt",
		}
	}
	if snap.HasActiveRun {
		return InteractionDecision{
			SpeechAct:   SpeechActUnknown,
			TargetScope: TargetScopeNone,
			ReplyAct:    ReplyActClarificationPrompt,
			Confidence:  0.1,
			Reason:      "default_ambiguous_active_run",
		}
	}
	return InteractionDecision{
		SpeechAct:   SpeechActUnknown,
		TargetScope: TargetScopeNone,
		ReplyAct:    ReplyActClarificationPrompt,
		Confidence:  0.1,
		Reason:      "default_ambiguous_idle",
	}
}

func ingressUnavailableDecision(snap InteractionContextSnapshot) InteractionDecision {
	if snap.WaitingInput {
		return InteractionDecision{
			SpeechAct:   SpeechActClarificationReply,
			TargetScope: TargetScopeNone,
			ReplyAct:    ReplyActClarificationPrompt,
			Confidence:  0,
			Reason:      "ingress_unavailable_waiting_input",
		}
	}
	return InteractionDecision{
		SpeechAct:   SpeechActUnknown,
		TargetScope: TargetScopeNone,
		ReplyAct:    ReplyActChatReply,
		Confidence:  0,
		Reason:      "ingress_unavailable",
	}
}

func (s *Service) loadRecentMessages(ctx context.Context, sessionID string, limit int) []contextengine.Message {
	messages, err := agent.LoadRecentMessages(ctx, s.sessions, sessionID, limit)
	if err != nil || len(messages) == 0 {
		return nil
	}
	return filterRecentMessages(messages, limit)
}

func filterRecentMessages(messages []contextengine.Message, limit int) []contextengine.Message {
	out := make([]contextengine.Message, 0, limit)
	for i := len(messages) - 1; i >= 0 && len(out) < limit; i-- {
		msg := messages[i]
		if msg.Role != contextengine.RoleUser && msg.Role != contextengine.RoleAssistant {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if msg.Role == contextengine.RoleUser {
			if parseApprovalText(content) != "" {
				continue
			}
			if parseCommandText(content) != "" {
				continue
			}
		}
		out = append(out, contextengine.Message{
			Role:    msg.Role,
			Content: content,
		})
	}
	// Reverse to chronological order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// ---------------------------------------------------------------------------
// Policy check
// ---------------------------------------------------------------------------

func applyInteractionPolicy(decision InteractionDecision, snap InteractionContextSnapshot) InteractionDecision {
	return applyInteractionPolicyWithThreshold(decision, snap, 0)
}

func applyInteractionPolicyWithThreshold(decision InteractionDecision, snap InteractionContextSnapshot, minActionConfidence float64) InteractionDecision {
	// Rule: approval reply requires pending approval.
	if decision.SpeechAct == SpeechActApprovalReply && !snap.WaitingApproval {
		decision.ReplyAct = ReplyActChatReply
		decision.TargetScope = TargetScopeNone
		decision.Reason = "approval_reply_no_pending"
	}

	// Rule: clarification reply requires waiting input.
	if decision.SpeechAct == SpeechActClarificationReply && !snap.WaitingInput {
		decision.SpeechAct = SpeechActNewTask
		decision.ReplyAct = ReplyActTaskAccept
		decision.TargetScope = TargetScopeNewRun
		decision.Reason = "clarification_no_waiting_input"
	}

	// Rule: status query never creates a run.
	if decision.SpeechAct == SpeechActStatusQuery {
		decision.ReplyAct = ReplyActStatusReply
		decision.TargetScope = TargetScopeSession
	}

	// Rule: casual chat / meta question / negative feedback → chat reply, no run.
	switch decision.SpeechAct {
	case SpeechActCasualChat, SpeechActMetaQuestion:
		decision.ReplyAct = ReplyActChatReply
		decision.TargetScope = TargetScopeNone
	case SpeechActNegativeFeedback:
		if snap.HasActiveRun {
			decision.SpeechAct = SpeechActTaskFollowup
			decision.ReplyAct = ReplyActResumeAck
			decision.TargetScope = TargetScopeActiveRun
		} else {
			decision.ReplyAct = ReplyActChatReply
			decision.TargetScope = TargetScopeNone
		}
	}

	// Rule: task followup with no active run → new task.
	if decision.SpeechAct == SpeechActTaskFollowup && !snap.HasActiveRun {
		decision.SpeechAct = SpeechActNewTask
		decision.ReplyAct = ReplyActTaskAccept
		decision.TargetScope = TargetScopeNewRun
	}

	if requiresHighConfidenceAction(decision) && decision.Confidence < normalizeMinActionConfidence(minActionConfidence) {
		decision.ReplyAct = ReplyActClarificationPrompt
		decision.TargetScope = TargetScopeNone
		decision.Reason = "low_confidence_action"
	}

	return decision
}

func normalizeMinActionConfidence(threshold float64) float64 {
	switch {
	case threshold < 0:
		return 0
	case threshold > 1:
		return 1
	default:
		return threshold
	}
}

func requiresHighConfidenceAction(decision InteractionDecision) bool {
	switch decision.ReplyAct {
	case ReplyActTaskAccept, ReplyActResumeAck, ReplyActActionAck:
		return true
	default:
		return false
	}
}

func defaultInteractionClarificationMessage(input string) string {
	if interactionLooksChinese(input) {
		return "我还不够确定，不会直接执行这个操作。请明确告诉我你要执行的动作、目标对象和预期结果，我确认后再继续。"
	}
	return "I am not confident enough to execute that action yet. Please confirm the exact action, target, and expected outcome, and I will continue after that."
}

func interactionConversationSystemPrompt(decision InteractionDecision, snap InteractionContextSnapshot) string {
	lines := []string{
		"<interaction_advisory>",
		"Runtime advisory only. The model should produce the actual user-facing reply for this turn.",
		"Do not claim that tools ran, that a tracked run was created, or that background execution already started.",
	}
	switch decision.ReplyAct {
	case ReplyActClarificationPrompt:
		lines = append(lines,
			"This turn is in the no-run clarification envelope.",
			"If the user can be answered directly without execution, answer directly.",
			"Otherwise ask only the smallest clarification needed to proceed safely.",
		)
	default:
		lines = append(lines,
			"This turn is in the no-run conversational envelope.",
			"Answer directly in natural language.",
			"If the request would require execution or tools, explain what is missing or what execution would be needed instead of pretending it already happened.",
		)
	}
	if special := interactionAdvisoryReasonInstruction(decision.Reason); special != "" {
		lines = append(lines, special)
	}
	lines = append(lines,
		"Advisory classification:",
		"- speech_act: "+string(decision.SpeechAct),
		"- reply_act: "+string(decision.ReplyAct),
	)
	if reason := strings.TrimSpace(decision.Reason); reason != "" {
		lines = append(lines, "- reason: "+reason)
	}
	if state := interactionStateSummary(snap); state != "" {
		lines = append(lines, "Current session state:", state)
	}
	lines = append(lines, "</interaction_advisory>")
	return strings.Join(lines, "\n")
}

func interactionAdvisoryReasonInstruction(reason string) string {
	switch strings.TrimSpace(reason) {
	case "approval_reply_no_pending":
		return "The user's input resembles an approval response, but there is no pending approval right now. Explain that clearly."
	case "low_confidence_action":
		return "The runtime advisory path was not confident enough to execute a side-effectful task automatically."
	case "default_ambiguous_active_run":
		return "There is already active work in this session. If the user is changing or questioning that work, respond in that context."
	case "ingress_unavailable", "ingress_unavailable_waiting_input":
		return "Routing is unavailable for this turn. Reply helpfully in natural language, but do not imply that execution or tool use has already started. If the request needs execution, explain that the task could not be started in this turn and ask the user to retry."
	default:
		return ""
	}
}

func interactionStateSummary(snap InteractionContextSnapshot) string {
	lines := make([]string, 0, 5)
	if state := strings.TrimSpace(snap.SessionState.String()); state != "" {
		lines = append(lines, "- session_state: "+state)
	}
	if snap.HasActiveRun {
		lines = append(lines, "- has_active_run: true")
	}
	if runID := strings.TrimSpace(snap.ActiveRunID); runID != "" {
		lines = append(lines, "- active_run_id: "+runID)
	}
	if status := strings.TrimSpace(string(snap.ActiveRunStatus)); status != "" {
		lines = append(lines, "- active_run_status: "+status)
	}
	if phase := strings.TrimSpace(string(snap.ActiveRunPhase)); phase != "" {
		lines = append(lines, "- active_run_phase: "+phase)
	}
	if snap.WaitingApproval {
		lines = append(lines, "- waiting_approval: true")
	}
	if snap.WaitingInput {
		lines = append(lines, "- waiting_input: true")
	}
	return strings.Join(lines, "\n")
}

func interactionLooksChinese(text string) bool {
	for _, r := range strings.TrimSpace(text) {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Execution
// ---------------------------------------------------------------------------

func (s *Service) executeInteraction(ctx context.Context, result *InteractionResult, req InteractionRequest, semantic *agent.SemanticSignal, sessionKey, content, model string, metadata map[string]any) {
	switch result.Decision.ReplyAct {
	case ReplyActTaskAccept:
		s.executeTaskAccept(ctx, result, req, semantic, sessionKey, content, model, metadata)
	case ReplyActResumeAck:
		s.executeResumeAck(ctx, result, req, semantic, sessionKey, content, model, metadata)
	case ReplyActActionAck:
		s.executeActionAck(ctx, result, sessionKey)
	case ReplyActStatusReply:
		s.executeStatusReply(ctx, result)
	case ReplyActChatReply, ReplyActClarificationPrompt:
		s.executeConversationReply(ctx, result, req, sessionKey, model, metadata)
	case ReplyActTaskResult, ReplyActTaskFailure:
		// No runtime side-effect. Caller handles delivery.
	}
}

func (s *Service) executeConversationReply(
	ctx context.Context,
	result *InteractionResult,
	req InteractionRequest,
	sessionKey, model string,
	metadata map[string]any,
) {
	if s == nil || result == nil {
		return
	}
	if s.agent == nil {
		result.Decision.ReplyAct = ReplyActTaskFailure
		result.Error = "agent component is required"
		return
	}

	mode := agent.ConversationTurnChat
	if result.Decision.ReplyAct == ReplyActClarificationPrompt {
		mode = agent.ConversationTurnClarification
	}
	turn, err := s.agent.ConversationTurn(ctx, agent.ConversationTurnRequest{
		SessionKey:             sessionKey,
		Content:                req.Content,
		ContentBlocks:          append([]contextengine.ContentBlock(nil), req.ContentBlocks...),
		Images:                 append([]string(nil), req.Images...),
		Model:                  model,
		Metadata:               cloneMetadata(metadata),
		Mode:                   mode,
		AdditionalSystemPrompt: interactionConversationSystemPrompt(result.Decision, result.Context),
	})
	if err != nil {
		result.ReplyMessage = "I encountered an issue processing your message. Please try again."
		result.Decision.ReplyAct = ReplyActTaskFailure
		result.Error = err.Error()
		return
	}
	if turn == nil {
		result.Decision.ReplyAct = ReplyActTaskFailure
		result.Error = "conversation turn returned nil result"
		return
	}
	result.Context.SessionID = firstNonEmpty(result.Context.SessionID, turn.SessionID)
	result.ReplyMessage = strings.TrimSpace(turn.Message.Content)
}

func (s *Service) submitInteractionRun(
	ctx context.Context,
	req InteractionRequest,
	semantic *agent.SemanticSignal,
	sessionKey, content, model string,
	metadata map[string]any,
) (*agent.Run, *SubmitRequest, error) {
	parentRunID := req.ParentRunID
	contentBlocks := append([]contextengine.ContentBlock(nil), req.ContentBlocks...)
	images := append([]string(nil), req.Images...)
	metadata = cloneMetadata(metadata)

	if req.StructuredCommand != nil && strings.EqualFold(strings.TrimSpace(req.StructuredCommand.Kind), "retry") {
		sourceRunID := strings.TrimSpace(req.StructuredCommand.RunID)
		if sourceRunID == "" {
			sourceRunID = strings.TrimSpace(req.ParentRunID)
		}
		sourceRun, sourceInput, err := s.loadRetrySource(ctx, sourceRunID)
		if err != nil {
			return nil, nil, err
		}
		parentRunID = sourceRun.ID
		content = sourceInput.Content
		contentBlocks = append([]contextengine.ContentBlock(nil), sourceInput.ContentBlocks...)
		images = nil
		if strings.TrimSpace(model) == "" {
			model = strings.TrimSpace(sourceRun.Model)
		}
		if metadata == nil {
			metadata = make(map[string]any, 3)
		}
		metadata["retry_run_id"] = sourceRun.ID
		metadata["retry_source"] = "structured_command"
		metadata["retry_source_status"] = string(sourceRun.Status)
	}

	submitReq := SubmitRequest{
		SessionKey:      sessionKey,
		ParentRunID:     parentRunID,
		ExternalEventID: req.ExternalEventID,
		Content:         content,
		ContentBlocks:   contentBlocks,
		Images:          images,
		Model:           model,
		AutomationID:    req.AutomationID,
		Metadata:        metadata,
		SemanticSignal:  agent.CloneSemanticSignal(semantic),
	}
	run, err := s.submit(ctx, submitReq, submitOptions{
		skipRateLimit:  true,
		skipAgentRoute: true,
	})
	if err != nil {
		return nil, &submitReq, err
	}
	return run, &submitReq, nil
}

func (s *Service) loadRetrySource(ctx context.Context, runID string) (*agent.Run, *contextengine.Message, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, nil, fmt.Errorf("retry run id is required")
	}
	if s == nil || s.runs == nil {
		return nil, nil, fmt.Errorf("run store is not configured")
	}
	if s.sessions == nil {
		return nil, nil, fmt.Errorf("session store is not configured")
	}
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	switch run.Status {
	case agent.RunQueued, agent.RunRunning, agent.RunStreaming, agent.RunWaitingApproval, agent.RunWaitingInput:
		return nil, nil, fmt.Errorf("run %s is not retryable while %s", run.ID, run.Status)
	}
	session, err := agent.LoadSession(ctx, s.sessions, run.SessionID, agent.ScopeFilter{})
	if err != nil {
		return nil, nil, err
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role != contextengine.RoleUser {
			continue
		}
		if !interactionMessageMatchesRunID(msg.Metadata, run.ID) {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" && len(msg.ContentBlocks) == 0 {
			continue
		}
		cloned := msg
		cloned.ContentBlocks = append([]contextengine.ContentBlock(nil), msg.ContentBlocks...)
		cloned.Metadata = cloneMetadata(msg.Metadata)
		return run, &cloned, nil
	}
	return nil, nil, fmt.Errorf("run %s has no retryable user input", run.ID)
}

func interactionMessageMatchesRunID(metadata map[string]any, runID string) bool {
	if strings.TrimSpace(runID) == "" || len(metadata) == 0 {
		return false
	}
	value, ok := metadata[meta.KeyRunID]
	if !ok || value == nil {
		return false
	}
	return strings.TrimSpace(fmt.Sprint(value)) == strings.TrimSpace(runID)
}

func (s *Service) executeTaskAccept(ctx context.Context, result *InteractionResult, req InteractionRequest, semantic *agent.SemanticSignal, sessionKey, content, model string, metadata map[string]any) {
	run, submitReq, err := s.submitInteractionRun(ctx, req, semantic, sessionKey, content, model, metadata)
	if err != nil {
		result.Decision.ReplyAct = ReplyActTaskFailure
		result.Error = err.Error()
		result.SubmitRequest = submitReq
		return
	}
	result.Run = run
	result.SubmitRequest = submitReq
}

func (s *Service) executeResumeAck(ctx context.Context, result *InteractionResult, req InteractionRequest, semantic *agent.SemanticSignal, sessionKey, content, model string, metadata map[string]any) {
	switch result.Decision.SpeechAct {
	case SpeechActApprovalReply:
		s.executeApprovalReply(ctx, result, req, content)
	case SpeechActClarificationReply:
		s.executeClarificationReply(ctx, result, req, semantic, sessionKey, content, model, metadata)
	case SpeechActTaskFollowup:
		s.executeTaskFollowup(ctx, result, sessionKey, content)
	}
}

func (s *Service) executeApprovalReply(ctx context.Context, result *InteractionResult, req InteractionRequest, content string) {
	if result.Context.PendingTicketID == "" {
		result.Error = "no pending approval"
		return
	}
	action := ""
	if req.StructuredApproval != nil {
		action = normalizeStructuredApprovalAction(req.StructuredApproval.Action)
	}
	if action == "" {
		action = parseApprovalSelection(content)
	}
	if action == "" {
		action = parseApprovalText(content)
	}
	resolution := approval.Resolution{ResolvedBy: "interaction"}
	switch action {
	case "approve":
		resolution.Status = approval.StatusApproved
		resolution.Note = "approved via interaction"
		result.Decision.Reason = "approval_reply_approve"
	case "always":
		resolution.Status = approval.StatusApproved
		resolution.Scope = approval.ScopeSession
		resolution.Note = "approved for current conversation via interaction"
		result.Decision.Reason = "approval_reply_always"
	case "deny":
		resolution.Status = approval.StatusDenied
		resolution.Note = "denied via interaction"
		result.Decision.Reason = "approval_reply_deny"
	default:
		resolution.Status = approval.StatusApproved
		resolution.Note = "approved via interaction (default)"
		result.Decision.Reason = "approval_reply_default"
	}
	ticket, err := s.ResolveApproval(ctx, result.Context.PendingTicketID, resolution)
	if err != nil {
		result.Error = err.Error()
		return
	}
	result.ApprovalResolved = true
	result.ApprovalStatus = resolution.Status
	if ticket != nil && ticket.RunID != "" {
		if run, err := s.runs.Get(ctx, ticket.RunID); err == nil {
			result.Run = run
		}
	}
}

func (s *Service) executeClarificationReply(ctx context.Context, result *InteractionResult, req InteractionRequest, semantic *agent.SemanticSignal, sessionKey, content, model string, metadata map[string]any) {
	if metadata == nil {
		metadata = make(map[string]any, 3)
	}
	metadata["preflight_followup"] = true
	if result.Context.ActiveRunID != "" {
		metadata["preflight_followup_for_run"] = result.Context.ActiveRunID
		metadata[agent.MetadataKeyClarificationSourceRunID] = result.Context.ActiveRunID
	}
	metadata[agent.MetadataKeyClarificationText] = strings.TrimSpace(content)
	run, submitReq, err := s.submitInteractionRun(ctx, req, semantic, sessionKey, content, model, metadata)
	if err != nil {
		result.Decision.ReplyAct = ReplyActTaskFailure
		result.Error = err.Error()
		result.SubmitRequest = submitReq
		return
	}
	result.Run = run
	result.SubmitRequest = submitReq
}

func (s *Service) executeTaskFollowup(ctx context.Context, result *InteractionResult, sessionKey, content string) {
	if s.directives == nil {
		result.Decision.ReplyAct = ReplyActTaskFailure
		result.Error = "interaction directives are not configured"
		return
	}
	if result.Context.SessionID == "" {
		result.Decision.ReplyAct = ReplyActTaskFailure
		result.Error = "interaction session context is missing"
		return
	}
	if err := s.directives.Push(ctx, result.Context.SessionID, agent.SessionDirective{
		Kind:    agent.SessionDirectiveSteer,
		Content: content,
	}); err != nil {
		result.Error = err.Error()
		return
	}
	result.SteerEnqueued = true
	if result.Context.ActiveRunID != "" {
		if run, err := s.runs.Get(ctx, result.Context.ActiveRunID); err == nil {
			result.Run = run
		}
	}
}

func (s *Service) executeActionAck(ctx context.Context, result *InteractionResult, sessionKey string) {
	switch result.Decision.Reason {
	case "structured_command_cancel", "text_command_cancel":
		if result.Context.ActiveRunID != "" {
			if run, err := s.CancelRun(ctx, result.Context.ActiveRunID); err == nil {
				result.Run = run
				result.RunCancelled = true
			} else {
				result.Error = err.Error()
			}
		}
	case "structured_command_status", "text_command_status":
		// Status is handled by ReplyActStatusReply, but if we got here via
		// action_ack path, load the run anyway.
		if result.Context.ActiveRunID != "" {
			if run, err := s.runs.Get(ctx, result.Context.ActiveRunID); err == nil {
				result.Run = run
			}
		}
	default:
		if result.Context.ActiveRunID != "" {
			if run, err := s.runs.Get(ctx, result.Context.ActiveRunID); err == nil {
				result.Run = run
			}
		}
	}
}

func (s *Service) executeStatusReply(ctx context.Context, result *InteractionResult) {
	if result.Context.ActiveRunID != "" {
		if run, err := s.runs.Get(ctx, result.Context.ActiveRunID); err == nil {
			result.Run = run
		}
	}
}
