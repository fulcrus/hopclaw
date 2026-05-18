package helloprovider

import (
	"context"
	"fmt"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

const (
	ProviderName = "hello-provider"
	DefaultModel = "hello-provider-chat"
)

type Plugin struct{}

func Manifest() sdkplugin.Manifest {
	manifest := sdkplugin.NewManifest(
		ProviderName,
		"1.0.0",
		"Example Level 0 provider plugin built with the HopClaw typed SDK.",
	)
	manifest.Providers = map[string]sdkplugin.ProviderDecl{
		ProviderName: {
			API:          "openai-completions",
			BaseURL:      "https://api.example.com/v1",
			DefaultModel: DefaultModel,
		},
	}
	return manifest
}

func (Plugin) Provider() sdkplugin.Provider {
	return sdkplugin.Provider{
		ModelsFunc: func(context.Context, sdkplugin.PluginRuntime) ([]sdkplugin.ModelInfo, error) {
			return []sdkplugin.ModelInfo{{
				ID:            DefaultModel,
				DisplayName:   "Hello Provider Chat",
				ContextWindow: 32000,
				Capabilities:  []string{"chat"},
			}}, nil
		},
		ChatFunc: func(ctx context.Context, runtime sdkplugin.PluginRuntime, request sdkplugin.ChatRequest) (sdkplugin.ChatResponse, error) {
			modelID := sdkplugin.ResolveModel(request, DefaultModel)
			replyText := "Hello from hello-provider!"
			if userMessage := sdkplugin.LastUserMessage(request.Messages); userMessage != "" {
				replyText = fmt.Sprintf("Hello, %s!", userMessage)
			}
			if err := runtime.Emit(ctx, sdkplugin.Event{
				Name: "hello-provider.chat",
				Payload: map[string]any{
					"model": modelID,
				},
			}); err != nil {
				return sdkplugin.ChatResponse{}, err
			}
			runtime.Logf("hello-provider answered with %s", modelID)
			return sdkplugin.ChatResponse{
				Model: modelID,
				Message: sdkplugin.ChatMessage{
					Role:    sdkplugin.ChatRoleAssistant,
					Content: replyText,
				},
				Usage: sdkplugin.TokenUsage{
					InputTokens:  len(request.Messages),
					OutputTokens: 4,
					TotalTokens:  len(request.Messages) + 4,
				},
				Metadata: map[string]any{
					"provider": ProviderName,
				},
			}, nil
		},
	}
}
