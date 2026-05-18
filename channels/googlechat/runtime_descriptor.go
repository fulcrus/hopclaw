package googlechat

import (
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.GoogleChat
		return registration.SharedBridgeDescriptors(
			deps,
			"googlechat",
			cfg,
			registration.ChannelActiveAny(cfg.Enabled, cfg.ServiceAccount, cfg.WebhookURL),
			"space_name",
			"message_id",
			"",
			channels.PolicyConfig{},
			false,
			func() channels.Adapter {
				return New(Config{ServiceAccount: cfg.ServiceAccount, WebhookURL: cfg.WebhookURL, VerificationKey: cfg.VerificationKey})
			},
			func(adapter channels.Adapter) registration.BridgeLifecycle {
				return NewBridge(adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay).
					WithPolicy(channels.PolicyConfig{}).
					WithMessageDeduper(channels.NewMessageDeduper(registration.ChannelDedupePath(deps.StorePath, "googlechat", ""), 24*time.Hour)).
					WithThreadBindings(deps.ThreadBindings)
			},
		)
	})
}
