// Package manager provides a registry and lifecycle controller for
// channel adapters. The gateway uses the manager to route inbound
// events to the runtime and outbound messages to the correct adapter.
package manager

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/fulcrus/hopclaw/channels"
)

// Manager tracks registered channel adapters and their lifecycle.
type Manager struct {
	mu       sync.RWMutex
	adapters map[string]channels.Adapter
}

type ConnectReport struct {
	Connected []string
	Failed    map[string]error
}

func (r ConnectReport) IsConnected(name string) bool {
	for _, connected := range r.Connected {
		if connected == name {
			return true
		}
	}
	return false
}

func (r ConnectReport) HasFailures() bool {
	return len(r.Failed) > 0
}

// New creates an empty channel manager.
func New() *Manager {
	return &Manager{
		adapters: make(map[string]channels.Adapter),
	}
}

// Register adds a named adapter. Returns an error if the name is already taken.
func (m *Manager) Register(name string, adapter channels.Adapter) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.adapters[name]; exists {
		return fmt.Errorf("channel %q already registered", name)
	}
	m.adapters[name] = adapter
	return nil
}

// Get returns a registered adapter by name.
func (m *Manager) Get(name string) (channels.Adapter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.adapters[name]
	return a, ok
}

// ConnectAll connects all registered adapters and keeps going when one fails.
func (m *Manager) ConnectAll(ctx context.Context) ConnectReport {
	m.mu.RLock()
	adapters := make(map[string]channels.Adapter, len(m.adapters))
	for name, adapter := range m.adapters {
		adapters[name] = adapter
	}
	m.mu.RUnlock()
	names := make([]string, 0, len(adapters))
	for name := range adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	report := ConnectReport{
		Connected: make([]string, 0, len(names)),
		Failed:    make(map[string]error),
	}
	for _, name := range names {
		adapter := adapters[name]
		if err := adapter.Connect(ctx); err != nil {
			report.Failed[name] = fmt.Errorf("connect channel %q: %w", name, err)
			continue
		}
		report.Connected = append(report.Connected, name)
	}
	if len(report.Failed) == 0 {
		report.Failed = nil
	}
	return report
}

// DisconnectAll gracefully disconnects all adapters.
func (m *Manager) DisconnectAll(ctx context.Context) error {
	m.mu.RLock()
	adapters := make(map[string]channels.Adapter, len(m.adapters))
	for name, adapter := range m.adapters {
		adapters[name] = adapter
	}
	m.mu.RUnlock()
	var firstErr error
	for name, adapter := range adapters {
		if err := adapter.Disconnect(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("disconnect channel %q: %w", name, err)
		}
	}
	return firstErr
}

// Names returns the list of registered adapter names.
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.adapters))
	for name := range m.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Unregister removes a named adapter from the manager. Returns the removed
// adapter and true if it existed, nil and false otherwise.
func (m *Manager) Unregister(name string) (channels.Adapter, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	adapter, ok := m.adapters[name]
	if !ok {
		return nil, false
	}
	delete(m.adapters, name)
	return adapter, true
}

// Replace atomically swaps one adapter for another. The old adapter is
// returned so the caller can disconnect it. Returns an error if the name
// is not currently registered.
func (m *Manager) Replace(name string, adapter channels.Adapter) (channels.Adapter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.adapters[name]
	if !ok {
		return nil, fmt.Errorf("channel %q not registered", name)
	}
	m.adapters[name] = adapter
	return old, nil
}
