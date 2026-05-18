package richedit

import (
	"os"
	"testing"
)

func TestDetectImageProtocol(t *testing.T) {
	tests := []struct {
		termProgram string
		term        string
		expected    ImageProtocol
	}{
		{"iTerm.app", "xterm-256color", ProtocolITerm2},
		{"WezTerm", "xterm-256color", ProtocolKitty},
		{"", "xterm-kitty", ProtocolKitty},
		{"Apple_Terminal", "xterm-256color", ProtocolNone},
	}

	for _, tt := range tests {
		os.Setenv("TERM_PROGRAM", tt.termProgram)
		os.Setenv("TERM", tt.term)
		got := DetectImageProtocol()
		if got != tt.expected {
			t.Errorf("TERM_PROGRAM=%q TERM=%q: expected %d, got %d",
				tt.termProgram, tt.term, tt.expected, got)
		}
	}
	// Clean up
	os.Unsetenv("TERM_PROGRAM")
	os.Unsetenv("TERM")
}

func TestImageInfoText(t *testing.T) {
	text := ImageInfoText(1, "image/png", 25088)
	if text != "[IMAGE#1 · image/png · 24.5 KB]" {
		t.Fatalf("unexpected: %q", text)
	}
}
