package runtime

import (
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
)

// RenderDirectInteractionReply converts an interaction result into the
// user-facing reply text for direct surfaces such as terminal, web chat, and
// websocket clients. Natural-language chat and clarification turns must come
// from model output; if that output is missing, this renderer returns an
// infrastructure-failure message instead of falling back to canned smalltalk.
func RenderDirectInteractionReply(result *InteractionResult, inputContent string) string {
	if result == nil {
		return ""
	}
	if reply := strings.TrimSpace(result.ReplyMessage); reply != "" {
		return reply
	}
	switch result.Decision.ReplyAct {
	case ReplyActTaskAccept:
		if result.Run != nil && result.Run.Status == agent.RunWaitingApproval {
			return directApprovalRequiredMessage(inputContent)
		}
		return directTaskAcceptedMessage(inputContent)
	case ReplyActResumeAck:
		switch {
		case result.ApprovalResolved && result.ApprovalStatus == approval.StatusDenied:
			return directApprovalDeniedMessage(inputContent)
		case result.ApprovalResolved:
			return directApprovalAcceptedMessage(inputContent)
		case result.SteerEnqueued:
			return directSteerAcceptedMessage(inputContent)
		default:
			return directQueuedFollowUpMessage(inputContent)
		}
	case ReplyActActionAck:
		if result.RunCancelled {
			return directCancelAcceptedMessage(inputContent)
		}
		return directNoActiveRunMessage(inputContent)
	case ReplyActStatusReply:
		if result.Run != nil {
			return directProgressStatusMessage(inputContent, result.Run)
		}
		return directNoActiveRunMessage(inputContent)
	case ReplyActChatReply:
		if result.Decision.Reason == "approval_reply_no_pending" {
			return directNoPendingApprovalMessage(inputContent)
		}
		return directConversationFailureMessage(inputContent, result.Error)
	case ReplyActClarificationPrompt:
		return directConversationFailureMessage(inputContent, result.Error)
	case ReplyActTaskFailure:
		return directFailureMessage(inputContent, result.Error)
	default:
		return ""
	}
}

func directConversationFailureMessage(inputContent, raw string) string {
	return directFailureMessage(inputContent, firstNonEmpty(strings.TrimSpace(raw), "conversation reply unavailable"))
}

func directFailureMessage(inputContent, raw string) string {
	if interactionLooksChinese(inputContent) {
		return "我在处理这条消息时遇到了问题。请稍后重试。"
	}
	return "I encountered an issue while processing that message. Please try again."
}

func directNoPendingApprovalMessage(inputContent string) string {
	if interactionLooksChinese(inputContent) {
		return "当前没有等待你确认的审批。"
	}
	return "There is no approval waiting right now."
}

func directApprovalAcceptedMessage(inputContent string) string {
	if interactionLooksChinese(inputContent) {
		return "已确认，请求继续处理中。"
	}
	return "Approved. The request is continuing."
}

func directApprovalRequiredMessage(inputContent string) string {
	if interactionLooksChinese(inputContent) {
		return "这项请求需要先审批，批准后才会继续执行。"
	}
	return "This request needs approval before execution can continue."
}

func directApprovalDeniedMessage(inputContent string) string {
	if interactionLooksChinese(inputContent) {
		return "已拒绝，这次请求不会继续处理。"
	}
	return "Denied. This request will not continue."
}

func directTaskAcceptedMessage(inputContent string) string {
	if interactionLooksChinese(inputContent) {
		return "已收到，请求开始处理中。"
	}
	return "Received. The request is now being processed."
}

func directQueuedFollowUpMessage(inputContent string) string {
	if interactionLooksChinese(inputContent) {
		return "已收到这条追加要求。这一项会接在当前处理完成后继续处理。"
	}
	return "Received. I will handle this follow-up after the current work finishes."
}

func directSteerAcceptedMessage(inputContent string) string {
	if interactionLooksChinese(inputContent) {
		return "收到。我会按你刚补充的要求调整当前处理。"
	}
	return "Received. I will adjust the current work with this new guidance."
}

func directCancelAcceptedMessage(inputContent string) string {
	if interactionLooksChinese(inputContent) {
		return "已取消当前请求。"
	}
	return "The current request has been cancelled."
}

func directNoActiveRunMessage(inputContent string) string {
	if interactionLooksChinese(inputContent) {
		return "当前没有正在处理的请求。"
	}
	return "There is no active work right now."
}

func directProgressStatusMessage(inputContent string, run *agent.Run) string {
	if run == nil {
		return directNoActiveRunMessage(inputContent)
	}
	status := strings.TrimSpace(string(run.Status))
	if status == "" {
		status = "running"
	}
	if interactionLooksChinese(inputContent) {
		return "我还在处理这件事。状态：" + status + "。"
	}
	return "I am still working on this. Status: " + status + "."
}
