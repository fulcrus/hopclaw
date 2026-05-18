package meta

import "testing"

func TestNormalizeChatType(t *testing.T) {
	t.Parallel()

	cases := map[string]ChatType{
		"direct":         ChatTypeDirect,
		"private":        ChatTypeDirect,
		"p2p":            ChatTypeDirect,
		"direct_message": ChatTypeDirect,
		"group":          ChatTypeGroup,
		"room":           ChatTypeGroup,
		"channel":        ChatTypeGroup,
		"unknown":        ChatTypeUnknown,
	}
	for input, want := range cases {
		if got := NormalizeChatType(input); got != want {
			t.Fatalf("NormalizeChatType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeStatusKind(t *testing.T) {
	t.Parallel()

	if got := NormalizeStatusKind("chat_reply"); got != StatusKindChatReply {
		t.Fatalf("NormalizeStatusKind(chat_reply) = %q", got)
	}
	if got := NormalizeStatusKind("PREFLIGHT_NEEDS_CONFIRMATION"); got != StatusKind("preflight_needs_confirmation") {
		t.Fatalf("NormalizeStatusKind(PREFLIGHT_NEEDS_CONFIRMATION) = %q", got)
	}
	if got := NormalizeStatusKind("made_up_kind"); got != StatusKindUnknown {
		t.Fatalf("NormalizeStatusKind(made_up_kind) = %q, want unknown", got)
	}
}

func TestPreflightStatusKind(t *testing.T) {
	t.Parallel()

	if got := PreflightStatusKind("Needs_Confirmation"); got != StatusKind("preflight_needs_confirmation") {
		t.Fatalf("PreflightStatusKind() = %q", got)
	}
	if got := PreflightStatusKind(" "); got != StatusKindUnknown {
		t.Fatalf("PreflightStatusKind(blank) = %q, want unknown", got)
	}
}
