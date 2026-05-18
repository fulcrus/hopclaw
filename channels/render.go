package channels

import (
	"fmt"
	"html"
	"path"
	"strings"
)

// ---------------------------------------------------------------------------
// Shared block/attachment rendering helpers for channel adapters
// ---------------------------------------------------------------------------

// RenderBlocksAsMarkdown converts OutboundBlocks to a markdown string.
// Suitable for platforms that support markdown natively (Discord, Mattermost).
func RenderBlocksAsMarkdown(blocks []OutboundBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		part := renderBlockMarkdown(b)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n\n")
}

func renderBlockMarkdown(b OutboundBlock) string {
	content := strings.TrimSpace(b.Content)
	if content == "" {
		return ""
	}
	title := strings.TrimSpace(b.Title)
	if title == "" {
		return content
	}
	return fmt.Sprintf("**%s**\n%s", title, content)
}

// RenderBlocksAsPlain converts OutboundBlocks to a plain text string.
// Suitable for platforms with no formatting (IRC, Signal, WhatsApp).
func RenderBlocksAsPlain(blocks []OutboundBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		content := strings.TrimSpace(b.Content)
		if content == "" {
			continue
		}
		title := strings.TrimSpace(b.Title)
		if title != "" {
			parts = append(parts, title+":\n"+content)
		} else {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n")
}

// RenderAttachmentsAsText appends attachment URIs as a text list.
func RenderAttachmentsAsText(attachments []OutboundAttachment) string {
	if len(attachments) == 0 {
		return ""
	}
	var lines []string
	for _, att := range attachments {
		uri := strings.TrimSpace(att.URI)
		if uri == "" {
			continue
		}
		label := strings.TrimSpace(att.Label)
		if label == "" {
			label = path.Base(uri)
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", label, uri))
	}
	if len(lines) == 0 {
		return ""
	}
	return "Attachments:\n" + strings.Join(lines, "\n")
}

// HasRichContent returns true if the message carries blocks or attachments
// beyond what is already in Content.
func HasRichContent(msg OutboundMessage) bool {
	return len(msg.Blocks) > 0 || len(msg.Attachments) > 0
}

// ContentWithBlocks builds the full text from blocks + attachments,
// falling back to msg.Content when no blocks are present.
func ContentWithBlocks(msg OutboundMessage, renderFn func([]OutboundBlock) string) string {
	text := msg.Content
	if len(msg.Blocks) > 0 {
		rendered := renderFn(msg.Blocks)
		if rendered != "" {
			text = rendered
		}
	}
	if att := RenderAttachmentsAsText(msg.Attachments); att != "" {
		text = text + "\n\n" + att
	}
	return text
}

// AttachmentsByKind partitions attachments into files and images based on
// content type. Useful for platforms that send files/images as separate API
// calls (Telegram, WhatsApp, LINE).
func AttachmentsByKind(attachments []OutboundAttachment) (files, images []OutboundAttachment) {
	for _, att := range attachments {
		uri := strings.TrimSpace(att.URI)
		if uri == "" {
			continue
		}
		ct := strings.ToLower(strings.TrimSpace(att.ContentType))
		if strings.HasPrefix(ct, "image/") || isImageExt(uri) {
			images = append(images, att)
		} else {
			files = append(files, att)
		}
	}
	return files, images
}

// TruncateUTF8 truncates a string to at most maxLen runes and appends "..."
// if truncated. Safe for multibyte UTF-8 characters.
func TruncateUTF8(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// EscapeHTML escapes user content for safe embedding in HTML.
func EscapeHTML(s string) string {
	return html.EscapeString(s)
}

func isImageExt(uri string) bool {
	ext := strings.ToLower(path.Ext(uri))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return true
	}
	return false
}
