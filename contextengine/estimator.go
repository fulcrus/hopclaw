package contextengine

import (
	"math"
	"sync"
	"unicode/utf8"

	"github.com/fulcrus/hopclaw/internal/support/ints"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// defaultCharsPerToken is the baseline character-to-token ratio for
	// Latin/ASCII text. Most BPE tokenizers average ~4 characters per token
	// on English prose.
	defaultCharsPerToken = 4.0

	// defaultEmptyMessageOverhead accounts for per-message framing tokens
	// (role markers, delimiters, etc.) that exist even for empty messages.
	defaultEmptyMessageOverhead = 4

	// cjkDensityRatio is the adjusted chars-per-token for CJK-heavy text.
	// CJK ideographs are typically 3 UTF-8 bytes each but encode as ~1-2
	// BPE tokens, making the byte-length / 4 heuristic overly optimistic.
	cjkDensityRatio = 2.5

	// cjkThreshold is the fraction of runes that must be CJK for the text
	// to be treated as CJK-heavy.
	cjkThreshold = 0.3

	calibratedEstimatorAlpha    = 0.2
	calibratedEstimatorMinScale = 0.5
	calibratedEstimatorMaxScale = 2.0
)

// CharRatioEstimator estimates token counts using a character-to-token ratio
// heuristic. It applies a safety margin and adjusts for CJK-heavy content.
type CharRatioEstimator struct {
	CharsPerToken        float64
	ToolCharsPerToken    float64
	EmptyMessageOverhead int

	// SafetyMargin inflates estimates to compensate for heuristic error.
	// 0 or negative values fall back to the default (1.2).
	SafetyMargin float64
}

func (e CharRatioEstimator) safetyFactor() float64 {
	if e.SafetyMargin > 0 {
		return e.SafetyMargin
	}
	return safetyMargin
}

// Estimate returns the estimated token count for a single text string.
func (e CharRatioEstimator) Estimate(text string) int {
	if text == "" {
		return 0
	}
	ratio := e.ratioForText(text, false)
	raw := math.Ceil(float64(len(text)) / ratio)
	return ints.Max(1, int(math.Ceil(raw*e.safetyFactor())))
}

// EstimateMessages returns the estimated total token count for a slice of
// messages, accounting for per-message overhead and role-specific ratios.
func (e CharRatioEstimator) EstimateMessages(msgs []Message) int {
	total := 0
	overhead := e.EmptyMessageOverhead
	if overhead <= 0 {
		overhead = defaultEmptyMessageOverhead
	}
	for _, msg := range msgs {
		isTool := msg.Role == RoleTool
		content := msg.TextContent()
		ratio := e.ratioForText(content, isTool)
		raw := math.Ceil(float64(len(content)) / ratio)
		tokens := ints.Max(1, int(math.Ceil(raw*e.safetyFactor()))) + overhead
		total += tokens
	}
	return total
}

// CalibratedEstimator wraps a base estimator and adjusts its output using
// observed prompt-token counts from model API responses.
type CalibratedEstimator struct {
	base       TokenEstimator
	alpha      float64
	mu         sync.RWMutex
	correction float64
}

func NewCalibratedEstimator(base TokenEstimator) *CalibratedEstimator {
	if base == nil {
		base = CharRatioEstimator{}
	}
	return &CalibratedEstimator{
		base:       base,
		alpha:      calibratedEstimatorAlpha,
		correction: 1.0,
	}
}

func (e *CalibratedEstimator) Estimate(text string) int {
	if e == nil {
		return 0
	}
	return e.applyCorrection(e.baseEstimator().Estimate(text))
}

func (e *CalibratedEstimator) EstimateMessages(msgs []Message) int {
	if e == nil {
		return 0
	}
	return e.applyCorrection(e.baseEstimator().EstimateMessages(msgs))
}

func (e *CalibratedEstimator) RecordActual(estimated, actual int) {
	if e == nil || estimated <= 0 || actual <= 0 {
		return
	}
	ratio := float64(actual) / float64(estimated)
	if ratio < calibratedEstimatorMinScale {
		ratio = calibratedEstimatorMinScale
	}
	if ratio > calibratedEstimatorMaxScale {
		ratio = calibratedEstimatorMaxScale
	}
	alpha := e.alpha
	if alpha <= 0 || alpha > 1 {
		alpha = calibratedEstimatorAlpha
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.correction = e.correction + alpha*(ratio-e.correction)
}

func (e *CalibratedEstimator) CorrectionFactor() float64 {
	if e == nil {
		return 1.0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.correction <= 0 {
		return 1.0
	}
	return e.correction
}

// ratioForText picks the chars-per-token ratio based on content type and
// CJK density.
func (e CharRatioEstimator) ratioForText(text string, isTool bool) float64 {
	ratio := e.CharsPerToken
	if ratio <= 0 {
		ratio = defaultCharsPerToken
	}
	if isTool && e.ToolCharsPerToken > 0 {
		ratio = e.ToolCharsPerToken
	}
	// CJK-heavy text needs a tighter ratio.
	if isCJKHeavy(text) && ratio > cjkDensityRatio {
		ratio = cjkDensityRatio
	}
	return ratio
}

func (e *CalibratedEstimator) baseEstimator() TokenEstimator {
	if e == nil || e.base == nil {
		return CharRatioEstimator{}
	}
	return e.base
}

func (e *CalibratedEstimator) applyCorrection(estimate int) int {
	if estimate <= 0 {
		return 0
	}
	return ints.Max(1, int(math.Ceil(float64(estimate)*e.CorrectionFactor())))
}

// isCJKHeavy returns true when more than cjkThreshold of the runes in text
// are CJK Unified Ideographs (common Chinese/Japanese/Korean characters).
func isCJKHeavy(text string) bool {
	if len(text) == 0 {
		return false
	}
	// Sample up to 200 runes to avoid scanning very large strings.
	const maxSample = 200
	total, cjk := 0, 0
	for i := 0; i < len(text) && total < maxSample; {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		total++
		if isCJKRune(r) {
			cjk++
		}
	}
	return float64(cjk)/float64(total) >= cjkThreshold
}

// isCJKRune reports whether r is a CJK Unified Ideograph, a common
// fullwidth punctuation character, or a Hangul syllable.
func isCJKRune(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility Ideographs
		(r >= 0x3000 && r <= 0x303F) || // CJK Symbols and Punctuation
		(r >= 0xFF00 && r <= 0xFFEF) || // Fullwidth Forms
		(r >= 0xAC00 && r <= 0xD7AF) // Hangul Syllables
}
