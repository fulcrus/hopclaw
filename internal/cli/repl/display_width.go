package repl

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

const displayEllipsis = "…"

func stripANSI(text string) string {
	if text == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(text))

	inEscape := false
	for _, r := range text {
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		if r == '\033' {
			inEscape = true
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func displayWidth(text string) int {
	return runewidth.StringWidth(stripANSI(text))
}

func displayPrefix(text string, width int) string {
	text = stripANSI(text)
	if width <= 0 || text == "" {
		return ""
	}
	if displayWidth(text) <= width {
		return text
	}
	var builder strings.Builder
	current := 0
	for _, r := range text {
		runeWidth := runewidth.RuneWidth(r)
		if runeWidth < 0 {
			runeWidth = 0
		}
		if current+runeWidth > width {
			break
		}
		builder.WriteRune(r)
		current += runeWidth
	}
	return builder.String()
}

func displaySuffix(text string, width int) string {
	text = stripANSI(text)
	if width <= 0 || text == "" {
		return ""
	}
	if displayWidth(text) <= width {
		return text
	}
	runes := []rune(text)
	collected := make([]rune, 0, len(runes))
	current := 0
	for i := len(runes) - 1; i >= 0; i-- {
		runeWidth := runewidth.RuneWidth(runes[i])
		if runeWidth < 0 {
			runeWidth = 0
		}
		if current+runeWidth > width {
			break
		}
		collected = append(collected, runes[i])
		current += runeWidth
	}
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}
	return string(collected)
}

func compactVisible(text string, limit int) string {
	return compactDisplay(stripANSI(text), limit)
}

func compactDisplay(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || text == "" {
		return text
	}
	if displayWidth(text) <= limit {
		return text
	}
	ellipsisWidth := displayWidth(displayEllipsis)
	if limit <= ellipsisWidth {
		return displayEllipsis
	}
	prefix := displayPrefix(text, limit-ellipsisWidth)
	if prefix == "" {
		return displayEllipsis
	}
	return prefix + displayEllipsis
}
