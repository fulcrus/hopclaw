package artifact

import (
	"mime"
	"strings"
)

const (
	PreviewDispositionInline     = "inline"
	PreviewDispositionAttachment = "attachment"
)

var inlinePreviewMediaTypes = map[string]struct{}{
	"text/plain":          {},
	"text/markdown":       {},
	"text/csv":            {},
	"application/json":    {},
	"application/ld+json": {},
	"image/png":           {},
	"image/jpeg":          {},
	"image/gif":           {},
	"image/webp":          {},
	"image/bmp":           {},
	"image/x-icon":        {},
}

func PreviewMediaType(contentType string) string {
	trimmed := strings.TrimSpace(contentType)
	if trimmed == "" {
		return "application/octet-stream"
	}
	mediaType, _, err := mime.ParseMediaType(trimmed)
	if err != nil || strings.TrimSpace(mediaType) == "" {
		return "application/octet-stream"
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}

func PreviewDisposition(contentType string) string {
	if IsSafeInlinePreviewType(contentType) {
		return PreviewDispositionInline
	}
	return PreviewDispositionAttachment
}

func IsSafeInlinePreviewType(contentType string) bool {
	mediaType := PreviewMediaType(contentType)
	_, ok := inlinePreviewMediaTypes[mediaType]
	return ok
}
