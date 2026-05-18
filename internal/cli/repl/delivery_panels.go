package repl

import (
	"context"
	"fmt"
	"strings"
)

func (r *REPL) renderDeliveryPanel(ctx context.Context) error {
	items, err := r.service.ListGovernanceDeliveries(ctx, "", 20)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		r.openInfoPanel("Delivery", []string{"No delivery records found."}, "Esc back")
		return nil
	}

	panelItems := make([]panelItem, 0, len(items))
	for _, item := range items {
		row := fmt.Sprintf("%-12s %-10s %-10s %d/%d  %s",
			compact(defaultString(item.ID, "-"), 12),
			compact(defaultString(item.AdapterName, "-"), 10),
			compact(defaultString(item.Status, "-"), 10),
			item.Attempts, item.MaxAttempts,
			compact(defaultString(item.Summary, "-"), 40),
		)
		panelItems = append(panelItems, panelItem{
			ID:         item.ID,
			Text:       row,
			SearchText: strings.Join([]string{item.ID, item.RunID, item.AdapterName, item.Status, item.Summary}, " "),
		})
	}

	panel := newSelectionPanel(r, "Deliveries", "Search:", "Enter inspect  r redrive  Esc back", panelItems)
	panel.onConfirm = func(selected panelItem) (string, error) {
		r.renderDeliveryDetail(ctx, selected.ID, items)
		return "", nil
	}
	panel.hotkeys = map[rune]func(*selectionPanel, panelItem) (string, error){
		'r': func(_ *selectionPanel, item panelItem) (string, error) {
			if strings.TrimSpace(item.ID) == "" {
				return "", nil
			}
			return "/delivery redrive " + item.ID, nil
		},
	}
	r.openPromptPanel(panel)
	return nil
}

func (r *REPL) renderDeliveryDetail(_ context.Context, id string, items []DeliveryListItem) {
	var found *DeliveryListItem
	for i := range items {
		if items[i].ID == id {
			found = &items[i]
			break
		}
	}
	if found == nil {
		return
	}
	lines := []string{
		"ID: " + defaultString(found.ID, "-"),
		"Run: " + defaultString(found.RunID, "-"),
		"Adapter: " + defaultString(found.AdapterName, "-"),
		"Status: " + defaultString(found.Status, "-"),
		"Attempts: " + fmt.Sprintf("%d / %d", found.Attempts, found.MaxAttempts),
		"Last error: " + defaultString(found.LastError, "none"),
		"Next retry: " + defaultString(found.NextAt, "-"),
		"Can redrive: " + fmt.Sprintf("%t", found.CanRedrive),
		"Summary: " + defaultString(found.Summary, "-"),
	}
	actions := "/delivery redrive " + found.ID + "  /delivery  Esc back"
	if !found.CanRedrive {
		actions = "/delivery  Esc back"
	}
	r.openInfoPanel("Delivery "+compact(found.ID, 12), lines, actions)
}
