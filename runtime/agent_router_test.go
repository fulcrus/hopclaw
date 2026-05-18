package runtime

import (
	"testing"
)

// ---------------------------------------------------------------------------
// NewAgentRouter
// ---------------------------------------------------------------------------

func TestNewAgentRouterEmpty(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter(nil)
	if r == nil {
		t.Fatal("NewAgentRouter returned nil")
	}
	if len(r.List()) != 0 {
		t.Fatalf("expected 0 profiles, got %d", len(r.List()))
	}
}

func TestNewAgentRouterSkipsEmptyName(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter([]AgentProfile{
		{Name: ""},
		{Name: "  "},
		{Name: "valid", Description: "ok"},
	})
	list := r.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(list))
	}
	if list[0].Name != "valid" {
		t.Fatalf("name = %q, want valid", list[0].Name)
	}
}

// ---------------------------------------------------------------------------
// Resolve
// ---------------------------------------------------------------------------

func TestResolveAgentSessionKey(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter([]AgentProfile{
		{Name: "sales", Model: "gpt-4", SystemPrompt: "You are a sales bot."},
	})

	name, inner, profile := r.Resolve("agent:sales:user123")
	if name != "sales" {
		t.Fatalf("agentName = %q, want sales", name)
	}
	if inner != "user123" {
		t.Fatalf("innerKey = %q, want user123", inner)
	}
	if profile == nil {
		t.Fatal("expected non-nil profile")
	}
	if profile.Model != "gpt-4" {
		t.Fatalf("profile.Model = %q, want gpt-4", profile.Model)
	}
	if profile.SystemPrompt != "You are a sales bot." {
		t.Fatalf("profile.SystemPrompt = %q", profile.SystemPrompt)
	}
}

func TestResolveMultiColonInnerKey(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter([]AgentProfile{
		{Name: "support", Description: "support agent"},
	})

	name, inner, profile := r.Resolve("agent:support:slack:dm:user456")
	if name != "support" {
		t.Fatalf("agentName = %q, want support", name)
	}
	if inner != "slack:dm:user456" {
		t.Fatalf("innerKey = %q, want slack:dm:user456", inner)
	}
	if profile == nil {
		t.Fatal("expected non-nil profile")
	}
}

func TestResolvePlainSessionKey(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter([]AgentProfile{
		{Name: "sales"},
	})

	name, inner, profile := r.Resolve("slack:channel123")
	if name != "" {
		t.Fatalf("agentName = %q, want empty", name)
	}
	if inner != "slack:channel123" {
		t.Fatalf("innerKey = %q, want slack:channel123", inner)
	}
	if profile != nil {
		t.Fatal("expected nil profile for plain key")
	}
}

func TestResolveUnknownAgent(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter([]AgentProfile{
		{Name: "sales"},
	})

	name, inner, profile := r.Resolve("agent:unknown:user1")
	if name != "" {
		t.Fatalf("agentName = %q, want empty", name)
	}
	if inner != "agent:unknown:user1" {
		t.Fatalf("innerKey = %q, want agent:unknown:user1", inner)
	}
	if profile != nil {
		t.Fatal("expected nil profile for unknown agent")
	}
}

func TestResolveTooFewParts(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter([]AgentProfile{
		{Name: "sales"},
	})

	// "agent:sales" has only 2 parts — no inner key.
	name, inner, profile := r.Resolve("agent:sales")
	if name != "" {
		t.Fatalf("agentName = %q, want empty", name)
	}
	if inner != "agent:sales" {
		t.Fatalf("innerKey = %q, want agent:sales", inner)
	}
	if profile != nil {
		t.Fatal("expected nil profile for incomplete key")
	}
}

func TestResolveEmptyKey(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter(nil)

	name, inner, profile := r.Resolve("")
	if name != "" {
		t.Fatalf("agentName = %q, want empty", name)
	}
	if inner != "" {
		t.Fatalf("innerKey = %q, want empty", inner)
	}
	if profile != nil {
		t.Fatal("expected nil profile for empty key")
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestListReturnsSorted(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter([]AgentProfile{
		{Name: "zebra"},
		{Name: "alpha"},
		{Name: "middle"},
	})

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(list))
	}
	if list[0].Name != "alpha" {
		t.Fatalf("list[0].Name = %q, want alpha", list[0].Name)
	}
	if list[1].Name != "middle" {
		t.Fatalf("list[1].Name = %q, want middle", list[1].Name)
	}
	if list[2].Name != "zebra" {
		t.Fatalf("list[2].Name = %q, want zebra", list[2].Name)
	}
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestGetExisting(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter([]AgentProfile{
		{Name: "sales", Model: "gpt-4", MaxTokens: 2000},
	})

	p, ok := r.Get("sales")
	if !ok {
		t.Fatal("expected ok=true for existing profile")
	}
	if p.Name != "sales" {
		t.Fatalf("p.Name = %q", p.Name)
	}
	if p.Model != "gpt-4" {
		t.Fatalf("p.Model = %q", p.Model)
	}
	if p.MaxTokens != 2000 {
		t.Fatalf("p.MaxTokens = %d", p.MaxTokens)
	}
}

func TestGetNonExisting(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter(nil)

	p, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected ok=false for non-existent profile")
	}
	if p != nil {
		t.Fatal("expected nil profile")
	}
}

func TestGetReturnsCopy(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter([]AgentProfile{
		{Name: "sales", Model: "gpt-4"},
	})

	p1, _ := r.Get("sales")
	p1.Model = "mutated"

	p2, _ := r.Get("sales")
	if p2.Model != "gpt-4" {
		t.Fatalf("Get should return a copy; got Model = %q after mutation", p2.Model)
	}
}

// ---------------------------------------------------------------------------
// Profile with Tools and Skills
// ---------------------------------------------------------------------------

func TestResolveProfileWithToolsAndSkills(t *testing.T) {
	t.Parallel()
	r := NewAgentRouter([]AgentProfile{
		{
			Name:   "restricted",
			Tools:  []string{"fs.read", "fs.write"},
			Skills: []string{"summarize"},
		},
	})

	_, _, profile := r.Resolve("agent:restricted:user1")
	if profile == nil {
		t.Fatal("expected non-nil profile")
	}
	if len(profile.Tools) != 2 {
		t.Fatalf("tools count = %d, want 2", len(profile.Tools))
	}
	if len(profile.Skills) != 1 {
		t.Fatalf("skills count = %d, want 1", len(profile.Skills))
	}
}
