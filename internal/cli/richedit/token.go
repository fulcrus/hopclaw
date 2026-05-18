package richedit

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mattn/go-runewidth"
)

const tokenKindCount = int(TokenLink) + 1

// TokenKind distinguishes text from structured attachment content in the document.
type TokenKind int

const (
	TokenText TokenKind = iota
	TokenImage
	TokenFile
	TokenDir
	TokenVideo
	TokenBlock
	TokenLink
)

// Token is the atomic unit of the document buffer.
// A text token holds one rune. Attachment tokens hold structured metadata.
type Token struct {
	Kind      TokenKind
	Rune      rune
	ID        int
	Label     string
	Path      string
	ImageData string
	MediaType string
	BlockText string
	LinkURL   string
}

// DisplayWidth returns the visual column width of this token.
func (t Token) DisplayWidth() int {
	if t.IsAttachment() {
		return runewidth.StringWidth(t.DisplayText())
	}
	return runewidth.RuneWidth(t.Rune)
}

func (t Token) IsAttachment() bool {
	return t.Kind != TokenText
}

func (t Token) DisplayText() string {
	switch t.Kind {
	case TokenImage:
		return fmt.Sprintf("[IMAGE#%d]", t.ID)
	case TokenFile:
		return fmt.Sprintf("[FILE#%d: %s]", t.ID, defaultTokenLabel(t, filepath.Base(strings.TrimSpace(t.Path))))
	case TokenDir:
		return fmt.Sprintf("[DIR#%d: %s]", t.ID, defaultTokenLabel(t, dirDisplayLabel(strings.TrimSpace(t.Path))))
	case TokenVideo:
		return fmt.Sprintf("[VIDEO#%d: %s]", t.ID, defaultTokenLabel(t, filepath.Base(strings.TrimSpace(t.Path))))
	case TokenBlock:
		return fmt.Sprintf("[BLOCK#%d: %s]", t.ID, defaultTokenLabel(t, blockLineLabel(t.BlockText)))
	case TokenLink:
		return fmt.Sprintf("[LINK#%d]", t.ID)
	default:
		return string(t.Rune)
	}
}

func (t Token) RailText() string {
	switch t.Kind {
	case TokenImage:
		return imageDetailTitle(t)
	case TokenFile, TokenDir, TokenVideo:
		return strings.TrimSpace(t.DisplayText())
	case TokenBlock:
		return fmt.Sprintf("BLOCK#%d · %s", t.ID, defaultTokenLabel(t, blockLineLabel(t.BlockText)))
	case TokenLink:
		if strings.TrimSpace(t.LinkURL) == "" {
			return fmt.Sprintf("LINK#%d", t.ID)
		}
		return fmt.Sprintf("LINK#%d · %s", t.ID, t.LinkURL)
	default:
		return ""
	}
}

func (t Token) RailBadge() string {
	switch t.Kind {
	case TokenImage:
		return fmt.Sprintf("IMAGE#%d", t.ID)
	case TokenFile:
		return fmt.Sprintf("FILE#%d", t.ID)
	case TokenDir:
		return fmt.Sprintf("DIR#%d", t.ID)
	case TokenVideo:
		return fmt.Sprintf("VIDEO#%d", t.ID)
	case TokenBlock:
		return fmt.Sprintf("BLOCK#%d", t.ID)
	case TokenLink:
		return fmt.Sprintf("LINK#%d", t.ID)
	default:
		return ""
	}
}

func (t Token) DetailLines() []string {
	switch t.Kind {
	case TokenImage:
		lines := []string{imageDetailTitle(t)}
		if path := strings.TrimSpace(t.Path); path != "" {
			lines = append(lines, path)
		}
		return lines
	case TokenFile, TokenDir, TokenVideo:
		lines := []string{strings.TrimSpace(t.DisplayText())}
		if path := strings.TrimSpace(t.Path); path != "" {
			lines = append(lines, path)
		}
		return lines
	case TokenBlock:
		preview := strings.TrimSpace(t.BlockText)
		if preview == "" {
			return []string{fmt.Sprintf("BLOCK#%d", t.ID)}
		}
		lines := strings.Split(preview, "\n")
		if len(lines) > 4 {
			lines = lines[:4]
		}
		header := fmt.Sprintf("BLOCK#%d · %s", t.ID, blockLineLabel(t.BlockText))
		return append([]string{header}, lines...)
	case TokenLink:
		if strings.TrimSpace(t.LinkURL) == "" {
			return []string{fmt.Sprintf("LINK#%d", t.ID)}
		}
		return []string{fmt.Sprintf("LINK#%d", t.ID), t.LinkURL}
	default:
		return nil
	}
}

// textToken creates a text token from a rune.
func textToken(r rune) Token {
	return Token{Kind: TokenText, Rune: r}
}

func imageToken(id int, dataURI, mediaType, path string) Token {
	return Token{
		Kind:      TokenImage,
		ID:        id,
		Path:      strings.TrimSpace(path),
		ImageData: dataURI,
		MediaType: strings.TrimSpace(mediaType),
		Label:     filepath.Base(strings.TrimSpace(path)),
	}
}

func fileToken(id int, path string) Token {
	path = strings.TrimSpace(path)
	return Token{Kind: TokenFile, ID: id, Path: path, Label: filepath.Base(path)}
}

func dirToken(id int, path string) Token {
	path = strings.TrimSpace(path)
	return Token{Kind: TokenDir, ID: id, Path: path, Label: dirDisplayLabel(path)}
}

func videoToken(id int, path string) Token {
	path = strings.TrimSpace(path)
	return Token{Kind: TokenVideo, ID: id, Path: path, Label: filepath.Base(path)}
}

func blockToken(id int, content string) Token {
	return Token{Kind: TokenBlock, ID: id, Label: blockLineLabel(content), BlockText: content}
}

func linkToken(id int, url string) Token {
	return Token{Kind: TokenLink, ID: id, LinkURL: strings.TrimSpace(url)}
}

func defaultTokenLabel(t Token, fallback string) string {
	if strings.TrimSpace(t.Label) != "" {
		return strings.TrimSpace(t.Label)
	}
	return strings.TrimSpace(fallback)
}

func dirDisplayLabel(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if cleaned == string(filepath.Separator) {
		return cleaned
	}
	if base := filepath.Base(cleaned); base != "" && base != "." {
		return base
	}
	return cleaned
}

func blockLineLabel(content string) string {
	lines := 1
	if content == "" {
		lines = 0
	} else {
		lines = strings.Count(content, "\n") + 1
	}
	if lines == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", lines)
}

func imageDetailTitle(t Token) string {
	name := strings.TrimSpace(t.Label)
	if name == "" {
		name = filepath.Base(strings.TrimSpace(t.Path))
	}
	parts := []string{fmt.Sprintf("IMAGE#%d", t.ID)}
	if name != "" && name != "." {
		parts = append(parts, name)
	}
	if strings.TrimSpace(t.MediaType) != "" {
		parts = append(parts, t.MediaType)
	}
	if data, mediaType := decodeDataURI(t.ImageData); data != nil {
		parts = append(parts, humanSize(len(data)))
		if strings.TrimSpace(t.MediaType) == "" && mediaType != "" {
			parts = append(parts, mediaType)
		}
	}
	return strings.Join(parts, " · ")
}

func humanSize(size int) string {
	if size <= 0 {
		return "0 B"
	}
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%d KB", (size+1023)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/1024.0/1024.0)
}
