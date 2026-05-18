package hellochannel

import (
	"context"
	"fmt"
	"strings"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

const ChannelName = "hello-channel"

type Plugin struct{}

func Manifest() sdkplugin.Manifest {
	manifest := sdkplugin.NewManifest(
		ChannelName,
		"1.0.0",
		"Example Level 0 channel plugin built with the HopClaw typed SDK.",
	)
	manifest.Channels = map[string]sdkplugin.ChannelDecl{
		ChannelName: {
			Type:         "stdio",
			Command:      "./hello-channel",
			Capabilities: []string{"connect", "send"},
		},
	}
	return manifest
}

func (Plugin) Channel() sdkplugin.Channel {
	return sdkplugin.Channel{
		ConnectFunc: func(ctx context.Context, runtime sdkplugin.PluginRuntime) error {
			runtime.Logf("hello-channel connected")
			return runtime.Emit(ctx, sdkplugin.Event{Name: "hello-channel.connected"})
		},
		SendFunc: func(ctx context.Context, runtime sdkplugin.PluginRuntime, message sdkplugin.OutboundMessage) (sdkplugin.SendResult, error) {
			content := strings.TrimSpace(message.Content)
			if content == "" {
				content = "hello from hello-channel"
			}
			if err := runtime.Emit(ctx, sdkplugin.Event{
				Name: "hello-channel.sent",
				Payload: map[string]any{
					"target":  message.TargetID,
					"content": content,
				},
			}); err != nil {
				return sdkplugin.SendResult{}, err
			}
			runtime.Logf("hello-channel sent %q", content)
			return sdkplugin.SendResult{
				MessageID: fmt.Sprintf("%s:%s", ChannelName, message.TargetID),
				Metadata: map[string]any{
					"echo": content,
				},
			}, nil
		},
	}
}
