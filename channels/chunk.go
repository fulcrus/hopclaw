package channels

import "strings"

// ---------------------------------------------------------------------------
// Message chunking
// ---------------------------------------------------------------------------

// ChunkText splits text into chunks that do not exceed limit bytes.
// It splits on the last newline before the limit; if no newline is found,
// it splits on the last space. As a last resort it hard-cuts at limit.
// An empty or zero-limit input returns a single-element slice.
func ChunkText(text string, limit int) []string {
	if limit <= 0 || len(text) <= limit {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= limit {
			chunks = append(chunks, text)
			break
		}

		cut := limit
		// Prefer splitting on newline.
		if idx := strings.LastIndexByte(text[:cut], '\n'); idx > 0 {
			cut = idx + 1
		} else if idx := strings.LastIndexByte(text[:cut], ' '); idx > 0 {
			cut = idx + 1
		}
		chunks = append(chunks, text[:cut])
		text = text[cut:]
	}
	return chunks
}
