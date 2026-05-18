package synologychat

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.SynologyChat
		return registration.SharedBridgeDescriptors(
			deps,
			"synology-chat",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.WebhookURL),
			"user_id",
			"message_id",
			"",
			channels.PolicyConfig{},
			false,
			func() channels.Adapter {
				return New(Config{BaseURL: cfg.BaseURL, WebhookURL: cfg.WebhookURL, BotToken: cfg.BotToken})
			},
		)
	})
}
