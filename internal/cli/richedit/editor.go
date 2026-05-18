package richedit

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fulcrus/hopclaw/contextengine"
	"golang.org/x/term"
)

// EditorConfig holds configuration for the editor.
type EditorConfig struct {
	Prompt     string
	Out        io.Writer
	In         *os.File
	History    EditorHistory // optional, for arrow-key history navigation
	Completion CompletionProvider
	Overlay    OverlayController
	Chrome     func(termWidth int) Chrome
}

// EditorHistory provides history navigation (Previous / Next).
// This matches the interface of repl.History without importing it.
type EditorHistory interface {
	Previous(current string) string
	Next() string
}

// DraftHistory is an optional richer history interface for full-token draft
// restore. It is discovered with a runtime type assertion to avoid importing
// repl from richedit.
type DraftHistory interface {
	AddDraft(DocumentSnapshot) error
	PreviousDraft(DocumentSnapshot) (DocumentSnapshot, bool)
	NextDraft() (DocumentSnapshot, bool)
}

type popupState struct {
	query    string
	items    []CompletionItem
	selected int
}

type expandedMode int

const (
	expandedPreview expandedMode = iota + 1
	expandedDetails
)

type expandedState struct {
	line int
	col  int
	mode expandedMode
}

type pasteDecision struct {
	content   string
	lineCount int
	charCount int
}

// Editor is the main rich-text input editor.
type Editor struct {
	doc           *Document
	cursor        Cursor
	undo          *UndoStack
	config        EditorConfig
	render        RenderState
	termWidth     int
	termHeight    int
	inPaste       bool
	pasteBuf      []byte
	popup         *popupState
	expanded      *expandedState
	pasteDecision *pasteDecision
	statusMessage string
}

// NewEditor creates a new editor with the given configuration.
func NewEditor(config EditorConfig) *Editor {
	return &Editor{
		doc:    NewDocument(),
		cursor: Cursor{},
		undo:   NewUndoStack(),
		config: config,
	}
}

// Run enters raw mode, renders the editor, handles input until Enter is pressed.
// Returns (text, images, content blocks, err).
func (e *Editor) Run() (string, []string, []contextengine.ContentBlock, error) {
	fd := int(e.config.In.Fd())

	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", nil, nil, err
	}
	defer term.Restore(fd, state)

	e.write([]byte("\033[?2004h"))
	defer e.write([]byte("\033[?2004l"))

	w, h, err := term.GetSize(fd)
	if err != nil {
		w = 80
		h = 24
	}
	e.termWidth = w
	e.termHeight = h

	e.undo.Push(e.doc, e.cursor)
	e.redraw()

	buf := make([]byte, 256)
	readBuf := make([]byte, 0, 256)

	for {
		n, err := e.config.In.Read(buf)
		if err != nil {
			return "", nil, nil, err
		}
		readBuf = append(readBuf, buf[:n]...)

		for len(readBuf) > 0 {
			evt, consumed := ParseKey(readBuf)
			if consumed == 0 {
				break
			}
			readBuf = readBuf[consumed:]

			text, images, blocks, done, err := e.handleEvent(evt)
			if err != nil {
				return "", nil, nil, err
			}
			if done {
				return text, images, blocks, nil
			}
		}
	}
}

func (e *Editor) handleEvent(evt KeyEvent) (string, []string, []contextengine.ContentBlock, bool, error) {
	if text, done, handled, err := e.handleOverlayEvent(evt); handled || err != nil {
		return text, nil, nil, done, err
	}
	if e.handlePasteDecision(evt) {
		return "", nil, nil, false, nil
	}

	switch evt.Action {
	case ActionSubmit:
		if e.popup != nil {
			e.confirmPopupSelection()
			e.redraw()
			break
		}
		if token := e.selectedAttachment(); token != nil {
			e.expanded = &expandedState{line: e.cursor.Line, col: e.cursor.Col, mode: expandedDetails}
			e.redraw()
			break
		}
		if e.handleAttachCommand() {
			e.redraw()
			break
		}
		if err := e.validateBeforeSubmit(); err != nil {
			e.statusMessage = err.Error()
			e.redraw()
			break
		}

		text := e.doc.SubmissionText()
		visible := e.doc.Text()
		images := e.doc.Images()
		e.storeDraft()
		ClearRender(e.config.Out, e.render)
		e.render = RenderState{}
		e.write([]byte(e.config.Prompt))
		e.write([]byte(visible))
		e.write([]byte(ansiCRLF))
		return text, images, e.doc.ContentBlocks(), true, nil

	case ActionInterrupt:
		ClearRender(e.config.Out, e.render)
		e.write([]byte(ansiCRLF))
		return "", nil, nil, true, ErrEditorInterrupted

	case ActionEOF:
		if e.doc.IsEmpty() {
			ClearRender(e.config.Out, e.render)
			e.write([]byte(ansiCRLF))
			return "", nil, nil, true, ErrEditorEOF
		}

	case ActionEscape:
		switch {
		case e.popup != nil:
			e.popup = nil
		case e.expanded != nil:
			e.expanded = nil
		case !e.doc.IsEmpty():
			e.pushUndo()
			e.doc = NewDocument()
			e.cursor = Cursor{}
			e.statusMessage = ""
			e.pasteDecision = nil
		default:
			break
		}
		e.redraw()

	case ActionInsertRune:
		if e.inPaste {
			var buf [utf8.UTFMax]byte
			n := utf8.EncodeRune(buf[:], evt.Rune)
			e.pasteBuf = append(e.pasteBuf, buf[:n]...)
			break
		}
		if e.popup != nil {
			if unicode.IsPrint(evt.Rune) {
				e.popup.query += string(evt.Rune)
				e.refreshPopup()
				e.redraw()
			}
			break
		}
		if evt.Rune == '@' && e.config.Completion != nil {
			e.openAttachmentPopup("")
			e.redraw()
			break
		}
		if unicode.IsPrint(evt.Rune) {
			e.pushUndo()
			e.statusMessage = ""
			e.doc.InsertRune(e.cursor.Line, e.cursor.Col, evt.Rune)
			e.cursor.Col++
			e.clearExpandedIfCursorMoved()
			e.redraw()
		}

	case ActionInsertNewline:
		if e.inPaste {
			e.pasteBuf = append(e.pasteBuf, '\n')
			break
		}
		if e.popup != nil {
			break
		}
		e.pushUndo()
		e.statusMessage = ""
		e.doc.InsertNewline(e.cursor.Line, e.cursor.Col)
		e.cursor.Line++
		e.cursor.Col = 0
		e.clearExpandedIfCursorMoved()
		e.redraw()

	case ActionBackspace:
		if e.popup != nil {
			e.backspacePopup()
			e.redraw()
			break
		}
		if token := e.selectedAttachment(); token != nil {
			e.pushUndo()
			e.statusMessage = ""
			e.doc.DeleteForward(e.cursor.Line, e.cursor.Col)
			e.expanded = nil
			e.redraw()
			_ = token
			break
		}
		if e.cursor.Line == 0 && e.cursor.Col == 0 {
			break
		}
		e.pushUndo()
		e.statusMessage = ""
		e.cursor.Line, e.cursor.Col = e.doc.DeleteBack(e.cursor.Line, e.cursor.Col)
		e.clearExpandedIfCursorMoved()
		e.redraw()

	case ActionDelete:
		if token := e.selectedAttachment(); token != nil {
			e.pushUndo()
			e.statusMessage = ""
			e.doc.DeleteForward(e.cursor.Line, e.cursor.Col)
			e.expanded = nil
			e.redraw()
			_ = token
			break
		}
		e.pushUndo()
		e.statusMessage = ""
		e.doc.DeleteForward(e.cursor.Line, e.cursor.Col)
		e.clearExpandedIfCursorMoved()
		e.redraw()

	case ActionMoveLeft:
		if e.popup != nil {
			break
		}
		e.cursor.MoveLeft(e.doc)
		e.clearExpandedIfCursorMoved()
		e.redraw()

	case ActionMoveRight:
		if e.popup != nil {
			break
		}
		e.cursor.MoveRight(e.doc)
		e.clearExpandedIfCursorMoved()
		e.redraw()

	case ActionMoveUp:
		switch {
		case e.popup != nil:
			e.movePopup(-1)
		case e.expanded != nil:
			// Expanded tokens absorb Up so history/cursor movement stays stable.
		case e.selectedAttachment() != nil && e.selectedAttachment().Kind == TokenImage:
			e.expanded = &expandedState{line: e.cursor.Line, col: e.cursor.Col, mode: expandedPreview}
		case e.cursor.Line > 0:
			e.cursor.MoveUp(e.doc)
			e.clearExpandedIfCursorMoved()
		default:
			e.restorePreviousDraft()
		}
		e.redraw()

	case ActionMoveDown:
		switch {
		case e.popup != nil:
			e.movePopup(1)
		case e.expanded != nil:
			e.expanded = nil
		case e.cursor.Line < e.doc.LineCount()-1:
			e.cursor.MoveDown(e.doc)
			e.clearExpandedIfCursorMoved()
		default:
			e.restoreNextDraft()
		}
		e.redraw()

	case ActionMoveWordLeft:
		if e.popup != nil {
			break
		}
		e.cursor.MoveWordLeft(e.doc)
		e.clearExpandedIfCursorMoved()
		e.redraw()

	case ActionMoveWordRight:
		if e.popup != nil {
			break
		}
		e.cursor.MoveWordRight(e.doc)
		e.clearExpandedIfCursorMoved()
		e.redraw()

	case ActionMoveHome:
		if e.popup != nil {
			break
		}
		e.cursor.MoveHome()
		e.clearExpandedIfCursorMoved()
		e.redraw()

	case ActionMoveEnd:
		if e.popup != nil {
			break
		}
		e.cursor.MoveEnd(e.doc)
		e.clearExpandedIfCursorMoved()
		e.redraw()

	case ActionKillToEnd:
		e.pushUndo()
		e.statusMessage = ""
		e.doc.KillToEnd(e.cursor.Line, e.cursor.Col)
		e.clearExpandedIfCursorMoved()
		e.redraw()

	case ActionKillToStart:
		e.pushUndo()
		e.statusMessage = ""
		e.doc.KillToStart(e.cursor.Line, e.cursor.Col)
		e.cursor.Col = 0
		e.clearExpandedIfCursorMoved()
		e.redraw()

	case ActionKillWord:
		e.pushUndo()
		e.statusMessage = ""
		_, newCol := e.doc.KillWord(e.cursor.Line, e.cursor.Col)
		e.cursor.Col = newCol
		e.clearExpandedIfCursorMoved()
		e.redraw()

	case ActionUndo:
		snap, ok := e.undo.Undo()
		if ok {
			e.restoreSnapshot(snap)
			e.popup = nil
			e.expanded = nil
			e.redraw()
		}

	case ActionRedo:
		snap, ok := e.undo.Redo()
		if ok {
			e.restoreSnapshot(snap)
			e.popup = nil
			e.expanded = nil
			e.redraw()
		}

	case ActionClearScreen:
		e.write([]byte("\033[2J\033[H"))
		e.render = RenderState{}
		e.redraw()

	case ActionPasteClipboard:
		e.pasteFromClipboard()

	case ActionPasteBegin:
		e.inPaste = true
		e.pasteBuf = e.pasteBuf[:0]

	case ActionPasteEnd:
		e.inPaste = false
		if len(e.pasteBuf) > 0 {
			e.processPastedContent(string(e.pasteBuf))
			e.pasteBuf = e.pasteBuf[:0]
		} else {
			e.pasteFromClipboard()
		}

	case ActionTab:
		if e.popup != nil {
			e.confirmPopupSelection()
			e.redraw()
			break
		}
		if e.completeTab() {
			e.redraw()
		}
	}

	return "", nil, nil, false, nil
}

func (e *Editor) handleOverlayEvent(evt KeyEvent) (string, bool, bool, error) {
	if e.config.Overlay == nil || e.config.Overlay.Panel() == nil {
		return "", false, false, nil
	}
	panel := e.config.Overlay.Panel()

	// Non-modal panels: only Esc is reserved for dismissing the panel.
	// All other keys fall through to normal editing.
	if !panel.Modal {
		if evt.Action != ActionEscape || e.popup != nil || e.expanded != nil {
			return "", false, false, nil
		}
		result, err := e.config.Overlay.HandleOverlayKey(evt)
		if err != nil {
			return "", false, true, err
		}
		if result.Submit != "" {
			ClearRender(e.config.Out, e.render)
			e.render = RenderState{}
			e.write([]byte(e.config.Prompt))
			e.write([]byte(result.Submit))
			e.write([]byte(ansiCRLF))
			return result.Submit, true, true, nil
		}
		if result.Handled {
			e.redraw()
		}
		return "", false, true, nil
	}

	// Modal panels: intercept all keys (except system actions) when doc is empty.
	if !e.doc.IsEmpty() {
		return "", false, false, nil
	}
	switch evt.Action {
	case ActionInterrupt, ActionEOF, ActionClearScreen:
		return "", false, false, nil
	}
	result, err := e.config.Overlay.HandleOverlayKey(evt)
	if err != nil {
		return "", false, true, err
	}
	if result.Submit != "" {
		ClearRender(e.config.Out, e.render)
		e.render = RenderState{}
		e.write([]byte(e.config.Prompt))
		e.write([]byte(result.Submit))
		e.write([]byte(ansiCRLF))
		return result.Submit, true, true, nil
	}
	if result.Handled {
		e.redraw()
	}
	return "", false, true, nil
}

func (e *Editor) handlePasteDecision(evt KeyEvent) bool {
	if e.pasteDecision == nil {
		return false
	}
	if evt.Action == ActionInsertRune {
		switch unicode.ToLower(evt.Rune) {
		case 'f':
			e.foldLargePaste()
			e.redraw()
			return true
		case 'k':
			e.keepLargePasteExpanded()
			e.redraw()
			return true
		}
	}
	e.keepLargePasteExpanded()
	return false
}

func (e *Editor) pushUndo() {
	e.undo.Push(e.doc, e.cursor)
}

func (e *Editor) processPastedContent(content string) {
	e.processPasteResult(ProcessPaste(content))
}

func (e *Editor) pasteFromClipboard() {
	result, err := ReadClipboardPaste()
	if err != nil {
		e.statusMessage = "Clipboard read failed: " + err.Error()
		e.redraw()
		return
	}
	if !result.IsImage && result.Text == "" {
		e.statusMessage = "Clipboard has no pasteable image or text."
		e.redraw()
		return
	}
	e.processPasteResult(result)
}

func (e *Editor) processPasteResult(result PasteResult) {
	if result.IsImage {
		e.pushUndo()
		e.statusMessage = ""
		e.doc.InsertImage(e.cursor.Line, e.cursor.Col, result.ImageData, result.MediaType, "")
		e.cursor.Col++
		e.redraw()
		return
	}
	if shouldOfferBlockFold(result.Text) {
		e.pasteDecision = newPasteDecision(result.Text)
		e.statusMessage = ""
		e.redraw()
		return
	}
	e.pushUndo()
	e.insertPlainText(result.Text)
	if result.PathHint {
		e.statusMessage = "Use @ or /attach to insert as token."
	} else {
		e.statusMessage = ""
	}
	e.redraw()
}

func (e *Editor) insertPlainText(text string) {
	for _, r := range text {
		switch r {
		case '\n':
			e.doc.InsertNewline(e.cursor.Line, e.cursor.Col)
			e.cursor.Line++
			e.cursor.Col = 0
		case '\r':
			continue
		default:
			e.doc.InsertRune(e.cursor.Line, e.cursor.Col, r)
			e.cursor.Col++
		}
	}
}

func shouldOfferBlockFold(text string) bool {
	if text == "" {
		return false
	}
	return len(text) > 4000 || strings.Count(text, "\n")+1 > 40
}

func newPasteDecision(content string) *pasteDecision {
	return &pasteDecision{
		content:   content,
		lineCount: strings.Count(content, "\n") + 1,
		charCount: utf8.RuneCountInString(content),
	}
}

func (e *Editor) foldLargePaste() {
	if e.pasteDecision == nil {
		return
	}
	e.pushUndo()
	e.doc.InsertBlock(e.cursor.Line, e.cursor.Col, e.pasteDecision.content)
	e.cursor.Col++
	e.statusMessage = ""
	e.pasteDecision = nil
}

func (e *Editor) keepLargePasteExpanded() {
	if e.pasteDecision == nil {
		return
	}
	e.pushUndo()
	e.insertPlainText(e.pasteDecision.content)
	e.statusMessage = ""
	e.pasteDecision = nil
}

func (e *Editor) selectedAttachment() *Token {
	tok := e.cursor.TokenAtCursor(e.doc)
	if tok == nil || !tok.IsAttachment() {
		return nil
	}
	return tok
}

func (e *Editor) clearExpandedIfCursorMoved() {
	if e.expanded == nil {
		return
	}
	if e.expanded.line != e.cursor.Line || e.expanded.col != e.cursor.Col || e.selectedAttachment() == nil {
		e.expanded = nil
	}
}

func (e *Editor) openAttachmentPopup(query string) {
	e.popup = &popupState{query: query}
	e.refreshPopup()
}

func (e *Editor) refreshPopup() {
	if e.popup == nil || e.config.Completion == nil {
		return
	}
	e.popup.items = e.config.Completion.AttachmentCandidates(e.popup.query)
	if e.popup.selected >= len(e.popup.items) {
		e.popup.selected = 0
		if len(e.popup.items) > 0 {
			e.popup.selected = len(e.popup.items) - 1
		}
	}
}

func (e *Editor) movePopup(delta int) {
	if e.popup == nil || len(e.popup.items) == 0 {
		return
	}
	e.popup.selected += delta
	if e.popup.selected < 0 {
		e.popup.selected = len(e.popup.items) - 1
	}
	if e.popup.selected >= len(e.popup.items) {
		e.popup.selected = 0
	}
}

func (e *Editor) backspacePopup() {
	if e.popup == nil {
		return
	}
	if e.popup.query == "" {
		e.popup = nil
		return
	}
	_, size := utf8.DecodeLastRuneInString(e.popup.query)
	e.popup.query = e.popup.query[:len(e.popup.query)-size]
	e.refreshPopup()
}

func (e *Editor) confirmPopupSelection() {
	if e.popup == nil {
		return
	}
	if len(e.popup.items) == 0 {
		e.popup = nil
		return
	}
	item := e.popup.items[e.popup.selected]
	e.popup = nil
	e.pushUndo()
	if err := e.insertAttachmentAtCursor(item.Kind, item.Path); err != nil {
		e.statusMessage = err.Error()
		return
	}
	e.statusMessage = ""
}

func (e *Editor) completeTab() bool {
	if e.config.Completion == nil {
		return false
	}
	linePrefix, start, end, current := e.currentWord()
	if current == "" {
		return false
	}
	if looksLikePathCompletion(current) {
		if completed, ok := e.config.Completion.CompletePath(current); ok && completed != "" && completed != current {
			e.pushUndo()
			e.doc.ReplaceRange(e.cursor.Line, start, end, tokensFromString(completed))
			e.cursor.Col = start + len([]rune(completed))
			e.statusMessage = ""
			return true
		}
	}
	if strings.HasPrefix(strings.TrimSpace(linePrefix), "/") {
		if completed, ok := e.config.Completion.CompleteSlash(linePrefix, current); ok && completed != "" && completed != current {
			e.pushUndo()
			e.doc.ReplaceRange(e.cursor.Line, start, end, tokensFromString(completed))
			e.cursor.Col = start + len([]rune(completed))
			e.statusMessage = ""
			return true
		}
	}
	return false
}

func (e *Editor) currentWord() (linePrefix string, start int, end int, current string) {
	if e.cursor.Line < 0 || e.cursor.Line >= e.doc.LineCount() {
		return "", 0, 0, ""
	}
	line := e.doc.Lines[e.cursor.Line]
	start = e.cursor.Col
	for start > 0 && line[start-1].Kind == TokenText && !unicode.IsSpace(line[start-1].Rune) {
		start--
	}
	end = e.cursor.Col
	for end < len(line) && line[end].Kind == TokenText && !unicode.IsSpace(line[end].Rune) {
		end++
	}
	linePrefix = tokenString(line[:e.cursor.Col])
	current = tokenString(line[start:e.cursor.Col])
	return linePrefix, start, end, current
}

func looksLikePathCompletion(value string) bool {
	return strings.HasPrefix(value, "./") ||
		strings.HasPrefix(value, "../") ||
		strings.HasPrefix(value, "~/") ||
		strings.HasPrefix(value, "/")
}

func (e *Editor) handleAttachCommand() bool {
	raw := strings.TrimSpace(e.doc.Text())
	if !strings.HasPrefix(raw, "/attach ") {
		return false
	}
	fields := strings.Fields(raw)
	if len(fields) < 3 {
		e.statusMessage = "usage: /attach <image|file|dir|video> <path>"
		return true
	}
	path := strings.TrimSpace(strings.Join(fields[2:], " "))
	kind, ok := ParseAttachKind(fields[1])
	if !ok {
		e.statusMessage = "attach type must be image, file, dir, or video"
		return true
	}
	e.pushUndo()
	e.doc = NewDocument()
	e.cursor = Cursor{}
	if err := e.insertAttachmentAtCursor(kind, path); err != nil {
		e.statusMessage = err.Error()
		return true
	}
	e.statusMessage = ""
	return true
}

func parseAttachKind(value string) (TokenKind, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image":
		return TokenImage, true
	case "file":
		return TokenFile, true
	case "dir":
		return TokenDir, true
	case "video":
		return TokenVideo, true
	default:
		return TokenText, false
	}
}

func (e *Editor) insertAttachmentAtCursor(kind TokenKind, path string) error {
	tok, err := buildAttachmentToken(kind, path)
	if err != nil {
		return err
	}
	tok.ID = e.doc.nextID(kind)
	e.doc.InsertTokens(e.cursor.Line, e.cursor.Col, []Token{tok})
	e.cursor.Col++
	return nil
}

func (e *Editor) validateBeforeSubmit() error {
	for _, tok := range e.doc.Attachments() {
		switch tok.Kind {
		case TokenImage:
			if path := strings.TrimSpace(tok.Path); path != "" {
				if _, err := os.Stat(path); err != nil {
					return fmt.Errorf("IMAGE#%d cannot be read: file not found", tok.ID)
				}
			}
		case TokenFile:
			info, err := os.Stat(tok.Path)
			if err != nil || info.IsDir() {
				return fmt.Errorf("FILE#%d cannot be read: file not found", tok.ID)
			}
		case TokenDir:
			info, err := os.Stat(tok.Path)
			if err != nil || !info.IsDir() {
				return fmt.Errorf("DIR#%d cannot be read: directory not found", tok.ID)
			}
		case TokenVideo:
			info, err := os.Stat(tok.Path)
			if err != nil || info.IsDir() {
				return fmt.Errorf("VIDEO#%d cannot be read: file not found", tok.ID)
			}
		}
	}
	return nil
}

func resolvePath(path string) (string, error) {
	path = strings.Trim(strings.TrimSpace(path), "\"'")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	return path, nil
}

func looksLikeVideo(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".mov", ".mkv", ".avi", ".webm":
		return true
	default:
		return false
	}
}

func tokenString(tokens []Token) string {
	var sb strings.Builder
	for _, tok := range tokens {
		if tok.Kind == TokenText {
			sb.WriteRune(tok.Rune)
		} else {
			sb.WriteString(tok.DisplayText())
		}
	}
	return sb.String()
}

func (e *Editor) storeDraft() {
	if history, ok := e.config.History.(DraftHistory); ok {
		_ = history.AddDraft(e.doc.Snapshot(e.cursor))
	}
}

func (e *Editor) restorePreviousDraft() {
	if history, ok := e.config.History.(DraftHistory); ok {
		if snap, ok := history.PreviousDraft(e.doc.Snapshot(e.cursor)); ok {
			e.restoreSnapshot(snap)
			return
		}
	}
	if e.doc.LineCount() == 1 && !e.doc.HasAttachments() && e.config.History != nil {
		prev := e.config.History.Previous(tokenString(e.doc.Lines[0]))
		e.setContent(prev)
	}
}

func (e *Editor) restoreNextDraft() {
	if history, ok := e.config.History.(DraftHistory); ok {
		if snap, ok := history.NextDraft(); ok {
			e.restoreSnapshot(snap)
			return
		}
	}
	if e.doc.LineCount() == 1 && !e.doc.HasAttachments() && e.config.History != nil {
		next := e.config.History.Next()
		e.setContent(next)
	}
}

// decodeDataURI extracts raw bytes and media type from a data URI.
func decodeDataURI(uri string) ([]byte, string) {
	if !strings.HasPrefix(uri, "data:") {
		return nil, ""
	}
	parts := strings.SplitN(uri, ";base64,", 2)
	if len(parts) != 2 {
		return nil, ""
	}
	mediaType := strings.TrimPrefix(parts[0], "data:")
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ""
	}
	return data, mediaType
}

func (e *Editor) restoreSnapshot(snap DocumentSnapshot) {
	e.doc.Restore(snap)
	e.cursor = snap.Cursor
	e.cursor.Clamp(e.doc)
}

func (e *Editor) setContent(text string) {
	e.doc = NewDocument()
	for _, r := range text {
		e.doc.InsertRune(0, e.doc.TokenCount(0), r)
	}
	e.cursor.Line = 0
	e.cursor.Col = e.doc.TokenCount(0)
}

func (e *Editor) redraw() {
	e.render = Render(e.config.Out, e.doc, e.cursor, e.config.Prompt, e.render, e.termWidth, e.termHeight, e.view())
}

func (e *Editor) view() EditorView {
	view := EditorView{
		Status: "",
	}
	if e.config.Chrome != nil {
		view.Chrome = e.config.Chrome(e.termWidth)
	}
	if e.statusMessage != "" {
		view.Status = e.statusMessage
	}
	if e.pasteDecision != nil {
		view.PastePrompt = largePastePrompt(e.pasteDecision)
	}
	if e.popup != nil {
		view.Popup = e.popup
	}
	if e.config.Overlay != nil {
		view.Overlay = e.config.Overlay.Panel()
	}
	if e.expanded != nil && e.expanded.line == e.cursor.Line && e.expanded.col == e.cursor.Col {
		if tok := e.selectedAttachment(); tok != nil {
			view.Expanded = tok
			view.ExpandedMode = e.expanded.mode
		}
	}
	return view
}

func largePastePrompt(decision *pasteDecision) string {
	if decision == nil {
		return ""
	}
	label := "lines"
	if decision.lineCount == 1 {
		label = "line"
	}
	return fmt.Sprintf("Large paste: %d %s · [f] fold into [BLOCK#1] · [k] keep expanded", decision.lineCount, label)
}

func (e *Editor) write(b []byte) {
	e.config.Out.Write(b)
}

var (
	ErrEditorInterrupted = errors.New("editor interrupted")
	ErrEditorEOF         = errors.New("editor EOF")
)
