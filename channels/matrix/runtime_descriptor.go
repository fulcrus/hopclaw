package matrix

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.Matrix
		policy := registration.CommonChannelPolicy(cfg.CommonChannelConfig)
		return registration.SharedBridgeDescriptors(
			deps,
			"matrix",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.HomeServer, cfg.AccessToken),
			"room_id",
			"event_id",
			"",
			policy,
			true,
			func() channels.Adapter {
				return New(Config{HomeServer: cfg.HomeServer, UserID: cfg.UserID, AccessToken: cfg.AccessToken})
			},
			func(adapter channels.Adapter) registration.BridgeLifecycle {
				return NewBridge(adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay).
					WithPolicy(policy).
					WithMessageDeduper(channels.NewMessageDeduper(registration.ChannelDedupePath(deps.StorePath, "matrix", cfg.DedupeDir), cfg.DedupeTTL)).
					WithThreadBindings(deps.ThreadBindings)
			},
		)
	})
}
