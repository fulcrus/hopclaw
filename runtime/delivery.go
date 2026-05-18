package runtime

import (
	"fmt"
	"path"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/resultmodel"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

type DeliveryBlock struct {
	Kind    string `json:"kind"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content,omitempty"`
}

type DeliveryAttachment struct {
	Kind        string `json:"kind"`
	Label       string `json:"label,omitempty"`
	URI         string `json:"uri,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

type DeliveryVerification struct {
	Status         string `json:"status,omitempty"`
	Summary        string `json:"summary,omitempty"`
	RequiredIssues int    `json:"required_issues,omitempty"`
	AdvisoryIssues int    `json:"advisory_issues,omitempty"`
	BlockingIssues int    `json:"blocking_issues,omitempty"`
}

type ConversationContext struct {
	Channel         string `json:"channel,omitempty"`
	MessageID       string `json:"message_id,omitempty"`
	ReplyToID       string `json:"reply_to_id,omitempty"`
	ThreadID        string `json:"thread_id,omitempty"`
	ParticipantID   string `json:"participant_id,omitempty"`
	ParticipantName string `json:"participant_name,omitempty"`
}

type DeliveryEnvelope struct {
	Summary      string                     `json:"summary,omitempty"`
	Governance   *GovernanceReceipt         `json:"governance,omitempty"`
	Blocks       []DeliveryBlock            `json:"blocks,omitempty"`
	Attachments  []DeliveryAttachment       `json:"attachments,omitempty"`
	NextActions  []resultmodel.ResultAction `json:"next_actions,omitempty"`
	Verification *DeliveryVerification      `json:"verification,omitempty"`
	Conversation *ConversationContext       `json:"conversation,omitempty"`
	Receipts     []DeliveryReceipt          `json:"receipts,omitempty"`
}

type DeliveryPlan = DeliveryEnvelope
type TerminalDelivery = DeliveryEnvelope

func buildDeliveryEnvelope(result *RunResult, run *agent.Run, session *agent.Session, verification *verifyrt.RunVerification) *DeliveryEnvelope {
	if result == nil {
		return nil
	}
	if verification != nil && verification.ShouldBlockDelivery() {
		return buildBlockedDeliveryEnvelope(result, run, session, verification)
	}
	delivery := &DeliveryEnvelope{
		Summary: strings.TrimSpace(result.Summary),
	}
	appendBlock := func(kind, title, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		delivery.Blocks = append(delivery.Blocks, DeliveryBlock{
			Kind:    strings.TrimSpace(kind),
			Title:   strings.TrimSpace(title),
			Content: content,
		})
	}
	if summary := strings.TrimSpace(result.Summary); summary != "" {
		appendBlock("summary", "Summary", summary)
	}
	if output := strings.TrimSpace(result.Output); output != "" && output != strings.TrimSpace(result.Summary) {
		appendBlock("output", "Result", output)
	}
	for _, item := range result.Deliverables {
		delivery.Attachments = append(delivery.Attachments, DeliveryAttachment{
			Kind:        normalize.FirstNonEmpty(strings.TrimSpace(item.Kind), "artifact"),
			Label:       deliveryAttachmentLabel(item),
			URI:         strings.TrimSpace(item.URI),
			ContentType: strings.TrimSpace(item.ContentType),
		})
	}
	if len(result.NextActions) > 0 {
		delivery.NextActions = append([]resultmodel.ResultAction(nil), result.NextActions...)
	}
	if len(result.Receipts) > 0 {
		delivery.Receipts = cloneDeliveryReceipts(result.Receipts)
	}
	if verification != nil {
		delivery.Verification = &DeliveryVerification{
			Status:         string(verification.Status),
			Summary:        strings.TrimSpace(verification.Summary),
			RequiredIssues: verification.RequiredWarnings + verification.RequiredFailures,
			AdvisoryIssues: verification.AdvisoryWarnings + verification.AdvisoryFailures,
			BlockingIssues: verification.BlockingFailures,
		}
		if delivery.Verification.Summary != "" {
			appendBlock("verification", "Verification", delivery.Verification.Summary)
		}
	}
	if result.Governance != nil {
		delivery.Governance = cloneGovernanceReceipt(result.Governance)
		if summary := strings.TrimSpace(result.Governance.Summary); summary != "" {
			appendBlock("governance", "Governance", summary)
		}
	}
	if ctx := conversationContextForRun(session, run); ctx != nil {
		delivery.Conversation = ctx
	}
	if delivery.Summary == "" {
		for _, block := range delivery.Blocks {
			if block.Content != "" {
				delivery.Summary = block.Content
				break
			}
		}
	}
	if delivery.Summary == "" && len(delivery.Attachments) > 0 {
		delivery.Summary = fmt.Sprintf("%d attachment(s) ready", len(delivery.Attachments))
	}
	if delivery.Summary == "" && delivery.Verification != nil {
		delivery.Summary = delivery.Verification.Summary
	}
	if delivery.Summary == "" && delivery.Conversation == nil && len(delivery.Blocks) == 0 && len(delivery.Attachments) == 0 && len(delivery.Receipts) == 0 {
		return nil
	}
	return delivery
}

func buildBlockedDeliveryEnvelope(result *RunResult, run *agent.Run, session *agent.Session, verification *verifyrt.RunVerification) *DeliveryEnvelope {
	delivery := &DeliveryEnvelope{
		Summary: blockedDeliverySummary(verification),
	}
	if verification != nil {
		delivery.Verification = &DeliveryVerification{
			Status:         string(verification.Status),
			Summary:        strings.TrimSpace(verification.Summary),
			RequiredIssues: verification.RequiredWarnings + verification.RequiredFailures,
			AdvisoryIssues: verification.AdvisoryWarnings + verification.AdvisoryFailures,
			BlockingIssues: verification.BlockingFailures,
		}
	}
	if delivery.Summary != "" {
		delivery.Blocks = append(delivery.Blocks, DeliveryBlock{
			Kind:    "verification_blocked",
			Title:   "Verification",
			Content: delivery.Summary,
		})
	}
	if len(result.NextActions) > 0 {
		delivery.NextActions = append([]resultmodel.ResultAction(nil), result.NextActions...)
	}
	if len(result.Receipts) > 0 {
		delivery.Receipts = cloneDeliveryReceipts(result.Receipts)
	}
	if result.Governance != nil {
		delivery.Governance = cloneGovernanceReceipt(result.Governance)
	}
	if ctx := conversationContextForRun(session, run); ctx != nil {
		delivery.Conversation = ctx
	}
	if delivery.Summary == "" && delivery.Verification != nil {
		delivery.Summary = delivery.Verification.Summary
	}
	if delivery.Summary == "" && delivery.Conversation == nil && len(delivery.Blocks) == 0 && len(delivery.Receipts) == 0 {
		return nil
	}
	return delivery
}

func blockedDeliverySummary(verification *verifyrt.RunVerification) string {
	if verification == nil {
		return "Task completed, but verification did not pass. The result was not delivered."
	}
	summary := strings.TrimSpace(verification.Summary)
	if summary == "" {
		return "Task completed, but verification did not pass. The result was not delivered."
	}
	return "Task completed, but verification did not pass: " + summary + ". The result was not delivered."
}

func deliveryAttachmentLabel(item DeliverableRef) string {
	if name := strings.TrimSpace(item.Name); name != "" {
		return name
	}
	uri := strings.TrimSpace(item.URI)
	if uri == "" {
		return normalize.FirstNonEmpty(strings.TrimSpace(item.ToolName), strings.TrimSpace(item.Kind))
	}
	label := path.Base(uri)
	if label == "." || label == "/" || label == "" {
		label = uri
	}
	return label
}

func conversationContextForRun(session *agent.Session, run *agent.Run) *ConversationContext {
	if session == nil {
		return nil
	}
	var matched *contextengine.Message
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := &session.Messages[i]
		if msg.Role != contextengine.RoleUser || msg.Metadata == nil {
			continue
		}
		if run != nil && strings.TrimSpace(run.InputEventID) != "" && metadataContainsValue(msg.Metadata, run.InputEventID) {
			matched = msg
			break
		}
		if matched == nil {
			matched = msg
		}
	}
	if matched == nil || matched.Metadata == nil {
		return nil
	}
	ctx := &ConversationContext{
		Channel:         firstMetadataString(matched.Metadata, meta.KeyChannel, "channel"),
		MessageID:       firstMetadataString(matched.Metadata, meta.KeyMessageID, "message_id", "ts"),
		ReplyToID:       firstMetadataString(matched.Metadata, meta.KeyReplyToID, "reply_to_message_id", "reply_to_id"),
		ThreadID:        firstMetadataString(matched.Metadata, meta.KeyThreadID, "thread_id", "thread_ts", "thread_name", "topic_id"),
		ParticipantID:   firstMetadataString(matched.Metadata, meta.KeySenderID, "user_id", "author_id"),
		ParticipantName: firstMetadataString(matched.Metadata, meta.KeySenderName, "sender_name", "display_name"),
	}
	if ctx.Channel == "" && ctx.MessageID == "" && ctx.ReplyToID == "" && ctx.ThreadID == "" && ctx.ParticipantID == "" && ctx.ParticipantName == "" {
		return nil
	}
	return ctx
}

func metadataContainsValue(metadata map[string]any, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" || metadata == nil {
		return false
	}
	for _, value := range metadata {
		if strings.TrimSpace(fmt.Sprint(value)) == want {
			return true
		}
	}
	return false
}

func firstMetadataString(metadata map[string]any, keys ...string) string {
	if metadata == nil {
		return ""
	}
	for _, key := range keys {
		if value, ok := metadata[key]; ok {
			if trimmed := strings.TrimSpace(fmt.Sprint(value)); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
