package autoreply

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// timeOfDayFormat is the layout used to parse ActiveFrom / ActiveUntil.
const timeOfDayFormat = "15:04"

// ---------------------------------------------------------------------------
// Engine
// ---------------------------------------------------------------------------

// Engine evaluates incoming messages against configured rules and returns
// auto-replies. It is safe for concurrent use.
type Engine struct {
	config   Config
	matcher  *Matcher
	cooldown *CooldownTracker
	mu       sync.RWMutex // guards config for hot-reload
}

// NewEngine creates an Engine from the given configuration. Call Start before
// use so the cooldown tracker's background goroutine is running.
func NewEngine(cfg Config) *Engine {
	return &Engine{
		config:   cfg,
		matcher:  NewMatcher(),
		cooldown: NewCooldownTracker(),
	}
}

// Start launches background maintenance goroutines (e.g. cooldown cleanup).
func (e *Engine) Start() {
	e.cooldown.Start()
}

// Stop terminates background goroutines.
func (e *Engine) Stop() {
	e.cooldown.Stop()
}

// Evaluate checks an incoming message against all enabled rules and returns a
// Reply for the first matching rule that is not on cooldown. Returns nil if no
// rule matches or the engine is disabled.
func (e *Engine) Evaluate(_ context.Context, msg MessageContext) *Reply {
	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()

	if !cfg.Enabled {
		return nil
	}

	rules := sortedRules(cfg.Rules)

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if !channelAllowed(rule, msg.Channel) {
			continue
		}
		if !sessionAllowed(rule, msg.SessionKey) {
			continue
		}
		if !timeOfDayAllowed(rule, msg.ReceivedAt) {
			continue
		}
		if !e.matcher.Match(rule, msg.Content) {
			continue
		}

		cooldown := rule.Cooldown
		if cooldown <= 0 {
			cooldown = cfg.DefaultCooldown
		}
		if e.cooldown.IsOnCooldown(rule.ID, msg.SessionKey, cooldown) {
			continue
		}
		if e.cooldown.ExceedsMaxPerHour(rule.ID, msg.SessionKey, rule.MaxPerHour) {
			continue
		}

		e.cooldown.RecordFire(rule.ID, msg.SessionKey)

		return &Reply{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Text:     applyTemplate(rule.Response, msg),
		}
	}

	return nil
}

// Reload replaces the engine configuration. This is safe for concurrent use.
func (e *Engine) Reload(cfg Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = cfg
}

// RuleCount returns the number of configured rules.
func (e *Engine) RuleCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.config.Rules)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sortedRules returns a copy of rules sorted by priority (ascending).
func sortedRules(rules []Rule) []Rule {
	sorted := make([]Rule, len(rules))
	copy(sorted, rules)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	return sorted
}

// channelAllowed returns true if the rule has no channel restriction or the
// message channel is in the allowed set.
func channelAllowed(rule Rule, channel string) bool {
	if len(rule.Channels) == 0 {
		return true
	}
	for _, ch := range rule.Channels {
		if strings.EqualFold(ch, channel) {
			return true
		}
	}
	return false
}

// sessionAllowed returns true if the rule has no session restriction or the
// message session key is in the allowed set.
func sessionAllowed(rule Rule, sessionKey string) bool {
	if len(rule.Sessions) == 0 {
		return true
	}
	for _, s := range rule.Sessions {
		if s == sessionKey {
			return true
		}
	}
	return false
}

// timeOfDayAllowed returns true if the rule has no time restriction or the
// message timestamp falls within the active window.
func timeOfDayAllowed(rule Rule, ts time.Time) bool {
	if rule.ActiveFrom == "" && rule.ActiveUntil == "" {
		return true
	}

	nowMinutes := ts.Hour()*60 + ts.Minute()

	fromMinutes := 0
	if rule.ActiveFrom != "" {
		t, err := time.Parse(timeOfDayFormat, rule.ActiveFrom)
		if err != nil {
			return true // unparseable constraint is treated as "no restriction"
		}
		fromMinutes = t.Hour()*60 + t.Minute()
	}

	untilMinutes := 24 * 60 // end of day
	if rule.ActiveUntil != "" {
		t, err := time.Parse(timeOfDayFormat, rule.ActiveUntil)
		if err != nil {
			return true
		}
		untilMinutes = t.Hour()*60 + t.Minute()
	}

	if fromMinutes <= untilMinutes {
		// Normal range, e.g. 09:00 - 17:00
		return nowMinutes >= fromMinutes && nowMinutes < untilMinutes
	}
	// Overnight range, e.g. 22:00 - 06:00
	return nowMinutes >= fromMinutes || nowMinutes < untilMinutes
}
