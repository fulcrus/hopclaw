package agent

import (
	"strings"
	"unicode"
)

// LanguageProfile describes the detected language characteristics of a message.
// Profiles may be populated by the unified ingress model path or synthesized
// locally for direct submit flows so multilingual requests still carry a stable
// semantic-language hint into the shared analyzer chain.
type LanguageProfile struct {
	Family                  string  `json:"family,omitempty"`
	Script                  string  `json:"script,omitempty"`
	SupportsKeywordFallback bool    `json:"supports_keyword_fallback,omitempty"`
	MainSemanticPath        bool    `json:"main_semantic_path,omitempty"`
	Confidence              float64 `json:"confidence,omitempty"`
}

type languageScriptCounts struct {
	latin      int
	han        int
	hiragana   int
	katakana   int
	hangul     int
	cyrillic   int
	arabic     int
	hebrew     int
	devanagari int
	greek      int
	thai       int
}

func detectLanguageProfile(message string) LanguageProfile {
	text := strings.TrimSpace(message)
	if text == "" {
		return LanguageProfile{}
	}

	counts := countLanguageScripts(text)
	total := counts.total()
	if total == 0 {
		return LanguageProfile{}
	}

	family, script, confidence := classifyLanguageProfile(text, counts, total)
	if family == "" || script == "" {
		return LanguageProfile{}
	}

	return normalizeLanguageProfile(LanguageProfile{
		Family:                  family,
		Script:                  script,
		SupportsKeywordFallback: false,
		MainSemanticPath:        true,
		Confidence:              confidence,
	})
}

func normalizeLanguageProfile(profile LanguageProfile) LanguageProfile {
	profile.Family = strings.TrimSpace(strings.ToLower(profile.Family))
	profile.Script = strings.TrimSpace(profile.Script)
	if profile.Confidence < 0 {
		profile.Confidence = 0
	}
	if profile.Confidence > 1 {
		profile.Confidence = 1
	}
	if profile.Family == "" || profile.Script == "" {
		return LanguageProfile{}
	}
	return profile
}

func mergeLanguageProfile(current, detected LanguageProfile) LanguageProfile {
	current = normalizeLanguageProfile(current)
	detected = normalizeLanguageProfile(detected)
	if current == (LanguageProfile{}) {
		return detected
	}
	if detected == (LanguageProfile{}) {
		return current
	}
	if current.Family == "" {
		current.Family = detected.Family
	}
	if current.Script == "" {
		current.Script = detected.Script
	}
	current.SupportsKeywordFallback = current.SupportsKeywordFallback || detected.SupportsKeywordFallback
	current.MainSemanticPath = current.MainSemanticPath || detected.MainSemanticPath
	if detected.Confidence > current.Confidence {
		current.Confidence = detected.Confidence
	}
	return current
}

func countLanguageScripts(text string) languageScriptCounts {
	var counts languageScriptCounts
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han):
			counts.han++
		case unicode.In(r, unicode.Hiragana):
			counts.hiragana++
		case unicode.In(r, unicode.Katakana):
			counts.katakana++
		case unicode.In(r, unicode.Hangul):
			counts.hangul++
		case unicode.In(r, unicode.Cyrillic):
			counts.cyrillic++
		case unicode.In(r, unicode.Arabic):
			counts.arabic++
		case unicode.In(r, unicode.Hebrew):
			counts.hebrew++
		case unicode.In(r, unicode.Devanagari):
			counts.devanagari++
		case unicode.In(r, unicode.Greek):
			counts.greek++
		case unicode.In(r, unicode.Thai):
			counts.thai++
		case unicode.In(r, unicode.Latin):
			if unicode.IsLetter(r) {
				counts.latin++
			}
		}
	}
	return counts
}

func (c languageScriptCounts) total() int {
	return c.latin +
		c.han +
		c.hiragana +
		c.katakana +
		c.hangul +
		c.cyrillic +
		c.arabic +
		c.hebrew +
		c.devanagari +
		c.greek +
		c.thai
}

func classifyLanguageProfile(text string, counts languageScriptCounts, total int) (string, string, float64) {
	switch {
	case counts.hiragana+counts.katakana > 0:
		return "ja", "Jpan", scriptConfidence(counts.han+counts.hiragana+counts.katakana, total)
	case counts.hangul > 0:
		return "ko", "Kore", scriptConfidence(counts.hangul, total)
	case counts.han > 0:
		return "zh", "Han", scriptConfidence(counts.han, total)
	case counts.arabic > 0:
		return "ar", "Arab", scriptConfidence(counts.arabic, total)
	case counts.hebrew > 0:
		return "he", "Hebr", scriptConfidence(counts.hebrew, total)
	case counts.cyrillic > 0:
		return "ru", "Cyrl", scriptConfidence(counts.cyrillic, total)
	case counts.devanagari > 0:
		return "hi", "Deva", scriptConfidence(counts.devanagari, total)
	case counts.greek > 0:
		return "el", "Grek", scriptConfidence(counts.greek, total)
	case counts.thai > 0:
		return "th", "Thai", scriptConfidence(counts.thai, total)
	case counts.latin > 0:
		family, confidence := detectLatinLanguageFamily(text, counts, total)
		if family == "" {
			return "", "", 0
		}
		return family, "Latn", confidence
	default:
		return "", "", 0
	}
}

func scriptConfidence(dominant, total int) float64 {
	if dominant <= 0 || total <= 0 {
		return 0
	}
	ratio := float64(dominant) / float64(total)
	switch {
	case ratio >= 0.95:
		return 0.99
	case ratio >= 0.85:
		return 0.95
	case ratio >= 0.7:
		return 0.9
	case ratio >= 0.55:
		return 0.82
	default:
		return 0.72
	}
}

var supportedSemanticLanguageFamilies = []string{
	"ar",
	"de",
	"el",
	"es",
	"fr",
	"he",
	"hi",
	"ja",
	"ko",
	"pt",
	"ru",
	"th",
	"und",
	"zh",
}

type latinLexicalFamilyProfile struct {
	family   string
	minScore int
	markers  map[string]int
}

var latinLexicalFamilyProfiles = []latinLexicalFamilyProfile{
	{
		family:   "es",
		minScore: 3,
		markers: map[string]int{
			"abre": 2, "abrir": 2,
			"guarda": 2, "guardar": 2,
			"resumen": 2, "resume": 1,
			"pagina": 1, "página": 1,
			"archivo": 1, "sitio": 1, "esta": 1, "está": 1, "puedes": 1,
		},
	},
	{
		family:   "pt",
		minScore: 3,
		markers: map[string]int{
			"salva": 2, "salvar": 2,
			"resumo": 2,
			"abre":   1,
			"pagina": 1, "página": 1,
			"arquivo": 1, "site": 1, "esta": 1, "está": 1, "voce": 1, "você": 1,
		},
	},
	{
		family:   "fr",
		minScore: 3,
		markers: map[string]int{
			"ouvre": 2, "ouvrir": 2,
			"enregistre": 2, "enregistrer": 2,
			"fichier": 2,
			"page":    1, "peux": 1, "pouvez": 1, "résumé": 2,
		},
	},
	{
		family:   "de",
		minScore: 3,
		markers: map[string]int{
			"oeffne": 2, "öffne": 2,
			"speichere": 2, "speichern": 2,
			"zusammenfassung": 2,
			"seite":           1, "datei": 1,
		},
	},
}

// SupportedSemanticLanguageFamilies lists the language families that currently
// enter the shared semantic analyzer chain through built-in language profiling.
func SupportedSemanticLanguageFamilies() []string {
	return append([]string(nil), supportedSemanticLanguageFamilies...)
}

func detectLatinLanguageFamily(text string, counts languageScriptCounts, total int) (string, float64) {
	lower := strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.ContainsAny(lower, "¿¡ñ"):
		return "es", latinOrthographyConfidence(counts, total)
	case strings.ContainsAny(lower, "ãõ"):
		return "pt", latinOrthographyConfidence(counts, total)
	case strings.ContainsAny(lower, "œæ"):
		return "fr", latinOrthographyConfidence(counts, total)
	case strings.ContainsAny(lower, "äöüß"):
		return "de", latinOrthographyConfidence(counts, total)
	default:
		if family, confidence, ok := detectLatinLanguageFamilyByLexicon(lower, counts, total); ok {
			return family, confidence
		}
		return "und", latinScriptConfidence(lower, counts, total)
	}
}

func detectLatinLanguageFamilyByLexicon(lower string, counts languageScriptCounts, total int) (string, float64, bool) {
	tokens := latinLexicalTokens(lower)
	if len(tokens) == 0 {
		return "", 0, false
	}
	bestFamily := ""
	bestScore := 0
	secondScore := 0
	bestMinScore := 0
	for _, profile := range latinLexicalFamilyProfiles {
		score := 0
		for token := range tokens {
			score += profile.markers[token]
		}
		if score > bestScore {
			secondScore = bestScore
			bestScore = score
			bestFamily = profile.family
			bestMinScore = profile.minScore
			continue
		}
		if score > secondScore {
			secondScore = score
		}
	}
	if bestScore < bestMinScore || bestScore == secondScore {
		return "", 0, false
	}
	return bestFamily, latinLexicalConfidence(lower, counts, total, bestScore), true
}

func latinLexicalTokens(lower string) map[string]struct{} {
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		out[field] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func latinLexicalConfidence(lower string, counts languageScriptCounts, total int, score int) float64 {
	confidence := latinScriptConfidence(lower, counts, total)
	switch {
	case score >= 5:
		if confidence < 0.82 {
			return 0.82
		}
	case score >= 4:
		if confidence < 0.76 {
			return 0.76
		}
	default:
		if confidence < 0.7 {
			return 0.7
		}
	}
	return confidence
}

func latinOrthographyConfidence(counts languageScriptCounts, total int) float64 {
	confidence := scriptConfidence(counts.latin, total)
	if confidence < 0.84 {
		return 0.84
	}
	return confidence
}

func latinScriptConfidence(lower string, counts languageScriptCounts, total int) float64 {
	confidence := scriptConfidence(counts.latin, total)
	if strings.IndexFunc(lower, func(r rune) bool {
		return unicode.In(r, unicode.Latin) && r > unicode.MaxASCII
	}) >= 0 {
		if confidence < 0.64 {
			return 0.64
		}
		return confidence
	}
	if confidence < 0.58 {
		return 0.58
	}
	return confidence
}
