package agent

import (
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/contextengine"
)

func TestRecentBrowserReferenceContextUsesLatestBrowserToolPayload(t *testing.T) {
	t.Parallel()

	context := recentBrowserReferenceContext([]contextengine.Message{
		{
			Role:    contextengine.RoleTool,
			Name:    "browser.snapshot",
			Content: `{"url":"https://httpbin.org/forms/post","title":"HTTPBin Form","session_id":"sess-1"}`,
		},
	})
	if !strings.Contains(context, "https://httpbin.org/forms/post") {
		t.Fatalf("context = %q, want page url", context)
	}
	if !strings.Contains(context, "Current page context") {
		t.Fatalf("context = %q, want current page marker", context)
	}
}

func TestRecentBrowserReferenceContextParsesCanonicalSummaryMarker(t *testing.T) {
	t.Parallel()

	context := recentBrowserReferenceContext([]contextengine.Message{
		{
			Role:    contextengine.RoleSystem,
			Name:    "session-summary",
			Content: "Current page context | https://httpbin.org/forms/post | title=HTTPBin Form | session=sess-1",
		},
	})
	if !strings.Contains(context, "https://httpbin.org/forms/post") {
		t.Fatalf("context = %q, want page url", context)
	}
	if !strings.Contains(context, "Current page context") {
		t.Fatalf("context = %q, want current page marker", context)
	}
}

func TestBrowserReferenceContextFromSummarySupportsCanonicalFormats(t *testing.T) {
	t.Parallel()

	for _, summary := range []string{
		"Current page context | https://example.com | title=Example Domain | session=sess-1",
		"Current page context: https://example.com",
	} {
		ctx, ok := browserReferenceContextFromSummary(summary)
		if !ok {
			t.Fatalf("browserReferenceContextFromSummary(%q) = not ok", summary)
		}
		if got := ctx.URL; got != "https://example.com" {
			t.Fatalf("browserReferenceContextFromSummary(%q).URL = %q, want https://example.com", summary, got)
		}
	}
}

func TestSessionBrowserReusePromptForSearchResultsForbidsReaskingQuery(t *testing.T) {
	t.Parallel()

	session := &Session{
		Session: contextengine.Session{
			Summary: "Current page context | https://www.bing.com/search?q=openai | title=openai - Search | session=sess-search",
		},
	}
	prompt := sessionBrowserReusePrompt(session, "打开页面，等搜索结果加载出来，再提取前 5 条")
	if !strings.Contains(prompt, "Do not ask the user to restate or confirm the current search query") {
		t.Fatalf("prompt = %q, want search-query reuse guidance", prompt)
	}
}

func TestMessageCanReuseSessionBrowserReferenceRejectsGenericWebsiteMention(t *testing.T) {
	t.Parallel()

	summary := "Current page context | https://example.com/dashboard | title=Dashboard | session=sess-1"
	if messageCanReuseSessionBrowserReference("Deploy the website to staging.", summary) {
		t.Fatal("expected generic website mention not to reuse current-page browser context")
	}
}
