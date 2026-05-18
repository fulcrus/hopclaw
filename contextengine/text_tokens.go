package contextengine

import (
	"strings"
	"unicode"
)

type textTokenClass uint8

const (
	textTokenClassSeparator textTokenClass = iota
	textTokenClassASCIIWord
	textTokenClassCJK
	textTokenClassUnicodeWord
)

func semanticTextTokens(text string, minWordRunes int) []string {
	if minWordRunes < 1 {
		minWordRunes = 1
	}
	seen := make(map[string]struct{})
	tokens := make([]string, 0, 16)
	buffer := make([]rune, 0, 16)
	currentClass := textTokenClassSeparator

	addToken := func(token string) {
		token = strings.TrimSpace(strings.ToLower(token))
		if token == "" {
			return
		}
		if _, ok := seen[token]; ok {
			return
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}

	addNGrams := func(segment []rune, size int) {
		if len(segment) < size || size <= 0 {
			return
		}
		for i := 0; i <= len(segment)-size; i++ {
			addToken(string(segment[i : i+size]))
		}
	}

	flush := func() {
		if len(buffer) == 0 {
			return
		}
		segment := string(buffer)
		switch currentClass {
		case textTokenClassASCIIWord:
			if len(buffer) >= minWordRunes {
				addToken(segment)
			}
		case textTokenClassCJK:
			addNGrams(buffer, 2)
			if len(buffer) <= 4 {
				addToken(segment)
			}
		case textTokenClassUnicodeWord:
			if len(buffer) >= minWordRunes {
				addToken(segment)
			}
			if len(buffer) >= 5 {
				addNGrams(buffer, 3)
			}
		}
		buffer = buffer[:0]
		currentClass = textTokenClassSeparator
	}

	for _, r := range text {
		nextClass := classifyTextTokenRune(r)
		if nextClass == textTokenClassSeparator {
			flush()
			continue
		}
		if currentClass != textTokenClassSeparator && currentClass != nextClass {
			flush()
		}
		currentClass = nextClass
		buffer = append(buffer, unicode.ToLower(r))
	}
	flush()
	return tokens
}

func semanticTextTokenSet(text string, minWordRunes int) map[string]struct{} {
	tokens := semanticTextTokens(text, minWordRunes)
	if len(tokens) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		out[token] = struct{}{}
	}
	return out
}

func classifyTextTokenRune(r rune) textTokenClass {
	switch {
	case isASCIIWordRune(r):
		return textTokenClassASCIIWord
	case isCJKLikeRune(r):
		return textTokenClassCJK
	case unicode.IsLetter(r) || unicode.IsDigit(r):
		return textTokenClassUnicodeWord
	default:
		return textTokenClassSeparator
	}
}

func isASCIIWordRune(r rune) bool {
	return r == '_' || r == '-' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}

func isCJKLikeRune(r rune) bool {
	return isCJKRune(r) || unicode.In(r, unicode.Hiragana, unicode.Katakana)
}
