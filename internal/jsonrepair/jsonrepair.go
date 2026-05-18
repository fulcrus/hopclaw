package jsonrepair

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var trailingCommaPattern = regexp.MustCompile(`,(\s*[}\]])`)

func DecodeJSONObjectCandidate(raw string, dst any) error {
	candidates := candidateForms(raw)
	var lastErr error
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(candidate), dst); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no JSON object candidate found")
	}
	return lastErr
}

func Repair(raw string) string {
	candidate := stripCodeFences(raw)
	candidate = normalizeQuotes(candidate)
	candidate = extractJSONObject(candidate)
	candidate = trailingCommaPattern.ReplaceAllString(candidate, "$1")
	candidate = balanceBraces(candidate)
	return strings.TrimSpace(candidate)
}

func candidateForms(raw string) []string {
	primary := strings.TrimSpace(raw)
	repaired := Repair(primary)
	if repaired == primary {
		return []string{primary}
	}
	return []string{primary, repaired}
}

func stripCodeFences(raw string) string {
	candidate := strings.TrimSpace(raw)
	candidate = strings.TrimPrefix(candidate, "```json")
	candidate = strings.TrimPrefix(candidate, "```JSON")
	candidate = strings.TrimPrefix(candidate, "```")
	candidate = strings.TrimSuffix(candidate, "```")
	return strings.TrimSpace(candidate)
}

func normalizeQuotes(raw string) string {
	replacer := strings.NewReplacer(
		"“", `"`,
		"”", `"`,
		"‘", `'`,
		"’", `'`,
	)
	return replacer.Replace(raw)
}

func extractJSONObject(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		return raw[start : end+1]
	}
	if start >= 0 {
		return raw[start:]
	}
	return raw
}

func balanceBraces(raw string) string {
	var curlyOpen, curlyClose, squareOpen, squareClose int
	for _, ch := range raw {
		switch ch {
		case '{':
			curlyOpen++
		case '}':
			curlyClose++
		case '[':
			squareOpen++
		case ']':
			squareClose++
		}
	}
	if squareOpen > squareClose {
		raw += strings.Repeat("]", squareOpen-squareClose)
	}
	if curlyOpen > curlyClose {
		raw += strings.Repeat("}", curlyOpen-curlyClose)
	}
	return raw
}
