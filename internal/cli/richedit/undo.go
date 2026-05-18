package richedit

const maxUndoStates = 100

// DocumentSnapshot stores a frozen state of the document and cursor.
type DocumentSnapshot struct {
	Lines   [][]Token
	NextIDs [tokenKindCount]int
	Cursor  Cursor
}

// UndoStack manages undo/redo with a circular buffer.
type UndoStack struct {
	states []DocumentSnapshot
	pos    int // current position in the stack (-1 means empty)
}

// NewUndoStack creates an empty undo stack.
func NewUndoStack() *UndoStack {
	return &UndoStack{pos: -1}
}

// Push saves the current document state. Discards any redo states beyond current position.
func (u *UndoStack) Push(doc *Document, cursor Cursor) {
	snap := doc.Snapshot(cursor)

	// Truncate any redo states
	if u.pos+1 < len(u.states) {
		u.states = u.states[:u.pos+1]
	}
	u.states = append(u.states, snap)
	u.pos = len(u.states) - 1

	// Enforce max size
	if len(u.states) > maxUndoStates {
		excess := len(u.states) - maxUndoStates
		u.states = u.states[excess:]
		u.pos -= excess
		if u.pos < 0 {
			u.pos = 0
		}
	}
}

// Undo returns the previous state, or false if at the beginning.
func (u *UndoStack) Undo() (DocumentSnapshot, bool) {
	if u.pos <= 0 {
		return DocumentSnapshot{}, false
	}
	u.pos--
	return u.states[u.pos], true
}

// Redo returns the next state, or false if at the end.
func (u *UndoStack) Redo() (DocumentSnapshot, bool) {
	if u.pos >= len(u.states)-1 {
		return DocumentSnapshot{}, false
	}
	u.pos++
	return u.states[u.pos], true
}

// Len returns the number of states in the stack.
func (u *UndoStack) Len() int {
	return len(u.states)
}
