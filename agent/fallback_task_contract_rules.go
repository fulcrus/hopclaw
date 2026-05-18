package agent

// These predicates isolate task-contract keyword fallback from the primary
// semantic contract path so the heuristics stay scoped, reviewable, and
// easier to delete over time.
var (
	fallbackTaskContractMonitorIntentMarkers    = []string{"watch.", "cron", "rrule"}
	fallbackTaskContractMonitorIntentPhrases    = []string{"监控", "monitor", "watch", "alert", "notify me when"}
	fallbackTaskContractBrowserReferenceMarkers = []string{
		"browser.",
		"window.location",
		"document.queryselector",
		"localstorage",
		"sessionstorage",
		"navigator.",
	}
	fallbackTaskContractBrowserReferencePhrases = []string{
		"页面", "网页", "网站", "page", "website", "página", "sitio web",
		"当前页面", "current page", "this page", "the page", "current tab", "esta página",
	}
	fallbackTaskContractBrowserWatchPhrases = []string{
		"监控", "监听", "monitor", "watch", "watch for", "notify me when", "alert me when",
		"when this changes", "when it changes", "有变化就", "变化时通知", "变化时提醒",
	}
	fallbackTaskContractBrowserStayOnPagePhrases = []string{
		"保持在当前页面", "保持当前页面", "停留在当前页面", "停留在页面", "保持在该页面",
		"页面保持打开", "页面保持打开状态", "保持页面打开", "保持打开状态",
		"不要继续分析", "不要提取内容", "不要提交", "不要离开当前页面",
		"stay on the current page", "stay on the page", "keep the current page",
		"keep the page open", "leave the page open", "remain on the current page",
		"remain on the page", "do not continue analyzing", "don't continue analyzing",
		"do not extract content", "don't extract content", "do not submit", "don't submit",
	}
	fallbackTaskContractDeploymentWorkflowMarkers      = []string{"kubectl rollout", "helm upgrade", "terraform apply", "docker push", "oci://"}
	fallbackTaskContractAutomationIntentMarkers        = []string{"cron", "rrule", "freq=", "schedule_kind", "schedule-kind", "--schedule-kind"}
	fallbackTaskContractDevelopmentIntentMarkers       = []string{"apply_patch", "go test", "npm test", "pytest", "cargo test", "go.mod", "package.json", "cargo.toml", "dockerfile", "makefile", "fs.edit", "git.apply"}
	fallbackTaskContractSpreadsheetDeliverableMarkers  = []string{"format=csv", "format=tsv", "format=xlsx", "format=xls", "format=ods", "output=csv", "output=xlsx"}
	fallbackTaskContractDocumentDeliverableMarkers     = []string{"format=markdown", "format=md", "format=doc", "format=docx", "format=odt", "format=rtf", "output=markdown", "output=md", "output=docx"}
	fallbackTaskContractPresentationDeliverableMarkers = []string{"format=ppt", "format=pptx", "format=key", "output=pptx", "output=key"}
	fallbackTaskContractExplicitDeploymentMarkers      = []string{"kubectl apply", "kubectl rollout", "helm upgrade", "terraform apply", "docker push", "oci://"}
	fallbackTaskContractPublishMarkers                 = []string{"docker push", "helm upgrade", "kubectl rollout", "terraform apply", "oci://"}
	fallbackTaskContractDeployableArtifactMarkers      = []string{"artifact://", "dockerfile", "oci://", "sha256:", ".tar", ".tgz", ".zip", ".jar", ".war", ".deb", ".rpm", ".apk", ".ipa"}
	fallbackTaskContractStructuredWriteupMarkers       = []string{"format=markdown", "format=md", "format=csv", "format=xlsx", "format=pptx", "output=markdown", "output=md", "output=csv", "output=xlsx", "output=pptx"}
	fallbackTaskContractArtifactOutputMarkers          = []string{"output=", "dest=", "destination=", "path=", "save_as=", "save-as=", "write_to=", "write-to=", "artifact://"}
	fallbackTaskContractDeploymentWorkflowPhrases      = []string{"发布", "上线", "推到", "推送到", "roll out", "rollout"}
	fallbackTaskContractAutomationIntentPhrases        = []string{"schedule", "scheduled", "hourly", "daily", "weekly", "every hour", "every day", "every week", "定时", "每天", "每周", "每月", "每小时", "下周开始", "automation"}
	fallbackTaskContractDevelopmentIntentPhrases       = []string{"build app", "开发", "implement", "fix bug", "写代码", "refactor", "编码", "fix the bug", "fix the failing test"}
	fallbackTaskContractSpreadsheetDeliverablePhrases  = []string{".xlsx", ".csv", "spreadsheet", "表格", "excel"}
	fallbackTaskContractDocumentDeliverablePhrases     = []string{".docx", ".md", "document", "文档", "报告", "markdown"}
	fallbackTaskContractPresentationDeliverablePhrases = []string{".ppt", "slides", "presentation", "演示文稿", "幻灯片"}
	fallbackTaskContractExplicitDeploymentPhrases      = []string{"deploy", "promote", "roll out", "rollout", "上线", "部署", "部署到", "发版", "发版到", "推到生产", "推到 staging", "推到 prod"}
	fallbackTaskContractPublishPhrases                 = []string{"publish", "ship", "发布", "发布到", "上线到", "推送到"}
	fallbackTaskContractDeployableArtifactPhrases      = []string{
		"build", "artifact", "binary", "image", "container", "service", "app", "application", "site", "website",
		"release candidate", "rc", "version", "helm", "chart", "bundle",
		"构建", "产物", "二进制", "镜像", "容器", "服务", "应用", "站点", "网站", "版本",
	}
	fallbackTaskContractStructuredWriteupPhrases = []string{
		"spreadsheet", "table", "markdown", "slides", "slide deck", "presentation",
		"表格", "markdown", "汇总表", "幻灯片", "演示文稿", "报告", "文档",
	}
	fallbackTaskContractCountedWriteupPhrases = []string{
		"summarize", "summarise", "summary", "recap", "outline", "condense", "rewrite", "write up",
		"总结", "汇总", "整理", "缩成", "改写", "归纳",
	}
	fallbackTaskContractTransformWriteupPhrases = []string{
		"summarize into", "summarise into", "turn into", "rewrite into", "write up", "boil down",
		"整理成", "总结成", "汇总成", "缩成", "改写成", "归纳成",
	}
	fallbackTaskContractArtifactOutputPhrases = []string{
		"write", "save", "export", "create", "generate", "produce", "render", "output", "into", " to ",
		"写到", "写入", "生成", "导出", "输出", "保存", "产出",
	}
	fallbackTaskContractInvestigationPhrases = []string{
		"research", "investigate", "analyze", "analyse", "audit", "diagnose", "root cause", "study",
		"调研", "分析", "排查", "诊断", "根因", "原因", "风险", "架构",
	}
	fallbackTaskContractDeliveryIntentMarkers = []string{
		"email.send",
		"channel.send",
		"mailto:",
		"smtp",
		"mailgun",
		"ses",
		"webhook",
	}
	fallbackTaskContractDeliveryIntentPhrases = []string{
		"send", "post to", "email me", "email to", "mail to", "deliver to",
		"发邮件", "发送", "发到", "邮件给", "发给",
	}
	fallbackTaskContractDeliveryNegationMarkers = []string{
		"dry-run",
		"--dry-run",
		"dry_run",
		"send=false",
		"delivery=false",
		"notify=false",
		"post=false",
		"email=false",
		"no_delivery",
		"skip_delivery",
	}
	fallbackTaskContractDeliveryNegationPhrases = []string{
		"不要发", "不发送", "别发", "不用发", "不需要发",
		"don't send", "do not send", "don't notify", "do not notify",
		"don't post", "do not post", "don't email", "do not email",
		"直接回复", "当前会话", "reply here", "just reply", "respond here",
	}
	fallbackTaskContractAmbiguousWorkspaceTargetPhrases = []string{
		"this file", "the file", "current file", "that file",
		"this document", "the document", "current document",
		"这个文件", "该文件", "当前文件",
		"这个文档", "该文档", "当前文档",
	}
	fallbackTaskContractWorkspaceChangePhrases = []string{
		"edit", "change", "modify", "update", "fix", "rewrite", "patch", "refactor",
		"改一下", "改", "修改", "更新", "修一下", "修复", "重写", "调整",
	}
	fallbackTaskContractExternalSubmissionMarkers = []string{
		"browser.click",
		"browser.fill",
		"browser.type",
		"browser.select",
		"browser.upload",
		"browser.click_aria",
		"browser.type_aria",
		"browser.select_aria",
		"input[",
		"textarea[",
		"select[",
		"button[",
		"type=submit",
		"type=file",
		"selector=",
		"autocomplete=",
		"form=",
	}
	fallbackTaskContractScheduledExecutionMarkers = []string{
		"cron", "rrule", "freq=", "watch.", "schedule_kind", "schedule-kind", "--schedule-kind",
	}
	fallbackTaskContractScheduledExecutionPhrases = []string{
		"schedule", "scheduled", "recurring", "repeat", "repeating", "cron", "rrule", "automation",
		"hourly", "daily", "weekly", "every hour", "every day", "every week",
		"定时", "周期", "循环", "计划执行", "每天", "每周", "每月", "每小时",
	}
	fallbackTaskContractCancellationMarkers = []string{
		"/cancel",
		"/abort",
		"watch.cancel",
		"watch.remove",
		"watch.disable",
		"cron.remove",
		"cron.disable",
		"automation.remove",
		"automation.disable",
		`"action":"cancel"`,
		`"status":"disabled"`,
	}
	fallbackTaskContractCancellationPhrases = []string{
		"cancel", "stop", "disable", "pause", "turn off", "unsubscribe", "remove", "delete",
		"取消", "停止", "停掉", "关闭", "禁用", "暂停", "退订", "移除", "删除",
	}
	fallbackTaskContractScheduleReferenceMarkers = []string{"rrule", "freq="}
	fallbackTaskContractScheduleReferencePhrases = []string{
		"每天", "每周", "每月", "每小时", "明天", "后天", "下周", "下个月",
		"daily", "weekly", "monthly", "hourly", "every hour", "every day", "every week",
		"starting now", "start now", "tomorrow", "next week",
		"现在开始", "立即开始",
	}
	fallbackTaskContractCurrentConversationMarkers = []string{
		"delivery_target=current_conversation",
		"destination=current_conversation",
		"reply_target=current_conversation",
		`"delivery_target":"current_conversation"`,
		`"destination":"current_conversation"`,
		"conversation://current",
	}
	fallbackTaskContractCurrentConversationPhrases    = []string{"当前会话", "this chat", "current chat", "reply here", "respond here", "直接回复我"}
	fallbackTaskContractExternalDeliveryTargetPhrases = []string{
		"slack #", "discord", "telegram", "whatsapp", "feishu", "飞书", "企业微信", "微信群",
		"webhook", "邮箱", "mailbox",
	}
	fallbackTaskContractDeploymentTargetMarkers = []string{
		"k8s",
		"namespace",
		"cluster",
		"--env",
		"env=",
		"environment=",
		"namespace=",
		"cluster=",
	}
	fallbackTaskContractDeploymentTargetPhrases = []string{"staging", "production", "prod", "dev", "cluster", "service", "server", "env", "测试环境", "生产环境", "正式环境", "集群", "服务", "服务器", "部署到"}
)

func fallbackTaskContractMentionsMonitorIntent(lower string) bool {
	return containsAny(lower, fallbackTaskContractMonitorIntentMarkers...) ||
		containsAny(lower, fallbackTaskContractMonitorIntentPhrases...)
}

func fallbackTaskContractMentionsBrowserReference(lower string) bool {
	if len(explicitBrowserReferenceURLs(lower)) > 0 || hasBrowserReferenceSummaryPrefix(lower) {
		return true
	}
	return containsAny(lower, fallbackTaskContractBrowserReferenceMarkers...) ||
		containsAny(lower, fallbackTaskContractBrowserReferencePhrases...)
}

func fallbackTaskContractMentionsBrowserWatchIntent(lower string) bool {
	return containsAny(lower, "watch.", "cron", "rrule") ||
		containsAny(lower, fallbackTaskContractBrowserWatchPhrases...)
}

func fallbackTaskContractMentionsStayOnPageIntent(lower string) bool {
	return hasBrowserReferenceSummaryPrefix(lower) ||
		containsAny(lower, fallbackTaskContractBrowserStayOnPagePhrases...)
}

func fallbackTaskContractMentionsDeploymentWorkflow(lower string) bool {
	return containsAny(lower, fallbackTaskContractDeploymentWorkflowMarkers...) ||
		containsAny(lower, fallbackTaskContractDeploymentWorkflowPhrases...)
}

func fallbackTaskContractMentionsAutomationIntent(lower string) bool {
	return containsAny(lower, fallbackTaskContractAutomationIntentMarkers...) ||
		containsAny(lower, fallbackTaskContractAutomationIntentPhrases...)
}

func fallbackTaskContractMentionsDevelopmentIntent(lower string) bool {
	return containsAny(lower, fallbackTaskContractDevelopmentIntentMarkers...) ||
		containsAny(lower, fallbackTaskContractDevelopmentIntentPhrases...)
}

func fallbackTaskContractMentionsSpreadsheetDeliverable(lower string) bool {
	return containsAny(lower, fallbackTaskContractSpreadsheetDeliverableMarkers...) ||
		containsAny(lower, fallbackTaskContractSpreadsheetDeliverablePhrases...)
}

func fallbackTaskContractMentionsDocumentDeliverable(lower string) bool {
	return containsAny(lower, fallbackTaskContractDocumentDeliverableMarkers...) ||
		containsAny(lower, fallbackTaskContractDocumentDeliverablePhrases...)
}

func fallbackTaskContractMentionsPresentationDeliverable(lower string) bool {
	return containsAny(lower, fallbackTaskContractPresentationDeliverableMarkers...) ||
		containsAny(lower, fallbackTaskContractPresentationDeliverablePhrases...)
}

func fallbackTaskContractMentionsExplicitDeployment(lower string) bool {
	return containsAny(lower, fallbackTaskContractExplicitDeploymentMarkers...) ||
		containsAny(lower, fallbackTaskContractExplicitDeploymentPhrases...)
}

func fallbackTaskContractMentionsPublishIntent(lower string) bool {
	return containsAny(lower, fallbackTaskContractPublishMarkers...) ||
		containsAny(lower, fallbackTaskContractPublishPhrases...)
}

func fallbackTaskContractMentionsDeployableArtifact(lower string) bool {
	return containsAny(lower, fallbackTaskContractDeployableArtifactMarkers...) ||
		containsAny(lower, fallbackTaskContractDeployableArtifactPhrases...)
}

func fallbackTaskContractMentionsStructuredWriteup(lower string) bool {
	return containsAny(lower, fallbackTaskContractStructuredWriteupMarkers...) ||
		containsAny(lower, fallbackTaskContractStructuredWriteupPhrases...)
}

func fallbackTaskContractMentionsCountedWriteup(lower string) bool {
	return containsAny(lower, fallbackTaskContractCountedWriteupPhrases...)
}

func fallbackTaskContractMentionsTransformWriteup(lower string) bool {
	return containsAny(lower, fallbackTaskContractTransformWriteupPhrases...)
}

func fallbackTaskContractMentionsArtifactOutput(lower string) bool {
	return containsAny(lower, fallbackTaskContractArtifactOutputMarkers...) ||
		containsAny(lower, fallbackTaskContractArtifactOutputPhrases...)
}

func fallbackTaskContractMentionsInvestigation(lower string) bool {
	if containsAny(lower, "stack trace", "traceback", "git diff", "benchmark", "latency", "throughput") {
		return true
	}
	return containsAny(lower, "panic:", "fatal:", "segfault", "core dump", "heap profile", "cpu profile") ||
		containsAny(lower, fallbackTaskContractInvestigationPhrases...)
}

func fallbackTaskContractMentionsDeliveryIntent(lower string) bool {
	return containsAny(lower, fallbackTaskContractDeliveryIntentMarkers...) ||
		containsAny(lower, fallbackTaskContractDeliveryIntentPhrases...)
}

func fallbackTaskContractMentionsDeliveryNegation(lower string) bool {
	return containsAny(lower, fallbackTaskContractDeliveryNegationMarkers...) ||
		containsAny(lower, fallbackTaskContractDeliveryNegationPhrases...)
}

func fallbackTaskContractMentionsAmbiguousWorkspaceTarget(lower string) bool {
	return containsAny(lower, fallbackTaskContractAmbiguousWorkspaceTargetPhrases...)
}

func fallbackTaskContractMentionsWorkspaceChange(lower string) bool {
	return containsAny(lower, fallbackTaskContractWorkspaceChangePhrases...)
}

func fallbackTaskContractMentionsExternalNotification(lower string) bool {
	return containsAny(lower, "slack", "discord", "telegram", "whatsapp", "feishu", "webhook", "smtp", "mailgun", "ses")
}

func fallbackTaskContractMentionsExternalSubmission(lower string) bool {
	return containsAny(lower, fallbackTaskContractExternalSubmissionMarkers...)
}

func fallbackTaskContractMentionsScheduledExecution(lower string) bool {
	return containsAny(lower, fallbackTaskContractScheduledExecutionMarkers...) ||
		containsAny(lower, fallbackTaskContractScheduledExecutionPhrases...)
}

func fallbackTaskContractMentionsCancellation(lower string) bool {
	return containsAny(lower, fallbackTaskContractCancellationMarkers...) ||
		containsAny(lower, fallbackTaskContractCancellationPhrases...)
}

func fallbackTaskContractMentionsScheduleReference(lower string) bool {
	return containsAny(lower, fallbackTaskContractScheduleReferenceMarkers...) ||
		containsAny(lower, fallbackTaskContractScheduleReferencePhrases...)
}

func fallbackTaskContractMentionsExternalDeliveryTarget(lower string) bool {
	return containsAny(lower, "slack", "discord", "telegram", "whatsapp", "feishu", "webhook", "mailto:", "smtp") ||
		containsAny(lower, fallbackTaskContractExternalDeliveryTargetPhrases...)
}

func fallbackTaskContractMentionsCurrentConversation(lower string) bool {
	return containsAny(lower, fallbackTaskContractCurrentConversationMarkers...) ||
		containsAny(lower, fallbackTaskContractCurrentConversationPhrases...)
}

func fallbackTaskContractMentionsDeploymentTarget(lower string) bool {
	return containsAny(lower, fallbackTaskContractDeploymentTargetMarkers...) ||
		containsAny(lower, fallbackTaskContractDeploymentTargetPhrases...)
}
