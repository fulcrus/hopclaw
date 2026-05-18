package webhook

import (
	"context"

	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, state registration.DescriptorState) []registration.Descriptor {
		if !registration.EnabledOrDefault(deps.Channels.Webhook.Enabled, true) {
			return nil
		}

		descriptors := make([]registration.Descriptor, 0, len(deps.Channels.Webhook.Instances))
		for id, inst := range deps.Channels.Webhook.Instances {
			id := id
			inst := inst
			descriptors = append(descriptors, registration.Descriptor{
				Name:          "webhook:" + id,
				Order:         200,
				RuntimeConfig: inst,
				Build: func(context.Context) ([]registration.Installation, error) {
					adapter := New(Config{ID: id, CallbackURL: inst.CallbackURL, Secret: inst.Secret})
					if err := deps.ChannelManager.Register("webhook:"+id, adapter); err != nil {
						return nil, err
					}
					if state != nil {
						state.RememberWebhookAdapter(id, adapter)
					}
					bridge := NewBridge(id, adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay)
					return []registration.Installation{{
						Name:    "webhook:" + id,
						Adapter: adapter,
						Bridge:  bridge,
					}}, nil
				},
			})
		}
		return descriptors
	})
}
