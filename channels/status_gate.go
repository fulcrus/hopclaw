package channels

import (
	"sync"
	"time"
)

const DefaultAuthFailureCooldown = 30 * time.Second
const DefaultAuthFailureReminderInterval = 15 * time.Second

type AuthFailureGate struct {
	cooldown time.Duration
	reminder time.Duration

	mu    sync.Mutex
	items map[string]authFailureItem
}

type authFailureItem struct {
	until      time.Time
	lastNotice time.Time
}

func NewAuthFailureGate(cooldown, reminder time.Duration) *AuthFailureGate {
	if cooldown <= 0 {
		cooldown = DefaultAuthFailureCooldown
	}
	if reminder <= 0 {
		reminder = DefaultAuthFailureReminderInterval
	}
	return &AuthFailureGate{
		cooldown: cooldown,
		reminder: reminder,
		items:    make(map[string]authFailureItem),
	}
}

func (g *AuthFailureGate) Arm(key string) {
	if g == nil || stringsTrim(key) == "" {
		return
	}
	now := time.Now()
	g.mu.Lock()
	g.items[key] = authFailureItem{
		until: now.Add(g.cooldown),
	}
	g.mu.Unlock()
}

func (g *AuthFailureGate) Clear(key string) {
	if g == nil || stringsTrim(key) == "" {
		return
	}
	g.mu.Lock()
	delete(g.items, key)
	g.mu.Unlock()
}

func (g *AuthFailureGate) Blocked(key string) (bool, bool) {
	if g == nil || stringsTrim(key) == "" {
		return false, false
	}
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	item, ok := g.items[key]
	if !ok {
		return false, false
	}
	if !item.until.After(now) {
		delete(g.items, key)
		return false, false
	}
	notify := item.lastNotice.IsZero() || now.Sub(item.lastNotice) >= g.reminder
	if notify {
		item.lastNotice = now
		g.items[key] = item
	}
	return true, notify
}
