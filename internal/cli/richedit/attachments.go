package richedit

import (
	"fmt"
	"os"

	"github.com/fulcrus/hopclaw/contextengine"
)

// ParseAttachKind maps the canonical /attach type tokens onto composer token kinds.
func ParseAttachKind(value string) (TokenKind, bool) {
	return parseAttachKind(value)
}

// BuildAttachmentContentBlock resolves and validates a filesystem attachment and
// converts it into the shared content-block model used across clients.
func BuildAttachmentContentBlock(kind TokenKind, path string) (contextengine.ContentBlock, error) {
	tok, err := buildAttachmentToken(kind, path)
	if err != nil {
		return contextengine.ContentBlock{}, err
	}
	block, ok := tokenContentBlock(tok)
	if !ok {
		return contextengine.ContentBlock{}, fmt.Errorf("unsupported attachment type")
	}
	return block, nil
}

func buildAttachmentToken(kind TokenKind, path string) (Token, error) {
	path, err := resolvePath(path)
	if err != nil {
		return Token{}, err
	}
	switch kind {
	case TokenImage:
		dataURI, mediaType, err := readImageFile(path)
		if err != nil {
			return Token{}, fmt.Errorf("IMAGE cannot be read: %w", err)
		}
		return imageToken(0, dataURI, mediaType, path), nil
	case TokenFile:
		info, err := os.Stat(path)
		if err != nil {
			return Token{}, fmt.Errorf("FILE cannot be read: %w", err)
		}
		if info.IsDir() {
			return Token{}, fmt.Errorf("FILE cannot be read: path is a directory")
		}
		return fileToken(0, path), nil
	case TokenDir:
		info, err := os.Stat(path)
		if err != nil {
			return Token{}, fmt.Errorf("DIR cannot be read: %w", err)
		}
		if !info.IsDir() {
			return Token{}, fmt.Errorf("DIR cannot be read: path is not a directory")
		}
		return dirToken(0, path), nil
	case TokenVideo:
		info, err := os.Stat(path)
		if err != nil {
			return Token{}, fmt.Errorf("VIDEO cannot be read: %w", err)
		}
		if info.IsDir() {
			return Token{}, fmt.Errorf("VIDEO cannot be read: path is a directory")
		}
		if !looksLikeVideo(path) {
			return Token{}, fmt.Errorf("VIDEO cannot be read: unsupported format")
		}
		return videoToken(0, path), nil
	default:
		return Token{}, fmt.Errorf("unsupported attachment type")
	}
}
