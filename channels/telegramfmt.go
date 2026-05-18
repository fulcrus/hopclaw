package channels

import "strings"

// MarkdownToTelegramHTML converts a subset of Markdown to Telegram-safe HTML.
// Supported: **bold**, *italic*, `code`, ```pre```, [text](url).
// Unsupported constructs are passed through as-is.
func MarkdownToTelegramHTML(md string) string {
	// First, escape HTML entities in the raw text.
	md = telegramEscapeHTML(md)

	// Code blocks: ```...``` → <pre>...</pre>
	md = replaceDelimited(md, "```", "<pre>", "</pre>")

	// Inline code: `...` → <code>...</code>
	md = replaceDelimited(md, "`", "<code>", "</code>")

	// Bold: **...** → <b>...</b>
	md = replaceDelimited(md, "**", "<b>", "</b>")

	// Italic: *...* → <i>...</i>  (but not inside words with **)
	md = replaceDelimited(md, "*", "<i>", "</i>")

	// Links: [text](url) → <a href="url">text</a>
	md = convertMarkdownLinks(md)

	return md
}

func telegramEscapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// replaceDelimited replaces paired delimiters with open/close HTML tags.
// e.g., replaceDelimited("hello **world**", "**", "<b>", "</b>") → "hello <b>world</b>"
func replaceDelimited(s, delim, open, close string) string {
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for {
		idx := strings.Index(s, delim)
		if idx < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:idx])
		if inTag {
			b.WriteString(close)
		} else {
			b.WriteString(open)
		}
		inTag = !inTag
		s = s[idx+len(delim):]
	}
	return b.String()
}

// convertMarkdownLinks converts [text](url) to <a href="url">text</a>.
func convertMarkdownLinks(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for {
		openBracket := strings.Index(s, "[")
		if openBracket < 0 {
			b.WriteString(s)
			break
		}
		closeBracket := strings.Index(s[openBracket:], "](")
		if closeBracket < 0 {
			b.WriteString(s)
			break
		}
		closeBracket += openBracket
		closeParen := strings.Index(s[closeBracket+2:], ")")
		if closeParen < 0 {
			b.WriteString(s)
			break
		}
		closeParen += closeBracket + 2

		b.WriteString(s[:openBracket])
		text := s[openBracket+1 : closeBracket]
		href := s[closeBracket+2 : closeParen]
		b.WriteString("<a href=\"")
		b.WriteString(href)
		b.WriteString("\">")
		b.WriteString(text)
		b.WriteString("</a>")
		s = s[closeParen+1:]
	}
	return b.String()
}
