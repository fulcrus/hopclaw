// Package allowlist controls which users and groups are permitted to interact
// with the agent on each channel. It supports explicit allow/deny lists and
// wildcard patterns using filepath.Match syntax.
package allowlist

import (
	"path/filepath"
	"sort"
	"sync"
)

// ---------------------------------------------------------------------------
// Sentinel reasons
// ---------------------------------------------------------------------------

const (
	reasonAllowAll     = "allow_all is enabled and user is not denied"
	reasonDeniedUser   = "user is in deny_users list"
	reasonDeniedGroup  = "group is in deny_groups list"
	reasonAllowedUser  = "user is in allow_users list"
	reasonAllowedGroup = "group is in allow_groups list"
	reasonDefaultDeny  = "user is not in any allow list"
	reasonNoRules      = "no rules configured for channel"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ChannelRules defines the access control rules for a single channel.
type ChannelRules struct {
	Channel     string   `json:"channel" yaml:"channel"`
	AllowAll    bool     `json:"allow_all" yaml:"allow_all"`
	AllowUsers  []string `json:"allow_users,omitempty" yaml:"allow_users"`
	DenyUsers   []string `json:"deny_users,omitempty" yaml:"deny_users"`
	AllowGroups []string `json:"allow_groups,omitempty" yaml:"allow_groups"`
	DenyGroups  []string `json:"deny_groups,omitempty" yaml:"deny_groups"`
}

// CheckResult reports whether an access check passed and why.
type CheckResult struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// ---------------------------------------------------------------------------
// Manager
// ---------------------------------------------------------------------------

// Manager controls which users/groups are allowed to interact with the agent
// on each channel. It supports explicit allow/deny lists and wildcard patterns.
type Manager struct {
	mu    sync.RWMutex
	rules map[string]*ChannelRules // channel name -> rules
}

// NewManager creates a Manager pre-loaded with the given rules.
func NewManager(rules []ChannelRules) *Manager {
	m := &Manager{
		rules: make(map[string]*ChannelRules, len(rules)),
	}
	for i := range rules {
		cp := rules[i]
		m.rules[cp.Channel] = &cp
	}
	return m
}

// Check determines whether userID in groupID is allowed on the given channel.
// Deny lists take precedence over allow lists. When AllowAll is true and the
// user is not denied, access is granted. When AllowAll is false, the user must
// appear in an allow list and must not appear in any deny list.
func (m *Manager) Check(channel, userID, groupID string) CheckResult {
	m.mu.RLock()
	r, ok := m.rules[channel]
	m.mu.RUnlock()

	if !ok {
		return CheckResult{Allowed: false, Reason: reasonNoRules}
	}

	// Deny takes precedence.
	if matchesAny(userID, r.DenyUsers) {
		return CheckResult{Allowed: false, Reason: reasonDeniedUser}
	}
	if matchesAny(groupID, r.DenyGroups) {
		return CheckResult{Allowed: false, Reason: reasonDeniedGroup}
	}

	// AllowAll short-circuit.
	if r.AllowAll {
		return CheckResult{Allowed: true, Reason: reasonAllowAll}
	}

	// Explicit allow lists.
	if matchesAny(userID, r.AllowUsers) {
		return CheckResult{Allowed: true, Reason: reasonAllowedUser}
	}
	if matchesAny(groupID, r.AllowGroups) {
		return CheckResult{Allowed: true, Reason: reasonAllowedGroup}
	}

	return CheckResult{Allowed: false, Reason: reasonDefaultDeny}
}

// SetRules adds or replaces the rules for a channel.
func (m *Manager) SetRules(channel string, rules ChannelRules) {
	cp := rules
	cp.Channel = channel
	m.mu.Lock()
	m.rules[channel] = &cp
	m.mu.Unlock()
}

// GetRules returns the rules for a channel. The second return value indicates
// whether rules exist for the channel.
func (m *Manager) GetRules(channel string) (*ChannelRules, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rules[channel]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

// ListRules returns a copy of all channel rules sorted by channel name.
func (m *Manager) ListRules() []ChannelRules {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]ChannelRules, 0, len(m.rules))
	for _, r := range m.rules {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Channel < out[j].Channel
	})
	return out
}

// RemoveRules deletes the rules for a channel.
func (m *Manager) RemoveRules(channel string) {
	m.mu.Lock()
	delete(m.rules, channel)
	m.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// matchesAny reports whether value matches any pattern in patterns. Patterns
// use filepath.Match syntax (e.g. "user-*", "admin-?"). An empty value never
// matches. Malformed patterns are silently skipped.
func matchesAny(value string, patterns []string) bool {
	if value == "" {
		return false
	}
	for _, p := range patterns {
		if matched, err := filepath.Match(p, value); err == nil && matched {
			return true
		}
	}
	return false
}
