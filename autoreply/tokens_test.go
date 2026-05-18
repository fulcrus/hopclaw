package autoreply

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// IsSilentReply
// ---------------------------------------------------------------------------

func TestIsSilentReply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"exact_token", "NO_REPLY", true},
		{"leading_space", "  NO_REPLY", true},
		{"trailing_space", "NO_REPLY  ", true},
		{"surrounding_whitespace", " \t NO_REPLY \n ", true},
		{"empty", "", false},
		{"whitespace_only", "   ", false},
		{"other_text", "some other text", false},
		{"partial_token", "NO_REP", false},
		{"token_in_sentence", "Please say NO_REPLY", false},
		{"lowercase", "no_reply", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsSilentReply(tt.input)
			if got != tt.want {
				t.Errorf("IsSilentReply(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// IsSilentReplyPrefix
// ---------------------------------------------------------------------------

func TestIsSilentReplyPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"bare_NO", "NO", true},
		{"NO_", "NO_", true},
		{"NO_R", "NO_R", true},
		{"NO_RE", "NO_RE", true},
		{"NO_REP", "NO_REP", true},
		{"NO_REPL", "NO_REPL", true},
		{"full_token", "NO_REPLY", true},
		{"leading_space", "  NO", true},

		// Rejects natural language.
		{"mixed_case_No", "No", false},
		{"mixed_case_No_problem", "No problem", false},
		{"lowercase_no", "no", false},

		// Too short.
		{"single_char_N", "N", false},

		// Non-ASCII or non-letter/underscore.
		{"digits", "NO1", false},
		{"spaces", "NO REPLY", false},

		// Unrelated uppercase.
		{"unrelated_HE", "HE", false},
		{"unrelated_HEART", "HEART", false},

		// Empty.
		{"empty", "", false},
		{"whitespace_only", "   ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsSilentReplyPrefix(tt.input)
			if got != tt.want {
				t.Errorf("IsSilentReplyPrefix(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// StripSilentToken
// ---------------------------------------------------------------------------

func TestStripSilentToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"trailing_token", "We'll look into it NO_REPLY", "We'll look into it"},
		{"trailing_with_whitespace", "Thanks NO_REPLY  ", "Thanks"},
		{"token_only", "NO_REPLY", ""},
		{"token_after_asterisks", "done **NO_REPLY", "done"},
		{"no_token", "normal text", "normal text"},
		{"empty", "", ""},
		{"token_mid_sentence", "Say NO_REPLY now please", "Say NO_REPLY now please"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StripSilentToken(tt.input)
			if got != tt.want {
				t.Errorf("StripSilentToken(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// StripHeartbeatToken
// ---------------------------------------------------------------------------

func TestStripHeartbeatTokenPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantSkip  bool
		wantText  string
		wantStrip bool
	}{
		{
			name:      "token_only",
			input:     "HEARTBEAT_OK",
			wantSkip:  true,
			wantStrip: true,
		},
		{
			name:      "token_with_trailing_text",
			input:     "HEARTBEAT_OK all systems operational",
			wantText:  "all systems operational",
			wantStrip: true,
		},
		{
			name:     "empty",
			input:    "",
			wantSkip: true,
		},
		{
			name:     "whitespace_only",
			input:    "   ",
			wantSkip: true,
		},
		{
			name:     "no_token",
			input:    "Everything is running fine",
			wantText: "Everything is running fine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StripHeartbeatToken(tt.input, HeartbeatStripMessage, 0)
			if got.ShouldSkip != tt.wantSkip {
				t.Errorf("ShouldSkip = %v, want %v", got.ShouldSkip, tt.wantSkip)
			}
			if got.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tt.wantText)
			}
			if got.DidStrip != tt.wantStrip {
				t.Errorf("DidStrip = %v, want %v", got.DidStrip, tt.wantStrip)
			}
		})
	}
}

func TestStripHeartbeatTokenSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantSkip  bool
		wantText  string
		wantStrip bool
	}{
		{
			name:      "token_at_end",
			input:     "All good HEARTBEAT_OK",
			wantText:  "All good",
			wantStrip: true,
		},
		{
			name:      "token_at_end_with_period",
			input:     "All good. HEARTBEAT_OK.",
			wantText:  "All good.",
			wantStrip: true,
		},
		{
			name:      "token_at_end_with_exclamation",
			input:     "Fine HEARTBEAT_OK!!!",
			wantText:  "Fine",
			wantStrip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StripHeartbeatToken(tt.input, HeartbeatStripMessage, 0)
			if got.ShouldSkip != tt.wantSkip {
				t.Errorf("ShouldSkip = %v, want %v", got.ShouldSkip, tt.wantSkip)
			}
			if got.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tt.wantText)
			}
			if got.DidStrip != tt.wantStrip {
				t.Errorf("DidStrip = %v, want %v", got.DidStrip, tt.wantStrip)
			}
		})
	}
}

func TestStripHeartbeatTokenMarkup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantSkip  bool
		wantText  string
		wantStrip bool
	}{
		{
			name:      "html_bold",
			input:     "<b>HEARTBEAT_OK</b>",
			wantSkip:  true,
			wantStrip: true,
		},
		{
			name:      "markdown_bold",
			input:     "**HEARTBEAT_OK**",
			wantSkip:  true,
			wantStrip: true,
		},
		{
			name:      "markdown_code",
			input:     "`HEARTBEAT_OK`",
			wantSkip:  true,
			wantStrip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StripHeartbeatToken(tt.input, HeartbeatStripMessage, 0)
			if got.ShouldSkip != tt.wantSkip {
				t.Errorf("ShouldSkip = %v, want %v", got.ShouldSkip, tt.wantSkip)
			}
			if got.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tt.wantText)
			}
			if got.DidStrip != tt.wantStrip {
				t.Errorf("DidStrip = %v, want %v", got.DidStrip, tt.wantStrip)
			}
		})
	}
}

func TestStripHeartbeatTokenHeartbeatMode(t *testing.T) {
	t.Parallel()

	// In heartbeat mode, short remaining text is suppressed.
	got := StripHeartbeatToken("HEARTBEAT_OK short ack", HeartbeatStripHeartbeat, 300)
	if !got.ShouldSkip {
		t.Error("heartbeat mode should suppress short ack text")
	}
	if !got.DidStrip {
		t.Error("expected DidStrip=true")
	}

	// Long remaining text is preserved.
	longBody := strings.Repeat("Alert: service degraded. ", 20) // >300 chars
	longText := "HEARTBEAT_OK " + longBody
	got = StripHeartbeatToken(longText, HeartbeatStripHeartbeat, 300)
	if got.ShouldSkip {
		t.Error("heartbeat mode should preserve text longer than maxAckChars")
	}
}

// ---------------------------------------------------------------------------
// stripMarkup helper
// ---------------------------------------------------------------------------

func TestStripMarkup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"html_tags", "<b>hello</b>", " hello "},
		{"nbsp", "hello&nbsp;world", "hello world"},
		{"markdown_asterisks", "**hello**", "hello"},
		{"markdown_backticks", "`code`", "code"},
		{"mixed", "**<em>text</em>**", " text "},
		{"plain", "plain text", "plain text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stripMarkup(tt.input)
			if got != tt.want {
				t.Errorf("stripMarkup(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Cooldown key collision (regression test)
// ---------------------------------------------------------------------------

func TestCooldownKeyNoCollision(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()

	// These two pairs would collide under the old "ruleID:sessionKey" scheme.
	tracker.RecordFire("a:b", "c")
	tracker.RecordFire("a", "b:c")

	// "a:b" + "c" should be on cooldown.
	if !tracker.IsOnCooldown("a:b", "c", 1<<62) {
		t.Error("expected cooldown for (a:b, c)")
	}
	// "a" + "b:c" should be on cooldown.
	if !tracker.IsOnCooldown("a", "b:c", 1<<62) {
		t.Error("expected cooldown for (a, b:c)")
	}
	// "a:b:c" with empty session should NOT be on cooldown.
	if tracker.IsOnCooldown("a:b:c", "", 1<<62) {
		t.Error("unexpected cooldown for (a:b:c, empty)")
	}
}
