package registration

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/channels/pairing"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/plugin"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type BridgeLifecycle interface {
	Start(context.Context)
	Stop()
}

type Descriptor struct {
	Name          string
	Order         int
	RuntimeConfig any
	Build         func(context.Context) ([]Installation, error)
}

type Installation struct {
	Name           string
	Adapter        channels.Adapter
	Bridge         BridgeLifecycle
	ManagedProcess *ManagedProcessPlan
}

type ManagedProcessPlan struct {
	Config plugin.ProcessConfig
	Spawn  plugin.SpawnFunc
}

type RuntimeDeps struct {
	Channels       config.ChannelsConfig
	StorePath      string
	ChannelManager *channelmgr.Manager
	RuntimeService *runtimesvc.Service
	Sessions       agent.SessionStore
	Bus            *eventbus.InMemoryBus
	StatusDelay    time.Duration
	ModuleCatalog  *modules.Store
	PairingManager *pairing.Manager
	ThreadBindings *channels.ThreadBinding
}

type DescriptorState interface {
	RememberWebhookAdapter(id string, adapter channels.Adapter)
}

type BuiltinProvider func(RuntimeDeps, DescriptorState) []Descriptor

var (
	builtinProvidersMu sync.RWMutex
	builtinProviders   []BuiltinProvider
)

func RegisterBuiltinProvider(provider BuiltinProvider) {
	if provider == nil {
		return
	}
	builtinProvidersMu.Lock()
	defer builtinProvidersMu.Unlock()
	builtinProviders = append(builtinProviders, provider)
}

func BuiltinProviders() []BuiltinProvider {
	builtinProvidersMu.RLock()
	defer builtinProvidersMu.RUnlock()
	out := make([]BuiltinProvider, len(builtinProviders))
	copy(out, builtinProviders)
	return out
}

func BuiltinDescriptors(deps RuntimeDeps, state DescriptorState) []Descriptor {
	providers := BuiltinProviders()
	if len(providers) == 0 {
		return nil
	}
	descriptors := make([]Descriptor, 0, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		descriptors = append(descriptors, provider(deps, state)...)
	}
	return descriptors
}

func ChannelActive(enabled *bool, requiredFields ...string) bool {
	if enabled != nil && !*enabled {
		return false
	}
	for _, field := range requiredFields {
		if strings.TrimSpace(field) == "" {
			return false
		}
	}
	return len(requiredFields) > 0
}

func ChannelActiveAny(enabled *bool, fields ...string) bool {
	if enabled != nil && !*enabled {
		return false
	}
	for _, field := range fields {
		if strings.TrimSpace(field) != "" {
			return true
		}
	}
	return false
}

func EnabledOrDefault(v *bool, fallback bool) bool {
	return normalize.BoolOrDefault(v, fallback)
}

func CommonChannelPolicy(cfg config.CommonChannelConfig) channels.PolicyConfig {
	return channels.PolicyConfig{
		DMPolicy:          cfg.DMPolicy,
		AllowFrom:         append([]string(nil), cfg.AllowFrom...),
		GroupPolicy:       cfg.GroupPolicy,
		GroupAllowFrom:    append([]string(nil), cfg.GroupAllowFrom...),
		RequireMention:    cfg.RequireMention != nil && *cfg.RequireMention,
		GroupSessionScope: cfg.GroupSessionScope,
		ReplyInThread:     strings.EqualFold(strings.TrimSpace(cfg.ReplyInThread), "enabled"),
		DedupeTTL:         cfg.DedupeTTL,
		DedupeDir:         cfg.DedupeDir,
	}
}

func ChannelDedupePath(storePath, channelName, configured string) string {
	if path := strings.TrimSpace(configured); path != "" {
		return path
	}
	return filepath.Join(storePath, "channels", channelName+"-dedupe.json")
}

func SharedBridgeDescriptors(
	deps RuntimeDeps,
	name string,
	runtimeConfig any,
	active bool,
	targetKey string,
	messageKey string,
	threadKey string,
	policy channels.PolicyConfig,
	directUsesChatID bool,
	buildAdapter func() channels.Adapter,
	buildBridge ...func(channels.Adapter) BridgeLifecycle,
) []Descriptor {
	if !active || buildAdapter == nil {
		return nil
	}
	return []Descriptor{{
		Name:          name,
		Order:         100,
		RuntimeConfig: runtimeConfig,
		Build: func(context.Context) ([]Installation, error) {
			adapter := buildAdapter()
			if err := deps.ChannelManager.Register(name, adapter); err != nil {
				return nil, err
			}

			var bridge BridgeLifecycle
			if len(buildBridge) > 0 && buildBridge[0] != nil {
				bridge = buildBridge[0](adapter)
			} else {
					bridge = channels.NewBridge(
						channels.BridgeConfig{ChannelName: name, TargetIDKey: targetKey, MessageIDKey: messageKey, ThreadIDKey: threadKey},
						adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay,
					).WithPolicy(policy).
						WithMessageDeduper(channels.NewMessageDeduper(ChannelDedupePath(deps.StorePath, name, policy.DedupeDir), policy.DedupeTTL)).
						WithThreadBindings(deps.ThreadBindings).
						WithDirectSessionUsesChatID(directUsesChatID)
			}
			return []Installation{{
				Name:    name,
				Adapter: adapter,
				Bridge:  bridge,
			}}, nil
		},
	}}
}
