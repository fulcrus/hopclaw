package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/logging"
)

type ConversationTurnMode string

const (
	ConversationTurnChat          ConversationTurnMode = "chat"
	ConversationTurnClarification ConversationTurnMode = "clarification"
)

type ConversationTurnRequest struct {
	SessionKey             string
	Content                string
	ContentBlocks          []contextengine.ContentBlock
	Images                 []string
	Model                  string
	Metadata               map[string]any
	Mode                   ConversationTurnMode
	AdditionalSystemPrompt string
}

type ConversationTurnResult struct {
	SessionID string
	Model     string
	Message   contextengine.Message
	Usage     *ModelUsageInfo
}

// ConversationTurn executes one model-backed reply turn without creating a tracked run.
// The user and assistant messages are still persisted into the session transcript.
func (a *AgentComponent) ConversationTurn(ctx context.Context, req ConversationTurnRequest) (*ConversationTurnResult, error) {
	if a == nil {
		return nil, fmt.Errorf("agent component is required")
	}
	if a.context == nil {
		return nil, fmt.Errorf("%w", ErrContextEngineNil)
	}
	if a.model == nil {
		return nil, fmt.Errorf("%w", ErrModelClientNil)
	}
	if strings.TrimSpace(req.SessionKey) == "" {
		return nil, fmt.Errorf("session key is required")
	}

	mode := req.Mode
	if mode == "" {
		mode = ConversationTurnChat
	}
	session, err := a.sessions.GetOrCreate(ctx, strings.TrimSpace(req.SessionKey), defaultString(strings.TrimSpace(req.Model), a.config.DefaultModel))
	if err != nil {
		return nil, err
	}

	turnID := conversationTurnID(req.Metadata)
	messageMetadata := conversationMessageMetadata(req.Metadata, turnID, mode)
	incoming := IncomingMessage{
		SessionKey:    session.Key,
		Content:       req.Content,
		ContentBlocks: cloneContentBlocks(req.ContentBlocks),
		Images:        append([]string(nil), req.Images...),
		Model:         strings.TrimSpace(req.Model),
		Metadata:      messageMetadata,
	}
	lockedSession, release, err := a.sessions.LoadForExecution(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	defer release()

	appendConversationUserMessage(lockedSession, incoming, nil)
	if err := a.saveSession(ctx, nil, lockedSession); err != nil {
		return nil, err
	}
	a.emitMessageReceivedHook(ctx, session, incoming)

	effectiveContent := effectiveConversationContent(req.Content, req.ContentBlocks)
	transientRun := &Run{
		Model:            defaultString(strings.TrimSpace(req.Model), defaultString(lockedSession.Model, a.config.DefaultModel)),
		EffectiveProfile: parseEffectiveAgentProfile(messageMetadata),
	}
	sessionSnapshot := cloneSession(lockedSession)
	candidate, err := a.buildPreparedTurnCandidate(ctx, &prepareTurnSeed{
		sessionSnapshot: sessionSnapshot,
		runSnapshot:     cloneRun(transientRun),
		sessionRevision: sessionSnapshot.Revision,
	}, turnPrepareOptions{
		prepareSystemPrompt: func(_ *Run, _ *Session, basePrompt string) string {
			return conversationTurnSystemPrompt(basePrompt, mode, req.AdditionalSystemPrompt)
		},
		guidanceInput: effectiveContent,
		selectTools: func(_ *Run, _ []ToolDefinition, _ string) []ToolDefinition {
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	if candidate.needsCompaction {
		compacted, err := a.autoCompactPreparedTurnSession(ctx, nil, lockedSession)
		if err != nil {
			return nil, err
		}
		if compacted {
			sessionSnapshot = cloneSession(lockedSession)
			candidate, err = a.buildPreparedTurnCandidate(ctx, &prepareTurnSeed{
				sessionSnapshot: sessionSnapshot,
				runSnapshot:     cloneRun(transientRun),
				sessionRevision: sessionSnapshot.Revision,
			}, turnPrepareOptions{
				prepareSystemPrompt: func(_ *Run, _ *Session, basePrompt string) string {
					return conversationTurnSystemPrompt(basePrompt, mode, req.AdditionalSystemPrompt)
				},
				guidanceInput: effectiveContent,
				selectTools: func(_ *Run, _ []ToolDefinition, _ string) []ToolDefinition {
					return nil
				},
			})
			if err != nil {
				return nil, err
			}
		}
	}

	chatReq := candidate.request
	callStartedAt := time.Now()
	response, err := a.retryModelCall(ctx, transientRun, candidate.sessionSnapshot, chatReq.Model, &chatReq, func() (*ModelResponse, error) {
		return a.chatConversationTurn(ctx, lockedSession, turnID, mode, chatReq)
	})
	if err != nil || response == nil || strings.TrimSpace(response.Message.Content) == "" {
		failureContent := "[conversation turn failed]"
		failureMsg := contextengine.Message{Role: contextengine.RoleAssistant, Content: failureContent}
		_, _ = a.commitConversationTurnLocked(ctx, lockedSession, turnID, mode, chatReq.Model, failureMsg, nil)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("conversation turn returned empty assistant text")
	}
	if len(response.ToolCalls) > 0 && strings.TrimSpace(response.Message.Content) == "" {
		return nil, fmt.Errorf("conversation turn returned tool calls without assistant text")
	}
	a.trackModelUsage(
		ctx,
		transientRun,
		candidate.sessionSnapshot,
		chatReq.Model,
		actualProviderForModel(chatReq.Model, candidate.provider),
		response,
		time.Since(callStartedAt),
	)
	return a.commitConversationTurnLocked(ctx, lockedSession, turnID, mode, candidate.modelSelection.Model, response.Message, response.Usage)
}

func (a *AgentComponent) commitConversationTurnLocked(
	ctx context.Context,
	session *Session,
	turnID string,
	mode ConversationTurnMode,
	model string,
	message contextengine.Message,
	usage *ModelUsageInfo,
) (*ConversationTurnResult, error) {
	if session == nil {
		return nil, fmt.Errorf("session is required")
	}
	session.Model = defaultString(strings.TrimSpace(model), session.Model)
	message.Role = defaultConversationRole(message.Role)
	message.Metadata = conversationMessageMetadata(message.Metadata, turnID, mode)
	appendAssistantTextMessage(nil, session, message)
	if err := a.saveSession(ctx, nil, session); err != nil {
		return nil, err
	}
	return &ConversationTurnResult{
		SessionID: session.ID,
		Model:     session.Model,
		Message:   message,
		Usage:     usage,
	}, nil
}

func (a *AgentComponent) chatConversationTurn(
	ctx context.Context,
	session *Session,
	turnID string,
	mode ConversationTurnMode,
	req ChatRequest,
) (*ModelResponse, error) {
	envelope := conversationEnvelopeValue(mode)
	if sc, ok := a.model.(StreamingModelClient); ok && a.bus != nil {
		cb := &EventBusStreamCallback{
			Bus:            a.bus,
			SessionID:      safeSessionID(session),
			ExtraAttrs:     conversationStreamAttrs(turnID, envelope),
			SkipCompletion: true,
		}
		response, err := sc.ChatStream(ctx, req, cb)
		if err != nil {
			return nil, err
		}
		a.emitConversationStreamComplete(ctx, session, turnID, envelope, response)
		return response, nil
	}
	response, err := a.model.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	a.emitConversationFallbackStream(ctx, session, turnID, envelope, response)
	return response, nil
}

func (a *AgentComponent) emitConversationFallbackStream(ctx context.Context, session *Session, turnID, envelope string, response *ModelResponse) {
	if a == nil || a.bus == nil || response == nil {
		return
	}
	if delta := response.Message.Content; strings.TrimSpace(delta) != "" {
		logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewModelTextDeltaEvent(
			"",
			safeSessionID(session),
			eventbus.DeltaAttrs{Delta: delta},
			conversationStreamAttrs(turnID, envelope),
		)), "emit event failed")
	}
	a.emitConversationStreamComplete(ctx, session, turnID, envelope, response)
}

func (a *AgentComponent) emitConversationStreamComplete(ctx context.Context, session *Session, turnID, envelope string, response *ModelResponse) {
	if a == nil || a.bus == nil {
		return
	}
	payload := eventbus.ModelStreamCompleteAttrs{}
	if response != nil && response.Usage != nil {
		payload.PromptTokens = response.Usage.PromptTokens
		payload.CompletionTokens = response.Usage.CompletionTokens
		payload.TotalTokens = response.Usage.TotalTokens
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewModelStreamCompleteEvent(
		"",
		safeSessionID(session),
		payload,
		conversationStreamAttrs(turnID, envelope),
	)), "emit event failed")
}

func appendConversationUserMessage(session *Session, msg IncomingMessage, media MediaStore) {
	if session == nil {
		return
	}
	now := time.Now().UTC()
	userMsg := contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   msg.Content,
		CreatedAt: now,
		Metadata:  cloneMap(msg.Metadata),
	}
	if blocks := incomingContentBlocks(msg, media); len(blocks) > 0 {
		userMsg.ContentBlocks = blocks
	}
	session.Messages = append(session.Messages, userMsg)
	session.MessageCount = len(session.Messages)
	session.Metadata = MergeSessionMetadata(session.Metadata, msg)
	session.Scope = MergeScopeRef(session.Scope, msg)
	if strings.TrimSpace(msg.Model) != "" {
		session.Model = msg.Model
	}
	session.UpdatedAt = now
}

func conversationStreamAttrs(turnID, envelope string) map[string]any {
	attrs := map[string]any{}
	if strings.TrimSpace(turnID) != "" {
		attrs[meta.KeyInteractionTurnID] = strings.TrimSpace(turnID)
	}
	if strings.TrimSpace(envelope) != "" {
		attrs[meta.KeyInteractionEnvelope] = strings.TrimSpace(envelope)
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}

func conversationTurnSystemPrompt(base string, mode ConversationTurnMode, extra string) string {
	lines := []string{
		"<conversation_envelope>",
		"This turn does not create or resume a tracked run.",
		"Do not call tools, do not claim that work has started, and do not imply hidden execution happened.",
	}
	switch mode {
	case ConversationTurnClarification:
		lines = append(lines,
			"Ask only for the missing information needed to proceed safely.",
			"Keep the clarification specific and minimal.",
		)
	default:
		lines = append(lines,
			"Answer the user directly in natural language.",
			"If the request actually needs execution or tools, explain what is missing instead of pretending the task already ran.",
		)
	}
	if trimmed := strings.TrimSpace(extra); trimmed != "" {
		lines = append(lines, trimmed)
	}
	lines = append(lines, "</conversation_envelope>")
	if trimmed := strings.TrimSpace(base); trimmed != "" {
		return trimmed + "\n\n" + strings.Join(lines, "\n")
	}
	return strings.Join(lines, "\n")
}

func effectiveConversationContent(content string, blocks []contextengine.ContentBlock) string {
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

func conversationMessageMetadata(metadata map[string]any, turnID string, mode ConversationTurnMode) map[string]any {
	out := cloneMap(metadata)
	if out == nil {
		out = make(map[string]any, 2)
	}
	out[meta.KeyInteractionTurnID] = strings.TrimSpace(turnID)
	out[meta.KeyInteractionEnvelope] = conversationEnvelopeValue(mode)
	return out
}

func conversationEnvelopeValue(mode ConversationTurnMode) string {
	switch mode {
	case ConversationTurnClarification:
		return "clarification"
	default:
		return "conversation"
	}
}

func defaultConversationRole(role contextengine.MessageRole) contextengine.MessageRole {
	if strings.TrimSpace(string(role)) == "" {
		return contextengine.RoleAssistant
	}
	return role
}

func conversationTurnID(metadata map[string]any) string {
	if metadata != nil {
		if value := strings.TrimSpace(fmt.Sprint(metadata[meta.KeyInteractionTurnID])); value != "" && value != "<nil>" {
			return value
		}
	}
	return "turn-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}
