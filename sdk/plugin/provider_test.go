package plugin

import (
	"context"
	"errors"
	"testing"
)

type testProviderPlugin struct {
	provider Provider
}

func (p testProviderPlugin) Provider() Provider {
	return p.provider
}

func TestProviderChatAndModels(t *testing.T) {
	t.Parallel()

	plugin := testProviderPlugin{
		provider: Provider{
			ChatFunc: func(_ context.Context, runtime PluginRuntime, request ChatRequest) (ChatResponse, error) {
				value, err := ConfigValue(runtime, "default_model")
				if err != nil {
					return ChatResponse{}, err
				}
				return ChatResponse{
					Model: value.(string),
					Message: ChatMessage{
						Role:    ChatRoleAssistant,
						Content: "hello " + request.Messages[0].Content,
					},
					Usage: TokenUsage{
						InputTokens:  3,
						OutputTokens: 2,
						TotalTokens:  5,
					},
					Metadata: map[string]any{
						"provider": "demo",
					},
				}, nil
			},
			ModelsFunc: func(context.Context, PluginRuntime) ([]ModelInfo, error) {
				return []ModelInfo{{
					ID:            "demo-chat",
					DisplayName:   "Demo Chat",
					ContextWindow: 128000,
					Capabilities:  []string{"chat"},
				}}, nil
			},
		},
	}

	runtime := stubRuntime{
		config: map[string]any{
			"default_model": "demo-chat",
		},
	}
	response, err := plugin.Provider().Chat(context.Background(), runtime, ChatRequest{
		Messages: []ChatMessage{{
			Role:    ChatRoleUser,
			Content: "hopclaw",
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if response.Model != "demo-chat" {
		t.Fatalf("Chat() Model = %q, want %q", response.Model, "demo-chat")
	}
	if response.Message.Content != "hello hopclaw" {
		t.Fatalf("Chat() Message.Content = %q", response.Message.Content)
	}

	models, err := plugin.Provider().Models(context.Background(), runtime)
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "demo-chat" {
		t.Fatalf("Models() = %#v", models)
	}
	listed, err := plugin.Provider().ListModels(context.Background(), runtime)
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "demo-chat" {
		t.Fatalf("ListModels() = %#v", listed)
	}
	models[0].Capabilities[0] = "mutated"
	if plugin.provider.ModelsFunc == nil {
		t.Fatalf("ModelsFunc unexpectedly nil")
	}
	if listed[0].Capabilities[0] != "chat" {
		t.Fatalf("ListModels() capabilities mutated = %#v", listed)
	}
}

func TestProviderChatAndModelsErrors(t *testing.T) {
	t.Parallel()

	plugin := testProviderPlugin{}

	if _, err := plugin.Provider().Chat(context.Background(), nil, ChatRequest{}); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("Chat(nil) error = %v, want ErrNilRuntime", err)
	}
	if _, err := plugin.Provider().Models(context.Background(), nil); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("Models(nil) error = %v, want ErrNilRuntime", err)
	}
	if _, err := plugin.Provider().ListModels(context.Background(), nil); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("ListModels(nil) error = %v, want ErrNilRuntime", err)
	}

	runtime := stubRuntime{}
	if _, err := plugin.Provider().Chat(context.Background(), runtime, ChatRequest{}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Chat() error = %v, want ErrNotImplemented", err)
	}
	if _, err := plugin.Provider().Models(context.Background(), runtime); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Models() error = %v, want ErrNotImplemented", err)
	}
	if _, err := plugin.Provider().ListModels(context.Background(), runtime); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("ListModels() error = %v, want ErrNotImplemented", err)
	}
}

func TestProviderHelpers(t *testing.T) {
	t.Parallel()

	request := ChatRequest{
		Model: "  demo-chat  ",
		Messages: []ChatMessage{
			{Role: ChatRoleSystem, Content: "You are helpful."},
			{Role: ChatRoleUser, Content: " first "},
			{Role: ChatRoleAssistant, Content: "seen"},
			{Role: ChatRoleUser, Content: "  latest  "},
		},
	}

	if got := ResolveModel(request, "fallback"); got != "demo-chat" {
		t.Fatalf("ResolveModel(explicit) = %q, want demo-chat", got)
	}
	request.Model = ""
	if got := ResolveModel(request, "  fallback "); got != "fallback" {
		t.Fatalf("ResolveModel(fallback) = %q, want fallback", got)
	}

	message, ok := FindLastMessage(request.Messages, ChatRoleAssistant)
	if !ok || message.Content != "seen" {
		t.Fatalf("FindLastMessage(assistant) = %#v, %v", message, ok)
	}
	if got := LastMessageContent(request.Messages, ChatRoleSystem); got != "You are helpful." {
		t.Fatalf("LastMessageContent(system) = %q", got)
	}
	if got := LastUserMessage(request.Messages); got != "latest" {
		t.Fatalf("LastUserMessage() = %q, want latest", got)
	}
	if _, ok := FindLastMessage(request.Messages, ChatRoleTool); ok {
		t.Fatal("FindLastMessage(tool) unexpectedly found a message")
	}
}
