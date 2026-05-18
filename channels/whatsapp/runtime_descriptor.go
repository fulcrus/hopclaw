package whatsapp

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.WhatsApp
		policy := registration.CommonChannelPolicy(cfg.CommonChannelConfig)
		return registration.SharedBridgeDescriptors(
			deps,
			"whatsapp",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.PhoneID, cfg.APIToken),
			"from",
			"message_id",
			"",
			policy,
			true,
			func() channels.Adapter {
				return New(Config{PhoneID: cfg.PhoneID, APIToken: cfg.APIToken, BaseURL: cfg.BaseURL})
			},
			func(adapter channels.Adapter) registration.BridgeLifecycle {
				return NewBridge(adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay).
					WithPolicy(policy).
					WithMessageDeduper(channels.NewMessageDeduper(registration.ChannelDedupePath(deps.StorePath, "whatsapp", cfg.DedupeDir), cfg.DedupeTTL)).
					WithThreadBindings(deps.ThreadBindings)
			},
		)
	})
}
