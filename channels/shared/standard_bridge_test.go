package shared

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

type standardBridgeAdapter struct {
	inbound chan channels.InboundMessage

	mu   sync.Mutex
	sent []channels.OutboundMessage
}

func (a *standardBridgeAdapter) Connect(context.Context) error    { return nil }
func (a *standardBridgeAdapter) Disconnect(context.Context) error { return nil }
func (a *standardBridgeAdapter) Send(_ context.Context, msg channels.OutboundMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, msg)
	return nil
}
func (a *standardBridgeAdapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{SendText: true, ReceiveMessage: true, ReceiveEvent: true}
}
func (a *standardBridgeAdapter) Status() channels.Status                         { return channels.StatusConnected }
func (a *standardBridgeAdapter) SubscribeEvents() <-chan channels.InboundMessage { return a.inbound }
func (a *standardBridgeAdapter) Messages() []channels.OutboundMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]channels.OutboundMessage, len(a.sent))
	copy(out, a.sent)
	return out
}

type standardBridgeStreamingAdapter struct {
	*standardBridgeAdapter

	streamMu    sync.Mutex
	beginCount  int
	updateCount int
	endCount    int
	contents    []string
}

func newStandardBridgeStreamingAdapter() *standardBridgeStreamingAdapter {
	return &standardBridgeStreamingAdapter{
		standardBridgeAdapter: &standardBridgeAdapter{inbound: make(chan channels.InboundMessage, 1)},
	}
}

func (a *standardBridgeStreamingAdapter) BeginStreaming(_ context.Context, _ channels.OutboundMessage) (string, error) {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	a.beginCount++
	return "stream-1", nil
}

func (a *standardBridgeStreamingAdapter) UpdateStreaming(_ context.Context, _ string, content string) error {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	a.updateCount++
	a.contents = append(a.contents, content)
	return nil
}

func (a *standardBridgeStreamingAdapter) EndStreaming(_ context.Context, _ string, final channels.OutboundMessage) error {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	a.endCount++
	a.contents = append(a.contents, final.Content)
	return nil
}

func TestStandardBridgeDeliveredStatePrunesStaleEntries(t *testing.T) {
	t.Parallel()

	bridge := NewStandardBridge(StandardBridgeConfig{ChannelName: "testbridge"}, nil, nil, nil, nil, channels.DefaultStatusReminderDelay)
	now := time.Now().UTC()

	bridge.mu.Lock()
	bridge.delivered["run-stale"] = now.Add(-channels.BridgeDeliveredStateTTL - time.Minute)
	bridge.delivered["run-fresh"] = now
	bridge.mu.Unlock()

	if bridge.isDelivered("run-stale") {
		t.Fatal("isDelivered(stale) = true, want false after pruning")
	}
	if !bridge.isDelivered("run-fresh") {
		t.Fatal("isDelivered(fresh) = false, want true")
	}

	bridge.mu.Lock()
	_, staleExists := bridge.delivered["run-stale"]
	_, freshExists := bridge.delivered["run-fresh"]
	bridge.mu.Unlock()
	if staleExists {
		t.Fatal("stale delivered state should be pruned")
	}
	if !freshExists {
		t.Fatal("fresh delivered state should be retained")
	}
}

func TestStandardBridgeEnsureStreamingStatePrunesStaleEntries(t *testing.T) {
	t.Parallel()

	adapter := newStandardBridgeStreamingAdapter()
	bridge := NewStandardBridge(StandardBridgeConfig{ChannelName: "testbridge"}, adapter, nil, nil, nil, channels.DefaultStatusReminderDelay)
	now := time.Now().UTC()

	bridge.mu.Lock()
	bridge.streams["run-stale"] = &channels.StreamingDeliveryState{
		Handle:    "stream-stale",
		UpdatedAt: now.Add(-channels.BridgeStreamingStateTTL - time.Minute),
	}
	bridge.mu.Unlock()

	state := bridge.ensureStreamingState(context.Background(), adapter, "run-fresh", channels.RunNotificationTarget{
		ChannelID:    "testbridge",
		TargetID:     "chat-1",
		ReplyToID:    "msg-1",
		InputContent: "stream please",
		Format:       "text",
		Metadata:     map[string]any{"k": "v"},
	})
	if state == nil {
		t.Fatal("ensureStreamingState() = nil")
	}
	if state.Handle != "stream-1" {
		t.Fatalf("state.Handle = %q, want %q", state.Handle, "stream-1")
	}
	if state.UpdatedAt.IsZero() {
		t.Fatal("state.UpdatedAt should be set")
	}

	bridge.mu.Lock()
	_, staleExists := bridge.streams["run-stale"]
	_, freshExists := bridge.streams["run-fresh"]
	bridge.mu.Unlock()
	if staleExists {
		t.Fatal("stale streaming state should be pruned")
	}
	if !freshExists {
		t.Fatal("fresh streaming state should be stored")
	}
	adapter.streamMu.Lock()
	beginCount := adapter.beginCount
	adapter.streamMu.Unlock()
	if beginCount != 1 {
		t.Fatalf("BeginStreaming() count = %d, want 1", beginCount)
	}
}

type standardBridgeRuntime struct {
	mu sync.Mutex

	interactionReq *runtimesvc.InteractionRequest
	run            *agent.Run
	runResult      *runtimesvc.RunResult
	verification   *verifyrt.RunVerification
}

func (r *standardBridgeRuntime) Interact(_ context.Context, req runtimesvc.InteractionRequest) (*runtimesvc.InteractionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyReq := req
	r.interactionReq = &copyReq
	return &runtimesvc.InteractionResult{
		Decision: runtimesvc.InteractionDecision{ReplyAct: runtimesvc.ReplyActTaskAccept},
		Run:      r.run,
	}, nil
}

func (r *standardBridgeRuntime) Submit(_ context.Context, _ runtimesvc.SubmitRequest) (*agent.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.run, nil
}

func (r *standardBridgeRuntime) GetRun(_ context.Context, _ string) (*agent.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyRun := *r.run
	return &copyRun, nil
}

func (r *standardBridgeRuntime) GetApproval(context.Context, string) (*approval.Ticket, error) {
	return nil, nil
}

func (r *standardBridgeRuntime) FindPendingApproval(context.Context, string) (*approval.Ticket, error) {
	return nil, nil
}

func (r *standardBridgeRuntime) ResolveApproval(context.Context, string, approval.Resolution) (*approval.Ticket, error) {
	return nil, nil
}

func (r *standardBridgeRuntime) CancelRun(context.Context, string) (*agent.Run, error) {
	return nil, nil
}

func (r *standardBridgeRuntime) GetArtifact(context.Context, string) (*artifact.Blob, error) {
	return nil, nil
}

func (r *standardBridgeRuntime) GetRunResult(context.Context, string) (*runtimesvc.RunResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runResult, nil
}

func (r *standardBridgeRuntime) GetRunVerification(context.Context, string) (*verifyrt.RunVerification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.verification, nil
}

func (r *standardBridgeRuntime) InteractionRequest() *runtimesvc.InteractionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.interactionReq == nil {
		return nil
	}
	copyReq := *r.interactionReq
	return &copyReq
}

func TestStandardBridgeHandleInboundMessageUsesConfiguredKeys(t *testing.T) {
	t.Parallel()

	adapter := &standardBridgeAdapter{inbound: make(chan channels.InboundMessage, 1)}
	runtime := &standardBridgeRuntime{run: &agent.Run{ID: "run-1"}}
	bridge := NewStandardBridge(StandardBridgeConfig{
		ChannelName:      "testbridge",
		TargetIDKey:      "chat_id",
		MessageIDKey:     "message_id",
		DirectUsesChatID: true,
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		SenderID:   "user-1",
		SenderName: "Alice",
		Content:    "hello bridge",
		RawEvent: map[string]any{
			"chat_id":    "chat-1",
			"message_id": "msg-1",
		},
	})

	req := runtime.InteractionRequest()
	if req == nil {
		t.Fatal("Interact() was not called")
	}
	if req.SessionKey != "testbridge:chat-1" {
		t.Fatalf("SessionKey = %q, want %q", req.SessionKey, "testbridge:chat-1")
	}
	if req.ExternalEventID != "msg-1" {
		t.Fatalf("ExternalEventID = %q, want %q", req.ExternalEventID, "msg-1")
	}
	if req.Metadata[meta.KeyChannel] != "testbridge" {
		t.Fatalf("Metadata[channel] = %v", req.Metadata[meta.KeyChannel])
	}
	if req.Metadata["chat_id"] != "chat-1" {
		t.Fatalf("Metadata[chat_id] = %v", req.Metadata["chat_id"])
	}
}

func TestNewStandardBridgeInitializesThreadBindings(t *testing.T) {
	t.Parallel()

	bridge := NewStandardBridge(StandardBridgeConfig{ChannelName: "testbridge"}, nil, nil, nil, nil, channels.DefaultStatusReminderDelay)
	if bridge.threadBindings == nil {
		t.Fatal("threadBindings should be initialized by default")
	}
}

func TestStandardBridgeNormalizesChatTypeFromRoomIDWithoutChannelNameGuessing(t *testing.T) {
	t.Parallel()

	adapter := &standardBridgeAdapter{inbound: make(chan channels.InboundMessage, 1)}
	runtime := &standardBridgeRuntime{run: &agent.Run{ID: "run-group-chat"}}
	bridge := NewStandardBridge(StandardBridgeConfig{
		ChannelName:  "custombridge",
		TargetIDKey:  "room_id",
		MessageIDKey: "event_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		SenderID: "@alice:example.org",
		Content:  "hello room",
		RawEvent: map[string]any{
			"room_id":  "!room:example.org",
			"event_id": "$event-1",
		},
	})

	req := runtime.InteractionRequest()
	if req == nil {
		t.Fatal("Interact() was not called")
	}
	if got := req.Metadata["chat_type"]; got != "group" {
		t.Fatalf("chat_type = %#v, want %q", got, "group")
	}
	if got := req.Metadata[meta.KeyChannelName]; got != "custombridge" {
		t.Fatalf("channel_name = %#v, want %q", got, "custombridge")
	}
}

func TestStandardBridgeHandleOutboundEventDeliversTerminalResult(t *testing.T) {
	t.Parallel()

	adapter := &standardBridgeAdapter{inbound: make(chan channels.InboundMessage, 1)}
	runtime := &standardBridgeRuntime{
		run: &agent.Run{
			ID:           "run-1",
			InputEventID: "msg-1",
		},
		runResult: &runtimesvc.RunResult{
			Output: "done",
		},
	}
	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "testbridge:chat-1", "test-model", "sess-1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	runtime.run.SessionID = session.ID

	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	locked.Messages = append(locked.Messages, contextengine.Message{
		Role:    contextengine.RoleUser,
		Content: "hello bridge",
		Metadata: map[string]any{
			meta.KeyChannel: "testbridge",
			"chat_id":       "chat-1",
			"message_id":    "msg-1",
		},
	})
	locked.MessageCount = len(locked.Messages)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	bus := eventbus.NewInMemoryBus()
	bridge := NewStandardBridge(StandardBridgeConfig{
		ChannelName:      "testbridge",
		TargetIDKey:      "chat_id",
		MessageIDKey:     "message_id",
		DirectUsesChatID: true,
	}, adapter, runtime, store, bus, channels.DefaultStatusReminderDelay)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge.Start(ctx)
	defer bridge.Stop()

	if err := bus.Publish(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-1",
		SessionID: session.ID,
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for outbound delivery")
		default:
		}
		msgs := adapter.Messages()
		if len(msgs) == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if msgs[0].TargetID != "chat-1" {
			t.Fatalf("TargetID = %q, want %q", msgs[0].TargetID, "chat-1")
		}
		if msgs[0].ReplyToID != "msg-1" {
			t.Fatalf("ReplyToID = %q, want %q", msgs[0].ReplyToID, "msg-1")
		}
		if msgs[0].Content != "done" {
			t.Fatalf("Content = %q, want %q", msgs[0].Content, "done")
		}
		return
	}
}

func TestStandardBridgeHandleOutboundEventUsesStoredChannelMetadataWithoutSessionKeyPrefix(t *testing.T) {
	t.Parallel()

	adapter := &standardBridgeAdapter{inbound: make(chan channels.InboundMessage, 1)}
	runtime := &standardBridgeRuntime{
		run: &agent.Run{
			ID:           "run-opaque",
			InputEventID: "msg-opaque-1",
		},
		runResult: &runtimesvc.RunResult{
			Output: "done",
		},
	}
	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "opaque-session", "test-model", "sess-opaque")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	runtime.run.SessionID = session.ID

	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	locked.Messages = append(locked.Messages, contextengine.Message{
		Role:    contextengine.RoleUser,
		Content: "hello bridge",
		Metadata: map[string]any{
			meta.KeyChannel: "testbridge",
			"chat_id":       "chat-opaque-1",
			"message_id":    "msg-opaque-1",
		},
	})
	locked.MessageCount = len(locked.Messages)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	bus := eventbus.NewInMemoryBus()
	bridge := NewStandardBridge(StandardBridgeConfig{
		ChannelName:      "testbridge",
		TargetIDKey:      "chat_id",
		MessageIDKey:     "message_id",
		DirectUsesChatID: true,
	}, adapter, runtime, store, bus, channels.DefaultStatusReminderDelay)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge.Start(ctx)
	defer bridge.Stop()

	if err := bus.Publish(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-opaque",
		SessionID: session.ID,
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for outbound delivery")
		default:
		}
		msgs := adapter.Messages()
		if len(msgs) == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if msgs[0].TargetID != "chat-opaque-1" {
			t.Fatalf("TargetID = %q, want %q", msgs[0].TargetID, "chat-opaque-1")
		}
		return
	}
}

func TestStandardBridgeStreamingRendererLifecycle(t *testing.T) {
	t.Parallel()

	adapter := newStandardBridgeStreamingAdapter()
	runtime := &standardBridgeRuntime{
		run: &agent.Run{
			ID:           "run-stream",
			InputEventID: "msg-1",
		},
		runResult: &runtimesvc.RunResult{
			Output: "hello world",
		},
	}
	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "testbridge:chat-1", "test-model", "sess-stream")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	runtime.run.SessionID = session.ID

	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	locked.Messages = append(locked.Messages, contextengine.Message{
		Role:    contextengine.RoleUser,
		Content: "stream please",
		Metadata: map[string]any{
			meta.KeyChannel: "testbridge",
			"chat_id":       "chat-1",
			"message_id":    "msg-1",
		},
	})
	locked.MessageCount = len(locked.Messages)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	bus := eventbus.NewInMemoryBus()
	bridge := NewStandardBridge(StandardBridgeConfig{
		ChannelName:      "testbridge",
		TargetIDKey:      "chat_id",
		MessageIDKey:     "message_id",
		DirectUsesChatID: true,
	}, adapter, runtime, store, bus, channels.DefaultStatusReminderDelay)
	bridge.status.Track(context.Background(), channels.RunNotificationTarget{
		RunID:        "run-stream",
		SessionKey:   "testbridge:chat-1",
		ChannelID:    "testbridge",
		TargetID:     "chat-1",
		ReplyToID:    "msg-1",
		InputContent: "stream please",
		Format:       "text",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge.Start(ctx)
	defer bridge.Stop()

	for _, event := range []eventbus.Event{
		{Type: eventbus.EventModelTextDelta, RunID: "run-stream", SessionID: session.ID, Attrs: map[string]any{"delta": "hello"}},
		{Type: eventbus.EventModelTextDelta, RunID: "run-stream", SessionID: session.ID, Attrs: map[string]any{"delta": " world"}},
		{Type: eventbus.EventRunCompleted, RunID: "run-stream", SessionID: session.ID},
	} {
		if err := bus.Publish(context.Background(), event); err != nil {
			t.Fatalf("Publish(%s) error = %v", event.Type, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		adapter.streamMu.Lock()
		beginCount := adapter.beginCount
		updateCount := adapter.updateCount
		endCount := adapter.endCount
		var last string
		if len(adapter.contents) > 0 {
			last = adapter.contents[len(adapter.contents)-1]
		}
		adapter.streamMu.Unlock()
		if beginCount == 1 && updateCount >= 1 && endCount == 1 && last == "hello world" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stream counts = begin:%d update:%d end:%d contents:%#v", adapter.beginCount, adapter.updateCount, adapter.endCount, adapter.contents)
}

func TestStandardBridgeWithoutStreamingRendererSendsOnlyTerminalResult(t *testing.T) {
	t.Parallel()

	adapter := &standardBridgeAdapter{inbound: make(chan channels.InboundMessage, 1)}
	runtime := &standardBridgeRuntime{
		run: &agent.Run{
			ID:           "run-no-stream",
			InputEventID: "msg-1",
		},
		runResult: &runtimesvc.RunResult{
			Output: "done",
		},
	}
	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "testbridge:chat-1", "test-model", "sess-nostream")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	runtime.run.SessionID = session.ID

	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	locked.Messages = append(locked.Messages, contextengine.Message{
		Role:    contextengine.RoleUser,
		Content: "plain delivery",
		Metadata: map[string]any{
			meta.KeyChannel: "testbridge",
			"chat_id":       "chat-1",
			"message_id":    "msg-1",
		},
	})
	locked.MessageCount = len(locked.Messages)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	bus := eventbus.NewInMemoryBus()
	bridge := NewStandardBridge(StandardBridgeConfig{
		ChannelName:      "testbridge",
		TargetIDKey:      "chat_id",
		MessageIDKey:     "message_id",
		DirectUsesChatID: true,
	}, adapter, runtime, store, bus, channels.DefaultStatusReminderDelay)
	bridge.status.Track(context.Background(), channels.RunNotificationTarget{
		RunID:        "run-no-stream",
		SessionKey:   "testbridge:chat-1",
		ChannelID:    "testbridge",
		TargetID:     "chat-1",
		ReplyToID:    "msg-1",
		InputContent: "plain delivery",
		Format:       "text",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge.Start(ctx)
	defer bridge.Stop()

	if err := bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventModelTextDelta, RunID: "run-no-stream", SessionID: session.ID, Attrs: map[string]any{"delta": "hello"}}); err != nil {
		t.Fatalf("Publish(delta) error = %v", err)
	}
	if err := bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventRunCompleted, RunID: "run-no-stream", SessionID: session.ID}); err != nil {
		t.Fatalf("Publish(completed) error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msgs := adapter.Messages()
		if len(msgs) == 1 && msgs[0].Content == "done" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("messages = %#v, want one terminal send", adapter.Messages())
}

func TestStandardBridgeStreamingThrottleLimitsUpdateCalls(t *testing.T) {
	t.Parallel()

	adapter := newStandardBridgeStreamingAdapter()
	bridge := NewStandardBridge(StandardBridgeConfig{
		ChannelName:      "testbridge",
		TargetIDKey:      "chat_id",
		MessageIDKey:     "message_id",
		DirectUsesChatID: true,
	}, adapter, nil, agent.NewInMemorySessionStore(), eventbus.NewInMemoryBus(), channels.DefaultStatusReminderDelay)
	bridge.status.Track(context.Background(), channels.RunNotificationTarget{
		RunID:        "run-throttle",
		SessionKey:   "testbridge:chat-1",
		ChannelID:    "testbridge",
		TargetID:     "chat-1",
		ReplyToID:    "msg-1",
		InputContent: "stream please",
		Format:       "text",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge.Start(ctx)
	defer bridge.Stop()

	bus := bridge.bus
	for _, delta := range []string{"a", "b", "c"} {
		if err := bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventModelTextDelta, RunID: "run-throttle", Attrs: map[string]any{"delta": delta}}); err != nil {
			t.Fatalf("Publish(%q) error = %v", delta, err)
		}
	}
	time.Sleep(100 * time.Millisecond)

	adapter.streamMu.Lock()
	firstWindow := adapter.updateCount
	adapter.streamMu.Unlock()
	if firstWindow != 1 {
		t.Fatalf("updateCount after first window = %d, want 1", firstWindow)
	}

	time.Sleep(channels.BridgeStreamingThrottle + 100*time.Millisecond)
	if err := bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventModelTextDelta, RunID: "run-throttle", Attrs: map[string]any{"delta": "d"}}); err != nil {
		t.Fatalf("Publish(after throttle) error = %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	adapter.streamMu.Lock()
	defer adapter.streamMu.Unlock()
	if adapter.updateCount != 2 {
		t.Fatalf("updateCount = %d, want 2", adapter.updateCount)
	}
}
