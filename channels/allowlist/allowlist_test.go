package allowlist

import (
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Check logic tests
// ---------------------------------------------------------------------------

func TestCheck_AllowAll(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{Channel: "slack", AllowAll: true},
	})

	result := mgr.Check("slack", "user-1", "group-a")
	if !result.Allowed {
		t.Fatalf("expected allowed, got denied: %s", result.Reason)
	}
	if result.Reason != reasonAllowAll {
		t.Fatalf("expected reason %q, got %q", reasonAllowAll, result.Reason)
	}
}

func TestCheck_ExplicitAllowUser(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{Channel: "slack", AllowUsers: []string{"user-1", "user-2"}},
	})

	result := mgr.Check("slack", "user-1", "")
	if !result.Allowed {
		t.Fatalf("expected allowed, got denied: %s", result.Reason)
	}
	if result.Reason != reasonAllowedUser {
		t.Fatalf("expected reason %q, got %q", reasonAllowedUser, result.Reason)
	}
}

func TestCheck_ExplicitAllowGroup(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{Channel: "slack", AllowGroups: []string{"admins"}},
	})

	result := mgr.Check("slack", "user-1", "admins")
	if !result.Allowed {
		t.Fatalf("expected allowed, got denied: %s", result.Reason)
	}
	if result.Reason != reasonAllowedGroup {
		t.Fatalf("expected reason %q, got %q", reasonAllowedGroup, result.Reason)
	}
}

func TestCheck_DenyPrecedenceOverAllow(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{
			Channel:   "slack",
			AllowAll:  true,
			DenyUsers: []string{"blocked-user"},
		},
	})

	// Denied user should be blocked even with AllowAll.
	result := mgr.Check("slack", "blocked-user", "")
	if result.Allowed {
		t.Fatal("expected denied for blocked user with AllowAll")
	}
	if result.Reason != reasonDeniedUser {
		t.Fatalf("expected reason %q, got %q", reasonDeniedUser, result.Reason)
	}

	// Other users should still be allowed.
	result = mgr.Check("slack", "good-user", "")
	if !result.Allowed {
		t.Fatalf("expected allowed for non-blocked user, got denied: %s", result.Reason)
	}
}

func TestCheck_DenyGroupPrecedence(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{
			Channel:    "slack",
			AllowAll:   true,
			DenyGroups: []string{"banned-group"},
		},
	})

	result := mgr.Check("slack", "user-1", "banned-group")
	if result.Allowed {
		t.Fatal("expected denied for user in banned group")
	}
	if result.Reason != reasonDeniedGroup {
		t.Fatalf("expected reason %q, got %q", reasonDeniedGroup, result.Reason)
	}
}

func TestCheck_WildcardMatching(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{
			Channel:    "slack",
			AllowUsers: []string{"admin-*"},
			DenyUsers:  []string{"admin-temp-*"},
		},
	})

	// admin-john matches allow wildcard.
	result := mgr.Check("slack", "admin-john", "")
	if !result.Allowed {
		t.Fatalf("expected admin-john allowed, got denied: %s", result.Reason)
	}

	// admin-temp-1 matches deny wildcard (deny takes precedence).
	result = mgr.Check("slack", "admin-temp-1", "")
	if result.Allowed {
		t.Fatal("expected admin-temp-1 denied")
	}
	if result.Reason != reasonDeniedUser {
		t.Fatalf("expected reason %q, got %q", reasonDeniedUser, result.Reason)
	}

	// regular-user matches neither.
	result = mgr.Check("slack", "regular-user", "")
	if result.Allowed {
		t.Fatal("expected regular-user denied (not in allow list)")
	}
}

func TestCheck_WildcardGroup(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{
			Channel:     "slack",
			AllowGroups: []string{"team-*"},
		},
	})

	result := mgr.Check("slack", "user-1", "team-engineering")
	if !result.Allowed {
		t.Fatalf("expected allowed for team-engineering, got denied: %s", result.Reason)
	}

	result = mgr.Check("slack", "user-1", "other-group")
	if result.Allowed {
		t.Fatal("expected denied for other-group")
	}
}

func TestCheck_EmptyRulesDefaultDeny(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{Channel: "slack"}, // no allow lists, AllowAll is false
	})

	result := mgr.Check("slack", "user-1", "group-a")
	if result.Allowed {
		t.Fatal("expected default deny with empty rules")
	}
	if result.Reason != reasonDefaultDeny {
		t.Fatalf("expected reason %q, got %q", reasonDefaultDeny, result.Reason)
	}
}

func TestCheck_NoRulesForChannel(t *testing.T) {
	t.Parallel()
	mgr := NewManager(nil)

	result := mgr.Check("unknown-channel", "user-1", "")
	if result.Allowed {
		t.Fatal("expected denied for channel with no rules")
	}
	if result.Reason != reasonNoRules {
		t.Fatalf("expected reason %q, got %q", reasonNoRules, result.Reason)
	}
}

func TestCheck_MultipleChannels(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{Channel: "slack", AllowAll: true},
		{Channel: "discord", AllowUsers: []string{"user-1"}},
		{Channel: "telegram", AllowAll: false},
	})

	// slack: allow all.
	result := mgr.Check("slack", "anyone", "")
	if !result.Allowed {
		t.Fatalf("expected allowed on slack, got denied: %s", result.Reason)
	}

	// discord: only user-1.
	result = mgr.Check("discord", "user-1", "")
	if !result.Allowed {
		t.Fatalf("expected user-1 allowed on discord, got denied: %s", result.Reason)
	}
	result = mgr.Check("discord", "user-2", "")
	if result.Allowed {
		t.Fatal("expected user-2 denied on discord")
	}

	// telegram: no allow lists → default deny.
	result = mgr.Check("telegram", "user-1", "")
	if result.Allowed {
		t.Fatal("expected denied on telegram")
	}
}

func TestCheck_EmptyUserID(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{Channel: "slack", AllowUsers: []string{"*"}},
	})

	// Empty user ID should not match wildcard.
	result := mgr.Check("slack", "", "")
	if result.Allowed {
		t.Fatal("expected denied for empty user ID")
	}
}

// ---------------------------------------------------------------------------
// CRUD tests
// ---------------------------------------------------------------------------

func TestSetAndGetRules(t *testing.T) {
	t.Parallel()
	mgr := NewManager(nil)

	mgr.SetRules("slack", ChannelRules{AllowAll: true})
	rules, ok := mgr.GetRules("slack")
	if !ok {
		t.Fatal("expected rules for slack")
	}
	if !rules.AllowAll {
		t.Fatal("expected AllowAll to be true")
	}
	if rules.Channel != "slack" {
		t.Fatalf("expected channel %q, got %q", "slack", rules.Channel)
	}
}

func TestGetRules_NotFound(t *testing.T) {
	t.Parallel()
	mgr := NewManager(nil)

	_, ok := mgr.GetRules("nonexistent")
	if ok {
		t.Fatal("expected no rules for nonexistent channel")
	}
}

func TestGetRules_ReturnsCopy(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{Channel: "slack", AllowAll: true},
	})

	rules, _ := mgr.GetRules("slack")
	rules.AllowAll = false

	rules2, _ := mgr.GetRules("slack")
	if !rules2.AllowAll {
		t.Fatal("GetRules returned a reference instead of a copy")
	}
}

func TestListRules(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{Channel: "telegram"},
		{Channel: "discord"},
		{Channel: "slack"},
	})

	list := mgr.ListRules()
	if len(list) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(list))
	}
	// Should be sorted.
	if list[0].Channel != "discord" || list[1].Channel != "slack" || list[2].Channel != "telegram" {
		t.Fatalf("expected sorted order [discord, slack, telegram], got [%s, %s, %s]",
			list[0].Channel, list[1].Channel, list[2].Channel)
	}
}

func TestRemoveRules(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{Channel: "slack", AllowAll: true},
	})

	mgr.RemoveRules("slack")
	_, ok := mgr.GetRules("slack")
	if ok {
		t.Fatal("expected rules to be removed")
	}

	// Removing nonexistent channel is a no-op.
	mgr.RemoveRules("nonexistent")
}

// ---------------------------------------------------------------------------
// Concurrency test
// ---------------------------------------------------------------------------

func TestManager_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	mgr := NewManager([]ChannelRules{
		{Channel: "slack", AllowAll: true},
	})

	var wg sync.WaitGroup
	const goroutines = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			mgr.Check("slack", "user-1", "group-a")
			mgr.SetRules("slack", ChannelRules{AllowAll: idx%2 == 0})
			mgr.ListRules()
			mgr.GetRules("slack")
		}(i)
	}
	wg.Wait()
}
