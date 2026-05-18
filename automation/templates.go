package automation

type TemplateKind string

const (
	TemplateKindCron   TemplateKind = "cron"
	TemplateKindWakeup TemplateKind = "wakeup"
	TemplateKindWatch  TemplateKind = "watch"
	TemplateKindHook   TemplateKind = "hook"
)

type TemplateFieldHint struct {
	Field       string `json:"field"`
	Label       string `json:"label,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
}

type CronTemplateDefaults struct {
	Name       string `json:"name,omitempty"`
	Schedule   string `json:"schedule,omitempty"`
	Timezone   string `json:"timezone,omitempty"`
	Channel    string `json:"channel,omitempty"`
	Target     string `json:"target,omitempty"`
	SessionKey string `json:"session_key,omitempty"`
	Model      string `json:"model,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	Enabled    bool   `json:"enabled"`
}

type WakeupTemplateDefaults struct {
	Name       string `json:"name,omitempty"`
	Schedule   string `json:"schedule,omitempty"`
	Channel    string `json:"channel,omitempty"`
	Message    string `json:"message,omitempty"`
	SessionKey string `json:"session_key,omitempty"`
	Enabled    bool   `json:"enabled"`
}

type WatchTemplateDefaults struct {
	Name             string `json:"name,omitempty"`
	Interval         string `json:"interval,omitempty"`
	SourceKind       string `json:"source_kind,omitempty"`
	SourceURL        string `json:"source_url,omitempty"`
	SourcePath       string `json:"source_path,omitempty"`
	SourceSessionKey string `json:"source_session_key,omitempty"`
	CalendarQuery    string `json:"calendar_query,omitempty"`
	CalendarLimit    int    `json:"calendar_limit,omitempty"`
	WebhookID        string `json:"webhook_id,omitempty"`
	WebhookSenderID  string `json:"webhook_sender_id,omitempty"`
	InboxLimit       int    `json:"inbox_limit,omitempty"`
	MailboxFolder    string `json:"mailbox_folder,omitempty"`
	MailboxQuery     string `json:"mailbox_query,omitempty"`
	MailboxLimit     int    `json:"mailbox_limit,omitempty"`
	DeliveryChannel  string `json:"delivery_channel,omitempty"`
	DeliveryTarget   string `json:"delivery_target,omitempty"`
	Prompt           string `json:"prompt,omitempty"`
	Enabled          bool   `json:"enabled"`
	FireOnStart      bool   `json:"fire_on_start,omitempty"`
}

type HookTemplateDefaults struct {
	Name       string `json:"name,omitempty"`
	Trigger    string `json:"trigger,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Phase      string `json:"phase,omitempty"`
	URL        string `json:"url,omitempty"`
	Command    string `json:"command,omitempty"`
	Filter     string `json:"filter,omitempty"`
	RetryCount int    `json:"retry_count,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
	Async      bool   `json:"async,omitempty"`
	Enabled    bool   `json:"enabled"`
}

type StarterTemplate struct {
	ID             string                  `json:"id"`
	Kind           TemplateKind            `json:"kind"`
	Category       string                  `json:"category,omitempty"`
	Name           string                  `json:"name"`
	Headline       string                  `json:"headline,omitempty"`
	Summary        string                  `json:"summary,omitempty"`
	Outcome        string                  `json:"outcome,omitempty"`
	Audience       string                  `json:"audience,omitempty"`
	Complexity     string                  `json:"complexity,omitempty"`
	Tags           []string                `json:"tags,omitempty"`
	SetupHints     []string                `json:"setup_hints,omitempty"`
	RequiredFields []TemplateFieldHint     `json:"required_fields,omitempty"`
	CronDefaults   *CronTemplateDefaults   `json:"cron_defaults,omitempty"`
	WakeupDefaults *WakeupTemplateDefaults `json:"wakeup_defaults,omitempty"`
	WatchDefaults  *WatchTemplateDefaults  `json:"watch_defaults,omitempty"`
	HookDefaults   *HookTemplateDefaults   `json:"hook_defaults,omitempty"`
}

func StarterTemplates() []StarterTemplate {
	return []StarterTemplate{
		{
			ID:         "daily-report-briefing",
			Kind:       TemplateKindCron,
			Category:   "reporting",
			Name:       "Daily Report Briefing",
			Headline:   "每天自动整理工作进展，产出可转发摘要",
			Summary:    "Run a scheduled agent prompt that turns source notes, tickets, or inbox summaries into a concise daily report.",
			Outcome:    "产出一份每天固定时间生成的工作摘要，可继续接到文档或群通知链路。",
			Audience:   "团队负责人、独立开发者、项目协调人",
			Complexity: "starter",
			Tags:       []string{"report", "daily", "summary", "management"},
			SetupHints: []string{
				"Connect the prompt to your real data source, such as issues, commits, inbox, or a local notes directory.",
				"Use a stable session key so the agent can accumulate context across runs.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "schedule", Label: "Cron schedule", Required: true, Placeholder: "0 18 * * 1-5", Help: "Run at 18:00 on weekdays."},
				{Field: "prompt", Label: "Prompt", Required: true, Help: "Explain where to collect the source material and what format the report should use."},
			},
			CronDefaults: &CronTemplateDefaults{
				Name:       "Daily report briefing",
				Schedule:   "0 18 * * 1-5",
				SessionKey: "ops:daily-report",
				Prompt:     "Collect today's work progress from the connected sources. Produce a concise daily report with: 1) completed items, 2) blockers, 3) tomorrow's priorities, 4) items needing escalation. Keep the output clear enough to forward directly.",
				Enabled:    true,
			},
		},
		{
			ID:         "daily-news-weather-feishu",
			Kind:       TemplateKindCron,
			Category:   "briefing",
			Name:       "Daily News + Weather Briefing",
			Headline:   "每天汇总新闻和天气，定时发到飞书或其他消息渠道",
			Summary:    "Schedule a daily agent run that gathers fresh news, city weather, and an operator-style summary, then delivers it to a channel target.",
			Outcome:    "直接对应“每天收集最新新闻和天气，发我飞书”的使用方式，适合先做成稳定模板。",
			Audience:   "创始人、运营、研究、个人信息订阅",
			Complexity: "starter",
			Tags:       []string{"news", "weather", "briefing", "feishu", "cron"},
			SetupHints: []string{
				"Use search.news or news.digest together with the weather skill or your preferred weather source inside the prompt.",
				"Start with one target city and one delivery channel, then expand after the prompt format is stable.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "schedule", Label: "Cron schedule", Required: true, Placeholder: "0 8 * * *", Help: "Example: 08:00 every day."},
				{Field: "target", Label: "Channel target", Required: true, Placeholder: "oc_xxx or chat_id", Help: "The Feishu/Slack/Telegram target that should receive the briefing."},
				{Field: "prompt", Label: "Prompt", Required: true, Help: "Describe which cities, news scope, and output format should be used."},
			},
			CronDefaults: &CronTemplateDefaults{
				Name:       "Daily news and weather briefing",
				Schedule:   "0 8 * * *",
				Timezone:   "Asia/Shanghai",
				Channel:    "feishu",
				Target:     "replace-with-feishu-target",
				SessionKey: "briefing:news-weather",
				Prompt:     "Collect today's latest major news and the current weather plus short forecast for Beijing. Write a concise morning briefing with sections: 1) Top headlines, 2) International developments worth watching, 3) Beijing weather, 4) Actions or meetings I should prepare for. Keep it brief enough to send directly to a channel.",
				Enabled:    true,
			},
		},
		{
			ID:         "international-breaking-news-watch",
			Kind:       TemplateKindWatch,
			Category:   "monitoring",
			Name:       "International Breaking News Watch",
			Headline:   "盯国际大事或行情信号，一有变化就发飞书提醒",
			Summary:    "Watch a news page, feed, or market signal source, detect meaningful changes, and push the alert to a delivery target.",
			Outcome:    "直接对应“订好金十数据的关键信息，有关国际的大事及时发飞书通知”的产品路径。",
			Audience:   "研究、运营、信息监控、创始人值守",
			Complexity: "intermediate",
			Tags:       []string{"watch", "breaking-news", "finance", "alert", "feishu"},
			SetupHints: []string{
				"Start with one structured source URL or feed instead of multiple noisy sources.",
				"Keep the prompt strict about what counts as a real alert so the watch does not spam the channel.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "source_url", Label: "Source URL", Required: true, Placeholder: "https://example.com/feed", Help: "The page, feed, or market endpoint to monitor."},
				{Field: "delivery_target", Label: "Delivery target", Required: true, Placeholder: "oc_xxx or channel id", Help: "The destination that should receive the alert."},
				{Field: "prompt", Label: "Prompt", Required: true, Help: "Describe what qualifies as a meaningful international event or market signal."},
			},
			WatchDefaults: &WatchTemplateDefaults{
				Name:            "International breaking news watch",
				Interval:        "5m",
				SourceKind:      "http",
				SourceURL:       "https://example.com/markets/international",
				DeliveryChannel: "feishu",
				DeliveryTarget:  "replace-with-feishu-target",
				Prompt:          "Inspect the latest content and alert me only when there is a meaningful international development. Prioritize war, diplomacy, sanctions, central-bank surprises, commodity shocks, and macro events with direct market impact. Output a short alert with: what changed, why it matters, and what to watch next.",
				Enabled:         true,
				FireOnStart:     true,
			},
		},
		{
			ID:         "market-signal-watch",
			Kind:       TemplateKindWatch,
			Category:   "monitoring",
			Name:       "Market Signal Watch",
			Headline:   "监控市场页面或资讯流，发现变化就触发分析",
			Summary:    "Watch a finance page or feed, detect meaningful changes, and produce a short alert summary for the operator.",
			Outcome:    "帮助你构建股价/资讯监控入口，再接通知或审批链路。",
			Audience:   "投资研究、行业观察、情报监控",
			Complexity: "starter",
			Tags:       []string{"watch", "market", "alert", "finance"},
			SetupHints: []string{
				"Replace the example URL with your real market page, news feed, or browser snapshot target.",
				"Keep the prompt focused on signal extraction so noisy changes do not spam the operator.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "source_url", Label: "Source URL", Required: true, Placeholder: "https://example.com/market", Help: "The page or feed to monitor."},
				{Field: "prompt", Label: "Prompt", Required: true, Help: "Describe what counts as a meaningful change and how the summary should be written."},
			},
			WatchDefaults: &WatchTemplateDefaults{
				Name:            "Market signal watch",
				Interval:        "15m",
				SourceKind:      "http",
				SourceURL:       "https://example.com/market",
				DeliveryChannel: "feishu",
				DeliveryTarget:  "replace-with-target",
				Prompt:          "Inspect the latest market content and tell me only the changes that matter. Highlight price moves, trend breaks, unusual volatility, and any news items worth escalating. End with a one-paragraph operator summary.",
				Enabled:         true,
				FireOnStart:     true,
			},
		},
		{
			ID:         "browser-release-smoke-check",
			Kind:       TemplateKindCron,
			Category:   "qa",
			Name:       "Browser Release Smoke Check",
			Headline:   "定时跑网页冒烟检查，并把异常发给操作人",
			Summary:    "Run a scheduled browser-based verification prompt against a target site after deployment windows or on a recurring cadence.",
			Outcome:    "对应网页自动化、部署验收、按钮点击和页面理解这些场景，是最先该产品化的 QA 能力。",
			Audience:   "独立开发者、测试、运维、站点运营",
			Complexity: "intermediate",
			Tags:       []string{"browser", "qa", "deployment", "smoke", "verification"},
			SetupHints: []string{
				"Use a deterministic checklist of pages, buttons, and assertions before adding more coverage.",
				"Pair screenshots and deliverables with a channel target so failed runs are visible immediately.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "schedule", Label: "Schedule", Required: true, Placeholder: "*/30 * * * *", Help: "How often the smoke check should run."},
				{Field: "target", Label: "Channel target", Required: true, Placeholder: "ops-room", Help: "Where the smoke summary should be sent."},
				{Field: "prompt", Label: "Prompt", Required: true, Help: "Describe the pages, interactions, and evidence that must be produced."},
			},
			CronDefaults: &CronTemplateDefaults{
				Name:       "Browser release smoke check",
				Schedule:   "*/30 * * * *",
				Channel:    "feishu",
				Target:     "replace-with-ops-target",
				SessionKey: "qa:browser-smoke",
				Prompt:     "Open the production site, verify the homepage loads, the login entry is visible, one key CTA can be clicked, and a representative page renders without obvious errors. Capture screenshots for the key checkpoints and summarize any regression in operator language.",
				Enabled:    true,
			},
		},
		{
			ID:         "ecommerce-site-anomaly-watch",
			Kind:       TemplateKindWatch,
			Category:   "operations",
			Name:       "E-commerce Site Anomaly Watch",
			Headline:   "巡检独立站关键页面，发现异常就提醒",
			Summary:    "Monitor storefront, checkout, pricing, or campaign pages and notify the operator when important content changes or disappears.",
			Outcome:    "适合跨境电商独立站的半自动运营，不承诺全自动经营，但能先把巡检、告警、排障做稳。",
			Audience:   "独立站运营、增长、技术运营",
			Complexity: "intermediate",
			Tags:       []string{"ecommerce", "ops", "watch", "anomaly", "storefront"},
			SetupHints: []string{
				"Start with one critical page such as landing, pricing, inventory, or checkout entry.",
				"Use browser_snapshot when static HTML is not enough and UI evidence matters.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "source_url", Label: "Page URL", Required: true, Placeholder: "https://shop.example.com/products/hero", Help: "The storefront or campaign page to monitor."},
				{Field: "delivery_target", Label: "Delivery target", Required: true, Placeholder: "ops-room", Help: "Where anomaly alerts should be sent."},
				{Field: "prompt", Label: "Prompt", Required: true, Help: "Describe the signals that count as stock, pricing, content, or layout anomalies."},
			},
			WatchDefaults: &WatchTemplateDefaults{
				Name:            "E-commerce site anomaly watch",
				Interval:        "10m",
				SourceKind:      "browser_snapshot",
				SourceURL:       "https://shop.example.com/products/hero",
				DeliveryChannel: "feishu",
				DeliveryTarget:  "replace-with-ops-target",
				Prompt:          "Compare the latest storefront state with the previous one and alert only on meaningful anomalies: missing buy buttons, obvious price drift, out-of-stock markers, broken hero content, broken checkout entry, or campaign text regressions. Include what changed and what should be checked manually.",
				Enabled:         true,
				FireOnStart:     true,
			},
		},
		{
			ID:         "customer-reply-draft",
			Kind:       TemplateKindWatch,
			Category:   "inbox",
			Name:       "Customer Reply Draft",
			Headline:   "监听客户消息并生成可审批回复草稿",
			Summary:    "Monitor a structured inbox or channel thread, classify the request, and draft a reply the operator can review before sending.",
			Outcome:    "把客服消息处理从人工盯盘，变成自动草拟 + 人审发送。",
			Audience:   "客服、销售支持、社区运营",
			Complexity: "intermediate",
			Tags:       []string{"customer", "reply", "approval", "channel"},
			SetupHints: []string{
				"Use a stable inbox session key such as slack:C123 or webhook:demo:user-1.",
				"Pair this with approval-enabled send actions so replies are reviewed before external delivery.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "source_session_key", Label: "Inbox session key", Required: true, Placeholder: "slack:C123", Help: "The conversation stream to monitor."},
				{Field: "prompt", Label: "Prompt", Required: true, Help: "Describe tone, escalation policy, and what should never be sent automatically."},
			},
			WatchDefaults: &WatchTemplateDefaults{
				Name:             "Customer reply draft",
				Interval:         "5m",
				SourceKind:       "structured_app_inbox",
				SourceSessionKey: "slack:C123",
				InboxLimit:       20,
				Prompt:           "Review new customer messages, classify intent, extract any order or issue details, and draft a concise reply. If the message is risky, angry, refund-related, or policy-sensitive, explicitly mark it for approval instead of pretending it is safe to send.",
				Enabled:          true,
				FireOnStart:      true,
			},
		},
		{
			ID:         "weekly-team-reminder",
			Kind:       TemplateKindWakeup,
			Category:   "coordination",
			Name:       "Weekly Team Reminder",
			Headline:   "固定时间向团队发出例行提醒",
			Summary:    "Send a wakeup message into a session or channel to trigger a recurring reminder or weekly planning note.",
			Outcome:    "适合每周同步、例会提醒、固定节奏运营提醒。",
			Audience:   "团队协作、运营、项目管理",
			Complexity: "starter",
			Tags:       []string{"wakeup", "reminder", "weekly", "team"},
			SetupHints: []string{
				"Wakeup is a good fit when you want a timed message to enter an existing session flow.",
				"Use the channel field when you want the reminder to surface in a specific chat channel.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "schedule", Label: "Schedule", Required: true, Placeholder: "0 9 * * 1", Help: "Example: Monday at 09:00."},
				{Field: "message", Label: "Message", Required: true, Help: "The reminder content that will be injected when the trigger fires."},
			},
			WakeupDefaults: &WakeupTemplateDefaults{
				Name:       "Weekly team reminder",
				Schedule:   "0 9 * * 1",
				Channel:    "webchat",
				SessionKey: "team:weekly-reminder",
				Message:    "Create this week's planning reminder. Ask the team to surface top priorities, risks, and deadlines, then summarize the responses into a short coordination note.",
				Enabled:    true,
			},
		},
		{
			ID:         "run-completion-webhook",
			Kind:       TemplateKindHook,
			Category:   "integration",
			Name:       "Run Completion Webhook",
			Headline:   "把任务完成事件推给外部系统",
			Summary:    "Fire an HTTP webhook whenever a run completes so downstream systems can update dashboards, ticketing, or analytics.",
			Outcome:    "适合接企业内部系统、埋点、看板或二次自动化编排。",
			Audience:   "平台工程、内部工具、集成开发",
			Complexity: "starter",
			Tags:       []string{"hook", "webhook", "integration", "run"},
			SetupHints: []string{
				"Use a secret if the endpoint is exposed beyond localhost.",
				"Keep the downstream endpoint idempotent because hooks may be retried.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "url", Label: "Webhook URL", Required: true, Placeholder: "https://example.com/webhooks/hopclaw", Help: "The HTTP endpoint that should receive run completion events."},
			},
			HookDefaults: &HookTemplateDefaults{
				Name:       "Run completion webhook",
				Trigger:    "run.completed",
				Kind:       "http",
				Phase:      "post",
				URL:        "https://example.com/webhooks/hopclaw",
				RetryCount: 3,
				TimeoutSec: 30,
				Async:      true,
				Enabled:    true,
			},
		},
		{
			ID:         "governance-dead-letter-webhook",
			Kind:       TemplateKindHook,
			Category:   "governance",
			Name:       "Governance Dead-Letter Webhook",
			Headline:   "治理投递进入死信时立刻推送外部告警",
			Summary:    "Send governance dead-letter events to your incident, SIEM, or compliance endpoint so operator intervention starts immediately.",
			Outcome:    "把治理投递失败从“沉默积压”变成可追踪、可升级、可审计的企业事件。",
			Audience:   "平台治理、安全运营、合规审计",
			Complexity: "starter",
			Tags:       []string{"hook", "governance", "dead-letter", "alert", "compliance"},
			SetupHints: []string{
				"Point this at an incident intake, SIEM, or audit endpoint that can open a ticket or page on-call immediately.",
				"Use delivery_id, adapter_name, delivery_status, and last_error to correlate dead-letter incidents with operator redrive actions.",
				"For local testing, run python3 ./scripts/hooks/serve-sample-webhook.py --port 8787 --outbox-dir /tmp/hopclaw-webhook-inbox and point this template at http://127.0.0.1:8787/governance/dead-letter.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "url", Label: "Webhook URL", Required: true, Placeholder: "https://example.com/governance/dead-letter", Help: "The endpoint that should receive dead-letter governance alerts."},
			},
			HookDefaults: &HookTemplateDefaults{
				Name:       "Governance dead-letter alert",
				Trigger:    "governance.delivery.dead_lettered",
				Kind:       "http",
				Phase:      "post",
				RetryCount: 3,
				TimeoutSec: 30,
				Async:      true,
				Enabled:    true,
			},
		},
		{
			ID:         "governance-retry-escalation-hook",
			Kind:       TemplateKindHook,
			Category:   "governance",
			Name:       "Governance Retry Escalation Hook",
			Headline:   "治理投递连续重试时自动升级处置",
			Summary:    "Escalate repeated governance delivery retries into a local wrapper script so you can page, ticket, or enrich the record before it becomes dead-letter.",
			Outcome:    "把“持续重试”提前暴露给值班与合规流程，减少真正进入死信后的人工补救成本。",
			Audience:   "值班工程、平台治理、安全运营",
			Complexity: "intermediate",
			Tags:       []string{"hook", "governance", "retry", "escalation", "sre"},
			SetupHints: []string{
				"Use a wrapper script so you can fan out to paging, ticketing, CMDB enrichment, or internal buses without coupling HopClaw to one vendor.",
				"The default filter starts escalation on the third delivery attempt. Raise or lower the threshold based on business criticality.",
				"The repository includes a starter wrapper at ./scripts/hooks/escalate-governance-retry.sh.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "command", Label: "Command", Required: true, Placeholder: "./scripts/hooks/escalate-governance-retry.sh", Help: "A local wrapper script that escalates repeated retry events."},
			},
			HookDefaults: &HookTemplateDefaults{
				Name:       "Governance retry escalation",
				Trigger:    "governance.delivery.retry_scheduled",
				Kind:       "command",
				Phase:      "post",
				Filter:     "delivery_attempts >= 3",
				RetryCount: 1,
				TimeoutSec: 20,
				Async:      true,
				Enabled:    true,
			},
		},
		{
			ID:         "governance-redrive-audit-webhook",
			Kind:       TemplateKindHook,
			Category:   "governance",
			Name:       "Governance Redrive Audit Webhook",
			Headline:   "把治理 redrive 操作同步到外部审计轨迹",
			Summary:    "Record governance redrive events in an external audit or case-management system so manual recovery actions stay attributable and reviewable.",
			Outcome:    "把人工重驱的责任链、恢复链同步到 B 端审计系统，满足更严格的治理留痕要求。",
			Audience:   "审计、风险控制、平台治理",
			Complexity: "starter",
			Tags:       []string{"hook", "governance", "redrive", "audit", "enterprise"},
			SetupHints: []string{
				"Route this to an immutable audit sink or case-management system that keeps operator recovery actions searchable.",
				"Pair this with the operator console governance queue so every redrive has a matching external audit record.",
				"For local testing, run python3 ./scripts/hooks/serve-sample-webhook.py --port 8787 --outbox-dir /tmp/hopclaw-webhook-inbox and point this template at http://127.0.0.1:8787/governance/redrive-audit.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "url", Label: "Webhook URL", Required: true, Placeholder: "https://example.com/governance/redrive-audit", Help: "The endpoint that should receive redrive audit events."},
			},
			HookDefaults: &HookTemplateDefaults{
				Name:       "Governance redrive audit",
				Trigger:    "governance.delivery.redriven",
				Kind:       "http",
				Phase:      "post",
				RetryCount: 3,
				TimeoutSec: 30,
				Async:      true,
				Enabled:    true,
			},
		},
		{
			ID:         "slack-governance-dead-letter-command",
			Kind:       TemplateKindHook,
			Category:   "chatops",
			Name:       "Slack Governance Alert Command",
			Headline:   "把治理死信告警发到 Slack 值班或 ChatOps 流程",
			Summary:    "Run a local wrapper command that reads the governance dead-letter payload from stdin and formats a Slack message, block, or relay call.",
			Outcome:    "适合接企业现有 Slack 值班群、告警机器人或内部 ChatOps 中继，不把供应商格式耦合进 HopClaw 核心。",
			Audience:   "SRE、平台治理、值班团队",
			Complexity: "intermediate",
			Tags:       []string{"hook", "slack", "chatops", "governance", "dead-letter"},
			SetupHints: []string{
				"The command receives the raw JSON payload on stdin. Use a wrapper script to render a Slack block, mention on-call, and de-duplicate repeated alerts.",
				"Prefer storing Slack secrets outside the template and let the wrapper fetch them from your secret manager or environment.",
				"The repository includes a starter wrapper at ./scripts/hooks/send-slack-governance-alert.sh.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "command", Label: "Command", Required: true, Placeholder: "./scripts/hooks/send-slack-governance-alert.sh", Help: "A local command that reads JSON from stdin and posts a Slack alert."},
			},
			HookDefaults: &HookTemplateDefaults{
				Name:       "Slack governance dead-letter alert",
				Trigger:    "governance.delivery.dead_lettered",
				Kind:       "command",
				Phase:      "post",
				RetryCount: 1,
				TimeoutSec: 20,
				Async:      true,
				Enabled:    true,
			},
		},
		{
			ID:         "feishu-governance-dead-letter-command",
			Kind:       TemplateKindHook,
			Category:   "chatops",
			Name:       "Feishu Governance Alert Command",
			Headline:   "把治理死信告警接入飞书机器人或群通知",
			Summary:    "Run a local wrapper command that reads the governance dead-letter payload and formats a Feishu bot card or enterprise relay message.",
			Outcome:    "适合国内团队把治理故障直接接进飞书群、值班机器人或内部通知编排。",
			Audience:   "平台治理、值班团队、企业运维",
			Complexity: "intermediate",
			Tags:       []string{"hook", "feishu", "chatops", "governance", "dead-letter"},
			SetupHints: []string{
				"Use a wrapper command so the bot secret, signature, and card format stay outside the HopClaw runtime.",
				"Good wrapper output usually includes adapter_name, delivery_id, next operator action, and the last error summary.",
				"The repository includes a starter wrapper at ./scripts/hooks/send-feishu-governance-alert.sh.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "command", Label: "Command", Required: true, Placeholder: "./scripts/hooks/send-feishu-governance-alert.sh", Help: "A local command that reads JSON from stdin and posts a Feishu alert."},
			},
			HookDefaults: &HookTemplateDefaults{
				Name:       "Feishu governance dead-letter alert",
				Trigger:    "governance.delivery.dead_lettered",
				Kind:       "command",
				Phase:      "post",
				RetryCount: 1,
				TimeoutSec: 20,
				Async:      true,
				Enabled:    true,
			},
		},
		{
			ID:         "email-governance-dead-letter-command",
			Kind:       TemplateKindHook,
			Category:   "notification",
			Name:       "Email Governance Alert Command",
			Headline:   "用邮件方式触发治理死信升级通知",
			Summary:    "Run a command that converts governance dead-letter payloads into an email alert, digest, or escalation ticket message.",
			Outcome:    "适合仍以邮件作为合规告警、审计抄送或跨团队升级通道的企业环境。",
			Audience:   "安全运营、审计、跨团队协作",
			Complexity: "starter",
			Tags:       []string{"hook", "email", "notification", "governance", "dead-letter"},
			SetupHints: []string{
				"Have the wrapper command decide recipients, subject conventions, and attachment policy from the JSON payload.",
				"Prefer one internal mail wrapper over embedding SMTP or provider credentials directly in the runtime.",
				"The repository includes a starter wrapper at ./scripts/hooks/send-governance-email-alert.sh.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "command", Label: "Command", Required: true, Placeholder: "./scripts/hooks/send-governance-email-alert.sh", Help: "A local command that reads JSON from stdin and sends an email alert."},
			},
			HookDefaults: &HookTemplateDefaults{
				Name:       "Email governance dead-letter alert",
				Trigger:    "governance.delivery.dead_lettered",
				Kind:       "command",
				Phase:      "post",
				RetryCount: 1,
				TimeoutSec: 20,
				Async:      true,
				Enabled:    true,
			},
		},
		{
			ID:         "ticket-governance-dead-letter-command",
			Kind:       TemplateKindHook,
			Category:   "itsm",
			Name:       "ITSM Governance Ticket Command",
			Headline:   "治理死信时自动创建工单或事件单",
			Summary:    "Run a local wrapper command that opens or updates a ticket in Jira, ServiceNow, or your internal incident system using the governance payload.",
			Outcome:    "把死信治理问题自动变成可追踪的工单实体，进入值班、分派、SLA 和复盘流程。",
			Audience:   "运维、ITSM、平台治理、合规团队",
			Complexity: "intermediate",
			Tags:       []string{"hook", "ticket", "itsm", "governance", "incident"},
			SetupHints: []string{
				"Use the wrapper to enforce dedupe keys so repeated dead-letter events update the same incident instead of creating ticket storms.",
				"Carry delivery_id, adapter_name, governance_kind, and error into the downstream incident body for operator triage.",
				"The repository includes a starter wrapper at ./scripts/hooks/open-governance-incident.sh.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "command", Label: "Command", Required: true, Placeholder: "./scripts/hooks/open-governance-incident.sh", Help: "A local command that reads JSON from stdin and opens or updates an incident ticket."},
			},
			HookDefaults: &HookTemplateDefaults{
				Name:       "Governance dead-letter incident ticket",
				Trigger:    "governance.delivery.dead_lettered",
				Kind:       "command",
				Phase:      "post",
				RetryCount: 1,
				TimeoutSec: 20,
				Async:      true,
				Enabled:    true,
			},
		},
		{
			ID:         "approval-resolved-callback-webhook",
			Kind:       TemplateKindHook,
			Category:   "approval",
			Name:       "Approval Resolved Callback Webhook",
			Headline:   "审批决策完成后回调业务系统或外部控制面",
			Summary:    "Send approval resolution events to a downstream business system, orchestration layer, or audit endpoint when a ticket is approved or denied.",
			Outcome:    "把审批引擎与业务系统解耦：HopClaw 只发标准化审批结果事件，下游自行决定怎么续流、落审计、关工单或更新业务状态。",
			Audience:   "平台工程、审批流集成、企业应用对接",
			Complexity: "starter",
			Tags:       []string{"hook", "approval", "callback", "integration", "enterprise"},
			SetupHints: []string{
				"Use the callback target to correlate approval_id, status, resolved_by, policy_summary, and external references with your business object.",
				"If the downstream system needs a vendor-specific shape, place a thin internal relay in front of it instead of coupling the runtime directly.",
				"For local testing, run python3 ./scripts/hooks/serve-sample-webhook.py --port 8787 --outbox-dir /tmp/hopclaw-webhook-inbox and point this template at http://127.0.0.1:8787/approval/resolved.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "url", Label: "Webhook URL", Required: true, Placeholder: "https://example.com/approval/callbacks/hopclaw", Help: "The endpoint that should receive approval resolution callbacks."},
			},
			HookDefaults: &HookTemplateDefaults{
				Name:       "Approval resolved callback",
				Trigger:    "approval.resolved",
				Kind:       "http",
				Phase:      "post",
				RetryCount: 3,
				TimeoutSec: 30,
				Async:      true,
				Enabled:    true,
			},
		},
		{
			ID:         "tool-failure-alert-hook",
			Kind:       TemplateKindHook,
			Category:   "ops",
			Name:       "Tool Failure Alert Hook",
			Headline:   "工具执行失败时立刻通知外部告警系统",
			Summary:    "Send post-tool failure events to an operator endpoint or command so repeated execution issues are visible immediately.",
			Outcome:    "适合把 runtime 的失败信号接入企业告警或值班流程。",
			Audience:   "运维、平台治理、SRE",
			Complexity: "intermediate",
			Tags:       []string{"hook", "alert", "tool", "failure"},
			SetupHints: []string{
				"Prefer a dedicated incident endpoint or wrapper script that can de-duplicate alerts.",
				"Combine this with recent error signatures in the UI to identify repeating failures quickly.",
			},
			RequiredFields: []TemplateFieldHint{
				{Field: "command", Label: "Command", Required: true, Placeholder: "notify-ops.sh", Help: "Use a local script or HTTP webhook to forward the failure."},
			},
			HookDefaults: &HookTemplateDefaults{
				Name:       "Tool failure alert",
				Trigger:    "after.tool_call",
				Kind:       "command",
				Phase:      "post",
				Command:    "notify-ops.sh",
				RetryCount: 1,
				TimeoutSec: 20,
				Async:      true,
				Enabled:    true,
			},
		},
	}
}
