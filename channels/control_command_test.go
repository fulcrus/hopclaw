package channels

import (
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestParseControlCommandRecognizesSlashCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  ControlCommand
		ok    bool
	}{
		{"/status", ControlCommandStatus, true},
		{"/progress", ControlCommandStatus, true},
		{"/cancel", ControlCommandCancel, true},
		{"/abort", ControlCommandCancel, true},
		// Non-English aliases must NOT match.
		{"/状态", "", false},
		{"/进度", "", false},
		{"/取消", "", false},
		{"hello", "", false},
		{"", "", false},
		{"/unknown", "", false},
	}

	for _, tt := range tests {
		cmd, ok := ParseControlCommand(tt.input)
		if ok != tt.ok || cmd != tt.want {
			t.Fatalf("ParseControlCommand(%q) = (%q, %v), want (%q, %v)", tt.input, cmd, ok, tt.want, tt.ok)
		}
	}
}

func TestParseControlCommandIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	cmd, ok := ParseControlCommand("/STATUS")
	if !ok || cmd != ControlCommandStatus {
		t.Fatalf("ParseControlCommand(/STATUS) = (%q, %v)", cmd, ok)
	}

	cmd, ok = ParseControlCommand("/Cancel")
	if !ok || cmd != ControlCommandCancel {
		t.Fatalf("ParseControlCommand(/Cancel) = (%q, %v)", cmd, ok)
	}
}

func TestParseControlCommandTrimsWhitespace(t *testing.T) {
	t.Parallel()

	cmd, ok := ParseControlCommand("  /status  ")
	if !ok || cmd != ControlCommandStatus {
		t.Fatalf("ParseControlCommand with whitespace = (%q, %v)", cmd, ok)
	}
}

func TestProgressRunPriorityRanking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status  string
		wantGt0 bool
	}{
		{"running", true},
		{"streaming", true},
		{"waiting_input", true},
		{"waiting_approval", true},
		{"queued", true},
		{"completed", false},
		{"failed", false},
		{"cancelled", false},
	}

	for _, tt := range tests {
		p := progressRunPriority(agent.RunStatus(tt.status))
		if tt.wantGt0 && p < 0 {
			t.Fatalf("progressRunPriority(%q) = %d, want >= 0", tt.status, p)
		}
		if !tt.wantGt0 && p >= 0 {
			t.Fatalf("progressRunPriority(%q) = %d, want < 0", tt.status, p)
		}
	}
}
