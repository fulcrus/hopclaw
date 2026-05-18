package cli

import "testing"

func TestAutomationUpdatePath(t *testing.T) {

	tests := []struct {
		kind string
		id   string
		want string
	}{
		{kind: "cron", id: "job-1", want: "/operator/cron/jobs/job-1"},
		{kind: "watch", id: "watch-1", want: "/operator/watch/items/watch-1"},
		{kind: "wakeup", id: "wake-1", want: "/operator/wakeup/triggers/wake-1"},
		{kind: "hook", id: "hook-1", want: "/operator/hooks/hook-1"},
	}

	for _, tt := range tests {
		got, err := automationUpdatePath(tt.kind, tt.id)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.kind, err)
		}
		if got != tt.want {
			t.Fatalf("%s: path = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestAutomationUpdatePathRejectsUnknownKind(t *testing.T) {

	if _, err := automationUpdatePath("unknown", "x"); err == nil {
		t.Fatal("expected error for unknown automation kind")
	}
}
