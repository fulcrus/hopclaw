package richedit

import (
	"strings"

	"github.com/fulcrus/hopclaw/contextengine"
)

// Document is a multi-line buffer of mixed text and attachment tokens.
type Document struct {
	Lines   [][]Token
	nextIDs [tokenKindCount]int
}

func NewDocument() *Document {
	return &Document{Lines: [][]Token{{}}}
}

func (d *Document) nextID(kind TokenKind) int {
	index := int(kind)
	if index < 0 || index >= len(d.nextIDs) {
		return 0
	}
	d.nextIDs[index]++
	return d.nextIDs[index]
}

func (d *Document) InsertRune(line, col int, r rune) {
	d.InsertTokens(line, col, []Token{textToken(r)})
}

func (d *Document) InsertTokens(line, col int, tokens []Token) {
	if line < 0 || line >= len(d.Lines) || len(tokens) == 0 {
		return
	}
	l := d.Lines[line]
	col = clampTokenIndex(col, len(l))
	inserted := append([]Token(nil), tokens...)
	d.Lines[line] = append(l[:col], append(inserted, l[col:]...)...)
}

func (d *Document) ReplaceRange(line, start, end int, tokens []Token) {
	if line < 0 || line >= len(d.Lines) {
		return
	}
	l := d.Lines[line]
	start = clampTokenIndex(start, len(l))
	end = clampTokenIndex(end, len(l))
	if end < start {
		end = start
	}
	replacement := append([]Token(nil), tokens...)
	d.Lines[line] = append(l[:start], append(replacement, l[end:]...)...)
}

func (d *Document) InsertNewline(line, col int) {
	if line < 0 || line >= len(d.Lines) {
		return
	}
	l := d.Lines[line]
	col = clampTokenIndex(col, len(l))
	after := append([]Token(nil), l[col:]...)
	d.Lines[line] = l[:col]
	newLines := make([][]Token, len(d.Lines)+1)
	copy(newLines, d.Lines[:line+1])
	newLines[line+1] = after
	copy(newLines[line+2:], d.Lines[line+1:])
	d.Lines = newLines
}

func (d *Document) InsertImage(line, col int, dataURI, mediaType, path string) int {
	id := d.nextID(TokenImage)
	d.InsertTokens(line, col, []Token{imageToken(id, dataURI, mediaType, path)})
	return id
}

func (d *Document) InsertFile(line, col int, path string) int {
	id := d.nextID(TokenFile)
	d.InsertTokens(line, col, []Token{fileToken(id, path)})
	return id
}

func (d *Document) InsertDir(line, col int, path string) int {
	id := d.nextID(TokenDir)
	d.InsertTokens(line, col, []Token{dirToken(id, path)})
	return id
}

func (d *Document) InsertVideo(line, col int, path string) int {
	id := d.nextID(TokenVideo)
	d.InsertTokens(line, col, []Token{videoToken(id, path)})
	return id
}

func (d *Document) InsertBlock(line, col int, content string) int {
	id := d.nextID(TokenBlock)
	d.InsertTokens(line, col, []Token{blockToken(id, content)})
	return id
}

func (d *Document) InsertLink(line, col int, url string) int {
	id := d.nextID(TokenLink)
	d.InsertTokens(line, col, []Token{linkToken(id, url)})
	return id
}

func (d *Document) DeleteBack(line, col int) (int, int) {
	if col > 0 {
		l := d.Lines[line]
		d.Lines[line] = append(l[:col-1], l[col:]...)
		return line, col - 1
	}
	if line > 0 {
		prevLen := len(d.Lines[line-1])
		d.Lines[line-1] = append(d.Lines[line-1], d.Lines[line]...)
		d.Lines = append(d.Lines[:line], d.Lines[line+1:]...)
		return line - 1, prevLen
	}
	return line, col
}

func (d *Document) DeleteForward(line, col int) {
	if line < 0 || line >= len(d.Lines) {
		return
	}
	l := d.Lines[line]
	if col < len(l) {
		d.Lines[line] = append(l[:col], l[col+1:]...)
		return
	}
	if line+1 < len(d.Lines) {
		d.Lines[line] = append(d.Lines[line], d.Lines[line+1]...)
		d.Lines = append(d.Lines[:line+1], d.Lines[line+2:]...)
	}
}

func (d *Document) KillToEnd(line, col int) []Token {
	if line < 0 || line >= len(d.Lines) {
		return nil
	}
	l := d.Lines[line]
	if col >= len(l) {
		return nil
	}
	killed := append([]Token(nil), l[col:]...)
	d.Lines[line] = l[:col]
	return killed
}

func (d *Document) KillToStart(line, col int) []Token {
	if line < 0 || line >= len(d.Lines) {
		return nil
	}
	l := d.Lines[line]
	if col <= 0 {
		return nil
	}
	col = clampTokenIndex(col, len(l))
	killed := append([]Token(nil), l[:col]...)
	d.Lines[line] = l[col:]
	return killed
}

func (d *Document) KillWord(line, col int) ([]Token, int) {
	if line < 0 || line >= len(d.Lines) || col <= 0 {
		return nil, col
	}
	l := d.Lines[line]
	col = clampTokenIndex(col, len(l))
	end := col
	start := col
	for start > 0 && l[start-1].Kind == TokenText && l[start-1].Rune == ' ' {
		start--
	}
	for start > 0 && l[start-1].Kind == TokenText && l[start-1].Rune != ' ' {
		start--
	}
	if start == end && start > 0 {
		start--
	}
	killed := append([]Token(nil), l[start:end]...)
	d.Lines[line] = append(l[:start], l[end:]...)
	return killed, start
}

// Text returns canonical inline text, including attachment anchors.
func (d *Document) Text() string {
	var sb strings.Builder
	for i, line := range d.Lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		for _, tok := range line {
			if tok.Kind == TokenText {
				sb.WriteRune(tok.Rune)
				continue
			}
			sb.WriteString(tok.DisplayText())
		}
	}
	return sb.String()
}

// SubmissionText returns the message body sent to the backend. Pure-image
// prompts keep the message empty for compatibility with the existing runtime.
func (d *Document) SubmissionText() string {
	if d.onlyImagePrompt() {
		return ""
	}
	return d.Text()
}

func (d *Document) ContentBlocks() []contextengine.ContentBlock {
	if d == nil || !d.HasAttachments() {
		return nil
	}

	blocks := make([]contextengine.ContentBlock, 0, d.TokenCount(0))
	var text strings.Builder
	flushText := func() {
		if text.Len() == 0 {
			return
		}
		blocks = append(blocks, contextengine.ContentBlock{
			Type: contextengine.ContentBlockText,
			Text: text.String(),
		})
		text.Reset()
	}

	for lineIndex, line := range d.Lines {
		if lineIndex > 0 {
			text.WriteByte('\n')
		}
		for _, tok := range line {
			if tok.Kind == TokenText {
				text.WriteRune(tok.Rune)
				continue
			}
			flushText()
			if block, ok := tokenContentBlock(tok); ok {
				blocks = append(blocks, block)
			}
		}
	}
	flushText()
	if len(blocks) == 0 {
		return nil
	}
	return blocks
}

func (d *Document) Images() []string {
	var imgs []string
	for _, line := range d.Lines {
		for _, tok := range line {
			if tok.Kind == TokenImage {
				imgs = append(imgs, tok.ImageData)
			}
		}
	}
	return imgs
}

func tokenContentBlock(tok Token) (contextengine.ContentBlock, bool) {
	switch tok.Kind {
	case TokenImage:
		mediaType, data := parseDataURIBase64(tok.ImageData)
		if data == "" {
			return contextengine.ContentBlock{}, false
		}
		if strings.TrimSpace(tok.MediaType) != "" {
			mediaType = strings.TrimSpace(tok.MediaType)
		}
		return contextengine.ContentBlock{
			Type:      contextengine.ContentBlockImage,
			Label:     strings.TrimSpace(tok.Label),
			Path:      strings.TrimSpace(tok.Path),
			MediaType: mediaType,
			Data:      data,
		}, true
	case TokenFile:
		return contextengine.ContentBlock{
			Type:  contextengine.ContentBlockFile,
			Label: strings.TrimSpace(tok.Label),
			Path:  strings.TrimSpace(tok.Path),
		}, true
	case TokenDir:
		return contextengine.ContentBlock{
			Type:  contextengine.ContentBlockDirectory,
			Label: strings.TrimSpace(tok.Label),
			Path:  strings.TrimSpace(tok.Path),
		}, true
	case TokenVideo:
		return contextengine.ContentBlock{
			Type:  contextengine.ContentBlockVideo,
			Label: strings.TrimSpace(tok.Label),
			Path:  strings.TrimSpace(tok.Path),
		}, true
	case TokenBlock:
		return contextengine.ContentBlock{
			Type:  contextengine.ContentBlockSnippet,
			Label: strings.TrimSpace(tok.Label),
			Text:  tok.BlockText,
		}, true
	case TokenLink:
		return contextengine.ContentBlock{
			Type:      contextengine.ContentBlockLink,
			SourceURL: strings.TrimSpace(tok.LinkURL),
		}, true
	default:
		return contextengine.ContentBlock{}, false
	}
}

func parseDataURIBase64(uri string) (mediaType, data string) {
	uri = strings.TrimSpace(uri)
	if !strings.HasPrefix(uri, "data:") {
		return "", ""
	}
	parts := strings.SplitN(uri, ",", 2)
	if len(parts) != 2 {
		return "", ""
	}
	header := strings.TrimPrefix(parts[0], "data:")
	if !strings.HasSuffix(strings.ToLower(header), ";base64") {
		return "", ""
	}
	return strings.TrimSuffix(header, ";base64"), parts[1]
}

func (d *Document) Attachments() []Token {
	var items []Token
	for _, line := range d.Lines {
		for _, tok := range line {
			if tok.IsAttachment() {
				items = append(items, tok)
			}
		}
	}
	return items
}

func (d *Document) LineCount() int { return len(d.Lines) }

func (d *Document) TokenCount(line int) int {
	if line < 0 || line >= len(d.Lines) {
		return 0
	}
	return len(d.Lines[line])
}

func (d *Document) HasImages() bool {
	for _, line := range d.Lines {
		for _, tok := range line {
			if tok.Kind == TokenImage {
				return true
			}
		}
	}
	return false
}

func (d *Document) HasAttachments() bool {
	for _, line := range d.Lines {
		for _, tok := range line {
			if tok.IsAttachment() {
				return true
			}
		}
	}
	return false
}

func (d *Document) IsEmpty() bool {
	return len(d.Lines) == 1 && len(d.Lines[0]) == 0
}

func (d *Document) Clone() *Document {
	clone := &Document{
		Lines: make([][]Token, len(d.Lines)),
	}
	copy(clone.nextIDs[:], d.nextIDs[:])
	for i, line := range d.Lines {
		clone.Lines[i] = append([]Token(nil), line...)
	}
	return clone
}

func (d *Document) Snapshot(cursor Cursor) DocumentSnapshot {
	snap := DocumentSnapshot{
		Cursor: cursor,
		Lines:  make([][]Token, len(d.Lines)),
	}
	copy(snap.NextIDs[:], d.nextIDs[:])
	for i, line := range d.Lines {
		snap.Lines[i] = append([]Token(nil), line...)
	}
	return snap
}

func (d *Document) Restore(snap DocumentSnapshot) {
	d.Lines = make([][]Token, len(snap.Lines))
	copy(d.nextIDs[:], snap.NextIDs[:])
	for i, line := range snap.Lines {
		d.Lines[i] = append([]Token(nil), line...)
	}
	if len(d.Lines) == 0 {
		d.Lines = [][]Token{{}}
	}
}

func tokensFromString(text string) []Token {
	if text == "" {
		return nil
	}
	tokens := make([]Token, 0, len(text))
	for _, r := range text {
		tokens = append(tokens, textToken(r))
	}
	return tokens
}

func clampTokenIndex(index, length int) int {
	if index < 0 {
		return 0
	}
	if index > length {
		return length
	}
	return index
}

func (d *Document) onlyImagePrompt() bool {
	hasImage := false
	for _, line := range d.Lines {
		for _, tok := range line {
			switch tok.Kind {
			case TokenImage:
				hasImage = true
			case TokenText:
				if !strings.ContainsRune(" \t\r\n", tok.Rune) {
					return false
				}
			default:
				return false
			}
		}
	}
	return hasImage
}
