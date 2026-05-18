package gateway

import (
	"strings"
	"testing"
)

func TestWebChatChatViewEmbedsImagePayloadFlow(t *testing.T) {
	t.Parallel()

	body, err := webChatUIFS.ReadFile("webchat-ui/js/views/chat.js")
	if err != nil {
		t.Fatalf("ReadFile(chat.js) error = %v", err)
	}
	source := string(body)

	for _, snippet := range []string{
		"pendingFileDataURI",
		"attachmentReading",
		"setAttachment(fi.files[0]);",
		"attachmentContentBlocks(uploadResp)",
		"buildSubmissionContentBlocks(text, this.attachmentContentBlocks(data))",
		"submitBody.content_blocks = contentBlocks;",
		"if ((!text && !this.attachment) || this.streaming) return;",
		"const previewText = text || attachmentPreviewText(this.attachment);",
		"parseDataURI(uri)",
		"self.setAttachment(e.dataTransfer.files[0]);",
	} {
		if !strings.Contains(source, snippet) {
			t.Fatalf("chat.js missing snippet %q", snippet)
		}
	}
	if strings.Contains(source, "submitBody.images = [this.pendingFileDataURI];") {
		t.Fatal("chat.js should not fallback to submitBody.images for uploads")
	}
	if strings.Contains(source, "metadata.attachments") {
		t.Fatal("chat.js should not encode uploads via metadata.attachments")
	}
}

func TestWebChatRunsViewUsesStructuredRetryInteraction(t *testing.T) {
	t.Parallel()

	body, err := webChatUIFS.ReadFile("webchat-ui/js/views/runs.js")
	if err != nil {
		t.Fatalf("ReadFile(runs.js) error = %v", err)
	}
	source := string(body)

	for _, snippet := range []string{
		"function retryInteractionPayload(run)",
		"structured_command:",
		"kind: 'retry'",
		"run_id: runID",
		"return api.post('/runtime/interact', retryInteractionPayload(run));",
	} {
		if !strings.Contains(source, snippet) {
			t.Fatalf("runs.js missing snippet %q", snippet)
		}
	}
	if strings.Contains(source, "Please retry the previous task.") {
		t.Fatal("runs.js should not encode retry as a natural-language task prompt")
	}
}

func TestWebChatChatViewUsesStructuredRetryForRunMessages(t *testing.T) {
	t.Parallel()

	body, err := webChatUIFS.ReadFile("webchat-ui/js/views/chat.js")
	if err != nil {
		t.Fatalf("ReadFile(chat.js) error = %v", err)
	}
	source := string(body)

	for _, snippet := range []string{
		"function retryInteractionPayloadForRun(sessionKey, runID)",
		"structured_command:",
		"kind: 'retry'",
		"run_id: trimmedRunID",
		"const payload = retryInteractionPayloadForRun(store.sessionKey, runID);",
		"payload.metadata = buildInteractionMetadata(turnId);",
		"api.post('/runtime/interact', payload).then(result => {",
	} {
		if !strings.Contains(source, snippet) {
			t.Fatalf("chat.js missing snippet %q", snippet)
		}
	}
}

func TestWebChatChatViewStreamsRunlessTurnsByInteractionTurnID(t *testing.T) {
	t.Parallel()

	body, err := webChatUIFS.ReadFile("webchat-ui/js/views/chat.js")
	if err != nil {
		t.Fatalf("ReadFile(chat.js) error = %v", err)
	}
	source := string(body)

	for _, snippet := range []string{
		"function newInteractionTurnId()",
		"function buildInteractionMetadata(turnId)",
		"metadata: buildInteractionMetadata(turnId)",
		"const eventTurnId = String((ev.attrs && ev.attrs.interaction_turn_id) || '').trim();",
		"const isCurrentTurn = this.streaming && !this.runId && !!this.interactionTurnId && eventTurnId === this.interactionTurnId;",
		"case 'model.stream_complete':",
		"this.finalizeRunlessTurn(this.interactionTurnId);",
		"finalizeRunlessTurn(turnId)",
	} {
		if !strings.Contains(source, snippet) {
			t.Fatalf("chat.js missing snippet %q", snippet)
		}
	}
}

func TestWebChatUIIncludesThemePreferenceSupport(t *testing.T) {
	t.Parallel()

	appBody, err := webChatUIFS.ReadFile("webchat-ui/js/app.js")
	if err != nil {
		t.Fatalf("ReadFile(app.js) error = %v", err)
	}
	settingsBody, err := webChatUIFS.ReadFile("webchat-ui/js/views/settings.js")
	if err != nil {
		t.Fatalf("ReadFile(settings.js) error = %v", err)
	}
	cssBody, err := webChatUIFS.ReadFile("webchat-ui/css/app.css")
	if err != nil {
		t.Fatalf("ReadFile(app.css) error = %v", err)
	}

	for _, snippet := range []string{
		"const THEME_STORAGE_KEY = 'hc_theme';",
		"const initialTheme = readStoredTheme();",
		"document.documentElement.setAttribute('data-theme', initialTheme);",
		"theme: initialTheme,",
	} {
		if !strings.Contains(string(appBody), snippet) {
			t.Fatalf("app.js missing snippet %q", snippet)
		}
	}

	for _, snippet := range []string{
		`data-testid="settings-theme-toggle"`,
		`themeOptions: ['light', 'dark']`,
		"localStorage.setItem('hc_theme', next);",
	} {
		if !strings.Contains(string(settingsBody), snippet) {
			t.Fatalf("settings.js missing snippet %q", snippet)
		}
	}

	if !strings.Contains(string(cssBody), `[data-theme="dark"]`) {
		t.Fatal("app.css missing dark theme variable block")
	}
}

func TestWebChatUIAppShowsGovernanceDeadLetterToastsFromGlobalSSE(t *testing.T) {
	t.Parallel()

	appBody, err := webChatUIFS.ReadFile("webchat-ui/js/app.js")
	if err != nil {
		t.Fatalf("ReadFile(app.js) error = %v", err)
	}
	source := string(appBody)

	for _, snippet := range []string{
		"const GLOBAL_SSE_RECONNECT_MS = 5000;",
		"function isGovernanceDeadLetterEvent(event)",
		"function governanceDeadLetterToastMessage(event)",
		"function startConsoleEventStream()",
		"consolePath('/sse')",
		"rememberGovernanceDeadLetterAlert(alertKey);",
		"showToast(governanceDeadLetterToastMessage(event), 'warning');",
	} {
		if !strings.Contains(source, snippet) {
			t.Fatalf("app.js missing snippet %q", snippet)
		}
	}
}

func TestWebChatUIAppBootstrapsConfigViaConsolePath(t *testing.T) {
	t.Parallel()

	appBody, err := webChatUIFS.ReadFile("webchat-ui/js/app.js")
	if err != nil {
		t.Fatalf("ReadFile(app.js) error = %v", err)
	}
	source := string(appBody)

	if !strings.Contains(source, "api.get(consolePath('/api/config') + (query ? '?' + query : ''), { background: true });") {
		t.Fatal("app.js should bootstrap config via consolePath('/api/config')")
	}
	if strings.Contains(source, "api.get('/dashboard/api/config'") {
		t.Fatal("app.js should not hardcode /dashboard/api/config")
	}
}

func TestWebChatUIUsesSharedConsoleAssetPaths(t *testing.T) {
	t.Parallel()

	indexBody, err := webChatUIFS.ReadFile("webchat-ui/index.html")
	if err != nil {
		t.Fatalf("ReadFile(index.html) error = %v", err)
	}
	indexSource := string(indexBody)
	for _, snippet := range []string{
		`<link rel="manifest" href="manifest.json">`,
		`<link rel="icon" href="icon-192.svg" type="image/svg+xml">`,
		`<link rel="apple-touch-icon" href="icon-192.svg">`,
	} {
		if !strings.Contains(indexSource, snippet) {
			t.Fatalf("index.html missing snippet %q", snippet)
		}
	}
	if strings.Contains(indexSource, `/dashboard/`) {
		t.Fatal("index.html should not hardcode /dashboard asset paths")
	}

	manifestBody, err := webChatUIFS.ReadFile("webchat-ui/manifest.json")
	if err != nil {
		t.Fatalf("ReadFile(manifest.json) error = %v", err)
	}
	manifestSource := string(manifestBody)
	for _, snippet := range []string{
		`"start_url": "./"`,
		`"scope": "./"`,
		`"src": "icon-192.svg"`,
		`"src": "icon-512.svg"`,
	} {
		if !strings.Contains(manifestSource, snippet) {
			t.Fatalf("manifest.json missing snippet %q", snippet)
		}
	}
	if strings.Contains(manifestSource, `/dashboard/`) {
		t.Fatal("manifest.json should not hardcode /dashboard asset paths")
	}
}

func TestWebChatApprovalsUsesConsolePathForNotifications(t *testing.T) {
	t.Parallel()

	body, err := webChatUIFS.ReadFile("webchat-ui/js/views/approvals.js")
	if err != nil {
		t.Fatalf("ReadFile(approvals.js) error = %v", err)
	}
	source := string(body)

	for _, snippet := range []string{
		"import { api, consolePath, getToken, showToast } from '../api.js';",
		"icon: consolePath('/icon-192.svg'),",
	} {
		if !strings.Contains(source, snippet) {
			t.Fatalf("approvals.js missing snippet %q", snippet)
		}
	}
	if strings.Contains(source, "icon: '/dashboard/icon-192.svg'") {
		t.Fatal("approvals.js should not hardcode /dashboard notification icon path")
	}
}

func TestWebChatApprovalViewsConsumeApprovalViewAndDottedEvents(t *testing.T) {
	t.Parallel()

	approvalsBody, err := webChatUIFS.ReadFile("webchat-ui/js/views/approvals.js")
	if err != nil {
		t.Fatalf("ReadFile(approvals.js) error = %v", err)
	}
	chatBody, err := webChatUIFS.ReadFile("webchat-ui/js/views/chat.js")
	if err != nil {
		t.Fatalf("ReadFile(chat.js) error = %v", err)
	}

	for _, snippet := range []string{
		"approval.requested",
		"approval.resolved",
		"ticket.tool_calls",
		"ticket.governance && Array.isArray(ticket.governance.tool_names)",
		"attrs.approval_id || attrs.ticket_id || attrs.id",
	} {
		if !strings.Contains(string(approvalsBody), snippet) {
			t.Fatalf("approvals.js missing snippet %q", snippet)
		}
	}

	for _, snippet := range []string{
		"approval.requested",
		"approval.resolved",
		"buildApprovalPopup(activeApproval)",
		"approvalPrimaryTool(ticket)",
		"ticket.governance && Array.isArray(ticket.governance.tool_names)",
	} {
		if !strings.Contains(string(chatBody), snippet) {
			t.Fatalf("chat.js missing snippet %q", snippet)
		}
	}
}

func TestWebChatOverviewUsesOperatorQualityAndEvalSurface(t *testing.T) {
	t.Parallel()

	body, err := webChatUIFS.ReadFile("webchat-ui/js/views/overview.js")
	if err != nil {
		t.Fatalf("ReadFile(overview.js) error = %v", err)
	}
	source := string(body)

	for _, snippet := range []string{
		"data-testid=\"overview-release-readiness-card\"",
		"data-testid=\"overview-quality-panel\"",
		"data-testid=\"overview-eval-run-report\"",
		"api.get('/operator/quality/summary', { background: true })",
		"api.get('/operator/quality/release-readiness', { background: true })",
		"api.get('/operator/evals/suites', { background: true })",
		"api.post('/operator/evals/run', { suite_id: suiteID })",
		"function formatPercent(value, digits = 0)",
		"evalSuiteRunTestID(suite)",
	} {
		if !strings.Contains(source, snippet) {
			t.Fatalf("overview.js missing snippet %q", snippet)
		}
	}
	if strings.Contains(source, "case_ids") {
		t.Fatal("overview.js should run eval suites without exposing case_ids selection")
	}
}
