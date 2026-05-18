package channels

import (
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func TestOutboundSerializerSerializesConcurrentCalls(t *testing.T) {
	t.Parallel()

	serializer := NewOutboundSerializer()
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	startedSecond := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32

	run := func() error {
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		return nil
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = serializer.Do(run)
	}()
	<-entered

	go func() {
		defer wg.Done()
		close(startedSecond)
		_ = serializer.Do(run)
	}()
	<-startedSecond

	for i := 0; i < 1000; i++ {
		goruntime.Gosched()
	}
	close(release)
	wg.Wait()

	if got := maxActive.Load(); got != 1 {
		t.Fatalf("max concurrent outbound calls = %d, want 1", got)
	}
}

func TestOutboundSerializerNilSafe(t *testing.T) {
	t.Parallel()

	called := false
	var serializer *OutboundSerializer
	if err := serializer.Do(func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if !called {
		t.Fatal("expected nil serializer to execute function")
	}
}
