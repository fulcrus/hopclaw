package channels

import (
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/planner"
)

func BridgeProcessingMessage(inputContent string, toolRounds int, toolNames []string, activeTask string, completedTasks, totalTasks int) string {
	_ = toolNames
	taskProgress := summarizeTaskProgress(activeTask, completedTasks, totalTasks)
	if bridgeLooksChinese(inputContent) {
		prefix := "已收到，正在处理。"
		if taskProgress != "" {
			return prefix + taskProgress
		}
		if toolRounds > 0 {
			return prefix + "我已经开始检查，处理完会直接给你可验证的结果。"
		}
		return "已收到，正在处理。如果时间稍长，我会继续同步进度。"
	}
	prefix := "Received. I am working on it."
	if taskProgressEN := summarizeTaskProgressEN(activeTask, completedTasks, totalTasks); taskProgressEN != "" {
		return prefix + " " + taskProgressEN
	}
	if toolRounds > 0 {
		return prefix + " I have already started checking and will reply with a verified result."
	}
	return "Received. I am working on it and will send another update if it takes longer."
}

func BridgeProgressUpdateMessage(inputContent, phase string, toolRounds int, toolNames []string, activeTask string, completedTasks, totalTasks int) string {
	phase = stringsTrim(strings.ToLower(phase))
	taskProgressZH := summarizeTaskProgress(activeTask, completedTasks, totalTasks)
	taskProgressEN := summarizeTaskProgressEN(activeTask, completedTasks, totalTasks)
	if bridgeLooksChinese(inputContent) {
		parts := []string{"已收到，正在处理。"}
		if phaseLine := summarizePhaseProgressZH(phase, toolNames); phaseLine != "" {
			parts = append(parts, phaseLine)
		}
		if taskProgressZH != "" {
			parts = append(parts, taskProgressZH)
		}
		if toolRounds > 0 {
			parts = append(parts, "我已经做了一些检查，正在继续推进。")
		} else if phase == "" && taskProgressZH == "" {
			parts = append(parts, "如果时间稍长，我会继续同步进度。")
		}
		return strings.Join(parts, "")
	}
	parts := []string{"Received. I am working on it."}
	if phaseLine := summarizePhaseProgressEN(phase, toolNames); phaseLine != "" {
		parts = append(parts, phaseLine)
	}
	if taskProgressEN != "" {
		parts = append(parts, taskProgressEN)
	}
	if toolRounds > 0 {
		parts = append(parts, "I have already done some checks and I am still working through them.")
	} else if phase == "" && taskProgressEN == "" {
		parts = append(parts, "I will send another update if it takes longer.")
	}
	return strings.Join(parts, " ")
}

func BridgeApprovalMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "当前请求正在等待审批确认。请按编号回复：[1] Approve  [2] Deny  [3] Always."
	}
	return "This request is waiting for approval. Reply with [1] Approve  [2] Deny  [3] Always."
}

func BridgeAutoApprovedMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "已自动确认这次审批，请求继续处理中。"
	}
	return "This approval was auto-approved for this chat. The request is continuing."
}

func BridgeApprovalAcceptedMessage(inputContent string, remembered bool) string {
	if bridgeLooksChinese(inputContent) {
		if remembered {
			return "已确认，并记住当前对话后续审批自动通过。请求继续处理中。"
		}
		return "已确认，请求继续处理中。"
	}
	if remembered {
		return "Approved. Later approvals will now be auto-approved in this chat, and the request is continuing."
	}
	return "Approved. The request is continuing."
}

func BridgeApprovalDeniedMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "已拒绝，这次请求不会继续处理。"
	}
	return "Denied. This request will not continue."
}

func BridgeApprovalResolveFailureMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "审批处理失败，请稍后重试，或前往控制台检查当前审批状态。"
	}
	return "The approval reply could not be processed. Please try again or review the approval in the console."
}

func BridgeCancelledMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "这个请求已被取消。"
	}
	return "This request was cancelled."
}

func BridgeCancelAcceptedMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "已取消当前请求。"
	}
	return "The current request has been cancelled."
}

func BridgeCancelFailureMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "没有成功取消当前请求，请稍后再试。"
	}
	return "The current request could not be cancelled. Please try again."
}

func BridgeQueuedFollowUpMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "已收到这条追加要求。这一项会接在当前处理完成后继续处理。"
	}
	return "Received. I will handle this follow-up after the current work finishes."
}

func BridgeSteerAcceptedMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "收到。我会立刻按你刚补充的要求调整当前处理，并在回复前重新检查结果。"
	}
	return "Received. I will adjust the current work with this new guidance and re-check the result before replying."
}

func BridgeIssueReportAcceptedMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "收到。我先排查你刚反馈的问题，不继续把它排队；修复后会把验证过的结果再发给你。"
	}
	return "Received. I will investigate the problem you just reported right away instead of queueing it, then reply with a verified result."
}

func BridgeVerificationRepairStartedMessage(inputContent string, summary string) string {
	summary = stringsTrim(summary)
	if bridgeLooksChinese(inputContent) {
		if summary == "" {
			return "我在发结果前做了检查，发现还不能确认成功。现在先重新排查，确认无误后再把结果发给你。"
		}
		return "我在发结果前做了检查，发现还不能确认成功：" + summary + "。我会先重新排查，确认无误后再把结果发给你。"
	}
	if summary == "" {
		return "I checked the result before sending it and could not verify it yet. I am re-checking it now and will reply after it is confirmed."
	}
	return "I checked the result before sending it and could not verify it yet: " + summary + ". I am re-checking it now and will reply after it is confirmed."
}

func BridgeVerificationFailureMessage(inputContent string, summary string) string {
	summary = stringsTrim(summary)
	if bridgeLooksChinese(inputContent) {
		if summary == "" {
			return "我没法把这次结果当作成功交付，因为关键结果还没有验证通过。请稍后重试，或联系管理员检查环境。"
		}
		return "我没法把这次结果当作成功交付，因为关键结果没有验证通过：" + summary + "。请稍后重试，或联系管理员检查环境。"
	}
	if summary == "" {
		return "I cannot deliver this as a successful result because the key output did not pass verification. Please try again later or ask an administrator to check the environment."
	}
	return "I cannot deliver this as a successful result because the key output did not pass verification: " + summary + ". Please try again later or ask an administrator to check the environment."
}

func BridgeVerificationWarningPrefix(inputContent string, summary string) string {
	summary = stringsTrim(summary)
	if bridgeLooksChinese(inputContent) {
		if summary == "" {
			return "注意：这次结果已完成，但质量检查里有警告。"
		}
		return "注意：这次结果已完成，但质量检查里有警告：" + summary + "。"
	}
	if summary == "" {
		return "Note: this result completed, but quality checks reported warnings."
	}
	return "Note: this result completed, but quality checks reported warnings: " + summary + "."
}

func BridgePartialResultPrefix(inputContent string, summary string) string {
	summary = stringsTrim(summary)
	if bridgeLooksChinese(inputContent) {
		if summary == "" {
			return "注意：主要结果已经完成，但这次交付仍有一部分没有完全确认。"
		}
		return "注意：主要结果已经完成，但这次交付仍有一部分没有完全确认：" + summary + "。"
	}
	if summary == "" {
		return "Note: the main result is ready, but part of this delivery could not be fully confirmed."
	}
	return "Note: the main result is ready, but part of this delivery could not be fully confirmed: " + summary + "."
}

func BridgeIdleChatReplyMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "我在。你可以直接发一个新任务，或者问我刚才那次任务的状态、结果和原因。"
	}
	return "I am here. You can send a new task, or ask about the status, result, or reasoning from the last run."
}

func BridgeNoPendingApprovalMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "当前没有等待你确认的审批。你可以直接继续发任务，或先问我上一次任务的状态。"
	}
	return "There is no approval waiting right now. You can send a new task or ask for the status of the last run."
}

func BridgeSmalltalkDuringTaskMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "我还在处理中。要看进度可以发 /status；如果发现结果不对，也可以直接告诉我问题，我会先排查。"
	}
	return "I am still working on this. Send /status for progress, or tell me directly if the result looks wrong and I will investigate first."
}

func BridgeSubmitFailureMessage(inputContent, raw string) string {
	if bridgeLooksLikeAuthFailure(raw) {
		return BridgeFailureMessage(inputContent, raw)
	}
	if bridgeLooksChinese(inputContent) {
		return "请求没有成功启动，请稍后重试或联系管理员检查服务状态。"
	}
	return "The request could not be started. Please try again later or ask an administrator to check the service."
}

func BridgeBackendAuthUnavailableMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "后端模型鉴权仍未恢复，当前无法处理新请求。请联系管理员刷新凭据后再试。"
	}
	return "Backend model authentication is still unavailable. New requests cannot be processed until credentials are refreshed."
}

func BridgeNoActiveRunMessage(inputContent string) string {
	if bridgeLooksChinese(inputContent) {
		return "当前没有正在处理的请求。请直接发送新的内容。"
	}
	return "There is no active work right now. Send a new request when you are ready."
}

func BridgeProgressStatusMessage(inputContent string, run *agent.Run, toolRounds int, toolNames []string) string {
	_ = toolNames
	if run == nil {
		return BridgeNoActiveRunMessage(inputContent)
	}
	statusLabel := bridgeRunStatusLabel(inputContent, run.Status)
	activeTask, completedTasks, totalTasks := activePlanProgress(run)
	if bridgeLooksChinese(inputContent) {
		parts := []string{"我还在处理这件事。", "状态：" + statusLabel + "。"}
		if run.Status == agent.RunWaitingInput {
			if line := bridgePreflightStatusLine(inputContent, run); line != "" {
				parts = append(parts, line)
			}
			if template := bridgePreflightReplyTemplate(inputContent, run.Preflight); template != "" {
				parts = append(parts, "可直接这样回："+strings.ReplaceAll(template, "\n", "；"))
			}
			parts = append(parts, "你直接补充我缺的目标、链接、路径，或回复确认，我就继续。")
			return strings.Join(parts, "")
		}
		if activeTask != "" {
			parts = append(parts, "当前在处理："+activeTask+"。")
		}
		if totalTasks > 0 {
			parts = append(parts, "进度："+strconv.Itoa(completedTasks)+"/"+strconv.Itoa(totalTasks)+"。")
		}
		if toolRounds > 0 {
			parts = append(parts, "我已经做了一些检查，正在继续推进。")
		}
		if line := bridgePreflightStatusLine(inputContent, run); line != "" {
			parts = append(parts, line)
		}
		if run.Status == agent.RunWaitingApproval {
			parts = append(parts, "如需继续，请按编号回复：[1] Approve  [2] Deny  [3] Always.")
		} else {
			parts = append(parts, "如果你发现结果不对，直接告诉我问题，我会优先排查。")
		}
		return strings.Join(parts, "")
	}
	parts := []string{"I am still working on this.", "Status: " + statusLabel + "."}
	if run.Status == agent.RunWaitingInput {
		if line := bridgePreflightStatusLine(inputContent, run); line != "" {
			parts = append(parts, line)
		}
		if template := bridgePreflightReplyTemplate(inputContent, run.Preflight); template != "" {
			parts = append(parts, "You can reply like this: "+strings.ReplaceAll(template, "\n", "; "))
		}
		parts = append(parts, "Reply with the missing target, path, URL, or confirmation and I will continue.")
		return strings.Join(parts, " ")
	}
	if activeTask != "" {
		parts = append(parts, "Current step: "+activeTask+".")
	}
	if totalTasks > 0 {
		parts = append(parts, "Progress: "+strconv.Itoa(completedTasks)+"/"+strconv.Itoa(totalTasks)+".")
	}
	if toolRounds > 0 {
		parts = append(parts, "I have already done some checks and I am still working through them.")
	}
	if line := bridgePreflightStatusLine(inputContent, run); line != "" {
		parts = append(parts, line)
	}
	if run.Status == agent.RunWaitingApproval {
		parts = append(parts, "Reply with [1] Approve  [2] Deny  [3] Always.")
	} else {
		parts = append(parts, "If the current result looks wrong, tell me what failed and I will investigate first.")
	}
	return strings.Join(parts, " ")
}

func BridgePreflightMessage(inputContent string, report *agent.RunPreflightReport) string {
	if report == nil || report.State == agent.RunPreflightReady {
		return ""
	}
	if bridgeLooksChinese(inputContent) {
		return strings.Join(bridgePreflightMessageLinesZH(report), "\n")
	}
	return strings.Join(bridgePreflightMessageLinesEN(report), "\n")
}

func bridgePreflightStatusLine(inputContent string, run *agent.Run) string {
	if run == nil || run.Preflight == nil || run.Preflight.State == agent.RunPreflightReady {
		return ""
	}
	summary := bridgePreflightSummary(inputContent, run.Preflight)
	if bridgeLooksChinese(inputContent) {
		switch run.Preflight.State {
		case agent.RunPreflightNeedsConfirmation:
			if summary != "" {
				return "前置条件：" + summary
			}
			return "前置条件：需要你确认。"
		case agent.RunPreflightAutoPreparing:
			if summary != "" {
				return "前置条件：" + summary
			}
			return "前置条件：系统正在自动准备执行条件。"
		}
		return ""
	}
	switch run.Preflight.State {
	case agent.RunPreflightNeedsConfirmation:
		if summary != "" {
			return "Preflight: " + summary
		}
		return "Preflight: confirmation needed."
	case agent.RunPreflightAutoPreparing:
		if summary != "" {
			return "Preflight: " + summary
		}
		return "Preflight: the system is preparing the execution prerequisites."
	default:
		return ""
	}
}

func bridgePreflightMessageLinesZH(report *agent.RunPreflightReport) []string {
	if report == nil || report.State == agent.RunPreflightReady {
		return nil
	}
	lines := []string{}
	switch report.State {
	case agent.RunPreflightNeedsConfirmation:
		lines = append(lines, "前置条件：需要你确认。")
	case agent.RunPreflightAutoPreparing:
		lines = append(lines, "前置条件：自动准备中。")
	default:
		lines = append(lines, "前置条件：处理中。")
	}
	if summary := bridgePreflightSummary("中文", report); summary != "" {
		lines = append(lines, "缺什么："+summary)
	}
	if question := bridgePreflightQuestion("中文", report); question != "" {
		lines = append(lines, "请直接回复："+question)
	}
	if template := bridgePreflightReplyTemplate("中文", report); template != "" {
		lines = append(lines, "可直接按这个格式回复：\n"+template)
	}
	if hints := bridgePreflightReplyHints("中文", report); len(hints) > 0 {
		lines = append(lines, "可直接回复示例："+strings.Join(hints, " / "))
	}
	if next := bridgePreflightContinueHint("中文", report); next != "" {
		lines = append(lines, "收到后："+next)
	}
	return lines
}

func bridgePreflightMessageLinesEN(report *agent.RunPreflightReport) []string {
	if report == nil || report.State == agent.RunPreflightReady {
		return nil
	}
	lines := []string{}
	switch report.State {
	case agent.RunPreflightNeedsConfirmation:
		lines = append(lines, "Preflight: confirmation needed.")
	case agent.RunPreflightAutoPreparing:
		lines = append(lines, "Preflight: auto preparing.")
	default:
		lines = append(lines, "Preflight: in progress.")
	}
	if summary := bridgePreflightSummary("", report); summary != "" {
		lines = append(lines, "Missing: "+summary)
	}
	if question := bridgePreflightQuestion("", report); question != "" {
		lines = append(lines, "Reply with: "+question)
	}
	if template := bridgePreflightReplyTemplate("", report); template != "" {
		lines = append(lines, "You can reply like this:\n"+template)
	}
	if hints := bridgePreflightReplyHints("", report); len(hints) > 0 {
		lines = append(lines, "Examples: "+strings.Join(hints, " / "))
	}
	if next := bridgePreflightContinueHint("", report); next != "" {
		lines = append(lines, "Next: "+next)
	}
	return lines
}

func bridgePreflightSummary(inputContent string, report *agent.RunPreflightReport) string {
	if report == nil {
		return ""
	}
	if !bridgeLooksChinese(inputContent) {
		return stringsTrim(report.Summary)
	}
	parts := make([]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		switch check.ID {
		case "reference_gap":
			parts = append(parts, "缺少明确的文件、链接、截图或仓库引用。")
		case "auto_prepare":
			parts = append(parts, "系统正在自动准备相关运行能力并验证后续交付。")
		case "expected_confirmation":
			parts = append(parts, "这类任务可能会改动文件、应用或外部系统，执行时通常需要确认。")
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "")
	}
	switch report.State {
	case agent.RunPreflightNeedsConfirmation:
		return "需要先补充关键信息或确认执行范围。"
	case agent.RunPreflightAutoPreparing:
		return "系统正在自动准备执行前置条件。"
	default:
		return ""
	}
}

func bridgePreflightQuestion(inputContent string, report *agent.RunPreflightReport) string {
	if report == nil {
		return ""
	}
	if !bridgeLooksChinese(inputContent) {
		return stringsTrim(report.Question)
	}
	if stringsTrim(report.Question) != "" {
		for _, check := range report.Checks {
			switch check.ID {
			case "reference_gap":
				return "补充具体的文件路径、网址、截图、仓库链接或目标对象。"
			case "expected_confirmation":
				return "回复确认继续，或补充更明确的执行范围。"
			}
		}
		return "补充缺失信息或确认后继续。"
	}
	return ""
}

func bridgePreflightReplyHints(inputContent string, report *agent.RunPreflightReport) []string {
	if report == nil || len(report.ReplyHints) == 0 {
		return nil
	}
	if !bridgeLooksChinese(inputContent) {
		return append([]string(nil), report.ReplyHints...)
	}
	out := make([]string, 0, len(report.ReplyHints))
	for _, hint := range report.ReplyHints {
		switch stringsTrim(hint) {
		case "Confirm and continue":
			out = append(out, "确认，继续")
		case "Continue, but only change README.md":
			out = append(out, "继续，但只改 README.md")
		default:
			out = append(out, hint)
		}
	}
	return out
}

func bridgePreflightReplyTemplate(inputContent string, report *agent.RunPreflightReport) string {
	if report == nil || len(report.Checks) == 0 {
		return ""
	}
	ids := make([]string, 0, len(report.Checks))
	seen := make(map[string]struct{}, len(report.Checks))
	for _, check := range report.Checks {
		id := stringsTrim(check.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return ""
	}
	lines := make([]string, 0, len(ids))
	zh := bridgeLooksChinese(inputContent)
	for _, id := range ids {
		switch id {
		case "reference_gap", "source_target":
			if zh {
				lines = append(lines, "目标对象：<文件路径 / URL / 仓库 / 截图>")
			} else {
				lines = append(lines, "Target: <file path / URL / repository / screenshot>")
			}
		case "delivery_target":
			if zh {
				lines = append(lines, "发送位置：<邮箱 / Slack 频道 / 飞书群 / 当前会话>")
			} else {
				lines = append(lines, "Destination: <email / Slack channel / chat thread / current chat>")
			}
		case "schedule":
			if zh {
				lines = append(lines, "执行时间：<例如 从下周一开始，每天 09:00>")
			} else {
				lines = append(lines, "Schedule: <for example every day at 09:00 starting next Monday>")
			}
		case "deployment_target":
			if zh {
				lines = append(lines, "部署目标：<staging / prod / 服务名 / URL>")
			} else {
				lines = append(lines, "Deploy target: <staging / prod / service name / URL>")
			}
		case "expected_confirmation":
			if zh {
				lines = append(lines, "确认：继续")
			} else {
				lines = append(lines, "Confirmation: continue")
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func bridgePreflightContinueHint(inputContent string, report *agent.RunPreflightReport) string {
	if report == nil {
		return ""
	}
	if !bridgeLooksChinese(inputContent) {
		return stringsTrim(report.ContinueHint)
	}
	if stringsTrim(report.ContinueHint) == "" {
		return ""
	}
	switch report.State {
	case agent.RunPreflightNeedsConfirmation:
		return "我会继续当前任务，不用你从头再说一遍，并在回复前验证结果。"
	case agent.RunPreflightAutoPreparing:
		return "系统准备完成后会自动继续。"
	default:
		return ""
	}
}

func summarizeTaskProgress(activeTask string, completedTasks, totalTasks int) string {
	activeTask = stringsTrim(activeTask)
	if totalTasks <= 0 && activeTask == "" {
		return ""
	}
	if completedTasks < 0 {
		completedTasks = 0
	}
	if activeTask != "" && totalTasks > 0 {
		return "当前在处理：" + activeTask + "。进度：" + strconv.Itoa(completedTasks) + "/" + strconv.Itoa(totalTasks) + "。"
	}
	if activeTask != "" {
		return "当前在处理：" + activeTask + "。"
	}
	if totalTasks > 0 {
		return "进度：" + strconv.Itoa(completedTasks) + "/" + strconv.Itoa(totalTasks) + "。"
	}
	return ""
}

func summarizeTaskProgressEN(activeTask string, completedTasks, totalTasks int) string {
	activeTask = stringsTrim(activeTask)
	if totalTasks <= 0 && activeTask == "" {
		return ""
	}
	if completedTasks < 0 {
		completedTasks = 0
	}
	if activeTask != "" && totalTasks > 0 {
		return "Current step: " + activeTask + ". Progress: " + strconv.Itoa(completedTasks) + "/" + strconv.Itoa(totalTasks) + "."
	}
	if activeTask != "" {
		return "Current step: " + activeTask + "."
	}
	if totalTasks > 0 {
		return "Progress: " + strconv.Itoa(completedTasks) + "/" + strconv.Itoa(totalTasks) + "."
	}
	return ""
}

func summarizePhaseProgressZH(phase string, toolNames []string) string {
	names := summarizeToolNames(toolNames)
	switch phase {
	case "thinking":
		return "我正在分析你的请求。"
	case "executing_tools":
		if names != "" {
			return "正在执行：" + names + "。"
		}
		return "正在执行需要的操作。"
	case "processing_results":
		return "我正在整理刚拿到的结果。"
	default:
		return ""
	}
}

func summarizePhaseProgressEN(phase string, toolNames []string) string {
	names := summarizeToolNames(toolNames)
	switch phase {
	case "thinking":
		return "I am thinking through the request."
	case "executing_tools":
		if names != "" {
			return "Currently running: " + names + "."
		}
		return "I am executing the next step now."
	case "processing_results":
		return "I am processing the latest results now."
	default:
		return ""
	}
}

func summarizeToolNames(toolNames []string) string {
	if len(toolNames) == 0 {
		return ""
	}
	names := make([]string, 0, len(toolNames))
	seen := make(map[string]struct{}, len(toolNames))
	for _, name := range toolNames {
		name = stringsTrim(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
		if len(names) >= 3 {
			break
		}
	}
	return strings.Join(names, ", ")
}

func activePlanProgress(run *agent.Run) (string, int, int) {
	if run == nil || run.Plan == nil {
		return "", 0, 0
	}
	completedTasks := 0
	totalTasks := len(run.Plan.Tasks)
	for _, task := range run.Plan.Tasks {
		if task.Status == planner.TaskCompleted {
			completedTasks++
		}
	}

	var runningLabels []string
	if len(run.Plan.RunningTasks) > 0 {
		for _, id := range run.Plan.RunningTasks {
			for _, task := range run.Plan.Tasks {
				if task.ID == id {
					if task.Title != "" {
						runningLabels = append(runningLabels, task.Title)
					} else {
						runningLabels = append(runningLabels, task.Goal)
					}
					break
				}
			}
		}
	}
	if len(runningLabels) == 0 && run.Plan.ActiveTask != "" {
		for _, task := range run.Plan.Tasks {
			if task.ID == run.Plan.ActiveTask {
				if task.Title != "" {
					runningLabels = append(runningLabels, task.Title)
				} else {
					runningLabels = append(runningLabels, task.Goal)
				}
				break
			}
		}
	}
	if len(runningLabels) == 0 && completedTasks < totalTasks {
		for _, task := range run.Plan.Tasks {
			if task.Status == planner.TaskRunning {
				if task.Title != "" {
					runningLabels = append(runningLabels, task.Title)
				} else {
					runningLabels = append(runningLabels, task.Goal)
				}
			}
		}
	}

	activeTask := ""
	if len(runningLabels) == 1 {
		activeTask = runningLabels[0]
	} else if len(runningLabels) > 1 {
		show := runningLabels
		if len(show) > 3 {
			show = show[:3]
		}
		activeTask = strings.Join(show, ", ")
	}
	return stringsTrim(activeTask), completedTasks, totalTasks
}

func bridgeRunStatusLabel(inputContent string, status agent.RunStatus) string {
	if bridgeLooksChinese(inputContent) {
		switch status {
		case agent.RunQueued:
			return "排队中"
		case agent.RunWaitingInput:
			return "等待补充信息"
		case agent.RunRunning, agent.RunStreaming:
			return "执行中"
		case agent.RunWaitingApproval:
			return "等待审批"
		case agent.RunCompleted:
			return "已完成"
		case agent.RunFailed:
			return "失败"
		case agent.RunCancelled:
			return "已取消"
		default:
			if stringsTrim(string(status)) == "" {
				return "处理中"
			}
			return string(status)
		}
	}
	switch status {
	case agent.RunQueued:
		return "queued"
	case agent.RunWaitingInput:
		return "waiting for input"
	case agent.RunRunning, agent.RunStreaming:
		return "running"
	case agent.RunWaitingApproval:
		return "waiting approval"
	case agent.RunCompleted:
		return "completed"
	case agent.RunFailed:
		return "failed"
	case agent.RunCancelled:
		return "cancelled"
	default:
		if stringsTrim(string(status)) == "" {
			return "in progress"
		}
		return string(status)
	}
}
