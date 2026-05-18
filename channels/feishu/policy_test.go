package feishu

import "testing"

func TestEvaluateInboundPolicyAllowlistAndMention(t *testing.T) {
	t.Parallel()

	account := ResolvedAccount{
		ID:                "china",
		DMPolicy:          "allowlist",
		AllowFrom:         []string{"ou_allowed"},
		GroupPolicy:       "allowlist",
		GroupAllowFrom:    []string{"ou_group"},
		RequireMention:    true,
		GroupSessionScope: "group_thread_sender",
	}

	if decision := evaluateInboundPolicy(account, inboundEnvelope{ChatType: "p2p", SenderID: "ou_denied"}); decision.allow {
		t.Fatal("expected DM allowlist denial")
	}
	if decision := evaluateInboundPolicy(account, inboundEnvelope{ChatType: "group", SenderID: "ou_group", Mentioned: false}); decision.allow {
		t.Fatal("expected group mention requirement denial")
	}
	if decision := evaluateInboundPolicy(account, inboundEnvelope{ChatType: "group", SenderID: "ou_group", Mentioned: true}); !decision.allow {
		t.Fatal("expected group message to be allowed")
	}
}

func TestEvaluateInboundPolicyTrimsChatTypeWhitespace(t *testing.T) {
	t.Parallel()

	account := ResolvedAccount{
		DMPolicy:       "allowlist",
		AllowFrom:      []string{"ou_allowed"},
		GroupPolicy:    "allowlist",
		GroupAllowFrom: []string{"ou_group"},
	}

	if decision := evaluateInboundPolicy(account, inboundEnvelope{ChatType: " p2p ", SenderID: "ou_allowed"}); !decision.allow || decision.groupScoped {
		t.Fatalf("expected trimmed p2p message to be allowed as dm, got %+v", decision)
	}
}

func TestSessionKeyForInboundUsesAccountAndThreadScope(t *testing.T) {
	t.Parallel()

	key := sessionKeyForInbound(inboundEnvelope{
		ChatType: "group",
		ChatID:   "oc_chat",
		ThreadID: "oc_thread",
		SenderID: "ou_user_1",
	}, ResolvedAccount{
		ID:                "china",
		GroupSessionScope: "group_thread_sender",
	})

	if key != "feishu:china:thread:oc_thread:sender:ou_user_1" {
		t.Fatalf("session key = %q", key)
	}
}

func TestSessionKeyForInboundTrimsWhitespace(t *testing.T) {
	t.Parallel()

	key := sessionKeyForInbound(inboundEnvelope{
		ChatType: " group ",
		ChatID:   " oc_chat ",
		ThreadID: " oc_thread ",
		SenderID: " ou_user_1 ",
	}, ResolvedAccount{
		ID:                "china",
		GroupSessionScope: "group_thread_sender",
	})

	if key != "feishu:china:thread:oc_thread:sender:ou_user_1" {
		t.Fatalf("session key = %q", key)
	}
}
