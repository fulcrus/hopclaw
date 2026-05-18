package repl

import (
	"fmt"
	"strings"

	badgepkg "github.com/fulcrus/hopclaw/internal/cli/badge"
)

func (r *REPL) renderBadgePanel() error {
	if r == nil || r.badgeMgr == nil {
		return fmt.Errorf("badge system is unavailable")
	}
	cfg := r.badgeMgr.Config()
	lines := []string{
		fmt.Sprintf("Current        %s · color %s · size %d · enabled %t", r.badgeMgr.Current(), cfg.Color, cfg.Size, cfg.Enabled),
		"Library        A-Z letter badges available",
		"Custom images  " + badgeCustomSummary(r.badgeMgr.ListSlots(), r.badgeMgr.Current()),
		"Actions        /badge set <id>  /badge show  /badge hide",
		"Actions        /badge import <path> [slot]  /badge remove <slot>",
		"Tip            /badge on and /badge off change the default for future terminal sessions.",
	}
	r.openInfoPanel("Badge", lines, "/badge set <id>  /badge show  /badge hide  /badge import <path> [slot]  /badge remove <slot>  Esc back")
	return nil
}

func badgeCustomSummary(slots []badgepkg.Slot, current string) string {
	items := make([]string, 0, 6)
	for _, slot := range slots {
		if slot.Kind != badgepkg.SlotCustom || !slot.Occupied {
			continue
		}
		label := fmt.Sprintf("custom-%d", slot.Index)
		if label == current {
			label += " current"
		}
		items = append(items, label)
		if len(items) >= 4 {
			break
		}
	}
	if len(items) == 0 {
		return "none imported"
	}
	return strings.Join(items, "  ")
}

func (r *REPL) confirmBadgeRemoval(slot int) (bool, error) {
	label := fmt.Sprintf("custom-%d", slot)
	if r.supportsInteractivePanels() {
		r.setPromptPanel(&confirmPanel{
			repl:    r,
			title:   "Remove " + label + "?",
			lines:   []string{"This deletes the stored image file."},
			actions: "[y] remove  [n] cancel",
			onYes: func() (string, error) {
				return "/badge remove " + internalConfirmedArg + " " + fmt.Sprintf("%d", slot), nil
			},
		})
		return false, nil
	}
	if r.prompter == nil {
		return true, nil
	}
	line, err := r.prompter.ReadLine(fmt.Sprintf("Remove %s? [y/N] ", label), r.commands)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
