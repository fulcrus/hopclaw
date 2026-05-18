package contextengine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/internal/support/ints"
	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
)

func TestSelectMessagesKeepAll(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		KeepFirstN: 2,
		KeepLastN:  3,
	}, nil)

	msgs := []Message{
		{Role: RoleUser, Content: "a"},
		{Role: RoleAssistant, Content: "b"},
		{Role: RoleUser, Content: "c"},
	}

	selected := engine.selectMessages(msgs)
	if len(selected) != 3 {
		t.Fatalf("expected all 3 messages (within budget), got %d", len(selected))
	}
}

func TestSelectMessagesKeepFirstAndLast(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		KeepFirstN: 1,
		KeepLastN:  2,
	}, nil)

	msgs := []Message{
		{Role: RoleUser, Content: "first"},
		{Role: RoleAssistant, Content: "dropped-1"},
		{Role: RoleUser, Content: "dropped-2"},
		{Role: RoleAssistant, Content: "dropped-3"},
		{Role: RoleUser, Content: "second-to-last"},
		{Role: RoleAssistant, Content: "last"},
	}

	selected := engine.selectMessages(msgs)
	if len(selected) != 3 {
		t.Fatalf("expected 3 messages (1 first + 2 last), got %d", len(selected))
	}
	if selected[0].Content != "first" {
		t.Fatalf("first = %q", selected[0].Content)
	}
	if selected[1].Content != "second-to-last" {
		t.Fatalf("second = %q", selected[1].Content)
	}
	if selected[2].Content != "last" {
		t.Fatalf("third = %q", selected[2].Content)
	}
}

func TestSelectMessagesKeepLastOnly(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		KeepFirstN: 0,
		KeepLastN:  2,
	}, nil)

	msgs := []Message{
		{Role: RoleUser, Content: "old"},
		{Role: RoleAssistant, Content: "older"},
		{Role: RoleUser, Content: "recent"},
		{Role: RoleAssistant, Content: "latest"},
	}

	selected := engine.selectMessages(msgs)
	if len(selected) != 2 {
		t.Fatalf("expected 2 messages (last 2), got %d", len(selected))
	}
	if selected[0].Content != "recent" {
		t.Fatalf("first = %q", selected[0].Content)
	}
}

func TestSelectMessagesEmptyInput(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{KeepLastN: 5}, nil)
	selected := engine.selectMessages(nil)
	if selected != nil {
		t.Fatalf("expected nil for empty input, got %v", selected)
	}
}

func TestTrimMessagesToBudgetDropsMiddle(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		KeepFirstN: 1,
		KeepLastN:  10,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, nil)

	msgs := []Message{
		{Role: RoleUser, Content: strings.Repeat("a", 20)},
		{Role: RoleAssistant, Content: strings.Repeat("b", 20)},
		{Role: RoleUser, Content: strings.Repeat("c", 20)},
		{Role: RoleAssistant, Content: strings.Repeat("d", 20)},
		{Role: RoleUser, Content: strings.Repeat("e", 20)},
	}

	// maxInput = 70, total = 100, system = 0, summary = ""
	trimmed := engine.trimMessagesToBudget("", "", msgs, 70)
	if len(trimmed) >= 5 {
		t.Fatalf("expected fewer messages after trim, got %d", len(trimmed))
	}
	// First message should be preserved (keepFirstN=1).
	if trimmed[0].Content != strings.Repeat("a", 20) {
		t.Fatalf("first message should be preserved: %q", trimmed[0].Content)
	}
}

func TestTrimMessagesToBudgetFitsWithinBudget(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		KeepFirstN: 0,
		KeepLastN:  10,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, nil)

	msgs := []Message{
		{Role: RoleUser, Content: "short"},
		{Role: RoleAssistant, Content: "brief"},
	}

	trimmed := engine.trimMessagesToBudget("", "", msgs, 1000)
	if len(trimmed) != 2 {
		t.Fatalf("expected 2 messages (within budget), got %d", len(trimmed))
	}
}

func TestTrimMessagesToBudgetWithSystemPrompt(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		KeepFirstN: 0,
		KeepLastN:  10,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, nil)

	msgs := []Message{
		{Role: RoleUser, Content: strings.Repeat("a", 30)},
		{Role: RoleAssistant, Content: strings.Repeat("b", 30)},
		{Role: RoleUser, Content: strings.Repeat("c", 30)},
	}

	// System prompt takes 50 tokens, leaving only 20 for messages.
	systemPrompt := strings.Repeat("s", 50)
	trimmed := engine.trimMessagesToBudget(systemPrompt, "", msgs, 70)
	if len(trimmed) >= 3 {
		t.Fatalf("expected fewer messages with large system prompt, got %d", len(trimmed))
	}
}

func TestTrimMessagesToBudgetWithSummary(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		KeepFirstN: 0,
		KeepLastN:  10,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, nil)

	msgs := []Message{
		{Role: RoleUser, Content: strings.Repeat("a", 30)},
		{Role: RoleAssistant, Content: strings.Repeat("b", 30)},
	}

	summary := strings.Repeat("z", 40)
	trimmed := engine.trimMessagesToBudget("", summary, msgs, 60)
	// Summary (40) + 2 messages (60) = 100 > 60, should trim.
	if len(trimmed) >= 2 {
		t.Fatalf("expected trimming with summary, got %d messages", len(trimmed))
	}
}

func TestTrimMessagesToBudgetEmptyMessages(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{}, nil)
	trimmed := engine.trimMessagesToBudget("", "", nil, 1000)
	if trimmed != nil {
		t.Fatalf("expected nil for empty messages, got %v", trimmed)
	}
}

func TestRepairOrphanedToolCallsDropsOrphanedResponse(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		{Role: RoleUser, Content: "do something"},
		{Role: RoleTool, ToolCallID: "orphan-1", Content: "result"},
		{Role: RoleAssistant, Content: "done"},
	}

	repaired := repairOrphanedToolCalls(msgs)
	for _, msg := range repaired {
		if msg.Role == RoleTool && msg.ToolCallID == "orphan-1" {
			t.Fatal("orphaned tool response should be dropped")
		}
	}
}

func TestRepairOrphanedToolCallsInsertsSyntheticResponse(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		{Role: RoleUser, Content: "run tool"},
		{
			Role:      RoleAssistant,
			Content:   "calling tool",
			ToolCalls: []ToolCallRef{{ID: "call-1", Name: "bash"}},
		},
		// No tool response for call-1.
		{Role: RoleUser, Content: "what happened?"},
	}

	repaired := repairOrphanedToolCalls(msgs)
	found := false
	for _, msg := range repaired {
		if msg.Role == RoleTool && msg.ToolCallID == "call-1" {
			found = true
			if !strings.Contains(msg.Content, "pending") {
				t.Fatalf("synthetic response content = %q", msg.Content)
			}
		}
	}
	if !found {
		t.Fatal("expected synthetic tool response for call-1")
	}
}

func TestRepairOrphanedToolCallsPreservesMatchedPairs(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		{
			Role:      RoleAssistant,
			ToolCalls: []ToolCallRef{{ID: "call-1", Name: "bash"}},
		},
		{Role: RoleTool, ToolCallID: "call-1", Content: "result"},
	}

	repaired := repairOrphanedToolCalls(msgs)
	if len(repaired) != 2 {
		t.Fatalf("expected 2 messages (matched pair), got %d", len(repaired))
	}
}

func TestRepairOrphanedToolCallsMultipleToolCalls(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		{
			Role: RoleAssistant,
			ToolCalls: []ToolCallRef{
				{ID: "call-1", Name: "bash"},
				{ID: "call-2", Name: "read"},
			},
		},
		{Role: RoleTool, ToolCallID: "call-1", Content: "bash result"},
		// call-2 has no response.
	}

	repaired := repairOrphanedToolCalls(msgs)
	call2Found := false
	for _, msg := range repaired {
		if msg.Role == RoleTool && msg.ToolCallID == "call-2" {
			call2Found = true
		}
	}
	if !call2Found {
		t.Fatal("expected synthetic response for call-2")
	}
}

func TestBuildSegments(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, nil)

	msgs := []Message{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi"},
	}

	segments := engine.buildSegments("system prompt", "run prompt", "skill block", "", "", "", "summary", msgs)
	if len(segments) != 5 {
		t.Fatalf("expected 5 segments, got %d", len(segments))
	}

	expectedKinds := []SegmentKind{SegmentBaseSystem, SegmentRunSystem, SegmentSkillPrompt, SegmentSummary, SegmentMessages}
	for i, kind := range expectedKinds {
		if segments[i].Kind != kind {
			t.Fatalf("segment[%d].Kind = %q, want %q", i, segments[i].Kind, kind)
		}
		if segments[i].Tokens <= 0 {
			t.Fatalf("segment[%d].Tokens = %d", i, segments[i].Tokens)
		}
	}
	if segments[4].MessageCount != 2 {
		t.Fatalf("messages segment count = %d", segments[4].MessageCount)
	}
}

func TestBuildSegmentsSkipsEmpty(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		Estimator: CharRatioEstimator{CharsPerToken: 1, EmptyMessageOverhead: 0, SafetyMargin: 1.0},
	}, nil)

	segments := engine.buildSegments("", "", "", "", "", "", "", nil)
	if len(segments) != 0 {
		t.Fatalf("expected 0 segments for all-empty input, got %d", len(segments))
	}
}

func TestSegmentTokens(t *testing.T) {
	t.Parallel()

	segments := []ContextSegment{
		{Tokens: 100},
		{Tokens: 200},
		{Tokens: 50},
	}
	total := segmentTokens(segments)
	if total != 350 {
		t.Fatalf("segmentTokens = %d, want 350", total)
	}
}

func TestSegmentTokensEmpty(t *testing.T) {
	t.Parallel()

	total := segmentTokens(nil)
	if total != 0 {
		t.Fatalf("segmentTokens(nil) = %d, want 0", total)
	}
}

func TestBuildCompactSummary(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		{Role: RoleUser, Content: "What is Go?"},
		{Role: RoleAssistant, Content: "Go is a programming language."},
		{Role: RoleUser, Content: "Tell me more."},
	}

	summary := buildCompactSummary(msgs, 0)
	if !strings.Contains(summary, "[user] What is Go?") {
		t.Fatalf("summary = %q", summary)
	}
	if !strings.Contains(summary, "[assistant] Go is a programming language.") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestBuildCompactSummaryTruncation(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		{Role: RoleUser, Content: strings.Repeat("a", 1000)},
		{Role: RoleAssistant, Content: strings.Repeat("b", 1000)},
	}

	summary := buildCompactSummary(msgs, 50)
	if len(summary) > 53 { // 50 + "..."
		t.Fatalf("summary length = %d, expected <= 53", len(summary))
	}
	if !strings.HasSuffix(summary, "...") {
		t.Fatalf("summary should end with '...': %q", summary)
	}
}

func TestBuildCompactSummaryEmpty(t *testing.T) {
	t.Parallel()

	summary := buildCompactSummary(nil, 0)
	if summary != "" {
		t.Fatalf("expected empty summary, got %q", summary)
	}
}

func TestBuildCompactSummarySkipsEmptyContent(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		{Role: RoleUser, Content: ""},
		{Role: RoleAssistant, Content: "   "},
		{Role: RoleUser, Content: "actual content"},
	}

	summary := buildCompactSummary(msgs, 0)
	if !strings.Contains(summary, "actual content") {
		t.Fatalf("summary = %q", summary)
	}
	// Should not contain empty entries.
	lines := strings.Split(summary, "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line (only non-empty content), got %d", len(lines))
	}
}

func TestComposeMessagesInjectsSummary(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{}, nil)
	msgs := []Message{
		{Role: RoleUser, Content: "hello"},
	}

	composed := engine.composeMessages("Previous context summary", msgs)
	if len(composed) != 2 {
		t.Fatalf("expected 2 messages (summary + original), got %d", len(composed))
	}
	if composed[0].Role != RoleSystem {
		t.Fatalf("summary message role = %q", composed[0].Role)
	}
	if composed[0].Name != "session-summary" {
		t.Fatalf("summary message name = %q", composed[0].Name)
	}
	if composed[0].Content != "Previous context summary" {
		t.Fatalf("summary content = %q", composed[0].Content)
	}
}

func TestComposeMessagesNoSummary(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{}, nil)
	msgs := []Message{
		{Role: RoleUser, Content: "hello"},
	}

	composed := engine.composeMessages("", msgs)
	if len(composed) != 1 {
		t.Fatalf("expected 1 message (no summary), got %d", len(composed))
	}
}

func TestCompactKeepsLastNMessages(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    2,
		CompactSummaryChars: 500,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "msg 1"},
			{Role: RoleAssistant, Content: "msg 2"},
			{Role: RoleUser, Content: "msg 3"},
			{Role: RoleAssistant, Content: "msg 4"},
			{Role: RoleUser, Content: "msg 5"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("expected 2 messages after compact, got %d", len(session.Messages))
	}
	if session.Messages[0].Content != "msg 4" {
		t.Fatalf("first remaining message = %q", session.Messages[0].Content)
	}
	if session.Summary == "" {
		t.Fatal("Summary should not be empty after compact")
	}
	if !strings.Contains(session.Summary, "[compact_reason] manual") {
		t.Fatalf("Summary = %q", session.Summary)
	}
}

func TestCompactNilSessionReturnsError(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{}, nil)
	err := engine.Compact(context.Background(), nil, CompactManual)
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestCompactNoOpWhenFewMessages(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{CompactKeepLastN: 10}, nil)
	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "hi"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactPeriodic); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("expected 1 message (no-op compact), got %d", len(session.Messages))
	}
	if session.Summary != "" {
		t.Fatalf("Summary should be empty (no-op), got %q", session.Summary)
	}
}

func TestCompactAppendsSummary(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 500,
	}, nil)

	session := &Session{
		Summary: "Previous summary from earlier.",
		Messages: []Message{
			{Role: RoleUser, Content: "msg 1"},
			{Role: RoleAssistant, Content: "msg 2"},
			{Role: RoleUser, Content: "msg 3"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactEmergency); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if !strings.HasPrefix(session.Summary, "Previous summary from earlier.") {
		t.Fatalf("Summary should start with previous summary: %q", session.Summary)
	}
	if !strings.Contains(session.Summary, "[compact_reason] emergency") {
		t.Fatalf("Summary = %q", session.Summary)
	}
}

func TestCompactKeepsSummaryBoundedAndDedupesReasonMarkers(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 120,
	}, nil)

	session := &Session{
		Summary: strings.Repeat("older summary paragraph. ", 8) + "\n[compact_reason] manual",
		Messages: []Message{
			{Role: RoleUser, Content: strings.Repeat("newer message ", 10)},
			{Role: RoleAssistant, Content: "keep latest"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactEmergency); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(session.Summary) > 120 {
		t.Fatalf("Summary length = %d, want <= 120", len(session.Summary))
	}
	if strings.Count(session.Summary, "[compact_reason]") != 1 {
		t.Fatalf("Summary should have exactly one compact reason marker: %q", session.Summary)
	}
	if !strings.Contains(session.Summary, "[compact_reason] emergency") {
		t.Fatalf("Summary = %q", session.Summary)
	}
}

func TestSummaryForPromptDropsCompactReasonMarkers(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{CompactSummaryChars: 200}, nil)
	summary := "Earlier context.\n[compact_reason] manual\n\nLatest context."
	got := engine.summaryForPrompt(summary)
	if strings.Contains(got, "[compact_reason]") {
		t.Fatalf("summaryForPrompt() should omit compact reason markers: %q", got)
	}
	if !strings.Contains(got, "Latest context.") {
		t.Fatalf("summaryForPrompt() lost latest content: %q", got)
	}
}

func TestSelectSkillPromptPrioritizesRelevantEntries(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		IncludeSkillCatalog: true,
		SkillPromptMaxChars: 220,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "Please triage github issues and open pull requests."},
		},
	}
	snap := skill.SessionSkillSnapshot{
		PromptCatalog: []skill.PromptCatalogEntry{
			{Name: "weather", Description: "Check weather forecasts", Location: "workspace:weather"},
			{Name: "github", Description: "Work with GitHub issues and pull requests", Location: "workspace:github"},
			{Name: "translate", Description: "Translate text between languages", Location: "workspace:translate"},
		},
		PromptBlock: skill.FormatPromptCatalog([]skill.PromptCatalogEntry{
			{Name: "weather", Description: strings.Repeat("forecast ", 8), Location: "workspace:weather"},
			{Name: "github", Description: strings.Repeat("issues pull requests ", 8), Location: "workspace:github"},
			{Name: "translate", Description: strings.Repeat("language ", 8), Location: "workspace:translate"},
		}),
	}

	got := engine.selectSkillPrompt(session, nil, snap)
	if !strings.Contains(got, `name="github"`) {
		t.Fatalf("skill prompt should keep relevant github entry: %q", got)
	}
	if !strings.Contains(got, `<note>`) {
		t.Fatalf("skill prompt should include omission note when truncated: %q", got)
	}
}

func TestSelectSkillPromptPrioritizesDetectedDomainsWhenTrimming(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		IncludeSkillCatalog: true,
		SkillPromptMaxChars: 180,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "Please check the weather first."},
		},
	}
	run := &Run{
		DetectedDomains: []string{"presentation"},
	}
	snap := skill.SessionSkillSnapshot{
		PromptCatalog: []skill.PromptCatalogEntry{
			{Name: "weather", Description: "Check weather forecasts", Location: "workspace:weather", ToolDomains: []string{"news"}},
			{Name: "slides", Description: "Create leadership slide decks", Location: "workspace:slides", ToolDomains: []string{"presentation"}},
		},
		PromptBlock: skill.FormatPromptCatalog([]skill.PromptCatalogEntry{
			{Name: "weather", Description: strings.Repeat("forecast ", 10), Location: "workspace:weather", ToolDomains: []string{"news"}},
			{Name: "slides", Description: strings.Repeat("slide deck ", 10), Location: "workspace:slides", ToolDomains: []string{"presentation"}},
		}),
	}

	got := engine.selectSkillPrompt(session, run, snap)
	if !strings.Contains(got, `name="slides"`) {
		t.Fatalf("skill prompt should keep detected-domain entry: %q", got)
	}
	if !strings.Contains(got, `<note>`) {
		t.Fatalf("skill prompt should include omission note when truncated: %q", got)
	}
}

func TestSelectSkillPromptReordersAgainstFullCatalogEvenWhenPromptBlockWasPretrimmed(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		IncludeSkillCatalog: true,
		SkillPromptMaxChars: 180,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "Please work on GitHub pull requests."},
		},
	}
	fullCatalog := []skill.PromptCatalogEntry{
		{Name: "weather", Description: "Check weather forecasts", Location: "workspace:weather"},
		{Name: "github", Description: "Work with GitHub pull requests and issues", Location: "workspace:github"},
		{Name: "translate", Description: "Translate text between languages", Location: "workspace:translate"},
	}
	snap := skill.SessionSkillSnapshot{
		PromptCatalog: fullCatalog,
		PromptBlock: skill.FormatPromptCatalogWithNotice([]skill.PromptCatalogEntry{
			fullCatalog[0],
		}, 2, "Additional eligible skill prompts omitted due to size."),
	}

	got := engine.selectSkillPrompt(session, nil, snap)
	if !strings.Contains(got, `name="github"`) {
		t.Fatalf("skill prompt should be rebuilt from full catalog and keep relevant github entry: %q", got)
	}
}

func TestSelectSkillPromptPrioritizesChineseContextWhenTrimming(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		IncludeSkillCatalog: true,
		SkillPromptMaxChars: 180,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "请帮我查询上海明天的天气预报，并总结注意事项。"},
		},
	}
	snap := skill.SessionSkillSnapshot{
		PromptCatalog: []skill.PromptCatalogEntry{
			{Name: "github", Description: "处理 GitHub issue 和 pull request", Location: "workspace:github"},
			{Name: "weather", Description: "查询天气预报和空气质量", Location: "workspace:weather"},
		},
		PromptBlock: skill.FormatPromptCatalog([]skill.PromptCatalogEntry{
			{Name: "github", Description: strings.Repeat("issue pull request ", 8), Location: "workspace:github"},
			{Name: "weather", Description: strings.Repeat("天气预报 空气质量 ", 8), Location: "workspace:weather"},
		}),
	}

	got := engine.selectSkillPrompt(session, nil, snap)
	if !strings.Contains(got, `name="weather"`) {
		t.Fatalf("skill prompt should keep relevant Chinese weather entry: %q", got)
	}
}

func TestPromptContextTokensPreservesCyrillicWords(t *testing.T) {
	t.Parallel()

	tokens := promptContextTokens("Пожалуйста переведи это письмо на английский")
	if _, ok := tokens["переведи"]; !ok {
		t.Fatalf("expected Cyrillic token to be preserved, got %#v", tokens)
	}
}

func TestAppendToolResultsTruncatesContent(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{ToolResultMaxChars: 10}, nil)
	session := &Session{}

	err := engine.AppendToolResults(context.Background(), session, []ToolResult{
		{ToolName: "bash", ToolCallID: "call-1", Content: "12345678901234567890"},
	})
	if err != nil {
		t.Fatalf("AppendToolResults() error = %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(session.Messages))
	}
	msg := session.Messages[0]
	if !strings.Contains(msg.Content, "[truncated]") {
		t.Fatalf("content = %q, expected truncation", msg.Content)
	}
}

func TestAppendToolResultsIncludesArtifact(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{ToolResultMaxChars: 1000}, nil)
	session := &Session{}

	err := engine.AppendToolResults(context.Background(), session, []ToolResult{
		{ToolName: "write", ToolCallID: "call-2", Content: "wrote file", ArtifactURI: "artifact://files/test.go"},
	})
	if err != nil {
		t.Fatalf("AppendToolResults() error = %v", err)
	}
	msg := session.Messages[0]
	if !strings.Contains(msg.Content, "wrote file") {
		t.Fatalf("content = %q, expected original content", msg.Content)
	}
	result, ok := resultmodel.DecodeToolResultMetadata(msg.Metadata)
	if !ok {
		t.Fatalf("tool result metadata = %#v", msg.Metadata)
	}
	if result.ArtifactURI != "artifact://files/test.go" {
		t.Fatalf("artifact_uri = %q", result.ArtifactURI)
	}
}

func TestAppendToolResultsNilSessionReturnsError(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{}, nil)
	err := engine.AppendToolResults(context.Background(), nil, []ToolResult{{Content: "x"}})
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestPrepareNilSessionReturnsError(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{}, nil)
	_, _, err := engine.Prepare(context.Background(), nil, nil, skill.RuntimeContext{})
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestPrepareDefaultBudget(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		DefaultContextWindow: 32000,
		DefaultOutputTokens:  4000,
		MaxInputRatio:        0.75,
	}, nil)

	session := &Session{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}

	_, budget, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if budget.ContextWindow != 32000 {
		t.Fatalf("ContextWindow = %d", budget.ContextWindow)
	}
	if budget.ReservedOutput != 4000 {
		t.Fatalf("ReservedOutput = %d", budget.ReservedOutput)
	}
	// MaxInput should be min(32000-4000, 32000*0.75) = min(28000, 24000) = 24000
	if budget.MaxInputTokens != 24000 {
		t.Fatalf("MaxInputTokens = %d, want 24000", budget.MaxInputTokens)
	}
}

func TestPrepareRunOverridesDefaults(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		DefaultContextWindow: 32000,
		DefaultOutputTokens:  4000,
	}, nil)

	session := &Session{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}
	run := &Run{
		MaxContextTokens: 100000,
		MaxOutputTokens:  8000,
	}

	_, budget, err := engine.Prepare(context.Background(), session, run, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if budget.ContextWindow != 100000 {
		t.Fatalf("ContextWindow = %d, want 100000", budget.ContextWindow)
	}
	if budget.ReservedOutput != 8000 {
		t.Fatalf("ReservedOutput = %d, want 8000", budget.ReservedOutput)
	}
}

func TestNewSlidingWindowEngineDefaults(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{}, nil)
	// Check that defaults are applied.
	if engine.config.KeepLastN != 20 {
		t.Fatalf("KeepLastN = %d, want 20", engine.config.KeepLastN)
	}
	if engine.config.MaxInputRatio != 0.75 {
		t.Fatalf("MaxInputRatio = %f, want 0.75", engine.config.MaxInputRatio)
	}
	if engine.config.DefaultContextWindow != 128000 {
		t.Fatalf("DefaultContextWindow = %d, want 128000", engine.config.DefaultContextWindow)
	}
	if engine.config.DefaultOutputTokens != 4000 {
		t.Fatalf("DefaultOutputTokens = %d, want 4000", engine.config.DefaultOutputTokens)
	}
	if engine.config.ToolResultMaxChars != 4000 {
		t.Fatalf("ToolResultMaxChars = %d, want 4000", engine.config.ToolResultMaxChars)
	}
	if engine.config.SkillPromptMaxChars != defaultSkillPromptMaxChars {
		t.Fatalf("SkillPromptMaxChars = %d, want %d", engine.config.SkillPromptMaxChars, defaultSkillPromptMaxChars)
	}
	if engine.config.CompactKeepLastN != 20 {
		t.Fatalf("CompactKeepLastN = %d, want 20", engine.config.CompactKeepLastN)
	}
	if engine.config.CompactSummaryChars != 4000 {
		t.Fatalf("CompactSummaryChars = %d, want 4000", engine.config.CompactSummaryChars)
	}
	if engine.config.Estimator == nil {
		t.Fatal("Estimator should not be nil")
	}
}

func TestComposeSystemPrompt(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		BaseSystemPrompt:    "You are an agent.",
		IncludeSkillCatalog: true,
	}, nil)

	skillSnap := skill.SessionSkillSnapshot{
		PromptBlock: `<skills>
  <skill name="test">Test skill</skill>
</skills>`,
	}

	base, runP, skillP, combined := engine.composeSystemPrompt(
		&Run{SystemPrompt: "Be precise."},
		skillSnap,
	)
	if base != "You are an agent." {
		t.Fatalf("base = %q", base)
	}
	if runP != "Be precise." {
		t.Fatalf("runPrompt = %q", runP)
	}
	if !strings.Contains(skillP, "<skills>") {
		t.Fatalf("skillPrompt = %q", skillP)
	}
	if !strings.Contains(combined, "You are an agent.") {
		t.Fatalf("combined = %q", combined)
	}
	if !strings.Contains(combined, "Be precise.") {
		t.Fatalf("combined = %q", combined)
	}
	if !strings.Contains(combined, "<skills>") {
		t.Fatalf("combined = %q", combined)
	}
}

func TestComposeSystemPromptSkillCatalogDisabled(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		BaseSystemPrompt:    "Base prompt.",
		IncludeSkillCatalog: false,
	}, nil)

	skillSnap := skill.SessionSkillSnapshot{
		PromptBlock: "<skills>test</skills>",
	}
	_, _, skillP, combined := engine.composeSystemPrompt(nil, skillSnap)
	if skillP != "" {
		t.Fatalf("skillPrompt should be empty when catalog disabled, got %q", skillP)
	}
	if strings.Contains(combined, "<skills>") {
		t.Fatalf("combined should not contain skills when disabled: %q", combined)
	}
}

func TestMinInt(t *testing.T) {
	t.Parallel()

	if ints.Min(3, 5) != 3 {
		t.Fatal("ints.Min(3, 5) should be 3")
	}
	if ints.Min(5, 3) != 3 {
		t.Fatal("ints.Min(5, 3) should be 3")
	}
	if ints.Min(4, 4) != 4 {
		t.Fatal("ints.Min(4, 4) should be 4")
	}
}

func TestInspect(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		BaseSystemPrompt:     "You are an agent.",
		DefaultContextWindow: 32000,
		DefaultOutputTokens:  4000,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
			{Role: RoleAssistant, Content: "hi there"},
		},
	}

	report, err := engine.Inspect(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if report.GeneratedAt.IsZero() {
		t.Fatal("GeneratedAt should be set")
	}
	if report.SystemPrompt != "You are an agent." {
		t.Fatalf("SystemPrompt = %q", report.SystemPrompt)
	}
	if len(report.Segments) == 0 {
		t.Fatal("Segments should not be empty")
	}
	if report.Budget.ContextWindow != 32000 {
		t.Fatalf("Budget.ContextWindow = %d", report.Budget.ContextWindow)
	}
}

// ---------------------------------------------------------------------------
// softTrimContent
// ---------------------------------------------------------------------------

func TestSoftTrimContent_NoTrimNeeded(t *testing.T) {
	t.Parallel()
	content := "hello world"
	if got := softTrimContent(content, 100, 5); got != content {
		t.Fatalf("softTrimContent = %q, want unchanged", got)
	}
}

func TestSoftTrimContent_SingleLineFallsBackToCharCut(t *testing.T) {
	t.Parallel()
	content := "1234567890ABCDEF"
	got := softTrimContent(content, 10, 20)
	if !strings.Contains(got, "[truncated]") {
		t.Fatalf("single-line content should fall back to char cut: %q", got)
	}
	if strings.Contains(got, "omitted") {
		t.Fatalf("single-line content should not have omitted marker: %q", got)
	}
}

func TestSoftTrimContent_MultiLinePreservesHeadAndTail(t *testing.T) {
	t.Parallel()
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = strings.Repeat("x", 20)
	}
	content := strings.Join(lines, "\n")
	keepLines := 5
	got := softTrimContent(content, 200, keepLines)

	if !strings.Contains(got, "omitted") {
		t.Fatalf("expected 'omitted' marker in trimmed content: %q", got)
	}
	// Head lines should be present.
	if !strings.HasPrefix(got, lines[0]) {
		t.Fatalf("head line missing: %q", got)
	}
	// Tail line should be present.
	if !strings.HasSuffix(got, lines[len(lines)-1]) {
		t.Fatalf("tail line missing: %q", got)
	}
}

func TestSoftTrimContent_KeepLinesZeroDisablesLineTrim(t *testing.T) {
	t.Parallel()
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = strings.Repeat("y", 20)
	}
	content := strings.Join(lines, "\n")
	got := softTrimContent(content, 100, 0) // keepLines=0 → char cut
	if strings.Contains(got, "omitted") {
		t.Fatalf("keepLines=0 should disable line-preserving trim: %q", got)
	}
	if !strings.Contains(got, "[truncated]") {
		t.Fatalf("expected [truncated] with keepLines=0: %q", got)
	}
}

// ---------------------------------------------------------------------------
// messageImportance
// ---------------------------------------------------------------------------

func TestMessageImportanceUserScoresHigherThanTool(t *testing.T) {
	t.Parallel()
	user := Message{Role: RoleUser, Content: "what should I do?"}
	tool := Message{Role: RoleTool, Content: "output line 1"}
	scoreUser := messageImportance(user, 0, 2)
	scoreTool := messageImportance(tool, 0, 2)
	if scoreUser <= scoreTool {
		t.Fatalf("user score (%f) should exceed tool score (%f)", scoreUser, scoreTool)
	}
}

func TestMessageImportanceRecencyBoost(t *testing.T) {
	t.Parallel()
	older := Message{Role: RoleTool, Content: "result"}
	newer := Message{Role: RoleTool, Content: "result"}
	scoreOld := messageImportance(older, 0, 5)
	scoreNew := messageImportance(newer, 4, 5)
	if scoreNew <= scoreOld {
		t.Fatalf("newer message (%f) should score higher than older (%f)", scoreNew, scoreOld)
	}
}

func TestMessageImportanceMetadataBoost(t *testing.T) {
	t.Parallel()
	plain := Message{Role: RoleAssistant, Content: "Some output", CreatedAt: time.Now()}
	boosted := Message{
		Role:      RoleAssistant,
		Content:   "Some output",
		CreatedAt: time.Now(),
		Metadata: map[string]any{
			MetadataKeyMessageImportance: 0.2,
		},
	}
	scorePlain := messageImportance(plain, 0, 2)
	scoreBoosted := messageImportance(boosted, 0, 2)
	if scoreBoosted <= scorePlain {
		t.Fatalf("message with metadata boost (%f) should score higher than plain (%f)", scoreBoosted, scorePlain)
	}
}

func TestMessageImportanceToolErrorBoost(t *testing.T) {
	t.Parallel()
	plain := Message{Role: RoleTool, Content: "completed successfully", CreatedAt: time.Now()}
	withErr := Message{Role: RoleTool, Content: "ERROR: disk full", CreatedAt: time.Now()}
	scorePlain := messageImportance(plain, 0, 2)
	scoreError := messageImportance(withErr, 0, 2)
	if scoreError <= scorePlain {
		t.Fatalf("tool error message (%f) should score higher than plain tool output (%f)", scoreError, scorePlain)
	}
}

func TestTrimMessagesToBudgetPrefersDroppingToolOverUser(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		KeepFirstN: 0,
		KeepLastN:  10,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, nil)

	// Total = 3 messages × 30 chars = 90 tokens; budget = 65; one must be dropped.
	// The tool result is earliest and lowest role, so it should be dropped first.
	msgs := []Message{
		{Role: RoleTool, Content: strings.Repeat("t", 30)},
		{Role: RoleAssistant, Content: strings.Repeat("a", 30)},
		{Role: RoleUser, Content: strings.Repeat("u", 30)},
	}

	trimmed := engine.trimMessagesToBudget("", "", msgs, 65)
	for _, m := range trimmed {
		if m.Role == RoleTool {
			t.Fatal("tool result should have been dropped before user/assistant messages")
		}
	}
}

func TestNewSlidingWindowEngineDefaultToolResultKeepLines(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{}, nil)
	if engine.config.ToolResultKeepLines != toolResultKeepLinesDefault {
		t.Fatalf("ToolResultKeepLines = %d, want %d", engine.config.ToolResultKeepLines, toolResultKeepLinesDefault)
	}
}

func TestCompactWithModelSummarizer(t *testing.T) {
	t.Parallel()

	model := &mockModelChat{response: "Model-generated summary of conversation."}
	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 500,
		Summarizer:          ModelSummarizer{Model: model},
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "first message", CreatedAt: time.Now()},
			{Role: RoleAssistant, Content: "response", CreatedAt: time.Now()},
			{Role: RoleUser, Content: "last message", CreatedAt: time.Now()},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactPeriodic); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if !strings.Contains(session.Summary, "Model-generated summary") {
		t.Fatalf("Summary = %q, expected model summary", session.Summary)
	}
	if model.calls != 1 {
		t.Fatalf("model.calls = %d, want 1", model.calls)
	}
}

type timeoutFallbackSummarizer struct {
	sawDeadline bool
}

func (s *timeoutFallbackSummarizer) Summarize(ctx context.Context, messages []Message, maxChars int) (string, error) {
	_, s.sawDeadline = ctx.Deadline()
	return "", context.DeadlineExceeded
}

func TestCompact_TimeoutFallsBackToNaive(t *testing.T) {
	summarizer := &timeoutFallbackSummarizer{}
	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 500,
		Summarizer:          summarizer,
	}, nil)

	discarded := []Message{
		{Role: RoleUser, Content: "first message"},
		{Role: RoleAssistant, Content: "response"},
	}
	session := &Session{
		Messages: append(append([]Message(nil), discarded...), Message{Role: RoleUser, Content: "last message"}),
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if !summarizer.sawDeadline {
		t.Fatal("expected summarizer to receive a timeout context")
	}

	expectedSummary := mergeCompactSummaries(
		"",
		buildCompactSummary(discarded, 500),
		CompactManual,
		500,
	)
	if session.Summary != expectedSummary {
		t.Fatalf("Summary = %q, want %q", session.Summary, expectedSummary)
	}
	if len(session.Messages) != 1 || session.Messages[0].Content != "last message" {
		t.Fatalf("remaining messages = %#v", session.Messages)
	}
}

// ---------------------------------------------------------------------------
// Round 2: Adaptive compaction tests
// ---------------------------------------------------------------------------

func TestAutoCompactThreshold_TriggersNeedsCompaction(t *testing.T) {
	t.Parallel()

	// Use a custom estimator that inflates token counts to easily exceed the threshold.
	bigEstimator := CharRatioEstimator{
		CharsPerToken:        1.0, // 1 char = 1 token → content is huge in token terms
		ToolCharsPerToken:    1.0,
		EmptyMessageOverhead: 4,
	}

	engine := NewSlidingWindowEngine(Config{
		DefaultContextWindow: 500,
		DefaultOutputTokens:  100,
		MaxInputRatio:        0.75,
		AutoCompactThreshold: 0.9,
		Estimator:            bigEstimator,
	}, nil)

	// At 1 char/token, this ~500 char message ≈ 500 tokens, well over 90% of maxInput (300).
	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: strings.Repeat("A", 400)},
		},
	}

	prepared, _, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !prepared.NeedsCompaction {
		t.Fatalf("expected NeedsCompaction=true, budget=%+v", prepared.Budget)
	}
}

func TestAutoCompactThreshold_Disabled(t *testing.T) {
	t.Parallel()

	bigEstimator := CharRatioEstimator{
		CharsPerToken:        1.0,
		ToolCharsPerToken:    1.0,
		EmptyMessageOverhead: 4,
	}

	engine := NewSlidingWindowEngine(Config{
		DefaultContextWindow: 500,
		DefaultOutputTokens:  100,
		AutoCompactThreshold: 0, // disabled
		Estimator:            bigEstimator,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: strings.Repeat("A", 400)},
		},
	}

	prepared, _, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.NeedsCompaction {
		t.Fatal("NeedsCompaction should be false when threshold is 0")
	}
}

func TestAutoCompactThreshold_NotTriggeredWhenBelowThreshold(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		DefaultContextWindow: 128000,
		DefaultOutputTokens:  4000,
		AutoCompactThreshold: 0.9,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "short message"},
		},
	}

	prepared, _, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.NeedsCompaction {
		t.Fatal("NeedsCompaction should be false for short conversation")
	}
}

func TestCompactKeepRecentTokens_PreservesMoreMessages(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:        2,     // count-based: keep 2
		CompactKeepRecentTokens: 10000, // token-based: would keep all 5
		CompactSummaryChars:     500,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "msg 1"},
			{Role: RoleAssistant, Content: "msg 2"},
			{Role: RoleUser, Content: "msg 3"},
			{Role: RoleAssistant, Content: "msg 4"},
			{Role: RoleUser, Content: "msg 5"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	// Token budget is large enough to keep all 5 messages, so token constraint
	// should win over count constraint (2). Result: no messages discarded because
	// cut would be <= 0.
	if len(session.Messages) < 3 {
		t.Fatalf("expected token budget to preserve more than count-based 2, got %d", len(session.Messages))
	}
}

func TestCompactKeepRecentTokens_MessageCountWins(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:        4, // count-based: keep 4
		CompactKeepRecentTokens: 1, // token-based: keep almost nothing
		CompactSummaryChars:     500,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "msg 1"},
			{Role: RoleAssistant, Content: "msg 2"},
			{Role: RoleUser, Content: "msg 3"},
			{Role: RoleAssistant, Content: "msg 4"},
			{Role: RoleUser, Content: "msg 5"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	// Count-based keeps 4, token-based keeps ~1. Count wins (more messages).
	if len(session.Messages) != 4 {
		t.Fatalf("expected count-based 4 messages to win, got %d", len(session.Messages))
	}
}
