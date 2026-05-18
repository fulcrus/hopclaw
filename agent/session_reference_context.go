package agent

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
)

const maxBrowserReferenceSignals = 64
const browserReferenceSummaryPrefix = "Current page context"

const browserSessionReusePrompt = `<browser_context_rules>
A current page context already exists in this session.
For follow-up browser tasks, reuse the current page instead of asking the user for the URL again.
Only ask for a new URL when the user explicitly requests a different page or a fresh navigation/search target.
If the current page context already shows a loaded search-results page, treat that current page and query as the active target.
Do not ask the user to restate or confirm the current search query unless the page context is missing or genuinely ambiguous.
Use browser.* tools against the current page for inspection, interaction, extraction, and screenshots.
Prefer structured browser tools such as browser.snapshot, browser.screenshot, browser.snapshot_aria, browser.click_aria, and browser.type_aria for ordinary inspection and interaction on the current page.
Use browser.element_* only for small targeted selectors, not for broad page-wide extraction on large search-result pages or heavy documents.
Do not use browser.eval for ordinary inspection, clicking, typing, or form submission. Reserve browser.eval for explicit JavaScript/script requests when no structured browser tool can do the job.
Do not refetch the same current page with net.* tools or shell/exec tools unless the user explicitly asks for that path.
</browser_context_rules>`

type browserReferenceContext struct {
	URL       string
	Title     string
	SessionID string
}

func sessionReferenceSummary(session *Session) string {
	if session == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if summary := strings.TrimSpace(session.Summary); summary != "" {
		parts = append(parts, summary)
	}
	if browser := recentBrowserReferenceContext(session.Messages); browser != "" && !containsCaseInsensitive(strings.Join(parts, "\n"), browser) {
		parts = append(parts, browser)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func persistBrowserReferenceSummary(session *Session) {
	if session == nil {
		return
	}
	browser := strings.TrimSpace(recentBrowserReferenceContext(session.Messages))
	if browser == "" {
		return
	}
	summary := strings.TrimSpace(session.Summary)
	if containsCaseInsensitive(summary, browser) {
		if session.SummaryAt.IsZero() {
			session.SummaryAt = time.Now().UTC()
		}
		return
	}

	lines := strings.Split(summary, "\n")
	filtered := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), strings.ToLower("Current page context")) {
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		filtered = append(filtered, line)
	}
	filtered = append(filtered, browser)
	session.Summary = strings.TrimSpace(strings.Join(filtered, "\n"))
	session.SummaryAt = time.Now().UTC()
}

func sessionBrowserReusePrompt(session *Session, latestMessage string) string {
	if session == nil {
		return ""
	}
	summary := sessionReferenceSummary(session)
	if !messageCanReuseSessionBrowserReference(latestMessage, summary) {
		return ""
	}
	if !sessionHasBrowserReferenceContext(summary) {
		return ""
	}
	return browserSessionReusePrompt
}

func recentBrowserReferenceContext(messages []contextengine.Message) string {
	if len(messages) == 0 {
		return ""
	}

	var ctx browserReferenceContext

	for i, seen := len(messages)-1, 0; i >= 0 && seen < maxBrowserReferenceSignals; i-- {
		msg := messages[i]
		switch msg.Role {
		case contextengine.RoleTool:
			if !strings.HasPrefix(strings.TrimSpace(msg.Name), "browser.") {
				continue
			}
			seen++
			candidate, ok := browserReferenceContextFromPayload(parseReferencePayload(msg.TextContent()))
			if !ok {
				continue
			}
			ctx = mergeBrowserReferenceContext(ctx, candidate)
		default:
			if candidate, ok := browserReferenceContextFromSummary(msg.TextContent()); ok {
				seen++
				ctx = mergeBrowserReferenceContext(ctx, candidate)
			}
		}
		if ctx.complete() {
			break
		}
	}

	if !ctx.present() {
		return ""
	}

	return ctx.summaryLine()
}

func parseReferencePayload(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	return payload
}

func firstReferenceValue(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(strings.TrimSpace(strings.Trim(strings.TrimSpace(stringifyReferenceValue(payload[key])), `"`)))
		if value != "" {
			return value
		}
	}
	return ""
}

func stringifyReferenceValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func browserReferenceContextFromPayload(payload map[string]any) (browserReferenceContext, bool) {
	if len(payload) == 0 {
		return browserReferenceContext{}, false
	}
	ctx := browserReferenceContext{
		URL:       firstReferenceValue(payload, "url", "final_url", "page_url", "source_url"),
		Title:     firstReferenceValue(payload, "title"),
		SessionID: firstReferenceValue(payload, "session_id"),
	}
	if !ctx.present() {
		return browserReferenceContext{}, false
	}
	return ctx, true
}

func browserReferenceContextFromSummary(summary string) (browserReferenceContext, bool) {
	lines := strings.Split(strings.TrimSpace(summary), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || !hasBrowserReferenceSummaryPrefix(line) {
			continue
		}
		ctx := parseBrowserReferenceSummaryLine(line)
		if ctx.present() {
			return ctx, true
		}
	}
	return browserReferenceContext{}, false
}

func hasBrowserReferenceSummaryPrefix(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(lower, strings.ToLower(browserReferenceSummaryPrefix+" |")) ||
		strings.HasPrefix(lower, strings.ToLower(browserReferenceSummaryPrefix+":"))
}

func parseBrowserReferenceSummaryLine(line string) browserReferenceContext {
	line = strings.TrimSpace(line)
	if line == "" {
		return browserReferenceContext{}
	}
	if strings.HasPrefix(strings.ToLower(line), strings.ToLower(browserReferenceSummaryPrefix+":")) {
		line = strings.TrimSpace(line[len(browserReferenceSummaryPrefix)+1:])
		if line == "" {
			return browserReferenceContext{}
		}
		ctx := browserReferenceContext{URL: strings.TrimSpace(line)}
		if taskContractURLPattern.MatchString(ctx.URL) {
			return ctx
		}
		return browserReferenceContext{}
	}
	parts := strings.Split(line, "|")
	ctx := browserReferenceContext{}
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(strings.ToLower(part), "title="):
			ctx.Title = strings.TrimSpace(part[len("title="):])
		case strings.HasPrefix(strings.ToLower(part), "session="):
			ctx.SessionID = strings.TrimSpace(part[len("session="):])
		case ctx.URL == "":
			ctx.URL = strings.TrimSpace(part)
		}
	}
	return ctx
}

func mergeBrowserReferenceContext(base, candidate browserReferenceContext) browserReferenceContext {
	if base.URL == "" {
		base.URL = candidate.URL
	}
	if base.Title == "" {
		base.Title = candidate.Title
	}
	if base.SessionID == "" {
		base.SessionID = candidate.SessionID
	}
	return base
}

func (c browserReferenceContext) present() bool {
	return strings.TrimSpace(c.URL) != "" ||
		strings.TrimSpace(c.Title) != "" ||
		strings.TrimSpace(c.SessionID) != ""
}

func (c browserReferenceContext) complete() bool {
	return strings.TrimSpace(c.URL) != "" &&
		strings.TrimSpace(c.Title) != "" &&
		strings.TrimSpace(c.SessionID) != ""
}

func (c browserReferenceContext) summaryLine() string {
	if !c.present() {
		return ""
	}
	parts := []string{browserReferenceSummaryPrefix}
	if strings.TrimSpace(c.URL) != "" {
		parts = append(parts, strings.TrimSpace(c.URL))
	}
	if strings.TrimSpace(c.Title) != "" {
		parts = append(parts, "title="+strings.TrimSpace(c.Title))
	}
	if strings.TrimSpace(c.SessionID) != "" {
		parts = append(parts, "session="+strings.TrimSpace(c.SessionID))
	}
	return strings.Join(parts, " | ")
}

func normalizeBrowserReferenceURL(value string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(value), "/"))
}

func browserReferenceContextLooksLikeSearchResults(ctx browserReferenceContext) bool {
	return browserURLLooksLikeSearchResults(ctx.URL)
}

func containsCaseInsensitive(text, needle string) bool {
	text = strings.TrimSpace(text)
	needle = strings.TrimSpace(needle)
	if text == "" || needle == "" {
		return false
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(needle))
}
