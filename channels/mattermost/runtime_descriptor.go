package mattermost

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.Mattermost
		policy := registration.CommonChannelPolicy(cfg.CommonChannelConfig)
		return registration.SharedBridgeDescriptors(
			deps,
			"mattermost",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.BaseURL, cfg.BotToken),
			"channel_id",
			"post_id",
			"root_id",
			policy,
			true,
			func() channels.Adapter {
				return New(Config{BaseURL: cfg.BaseURL, BotToken: cfg.BotToken, WebSocketURL: cfg.WebSocketURL})
			},
			func(adapter channels.Adapter) registration.BridgeLifecycle {
				return NewBridge(adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay).
					WithPolicy(policy).
					WithMessageDeduper(channels.NewMessageDeduper(registration.ChannelDedupePath(deps.StorePath, "mattermost", cfg.DedupeDir), cfg.DedupeTTL)).
					WithThreadBindings(deps.ThreadBindings)
			},
		)
	})
}
