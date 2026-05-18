package retriever

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

// Rerank applies the shared Phase 4 scoring formula to mixed-source hits and
// returns the highest-ranked subset.
func Rerank(hits []Hit, limit int) []Hit {
	if len(hits) == 0 {
		return nil
	}
	if limit <= 0 || limit > len(hits) {
		limit = len(hits)
	}

	type scoredHit struct {
		hit       Hit
		final     float64
		kindBonus float64
		index     int
	}

	scored := make([]scoredHit, 0, len(hits))
	for idx, hit := range hits {
		hit = normalizeHit(hit, hit.Kind, defaultAuthorityForKind(hit.Kind))
		kindBonus := kindBonusFor(hit.Kind)
		final := clamp01(0.40*hit.Score + 0.25*hit.Authority + 0.20*hit.Freshness + 0.15*kindBonus)
		hit.Score = final
		scored = append(scored, scoredHit{
			hit:       hit,
			final:     final,
			kindBonus: kindBonus,
			index:     idx,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		left := scored[i]
		right := scored[j]
		switch {
		case left.final != right.final:
			return left.final > right.final
		case left.hit.Authority != right.hit.Authority:
			return left.hit.Authority > right.hit.Authority
		case left.hit.Freshness != right.hit.Freshness:
			return left.hit.Freshness > right.hit.Freshness
		case left.kindBonus != right.kindBonus:
			return left.kindBonus > right.kindBonus
		case left.hit.Kind != right.hit.Kind:
			return left.hit.Kind < right.hit.Kind
		case left.hit.ID != right.hit.ID:
			return left.hit.ID < right.hit.ID
		default:
			return left.index < right.index
		}
	})

	out := make([]Hit, 0, limit)
	for _, item := range scored[:limit] {
		out = append(out, item.hit)
	}
	return out
}

func normalizeHits(hits []Hit, kind HitKind, defaultAuthority float64) []Hit {
	if len(hits) == 0 {
		return nil
	}

	out := make([]Hit, 0, len(hits))
	for _, hit := range hits {
		normalized := normalizeHit(hit, kind, defaultAuthority)
		if normalized.ID == "" && normalized.Content == "" && normalized.Citation == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func normalizeHit(hit Hit, kind HitKind, defaultAuthority float64) Hit {
	if kind != "" {
		hit.Kind = kind
	}
	if hit.Kind == "" {
		hit.Kind = HitSegment
	}

	hit.ID = strings.TrimSpace(hit.ID)
	hit.Reason = strings.TrimSpace(hit.Reason)
	hit.Scope = strings.TrimSpace(hit.Scope)
	hit.Content = strings.TrimSpace(hit.Content)
	hit.Citation = strings.TrimSpace(hit.Citation)
	hit.Score = clamp01(hit.Score)
	hit.Authority = clamp01(defaultIfZero(hit.Authority, defaultAuthority))
	hit.Freshness = clamp01(defaultIfZero(hit.Freshness, defaultFreshnessForKind(hit.Kind)))
	if hit.Citation == "" {
		if hit.ID != "" {
			hit.Citation = string(hit.Kind) + ":" + hit.ID
		} else {
			hit.Citation = string(hit.Kind)
		}
	}
	if hit.Tokens <= 0 {
		hit.Tokens = estimateTokens(hit.Content)
	}
	return hit
}

func defaultAuthorityForKind(kind HitKind) float64 {
	switch kind {
	case HitMemory:
		return 0.6
	case HitSegment:
		return 0.5
	case HitKnowledge:
		return 0.4
	default:
		return 0.5
	}
}

func defaultFreshnessForKind(kind HitKind) float64 {
	switch kind {
	case HitMemory:
		return 0.8
	case HitSegment:
		return 0.6
	case HitKnowledge:
		return 0.5
	default:
		return 0.5
	}
}

func kindBonusFor(kind HitKind) float64 {
	switch kind {
	case HitMemory:
		return 0.1
	case HitKnowledge:
		return 0.05
	case HitSegment:
		return 0.0
	default:
		return 0.0
	}
}

func estimateTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}

	if words := len(strings.Fields(text)); words > 0 {
		return maxInt(1, int(math.Ceil(float64(words)*1.3)))
	}
	return maxInt(1, int(math.Ceil(float64(utf8.RuneCountInString(text))/4.0)))
}

func defaultIfZero(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

func clamp01(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
