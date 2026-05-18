package richedit

// Action represents an editor command triggered by a key.
type Action int

const (
	ActionNone Action = iota
	ActionInsertRune
	ActionInsertNewline // Ctrl+Enter (or Ctrl+J)
	ActionSubmit        // Enter
	ActionBackspace
	ActionDelete
	ActionMoveLeft
	ActionMoveRight
	ActionMoveUp
	ActionMoveDown
	ActionMoveWordLeft   // Ctrl+Left or Alt+B
	ActionMoveWordRight  // Ctrl+Right or Alt+F
	ActionMoveHome       // Home or Ctrl+A
	ActionMoveEnd        // End or Ctrl+E
	ActionKillToEnd      // Ctrl+K
	ActionKillToStart    // Ctrl+U
	ActionKillWord       // Ctrl+W
	ActionUndo           // Ctrl+Z
	ActionRedo           // Ctrl+Y
	ActionTab            // Tab
	ActionEscape         // Esc
	ActionInterrupt      // Ctrl+C
	ActionEOF            // Ctrl+D
	ActionClearScreen    // Ctrl+L
	ActionPasteClipboard // Ctrl+V
	ActionPasteBegin     // ESC [200~
	ActionPasteEnd       // ESC [201~
)

// KeyEvent holds a parsed key input.
type KeyEvent struct {
	Action Action
	Rune   rune // only valid when Action == ActionInsertRune
}

// ParseKey reads bytes from a raw terminal input buffer and returns a KeyEvent.
// buf is the bytes read so far. Returns the event and how many bytes were consumed.
func ParseKey(buf []byte) (KeyEvent, int) {
	if len(buf) == 0 {
		return KeyEvent{Action: ActionNone}, 0
	}

	b := buf[0]

	// Control characters
	switch b {
	case 1: // Ctrl+A
		return KeyEvent{Action: ActionMoveHome}, 1
	case 3: // Ctrl+C
		return KeyEvent{Action: ActionInterrupt}, 1
	case 4: // Ctrl+D
		return KeyEvent{Action: ActionEOF}, 1
	case 5: // Ctrl+E
		return KeyEvent{Action: ActionMoveEnd}, 1
	case 10: // Ctrl+J (line feed) — treat as insert newline
		return KeyEvent{Action: ActionInsertNewline}, 1
	case 11: // Ctrl+K
		return KeyEvent{Action: ActionKillToEnd}, 1
	case 12: // Ctrl+L
		return KeyEvent{Action: ActionClearScreen}, 1
	case 13: // Enter (CR)
		return KeyEvent{Action: ActionSubmit}, 1
	case 9: // Tab
		return KeyEvent{Action: ActionTab}, 1
	case 21: // Ctrl+U
		return KeyEvent{Action: ActionKillToStart}, 1
	case 22: // Ctrl+V
		return KeyEvent{Action: ActionPasteClipboard}, 1
	case 23: // Ctrl+W
		return KeyEvent{Action: ActionKillWord}, 1
	case 25: // Ctrl+Y
		return KeyEvent{Action: ActionRedo}, 1
	case 26: // Ctrl+Z
		return KeyEvent{Action: ActionUndo}, 1
	case 127, 8: // Backspace / Ctrl+H
		return KeyEvent{Action: ActionBackspace}, 1
	}

	// ESC sequences
	if b == 27 {
		if len(buf) < 2 {
			return KeyEvent{Action: ActionEscape}, 1
		}
		if buf[1] == 13 || buf[1] == 10 {
			return KeyEvent{Action: ActionInsertNewline}, 2
		}
		if buf[1] == '[' {
			return parseCSI(buf[2:])
		}
		// Alt+key
		if buf[1] == 'b' || buf[1] == 'B' {
			return KeyEvent{Action: ActionMoveWordLeft}, 2
		}
		if buf[1] == 'f' || buf[1] == 'F' {
			return KeyEvent{Action: ActionMoveWordRight}, 2
		}
		return KeyEvent{Action: ActionNone}, 2 // unknown ESC sequence
	}

	// UTF-8 rune
	r, size := decodeUTF8(buf)
	if size == 0 {
		return KeyEvent{Action: ActionNone}, 0 // need more bytes
	}
	return KeyEvent{Action: ActionInsertRune, Rune: r}, size
}

// parseCSI handles ESC [ ... sequences. buf starts after "ESC [".
func parseCSI(buf []byte) (KeyEvent, int) {
	if len(buf) == 0 {
		return KeyEvent{Action: ActionNone}, 0
	}

	// Bracketed paste
	if len(buf) >= 4 && buf[0] == '2' && buf[1] == '0' && buf[2] == '0' && buf[3] == '~' {
		return KeyEvent{Action: ActionPasteBegin}, 6 // ESC [ 2 0 0 ~
	}
	if len(buf) >= 4 && buf[0] == '2' && buf[1] == '0' && buf[2] == '1' && buf[3] == '~' {
		return KeyEvent{Action: ActionPasteEnd}, 6 // ESC [ 2 0 1 ~
	}

	switch buf[0] {
	case 'A':
		return KeyEvent{Action: ActionMoveUp}, 3 // ESC [ A
	case 'B':
		return KeyEvent{Action: ActionMoveDown}, 3
	case 'C':
		return KeyEvent{Action: ActionMoveRight}, 3
	case 'D':
		return KeyEvent{Action: ActionMoveLeft}, 3
	case 'H':
		return KeyEvent{Action: ActionMoveHome}, 3
	case 'F':
		return KeyEvent{Action: ActionMoveEnd}, 3
	case '3':
		if len(buf) >= 2 && buf[1] == '~' {
			return KeyEvent{Action: ActionDelete}, 4 // ESC [ 3 ~
		}
	case '1':
		// ESC [ 1 ; 5 C = Ctrl+Right, ESC [ 1 ; 5 D = Ctrl+Left
		if len(buf) >= 4 && buf[1] == ';' && buf[2] == '5' {
			switch buf[3] {
			case 'C':
				return KeyEvent{Action: ActionMoveWordRight}, 6
			case 'D':
				return KeyEvent{Action: ActionMoveWordLeft}, 6
			}
		}
	}

	// Unknown CSI — skip to next letter
	for i, c := range buf {
		if c >= 0x40 && c <= 0x7E {
			return KeyEvent{Action: ActionNone}, i + 3
		}
	}
	return KeyEvent{Action: ActionNone}, 2
}

// decodeUTF8 decodes a single UTF-8 rune from buf.
// Returns the rune and byte count, or (0, 0) if buf is incomplete.
func decodeUTF8(buf []byte) (rune, int) {
	if len(buf) == 0 {
		return 0, 0
	}
	b := buf[0]
	if b < 0x80 {
		return rune(b), 1
	}
	var size int
	switch {
	case b&0xE0 == 0xC0:
		size = 2
	case b&0xF0 == 0xE0:
		size = 3
	case b&0xF8 == 0xF0:
		size = 4
	default:
		return 0xFFFD, 1 // replacement char
	}
	if len(buf) < size {
		return 0, 0 // incomplete
	}
	r := rune(b & (0xFF >> (size + 1)))
	for i := 1; i < size; i++ {
		if buf[i]&0xC0 != 0x80 {
			return 0xFFFD, 1
		}
		r = r<<6 | rune(buf[i]&0x3F)
	}
	return r, size
}
