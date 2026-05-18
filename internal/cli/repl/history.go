package repl

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

type History struct {
	path        string
	maxEntries  int
	entries     []string
	cursor      int
	draft       string
	richEntries []richedit.DocumentSnapshot
	richCursor  int
	richDraft   *richedit.DocumentSnapshot
}

func NewHistory(path string, maxEntries int) *History {
	if maxEntries <= 0 {
		maxEntries = 500
	}
	history := &History{
		path:       path,
		maxEntries: maxEntries,
	}
	_ = history.Load()
	return history
}

func (h *History) Load() error {
	if h == nil || h.path == "" {
		return nil
	}
	data, err := os.ReadFile(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(data), "\n")
	h.entries = h.entries[:0]
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		h.entries = append(h.entries, line)
	}
	if len(h.entries) > h.maxEntries {
		h.entries = append([]string(nil), h.entries[len(h.entries)-h.maxEntries:]...)
	}
	h.cursor = len(h.entries)
	h.richCursor = len(h.richEntries)
	return nil
}

func (h *History) Add(line string) error {
	if h == nil {
		return nil
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == line {
		h.cursor = len(h.entries)
		h.draft = ""
		return nil
	}
	h.entries = append(h.entries, line)
	if len(h.entries) > h.maxEntries {
		h.entries = append([]string(nil), h.entries[len(h.entries)-h.maxEntries:]...)
	}
	h.cursor = len(h.entries)
	h.draft = ""
	h.richCursor = len(h.richEntries)
	h.richDraft = nil
	if h.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(h.path, []byte(strings.Join(h.entries, "\n")+"\n"), 0o644)
}

func (h *History) Previous(current string) string {
	if h == nil || len(h.entries) == 0 {
		return current
	}
	if h.cursor >= len(h.entries) {
		h.draft = current
	}
	if h.cursor > 0 {
		h.cursor--
	}
	return h.entries[h.cursor]
}

func (h *History) Next() string {
	if h == nil || len(h.entries) == 0 {
		return ""
	}
	if h.cursor < len(h.entries) {
		h.cursor++
	}
	if h.cursor >= len(h.entries) {
		return h.draft
	}
	return h.entries[h.cursor]
}

func (h *History) AddDraft(snap richedit.DocumentSnapshot) error {
	if h == nil {
		return nil
	}
	h.richEntries = append(h.richEntries, cloneSnapshot(snap))
	if len(h.richEntries) > h.maxEntries {
		h.richEntries = append([]richedit.DocumentSnapshot(nil), h.richEntries[len(h.richEntries)-h.maxEntries:]...)
	}
	h.richCursor = len(h.richEntries)
	h.richDraft = nil
	return nil
}

func (h *History) PreviousDraft(current richedit.DocumentSnapshot) (richedit.DocumentSnapshot, bool) {
	if h == nil || len(h.richEntries) == 0 {
		return richedit.DocumentSnapshot{}, false
	}
	if h.richCursor >= len(h.richEntries) {
		draft := cloneSnapshot(current)
		h.richDraft = &draft
	}
	if h.richCursor > 0 {
		h.richCursor--
	}
	return cloneSnapshot(h.richEntries[h.richCursor]), true
}

func (h *History) NextDraft() (richedit.DocumentSnapshot, bool) {
	if h == nil || len(h.richEntries) == 0 {
		if h != nil && h.richDraft != nil {
			snap := cloneSnapshot(*h.richDraft)
			h.richDraft = nil
			return snap, true
		}
		return richedit.DocumentSnapshot{}, false
	}
	if h.richCursor < len(h.richEntries) {
		h.richCursor++
	}
	if h.richCursor >= len(h.richEntries) {
		if h.richDraft == nil {
			return richedit.DocumentSnapshot{}, false
		}
		snap := cloneSnapshot(*h.richDraft)
		h.richDraft = nil
		return snap, true
	}
	return cloneSnapshot(h.richEntries[h.richCursor]), true
}

func cloneSnapshot(in richedit.DocumentSnapshot) richedit.DocumentSnapshot {
	out := richedit.DocumentSnapshot{
		Cursor: in.Cursor,
	}
	copy(out.NextIDs[:], in.NextIDs[:])
	out.Lines = make([][]richedit.Token, len(in.Lines))
	for i, line := range in.Lines {
		out.Lines[i] = append([]richedit.Token(nil), line...)
	}
	return out
}
