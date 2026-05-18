package badge

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/internal/cli/richedit"
	"golang.org/x/term"
)

// Selector provides an interactive terminal UI for badge selection.
type Selector struct {
	mgr        *Manager
	rdr        *Renderer
	out        io.Writer
	in         *os.File
	cursor     int
	selected   string
	termWidth  int
	termHeight int
	status     string
}

// NewSelector constructs an interactive selector for badge management.
func NewSelector(mgr *Manager, rdr *Renderer, out io.Writer, in *os.File) *Selector {
	if out == nil {
		out = io.Discard
	}
	selector := &Selector{
		mgr:    mgr,
		rdr:    rdr,
		out:    out,
		in:     in,
		cursor: 0,
	}
	if mgr != nil {
		selector.selected = mgr.Current()
		selector.cursor = selectorIndexForID(mgr.Current())
	}
	return selector
}

// Run executes the selector until the user confirms or exits.
func (s *Selector) Run() (changed bool, err error) {
	if s.mgr == nil {
		return false, fmt.Errorf("badge manager is required")
	}
	if s.in == nil {
		return false, fmt.Errorf("interactive badge selector requires a TTY")
	}
	fd := int(s.in.Fd())
	if !term.IsTerminal(fd) {
		return false, fmt.Errorf("interactive badge selector requires a TTY")
	}

	width, height, err := term.GetSize(fd)
	if err != nil || width <= 0 || height <= 0 {
		width, height = 100, 30
	}
	s.termWidth = width
	s.termHeight = height
	if s.rdr != nil {
		s.rdr.SetRow(4)
	}

	originalState, err := term.MakeRaw(fd)
	if err != nil {
		return false, err
	}
	defer term.Restore(fd, originalState)
	fmt.Fprint(s.out, "\033[?1049h\033[H\033[?25l")
	defer fmt.Fprint(s.out, "\033[?25h\033[?1049l")

	dirty := false
	for {
		if err := s.render(); err != nil {
			return dirty, err
		}

		event, raw, err := s.readKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return dirty, nil
			}
			return dirty, err
		}
		if len(raw) == 1 && raw[0] == 27 {
			return dirty, nil
		}

		switch event.Action {
		case richedit.ActionMoveLeft:
			s.moveHorizontal(-1)
		case richedit.ActionMoveRight:
			s.moveHorizontal(1)
		case richedit.ActionMoveUp:
			s.moveVertical(-1)
		case richedit.ActionMoveDown:
			s.moveVertical(1)
		case richedit.ActionSubmit:
			exit, selectedChanged, status, err := s.confirmSelection()
			if err != nil {
				s.status = err.Error()
				continue
			}
			s.status = status
			if exit {
				return dirty || selectedChanged, nil
			}
		case richedit.ActionInterrupt, richedit.ActionEOF:
			return dirty, nil
		case richedit.ActionInsertRune:
			r := strings.ToLower(string(event.Rune))
			switch r {
			case "q":
				return dirty, nil
			case "i":
				updated, status, err := s.importIntoCursor(originalState)
				if err != nil {
					s.status = err.Error()
					continue
				}
				dirty = dirty || updated
				s.status = status
			case "d":
				updated, status, err := s.deleteAtCursor(originalState)
				if err != nil {
					s.status = err.Error()
					continue
				}
				dirty = dirty || updated
				s.status = status
			case "c":
				updated, status, err := s.changeColor(originalState)
				if err != nil {
					s.status = err.Error()
					continue
				}
				dirty = dirty || updated
				s.status = status
			case "s":
				updated, status, err := s.changeSize(originalState)
				if err != nil {
					s.status = err.Error()
					continue
				}
				dirty = dirty || updated
				s.status = status
			case "h":
				s.status = "CLI: /badge set A  /badge import ~/Downloads/me.png 2  /badge color #00ff88  /badge size 4"
			}
		}
	}
}

func (s *Selector) render() error {
	fmt.Fprint(s.out, "\033[2J\033[H")

	cfg := s.mgr.Config()
	fmt.Fprintln(s.out, "Badge Setup")
	fmt.Fprintln(s.out, "Personal badge shown in terminal dock and chat header")
	fmt.Fprintln(s.out)

	fmt.Fprintln(s.out, "Current")
	fmt.Fprintf(s.out, "  Active:  %s\n", s.mgr.Current())
	fmt.Fprintf(s.out, "  Enabled: %t\n", cfg.Enabled)
	fmt.Fprintf(s.out, "  Color:   %s\n", cfg.Color)
	fmt.Fprintf(s.out, "  Size:    %d cells\n", cfg.Size)
	if source := s.currentSourcePath(); source != "" {
		fmt.Fprintf(s.out, "  Source:  %s\n", source)
	} else {
		fmt.Fprintln(s.out, "  Source:  built-in letter badge")
	}
	fmt.Fprintln(s.out)

	slots := s.mgr.ListSlots()
	fmt.Fprintln(s.out, "Library")
	fmt.Fprintln(s.out, "  Letters")
	fmt.Fprintln(s.out, s.renderLettersRow(0, 9, slots))
	fmt.Fprintln(s.out, s.renderLettersRow(9, 18, slots))
	fmt.Fprintln(s.out, s.renderLettersRow(18, 26, slots))
	fmt.Fprintln(s.out)
	fmt.Fprintln(s.out, "  Custom images")
	for row := 0; row < 4; row++ {
		start := 26 + row*6
		end := min(start+6, 50)
		fmt.Fprintln(s.out, s.renderCustomRow(start, end, slots))
	}
	fmt.Fprintln(s.out)

	if s.rdr != nil && s.rdr.Supported() && s.termWidth >= 80 {
		fmt.Fprintln(s.out, "Preview available in the right margin.")
		if err := s.renderPreview(cfg.Color); err != nil {
			s.status = err.Error()
		}
	} else {
		if s.termWidth < 80 {
			fmt.Fprintln(s.out, "Preview unavailable in compact view.")
		} else {
			fmt.Fprintln(s.out, "Preview unavailable in this terminal. Badge still works; use Enter to apply.")
		}
		fmt.Fprintln(s.out, "Selected:", s.previewDescription(slots))
	}

	fmt.Fprintln(s.out)
	fmt.Fprintln(s.out, "Actions")
	fmt.Fprintln(s.out, "  Enter apply   i import/replace   d remove   c color   s size   h help   Esc back")
	fmt.Fprintln(s.out, "CLI")
	fmt.Fprintln(s.out, "  /badge set A")
	fmt.Fprintln(s.out, "  /badge import ~/Downloads/me.png 2")
	fmt.Fprintln(s.out, "  /badge color #00ff88")
	fmt.Fprintln(s.out, "  /badge size 4")

	if strings.TrimSpace(s.status) != "" {
		fmt.Fprintln(s.out)
		fmt.Fprintln(s.out, s.status)
	}

	fmt.Fprintln(s.out)
	if !hasCustomSlots(slots) {
		fmt.Fprintln(s.out, "No custom badge yet.")
		fmt.Fprintln(s.out, "Press i to import a PNG/JPEG, or pick a letter badge directly.")
		fmt.Fprintln(s.out, "Example: /badge import ~/Downloads/me.png 0")
		return nil
	}
	fmt.Fprintln(s.out, "Tip: import accepts PNG/JPEG and stores it under ~/.hopclaw/avatars/custom-N.png")
	return nil
}

func (s *Selector) renderLettersRow(start, end int, slots []Slot) string {
	parts := make([]string, 0, end-start)
	for idx := start; idx < end; idx++ {
		parts = append(parts, s.formatSlot(idx, fmt.Sprintf("[%c]", rune('A'+idx)), slots[idx].Occupied))
	}
	return strings.Join(parts, " ")
}

func (s *Selector) renderCustomRow(start, end int, slots []Slot) string {
	parts := make([]string, 0, end-start)
	for idx := start; idx < end; idx++ {
		slot := slots[idx]
		label := fmt.Sprintf("[%d:---]", slot.Index+1)
		if slot.Occupied {
			label = fmt.Sprintf("[%d:img]", slot.Index+1)
		}
		parts = append(parts, s.formatSlot(idx, label, slot.Occupied))
	}
	return strings.Join(parts, " ")
}

func (s *Selector) formatSlot(index int, label string, occupied bool) string {
	id := selectorIDForIndex(index)
	if id == s.selected {
		label = "*" + label
	} else {
		label = " " + label
	}
	if index == s.cursor {
		return "\033[7m" + label + "\033[0m"
	}
	if !occupied && index >= 26 {
		return "\033[90m" + label + "\033[0m"
	}
	return label
}

func (s *Selector) renderPreview(hexColor string) error {
	if s.rdr == nil || !s.rdr.Supported() {
		return nil
	}
	slot := s.mgr.ListSlots()[s.cursor]
	if slot.Kind == SlotCustom && !slot.Occupied {
		return s.rdr.Clear(s.termWidth)
	}
	previewID := selectorIDForIndex(s.cursor)
	img, err := s.mgr.imageForID(previewID, hexColor)
	if err != nil {
		return s.rdr.Clear(s.termWidth)
	}
	return s.rdr.Show(img, s.termWidth)
}

func (s *Selector) previewDescription(slots []Slot) string {
	slot := slots[s.cursor]
	if slot.Kind == SlotLetter {
		return selectorIDForIndex(s.cursor)
	}
	if !slot.Occupied {
		return fmt.Sprintf("custom-%d is empty", slot.Index)
	}
	return selectorIDForIndex(s.cursor)
}

func (s *Selector) readKey() (richedit.KeyEvent, []byte, error) {
	buffer := make([]byte, 16)
	n, err := s.in.Read(buffer)
	if err != nil {
		return richedit.KeyEvent{}, nil, err
	}
	buffer = buffer[:n]
	if len(buffer) == 1 && buffer[0] == 27 {
		return richedit.KeyEvent{Action: richedit.ActionNone}, buffer, nil
	}
	event, _ := richedit.ParseKey(buffer)
	return event, buffer, nil
}

func (s *Selector) confirmSelection() (bool, bool, string, error) {
	slot := s.mgr.ListSlots()[s.cursor]
	if slot.Kind == SlotCustom && !slot.Occupied {
		return false, false, "Empty slot. Press i to import.", nil
	}

	before := s.mgr.Current()
	if err := s.mgr.SetCurrent(selectorIDForIndex(s.cursor)); err != nil {
		return false, false, "", err
	}
	if err := s.mgr.Save(); err != nil {
		return false, false, "", err
	}
	s.selected = s.mgr.Current()
	return true, before != s.selected, "", nil
}

func (s *Selector) importIntoCursor(originalState *term.State) (bool, string, error) {
	slot := s.mgr.ListSlots()[s.cursor]
	if slot.Kind != SlotCustom {
		return false, "Import is only available for custom slots.", nil
	}
	path, err := s.promptLine(originalState, "Import image path: ")
	if err != nil {
		return false, "", err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return false, "Import cancelled.", nil
	}
	if err := s.mgr.ImportImage(slot.Index, path); err != nil {
		return false, "", err
	}
	return true, fmt.Sprintf("Imported image into custom-%d.", slot.Index), nil
}

func (s *Selector) deleteAtCursor(originalState *term.State) (bool, string, error) {
	slot := s.mgr.ListSlots()[s.cursor]
	if slot.Kind != SlotCustom {
		return false, "Delete is only available for custom slots.", nil
	}
	if !slot.Occupied {
		return false, fmt.Sprintf("custom-%d is already empty.", slot.Index), nil
	}
	answer, err := s.promptLine(originalState, fmt.Sprintf("Delete custom-%d? [y/N]: ", slot.Index))
	if err != nil {
		return false, "", err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		return false, "Delete cancelled.", nil
	}
	if err := s.mgr.RemoveImage(slot.Index); err != nil {
		return false, "", err
	}
	if err := s.mgr.Save(); err != nil {
		return false, "", err
	}
	s.selected = s.mgr.Current()
	return true, fmt.Sprintf("Removed custom-%d.", slot.Index), nil
}

func (s *Selector) changeColor(originalState *term.State) (bool, string, error) {
	value, err := s.promptLine(originalState, "Badge color (#rgb or #rrggbb): ")
	if err != nil {
		return false, "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return false, "Color change cancelled.", nil
	}
	if err := s.mgr.SetColor(value); err != nil {
		return false, "", err
	}
	if err := s.mgr.Save(); err != nil {
		return false, "", err
	}
	return true, "Badge color updated to " + s.mgr.Config().Color + ".", nil
}

func (s *Selector) changeSize(originalState *term.State) (bool, string, error) {
	value, err := s.promptLine(originalState, "Badge size (2-6 cells): ")
	if err != nil {
		return false, "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return false, "Size change cancelled.", nil
	}
	size, err := strconv.Atoi(value)
	if err != nil {
		return false, "", fmt.Errorf("badge size must be an integer")
	}
	if err := s.mgr.SetSize(size); err != nil {
		return false, "", err
	}
	if err := s.mgr.Save(); err != nil {
		return false, "", err
	}
	return true, fmt.Sprintf("Badge size updated to %d cells.", s.mgr.Config().Size), nil
}

func (s *Selector) currentSourcePath() string {
	current := strings.TrimSpace(s.mgr.Current())
	if !strings.HasPrefix(current, "custom-") {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "~/.hopclaw/avatars/" + current + ".png"
	}
	return strings.Replace(filepath.Join(home, ".hopclaw", "avatars", current+".png"), home, "~", 1)
}

func hasCustomSlots(slots []Slot) bool {
	for _, slot := range slots {
		if slot.Kind == SlotCustom && slot.Occupied {
			return true
		}
	}
	return false
}

func (s *Selector) promptLine(originalState *term.State, prompt string) (string, error) {
	fd := int(s.in.Fd())
	if err := term.Restore(fd, originalState); err != nil {
		return "", err
	}
	fmt.Fprint(s.out, "\033[?25h")
	fmt.Fprintf(s.out, "\033[%d;1H\033[2K%s", s.termHeight, prompt)
	reader := bufio.NewReader(s.in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if _, err := term.MakeRaw(fd); err != nil {
		return "", err
	}
	fmt.Fprint(s.out, "\033[?25l")
	return strings.TrimRight(line, "\r\n"), nil
}

func (s *Selector) moveHorizontal(delta int) {
	next := s.cursor + delta
	switch {
	case next < 0:
		next = 49
	case next >= 50:
		next = 0
	}
	s.cursor = next
}

func (s *Selector) moveVertical(delta int) {
	current := selectorGridPos(s.cursor)
	bestIndex := s.cursor
	bestScore := int(^uint(0) >> 1)
	for idx := 0; idx < 50; idx++ {
		pos := selectorGridPos(idx)
		if delta < 0 && pos.row >= current.row {
			continue
		}
		if delta > 0 && pos.row <= current.row {
			continue
		}
		score := absInt(pos.row-current.row)*10 + absInt(pos.col-current.col)
		if score < bestScore {
			bestScore = score
			bestIndex = idx
		}
	}
	s.cursor = bestIndex
}

type selectorPos struct {
	row int
	col int
}

func selectorGridPos(index int) selectorPos {
	if index < 9 {
		return selectorPos{row: 0, col: index}
	}
	if index < 18 {
		return selectorPos{row: 1, col: index - 9}
	}
	if index < 26 {
		return selectorPos{row: 2, col: index - 18}
	}
	custom := index - 26
	return selectorPos{row: 4 + custom/6, col: custom % 6}
}

func selectorIndexForID(id string) int {
	kind, index, _, err := parseBadgeID(id)
	if err != nil {
		return 0
	}
	if kind == SlotLetter {
		return index
	}
	return 26 + index
}

func selectorIDForIndex(index int) string {
	if index < 26 {
		return string(rune('A' + index))
	}
	return fmt.Sprintf("custom-%d", index-26)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
