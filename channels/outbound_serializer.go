package channels

import "sync"

// OutboundSerializer provides one shared ordering gate for outbound channel
// operations that must not race on the underlying transport.
type OutboundSerializer struct {
	mu sync.Mutex
}

func NewOutboundSerializer() *OutboundSerializer {
	return &OutboundSerializer{}
}

func (s *OutboundSerializer) Do(fn func() error) error {
	if fn == nil {
		return nil
	}
	if s == nil {
		return fn()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn()
}
