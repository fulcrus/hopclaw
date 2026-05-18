package channels

import (
	"strings"
	"sync"
	"sync/atomic"
)

const DefaultSubscriberBuffer = 64

var subscriberDropCounters sync.Map

func RecordSubscriberDrop(adapter string) {
	adapter = strings.TrimSpace(adapter)
	if adapter == "" {
		adapter = "unknown"
	}
	counterAny, _ := subscriberDropCounters.LoadOrStore(adapter, &atomic.Uint64{})
	counterAny.(*atomic.Uint64).Add(1)
}

func SubscriberDropCount(adapter string) uint64 {
	adapter = strings.TrimSpace(adapter)
	if adapter == "" {
		return 0
	}
	counterAny, ok := subscriberDropCounters.Load(adapter)
	if !ok {
		return 0
	}
	return counterAny.(*atomic.Uint64).Load()
}
