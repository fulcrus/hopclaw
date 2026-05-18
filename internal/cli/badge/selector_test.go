package badge

import (
	"testing"
)

func TestSelectorIndexMapping(t *testing.T) {
	tests := []struct {
		id   string
		want int
	}{
		{id: "A", want: 0},
		{id: "Z", want: 25},
		{id: "custom-0", want: 26},
		{id: "custom-23", want: 49},
	}

	for _, tt := range tests {
		if got := selectorIndexForID(tt.id); got != tt.want {
			t.Fatalf("selectorIndexForID(%q) = %d, want %d", tt.id, got, tt.want)
		}
		if got := selectorIDForIndex(tt.want); got != tt.id {
			t.Fatalf("selectorIDForIndex(%d) = %q, want %q", tt.want, got, tt.id)
		}
	}
}

func TestSelectorMoveVertical(t *testing.T) {
	selector := &Selector{cursor: 0}
	selector.moveVertical(1)
	if selector.cursor != 9 {
		t.Fatalf("moveVertical(down) from A = %d, want 9", selector.cursor)
	}

	selector.cursor = 9
	selector.moveVertical(-1)
	if selector.cursor != 0 {
		t.Fatalf("moveVertical(up) from J = %d, want 0", selector.cursor)
	}

	selector.cursor = 25
	selector.moveVertical(1)
	if selector.cursor != 31 {
		t.Fatalf("moveVertical(down) from Z = %d, want 31", selector.cursor)
	}
}

func TestSelectorConfirmSelection(t *testing.T) {
	manager := newTestManager(t)
	selector := NewSelector(manager, nil, nil, nil)
	selector.cursor = selectorIndexForID("B")

	exit, changed, status, err := selector.confirmSelection()
	if err != nil {
		t.Fatalf("confirmSelection() error = %v", err)
	}
	if !exit || !changed {
		t.Fatalf("confirmSelection() = exit:%v changed:%v, want both true", exit, changed)
	}
	if status != "" {
		t.Fatalf("status = %q, want empty", status)
	}
	if got := manager.Config().Current; got != "B" {
		t.Fatalf("current = %q, want %q", got, "B")
	}
}

func TestSelectorConfirmEmptyCustomSlot(t *testing.T) {
	manager := newTestManager(t)
	selector := NewSelector(manager, nil, nil, nil)
	selector.cursor = selectorIndexForID("custom-0")

	exit, changed, status, err := selector.confirmSelection()
	if err != nil {
		t.Fatalf("confirmSelection() error = %v", err)
	}
	if exit || changed {
		t.Fatalf("confirmSelection() = exit:%v changed:%v, want both false", exit, changed)
	}
	if status != "Empty slot. Press i to import." {
		t.Fatalf("status = %q", status)
	}
}
