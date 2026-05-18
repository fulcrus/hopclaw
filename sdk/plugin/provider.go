package plugin

import (
	"context"
	"slices"
	"strings"
)

type ChatRole string

const (
	ChatRoleSystem    ChatRole = "system"
	ChatRoleUser      ChatRole = "user"
	ChatRoleAssistant ChatRole = "assistant"
	ChatRoleTool      ChatRole = "tool"
)

type ChatMessage struct {
	Role    ChatRole
	Content string
}

type ChatRequest struct {
	Model    string
	Messages []ChatMessage
	Metadata map[string]any
}

type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type ChatResponse struct {
	Model    string
	Message  ChatMessage
	Usage    TokenUsage
	Metadata map[string]any
}

type ModelInfo struct {
	ID            string
	DisplayName   string
	ContextWindow int
	Capabilities  []string
}

type ProviderPlugin interface {
	Provider() Provider
}

type Provider struct {
	ChatFunc   func(ctx context.Context, runtime PluginRuntime, request ChatRequest) (ChatResponse, error)
	ModelsFunc func(ctx context.Context, runtime PluginRuntime) ([]ModelInfo, error)
}

func (p Provider) Chat(ctx context.Context, runtime PluginRuntime, request ChatRequest) (ChatResponse, error) {
	if runtime == nil {
		return ChatResponse{}, ErrNilRuntime
	}
	if p.ChatFunc == nil {
		return ChatResponse{}, ErrNotImplemented
	}
	request.Messages = slices.Clone(request.Messages)
	request.Metadata = cloneMapAny(request.Metadata)
	response, err := p.ChatFunc(ctx, runtime, request)
	if err != nil {
		return ChatResponse{}, err
	}
	response.Metadata = cloneMapAny(response.Metadata)
	return response, nil
}

func (p Provider) Models(ctx context.Context, runtime PluginRuntime) ([]ModelInfo, error) {
	return p.ListModels(ctx, runtime)
}

func (p Provider) ListModels(ctx context.Context, runtime PluginRuntime) ([]ModelInfo, error) {
	if runtime == nil {
		return nil, ErrNilRuntime
	}
	if p.ModelsFunc == nil {
		return nil, ErrNotImplemented
	}
	models, err := p.ModelsFunc(ctx, runtime)
	if err != nil {
		return nil, err
	}
	return cloneModels(models), nil
}

// ResolveModel returns the explicit request model when present, otherwise the
// provided fallback.
func ResolveModel(request ChatRequest, fallback string) string {
	if model := strings.TrimSpace(request.Model); model != "" {
		return model
	}
	return strings.TrimSpace(fallback)
}

// FindLastMessage returns the last message for the requested role.
func FindLastMessage(messages []ChatMessage, role ChatRole) (ChatMessage, bool) {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		if messages[idx].Role != role {
			continue
		}
		return messages[idx], true
	}
	return ChatMessage{}, false
}

// LastMessageContent returns the content of the last message for the requested
// role, or an empty string when none exists.
func LastMessageContent(messages []ChatMessage, role ChatRole) string {
	message, ok := FindLastMessage(messages, role)
	if !ok {
		return ""
	}
	return message.Content
}

// LastUserMessage returns the trimmed content from the last user message.
func LastUserMessage(messages []ChatMessage) string {
	return strings.TrimSpace(LastMessageContent(messages, ChatRoleUser))
}

func cloneModels(src []ModelInfo) []ModelInfo {
	if len(src) == 0 {
		return nil
	}
	dst := make([]ModelInfo, len(src))
	for idx, value := range src {
		value.Capabilities = slices.Clone(value.Capabilities)
		dst[idx] = value
	}
	return dst
}
