package eventbus

import (
	"context"
	"testing"
)

type benchmarkSink struct{}

func (benchmarkSink) Handle(_ context.Context, _ Event) error {
	return nil
}

func BenchmarkInMemoryBusPublishParallel(b *testing.B) {
	limit := b.N + 1
	if limit < 1 {
		limit = 1
	}
	bus := NewInMemoryBusWithLimit(limit)
	bus.Subscribe(benchmarkSink{})
	ctx := context.Background()
	event := Event{Type: EventRunCompleted}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := bus.Publish(ctx, event); err != nil {
				b.Fatal(err)
			}
		}
	})
}
