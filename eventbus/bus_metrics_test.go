package eventbus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestInMemoryBusPublishRecordsMetrics(t *testing.T) {
	bus := NewInMemoryBus()
	eventType := EventType("observability." + strings.ReplaceAll(t.Name(), "/", "_"))

	beforePublished := testutil.ToFloat64(metrics.EventBusPublished.WithLabelValues(string(eventType)))
	if err := bus.Publish(context.Background(), Event{Type: eventType}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	afterPublished := testutil.ToFloat64(metrics.EventBusPublished.WithLabelValues(string(eventType)))
	if afterPublished <= beforePublished {
		t.Fatalf("published counter = %v, want > %v", afterPublished, beforePublished)
	}

	if depth := testutil.ToFloat64(metrics.EventBusQueueDepth); depth < 1 {
		t.Fatalf("queue depth = %v, want >= 1", depth)
	}
}

func TestInMemoryBusPublishRecordsSinkErrorMetricAndSubscriberDrops(t *testing.T) {
	bus := NewInMemoryBus()
	eventType := EventType("observability." + strings.ReplaceAll(t.Name(), "/", "_"))
	sinkLabel := "*eventbus.failingSink"

	sub := bus.SubscribeChannel(1)
	beforeDropped := testutil.ToFloat64(metrics.EventBusSubscriberDropped.WithLabelValues(string(eventType)))
	if err := bus.Publish(context.Background(), Event{Type: eventType}); err != nil {
		t.Fatalf("Publish(first) error = %v", err)
	}
	if err := bus.Publish(context.Background(), Event{Type: eventType}); err != nil {
		t.Fatalf("Publish(second) error = %v", err)
	}
	afterDropped := testutil.ToFloat64(metrics.EventBusSubscriberDropped.WithLabelValues(string(eventType)))
	if afterDropped <= beforeDropped {
		t.Fatalf("subscriber dropped counter = %v, want > %v", afterDropped, beforeDropped)
	}
	if got := sub.DroppedCount(); got == 0 {
		t.Fatal("DroppedCount() = 0, want tracked drop")
	}

	wantErr := errors.New("sink metric failure")
	bus.Subscribe(&failingSink{err: wantErr})
	beforeSinkErr := testutil.ToFloat64(metrics.EventBusSinkErrors.WithLabelValues(sinkLabel, string(eventType)))
	err := bus.Publish(context.Background(), Event{Type: eventType})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Publish(third) error = %v, want wrapped %v", err, wantErr)
	}
	afterSinkErr := testutil.ToFloat64(metrics.EventBusSinkErrors.WithLabelValues(sinkLabel, string(eventType)))
	if afterSinkErr <= beforeSinkErr {
		t.Fatalf("sink error counter = %v, want > %v", afterSinkErr, beforeSinkErr)
	}
}
