package agent

import (
	"context"
	"log/slog"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
)

// EventBusStreamCallback bridges StreamCallback to eventbus events.
type EventBusStreamCallback struct {
	Bus            eventbus.Bus
	RunID          string
	SessionID      string
	ExtraAttrs     map[string]any
	SkipCompletion bool
}

func (cb *EventBusStreamCallback) OnTextDelta(ctx context.Context, delta string) {
	if cb.Bus == nil {
		return
	}
	attrs := mergeStreamEventAttrs(cb.ExtraAttrs, eventbus.DeltaAttrs{Delta: delta}.ToMap())
	if traceID := logging.TraceIDFromContext(ctx); traceID != "" {
		attrs[logging.AttrKeyTraceID] = traceID
	}
	logging.LogIfErr(ctx, cb.Bus.Publish(ctx, eventbus.NewModelTextDeltaEvent(
		cb.RunID,
		cb.SessionID,
		eventbus.DeltaAttrs{Delta: delta},
		attrs,
	)), "emit event failed", slog.String("kind", string(eventbus.EventModelTextDelta)))
}

func (cb *EventBusStreamCallback) OnReasoningDelta(ctx context.Context, delta string) {
	if cb.Bus == nil {
		return
	}
	attrs := mergeStreamEventAttrs(cb.ExtraAttrs, eventbus.DeltaAttrs{Delta: delta}.ToMap())
	if traceID := logging.TraceIDFromContext(ctx); traceID != "" {
		attrs[logging.AttrKeyTraceID] = traceID
	}
	logging.LogIfErr(ctx, cb.Bus.Publish(ctx, eventbus.NewModelReasoningDeltaEvent(
		cb.RunID,
		cb.SessionID,
		eventbus.DeltaAttrs{Delta: delta},
		attrs,
	)), "emit event failed", slog.String("kind", string(eventbus.EventModelReasoningDelta)))
}

func (cb *EventBusStreamCallback) OnToolCallStart(_ context.Context, _, _ string) {
	// No-op: tool calls are already tracked via EventToolExecuted.
}

func (cb *EventBusStreamCallback) OnToolCallDelta(_ context.Context, _, _ string) {
	// No-op: tool calls are already tracked via EventToolExecuted.
}

func (cb *EventBusStreamCallback) OnComplete(ctx context.Context) {
	if cb.Bus == nil || cb.SkipCompletion {
		return
	}
	attrs := cloneMap(cb.ExtraAttrs)
	if traceID := logging.TraceIDFromContext(ctx); traceID != "" {
		if attrs == nil {
			attrs = make(map[string]any, 1)
		}
		attrs[logging.AttrKeyTraceID] = traceID
	}
	logging.LogIfErr(ctx, cb.Bus.Publish(ctx, eventbus.NewModelStreamCompleteEvent(
		cb.RunID,
		cb.SessionID,
		eventbus.ModelStreamCompleteAttrs{},
		attrs,
	)), "emit event failed", slog.String("kind", string(eventbus.EventModelStreamComplete)))
}

func (cb *EventBusStreamCallback) OnError(_ context.Context, _ error) {
	// No-op: errors are handled by the caller.
}

func mergeStreamEventAttrs(base map[string]any, payload map[string]any) map[string]any {
	merged := cloneMap(base)
	if merged == nil {
		merged = make(map[string]any, len(payload))
	}
	for key, value := range payload {
		merged[key] = value
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}
