package repl

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

type promptPanel interface {
	richedit.OverlayController
	Fallback() (string, []string, string)
}

type panelPrompterSetter interface {
	SetOverlayController(richedit.OverlayController)
}

type panelItem struct {
	ID         string
	Text       string
	SearchText string
}

type infoPanel struct {
	repl    *REPL
	title   string
	lines   []string
	actions string
	closed  bool
}

func (p *infoPanel) Panel() *richedit.OverlayPanel {
	if p.closed {
		return nil
	}
	lines := make([]richedit.OverlayLine, 0, len(p.lines))
	for _, line := range p.lines {
		lines = append(lines, richedit.OverlayLine{Text: line})
	}
	return &richedit.OverlayPanel{
		Modal:   false,
		Title:   p.title,
		Lines:   lines,
		Actions: strings.TrimSpace(p.actions),
	}
}

func (p *infoPanel) Fallback() (string, []string, string) {
	return p.title, append([]string(nil), p.lines...), p.actions
}

func (p *infoPanel) HandleOverlayKey(evt richedit.KeyEvent) (richedit.OverlayResult, error) {
	switch evt.Action {
	case richedit.ActionEscape:
		p.closed = true
		p.repl.clearPanel()
		return richedit.OverlayResult{Handled: true}, nil
	default:
		return richedit.OverlayResult{}, nil
	}
}

type selectionPanel struct {
	repl        *REPL
	title       string
	searchLabel string
	query       string
	baseItems   []panelItem
	items       []panelItem
	selected    int
	actions     string
	emptyText   string
	status      string
	load        func(string) ([]panelItem, error)
	onConfirm   func(panelItem) (string, error)
	hotkeys     map[rune]func(*selectionPanel, panelItem) (string, error)
	closed      bool
}

func newSelectionPanel(repl *REPL, title, searchLabel, actions string, items []panelItem) *selectionPanel {
	panel := &selectionPanel{
		repl:        repl,
		title:       title,
		searchLabel: strings.TrimSpace(searchLabel),
		actions:     strings.TrimSpace(actions),
		emptyText:   "(no matches)",
		baseItems:   append([]panelItem(nil), items...),
	}
	panel.items = append([]panelItem(nil), items...)
	return panel
}

func (p *selectionPanel) Panel() *richedit.OverlayPanel {
	if p.closed {
		return nil
	}
	lines := make([]richedit.OverlayLine, 0, len(p.items))
	if len(p.items) == 0 {
		lines = append(lines, richedit.OverlayLine{Text: defaultString(p.emptyText, "(no matches)")})
	} else {
		for index, item := range p.items {
			lines = append(lines, richedit.OverlayLine{Text: item.Text, Selected: index == p.selected})
		}
	}
	return &richedit.OverlayPanel{
		Modal:   true,
		Title:   p.title,
		Summary: strings.TrimSpace(strings.TrimSpace(p.searchLabel) + " " + strings.TrimSpace(p.query)),
		Lines:   lines,
		Actions: strings.TrimSpace(p.actions),
		Tip:     strings.TrimSpace(p.status),
	}
}

func (p *selectionPanel) Fallback() (string, []string, string) {
	lines := make([]string, 0, len(p.items)+2)
	if p.searchLabel != "" {
		lines = append(lines, p.searchLabel+" "+strings.TrimSpace(p.query))
	}
	if len(p.items) == 0 {
		lines = append(lines, defaultString(p.emptyText, "(no matches)"))
	} else {
		for index, item := range p.items {
			prefix := "  "
			if index == p.selected {
				prefix = "> "
			}
			lines = append(lines, prefix+item.Text)
		}
	}
	if strings.TrimSpace(p.status) != "" {
		lines = append(lines, p.status)
	}
	return p.title, lines, p.actions
}

func (p *selectionPanel) HandleOverlayKey(evt richedit.KeyEvent) (richedit.OverlayResult, error) {
	switch evt.Action {
	case richedit.ActionEscape:
		p.closed = true
		p.repl.clearPanel()
		return richedit.OverlayResult{Handled: true}, nil
	case richedit.ActionMoveUp:
		p.move(-1)
		return richedit.OverlayResult{Handled: true}, nil
	case richedit.ActionMoveDown:
		p.move(1)
		return richedit.OverlayResult{Handled: true}, nil
	case richedit.ActionBackspace:
		if p.searchLabel == "" || p.query == "" {
			return richedit.OverlayResult{}, nil
		}
		_, size := utf8.DecodeLastRuneInString(p.query)
		if size > 0 {
			p.query = p.query[:len(p.query)-size]
			if err := p.refresh(); err != nil {
				return richedit.OverlayResult{}, err
			}
		}
		return richedit.OverlayResult{Handled: true}, nil
	case richedit.ActionSubmit:
		if p.onConfirm == nil || len(p.items) == 0 {
			p.closed = true
			p.repl.clearPanel()
			return richedit.OverlayResult{Handled: true}, nil
		}
		submit, err := p.onConfirm(p.items[p.selected])
		if err != nil {
			return richedit.OverlayResult{}, err
		}
		if strings.TrimSpace(submit) != "" {
			p.closed = true
			p.repl.clearPanel()
			return richedit.OverlayResult{Handled: true, Submit: submit}, nil
		}
		return richedit.OverlayResult{Handled: true}, nil
	case richedit.ActionInsertRune:
		if handler := p.hotkeys[unicode.ToLower(evt.Rune)]; handler != nil {
			item := panelItem{}
			if len(p.items) > 0 && p.selected >= 0 && p.selected < len(p.items) {
				item = p.items[p.selected]
			}
			submit, err := handler(p, item)
			if err != nil {
				return richedit.OverlayResult{}, err
			}
			if strings.TrimSpace(submit) != "" {
				p.closed = true
				p.repl.clearPanel()
				return richedit.OverlayResult{Handled: true, Submit: submit}, nil
			}
			return richedit.OverlayResult{Handled: true}, nil
		}
		if p.searchLabel != "" && unicode.IsPrint(evt.Rune) {
			p.query += string(evt.Rune)
			if err := p.refresh(); err != nil {
				return richedit.OverlayResult{}, err
			}
			return richedit.OverlayResult{Handled: true}, nil
		}
		return richedit.OverlayResult{}, nil
	default:
		return richedit.OverlayResult{}, nil
	}
}

func (p *selectionPanel) move(delta int) {
	if len(p.items) == 0 {
		p.selected = 0
		return
	}
	p.selected += delta
	if p.selected < 0 {
		p.selected = len(p.items) - 1
	}
	if p.selected >= len(p.items) {
		p.selected = 0
	}
}

func (p *selectionPanel) refresh() error {
	items := p.baseItems
	if p.load != nil {
		loaded, err := p.load(strings.TrimSpace(p.query))
		if err != nil {
			p.status = err.Error()
			return nil
		}
		items = loaded
		p.status = ""
	}
	if p.load == nil {
		query := strings.ToLower(strings.TrimSpace(p.query))
		if query != "" {
			filtered := make([]panelItem, 0, len(p.baseItems))
			for _, item := range p.baseItems {
				haystack := strings.ToLower(strings.TrimSpace(item.SearchText))
				if haystack == "" {
					haystack = strings.ToLower(item.Text)
				}
				if strings.Contains(haystack, query) {
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}
	}
	p.items = append(p.items[:0], items...)
	if len(p.items) == 0 {
		p.selected = 0
		return nil
	}
	p.selected = min(max(p.selected, 0), len(p.items)-1)
	return nil
}

type confirmPanel struct {
	repl     *REPL
	title    string
	lines    []string
	actions  string
	onYes    func() (string, error)
	onCancel func()
	closed   bool
}

func (p *confirmPanel) Panel() *richedit.OverlayPanel {
	if p.closed {
		return nil
	}
	lines := make([]richedit.OverlayLine, 0, len(p.lines))
	for _, line := range p.lines {
		lines = append(lines, richedit.OverlayLine{Text: line})
	}
	return &richedit.OverlayPanel{
		Modal:   true,
		Title:   p.title,
		Lines:   lines,
		Actions: strings.TrimSpace(p.actions),
	}
}

func (p *confirmPanel) Fallback() (string, []string, string) {
	return p.title, append([]string(nil), p.lines...), p.actions
}

func (p *confirmPanel) HandleOverlayKey(evt richedit.KeyEvent) (richedit.OverlayResult, error) {
	cancel := func() {
		p.closed = true
		if p.onCancel != nil {
			p.onCancel()
			return
		}
		p.repl.clearPanel()
	}
	switch evt.Action {
	case richedit.ActionEscape:
		cancel()
		return richedit.OverlayResult{Handled: true}, nil
	case richedit.ActionInsertRune:
		switch unicode.ToLower(evt.Rune) {
		case 'y':
			if p.onYes == nil {
				p.closed = true
				p.repl.clearPanel()
				return richedit.OverlayResult{Handled: true}, nil
			}
			submit, err := p.onYes()
			if err != nil {
				return richedit.OverlayResult{}, err
			}
			if strings.TrimSpace(submit) != "" {
				p.closed = true
				p.repl.clearPanel()
				return richedit.OverlayResult{Handled: true, Submit: submit}, nil
			}
			return richedit.OverlayResult{Handled: true}, nil
		case 'n':
			cancel()
			return richedit.OverlayResult{Handled: true}, nil
		}
	}
	return richedit.OverlayResult{}, nil
}

func (r *REPL) supportsInteractivePanels() bool {
	if r == nil || r.renderer == nil || !r.renderer.tty {
		return false
	}
	_, ok := r.prompter.(panelPrompterSetter)
	return ok
}

func (r *REPL) setPromptPanel(panel promptPanel) {
	if r == nil {
		return
	}
	r.panelController = panel
	if panel == nil {
		r.activePanel = ""
	} else if overlay := panel.Panel(); overlay != nil {
		r.activePanel = strings.TrimSpace(overlay.Title)
	} else {
		title, _, _ := panel.Fallback()
		r.activePanel = strings.TrimSpace(title)
	}
	r.refreshViewState()
}

func (r *REPL) openPromptPanel(panel promptPanel) {
	if panel == nil {
		r.clearPanel()
		return
	}
	if r.supportsInteractivePanels() {
		r.setPromptPanel(panel)
		return
	}
	title, lines, actions := panel.Fallback()
	r.showPanel(title, lines, actions)
}

func (r *REPL) newInfoPanel(title string, lines []string, actions string) promptPanel {
	return &infoPanel{
		repl:    r,
		title:   strings.TrimSpace(title),
		lines:   append([]string(nil), lines...),
		actions: strings.TrimSpace(actions),
	}
}

func (r *REPL) openInfoPanel(title string, lines []string, actions string) {
	r.openPromptPanel(r.newInfoPanel(title, lines, actions))
}

func matchPanelItems(items []panelItem, query string) []panelItem {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]panelItem(nil), items...)
	}
	filtered := make([]panelItem, 0, len(items))
	for _, item := range items {
		haystack := strings.ToLower(strings.TrimSpace(item.SearchText))
		if haystack == "" {
			haystack = strings.ToLower(item.Text)
		}
		if strings.Contains(haystack, query) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func firstPanelItem(items []panelItem) panelItem {
	if len(items) == 0 {
		return panelItem{}
	}
	return items[0]
}

func switchConfirmPanel(r *REPL, title string, body []string, submit string, previous promptPanel) promptPanel {
	return &confirmPanel{
		repl:    r,
		title:   title,
		lines:   append([]string(nil), body...),
		actions: "[y] confirm  [n] cancel",
		onYes: func() (string, error) {
			return submit, nil
		},
		onCancel: func() {
			if previous != nil {
				r.setPromptPanel(previous)
				return
			}
			r.clearPanel()
		},
	}
}

func sessionPanelItem(summary SessionSummary, currentSessionID string) panelItem {
	row := fmt.Sprintf("%-18s %-14s %d turns%s",
		compact(summary.Key, 18),
		compact(defaultString(summary.Model, "(default)"), 14),
		summary.MessageCount,
		currentMarker(summary.ID == currentSessionID),
	)
	return panelItem{
		ID:         summary.ID,
		Text:       row,
		SearchText: strings.Join([]string{summary.Key, summary.Model, row}, " "),
	}
}

func remotePanelItem(target TargetInfo, currentTarget string) panelItem {
	row := fmt.Sprintf("%-16s %s%s",
		compact(target.Name, 16),
		compact(target.Description, 54),
		currentMarker(target.Name == currentTarget),
	)
	return panelItem{
		ID:         target.Name,
		Text:       row,
		SearchText: strings.Join([]string{target.Name, target.Description}, " "),
	}
}

func modelPanelItem(item ModelInfo, current string) panelItem {
	ctxWindow := "-"
	if item.ContextWindow > 0 {
		ctxWindow = fmt.Sprintf("%dk ctx", item.ContextWindow/1000)
	}
	thinking := "no"
	if item.SupportsThinking {
		thinking = "yes"
	}
	row := fmt.Sprintf("%-18s %-10s thinking %-3s%s",
		compact(item.ID, 18),
		ctxWindow,
		thinking,
		currentMarker(item.ID == current),
	)
	return panelItem{
		ID:         item.ID,
		Text:       row,
		SearchText: strings.Join([]string{item.ID, row}, " "),
	}
}

func approvalPanelItem(item ApprovalSummary) panelItem {
	row := fmt.Sprintf("%-14s %-10s %-18s %-42s %s",
		compact(defaultString(item.ID, "(approval)"), 14),
		compact(defaultString(item.Status, "pending"), 10),
		compact(defaultString(item.ToolName, "tool"), 18),
		compact(defaultString(item.PolicySummary, "-"), 42),
		compact(defaultString(item.CreatedAt, "-"), 20),
	)
	return panelItem{
		ID:         item.ID,
		Text:       row,
		SearchText: strings.Join([]string{item.ID, item.Status, item.ToolName, item.PolicySummary, item.CreatedAt}, " "),
	}
}

func memoryPanelItem(itemKey, label, value, source, scope string) panelItem {
	row := joinNonEmpty("  ",
		compact(defaultString(itemKey, "(memory)"), 18),
		compact(defaultString(label, "-"), 12),
		compact(defaultString(source, "-"), 10),
		compact(value, 54),
		scope,
	)
	return panelItem{
		ID:         itemKey,
		Text:       row,
		SearchText: strings.Join([]string{itemKey, label, source, value, scope}, " "),
	}
}

func findPanelItemIndex(items []panelItem, id string) int {
	return slices.IndexFunc(items, func(item panelItem) bool {
		return item.ID == id
	})
}
