package channels

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

func TestParseApprovalReplyApprove(t *testing.T) {
	t.Parallel()

	approves := []string{"y", "Y", "yes", "YES", "  y  "}
	for _, input := range approves {
		action, ok := ParseApprovalReply(input)
		if !ok || action != ApprovalReplyApprove {
			t.Fatalf("ParseApprovalReply(%q) = (%q, %v), want (%q, true)", input, action, ok, ApprovalReplyApprove)
		}
	}
}

func TestParseApprovalReplyDeny(t *testing.T) {
	t.Parallel()

	denies := []string{"n", "N", "no", "NO", " no "}
	for _, input := range denies {
		action, ok := ParseApprovalReply(input)
		if !ok || action != ApprovalReplyDeny {
			t.Fatalf("ParseApprovalReply(%q) = (%q, %v), want (%q, true)", input, action, ok, ApprovalReplyDeny)
		}
	}
}

func TestParseApprovalReplyAlways(t *testing.T) {
	t.Parallel()

	always := []string{"a", "A", "always", "ALWAYS", "  a  "}
	for _, input := range always {
		action, ok := ParseApprovalReply(input)
		if !ok || action != ApprovalReplyAlways {
			t.Fatalf("ParseApprovalReply(%q) = (%q, %v), want (%q, true)", input, action, ok, ApprovalReplyAlways)
		}
	}
}

func TestParseApprovalReplyUnrecognized(t *testing.T) {
	t.Parallel()

	unknowns := []string{"", "hello", "maybe", "1", "2", "3", "yep"}
	for _, input := range unknowns {
		_, ok := ParseApprovalReply(input)
		if ok {
			t.Fatalf("ParseApprovalReply(%q) returned ok=true, want false", input)
		}
	}
}

func TestParseApprovalReplySelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  ApprovalReplyAction
	}{
		{"1", ApprovalReplyApprove},
		{"2", ApprovalReplyDeny},
		{"3", ApprovalReplyAlways},
	}
	for _, tt := range tests {
		got, ok := ParseApprovalReplySelection(tt.input)
		if !ok || got != tt.want {
			t.Fatalf("ParseApprovalReplySelection(%q) = (%q, %v), want (%q, true)", tt.input, got, ok, tt.want)
		}
	}
}

func TestParseApprovalReplySignalStructuredCallback(t *testing.T) {
	t.Parallel()

	action, source, ok := ParseApprovalReplySignal(map[string]any{
		"approval_action": "approval:deny",
	}, "")
	if !ok {
		t.Fatal("expected structured approval signal")
	}
	if action != ApprovalReplyDeny {
		t.Fatalf("action = %q, want %q", action, ApprovalReplyDeny)
	}
	if source != ApprovalReplySourceStructured {
		t.Fatalf("source = %q, want %q", source, ApprovalReplySourceStructured)
	}
}

func TestParseApprovalReplySignalNumberedBeforeDeprecatedText(t *testing.T) {
	t.Parallel()

	action, source, ok := ParseApprovalReplySignal(nil, "1")
	if !ok {
		t.Fatal("expected numbered approval signal")
	}
	if action != ApprovalReplyApprove {
		t.Fatalf("action = %q, want %q", action, ApprovalReplyApprove)
	}
	if source != ApprovalReplySourceNumbered {
		t.Fatalf("source = %q, want %q", source, ApprovalReplySourceNumbered)
	}
}

func TestSessionAutoApproveSessionNil(t *testing.T) {
	t.Parallel()

	if SessionAutoApproveSession(nil) {
		t.Fatal("expected false for nil session")
	}

	session := &agent.Session{}
	if SessionAutoApproveSession(session) {
		t.Fatal("expected false for session with nil metadata")
	}
}

func TestSessionAutoApproveSessionBool(t *testing.T) {
	t.Parallel()

	session := &agent.Session{
		Metadata: map[string]any{
			"channel.auto_approve_session": true,
		},
	}
	if !SessionAutoApproveSession(session) {
		t.Fatal("expected true when metadata has bool true")
	}

	session.Metadata["channel.auto_approve_session"] = false
	if SessionAutoApproveSession(session) {
		t.Fatal("expected false when metadata has bool false")
	}
}

func TestSessionAutoApproveSessionString(t *testing.T) {
	t.Parallel()

	session := &agent.Session{
		Metadata: map[string]any{
			"channel.auto_approve_session": "true",
		},
	}
	if !SessionAutoApproveSession(session) {
		t.Fatal("expected true when metadata has string 'true'")
	}

	session.Metadata["channel.auto_approve_session"] = "false"
	if SessionAutoApproveSession(session) {
		t.Fatal("expected false when metadata has string 'false'")
	}
}

func TestApprovalLanguageHintReturnsLastUserMessage(t *testing.T) {
	t.Parallel()

	session := &agent.Session{
		Session: contextengine.Session{
			Messages: []contextengine.Message{
				{Role: "user", Content: "帮我搜索新闻"},
				{Role: "assistant", Content: "好的，正在搜索..."},
				{Role: "user", Content: "1"},
			},
		},
	}

	hint := ApprovalLanguageHint(session)
	if hint != "帮我搜索新闻" {
		t.Fatalf("ApprovalLanguageHint = %q, want %q", hint, "帮我搜索新闻")
	}
}

func TestApprovalLanguageHintNilSession(t *testing.T) {
	t.Parallel()

	hint := ApprovalLanguageHint(nil)
	if hint != "" {
		t.Fatalf("ApprovalLanguageHint(nil) = %q, want empty", hint)
	}
}

func TestApprovalLanguageHintSkipsApprovalReplies(t *testing.T) {
	t.Parallel()

	session := &agent.Session{
		Session: contextengine.Session{
			Messages: []contextengine.Message{
				{Role: "user", Content: "search for news"},
				{Role: "assistant", Content: "I need approval."},
				{Role: "user", Content: "2"},
			},
		},
	}

	hint := ApprovalLanguageHint(session)
	if hint != "search for news" {
		t.Fatalf("ApprovalLanguageHint = %q, want %q", hint, "search for news")
	}
}

func TestEnableSessionAutoApproveNilStore(t *testing.T) {
	t.Parallel()

	err := EnableSessionAutoApproveSession(context.Background(), nil, "session-1")
	if err == nil {
		t.Fatal("expected error for nil session store")
	}
}
