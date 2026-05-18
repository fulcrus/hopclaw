package msteams

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.MSTeams
		policy := registration.CommonChannelPolicy(cfg.CommonChannelConfig)
		return registration.SharedBridgeDescriptors(
			deps,
			"msteams",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.AppID, cfg.Password),
			"conversation_id",
			"message_id",
			"reply_to_id",
			policy,
			true,
			func() channels.Adapter {
				return New(Config{AppID: cfg.AppID, Password: cfg.Password})
			},
			func(adapter channels.Adapter) registration.BridgeLifecycle {
				return NewBridge(adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay).
					WithPolicy(policy).
					WithMessageDeduper(channels.NewMessageDeduper(registration.ChannelDedupePath(deps.StorePath, "msteams", cfg.DedupeDir), cfg.DedupeTTL)).
					WithThreadBindings(deps.ThreadBindings)
			},
		)
	})
}
