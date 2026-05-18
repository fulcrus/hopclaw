package channels

import "strings"

// SubscribeInbound registers a buffered subscriber channel in the caller-owned
// subscriber map. Callers are responsible for holding any required locks.
func SubscribeInbound(subscribers map[chan InboundMessage]struct{}) chan InboundMessage {
	ch := make(chan InboundMessage, DefaultSubscriberBuffer)
	subscribers[ch] = struct{}{}
	return ch
}

// CloseInboundSubscribers closes and removes every subscriber channel in the
// caller-owned subscriber map. Callers are responsible for holding any
// required locks.
func CloseInboundSubscribers(subscribers map[chan InboundMessage]struct{}) {
	for ch := range subscribers {
		close(ch)
		delete(subscribers, ch)
	}
}

// PublishInbound delivers msg to every registered subscriber. Callers are
// responsible for holding any required locks.
func PublishInbound(subscribers map[chan InboundMessage]struct{}, adapter string, msg InboundMessage, onDrop func()) {
	adapter = strings.TrimSpace(adapter)
	for ch := range subscribers {
		select {
		case ch <- msg:
		default:
			RecordSubscriberDrop(adapter)
			if onDrop != nil {
				onDrop()
			}
		}
	}
}
