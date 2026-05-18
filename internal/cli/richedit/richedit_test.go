package richedit

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/contextengine"
)

// --- Document tests ---

func TestNewDocument(t *testing.T) {
	doc := NewDocument()
	if doc.LineCount() != 1 {
		t.Fatalf("expected 1 line, got %d", doc.LineCount())
	}
	if !doc.IsEmpty() {
		t.Fatal("new document should be empty")
	}
	if doc.Text() != "" {
		t.Fatalf("expected empty text, got %q", doc.Text())
	}
}

func TestDocumentInsertRune(t *testing.T) {
	doc := NewDocument()
	doc.InsertRune(0, 0, 'H')
	doc.InsertRune(0, 1, 'i')
	if doc.Text() != "Hi" {
		t.Fatalf("expected %q, got %q", "Hi", doc.Text())
	}
	if doc.TokenCount(0) != 2 {
		t.Fatalf("expected 2 tokens, got %d", doc.TokenCount(0))
	}
}

func TestDocumentInsertRuneMiddle(t *testing.T) {
	doc := NewDocument()
	doc.InsertRune(0, 0, 'A')
	doc.InsertRune(0, 1, 'C')
	doc.InsertRune(0, 1, 'B') // insert B between A and C
	if doc.Text() != "ABC" {
		t.Fatalf("expected %q, got %q", "ABC", doc.Text())
	}
}

func TestDocumentInsertNewline(t *testing.T) {
	doc := NewDocument()
	for _, r := range "Hello" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	doc.InsertNewline(0, 3) // Split after "Hel"
	if doc.LineCount() != 2 {
		t.Fatalf("expected 2 lines, got %d", doc.LineCount())
	}
	if doc.Text() != "Hel\nlo" {
		t.Fatalf("expected %q, got %q", "Hel\nlo", doc.Text())
	}
}

func TestDocumentInsertImage(t *testing.T) {
	doc := NewDocument()
	doc.InsertRune(0, 0, 'A')
	id := doc.InsertImage(0, 1, "data:image/png;base64,abc", "image/png", "")
	doc.InsertRune(0, 2, 'B')
	if id != 1 {
		t.Fatalf("expected image ID 1, got %d", id)
	}
	if doc.Text() != "A[IMAGE#1]B" {
		t.Fatalf("expected text %q, got %q", "A[IMAGE#1]B", doc.Text())
	}
	imgs := doc.Images()
	if len(imgs) != 1 || imgs[0] != "data:image/png;base64,abc" {
		t.Fatalf("unexpected images: %v", imgs)
	}
	if !doc.HasImages() {
		t.Fatal("expected HasImages true")
	}
}

func TestDocumentAttachmentTokensUseCanonicalASCIIText(t *testing.T) {
	doc := NewDocument()
	doc.InsertImage(0, 0, "data:image/png;base64,abc", "image/png", "")
	doc.InsertFile(0, 1, filepath.Join(t.TempDir(), "notes.txt"))
	doc.InsertDir(0, 2, filepath.Join(t.TempDir(), "workspace"))
	doc.InsertVideo(0, 3, filepath.Join(t.TempDir(), "demo.mp4"))
	doc.InsertBlock(0, 4, "first line\nsecond line")
	doc.InsertLink(0, 5, "https://example.com/release-notes")

	if got, want := doc.Text(), "[IMAGE#1][FILE#1: notes.txt][DIR#1: workspace][VIDEO#1: demo.mp4][BLOCK#1: 2 lines][LINK#1]"; got != want {
		t.Fatalf("doc.Text() = %q, want %q", got, want)
	}
}

func TestDocumentContentBlocksPreserveInlineOrder(t *testing.T) {
	doc := NewDocument()
	for _, r := range "Review " {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	doc.InsertFile(0, doc.TokenCount(0), "/tmp/spec.md")
	for _, r := range " and " {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	doc.InsertImage(0, doc.TokenCount(0), "data:image/png;base64,ZmFrZS1wbmc=", "image/png", "/tmp/mock.png")
	doc.InsertNewline(0, doc.TokenCount(0))
	doc.InsertLink(1, doc.TokenCount(1), "https://example.com/spec")
	doc.InsertBlock(1, doc.TokenCount(1), "line 1\nline 2")

	got := doc.ContentBlocks()
	want := []contextengine.ContentBlock{
		{Type: contextengine.ContentBlockText, Text: "Review "},
		{Type: contextengine.ContentBlockFile, Label: "spec.md", Path: "/tmp/spec.md"},
		{Type: contextengine.ContentBlockText, Text: " and "},
		{Type: contextengine.ContentBlockImage, Label: "mock.png", Path: "/tmp/mock.png", MediaType: "image/png", Data: "ZmFrZS1wbmc="},
		{Type: contextengine.ContentBlockText, Text: "\n"},
		{Type: contextengine.ContentBlockLink, SourceURL: "https://example.com/spec"},
		{Type: contextengine.ContentBlockSnippet, Label: "2 lines", Text: "line 1\nline 2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ContentBlocks() = %#v, want %#v", got, want)
	}
}

func TestDocumentDeleteBack(t *testing.T) {
	doc := NewDocument()
	for _, r := range "ABC" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	line, col := doc.DeleteBack(0, 2) // delete 'B'
	if line != 0 || col != 1 {
		t.Fatalf("expected (0,1), got (%d,%d)", line, col)
	}
	if doc.Text() != "AC" {
		t.Fatalf("expected %q, got %q", "AC", doc.Text())
	}
}

func TestDocumentDeleteBackMergeLines(t *testing.T) {
	doc := NewDocument()
	for _, r := range "AB" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	doc.InsertNewline(0, 2)
	for _, r := range "CD" {
		doc.InsertRune(1, doc.TokenCount(1), r)
	}
	// Delete at start of line 1 -> merge with line 0
	line, col := doc.DeleteBack(1, 0)
	if line != 0 || col != 2 {
		t.Fatalf("expected (0,2), got (%d,%d)", line, col)
	}
	if doc.LineCount() != 1 {
		t.Fatalf("expected 1 line, got %d", doc.LineCount())
	}
	if doc.Text() != "ABCD" {
		t.Fatalf("expected %q, got %q", "ABCD", doc.Text())
	}
}

func TestDocumentDeleteForward(t *testing.T) {
	doc := NewDocument()
	for _, r := range "ABC" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	doc.DeleteForward(0, 1) // delete 'B'
	if doc.Text() != "AC" {
		t.Fatalf("expected %q, got %q", "AC", doc.Text())
	}
}

func TestDocumentDeleteForwardMergeLines(t *testing.T) {
	doc := NewDocument()
	for _, r := range "AB" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	doc.InsertNewline(0, 2)
	for _, r := range "CD" {
		doc.InsertRune(1, doc.TokenCount(1), r)
	}
	// Delete forward at end of line 0 -> merge
	doc.DeleteForward(0, 2)
	if doc.LineCount() != 1 {
		t.Fatalf("expected 1 line, got %d", doc.LineCount())
	}
	if doc.Text() != "ABCD" {
		t.Fatalf("expected %q, got %q", "ABCD", doc.Text())
	}
}

func TestDocumentKillToEnd(t *testing.T) {
	doc := NewDocument()
	for _, r := range "Hello World" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	killed := doc.KillToEnd(0, 5)
	if len(killed) != 6 {
		t.Fatalf("expected 6 killed tokens, got %d", len(killed))
	}
	if doc.Text() != "Hello" {
		t.Fatalf("expected %q, got %q", "Hello", doc.Text())
	}
}

func TestDocumentKillToStart(t *testing.T) {
	doc := NewDocument()
	for _, r := range "Hello World" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	killed := doc.KillToStart(0, 6)
	if len(killed) != 6 {
		t.Fatalf("expected 6 killed tokens, got %d", len(killed))
	}
	if doc.Text() != "World" {
		t.Fatalf("expected %q, got %q", "World", doc.Text())
	}
}

func TestDocumentKillWord(t *testing.T) {
	doc := NewDocument()
	for _, r := range "Hello World" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	killed, newCol := doc.KillWord(0, 11) // at end, kill "World"
	if len(killed) != 5 {
		t.Fatalf("expected 5 killed tokens, got %d", len(killed))
	}
	if newCol != 6 {
		t.Fatalf("expected newCol 6, got %d", newCol)
	}
	if doc.Text() != "Hello " {
		t.Fatalf("expected %q, got %q", "Hello ", doc.Text())
	}
}

func TestDocumentClone(t *testing.T) {
	doc := NewDocument()
	for _, r := range "AB" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	clone := doc.Clone()
	clone.InsertRune(0, 2, 'C')
	if doc.Text() != "AB" {
		t.Fatal("original doc was mutated by clone modification")
	}
	if clone.Text() != "ABC" {
		t.Fatalf("clone expected %q, got %q", "ABC", clone.Text())
	}
}

// --- Cursor tests ---

func TestCursorMoveLeftRight(t *testing.T) {
	doc := NewDocument()
	for _, r := range "ABC" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	c := Cursor{Line: 0, Col: 3}
	c.MoveLeft(doc)
	if c.Col != 2 {
		t.Fatalf("expected col 2, got %d", c.Col)
	}
	c.MoveRight(doc)
	if c.Col != 3 {
		t.Fatalf("expected col 3, got %d", c.Col)
	}
}

func TestCursorMoveLeftWrap(t *testing.T) {
	doc := NewDocument()
	for _, r := range "AB" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	doc.InsertNewline(0, 2)
	for _, r := range "CD" {
		doc.InsertRune(1, doc.TokenCount(1), r)
	}
	c := Cursor{Line: 1, Col: 0}
	c.MoveLeft(doc)
	if c.Line != 0 || c.Col != 2 {
		t.Fatalf("expected (0,2), got (%d,%d)", c.Line, c.Col)
	}
}

func TestCursorMoveRightWrap(t *testing.T) {
	doc := NewDocument()
	for _, r := range "AB" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	doc.InsertNewline(0, 2)
	c := Cursor{Line: 0, Col: 2}
	c.MoveRight(doc)
	if c.Line != 1 || c.Col != 0 {
		t.Fatalf("expected (1,0), got (%d,%d)", c.Line, c.Col)
	}
}

func TestCursorMoveUpDown(t *testing.T) {
	doc := NewDocument()
	for _, r := range "ABCD" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	doc.InsertNewline(0, 2) // Line 0: AB, Line 1: CD
	c := Cursor{Line: 0, Col: 1}
	c.MoveDown(doc)
	if c.Line != 1 || c.Col != 1 {
		t.Fatalf("expected (1,1), got (%d,%d)", c.Line, c.Col)
	}
	c.MoveUp(doc)
	if c.Line != 0 || c.Col != 1 {
		t.Fatalf("expected (0,1), got (%d,%d)", c.Line, c.Col)
	}
}

func TestCursorMoveUpClamp(t *testing.T) {
	doc := NewDocument()
	for _, r := range "ABCD" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	doc.InsertNewline(0, 4) // Line 0: ABCD, Line 1: (empty)
	doc.InsertNewline(1, 0)
	for _, r := range "E" {
		doc.InsertRune(2, doc.TokenCount(2), r)
	}
	c := Cursor{Line: 0, Col: 4}
	c.MoveDown(doc) // to line 1 (empty) -> col clamped to 0
	if c.Col != 0 {
		t.Fatalf("expected col 0, got %d", c.Col)
	}
}

func TestCursorWordMovement(t *testing.T) {
	doc := NewDocument()
	for _, r := range "Hello World Foo" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	c := Cursor{Line: 0, Col: 15} // end
	c.MoveWordLeft(doc)
	if c.Col != 12 {
		t.Fatalf("expected col 12, got %d", c.Col)
	}
	c.MoveWordLeft(doc)
	if c.Col != 6 {
		t.Fatalf("expected col 6, got %d", c.Col)
	}
	c.MoveWordRight(doc)
	if c.Col != 12 {
		t.Fatalf("expected col 12, got %d", c.Col)
	}
}

func TestCursorHomeEnd(t *testing.T) {
	doc := NewDocument()
	for _, r := range "Hello" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	c := Cursor{Line: 0, Col: 3}
	c.MoveHome()
	if c.Col != 0 {
		t.Fatalf("expected col 0, got %d", c.Col)
	}
	c.MoveEnd(doc)
	if c.Col != 5 {
		t.Fatalf("expected col 5, got %d", c.Col)
	}
}

func TestCursorTokenAtCursor(t *testing.T) {
	doc := NewDocument()
	doc.InsertRune(0, 0, 'A')
	doc.InsertImage(0, 1, "data:img", "image/png", "")
	c := Cursor{Line: 0, Col: 1}
	tok := c.TokenAtCursor(doc)
	if tok == nil || tok.Kind != TokenImage {
		t.Fatal("expected image token at cursor")
	}
	c.Col = 2 // past end
	tok = c.TokenAtCursor(doc)
	if tok != nil {
		t.Fatal("expected nil at end of line")
	}
}

func TestCursorClamp(t *testing.T) {
	doc := NewDocument()
	for _, r := range "AB" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}
	c := Cursor{Line: 5, Col: 10}
	c.Clamp(doc)
	if c.Line != 0 || c.Col != 2 {
		t.Fatalf("expected (0,2), got (%d,%d)", c.Line, c.Col)
	}
}

// --- Undo tests ---

func TestUndoBasic(t *testing.T) {
	stack := NewUndoStack()
	doc := NewDocument()
	cursor := Cursor{Line: 0, Col: 0}

	// Push initial state
	stack.Push(doc, cursor)

	// Modify and push
	doc.InsertRune(0, 0, 'A')
	cursor.Col = 1
	stack.Push(doc, cursor)

	// Undo
	snap, ok := stack.Undo()
	if !ok {
		t.Fatal("expected undo to succeed")
	}
	if len(snap.Lines) != 1 || len(snap.Lines[0]) != 0 {
		t.Fatal("expected empty document after undo")
	}
	if snap.Cursor.Col != 0 {
		t.Fatalf("expected cursor col 0, got %d", snap.Cursor.Col)
	}
}

func TestUndoRedo(t *testing.T) {
	stack := NewUndoStack()
	doc := NewDocument()
	stack.Push(doc, Cursor{})

	doc.InsertRune(0, 0, 'A')
	stack.Push(doc, Cursor{Col: 1})

	doc.InsertRune(0, 1, 'B')
	stack.Push(doc, Cursor{Col: 2})

	// Undo twice
	stack.Undo()
	snap, ok := stack.Undo()
	if !ok {
		t.Fatal("expected undo to succeed")
	}
	if len(snap.Lines[0]) != 0 {
		t.Fatal("expected empty line after two undos")
	}

	// Redo
	snap, ok = stack.Redo()
	if !ok {
		t.Fatal("expected redo to succeed")
	}
	if len(snap.Lines[0]) != 1 || snap.Lines[0][0].Rune != 'A' {
		t.Fatal("expected 'A' after redo")
	}
}

func TestUndoRedoTruncation(t *testing.T) {
	stack := NewUndoStack()
	doc := NewDocument()
	stack.Push(doc, Cursor{})

	doc.InsertRune(0, 0, 'A')
	stack.Push(doc, Cursor{Col: 1})

	doc.InsertRune(0, 1, 'B')
	stack.Push(doc, Cursor{Col: 2})

	// Undo once
	stack.Undo()

	// Push new state - should discard the 'B' redo
	doc2 := NewDocument()
	doc2.InsertRune(0, 0, 'X')
	stack.Push(doc2, Cursor{Col: 1})

	// Redo should fail now
	_, ok := stack.Redo()
	if ok {
		t.Fatal("expected redo to fail after new push")
	}
}

func TestUndoMaxStates(t *testing.T) {
	stack := NewUndoStack()
	for i := range maxUndoStates + 20 {
		doc := NewDocument()
		doc.InsertRune(0, 0, rune('A'+i%26))
		stack.Push(doc, Cursor{Col: 1})
	}
	if stack.Len() > maxUndoStates {
		t.Fatalf("expected max %d states, got %d", maxUndoStates, stack.Len())
	}
}

// --- Token tests ---

func TestTokenDisplayWidth(t *testing.T) {
	tok := textToken('A')
	if tok.DisplayWidth() != 1 {
		t.Fatalf("expected width 1, got %d", tok.DisplayWidth())
	}
	// CJK character
	tok = textToken('中')
	if tok.DisplayWidth() != 2 {
		t.Fatalf("expected width 2 for CJK, got %d", tok.DisplayWidth())
	}
	// Image token
	tok = imageToken(1, "data:img", "image/png", "")
	w := tok.DisplayWidth()
	if w < 5 {
		t.Fatalf("expected image placeholder width >= 5, got %d", w)
	}
}

func TestEditor_PasteImageDataURI(t *testing.T) {
	editor := newTestEditor()

	pasteString(t, editor, testPNGDataURI)

	if !editor.doc.HasImages() {
		t.Fatal("expected pasted data URI to insert an image token")
	}
	images := editor.doc.Images()
	if len(images) != 1 || images[0] != testPNGDataURI {
		t.Fatalf("unexpected images: %v", images)
	}
	if got := editor.doc.Text(); got != "[IMAGE#1]" {
		t.Fatalf("expected pasted text %q, got %q", "[IMAGE#1]", got)
	}
}

func TestEditor_PasteMultiLineText(t *testing.T) {
	editor := newTestEditor()
	content := "first line\nsecond line\nthird line"

	pasteString(t, editor, content)

	if editor.doc.LineCount() != 3 {
		t.Fatalf("expected 3 lines, got %d", editor.doc.LineCount())
	}
	if got := editor.doc.Text(); got != content {
		t.Fatalf("expected pasted text %q, got %q", content, got)
	}
}

func TestEditor_PasteFilePath(t *testing.T) {
	editor := newTestEditor()
	path := writeTempPNG(t)

	pasteString(t, editor, path)

	if editor.doc.HasImages() {
		t.Fatal("expected pasted file path to remain plain text")
	}
	if got := editor.doc.Text(); got != path {
		t.Fatalf("expected pasted file path text %q, got %q", path, got)
	}
	if editor.statusMessage != "Use @ or /attach to insert as token." {
		t.Fatalf("expected path hint, got %q", editor.statusMessage)
	}
}

func TestEditor_CtrlVPastesClipboardImage(t *testing.T) {
	editor := newTestEditor()
	clipboardImageReader = func() (string, string, error) {
		return testPNGDataURI, "image/png", nil
	}
	clipboardTextReader = func() (string, error) {
		return "", nil
	}
	t.Cleanup(func() {
		clipboardImageReader = readSystemClipboardImage
		clipboardTextReader = readSystemClipboardText
	})

	mustHandleEvent(t, editor, KeyEvent{Action: ActionPasteClipboard})

	if !editor.doc.HasImages() {
		t.Fatal("expected clipboard image to insert an image token")
	}
	if got := editor.doc.Text(); got != "[IMAGE#1]" {
		t.Fatalf("doc.Text() = %q, want %q", got, "[IMAGE#1]")
	}
}

func TestEditor_EmptyBracketedPasteFallsBackToClipboardText(t *testing.T) {
	editor := newTestEditor()
	clipboardImageReader = func() (string, string, error) {
		return "", "", nil
	}
	clipboardTextReader = func() (string, error) {
		return "clipboard text", nil
	}
	t.Cleanup(func() {
		clipboardImageReader = readSystemClipboardImage
		clipboardTextReader = readSystemClipboardText
	})

	mustHandleEvent(t, editor, KeyEvent{Action: ActionPasteBegin})
	mustHandleEvent(t, editor, KeyEvent{Action: ActionPasteEnd})

	if got := editor.doc.Text(); got != "clipboard text" {
		t.Fatalf("doc.Text() = %q, want %q", got, "clipboard text")
	}
}

func TestEditor_SubmitUsesCanonicalInlineImageAnchors(t *testing.T) {
	editor, out := newBufferedTestEditor()
	editor.doc.InsertRune(0, 0, 'O')
	editor.doc.InsertRune(0, 1, 'K')
	editor.doc.InsertImage(0, 2, testPNGDataURI, "image/png", "")

	text, images, _, done, err := editor.handleEvent(KeyEvent{Action: ActionSubmit})
	if err != nil {
		t.Fatalf("submit returned error: %v", err)
	}
	if !done {
		t.Fatal("submit did not finish editor")
	}
	if text != "OK[IMAGE#1]" {
		t.Fatalf("expected submitted text %q, got %q", "OK[IMAGE#1]", text)
	}
	if len(images) != 1 || images[0] != testPNGDataURI {
		t.Fatalf("unexpected submitted images: %v", images)
	}
	output := out.String()
	if !strings.Contains(output, "> OK[IMAGE#1]\r\n") {
		t.Fatalf("expected inline token render in output, got %q", output)
	}
	if strings.Contains(output, "\x1b_G") {
		t.Fatalf("expected no post-submit terminal image preview, got %q", output)
	}
}

func TestRenderUsesCRLFBetweenWorkbenchChromeAndPrompt(t *testing.T) {
	doc := NewDocument()
	doc.InsertRune(0, 0, 'h')
	doc.InsertRune(0, 1, 'i')

	var out bytes.Buffer
	Render(&out, doc, Cursor{Line: 0, Col: 2}, "> ", RenderState{}, 80, 0, EditorView{
		Chrome: Chrome{
			Top:    "top rail",
			Bottom: "bottom rail",
		},
	})

	got := out.String()
	if !strings.Contains(got, "top rail\r\n> hi\r\nbottom rail") {
		t.Fatalf("Render() should use CRLF between chrome and prompt rows in raw mode, got %q", got)
	}
	if strings.Contains(got, "top rail\n> hi\nbottom rail") {
		t.Fatalf("Render() should not use bare LF between raw-mode rows, got %q", got)
	}
}

func TestDocumentSubmissionTextOmitsPureImageBody(t *testing.T) {
	doc := NewDocument()
	doc.InsertImage(0, 0, testPNGDataURI, "image/png", "")

	if got := doc.SubmissionText(); got != "" {
		t.Fatalf("SubmissionText() = %q, want empty string for pure image prompt", got)
	}
	if got := doc.Text(); got != "[IMAGE#1]" {
		t.Fatalf("Text() = %q, want %q", got, "[IMAGE#1]")
	}
}

func TestEditor_BackspaceDeletesSelectedTokenAtomically(t *testing.T) {
	editor := newTestEditor()
	editor.doc.InsertRune(0, 0, 'A')
	editor.doc.InsertImage(0, 1, testPNGDataURI, "image/png", "")
	editor.doc.InsertRune(0, 2, 'B')
	editor.cursor = Cursor{Line: 0, Col: 1}

	mustHandleEvent(t, editor, KeyEvent{Action: ActionBackspace})

	if got := editor.doc.Text(); got != "AB" {
		t.Fatalf("doc.Text() = %q, want %q", got, "AB")
	}
}

func TestEditor_ImagePreviewExpandsAndCollapsesInEditMode(t *testing.T) {
	editor := newTestEditor()
	editor.doc.InsertImage(0, 0, testPNGDataURI, "image/png", "")
	editor.cursor = Cursor{Line: 0, Col: 0}

	mustHandleEvent(t, editor, KeyEvent{Action: ActionMoveUp})
	if editor.expanded == nil || editor.expanded.mode != expandedPreview {
		t.Fatalf("expected preview mode, got %#v", editor.expanded)
	}

	mustHandleEvent(t, editor, KeyEvent{Action: ActionMoveDown})
	if editor.expanded != nil {
		t.Fatalf("expected preview to collapse, got %#v", editor.expanded)
	}
}

func TestEditor_LargePasteCanFoldIntoBlockToken(t *testing.T) {
	editor := newTestEditor()
	content := strings.Repeat("line\n", 41)

	pasteString(t, editor, content)
	if editor.pasteDecision == nil {
		t.Fatal("expected large paste fold prompt")
	}

	mustHandleEvent(t, editor, KeyEvent{Action: ActionInsertRune, Rune: 'f'})
	if editor.pasteDecision != nil {
		t.Fatal("expected fold prompt to clear after choosing fold")
	}
	if got := editor.doc.Text(); got != "[BLOCK#1: 42 lines]" {
		t.Fatalf("doc.Text() = %q, want %q", got, "[BLOCK#1: 42 lines]")
	}
}

func TestEditor_LargePastePromptShowsDesignSummary(t *testing.T) {
	editor := newTestEditor()
	content := strings.Repeat("line\n", 41)

	pasteString(t, editor, content)

	if editor.pasteDecision == nil {
		t.Fatal("expected large paste fold prompt")
	}
	if got := editor.view().PastePrompt; got != "Large paste: 42 lines · [f] fold into [BLOCK#1] · [k] keep expanded" {
		t.Fatalf("view().PastePrompt = %q", got)
	}
}

func TestEditor_LargePasteContinuedTypingKeepsExpanded(t *testing.T) {
	editor := newTestEditor()
	content := strings.Repeat("line\n", 41)

	pasteString(t, editor, content)
	if editor.pasteDecision == nil {
		t.Fatal("expected large paste fold prompt")
	}

	mustHandleEvent(t, editor, KeyEvent{Action: ActionInsertRune, Rune: 'x'})

	if editor.pasteDecision != nil {
		t.Fatal("expected continued typing to accept keep expanded")
	}
	if got := editor.doc.Text(); got != content+"x" {
		t.Fatalf("doc.Text() = %q, want %q", got, content+"x")
	}
}

func TestEditor_AttachCommandInsertsImageToken(t *testing.T) {
	editor := newTestEditor()
	path := writeTempPNG(t)
	editor.setContent("/attach image " + path)
	editor.cursor = Cursor{Line: 0, Col: editor.doc.TokenCount(0)}

	if _, _, _, done, err := editor.handleEvent(KeyEvent{Action: ActionSubmit}); err != nil {
		t.Fatalf("submit returned error: %v", err)
	} else if done {
		t.Fatal("submit should transform /attach into an editor-side insertion")
	}

	if got := editor.doc.Text(); got != "[IMAGE#1]" {
		t.Fatalf("doc.Text() = %q, want %q", got, "[IMAGE#1]")
	}
	if len(editor.doc.Images()) != 1 {
		t.Fatalf("expected one image attachment, got %d", len(editor.doc.Images()))
	}
}

func TestEditor_AttachmentPopupInsertsTokenAtCaret(t *testing.T) {
	editor := newTestEditor()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("name: hopclaw\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	editor.config.Completion = stubCompletionProvider{
		items: []CompletionItem{{
			Kind:   TokenFile,
			Label:  "config.yaml",
			Detail: "config.yaml",
			Path:   path,
		}},
	}

	mustHandleEvent(t, editor, KeyEvent{Action: ActionInsertRune, Rune: '@'})
	if editor.popup == nil {
		t.Fatal("expected popup to open after @")
	}
	mustHandleEvent(t, editor, KeyEvent{Action: ActionSubmit})

	if got := editor.doc.Text(); got != "[FILE#1: config.yaml]" {
		t.Fatalf("doc.Text() = %q, want %q", got, "[FILE#1: config.yaml]")
	}
}

func TestEditor_EscapeClosesPopupAndExpandedPreview(t *testing.T) {
	editor := newTestEditor()
	editor.config.Completion = stubCompletionProvider{
		items: []CompletionItem{{
			Kind:   TokenFile,
			Label:  "config.yaml",
			Detail: "config.yaml",
			Path:   filepath.Join(t.TempDir(), "config.yaml"),
		}},
	}

	mustHandleEvent(t, editor, KeyEvent{Action: ActionInsertRune, Rune: '@'})
	if editor.popup == nil {
		t.Fatal("expected popup to open after @")
	}
	mustHandleEvent(t, editor, KeyEvent{Action: ActionEscape})
	if editor.popup != nil {
		t.Fatalf("popup = %#v, want nil after Esc", editor.popup)
	}

	editor.doc.InsertImage(0, 0, testPNGDataURI, "image/png", "")
	editor.cursor = Cursor{Line: 0, Col: 0}
	mustHandleEvent(t, editor, KeyEvent{Action: ActionMoveUp})
	if editor.expanded == nil {
		t.Fatal("expected image preview to expand")
	}
	mustHandleEvent(t, editor, KeyEvent{Action: ActionEscape})
	if editor.expanded != nil {
		t.Fatalf("expanded = %#v, want nil after Esc", editor.expanded)
	}
}

func TestEditor_EscapeClearsDraftTextAndAttachments(t *testing.T) {
	editor := newTestEditor()
	editor.doc.InsertRune(0, 0, 'h')
	editor.doc.InsertRune(0, 1, 'i')
	editor.doc.InsertImage(0, 2, testPNGDataURI, "image/png", "")
	editor.cursor = Cursor{Line: 0, Col: 3}

	mustHandleEvent(t, editor, KeyEvent{Action: ActionEscape})

	if got := editor.doc.Text(); got != "" {
		t.Fatalf("doc.Text() = %q, want empty after Esc clear", got)
	}
	if editor.doc.HasAttachments() {
		t.Fatal("expected Esc to clear attachment tokens as well")
	}
	if editor.cursor.Line != 0 || editor.cursor.Col != 0 {
		t.Fatalf("cursor = %#v, want zero cursor after Esc clear", editor.cursor)
	}
}

func TestEditor_EscapeClosesNonModalOverlayWithoutClearingDraft(t *testing.T) {
	editor := newTestEditor()
	overlay := &stubOverlayController{panel: &OverlayPanel{Title: "Badge"}}
	editor.config.Overlay = overlay
	editor.doc.InsertRune(0, 0, 'h')
	editor.doc.InsertRune(0, 1, 'i')
	editor.cursor = Cursor{Line: 0, Col: 2}

	mustHandleEvent(t, editor, KeyEvent{Action: ActionEscape})

	if overlay.handled != 1 {
		t.Fatalf("overlay handled count = %d, want 1", overlay.handled)
	}
	if got := editor.doc.Text(); got != "hi" {
		t.Fatalf("doc.Text() = %q, want %q after closing overlay", got, "hi")
	}
	if editor.cursor != (Cursor{Line: 0, Col: 2}) {
		t.Fatalf("cursor = %#v, want unchanged draft cursor", editor.cursor)
	}
}

func TestEditor_EscapePrefersPopupOverNonModalOverlay(t *testing.T) {
	editor := newTestEditor()
	overlay := &stubOverlayController{panel: &OverlayPanel{Title: "Badge"}}
	editor.config.Overlay = overlay
	editor.config.Completion = stubCompletionProvider{
		items: []CompletionItem{{
			Kind:   TokenFile,
			Label:  "config.yaml",
			Detail: "config.yaml",
			Path:   filepath.Join(t.TempDir(), "config.yaml"),
		}},
	}

	mustHandleEvent(t, editor, KeyEvent{Action: ActionInsertRune, Rune: '@'})
	if editor.popup == nil {
		t.Fatal("expected popup to open after @")
	}
	mustHandleEvent(t, editor, KeyEvent{Action: ActionEscape})

	if editor.popup != nil {
		t.Fatalf("popup = %#v, want nil after Esc", editor.popup)
	}
	if overlay.handled != 0 {
		t.Fatalf("overlay handled count = %d, want 0 while popup is active", overlay.handled)
	}
}

func TestEditor_TabCompletesPathText(t *testing.T) {
	editor := newTestEditor()
	editor.config.Completion = stubCompletionProvider{pathCompletion: "./internal/cli/"}
	editor.setContent("./inte")
	editor.cursor = Cursor{Line: 0, Col: editor.doc.TokenCount(0)}

	mustHandleEvent(t, editor, KeyEvent{Action: ActionTab})

	if got := editor.doc.Text(); got != "./internal/cli/" {
		t.Fatalf("doc.Text() = %q, want %q", got, "./internal/cli/")
	}
}

func TestEditor_RestoreDraftWithAttachmentSnapshot(t *testing.T) {
	history := &stubDraftHistory{}
	editor := newTestEditor()
	editor.config.History = history
	editor.doc.InsertRune(0, 0, 'A')
	editor.doc.InsertImage(0, 1, testPNGDataURI, "image/png", "")
	editor.cursor = Cursor{Line: 0, Col: 2}

	if _, _, _, done, err := editor.handleEvent(KeyEvent{Action: ActionSubmit}); err != nil {
		t.Fatalf("submit returned error: %v", err)
	} else if !done {
		t.Fatal("submit did not complete editor")
	}

	restored := newTestEditor()
	restored.config.History = history
	mustHandleEvent(t, restored, KeyEvent{Action: ActionMoveUp})

	if got := restored.doc.Text(); got != "A[IMAGE#1]" {
		t.Fatalf("restored doc.Text() = %q, want %q", got, "A[IMAGE#1]")
	}
	if !restored.doc.HasImages() {
		t.Fatal("expected restored draft to keep image attachment")
	}
}

func TestDecodeDataURI(t *testing.T) {
	data, mediaType := decodeDataURI(testPNGDataURI)
	if mediaType != "image/png" {
		t.Fatalf("expected image/png, got %q", mediaType)
	}
	expected, err := base64.StdEncoding.DecodeString(testPNGBase64)
	if err != nil {
		t.Fatalf("failed to decode expected PNG data: %v", err)
	}
	if !bytes.Equal(data, expected) {
		t.Fatalf("decoded bytes mismatch: got %d bytes, want %d bytes", len(data), len(expected))
	}

	if data, mediaType := decodeDataURI("not-a-data-uri"); data != nil || mediaType != "" {
		t.Fatalf("expected invalid URI to return nil/empty, got %v/%q", data, mediaType)
	}
	if data, mediaType := decodeDataURI("data:image/png,abc"); data != nil || mediaType != "" {
		t.Fatalf("expected non-base64 URI to return nil/empty, got %v/%q", data, mediaType)
	}
	if data, mediaType := decodeDataURI("data:image/png;base64,%%%"); data != nil || mediaType != "" {
		t.Fatalf("expected malformed base64 URI to return nil/empty, got %v/%q", data, mediaType)
	}
}

func newTestEditor() *Editor {
	editor, _ := newBufferedTestEditor()
	return editor
}

func newBufferedTestEditor() (*Editor, *bytes.Buffer) {
	out := &bytes.Buffer{}
	editor := &Editor{
		doc:    NewDocument(),
		cursor: Cursor{},
		undo:   NewUndoStack(),
		config: EditorConfig{
			Prompt: "> ",
			Out:    out,
		},
		termWidth:  80,
		termHeight: 24,
	}
	return editor, out
}

func TestRenderIncludesWorkbenchChrome(t *testing.T) {
	doc := NewDocument()
	doc.InsertRune(0, 0, 'h')
	doc.InsertRune(0, 1, 'i')

	var out bytes.Buffer
	Render(&out, doc, Cursor{Line: 0, Col: 2}, "> ", RenderState{}, 80, 0, EditorView{
		Chrome: Chrome{
			Top:    "top rail",
			Bottom: "bottom rail",
		},
	})

	got := out.String()
	top := strings.Index(got, "top rail")
	prompt := strings.Index(got, "> hi")
	bottom := strings.Index(got, "bottom rail")
	switch {
	case top < 0:
		t.Fatalf("render output missing top chrome: %q", got)
	case prompt < 0:
		t.Fatalf("render output missing prompt body: %q", got)
	case bottom < 0:
		t.Fatalf("render output missing bottom chrome: %q", got)
	case !(top < prompt && prompt < bottom):
		t.Fatalf("render output order = top:%d prompt:%d bottom:%d, want top < prompt < bottom in %q", top, prompt, bottom, got)
	}
}

func TestRenderStateTracksPromptCursorRowWhenChromeIsPresent(t *testing.T) {
	doc := NewDocument()

	state := Render(io.Discard, doc, Cursor{Line: 0, Col: 0}, "> ", RenderState{}, 80, 0, EditorView{
		Chrome: Chrome{
			Top:    "top rail",
			Bottom: "bottom rail",
		},
	})

	if state.lineCount != 3 {
		t.Fatalf("RenderState.lineCount = %d, want 3", state.lineCount)
	}
	if state.cursorRow != 1 {
		t.Fatalf("RenderState.cursorRow = %d, want 1 for prompt row between chrome lines", state.cursorRow)
	}
}

func TestClearRenderUsesPromptCursorRowInsteadOfBottomLine(t *testing.T) {
	var out bytes.Buffer

	ClearRender(&out, RenderState{lineCount: 3, cursorRow: 1})

	got := out.String()
	if !strings.HasPrefix(got, "\033[1A\r") {
		t.Fatalf("ClearRender() should move up to the top from the prompt row, got %q", got)
	}
	if strings.HasPrefix(got, "\033[2A") {
		t.Fatalf("ClearRender() overshot by treating the cursor as if it were on the bottom row: %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("ClearRender() should use cursor motion instead of literal newlines, got %q", got)
	}
	if !strings.Contains(got, "\033[1B\r") {
		t.Fatalf("ClearRender() should step through lines with cursor-down motion, got %q", got)
	}
}

func TestRenderWrapsLongPromptLinesBeforeTerminalAutoWrap(t *testing.T) {
	doc := NewDocument()
	for _, r := range "abcdefghij" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}

	var out bytes.Buffer
	state := Render(&out, doc, Cursor{Line: 0, Col: doc.TokenCount(0)}, "> ", RenderState{}, 10, 0, EditorView{})

	got := stripANSI(out.String())
	if !strings.Contains(got, "> abcdefg\r\n  hij") {
		t.Fatalf("Render() should insert explicit CRLF for wrapped prompt text, got %q", got)
	}
	if state.lineCount != 2 {
		t.Fatalf("RenderState.lineCount = %d, want 2 for wrapped body rows", state.lineCount)
	}
	if state.cursorRow != 1 {
		t.Fatalf("RenderState.cursorRow = %d, want 1 on wrapped continuation row", state.cursorRow)
	}
}

func TestRenderWrapsWideRunesUsingDisplayWidth(t *testing.T) {
	doc := NewDocument()
	for _, r := range "垃圾焚烧发电厂房" {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}

	var out bytes.Buffer
	state := Render(&out, doc, Cursor{Line: 0, Col: doc.TokenCount(0)}, "> ", RenderState{}, 12, 0, EditorView{})

	got := stripANSI(out.String())
	if !strings.Contains(got, "> 垃圾焚烧\r\n  发电厂房") {
		t.Fatalf("Render() should wrap wide runes by display width, got %q", got)
	}
	if state.lineCount != 2 {
		t.Fatalf("RenderState.lineCount = %d, want 2 for wrapped wide-rune body rows", state.lineCount)
	}
	if state.cursorRow != 1 {
		t.Fatalf("RenderState.cursorRow = %d, want 1 on wrapped wide-rune continuation row", state.cursorRow)
	}
}

func TestRenderClipsTallBodyToKeepCursorVisible(t *testing.T) {
	doc := NewDocument()
	for i := 1; i <= 8; i++ {
		for _, r := range fmt.Sprintf("line-%d", i) {
			doc.InsertRune(doc.LineCount()-1, doc.TokenCount(doc.LineCount()-1), r)
		}
		if i < 8 {
			doc.InsertNewline(doc.LineCount()-1, doc.TokenCount(doc.LineCount()-1))
		}
	}

	var out bytes.Buffer
	state := Render(&out, doc, Cursor{Line: 7, Col: doc.TokenCount(7)}, "> ", RenderState{}, 32, 4, EditorView{})

	got := stripANSI(out.String())
	if strings.Contains(got, "line-1") || strings.Contains(got, "line-2") || strings.Contains(got, "line-3") || strings.Contains(got, "line-4") {
		t.Fatalf("Render() should clip lines above the viewport, got %q", got)
	}
	for _, want := range []string{"line-5", "line-6", "line-7", "line-8"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Render() missing visible clipped row %q in %q", want, got)
		}
	}
	if state.lineCount != 4 {
		t.Fatalf("RenderState.lineCount = %d, want 4 clipped rows", state.lineCount)
	}
	if state.cursorRow != 3 {
		t.Fatalf("RenderState.cursorRow = %d, want 3 on the last visible row", state.cursorRow)
	}
}

func TestRenderClipsWrappedBodyToKeepCursorVisible(t *testing.T) {
	doc := NewDocument()
	for _, r := range strings.Repeat("x", 40) {
		doc.InsertRune(0, doc.TokenCount(0), r)
	}

	var out bytes.Buffer
	state := Render(&out, doc, Cursor{Line: 0, Col: doc.TokenCount(0)}, "> ", RenderState{}, 12, 3, EditorView{})

	got := stripANSI(out.String())
	if strings.Contains(got, "> xxxxxxxx") {
		t.Fatalf("Render() should clip wrapped rows above the viewport, got %q", got)
	}
	if !strings.Contains(got, "  xxxxxxxx") {
		t.Fatalf("Render() should keep wrapped continuation rows visible, got %q", got)
	}
	if state.lineCount != 3 {
		t.Fatalf("RenderState.lineCount = %d, want 3 clipped wrapped rows", state.lineCount)
	}
	if state.cursorRow != 2 {
		t.Fatalf("RenderState.cursorRow = %d, want 2 on the last visible wrapped row", state.cursorRow)
	}
}

func TestCompactTerminalLinePreservesVisibleANSILines(t *testing.T) {
	line := "\033[34m[LOCAL]\033[0m \033[36m[MODEL deepseek-chat]\033[0m \033[32m[READY]\033[0m"

	got := compactTerminalLine(line, visibleWidth(line))

	if got != line {
		t.Fatalf("compactTerminalLine() should preserve visible-fitting ANSI line, got %q want %q", got, line)
	}
}

func TestCompactTerminalLineDropsBrokenANSIWhenCompacting(t *testing.T) {
	line := "\033[34m[LOCAL]\033[0m \033[36m[MODEL deepseek-chat]\033[0m \033[32m[READY]\033[0m"

	got := compactTerminalLine(line, 12)

	if strings.Contains(got, "\033") {
		t.Fatalf("compactTerminalLine() should not emit truncated ANSI escapes when compacting, got %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("compactTerminalLine() = %q, want ellipsis when compacted", got)
	}
}

func TestRenderShowsCanonicalAttachmentTokens(t *testing.T) {
	doc := NewDocument()
	doc.InsertImage(0, 0, testPNGDataURI, "image/png", "")
	doc.InsertRune(0, 1, ' ')
	doc.InsertFile(0, 2, filepath.Join(t.TempDir(), "plan.md"))
	doc.InsertRune(0, 3, ' ')
	doc.InsertLink(0, 4, "https://example.com/docs")

	var out bytes.Buffer
	Render(&out, doc, Cursor{Line: 0, Col: doc.TokenCount(0)}, "> ", RenderState{}, 96, 0, EditorView{})

	got := out.String()
	for _, want := range []string{"[IMAGE#1]", "[FILE#1: plan.md]", "[LINK#1]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("render output missing %q: %q", want, got)
		}
	}
}

func TestRenderDoesNotDuplicateAttachmentSummaryWhenUnselected(t *testing.T) {
	doc := NewDocument()
	doc.InsertImage(0, 0, testPNGDataURI, "image/png", "")
	doc.InsertImage(0, 1, testPNGDataURI, "image/png", "")
	doc.InsertImage(0, 2, testPNGDataURI, "image/png", "")

	var out bytes.Buffer
	Render(&out, doc, Cursor{Line: 0, Col: doc.TokenCount(0)}, "> ", RenderState{}, 120, 0, EditorView{})

	got := stripANSI(out.String())
	for _, unwanted := range []string{"attachments:", "selected:", "image/png", "222 KB"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("unselected attachments should not render duplicate rail output; found %q in %q", unwanted, got)
		}
	}
}

func TestRenderShowsSelectedAttachmentDetailsWithoutSummaryRail(t *testing.T) {
	doc := NewDocument()
	doc.InsertImage(0, 0, testPNGDataURI, "image/png", "")
	doc.InsertFile(0, 1, filepath.Join(t.TempDir(), "plan.md"))

	var out bytes.Buffer
	Render(&out, doc, Cursor{Line: 0, Col: 0}, "> ", RenderState{}, 120, 0, EditorView{})

	got := stripANSI(out.String())
	if strings.Contains(got, "attachments:") {
		t.Fatalf("selected attachment should not render a duplicate summary rail, got %q", got)
	}
	if !strings.Contains(got, "selected: IMAGE#1 · image/png") {
		t.Fatalf("selected attachment should render detail summary, got %q", got)
	}
}

func TestRenderOverlayUsesSharedPanelShell(t *testing.T) {
	var out bytes.Buffer
	Render(&out, NewDocument(), Cursor{}, "> ", RenderState{}, 96, 0, EditorView{
		Overlay: &OverlayPanel{
			Title:   "Runs",
			Summary: "Search: deploy",
			Lines: []OverlayLine{
				{Text: "run-128 deploy rollback", Selected: true},
				{Text: "run-129 config sync"},
			},
			Actions: "Enter inspect  Esc back",
			Tip:     "Use /runs recent for the full list.",
		},
	})

	got := out.String()
	for _, want := range []string{"Runs", "Search: deploy", "Actions: Enter inspect  Esc back", "Tip: Use /runs recent for the full list."} {
		if !strings.Contains(got, want) {
			t.Fatalf("overlay shell missing %q: %q", want, got)
		}
	}
}

func TestRenderOverlayBoxRowsStayAligned(t *testing.T) {
	var out bytes.Buffer
	Render(&out, NewDocument(), Cursor{}, "> ", RenderState{}, 96, 0, EditorView{
		Overlay: &OverlayPanel{
			Title:   "Badge",
			Lines:   []OverlayLine{{Text: "Current        B · color #00ff88 · size 3 · enabled false"}},
			Actions: "/badge set <id>  /badge show  /badge hide  Esc back",
		},
	})

	got := stripANSI(out.String())
	var boxLines []string
	for _, line := range strings.Split(got, "\r\n") {
		line = strings.TrimRight(line, "\n")
		if strings.HasPrefix(line, "┌") || strings.HasPrefix(line, "│") || strings.HasPrefix(line, "├") || strings.HasPrefix(line, "└") {
			boxLines = append(boxLines, line)
		}
	}
	if len(boxLines) == 0 {
		t.Fatalf("expected overlay box lines in %q", got)
	}
	want := visibleWidth(boxLines[0])
	for _, line := range boxLines[1:] {
		if visibleWidth(line) != want {
			t.Fatalf("overlay row width = %d, want %d: %q", visibleWidth(line), want, line)
		}
	}
}

type stubCompletionProvider struct {
	items           []CompletionItem
	pathCompletion  string
	slashCompletion string
}

type stubOverlayController struct {
	panel   *OverlayPanel
	handled int
}

func (s *stubOverlayController) Panel() *OverlayPanel { return s.panel }

func (s *stubOverlayController) HandleOverlayKey(evt KeyEvent) (OverlayResult, error) {
	if evt.Action != ActionEscape {
		return OverlayResult{}, nil
	}
	s.handled++
	s.panel = nil
	return OverlayResult{Handled: true}, nil
}

func (s stubCompletionProvider) AttachmentCandidates(string) []CompletionItem {
	return append([]CompletionItem(nil), s.items...)
}

func (s stubCompletionProvider) CompletePath(string) (string, bool) {
	if s.pathCompletion == "" {
		return "", false
	}
	return s.pathCompletion, true
}

func (s stubCompletionProvider) CompleteSlash(string, string) (string, bool) {
	if s.slashCompletion == "" {
		return "", false
	}
	return s.slashCompletion, true
}

type stubDraftHistory struct {
	entries []DocumentSnapshot
	cursor  int
	draft   *DocumentSnapshot
}

func (s *stubDraftHistory) Previous(current string) string { return current }

func (s *stubDraftHistory) Next() string { return "" }

func (s *stubDraftHistory) AddDraft(snap DocumentSnapshot) error {
	s.entries = append(s.entries, snap)
	s.cursor = len(s.entries)
	s.draft = nil
	return nil
}

func (s *stubDraftHistory) PreviousDraft(current DocumentSnapshot) (DocumentSnapshot, bool) {
	if len(s.entries) == 0 {
		return DocumentSnapshot{}, false
	}
	if s.cursor >= len(s.entries) {
		copy := current
		s.draft = &copy
	}
	if s.cursor > 0 {
		s.cursor--
	}
	return s.entries[s.cursor], true
}

func (s *stubDraftHistory) NextDraft() (DocumentSnapshot, bool) {
	if len(s.entries) == 0 {
		return DocumentSnapshot{}, false
	}
	if s.cursor < len(s.entries) {
		s.cursor++
	}
	if s.cursor >= len(s.entries) {
		if s.draft == nil {
			return DocumentSnapshot{}, false
		}
		return *s.draft, true
	}
	return s.entries[s.cursor], true
}

func pasteString(t *testing.T, editor *Editor, content string) {
	t.Helper()
	mustHandleEvent(t, editor, KeyEvent{Action: ActionPasteBegin})
	for _, r := range content {
		switch r {
		case '\n':
			mustHandleEvent(t, editor, KeyEvent{Action: ActionInsertNewline})
		default:
			mustHandleEvent(t, editor, KeyEvent{Action: ActionInsertRune, Rune: r})
		}
	}
	mustHandleEvent(t, editor, KeyEvent{Action: ActionPasteEnd})
}

func mustHandleEvent(t *testing.T, editor *Editor, evt KeyEvent) {
	t.Helper()
	if _, _, _, done, err := editor.handleEvent(evt); err != nil {
		t.Fatalf("handleEvent(%v) returned error: %v", evt.Action, err)
	} else if done {
		t.Fatalf("handleEvent(%v) unexpectedly completed editor", evt.Action)
	}
}

func writeTempPNG(t *testing.T) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(testPNGBase64)
	if err != nil {
		t.Fatalf("failed to decode test PNG: %v", err)
	}
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("failed to write test PNG: %v", err)
	}
	return path
}

const testPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+tmIoAAAAASUVORK5CYII="
const testPNGDataURI = "data:image/png;base64," + testPNGBase64
