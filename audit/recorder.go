package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
)

type InMemoryRecorder struct {
	mu     sync.RWMutex
	events []eventbus.Event
}

func NewInMemoryRecorder() *InMemoryRecorder {
	return &InMemoryRecorder{}
}

func (r *InMemoryRecorder) Handle(_ context.Context, event eventbus.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, cloneEvent(event))
	return nil
}

func (r *InMemoryRecorder) Snapshot() []eventbus.Event {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]eventbus.Event, len(r.events))
	for i, event := range r.events {
		out[i] = cloneEvent(event)
	}
	return out
}

type JSONLRecorder struct {
	mu   sync.Mutex
	Path string
}

func (r *JSONLRecorder) Handle(_ context.Context, event eventbus.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(r.Path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(r.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func cloneEvent(in eventbus.Event) eventbus.Event {
	out := in
	if in.Attrs != nil {
		out.Attrs = make(map[string]any, len(in.Attrs))
		for k, v := range in.Attrs {
			out.Attrs[k] = v
		}
	}
	return out
}
