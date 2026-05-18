package richedit

// Cursor tracks position within the Document.
// Line is 0-based line index. Col is 0-based token index within the line.
type Cursor struct {
	Line int
	Col  int
}

// Clamp ensures cursor is within document bounds.
func (c *Cursor) Clamp(doc *Document) {
	if c.Line < 0 {
		c.Line = 0
	}
	if c.Line >= doc.LineCount() {
		c.Line = doc.LineCount() - 1
	}
	if c.Line < 0 {
		c.Line = 0
	}
	max := doc.TokenCount(c.Line)
	if c.Col > max {
		c.Col = max
	}
	if c.Col < 0 {
		c.Col = 0
	}
}

// MoveLeft moves cursor left by one token.
// If at start of line, wraps to end of previous line.
func (c *Cursor) MoveLeft(doc *Document) {
	if c.Col > 0 {
		c.Col--
		return
	}
	if c.Line > 0 {
		c.Line--
		c.Col = doc.TokenCount(c.Line)
	}
}

// MoveRight moves cursor right by one token.
// If at end of line, wraps to start of next line.
func (c *Cursor) MoveRight(doc *Document) {
	if c.Col < doc.TokenCount(c.Line) {
		c.Col++
		return
	}
	if c.Line < doc.LineCount()-1 {
		c.Line++
		c.Col = 0
	}
}

// MoveUp moves to the previous line, keeping col clamped.
func (c *Cursor) MoveUp(doc *Document) {
	if c.Line > 0 {
		c.Line--
		max := doc.TokenCount(c.Line)
		if c.Col > max {
			c.Col = max
		}
	}
}

// MoveDown moves to the next line, keeping col clamped.
func (c *Cursor) MoveDown(doc *Document) {
	if c.Line < doc.LineCount()-1 {
		c.Line++
		max := doc.TokenCount(c.Line)
		if c.Col > max {
			c.Col = max
		}
	}
}

// MoveWordLeft moves to the previous word boundary.
func (c *Cursor) MoveWordLeft(doc *Document) {
	if c.Col == 0 {
		if c.Line > 0 {
			c.Line--
			c.Col = doc.TokenCount(c.Line)
		}
		return
	}
	line := doc.Lines[c.Line]
	pos := c.Col
	// Skip spaces
	for pos > 0 && pos-1 < len(line) && line[pos-1].Kind == TokenText && line[pos-1].Rune == ' ' {
		pos--
	}
	// Skip word chars
	for pos > 0 && pos-1 < len(line) && line[pos-1].Kind == TokenText && line[pos-1].Rune != ' ' {
		pos--
	}
	c.Col = pos
}

// MoveWordRight moves to the next word boundary.
func (c *Cursor) MoveWordRight(doc *Document) {
	line := doc.Lines[c.Line]
	pos := c.Col
	if pos >= len(line) {
		if c.Line < doc.LineCount()-1 {
			c.Line++
			c.Col = 0
		}
		return
	}
	// Skip word chars
	for pos < len(line) && line[pos].Kind == TokenText && line[pos].Rune != ' ' {
		pos++
	}
	// Skip spaces
	for pos < len(line) && line[pos].Kind == TokenText && line[pos].Rune == ' ' {
		pos++
	}
	c.Col = pos
}

// MoveHome moves cursor to start of current line.
func (c *Cursor) MoveHome() {
	c.Col = 0
}

// MoveEnd moves cursor to end of current line.
func (c *Cursor) MoveEnd(doc *Document) {
	c.Col = doc.TokenCount(c.Line)
}

// TokenAtCursor returns the token under the cursor, or nil if cursor is at end of line.
func (c *Cursor) TokenAtCursor(doc *Document) *Token {
	if c.Line < 0 || c.Line >= doc.LineCount() {
		return nil
	}
	line := doc.Lines[c.Line]
	if c.Col < 0 || c.Col >= len(line) {
		return nil
	}
	return &line[c.Col]
}
