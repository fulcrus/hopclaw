package agent

import (
	"strings"
	"testing"
)

func TestFallbackTaskContractMentionsBrowserReferenceSupportsNaturalLanguage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		message string
		want    bool
	}{
		{
			name:    "explicit url",
			message: "Open https://example.com/dashboard and inspect it.",
			want:    true,
		},
		{
			name:    "canonical current page context",
			message: "Current page context | https://example.com/dashboard | title=Dashboard | session=sess-1",
			want:    true,
		},
		{
			name:    "browser eval marker",
			message: "Use browser.eval to inspect window.location.href on the active page.",
			want:    true,
		},
		{
			name:    "plain page wording alone",
			message: "Capture page info for me.",
			want:    true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := fallbackTaskContractMentionsBrowserReference(strings.ToLower(tc.message)); got != tc.want {
				t.Fatalf("fallbackTaskContractMentionsBrowserReference(%q) = %v, want %v", tc.message, got, tc.want)
			}
		})
	}
}

func TestFallbackTaskContractMentionsStayOnPageSupportsNaturalLanguage(t *testing.T) {
	t.Parallel()

	if !fallbackTaskContractMentionsStayOnPageIntent(strings.ToLower("Current page context | https://example.com/dashboard | title=Dashboard")) {
		t.Fatal("expected canonical current-page summary marker to stay true")
	}
	if !fallbackTaskContractMentionsStayOnPageIntent(strings.ToLower("Stay on the current page and do not analyze it.")) {
		t.Fatal("expected natural-language keep-page wording to stay true")
	}
}

func TestFallbackTaskContractMentionsExternalSubmissionTechnicalMarkers(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Open the form and fill input[name=email] with qa@example.com.",
		"Submit via button[type=submit] after completing the required fields.",
		"Use selector=form#signup and autocomplete=email to complete the browser flow.",
	}

	for _, message := range cases {
		if !fallbackTaskContractMentionsExternalSubmission(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsExternalSubmission(%q) = false, want true", message)
		}
	}
}

func TestFallbackTaskContractMentionsExternalSubmissionPlainFormVerbsDoNotCount(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Please fill the form and submit it for me.",
		"请填写表单并提交。",
		"Apply for the conference on the website for me.",
		"Please reserve the booking and check out for me.",
		"请帮我预约报名这个活动。",
		"Please purchase the item in my cart for me.",
		"请帮我下单购买这个商品。",
	}

	for _, message := range cases {
		if fallbackTaskContractMentionsExternalSubmission(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsExternalSubmission(%q) = true, want false", message)
		}
	}
}

func TestFallbackTaskContractMentionsDeploymentTechnicalMarkers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		fn      func(string) bool
		message string
	}{
		{
			name:    "explicit deployment via kubectl",
			fn:      fallbackTaskContractMentionsExplicitDeployment,
			message: "Use kubectl apply -f deploy.yaml against the cluster.",
		},
		{
			name:    "publish via helm",
			fn:      fallbackTaskContractMentionsPublishIntent,
			message: "Run helm upgrade api oci://registry.example.com/charts/api.",
		},
		{
			name:    "deployable artifact via oci",
			fn:      fallbackTaskContractMentionsDeployableArtifact,
			message: "Promote oci://registry.example.com/charts/api with digest sha256:abcd.",
		},
		{
			name:    "deployment target via k8s namespace",
			fn:      fallbackTaskContractMentionsDeploymentTarget,
			message: "Ship this release to the k8s namespace payments-staging.",
		},
		{
			name:    "deployment target via env flag",
			fn:      fallbackTaskContractMentionsDeploymentTarget,
			message: "Run deployctl release --env staging for this rollout.",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !tc.fn(strings.ToLower(tc.message)) {
				t.Fatalf("%s(%q) = false, want true", tc.name, tc.message)
			}
		})
	}
}

func TestFallbackTaskContractMentionsDeploymentNaturalLanguagePhrasesCount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		fn      func(string) bool
		message string
	}{
		{
			name:    "plain deployment request",
			fn:      fallbackTaskContractMentionsExplicitDeployment,
			message: "Deploy the latest build to staging.",
		},
		{
			name:    "plain publish request",
			fn:      fallbackTaskContractMentionsPublishIntent,
			message: "Publish this release for production.",
		},
		{
			name:    "plain artifact wording",
			fn:      fallbackTaskContractMentionsDeployableArtifact,
			message: "Ship the service binary to the production app.",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !tc.fn(strings.ToLower(tc.message)) {
				t.Fatalf("%s(%q) = false, want true", tc.name, tc.message)
			}
		})
	}
}

func TestFallbackTaskContractMentionsDevelopmentTechnicalMarkers(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Run go test ./... after apply_patch updates.",
		"Update package.json and rerun npm test.",
		"Use fs.edit to adjust the Dockerfile before cargo test.",
	}

	for _, message := range cases {
		if !fallbackTaskContractMentionsDevelopmentIntent(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsDevelopmentIntent(%q) = false, want true", message)
		}
	}
}

func TestFallbackTaskContractMentionsDevelopmentNaturalLanguagePhrasesCount(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Implement the feature and refactor the module.",
		"请继续开发这个功能。",
		"Fix the bug in the service.",
	}

	for _, message := range cases {
		if !fallbackTaskContractMentionsDevelopmentIntent(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsDevelopmentIntent(%q) = false, want true", message)
		}
	}
}

func TestFallbackTaskContractMentionsWriteupTechnicalMarkers(t *testing.T) {
	t.Parallel()

	if !fallbackTaskContractMentionsStructuredWriteup(strings.ToLower("Use format=markdown and output=md for the final artifact.")) {
		t.Fatal("expected structured writeup marker to stay true")
	}
	if !fallbackTaskContractMentionsSpreadsheetDeliverable(strings.ToLower("Set format=xlsx for the export.")) {
		t.Fatal("expected spreadsheet format marker to stay true")
	}
	if !fallbackTaskContractMentionsDocumentDeliverable(strings.ToLower("Use output=docx for the final export.")) {
		t.Fatal("expected document output marker to stay true")
	}
	if !fallbackTaskContractMentionsPresentationDeliverable(strings.ToLower("Produce the deck with format=pptx.")) {
		t.Fatal("expected presentation format marker to stay true")
	}
	if !fallbackTaskContractMentionsArtifactOutput(strings.ToLower("artifact://reports/daily-brief with output=markdown")) {
		t.Fatal("expected structured artifact output marker to stay true")
	}
}

func TestFallbackTaskContractMentionsWriteupNaturalLanguagePhrasesCount(t *testing.T) {
	t.Parallel()

	if !fallbackTaskContractMentionsStructuredWriteup(strings.ToLower("Summarize it into a markdown report.")) {
		t.Fatal("expected plain markdown writeup wording to stay true")
	}
	if !fallbackTaskContractMentionsSpreadsheetDeliverable(strings.ToLower("整理成 spreadsheet 表格")) {
		t.Fatal("expected plain spreadsheet wording to stay true")
	}
	if !fallbackTaskContractMentionsDocumentDeliverable(strings.ToLower("输出一个文档报告")) {
		t.Fatal("expected plain document wording to stay true")
	}
	if !fallbackTaskContractMentionsPresentationDeliverable(strings.ToLower("整理成演示文稿")) {
		t.Fatal("expected plain presentation wording to stay true")
	}
	if !fallbackTaskContractMentionsArtifactOutput(strings.ToLower("Please save the summary for me.")) {
		t.Fatal("expected plain output verbs to stay true")
	}
}

func TestFallbackTaskContractMentionsWorkspaceNaturalLanguagePhrasesCount(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Please edit this file for me.",
		"把这个文件改一下。",
		"Rewrite the current document.",
	}

	for _, message := range cases {
		lower := strings.ToLower(message)
		if !fallbackTaskContractMentionsAmbiguousWorkspaceTarget(lower) {
			t.Fatalf("fallbackTaskContractMentionsAmbiguousWorkspaceTarget(%q) = false, want true", message)
		}
		if !fallbackTaskContractMentionsWorkspaceChange(lower) {
			t.Fatalf("fallbackTaskContractMentionsWorkspaceChange(%q) = false, want true", message)
		}
	}
}

func TestFallbackTaskContractMentionsInvestigationTechnicalMarkers(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Review the stack trace and git diff before collecting logs.",
		"Inspect the cpu profile and benchmark latency regression.",
	}

	for _, message := range cases {
		if !fallbackTaskContractMentionsInvestigation(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsInvestigation(%q) = false, want true", message)
		}
	}
}

func TestFallbackTaskContractMentionsInvestigationNaturalLanguagePhrasesCount(t *testing.T) {
	t.Parallel()

	positiveCases := []string{
		"Research this problem and analyze the architecture.",
		"请调研一下这个风险并分析原因。",
	}
	for _, message := range positiveCases {
		if !fallbackTaskContractMentionsInvestigation(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsInvestigation(%q) = false, want true", message)
		}
	}

	negativeCases := []string{
		"Update the profile page and keep the current avatar unchanged.",
	}
	for _, message := range negativeCases {
		if fallbackTaskContractMentionsInvestigation(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsInvestigation(%q) = true, want false", message)
		}
	}
}

func TestFallbackTaskContractMentionsExternalDeliveryTechnicalMarkers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		fn      func(string) bool
		message string
	}{
		{
			name:    "external notification via webhook",
			fn:      fallbackTaskContractMentionsExternalNotification,
			message: "Notify the downstream system through webhook delivery.",
		},
		{
			name:    "external notification via smtp",
			fn:      fallbackTaskContractMentionsExternalNotification,
			message: "Send the alert through smtp relay.",
		},
		{
			name:    "external delivery target via slack",
			fn:      fallbackTaskContractMentionsExternalDeliveryTarget,
			message: "Post the summary to slack #ops.",
		},
		{
			name:    "external delivery target via feishu",
			fn:      fallbackTaskContractMentionsExternalDeliveryTarget,
			message: "Deliver the incident notice to feishu webhook.",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !tc.fn(strings.ToLower(tc.message)) {
				t.Fatalf("%s(%q) = false, want true", tc.name, tc.message)
			}
		})
	}
}

func TestFallbackTaskContractMentionsExternalNotificationPlainVerbsDoNotCount(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Notify the recipient when the report is ready.",
		"提醒我在报告准备好时发出去。",
	}

	for _, message := range cases {
		if fallbackTaskContractMentionsExternalNotification(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsExternalNotification(%q) = true, want false", message)
		}
	}
}

func TestFallbackTaskContractMentionsExternalDeliveryTargetBareAtDoesNotCount(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Keep the @decorator exactly as-is in the code example.",
		"解释一下 @Published 这个属性包装器的作用。",
	}

	for _, message := range cases {
		if fallbackTaskContractMentionsExternalDeliveryTarget(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsExternalDeliveryTarget(%q) = true, want false", message)
		}
	}
}

func TestFallbackTaskContractMentionsExternalDeliveryTargetLocalizedNamesCount(t *testing.T) {
	t.Parallel()

	cases := []string{
		"把结果发到飞书给我。",
		"请同步到企业微信群。",
	}

	for _, message := range cases {
		if !fallbackTaskContractMentionsExternalDeliveryTarget(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsExternalDeliveryTarget(%q) = false, want true", message)
		}
	}
}

func TestFallbackTaskContractMentionsScheduledExecutionTechnicalMarkers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		fn      func(string) bool
		message string
	}{
		{
			name:    "scheduled execution via cron",
			fn:      fallbackTaskContractMentionsScheduledExecution,
			message: "Use cron 0 * * * * for this monitor.",
		},
		{
			name:    "scheduled execution via rrule",
			fn:      fallbackTaskContractMentionsScheduledExecution,
			message: "Run with rrule:FREQ=HOURLY;INTERVAL=1.",
		},
		{
			name:    "automation intent via cron",
			fn:      fallbackTaskContractMentionsAutomationIntent,
			message: "This workflow should run under cron scheduling.",
		},
		{
			name:    "scheduled execution via structured schedule kind",
			fn:      fallbackTaskContractMentionsScheduledExecution,
			message: "Create the job with --schedule-kind cron and a cron expression.",
		},
		{
			name:    "browser watch intent via watch marker",
			fn:      fallbackTaskContractMentionsBrowserWatchIntent,
			message: "Keep this browser task under watch.poll with an hourly cadence.",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !tc.fn(strings.ToLower(tc.message)) {
				t.Fatalf("%s(%q) = false, want true", tc.name, tc.message)
			}
		})
	}
}

func TestFallbackTaskContractMentionsMonitorNaturalLanguagePhrasesCount(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Please monitor the site and notify me when it changes.",
		"请监控这个页面，有变化就提醒我。",
	}

	for _, message := range cases {
		lower := strings.ToLower(message)
		if !fallbackTaskContractMentionsMonitorIntent(lower) {
			t.Fatalf("fallbackTaskContractMentionsMonitorIntent(%q) = false, want true", message)
		}
		if !fallbackTaskContractMentionsBrowserWatchIntent(lower) {
			t.Fatalf("fallbackTaskContractMentionsBrowserWatchIntent(%q) = false, want true", message)
		}
	}
}

func TestFallbackTaskContractMentionsScheduleNaturalLanguagePhrasesCount(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Check the page every hour and tell me here when it changes.",
		"每天早上 9 点检查这个页面。",
		"Start now and repeat daily.",
	}

	for _, message := range cases {
		lower := strings.ToLower(message)
		if !fallbackTaskContractMentionsAutomationIntent(lower) {
			t.Fatalf("fallbackTaskContractMentionsAutomationIntent(%q) = false, want true", message)
		}
		if !fallbackTaskContractMentionsScheduledExecution(lower) {
			t.Fatalf("fallbackTaskContractMentionsScheduledExecution(%q) = false, want true", message)
		}
		if !fallbackTaskContractMentionsScheduleReference(lower) {
			t.Fatalf("fallbackTaskContractMentionsScheduleReference(%q) = false, want true", message)
		}
	}
}

func TestFallbackTaskContractMentionsCancellationTechnicalMarkers(t *testing.T) {
	t.Parallel()

	cases := []string{
		"/cancel",
		"Use watch.cancel for this monitoring job.",
		"Run cron.remove on the existing schedule.",
		`{"action":"cancel","target_ref":"example.com"}`,
	}

	for _, message := range cases {
		if !fallbackTaskContractMentionsCancellation(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsCancellation(%q) = false, want true", message)
		}
	}
}

func TestFallbackTaskContractMentionsCancellationNaturalLanguagePhrasesCount(t *testing.T) {
	t.Parallel()

	cases := []string{
		"取消刚才那个监控提醒",
		"停掉所有和 example.com 相关的监控",
		"Stop the current watch job.",
	}

	for _, message := range cases {
		if !fallbackTaskContractMentionsCancellation(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsCancellation(%q) = false, want true", message)
		}
	}
}

func TestFallbackTaskContractMentionsDeliveryIntentTechnicalMarkers(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Use email.send to deliver the report.",
		"Post the result through channel.send to the operator bridge.",
		"Send it with mailto:ceo@example.com via smtp relay.",
	}

	for _, message := range cases {
		if !fallbackTaskContractMentionsDeliveryIntent(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsDeliveryIntent(%q) = false, want true", message)
		}
	}
}

func TestFallbackTaskContractMentionsDeliveryIntentNaturalLanguageVerbsCount(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Send the report to the recipient.",
		"把结果发给相关人。",
		"发邮件通知团队。",
	}

	for _, message := range cases {
		if !fallbackTaskContractMentionsDeliveryIntent(strings.ToLower(message)) {
			t.Fatalf("fallbackTaskContractMentionsDeliveryIntent(%q) = false, want true", message)
		}
	}
}

func TestFallbackTaskContractNegationAndCurrentConversationSupportNaturalLanguage(t *testing.T) {
	t.Parallel()

	if !fallbackTaskContractMentionsDeliveryNegation(strings.ToLower("Use email.send in dry-run mode only.")) {
		t.Fatal("expected structured delivery-disable marker to stay true")
	}
	if !fallbackTaskContractMentionsDeliveryNegation(strings.ToLower("Do not send the result externally.")) {
		t.Fatal("expected natural-language negation to stay true")
	}
	if !fallbackTaskContractMentionsCurrentConversation(strings.ToLower("Reply here with the summary only.")) {
		t.Fatal("expected natural-language current-chat phrasing to stay true")
	}
	if !fallbackTaskContractMentionsCurrentConversation(strings.ToLower(`{"delivery_target":"current_conversation"}`)) {
		t.Fatal("expected structured current-conversation marker to stay true")
	}
}
