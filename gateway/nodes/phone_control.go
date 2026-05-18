package nodes

import (
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Phone Control Gate — Permission management for sensitive mobile operations
// ---------------------------------------------------------------------------

const defaultArmDuration = 10 * time.Minute

// PhoneControlGate manages temporary authorization for sensitive phone operations.
type PhoneControlGate struct {
	mu    sync.Mutex
	armed map[string]armState
}

type armState struct {
	ArmedAt   time.Time `json:"armed_at"`
	ExpiresAt time.Time `json:"expires_at"`
	ArmedBy   string    `json:"armed_by"`
}

// sensitiveGroups maps group names to their sensitive commands.
var sensitiveGroups = map[string][]string{
	"camera": {"camera.snap", "camera.clip"},
	"screen": {"screen.record"},
	"writes": {"calendar.add", "contacts.add", "reminders.add", "sms.send"},
}

// NewPhoneControlGate creates a new phone control gate.
func NewPhoneControlGate() *PhoneControlGate {
	return &PhoneControlGate{armed: make(map[string]armState)}
}

// Arm authorizes a group of operations for a limited duration.
func (g *PhoneControlGate) Arm(group, armedBy string, duration time.Duration) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if duration == 0 {
		duration = defaultArmDuration
	}

	g.armed[group] = armState{
		ArmedAt:   time.Now(),
		ExpiresAt: time.Now().Add(duration),
		ArmedBy:   armedBy,
	}
	return nil
}

// Disarm revokes authorization for a group.
func (g *PhoneControlGate) Disarm(group string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.armed, group)
}

// IsAllowed checks if a command is currently authorized.
// Non-sensitive commands are always allowed.
func (g *PhoneControlGate) IsAllowed(command string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	for group, cmds := range sensitiveGroups {
		for _, c := range cmds {
			if c == command {
				state, ok := g.armed[group]
				if !ok || time.Now().After(state.ExpiresAt) {
					return false
				}
				return true
			}
		}
	}
	return true // non-sensitive commands are always allowed
}

// Status returns the current arm status for all groups.
func (g *PhoneControlGate) Status() map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()

	result := make(map[string]any, len(sensitiveGroups))
	now := time.Now()
	for group := range sensitiveGroups {
		state, ok := g.armed[group]
		if ok && now.Before(state.ExpiresAt) {
			result[group] = map[string]any{
				"armed":      true,
				"armed_by":   state.ArmedBy,
				"expires_at": state.ExpiresAt,
				"remaining":  state.ExpiresAt.Sub(now).Seconds(),
			}
		} else {
			result[group] = map[string]any{"armed": false}
		}
	}
	return result
}
