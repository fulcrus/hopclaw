package nostr

import (
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
	"github.com/fulcrus/hopclaw/config"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.Nostr
		return registration.SharedBridgeDescriptors(
			deps,
			"nostr",
			cfg,
			runtimeDescriptorNostrActive(cfg),
			"pubkey",
			"event_id",
			"",
			channels.PolicyConfig{},
			false,
			func() channels.Adapter {
				return New(Config{PrivateKey: cfg.PrivateKey, Relays: cfg.Relays})
			},
			func(adapter channels.Adapter) registration.BridgeLifecycle {
				return NewBridge(adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay).
					WithPolicy(channels.PolicyConfig{}).
					WithMessageDeduper(channels.NewMessageDeduper(registration.ChannelDedupePath(deps.StorePath, "nostr", ""), 24*time.Hour)).
					WithThreadBindings(deps.ThreadBindings)
			},
		)
	})
}

func runtimeDescriptorNostrActive(cfg config.NostrChannelConfig) bool {
	if cfg.Enabled != nil && !*cfg.Enabled {
		return false
	}
	return strings.TrimSpace(cfg.PrivateKey) != "" && len(cfg.Relays) > 0
}
