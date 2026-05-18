package autoreply

import "time"

// ---------------------------------------------------------------------------
// Match modes
// ---------------------------------------------------------------------------

// MatchMode determines how a rule's pattern is matched.
type MatchMode string

const (
	// MatchExact performs a case-insensitive exact match.
	MatchExact MatchMode = "exact"
	// MatchPrefix performs a case-insensitive prefix match.
	MatchPrefix MatchMode = "prefix"
	// MatchContains performs a case-insensitive substring match.
	MatchContains MatchMode = "contains"
	// MatchRegex uses Go regexp for matching.
	MatchRegex MatchMode = "regex"
	// MatchAny matches every message (used for fallback rules).
	MatchAny MatchMode = "any"
)

// ---------------------------------------------------------------------------
// Rule
// ---------------------------------------------------------------------------

// Rule defines a single auto-reply rule.
type Rule struct {
	ID          string        `json:"id" yaml:"id"`
	Name        string        `json:"name" yaml:"name"`
	Enabled     bool          `json:"enabled" yaml:"enabled"`
	Priority    int           `json:"priority" yaml:"priority"` // lower = higher priority
	MatchMode   MatchMode     `json:"match_mode" yaml:"match_mode"`
	Pattern     string        `json:"pattern" yaml:"pattern"`                       // the pattern to match
	Response    string        `json:"response" yaml:"response"`                     // response template
	Channels    []string      `json:"channels,omitempty" yaml:"channels,omitempty"` // restrict to channels; empty = all
	Sessions    []string      `json:"sessions,omitempty" yaml:"sessions,omitempty"` // restrict to sessions; empty = all
	Cooldown    time.Duration `json:"cooldown,omitempty" yaml:"cooldown,omitempty"` // per-session cooldown
	MaxPerHour  int           `json:"max_per_hour,omitempty" yaml:"max_per_hour,omitempty"`
	ActiveFrom  string        `json:"active_from,omitempty" yaml:"active_from,omitempty"`   // "HH:MM" (24h)
	ActiveUntil string        `json:"active_until,omitempty" yaml:"active_until,omitempty"` // "HH:MM" (24h)
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// Config holds the auto-reply configuration.
type Config struct {
	Enabled         bool          `json:"enabled" yaml:"enabled"`
	DefaultCooldown time.Duration `json:"default_cooldown" yaml:"default_cooldown"`
	Rules           []Rule        `json:"rules" yaml:"rules"`
}

// ---------------------------------------------------------------------------
// Message context & reply
// ---------------------------------------------------------------------------

// MessageContext provides context about the incoming message for template
// interpolation.
type MessageContext struct {
	SenderID   string
	SenderName string
	Channel    string
	SessionKey string
	Content    string
	ReceivedAt time.Time
}

// Reply is the result of a successful rule match.
type Reply struct {
	RuleID   string
	RuleName string
	Text     string
}
