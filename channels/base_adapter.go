package channels

import (
	"context"
	"strings"
	"sync"
)

// BaseAdapter provides a narrow lifecycle scaffold for channel adapters:
// connection status, optional cancellation, subscriber fan-out, and inbound
// publishing. It intentionally does not model protocol-specific auth,
// webhook parsing, or outbound delivery.
type BaseAdapter struct {
	name string

	mu          sync.Mutex
	status      Status
	cancel      context.CancelFunc
	subscribers map[chan InboundMessage]struct{}
}

func NewBaseAdapter(name string) BaseAdapter {
	return BaseAdapter{
		name:        strings.TrimSpace(name),
		status:      StatusDisconnected,
		subscribers: make(map[chan InboundMessage]struct{}),
	}
}

func (a *BaseAdapter) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureReadyLocked()
	return a.status
}

// SetStatus updates the adapter status without touching cancel state or
// subscriber lifecycle. Use MarkDisconnected for terminal teardown.
func (a *BaseAdapter) SetStatus(status Status) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureReadyLocked()
	if strings.TrimSpace(string(status)) == "" {
		status = StatusDisconnected
	}
	a.status = status
}

func (a *BaseAdapter) SubscribeEvents() <-chan InboundMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureReadyLocked()
	return SubscribeInbound(a.subscribers)
}

// MarkConnected transitions the adapter to connected and stores an optional
// cancel function for long-lived receive loops. Returns false when the
// adapter is already connected.
func (a *BaseAdapter) MarkConnected(cancel context.CancelFunc) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureReadyLocked()
	if a.status == StatusConnected {
		return false
	}
	a.status = StatusConnected
	a.cancel = cancel
	return true
}

// MarkDisconnected transitions the adapter to disconnected, closes all
// subscribers, and returns the previously registered cancel function.
func (a *BaseAdapter) MarkDisconnected() (context.CancelFunc, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureReadyLocked()
	if a.status == StatusDisconnected {
		return nil, false
	}
	a.status = StatusDisconnected
	cancel := a.cancel
	a.cancel = nil
	CloseInboundSubscribers(a.subscribers)
	return cancel, true
}

func (a *BaseAdapter) PublishInbound(msg InboundMessage, onDrop func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureReadyLocked()
	PublishInbound(a.subscribers, a.name, msg, onDrop)
}

func (a *BaseAdapter) ensureReadyLocked() {
	if strings.TrimSpace(string(a.status)) == "" {
		a.status = StatusDisconnected
	}
	if a.subscribers == nil {
		a.subscribers = make(map[chan InboundMessage]struct{})
	}
}
