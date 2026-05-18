package signal

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.Signal
		policy := registration.CommonChannelPolicy(cfg.CommonChannelConfig)
		return registration.SharedBridgeDescriptors(
			deps,
			"signal",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.BaseURL, cfg.Number),
			"group_id",
			"message_id",
			"",
			policy,
			true,
			func() channels.Adapter {
				return New(Config{BaseURL: cfg.BaseURL, Number: cfg.Number, AuthToken: cfg.AuthToken})
			},
			func(adapter channels.Adapter) registration.BridgeLifecycle {
				return NewBridge(adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay).
					WithPolicy(policy).
					WithMessageDeduper(channels.NewMessageDeduper(registration.ChannelDedupePath(deps.StorePath, "signal", cfg.DedupeDir), cfg.DedupeTTL)).
					WithThreadBindings(deps.ThreadBindings)
			},
		)
	})
}
