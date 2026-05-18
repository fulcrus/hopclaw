package richedit

import "testing"

func TestParseKeyAltEnterInsertsNewline(t *testing.T) {
	for _, seq := range [][]byte{{27, 13}, {27, 10}} {
		event, consumed := ParseKey(seq)
		if event.Action != ActionInsertNewline {
			t.Fatalf("ParseKey(%v) action = %v, want %v", seq, event.Action, ActionInsertNewline)
		}
		if consumed != 2 {
			t.Fatalf("ParseKey(%v) consumed = %d, want 2", seq, consumed)
		}
	}
}

func TestParseKeyCtrlVPastesClipboard(t *testing.T) {
	event, consumed := ParseKey([]byte{22})
	if event.Action != ActionPasteClipboard {
		t.Fatalf("ParseKey(ctrl+v) action = %v, want %v", event.Action, ActionPasteClipboard)
	}
	if consumed != 1 {
		t.Fatalf("ParseKey(ctrl+v) consumed = %d, want 1", consumed)
	}
}
