package autoreply

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func baseConfig(rules ...Rule) Config {
	return Config{
		Enabled: true,
		Rules:   rules,
	}
}

func enabledRule(id, name string, mode MatchMode, pattern, response string) Rule {
	return Rule{
		ID:        id,
		Name:      name,
		Enabled:   true,
		Priority:  10,
		MatchMode: mode,
		Pattern:   pattern,
		Response:  response,
	}
}

func makeMsg(content string) MessageContext {
	return MessageContext{
		SenderID:   "u1",
		SenderName: "Alice",
		Channel:    "slack",
		SessionKey: "sess-1",
		Content:    content,
		ReceivedAt: time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC),
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestEvaluateExactMatch(t *testing.T) {
	t.Parallel()

	e := NewEngine(baseConfig(
		enabledRule("r1", "greeting", MatchExact, "hello", "Hi there!"),
	))

	reply := e.Evaluate(context.Background(), makeMsg("hello"))
	if reply == nil {
		t.Fatal("expected a reply")
	}
	if reply.Text != "Hi there!" {
		t.Errorf("got %q, want %q", reply.Text, "Hi there!")
	}
	if reply.RuleID != "r1" {
		t.Errorf("got rule ID %q, want %q", reply.RuleID, "r1")
	}
}

func TestEvaluateRegexMatch(t *testing.T) {
	t.Parallel()

	e := NewEngine(baseConfig(
		enabledRule("r1", "order", MatchRegex, `order\s+#?\d+`, "Got your order!"),
	))

	reply := e.Evaluate(context.Background(), makeMsg("I need help with order #42"))
	if reply == nil {
		t.Fatal("expected a reply")
	}
	if reply.Text != "Got your order!" {
		t.Errorf("got %q", reply.Text)
	}
}

func TestEvaluatePrefixMatch(t *testing.T) {
	t.Parallel()

	e := NewEngine(baseConfig(
		enabledRule("r1", "help", MatchPrefix, "/help", "How can I help?"),
	))

	reply := e.Evaluate(context.Background(), makeMsg("/help me with something"))
	if reply == nil {
		t.Fatal("expected a reply")
	}
	if reply.Text != "How can I help?" {
		t.Errorf("got %q", reply.Text)
	}

	noMatch := e.Evaluate(context.Background(), makeMsg("I need /help"))
	if noMatch != nil {
		t.Error("prefix should not match when pattern is in the middle")
	}
}

func TestEvaluateContainsMatch(t *testing.T) {
	t.Parallel()

	e := NewEngine(baseConfig(
		enabledRule("r1", "pricing", MatchContains, "pricing", "Check our website!"),
	))

	reply := e.Evaluate(context.Background(), makeMsg("What is your pricing plan?"))
	if reply == nil {
		t.Fatal("expected a reply")
	}
	if reply.Text != "Check our website!" {
		t.Errorf("got %q", reply.Text)
	}
}

func TestEvaluateAnyMatch(t *testing.T) {
	t.Parallel()

	e := NewEngine(baseConfig(
		enabledRule("r1", "fallback", MatchAny, "", "Sorry, I'm away."),
	))

	reply := e.Evaluate(context.Background(), makeMsg("literally anything"))
	if reply == nil {
		t.Fatal("expected a reply")
	}
	if reply.Text != "Sorry, I'm away." {
		t.Errorf("got %q", reply.Text)
	}
}

func TestEvaluatePriority(t *testing.T) {
	t.Parallel()

	low := enabledRule("r-low", "low", MatchContains, "test", "low priority")
	low.Priority = 20

	high := enabledRule("r-high", "high", MatchContains, "test", "high priority")
	high.Priority = 1

	e := NewEngine(baseConfig(low, high))

	reply := e.Evaluate(context.Background(), makeMsg("test message"))
	if reply == nil {
		t.Fatal("expected a reply")
	}
	if reply.RuleID != "r-high" {
		t.Errorf("expected high-priority rule, got %q", reply.RuleID)
	}
}

func TestEvaluateChannelRestriction(t *testing.T) {
	t.Parallel()

	rule := enabledRule("r1", "discord-only", MatchAny, "", "Discord reply")
	rule.Channels = []string{"discord"}

	e := NewEngine(baseConfig(rule))

	// Slack message should not match.
	reply := e.Evaluate(context.Background(), makeMsg("hello"))
	if reply != nil {
		t.Error("expected no reply for non-matching channel")
	}

	// Discord message should match.
	msg := makeMsg("hello")
	msg.Channel = "discord"
	reply = e.Evaluate(context.Background(), msg)
	if reply == nil {
		t.Fatal("expected a reply for matching channel")
	}
}

func TestEvaluateSessionRestriction(t *testing.T) {
	t.Parallel()

	rule := enabledRule("r1", "vip", MatchAny, "", "VIP reply")
	rule.Sessions = []string{"vip-session"}

	e := NewEngine(baseConfig(rule))

	// Default session should not match.
	reply := e.Evaluate(context.Background(), makeMsg("hello"))
	if reply != nil {
		t.Error("expected no reply for non-matching session")
	}

	// VIP session should match.
	msg := makeMsg("hello")
	msg.SessionKey = "vip-session"
	reply = e.Evaluate(context.Background(), msg)
	if reply == nil {
		t.Fatal("expected a reply for matching session")
	}
}

func TestEvaluateCooldown(t *testing.T) {
	t.Parallel()

	rule := enabledRule("r1", "rate-limited", MatchAny, "", "auto reply")
	rule.Cooldown = time.Hour

	e := NewEngine(baseConfig(rule))

	// First evaluation should match.
	reply := e.Evaluate(context.Background(), makeMsg("first"))
	if reply == nil {
		t.Fatal("expected reply on first evaluation")
	}

	// Immediate second evaluation should be on cooldown.
	reply = e.Evaluate(context.Background(), makeMsg("second"))
	if reply != nil {
		t.Error("expected nil reply while on cooldown")
	}
}

func TestEvaluateDefaultCooldown(t *testing.T) {
	t.Parallel()

	rule := enabledRule("r1", "default-cd", MatchAny, "", "auto reply")
	// rule.Cooldown is zero, so engine default should apply.

	cfg := baseConfig(rule)
	cfg.DefaultCooldown = time.Hour

	e := NewEngine(cfg)

	reply := e.Evaluate(context.Background(), makeMsg("first"))
	if reply == nil {
		t.Fatal("expected reply on first evaluation")
	}

	reply = e.Evaluate(context.Background(), makeMsg("second"))
	if reply != nil {
		t.Error("expected nil reply due to default cooldown")
	}
}

func TestEvaluateTimeRestriction(t *testing.T) {
	t.Parallel()

	rule := enabledRule("r1", "business-hours", MatchAny, "", "We're open!")
	rule.ActiveFrom = "09:00"
	rule.ActiveUntil = "17:00"

	e := NewEngine(baseConfig(rule))

	// 14:30 is within business hours.
	msg := makeMsg("hello")
	msg.ReceivedAt = time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)
	reply := e.Evaluate(context.Background(), msg)
	if reply == nil {
		t.Fatal("expected reply during business hours")
	}

	// 20:00 is outside business hours.
	msg.ReceivedAt = time.Date(2025, 6, 15, 20, 0, 0, 0, time.UTC)
	reply = e.Evaluate(context.Background(), msg)
	if reply != nil {
		t.Error("expected no reply outside business hours")
	}
}

func TestEvaluateTimeRestrictionOvernight(t *testing.T) {
	t.Parallel()

	rule := enabledRule("r1", "night-shift", MatchAny, "", "Night shift active")
	rule.ActiveFrom = "22:00"
	rule.ActiveUntil = "06:00"

	e := NewEngine(baseConfig(rule))

	// 23:00 should match.
	msg := makeMsg("hello")
	msg.ReceivedAt = time.Date(2025, 6, 15, 23, 0, 0, 0, time.UTC)
	reply := e.Evaluate(context.Background(), msg)
	if reply == nil {
		t.Fatal("expected reply at 23:00 for overnight rule")
	}

	// 03:00 should match.
	msg.ReceivedAt = time.Date(2025, 6, 16, 3, 0, 0, 0, time.UTC)
	reply = e.Evaluate(context.Background(), msg)
	if reply == nil {
		t.Fatal("expected reply at 03:00 for overnight rule")
	}

	// 12:00 should not match.
	msg.ReceivedAt = time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
	reply = e.Evaluate(context.Background(), msg)
	if reply != nil {
		t.Error("expected no reply at 12:00 for overnight rule")
	}
}

func TestEvaluateDisabled(t *testing.T) {
	t.Parallel()

	rule := enabledRule("r1", "disabled", MatchAny, "", "should not fire")
	rule.Enabled = false

	e := NewEngine(baseConfig(rule))

	reply := e.Evaluate(context.Background(), makeMsg("hello"))
	if reply != nil {
		t.Error("disabled rule should not produce a reply")
	}
}

func TestEvaluateEngineDisabled(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(enabledRule("r1", "any", MatchAny, "", "reply"))
	cfg.Enabled = false

	e := NewEngine(cfg)

	reply := e.Evaluate(context.Background(), makeMsg("hello"))
	if reply != nil {
		t.Error("disabled engine should return nil")
	}
}

func TestEvaluateTemplateInterpolation(t *testing.T) {
	t.Parallel()

	e := NewEngine(baseConfig(
		enabledRule("r1", "greet", MatchAny, "", "Hello {{.SenderName}}, you said: {{.Content}}"),
	))

	msg := makeMsg("ping")
	msg.SenderName = "Bob"
	reply := e.Evaluate(context.Background(), msg)
	if reply == nil {
		t.Fatal("expected a reply")
	}

	want := "Hello Bob, you said: ping"
	if reply.Text != want {
		t.Errorf("got %q, want %q", reply.Text, want)
	}
}

func TestReload(t *testing.T) {
	t.Parallel()

	e := NewEngine(baseConfig(
		enabledRule("r1", "old", MatchExact, "hello", "old reply"),
	))

	reply := e.Evaluate(context.Background(), makeMsg("hello"))
	if reply == nil || reply.Text != "old reply" {
		t.Fatal("expected old reply before reload")
	}

	e.Reload(baseConfig(
		enabledRule("r2", "new", MatchExact, "hello", "new reply"),
	))

	reply = e.Evaluate(context.Background(), makeMsg("hello"))
	if reply == nil {
		t.Fatal("expected reply after reload")
	}
	if reply.Text != "new reply" {
		t.Errorf("got %q, want %q", reply.Text, "new reply")
	}
	if e.RuleCount() != 1 {
		t.Errorf("got rule count %d, want 1", e.RuleCount())
	}
}

func TestEvaluateNoRules(t *testing.T) {
	t.Parallel()

	e := NewEngine(baseConfig())
	reply := e.Evaluate(context.Background(), makeMsg("hello"))
	if reply != nil {
		t.Error("expected nil when no rules configured")
	}
}

func TestEvaluateMaxPerHour(t *testing.T) {
	t.Parallel()

	rule := enabledRule("r1", "limited", MatchAny, "", "reply")
	rule.MaxPerHour = 2
	rule.Cooldown = 0 // no per-message cooldown, only hourly limit

	cfg := baseConfig(rule)
	cfg.DefaultCooldown = 0
	e := NewEngine(cfg)

	// First two should succeed.
	for i := range 2 {
		reply := e.Evaluate(context.Background(), makeMsg("msg"))
		if reply == nil {
			t.Fatalf("expected reply on evaluation %d", i+1)
		}
	}

	// Third should be blocked by MaxPerHour.
	reply := e.Evaluate(context.Background(), makeMsg("msg"))
	if reply != nil {
		t.Error("expected nil reply after exceeding MaxPerHour")
	}
}

func TestEvaluateConcurrentReload(t *testing.T) {
	t.Parallel()

	e := NewEngine(baseConfig(
		enabledRule("r1", "initial", MatchExact, "hello", "initial reply"),
	))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			e.Reload(baseConfig(
				enabledRule("r2", "reloaded", MatchExact, "hello", "reloaded reply"),
			))
		}
	}()

	for range 100 {
		e.Evaluate(context.Background(), makeMsg("hello"))
	}
	<-done
}

func TestStartStopIdempotent(t *testing.T) {
	t.Parallel()

	e := NewEngine(baseConfig())
	e.Start()
	e.Start() // second Start should be no-op
	e.Stop()
	e.Stop() // second Stop should not panic
}
