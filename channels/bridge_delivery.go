package channels

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/logging"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type interactionDeliveryRetryBackoffKey struct{}

var defaultInteractionDeliveryRetryBackoffs = []time.Duration{
	time.Second,
	2 * time.Second,
	4 * time.Second,
}

// DeliveryFailureContext captures the stable routing data used when an
// outbound reply could not be delivered and the bridge must emit a fallback
// notice plus a delivery-failed event.
type DeliveryFailureContext struct {
	ChannelName string
	RunID       string
	SessionID   string
	Target      RunNotificationTarget
}

// DeliverInteractionResult renders a runtime interaction decision to a channel
// adapter and updates status tracking for run-scoped replies.
func DeliverInteractionResult(
	ctx context.Context,
	send func(context.Context, OutboundMessage) error,
	events eventbus.Bus,
	status *RunStatusNotifier,
	channelName string,
	result *runtimesvc.InteractionResult,
	target RunNotificationTarget,
) {
	if result == nil {
		return
	}
	metadataOut := cloneAnyMap(target.Metadata)
	if metadataOut == nil {
		metadataOut = make(map[string]any, 4)
	}
	if statusKind := interactionReplyActStatusKind(result.Decision.ReplyAct); statusKind != meta.StatusKindUnknown {
		metadataOut[meta.KeyStatusKind] = statusKind.String()
	}
	if result.Run != nil {
		metadataOut[meta.KeyRunID] = result.Run.ID
	}

	if strings.TrimSpace(target.Format) == "" {
		target.Format = "text"
	}

	switch result.Decision.ReplyAct {
	case runtimesvc.ReplyActTaskAccept:
		deliverTaskAccept(ctx, status, channelName, result, target)
	case runtimesvc.ReplyActResumeAck:
		deliverResumeAck(ctx, send, events, status, channelName, result, target, metadataOut)
	case runtimesvc.ReplyActActionAck:
		deliverActionAck(ctx, send, events, channelName, result, target, metadataOut)
	case runtimesvc.ReplyActStatusReply:
		deliverStatusReply(ctx, send, events, status, channelName, result, target, metadataOut)
	case runtimesvc.ReplyActChatReply:
		deliverChatReply(ctx, send, events, channelName, result, target, metadataOut)
	case runtimesvc.ReplyActClarificationPrompt:
		deliverClarificationPrompt(ctx, send, events, channelName, result, target, metadataOut)
	case runtimesvc.ReplyActTaskFailure:
		deliverTaskFailure(ctx, send, events, channelName, result, target, metadataOut)
	case runtimesvc.ReplyActTaskResult:
		// These are typically handled by the run result delivery pipeline.
	}
}

func deliverTaskAccept(ctx context.Context, status *RunStatusNotifier, channelName string, result *runtimesvc.InteractionResult, target RunNotificationTarget) {
	if result.Run == nil || strings.TrimSpace(result.Run.ID) == "" {
		if status != nil {
			status.NotifySubmitFailure(ctx, target, "runtime returned empty run")
		}
		return
	}
	if status == nil {
		return
	}
	target.RunID = result.Run.ID
	status.Track(ctx, target)
	status.NotifyPreflight(ctx, target, result.Run.Preflight)
	log.Info("bridge: submitted run via interact", "channel", channelName, "run_id", result.Run.ID, "session_key", target.SessionKey)
}

func deliverResumeAck(
	ctx context.Context,
	send func(context.Context, OutboundMessage) error,
	events eventbus.Bus,
	status *RunStatusNotifier,
	channelName string,
	result *runtimesvc.InteractionResult,
	target RunNotificationTarget,
	metadataOut map[string]any,
) {
	var content string
	switch {
	case result.ApprovalResolved:
		if result.ApprovalStatus == approval.StatusDenied {
			content = BridgeApprovalDeniedMessage(target.InputContent)
			metadataOut[meta.KeyStatusKind] = meta.StatusKindApprovalReplyDenied.String()
		} else {
			remembered := strings.HasSuffix(result.Decision.Reason, "_always")
			content = BridgeApprovalAcceptedMessage(target.InputContent, remembered)
			metadataOut[meta.KeyStatusKind] = meta.StatusKindApprovalReplyApproved.String()
		}
	case result.SteerEnqueued:
		content = BridgeSteerAcceptedMessage(target.InputContent)
		metadataOut[meta.KeyStatusKind] = meta.StatusKindSteerAccepted.String()
	default:
		content = BridgeQueuedFollowUpMessage(target.InputContent)
		metadataOut[meta.KeyStatusKind] = meta.StatusKindResumeAck.String()
	}
	content = interactionReplyContent(result, content)
	if result.Run != nil && !(result.ApprovalResolved && result.ApprovalStatus == approval.StatusDenied) {
		target.RunID = result.Run.ID
		if status != nil {
			status.Track(ctx, target)
			status.NotifyPreflight(ctx, target, result.Run.Preflight)
		}
	}
	if send == nil {
		return
	}
	outbound := OutboundMessage{
		ChannelID: target.ChannelID,
		TargetID:  target.TargetID,
		ReplyToID: target.ReplyToID,
		Content:   content,
		Format:    target.Format,
		Metadata:  metadataOut,
	}
	logging.LogIfErr(ctx, sendInteractionDelivery(ctx, send, events, channelName, result, target, outbound), "send resume ack failed")
}

func deliverActionAck(
	ctx context.Context,
	send func(context.Context, OutboundMessage) error,
	events eventbus.Bus,
	channelName string,
	result *runtimesvc.InteractionResult,
	target RunNotificationTarget,
	metadataOut map[string]any,
) {
	var content string
	if result.RunCancelled {
		content = BridgeCancelAcceptedMessage(target.InputContent)
		metadataOut[meta.KeyStatusKind] = meta.StatusKindControlCancel.String()
	} else if result.Decision.Reason == "structured_command_cancel" || result.Decision.Reason == "text_command_cancel" {
		content = BridgeNoActiveRunMessage(target.InputContent)
		metadataOut[meta.KeyStatusKind] = meta.StatusKindControlCancel.String()
	} else {
		content = BridgeNoActiveRunMessage(target.InputContent)
		metadataOut[meta.KeyStatusKind] = meta.StatusKindControlCommand.String()
	}
	content = interactionReplyContent(result, content)
	if send == nil {
		return
	}
	outbound := OutboundMessage{
		ChannelID: target.ChannelID,
		TargetID:  target.TargetID,
		ReplyToID: target.ReplyToID,
		Content:   content,
		Format:    target.Format,
		Metadata:  metadataOut,
	}
	logging.LogIfErr(ctx, sendInteractionDelivery(ctx, send, events, channelName, result, target, outbound), "send action ack failed")
}

func deliverStatusReply(
	ctx context.Context,
	send func(context.Context, OutboundMessage) error,
	events eventbus.Bus,
	status *RunStatusNotifier,
	channelName string,
	result *runtimesvc.InteractionResult,
	target RunNotificationTarget,
	metadataOut map[string]any,
) {
	metadataOut[meta.KeyStatusKind] = meta.StatusKindControlStatus.String()
	metadataOut["control_command"] = string(ControlCommandStatus)
	var content string
	if result.Run != nil {
		toolRounds := result.Run.ToolRounds
		var toolNames []string
		if status != nil {
			if snapshot, ok := status.SnapshotRun(result.Run.ID); ok {
				toolRounds = snapshot.ToolRounds
				toolNames = snapshot.ToolNames
			}
		}
		content = BridgeProgressStatusMessage(target.InputContent, result.Run, toolRounds, toolNames)
		metadataOut[meta.KeyRunID] = result.Run.ID
		metadataOut["run_status"] = result.Run.Status
	} else {
		content = BridgeNoActiveRunMessage(target.InputContent)
		metadataOut["run_status"] = "idle"
	}
	content = interactionReplyContent(result, content)
	if send == nil {
		return
	}
	outbound := OutboundMessage{
		ChannelID: target.ChannelID,
		TargetID:  target.TargetID,
		ReplyToID: target.ReplyToID,
		Content:   content,
		Format:    target.Format,
		Metadata:  metadataOut,
	}
	logging.LogIfErr(ctx, sendInteractionDelivery(ctx, send, events, channelName, result, target, outbound), "send status reply failed")
}

func deliverChatReply(
	ctx context.Context,
	send func(context.Context, OutboundMessage) error,
	events eventbus.Bus,
	channelName string,
	result *runtimesvc.InteractionResult,
	target RunNotificationTarget,
	metadataOut map[string]any,
) {
	var content string
	switch {
	case strings.TrimSpace(result.ReplyMessage) != "":
		switch {
		case result.Decision.Reason == "approval_reply_no_pending":
			metadataOut[meta.KeyStatusKind] = meta.StatusKindApprovalReplyMissing.String()
		case result.Context.HasActiveRun:
			metadataOut[meta.KeyStatusKind] = meta.StatusKindSmalltalkDuringTask.String()
		default:
			metadataOut[meta.KeyStatusKind] = meta.StatusKindChatReply.String()
		}
		content = result.ReplyMessage
	case result.Decision.Reason == "approval_reply_no_pending":
		metadataOut[meta.KeyStatusKind] = meta.StatusKindApprovalReplyMissing.String()
		content = BridgeNoPendingApprovalMessage(target.InputContent)
	default:
		metadataOut[meta.KeyStatusKind] = meta.StatusKindTaskFailure.String()
		content = interactionConversationFailureContent(result, target.InputContent)
	}
	if send == nil {
		return
	}
	outbound := OutboundMessage{
		ChannelID: target.ChannelID,
		TargetID:  target.TargetID,
		ReplyToID: target.ReplyToID,
		Content:   content,
		Format:    target.Format,
		Metadata:  metadataOut,
	}
	logging.LogIfErr(ctx, sendInteractionDelivery(ctx, send, events, channelName, result, target, outbound), "send chat reply failed")
}

func deliverClarificationPrompt(
	ctx context.Context,
	send func(context.Context, OutboundMessage) error,
	events eventbus.Bus,
	channelName string,
	result *runtimesvc.InteractionResult,
	target RunNotificationTarget,
	metadataOut map[string]any,
) {
	content := strings.TrimSpace(result.ReplyMessage)
	if content != "" {
		metadataOut[meta.KeyStatusKind] = meta.StatusKindClarificationPrompt.String()
	} else {
		metadataOut[meta.KeyStatusKind] = meta.StatusKindTaskFailure.String()
		content = interactionConversationFailureContent(result, target.InputContent)
	}
	if send == nil {
		return
	}
	outbound := OutboundMessage{
		ChannelID: target.ChannelID,
		TargetID:  target.TargetID,
		ReplyToID: target.ReplyToID,
		Content:   content,
		Format:    target.Format,
		Metadata:  metadataOut,
	}
	logging.LogIfErr(ctx, sendInteractionDelivery(ctx, send, events, channelName, result, target, outbound), "send clarification prompt failed")
}

func deliverTaskFailure(
	ctx context.Context,
	send func(context.Context, OutboundMessage) error,
	events eventbus.Bus,
	channelName string,
	result *runtimesvc.InteractionResult,
	target RunNotificationTarget,
	metadataOut map[string]any,
) {
	metadataOut[meta.KeyStatusKind] = meta.StatusKindTaskFailure.String()
	content := BridgeFailureMessage(target.InputContent, result.Error)
	if result.SubmitRequest != nil && result.Run == nil {
		metadataOut[meta.KeyStatusKind] = meta.StatusKindSubmitFailed.String()
		content = BridgeSubmitFailureMessage(target.InputContent, result.Error)
	}
	content = interactionReplyContent(result, content)
	if send == nil {
		return
	}
	outbound := OutboundMessage{
		ChannelID: target.ChannelID,
		TargetID:  target.TargetID,
		ReplyToID: target.ReplyToID,
		Content:   content,
		Format:    target.Format,
		Metadata:  metadataOut,
	}
	logging.LogIfErr(ctx, sendInteractionDelivery(ctx, send, events, channelName, result, target, outbound), "send task failure failed")
}

func sendInteractionDelivery(
	ctx context.Context,
	send func(context.Context, OutboundMessage) error,
	events eventbus.Bus,
	channelName string,
	result *runtimesvc.InteractionResult,
	target RunNotificationTarget,
	outbound OutboundMessage,
) error {
	if send == nil {
		return nil
	}
	backoffs := interactionDeliveryRetryBackoffs(ctx)
	totalAttempts := len(backoffs) + 1
	var lastErr error
	attemptsUsed := 0
	for attempt := 1; attempt <= totalAttempts; attempt++ {
		attemptsUsed = attempt
		lastErr = send(ctx, outbound)
		if lastErr == nil {
			return nil
		}
		if !ShouldRetrySend(lastErr) {
			break
		}
		if attempt == totalAttempts {
			break
		}
		backoff := backoffs[attempt-1]
		log.Warn("bridge: interaction delivery retry scheduled",
			"channel", channelName,
			"run_id", interactionDeliveryRunID(result),
			"target_id", target.TargetID,
			"attempt", attempt,
			"max_attempts", totalAttempts,
			"backoff", backoff,
			"error", lastErr,
		)
		if err := waitInteractionDeliveryBackoff(ctx, backoff); err != nil {
			lastErr = err
			break
		}
	}
	log.Error("bridge: interaction delivery failed",
		"channel", channelName,
		"run_id", interactionDeliveryRunID(result),
		"target_id", target.TargetID,
		"reply_to_id", target.ReplyToID,
		"session_key", target.SessionKey,
		"status_kind", fmt.Sprint(outbound.Metadata[meta.KeyStatusKind]),
		"attempts", attemptsUsed,
		"error", lastErr,
	)
	HandleOutboundDeliveryFailure(ctx, send, events, DeliveryFailureContext{
		ChannelName: channelName,
		RunID:       interactionDeliveryRunID(result),
		SessionID:   interactionDeliverySessionID(result),
		Target:      target,
	}, outbound, attemptsUsed, lastErr)
	return lastErr
}

func interactionDeliveryRetryBackoffs(ctx context.Context) []time.Duration {
	if ctx != nil {
		if custom, ok := ctx.Value(interactionDeliveryRetryBackoffKey{}).([]time.Duration); ok {
			return append([]time.Duration(nil), custom...)
		}
	}
	return append([]time.Duration(nil), defaultInteractionDeliveryRetryBackoffs...)
}

func withInteractionDeliveryRetryBackoffs(ctx context.Context, backoffs []time.Duration) context.Context {
	return context.WithValue(ctx, interactionDeliveryRetryBackoffKey{}, append([]time.Duration(nil), backoffs...))
}

func waitInteractionDeliveryBackoff(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// HandleOutboundDeliveryFailure emits a best-effort user-facing failure notice
// and publishes the durable delivery.failed event for operator surfaces.
func HandleOutboundDeliveryFailure(
	ctx context.Context,
	send func(context.Context, OutboundMessage) error,
	events eventbus.Bus,
	failure DeliveryFailureContext,
	outbound OutboundMessage,
	attempts int,
	deliverErr error,
) {
	if noticeErr := sendDeliveryFailureNotice(ctx, send, failure, outbound); noticeErr != nil {
		log.Warn("bridge: delivery failure notice failed",
			"channel", failure.ChannelName,
			"run_id", failure.RunID,
			"target_id", failure.Target.TargetID,
			"error", noticeErr,
		)
	}
	publishDeliveryFailureEvent(ctx, events, failure, outbound, attempts, deliverErr)
}

func sendDeliveryFailureNotice(
	ctx context.Context,
	send func(context.Context, OutboundMessage) error,
	failure DeliveryFailureContext,
	outbound OutboundMessage,
) error {
	if send == nil || strings.TrimSpace(failure.Target.TargetID) == "" {
		return nil
	}
	channelID := strings.TrimSpace(outbound.ChannelID)
	if channelID == "" {
		channelID = strings.TrimSpace(failure.Target.ChannelID)
	}
	notice := OutboundMessage{
		ChannelID: channelID,
		TargetID:  strings.TrimSpace(failure.Target.TargetID),
		Content:   BridgeDeliveryFailureNoticeMessage(failure.Target.InputContent),
		Format:    defaultOutboundFormat(outbound.Format),
		Metadata:  cloneAnyMap(outbound.Metadata),
	}
	if notice.Metadata == nil {
		notice.Metadata = cloneAnyMap(failure.Target.Metadata)
	}
	if notice.Metadata == nil {
		notice.Metadata = make(map[string]any, 4)
	}
	notice.Metadata[meta.KeyStatusKind] = meta.StatusKindFailed.String()
	notice.Metadata["delivery_failure_notice"] = true
	notice.ReplyToID = ""
	return send(ctx, notice)
}

// BridgeDeliveryFailureNoticeMessage returns the user-facing fallback shown
// when the bridge could not deliver the original reply after retries.
func BridgeDeliveryFailureNoticeMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "上一条回复未送达。请稍后发送“status”查看任务状态，或直接重试。"
	}
	return "The previous reply could not be delivered. Send \"status\" to check the run, or retry once the channel is available."
}

func publishDeliveryFailureEvent(
	ctx context.Context,
	events eventbus.Bus,
	failure DeliveryFailureContext,
	outbound OutboundMessage,
	attempts int,
	deliverErr error,
) {
	if events == nil {
		return
	}
	statusKind := strings.TrimSpace(fmt.Sprint(outbound.Metadata[meta.KeyStatusKind]))
	contentPreview := ""
	if content := strings.TrimSpace(outbound.Content); content != "" {
		contentPreview = truncateInteractionDeliveryPreview(content, 200)
	}
	logging.LogIfErr(ctx, events.Publish(ctx, eventbus.NewDeliveryFailedEvent(
		failure.RunID,
		failure.SessionID,
		eventbus.DeliveryFailedPayload{
			Channel:        strings.TrimSpace(failure.ChannelName),
			TargetID:       strings.TrimSpace(failure.Target.TargetID),
			ReplyToID:      strings.TrimSpace(failure.Target.ReplyToID),
			SessionKey:     strings.TrimSpace(failure.Target.SessionKey),
			Attempts:       attempts,
			Error:          interactionDeliveryErrorText(deliverErr),
			StatusKind:     statusKind,
			ContentPreview: contentPreview,
		},
		nil,
	)), "publish delivery failed event failed")
}

func interactionDeliveryRunID(result *runtimesvc.InteractionResult) string {
	if result == nil || result.Run == nil {
		return ""
	}
	return strings.TrimSpace(result.Run.ID)
}

func interactionDeliverySessionID(result *runtimesvc.InteractionResult) string {
	if result == nil {
		return ""
	}
	if result.Run != nil && strings.TrimSpace(result.Run.SessionID) != "" {
		return strings.TrimSpace(result.Run.SessionID)
	}
	return strings.TrimSpace(result.Context.SessionID)
}

func truncateInteractionDeliveryPreview(content string, max int) string {
	if max <= 0 || len(content) <= max {
		return content
	}
	return content[:max] + "..."
}

func interactionDeliveryErrorText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func interactionReplyContent(result *runtimesvc.InteractionResult, fallback string) string {
	if result == nil {
		return fallback
	}
	if strings.TrimSpace(result.ReplyMessage) != "" {
		return result.ReplyMessage
	}
	return fallback
}

func interactionConversationFailureContent(result *runtimesvc.InteractionResult, inputContent string) string {
	raw := ""
	if result != nil {
		raw = strings.TrimSpace(result.Error)
	}
	if raw == "" {
		raw = "conversation reply unavailable"
	}
	return BridgeFailureMessage(inputContent, raw)
}

// RenderInteractionReply converts an interaction result into the user-facing
// reply text used by chat-like surfaces when no streaming run body is needed.
func RenderInteractionReply(result *runtimesvc.InteractionResult, inputContent string) string {
	if result == nil {
		return ""
	}
	if strings.TrimSpace(result.ReplyMessage) != "" {
		return result.ReplyMessage
	}
	if result.Run != nil {
		switch result.Decision.ReplyAct {
		case runtimesvc.ReplyActTaskAccept, runtimesvc.ReplyActResumeAck:
			if message := BridgePreflightMessage(inputContent, result.Run.Preflight); message != "" {
				return message
			}
		}
	}
	switch result.Decision.ReplyAct {
	case runtimesvc.ReplyActResumeAck:
		switch {
		case result.ApprovalResolved && result.ApprovalStatus == approval.StatusDenied:
			return BridgeApprovalDeniedMessage(inputContent)
		case result.ApprovalResolved:
			return BridgeApprovalAcceptedMessage(inputContent, false)
		case result.SteerEnqueued:
			return BridgeSteerAcceptedMessage(inputContent)
		default:
			return BridgeQueuedFollowUpMessage(inputContent)
		}
	case runtimesvc.ReplyActActionAck:
		if result.RunCancelled {
			return BridgeCancelAcceptedMessage(inputContent)
		}
		return BridgeNoActiveRunMessage(inputContent)
	case runtimesvc.ReplyActStatusReply:
		if result.Run != nil {
			return BridgeProgressStatusMessage(inputContent, result.Run, result.Run.ToolRounds, nil)
		}
		return BridgeNoActiveRunMessage(inputContent)
	case runtimesvc.ReplyActChatReply:
		if result.Decision.Reason == "approval_reply_no_pending" {
			return BridgeNoPendingApprovalMessage(inputContent)
		}
		return interactionConversationFailureContent(result, inputContent)
	case runtimesvc.ReplyActClarificationPrompt:
		return interactionConversationFailureContent(result, inputContent)
	case runtimesvc.ReplyActTaskFailure:
		if result.SubmitRequest != nil && result.Run == nil {
			return BridgeSubmitFailureMessage(inputContent, result.Error)
		}
		return BridgeFailureMessage(inputContent, result.Error)
	default:
		return ""
	}
}
