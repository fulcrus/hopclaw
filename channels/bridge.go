package channels

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

var log = logging.WithSubsystem("channels")

// BridgeRuntime is the subset of runtimesvc.Service the bridge needs.
type BridgeRuntime interface {
	InteractableRuntime
	Submit(ctx context.Context, req runtimesvc.SubmitRequest) (*agent.Run, error)
	GetRun(ctx context.Context, id string) (*agent.Run, error)
	GetApproval(ctx context.Context, id string) (*approval.Ticket, error)
	FindPendingApproval(ctx context.Context, sessionID string) (*approval.Ticket, error)
	ResolveApproval(ctx context.Context, id string, resolution approval.Resolution) (*approval.Ticket, error)
	CancelRun(ctx context.Context, runID string) (*agent.Run, error)
	GetArtifact(ctx context.Context, id string) (*artifact.Blob, error)
}

// InteractableRuntime is an optional extension of BridgeRuntime that supports
// the unified Interact entry point. When the runtime implements this interface,
// the bridge delegates classification and execution to Interact instead of
// running its own classification chain.
type InteractableRuntime interface {
	Interact(ctx context.Context, req runtimesvc.InteractionRequest) (*runtimesvc.InteractionResult, error)
}

// BridgeConfig parameterises a shared Bridge for any channel adapter.
type BridgeConfig struct {
	// ChannelName is used as the session-key prefix (e.g. "telegram", "slack").
	ChannelName string
	// TargetIDKey is the metadata key holding the delivery target
	// (e.g. "chat_id", "channel_id"). Falls back to sender_id if empty.
	TargetIDKey string
	// MessageIDKey is the metadata key holding the message/reply ID
	// (e.g. "message_id", "thread_ts").
	MessageIDKey string
	// ThreadIDKey is the metadata key holding the thread/topic/root ID when
	// the platform supports thread-scoped routing.
	ThreadIDKey string
}

// Bridge connects any channels.Adapter to the HopClaw runtime.
// It forwards inbound messages to the runtime and sends terminal
// run results back through the adapter.
type Bridge struct {
	cfg              BridgeConfig
	adapter          Adapter
	runtime          BridgeRuntime
	sessions         agent.SessionStore
	bus              *eventbus.InMemoryBus
	status           *RunStatusNotifier
	authGate         *AuthFailureGate
	projector        *RunEventProjector
	threadBindings   *ThreadBinding
	policy           PolicyConfig
	deduper          *MessageDeduper
	directUsesChatID bool
	outbound         *OutboundSerializer

	cancel context.CancelFunc

	mu        sync.Mutex
	delivered map[string]time.Time
	streams   map[string]*StreamingDeliveryState
}

func NewBridge(cfg BridgeConfig, adapter Adapter, runtime BridgeRuntime, sessions agent.SessionStore, bus *eventbus.InMemoryBus, statusDelay time.Duration) *Bridge {
	if cfg.TargetIDKey == "" {
		cfg.TargetIDKey = "chat_id"
	}
	if cfg.MessageIDKey == "" {
		cfg.MessageIDKey = "message_id"
	}
	serializer := NewOutboundSerializer()
	var send func(context.Context, OutboundMessage) error
	if adapter != nil {
		send = func(ctx context.Context, msg OutboundMessage) error {
			return serializer.Do(func() error {
				return adapter.Send(ctx, msg)
			})
		}
	}
	return &Bridge{
		cfg:              cfg,
		adapter:          adapter,
		runtime:          runtime,
		sessions:         sessions,
		bus:              bus,
		status:           NewRunStatusNotifier(statusDelay, send),
		authGate:         NewAuthFailureGate(DefaultAuthFailureCooldown, DefaultAuthFailureReminderInterval),
		projector:        NewRunEventProjector(),
		directUsesChatID: true,
		outbound:         serializer,
		delivered:        make(map[string]time.Time),
		streams:          make(map[string]*StreamingDeliveryState),
	}
}

func (b *Bridge) WithPolicy(policy PolicyConfig) *Bridge {
	if b == nil {
		return nil
	}
	b.policy = NormalizePolicyConfig(policy)
	return b
}

func (b *Bridge) WithMessageDeduper(deduper *MessageDeduper) *Bridge {
	if b == nil {
		return nil
	}
	b.deduper = deduper
	return b
}

func (b *Bridge) WithThreadBindings(bindings *ThreadBinding) *Bridge {
	if b == nil {
		return nil
	}
	b.threadBindings = bindings
	return b
}

func (b *Bridge) WithDirectSessionUsesChatID(enabled bool) *Bridge {
	if b == nil {
		return nil
	}
	b.directUsesChatID = enabled
	return b
}

func (b *Bridge) Start(ctx context.Context) {
	if b == nil || b.adapter == nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	b.cancel = cancel
	b.mu.Unlock()

	if inbound := b.adapter.SubscribeEvents(); inbound != nil {
		go b.inboundLoop(ctx, inbound)
	}
	if b.bus != nil {
		sub := b.bus.SubscribeChannel(128)
		go b.outboundLoop(ctx, sub)
	}
}

func (b *Bridge) Stop() {
	if b == nil {
		return
	}
	b.mu.Lock()
	cancel := b.cancel
	b.cancel = nil
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (b *Bridge) RestoreRun(ctx context.Context, target RunNotificationTarget, run *agent.Run) bool {
	if b == nil || b.status == nil {
		return false
	}
	return b.status.Restore(ctx, target, run)
}

func (b *Bridge) send(ctx context.Context, msg OutboundMessage) error {
	if b == nil || b.adapter == nil {
		return fmt.Errorf("bridge: adapter is required")
	}
	return b.outbound.Do(func() error {
		return b.adapter.Send(ctx, msg)
	})
}
