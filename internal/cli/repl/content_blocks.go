package repl

import (
	"strings"

	"github.com/fulcrus/hopclaw/contextengine"
)

// BuildLegacyImageContentBlocks upgrades legacy image payloads into the shared
// content_blocks representation while preserving plain text as the leading
// block when present.
func BuildLegacyImageContentBlocks(text string, images []string) []contextengine.ContentBlock {
	if len(images) == 0 {
		return nil
	}

	blocks := make([]contextengine.ContentBlock, 0, len(images)+1)
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, contextengine.ContentBlock{
			Type: contextengine.ContentBlockText,
			Text: text,
		})
	}
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if mediaType, data, ok := decodeLegacyImageDataURI(image); ok {
			blocks = append(blocks, contextengine.ContentBlock{
				Type:      contextengine.ContentBlockImage,
				MediaType: mediaType,
				Data:      data,
			})
			continue
		}
		blocks = append(blocks, contextengine.ContentBlock{
			Type:      contextengine.ContentBlockImage,
			SourceURL: image,
		})
	}
	if len(blocks) == 0 {
		return nil
	}
	return blocks
}

func decodeLegacyImageDataURI(value string) (mediaType string, data string, ok bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "data:") {
		return "", "", false
	}
	body := strings.TrimPrefix(value, "data:")
	header, payload, found := strings.Cut(body, ",")
	if !found {
		return "", "", false
	}
	mediaType, _, _ = strings.Cut(header, ";")
	mediaType = strings.TrimSpace(mediaType)
	if mediaType == "" {
		return "", "", false
	}
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", "", false
	}
	return mediaType, payload, true
}
