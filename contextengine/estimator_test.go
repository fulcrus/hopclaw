package contextengine

import (
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/internal/support/ints"
)

// noMargin returns a CharRatioEstimator with safety margin disabled (1.0)
// so legacy tests keep their exact expected values.
func noMarginEstimator(cpt float64) CharRatioEstimator {
	return CharRatioEstimator{CharsPerToken: cpt, SafetyMargin: 1.0}
}

func TestCharRatioEstimatorEstimateEmptyString(t *testing.T) {
	t.Parallel()

	est := noMarginEstimator(4.0)
	got := est.Estimate("")
	if got != 0 {
		t.Fatalf("Estimate(\"\") = %d, want 0", got)
	}
}

func TestCharRatioEstimatorEstimateShortString(t *testing.T) {
	t.Parallel()

	est := noMarginEstimator(4.0)
	got := est.Estimate("hi")
	if got != 1 {
		t.Fatalf("Estimate(\"hi\") = %d, want 1 (ceil(2/4)=1)", got)
	}
}

func TestCharRatioEstimatorEstimateExactDivision(t *testing.T) {
	t.Parallel()

	est := noMarginEstimator(4.0)
	got := est.Estimate("abcd")
	if got != 1 {
		t.Fatalf("Estimate(\"abcd\") = %d, want 1", got)
	}
}

func TestCharRatioEstimatorEstimateLongString(t *testing.T) {
	t.Parallel()

	est := noMarginEstimator(4.0)
	text := strings.Repeat("a", 100)
	got := est.Estimate(text)
	if got != 25 {
		t.Fatalf("Estimate(100 chars at 4 cpt) = %d, want 25", got)
	}
}

func TestCharRatioEstimatorEstimateCeiling(t *testing.T) {
	t.Parallel()

	est := noMarginEstimator(4.0)
	got := est.Estimate("abcde") // 5/4 = 1.25, ceil = 2
	if got != 2 {
		t.Fatalf("Estimate(\"abcde\") = %d, want 2", got)
	}
}

func TestCharRatioEstimatorEstimateMinimumOne(t *testing.T) {
	t.Parallel()

	est := CharRatioEstimator{CharsPerToken: 100.0, SafetyMargin: 1.0}
	got := est.Estimate("a") // 1/100 = 0.01, ceil = 1, max(1, 1) = 1
	if got != 1 {
		t.Fatalf("Estimate(\"a\") with high ratio = %d, want 1", got)
	}
}

func TestCharRatioEstimatorDefaultRatio(t *testing.T) {
	t.Parallel()

	est := CharRatioEstimator{CharsPerToken: 0, SafetyMargin: 1.0} // should default to 4.0
	got := est.Estimate(strings.Repeat("x", 8))
	if got != 2 {
		t.Fatalf("Estimate(8 chars with default ratio) = %d, want 2", got)
	}
}

func TestCharRatioEstimatorEstimateMessagesEmpty(t *testing.T) {
	t.Parallel()

	est := CharRatioEstimator{CharsPerToken: 4.0, EmptyMessageOverhead: 4, SafetyMargin: 1.0}
	got := est.EstimateMessages(nil)
	if got != 0 {
		t.Fatalf("EstimateMessages(nil) = %d, want 0", got)
	}
}

func TestCharRatioEstimatorEstimateMessagesSingle(t *testing.T) {
	t.Parallel()

	est := CharRatioEstimator{
		CharsPerToken:        4.0,
		ToolCharsPerToken:    2.0,
		EmptyMessageOverhead: 4,
		SafetyMargin:         1.0,
	}
	msgs := []Message{
		{Role: RoleUser, Content: strings.Repeat("a", 8)}, // 8/4 = 2 tokens + 4 overhead = 6
	}
	got := est.EstimateMessages(msgs)
	if got != 6 {
		t.Fatalf("EstimateMessages = %d, want 6", got)
	}
}

func TestCharRatioEstimatorToolRatio(t *testing.T) {
	t.Parallel()

	est := CharRatioEstimator{
		CharsPerToken:        4.0,
		ToolCharsPerToken:    2.0,
		EmptyMessageOverhead: 4,
		SafetyMargin:         1.0,
	}
	msgs := []Message{
		{Role: RoleTool, Content: strings.Repeat("x", 8)}, // 8/2 = 4 tokens + 4 overhead = 8
	}
	got := est.EstimateMessages(msgs)
	if got != 8 {
		t.Fatalf("EstimateMessages(tool) = %d, want 8", got)
	}
}

func TestCharRatioEstimatorMultipleMessages(t *testing.T) {
	t.Parallel()

	est := CharRatioEstimator{
		CharsPerToken:        4.0,
		ToolCharsPerToken:    2.0,
		EmptyMessageOverhead: 4,
		SafetyMargin:         1.0,
	}
	msgs := []Message{
		{Role: RoleUser, Content: strings.Repeat("a", 8)},       // 2 + 4 = 6
		{Role: RoleAssistant, Content: strings.Repeat("b", 12)}, // 3 + 4 = 7
		{Role: RoleTool, Content: strings.Repeat("c", 6)},       // 6/2=3 + 4 = 7
	}
	got := est.EstimateMessages(msgs)
	expected := 6 + 7 + 7
	if got != expected {
		t.Fatalf("EstimateMessages = %d, want %d", got, expected)
	}
}

func TestCharRatioEstimatorDefaultOverhead(t *testing.T) {
	t.Parallel()

	est := CharRatioEstimator{
		CharsPerToken:        4.0,
		EmptyMessageOverhead: 0, // should default to 4
		SafetyMargin:         1.0,
	}
	msgs := []Message{
		{Role: RoleUser, Content: strings.Repeat("a", 4)}, // 1 + 4(default) = 5
	}
	got := est.EstimateMessages(msgs)
	if got != 5 {
		t.Fatalf("EstimateMessages with zero overhead = %d, want 5", got)
	}
}

func TestCharRatioEstimatorEstimateMessagesUsesTextContent(t *testing.T) {
	t.Parallel()

	est := CharRatioEstimator{
		CharsPerToken:        1.0,
		EmptyMessageOverhead: 2,
		SafetyMargin:         1.0,
	}
	// Message with ContentBlocks should use TextContent().
	msgs := []Message{
		{
			Role: RoleUser,
			ContentBlocks: []ContentBlock{
				{Type: ContentBlockText, Text: "hello"},
				{Type: ContentBlockImage, Data: "base64data"},
				{Type: ContentBlockText, Text: " world"},
			},
		},
	}
	got := est.EstimateMessages(msgs)
	// TextContent() returns "hello world" = 11 chars at ratio 1 = 11 tokens + 2 overhead = 13
	if got != 13 {
		t.Fatalf("EstimateMessages with content blocks = %d, want 13", got)
	}
}

func TestCalibratedEstimatorAdjustsTowardObservedUsage(t *testing.T) {
	t.Parallel()

	base := CharRatioEstimator{CharsPerToken: 4.0, SafetyMargin: 1.0}
	est := NewCalibratedEstimator(base)

	if got := est.Estimate(strings.Repeat("a", 100)); got != 25 {
		t.Fatalf("Estimate(before calibration) = %d, want 25", got)
	}

	est.RecordActual(25, 50)

	if est.CorrectionFactor() <= 1.0 {
		t.Fatalf("CorrectionFactor() = %f, want > 1.0", est.CorrectionFactor())
	}
	if got := est.Estimate(strings.Repeat("a", 100)); got != 30 {
		t.Fatalf("Estimate(after calibration) = %d, want 30", got)
	}
}

func TestCalibratedEstimatorEstimateMessagesUsesCorrectionFactor(t *testing.T) {
	t.Parallel()

	base := CharRatioEstimator{
		CharsPerToken:        4.0,
		EmptyMessageOverhead: 4,
		SafetyMargin:         1.0,
	}
	est := NewCalibratedEstimator(base)
	msgs := []Message{{Role: RoleUser, Content: strings.Repeat("a", 8)}}

	if got := est.EstimateMessages(msgs); got != 6 {
		t.Fatalf("EstimateMessages(before calibration) = %d, want 6", got)
	}

	est.RecordActual(6, 12)

	if got := est.EstimateMessages(msgs); got != 8 {
		t.Fatalf("EstimateMessages(after calibration) = %d, want 8", got)
	}
}

func TestIntsMax(t *testing.T) {
	t.Parallel()

	if ints.Max(3, 5) != 5 {
		t.Fatal("ints.Max(3, 5) should be 5")
	}
	if ints.Max(5, 3) != 5 {
		t.Fatal("ints.Max(5, 3) should be 5")
	}
	if ints.Max(4, 4) != 4 {
		t.Fatal("ints.Max(4, 4) should be 4")
	}
	if ints.Max(-1, 0) != 0 {
		t.Fatal("ints.Max(-1, 0) should be 0")
	}
}

// ---------------------------------------------------------------------------
// Safety margin tests
// ---------------------------------------------------------------------------

func TestEstimateSafetyMarginDefault(t *testing.T) {
	t.Parallel()

	// Default safety margin is 1.2
	est := CharRatioEstimator{CharsPerToken: 4.0}
	got := est.Estimate(strings.Repeat("a", 100)) // raw=25, *1.2=30
	if got != 30 {
		t.Fatalf("Estimate with default safety margin = %d, want 30", got)
	}
}

func TestEstimateSafetyMarginCustom(t *testing.T) {
	t.Parallel()

	est := CharRatioEstimator{CharsPerToken: 4.0, SafetyMargin: 1.5}
	got := est.Estimate(strings.Repeat("a", 100)) // raw=25, *1.5=37.5, ceil=38
	if got != 38 {
		t.Fatalf("Estimate with 1.5 safety margin = %d, want 38", got)
	}
}

func TestEstimateMessagesSafetyMargin(t *testing.T) {
	t.Parallel()

	est := CharRatioEstimator{
		CharsPerToken:        4.0,
		EmptyMessageOverhead: 4,
	}
	msgs := []Message{
		{Role: RoleUser, Content: strings.Repeat("a", 100)}, // raw=25, *1.2=30, +4=34
	}
	got := est.EstimateMessages(msgs)
	if got != 34 {
		t.Fatalf("EstimateMessages with safety margin = %d, want 34", got)
	}
}

// ---------------------------------------------------------------------------
// CJK detection and density tests
// ---------------------------------------------------------------------------

func TestIsCJKHeavyPureChinese(t *testing.T) {
	t.Parallel()
	if !isCJKHeavy("你好世界这是一段中文文本") {
		t.Fatal("pure Chinese text should be detected as CJK-heavy")
	}
}

func TestIsCJKHeavyPureEnglish(t *testing.T) {
	t.Parallel()
	if isCJKHeavy("hello world this is english text") {
		t.Fatal("pure English text should not be CJK-heavy")
	}
}

func TestIsCJKHeavyMixed(t *testing.T) {
	t.Parallel()
	// Below threshold: mostly English with a few CJK chars
	if isCJKHeavy("hello world 你好") {
		t.Fatal("mostly English with few CJK chars should not be CJK-heavy")
	}
}

func TestIsCJKHeavyEmpty(t *testing.T) {
	t.Parallel()
	if isCJKHeavy("") {
		t.Fatal("empty string should not be CJK-heavy")
	}
}

func TestEstimateCJKText(t *testing.T) {
	t.Parallel()

	est := CharRatioEstimator{CharsPerToken: 4.0, SafetyMargin: 1.0}
	// 10 CJK chars = 30 bytes in UTF-8. With CJK ratio 2.5: 30/2.5=12
	text := "你好世界测试中文内容啊"
	got := est.Estimate(text)
	// Each CJK char is 3 bytes, 11 chars = 33 bytes. 33/2.5=13.2, ceil=14
	if got < 10 {
		t.Fatalf("CJK estimate = %d, should be >= 10 for CJK text", got)
	}
}

func TestEstimateCJKHigherThanLatin(t *testing.T) {
	t.Parallel()

	est := CharRatioEstimator{CharsPerToken: 4.0, SafetyMargin: 1.0}

	// Same semantic content, CJK should estimate higher tokens per character
	latinText := strings.Repeat("abcd", 10) // 40 bytes, 40/4=10
	cjkText := strings.Repeat("你好世界", 10)   // 120 bytes, 120/2.5=48

	latinTokens := est.Estimate(latinText)
	cjkTokens := est.Estimate(cjkText)

	// CJK estimate should be significantly higher due to lower ratio
	if cjkTokens <= latinTokens {
		t.Fatalf("CJK tokens (%d) should be > Latin tokens (%d)", cjkTokens, latinTokens)
	}
}

func TestIsCJKRuneRanges(t *testing.T) {
	t.Parallel()

	cases := []struct {
		r    rune
		want bool
		desc string
	}{
		{'中', true, "CJK Unified Ideograph"},
		{'ア', false, "Katakana (not in CJK ranges)"},
		{'가', true, "Hangul Syllable"},
		{'A', false, "Latin"},
		{'，', true, "CJK fullwidth punctuation"},
		{'〇', true, "CJK Symbol"},
	}
	for _, tc := range cases {
		got := isCJKRune(tc.r)
		if got != tc.want {
			t.Errorf("isCJKRune(%q) [%s] = %v, want %v", tc.r, tc.desc, got, tc.want)
		}
	}
}
