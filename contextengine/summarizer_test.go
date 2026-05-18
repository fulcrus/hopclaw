package contextengine

import (
	"context"
	"math"
	"strings"
	"testing"
)

// mockModelChat returns a fixed response.
type mockModelChat struct {
	response string
	calls    int
}

func (m *mockModelChat) ChatSimple(_ context.Context, _, _ string) (string, error) {
	m.calls++
	return m.response, nil
}

func TestSplitByTokenShare(t *testing.T) {
	t.Parallel()
	est := CharRatioEstimator{CharsPerToken: 1, SafetyMargin: 1.0}

	messages := []Message{
		{Role: RoleUser, Content: strings.Repeat("a", 100)},
		{Role: RoleAssistant, Content: strings.Repeat("b", 100)},
		{Role: RoleUser, Content: strings.Repeat("c", 100)},
		{Role: RoleAssistant, Content: strings.Repeat("d", 100)},
	}

	chunks := splitByTokenShare(messages, 2, est)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if len(chunks[0])+len(chunks[1]) != 4 {
		t.Fatal("chunks should contain all messages")
	}
}

func TestChunkByMaxTokens(t *testing.T) {
	t.Parallel()
	est := CharRatioEstimator{CharsPerToken: 1, SafetyMargin: 1.0}

	messages := []Message{
		{Role: RoleUser, Content: strings.Repeat("a", 100)},
		{Role: RoleAssistant, Content: strings.Repeat("b", 100)},
		{Role: RoleUser, Content: strings.Repeat("c", 100)},
	}

	// With 200-token max (÷1.2 safety = ~166), should split into 2 chunks.
	chunks := chunkByMaxTokens(messages, 200, est)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
}

func TestChunkByMaxTokensDoesNotApplySecondSafetyMargin(t *testing.T) {
	t.Parallel()
	est := CharRatioEstimator{CharsPerToken: 1, SafetyMargin: 1.2}

	messages := []Message{
		{Role: RoleUser, Content: strings.Repeat("a", 90)},
		{Role: RoleAssistant, Content: strings.Repeat("b", 90)},
	}

	chunks := chunkByMaxTokens(messages, 216, est)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk when estimator already includes safety margin, got %d", len(chunks))
	}
}

func TestComputeAdaptiveChunkRatio(t *testing.T) {
	t.Parallel()
	est := CharRatioEstimator{CharsPerToken: 1, SafetyMargin: 1.0}

	// Small messages → base ratio.
	small := []Message{{Role: RoleUser, Content: "hello"}}
	ratio := computeAdaptiveChunkRatio(small, 32000, est)
	if ratio != baseChunkRatio {
		t.Fatalf("expected base ratio %f, got %f", baseChunkRatio, ratio)
	}

	// Large messages → reduced ratio.
	large := []Message{{Role: RoleUser, Content: strings.Repeat("x", 5000)}}
	ratio = computeAdaptiveChunkRatio(large, 32000, est)
	if ratio >= baseChunkRatio {
		t.Fatalf("expected reduced ratio, got %f", ratio)
	}
	if ratio < minChunkRatio {
		t.Fatalf("ratio should not go below minimum %f, got %f", minChunkRatio, ratio)
	}
}

func TestComputeAdaptiveChunkRatioUsesEstimatorOutputDirectly(t *testing.T) {
	t.Parallel()
	est := CharRatioEstimator{CharsPerToken: 1, SafetyMargin: 2.0}

	messages := []Message{{Role: RoleUser, Content: strings.Repeat("x", 1600)}}
	ratio := computeAdaptiveChunkRatio(messages, 32000, est)

	const expected = 0.19975
	if math.Abs(ratio-expected) > 0.00001 {
		t.Fatalf("ratio = %f, want %f", ratio, expected)
	}
}

func TestExtractOpaqueIdentifiers(t *testing.T) {
	t.Parallel()

	text := `Working on commit abc123def456. File at /usr/local/bin/tool.
	Visit https://example.com/api. Server at 192.168.1.1:8080. ID 12345678.`

	ids := extractOpaqueIdentifiers(text, 10)
	if len(ids) == 0 {
		t.Fatal("expected extracted identifiers")
	}

	// Should find hex ID.
	foundHex := false
	for _, id := range ids {
		if strings.Contains(strings.ToLower(id), "abc123def456") {
			foundHex = true
		}
	}
	if !foundHex {
		t.Fatalf("expected hex ID abc123def456 in %v", ids)
	}
}

func TestAuditSummaryQualityPassesWithAllSections(t *testing.T) {
	t.Parallel()

	summary := `## Decisions
Decided to use Go for the rewrite.

## Open TODOs
- Implement streaming

## Constraints/Rules
Must maintain backward compatibility.

## Pending user asks
User asked to implement all P1 items.

## Exact identifiers
abc123def456, /usr/local/bin/tool`

	result := auditSummaryQuality(summary, requiredSummarySections, nil, "", "off")
	if !result.OK {
		t.Fatalf("expected OK, got reasons: %v", result.Reasons)
	}
}

func TestAuditSummaryQualityFailsOnMissingSection(t *testing.T) {
	t.Parallel()

	summary := `## Decisions
Some decisions.

## Open TODOs
Some todos.`

	result := auditSummaryQuality(summary, requiredSummarySections, nil, "", "off")
	if result.OK {
		t.Fatal("expected quality audit to fail")
	}
	if len(result.Reasons) == 0 {
		t.Fatal("expected reasons for failure")
	}
}

func TestAuditSummaryQualityChecksIdentifiers(t *testing.T) {
	t.Parallel()

	summary := `## Decisions
x
## Open TODOs
x
## Constraints/Rules
x
## Pending user asks
x
## Exact identifiers
abc123def456`

	result := auditSummaryQuality(summary, requiredSummarySections,
		[]string{"abc123def456", "missing999888"}, "", "strict")
	if result.OK {
		t.Fatal("expected failure for missing identifier")
	}
	found := false
	for _, r := range result.Reasons {
		if strings.Contains(r, "missing_identifiers") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected missing_identifiers reason, got %v", result.Reasons)
	}
}

func TestSummaryPromptsPreserveUserPreferencesAndApprovals(t *testing.T) {
	t.Parallel()

	for name, prompt := range map[string]string{
		"single": singleStageSystemPrompt,
		"multi":  multiStageSystemPrompt,
		"merge":  mergeSummariesInstructions,
	} {
		normalized := strings.ToLower(prompt)
		for _, want := range []string{
			"Pending approvals",
			"output format",
			"language preference",
			"citation",
		} {
			if !strings.Contains(normalized, strings.ToLower(want)) {
				t.Fatalf("%s prompt missing %q:\n%s", name, want, prompt)
			}
		}
	}
}

func TestAuditSummaryQualityAcceptsChineseLatestAskCoverage(t *testing.T) {
	t.Parallel()

	summary := `## Decisions
将最近三条 RSS 摘要翻译成中文。

## Open TODOs
- 仅保留最新三条 RSS 条目。

## Constraints/Rules
保留源内容中的关键事实。

## Pending user asks
请把最近三条 RSS 摘要翻译成中文并保留关键点。

## Exact identifiers
feed.xml`

	result := auditSummaryQuality(summary, requiredSummarySections, nil, "请把最近三条RSS摘要翻译成中文并保留关键点", "off")
	if !result.OK {
		t.Fatalf("expected Chinese ask to be recognized as reflected, got reasons: %v", result.Reasons)
	}
}

func TestMultiStageSummarizerSingleStage(t *testing.T) {
	t.Parallel()

	model := &mockModelChat{response: `## Decisions
Done.
## Open TODOs
None.
## Constraints/Rules
None.
## Pending user asks
None.
## Exact identifiers
None.`}

	s := NewMultiStageSummarizer(CompactionConfig{
		Model:         model,
		Estimator:     CharRatioEstimator{CharsPerToken: 1, SafetyMargin: 1.0},
		ContextWindow: 32000,
	})

	messages := []Message{
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleAssistant, Content: "Hi there"},
	}

	summary, err := s.Summarize(context.Background(), messages, 4000)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if model.calls != 1 {
		t.Fatalf("expected 1 model call, got %d", model.calls)
	}
}

func TestMultiStageSummarizerMultiStage(t *testing.T) {
	t.Parallel()

	model := &mockModelChat{response: "Partial summary of conversation."}

	s := NewMultiStageSummarizer(CompactionConfig{
		Model:         model,
		Estimator:     CharRatioEstimator{CharsPerToken: 1, SafetyMargin: 1.0},
		ContextWindow: 500, // Small window forces multi-stage.
	})

	// Create enough messages to exceed chunk size.
	var messages []Message
	for i := 0; i < 10; i++ {
		messages = append(messages, Message{
			Role:    RoleUser,
			Content: strings.Repeat("message content here ", 20),
		})
	}

	summary, err := s.Summarize(context.Background(), messages, 4000)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	// Multi-stage should make multiple model calls (chunks + merge).
	if model.calls < 2 {
		t.Fatalf("expected multiple model calls for multi-stage, got %d", model.calls)
	}
}

func TestQualityGuardRetries(t *testing.T) {
	t.Parallel()

	callCount := 0
	model := &mockModelChat{}

	s := NewMultiStageSummarizer(CompactionConfig{
		Model:               model,
		Estimator:           CharRatioEstimator{CharsPerToken: 1, SafetyMargin: 1.0},
		ContextWindow:       32000,
		QualityGuardEnabled: true,
		QualityGuardRetries: 2,
	})

	// First call returns bad summary, second returns good.
	origChat := model.ChatSimple
	_ = origChat
	model.response = "Bad summary without sections."

	messages := []Message{{Role: RoleUser, Content: "Do something"}}

	// Should retry and keep best effort even if quality never passes.
	summary, err := s.Summarize(context.Background(), messages, 4000)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	_ = summary
	callCount = model.calls
	// Should have made initial call + retry attempts.
	if callCount < 2 {
		t.Fatalf("expected at least 2 calls with quality guard, got %d", callCount)
	}
}

func TestIsOversizedDoesNotApplyEstimatorSafetyMarginTwice(t *testing.T) {
	t.Parallel()

	s := NewMultiStageSummarizer(CompactionConfig{
		Model:         &mockModelChat{response: "summary"},
		Estimator:     CharRatioEstimator{CharsPerToken: 1, SafetyMargin: 1.2},
		ContextWindow: 100,
	})

	msg := Message{Role: RoleUser, Content: strings.Repeat("x", 41)}
	if s.isOversized(msg) {
		t.Fatal("message should not be oversized when the estimator safety margin has already been applied once")
	}
}

func TestIsPureHex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{"abc123def456", true},
		{"ABCDEF00", true},
		{"abc123", false}, // too short
		{"not-hex-at-all", false},
		{"12345678", true}, // pure digits are also valid hex
	}
	for _, tt := range tests {
		if got := isPureHex(tt.input); got != tt.want {
			t.Errorf("isPureHex(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
