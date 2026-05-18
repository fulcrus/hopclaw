package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"mime"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type chunkPart struct {
	content   string
	startRune int
	endRune   int
}

func populateDocumentMetadata(document Document, content string) Document {
	content = normalizeChunkContent(content)
	document.Path = strings.TrimSpace(document.Path)
	document.URI = strings.TrimSpace(document.URI)
	document.Title = strings.TrimSpace(document.Title)
	document.Locale = normalizeLocale(document.Locale)
	if document.Locale == "" {
		document.Locale = detectLocale(firstNonEmpty(content, document.Title))
	}
	if document.Bytes == 0 {
		document.Bytes = int64(len(content))
	}
	if document.ContentHash == "" {
		document.ContentHash = hashKnowledgeText(content)
	}
	if document.Metadata.Extension == "" {
		document.Metadata.Extension = strings.ToLower(filepath.Ext(document.Path))
	}
	if document.Metadata.MIMEType == "" && document.Metadata.Extension != "" {
		document.Metadata.MIMEType = mime.TypeByExtension(document.Metadata.Extension)
	}
	return document
}

func buildDocumentSnapshot(document Document, content string) DocumentSnapshot {
	content = normalizeChunkContent(content)
	document = populateDocumentMetadata(document, content)
	return DocumentSnapshot{
		Document: document,
		Content:  content,
	}
}

func buildDocumentChunks(document Document, content string) []Chunk {
	content = normalizeChunkContent(content)
	if content == "" {
		return nil
	}
	document = populateDocumentMetadata(document, content)
	parts := splitIntoChunkParts(content, chunkRuneLimit, chunkOverlapRuneSize)
	out := make([]Chunk, 0, len(parts))
	for idx, part := range parts {
		payload := strings.TrimSpace(part.content)
		sum := sha256.Sum256([]byte(document.SourceID + "|" + document.ID + "|" + payload + "|" + strconvInt(idx)))
		hash := hex.EncodeToString(sum[:])
		out = append(out, Chunk{
			ID:         hash,
			SourceID:   document.SourceID,
			DocumentID: document.ID,
			Ordinal:    idx,
			Title:      document.Title,
			Path:       document.Path,
			URI:        document.URI,
			Locale:     document.Locale,
			Content:    payload,
			Preview:    buildPreview(payload),
			Hash:       hashKnowledgeText(payload),
			Bytes:      int64(len(payload)),
			StartRune:  part.startRune,
			EndRune:    part.endRune,
			UpdatedAt:  document.SourceUpdatedAt,
			Metadata: ChunkMetadata{
				TokenCount: len(strings.Fields(payload)),
			},
		})
	}
	return out
}

func splitIntoChunkParts(content string, limit int, overlap int) []chunkPart {
	if limit <= 0 {
		return []chunkPart{{content: strings.TrimSpace(content), startRune: 0, endRune: len([]rune(content))}}
	}
	runes := []rune(content)
	if len(runes) <= limit {
		return []chunkPart{{content: strings.TrimSpace(content), startRune: 0, endRune: len(runes)}}
	}
	if overlap < 0 {
		overlap = 0
	}
	step := limit - overlap
	if step <= 0 {
		step = limit
	}
	out := make([]chunkPart, 0, len(runes)/step+1)
	for start := 0; start < len(runes); start += step {
		end := start + limit
		if end > len(runes) {
			end = len(runes)
		}
		part := strings.TrimSpace(string(runes[start:end]))
		if part != "" {
			out = append(out, chunkPart{
				content:   part,
				startRune: start,
				endRune:   end,
			})
		}
		if end == len(runes) {
			break
		}
	}
	return out
}

func hashKnowledgeText(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func detectLocale(value string) string {
	hasLatin := false
	for _, r := range value {
		switch {
		case unicode.Is(unicode.Han, r):
			return "zh-CN"
		case unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
			return "ja"
		case unicode.Is(unicode.Hangul, r):
			return "ko"
		case unicode.IsLetter(r) && r <= unicode.MaxASCII:
			hasLatin = true
		}
	}
	if hasLatin {
		return "en"
	}
	return ""
}

func normalizeLocale(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "zh", "zh-cn", "zh_hans":
		return "zh-CN"
	case "en", "en-us", "en_us":
		return "en"
	case "ja", "ja-jp", "ja_jp":
		return "ja"
	case "ko", "ko-kr", "ko_kr":
		return "ko"
	default:
		return value
	}
}

func localeMatchBoost(queryLocale, candidateLocale string) float64 {
	queryLocale = normalizeLocale(queryLocale)
	candidateLocale = normalizeLocale(candidateLocale)
	switch {
	case queryLocale == "" || candidateLocale == "":
		return 0
	case queryLocale == candidateLocale:
		return 0.25
	case localeFamily(queryLocale) == localeFamily(candidateLocale):
		return 0.1
	default:
		return 0
	}
}

func localeFamily(value string) string {
	value = normalizeLocale(value)
	if idx := strings.Index(value, "-"); idx > 0 {
		return value[:idx]
	}
	return value
}

func sortSearchResults(results []SearchResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if !results[i].UpdatedAt.Equal(results[j].UpdatedAt) {
			return results[i].UpdatedAt.After(results[j].UpdatedAt)
		}
		return results[i].ChunkID < results[j].ChunkID
	})
}

func documentChanged(existing *Document, next Document) bool {
	if existing == nil {
		return true
	}
	return existing.Kind != next.Kind ||
		existing.Title != next.Title ||
		existing.Path != next.Path ||
		existing.URI != next.URI ||
		existing.Locale != next.Locale ||
		existing.ContentHash != next.ContentHash ||
		existing.Bytes != next.Bytes ||
		existing.SourceUpdatedAt.UTC() != next.SourceUpdatedAt.UTC() ||
		existing.Metadata != next.Metadata
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func strconvInt(value int) string {
	return strconv.Itoa(value)
}

func cosineSimilarity32(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	var normA float64
	var normB float64
	for idx := range a {
		av := float64(a[idx])
		bv := float64(b[idx])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	denominator := math.Sqrt(normA) * math.Sqrt(normB)
	if denominator == 0 {
		return 0
	}
	return dot / denominator
}
