package autoreply

import (
	"testing"
	"time"
)

func TestApplyTemplateBasic(t *testing.T) {
	t.Parallel()

	ctx := MessageContext{
		SenderID:   "u123",
		SenderName: "Alice",
		Channel:    "slack",
		SessionKey: "sess-1",
		Content:    "hello",
		ReceivedAt: time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC),
	}

	got := applyTemplate("Hi {{.SenderName}}, welcome to {{.Channel}}!", ctx)
	want := "Hi Alice, welcome to slack!"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyTemplateDate(t *testing.T) {
	t.Parallel()

	ctx := MessageContext{
		ReceivedAt: time.Date(2025, 3, 10, 9, 5, 0, 0, time.UTC),
	}

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"date", "Today is {{.Date}}", "Today is 2025-03-10"},
		{"time", "Now {{.Time}}", "Now 09:05"},
		{"day_of_week", "It's {{.DayOfWeek}}", "It's Monday"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := applyTemplate(tt.tmpl, ctx)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyTemplateUnknown(t *testing.T) {
	t.Parallel()

	ctx := MessageContext{SenderName: "Bob"}
	got := applyTemplate("Hello {{.Unknown}} and {{.SenderName}}", ctx)
	want := "Hello {{.Unknown}} and Bob"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyTemplateEmpty(t *testing.T) {
	t.Parallel()

	got := applyTemplate("", MessageContext{})
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestApplyTemplateNoPlaceholders(t *testing.T) {
	t.Parallel()

	text := "This has no placeholders at all."
	got := applyTemplate(text, MessageContext{})
	if got != text {
		t.Errorf("got %q, want %q", got, text)
	}
}
