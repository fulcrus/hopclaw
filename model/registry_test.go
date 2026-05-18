package model

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

type stubModelClient struct {
	lastModel string
	resp      *agent.ModelResponse
	err       error
}

func (s *stubModelClient) Chat(_ context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	s.lastModel = req.Model
	if s.err != nil {
		return nil, s.err
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &agent.ModelResponse{}, nil
}

type stubStreamingModelClient struct {
	stubModelClient
	streamResp *agent.ModelResponse
	streamErr  error
}

func (s *stubStreamingModelClient) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	s.lastModel = req.Model
	if s.streamErr != nil {
		return nil, s.streamErr
	}
	if s.streamResp == nil {
		s.streamResp = &agent.ModelResponse{}
	}
	if cb != nil {
		cb.OnTextDelta(ctx, s.streamResp.Message.Content)
		cb.OnComplete(ctx)
	}
	return s.streamResp, nil
}

type stableModelClient struct {
	resp *agent.ModelResponse
	err  error
}

func (s *stableModelClient) Chat(_ context.Context, _ agent.ChatRequest) (*agent.ModelResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &agent.ModelResponse{}, nil
}

func TestRegistryResolveProviderModelTreatsUnknownPrefixAsModelID(t *testing.T) {
	t.Parallel()

	client := &stubModelClient{}
	reg := &Registry{
		clients: map[string]agent.ModelClient{
			"openrouter": client,
		},
		defaultName: "openrouter",
	}

	if _, err := reg.Chat(context.Background(), agent.ChatRequest{Model: "deepseek-ai/DeepSeek-R1"}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if client.lastModel != "deepseek-ai/DeepSeek-R1" {
		t.Fatalf("client.lastModel = %q", client.lastModel)
	}
}

func TestRegistryResolveProviderModelUsesKnownProviderPrefix(t *testing.T) {
	t.Parallel()

	openAI := &stubModelClient{}
	anthropic := &stubModelClient{}
	reg := &Registry{
		clients: map[string]agent.ModelClient{
			"openai":    openAI,
			"anthropic": anthropic,
		},
		defaultName: "openai",
	}

	if _, err := reg.Chat(context.Background(), agent.ChatRequest{Model: "anthropic/claude-sonnet-4-5"}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if anthropic.lastModel != "claude-sonnet-4-5" {
		t.Fatalf("anthropic.lastModel = %q", anthropic.lastModel)
	}
	if openAI.lastModel != "" {
		t.Fatalf("openAI.lastModel = %q", openAI.lastModel)
	}
}

func TestRegistryResolveProviderModelUsesLongestMatchingProviderPrefix(t *testing.T) {
	t.Parallel()

	rootProvider := &stubModelClient{}
	pluginProvider := &stubModelClient{}
	reg := &Registry{
		clients: map[string]agent.ModelClient{
			"demo":         rootProvider,
			"demo/copilot": pluginProvider,
		},
		defaultName: "demo",
	}

	if _, err := reg.Chat(context.Background(), agent.ChatRequest{Model: "demo/copilot/gpt-4o"}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if pluginProvider.lastModel != "gpt-4o" {
		t.Fatalf("pluginProvider.lastModel = %q, want gpt-4o", pluginProvider.lastModel)
	}
	if rootProvider.lastModel != "" {
		t.Fatalf("rootProvider.lastModel = %q, want empty", rootProvider.lastModel)
	}
}

func TestMatchProviderPrefixSupportsProviderNamesWithSlash(t *testing.T) {
	t.Parallel()

	provider, modelID, ok := MatchProviderPrefix("demo/copilot/gpt-4o", []string{"demo", "demo/copilot"})
	if !ok {
		t.Fatal("expected MatchProviderPrefix() to resolve plugin-style provider")
	}
	if provider != "demo/copilot" {
		t.Fatalf("provider = %q, want demo/copilot", provider)
	}
	if modelID != "gpt-4o" {
		t.Fatalf("modelID = %q, want gpt-4o", modelID)
	}
}

func TestRegistrySetDefaultRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	reg := &Registry{
		clients: map[string]agent.ModelClient{"openai": &stubModelClient{}},
	}
	if err := reg.SetDefault("missing"); err == nil {
		t.Fatal("expected SetDefault to reject unknown provider")
	}
}

func TestRegistrySetFallbacksRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	reg := &Registry{
		clients: map[string]agent.ModelClient{
			"primary": &stubModelClient{},
		},
	}
	if err := reg.SetFallbacks("primary", "missing"); err == nil {
		t.Fatal("expected SetFallbacks to reject unknown fallback provider")
	}
}

func TestRegistryConcurrentConfigurationUpdates(t *testing.T) {
	t.Parallel()

	reg := &Registry{
		clients: map[string]agent.ModelClient{
			"primary": &stableModelClient{
				resp: &agent.ModelResponse{
					Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "primary"},
				},
			},
			"backup": &stableModelClient{
				resp: &agent.ModelResponse{
					Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "backup"},
				},
			},
		},
		defaultName: "primary",
		fallbacks: map[string][]string{
			"primary": {"backup"},
			"backup":  {"primary"},
		},
	}

	const writers = 4
	const readers = 16
	const iterations = 200

	start := make(chan struct{})
	errs := make(chan error, writers+readers)
	var wg sync.WaitGroup

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				defaultName := "primary"
				fallbackName := "backup"
				if (idx+j)%2 == 1 {
					defaultName, fallbackName = fallbackName, defaultName
				}
				if err := reg.SetDefault(defaultName); err != nil {
					errs <- err
					return
				}
				if err := reg.SetFallbacks(defaultName, fallbackName); err != nil {
					errs <- err
					return
				}
				if err := reg.SetFallbacks(fallbackName, defaultName); err != nil {
					errs <- err
					return
				}
			}
		}(i)
	}

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				resp, err := reg.Chat(context.Background(), agent.ChatRequest{Model: "gpt-4o-mini"})
				if err != nil {
					errs <- err
					return
				}
				if resp == nil {
					errs <- fmt.Errorf("Chat() returned nil response")
					return
				}
				if got := reg.DefaultProvider(); got != "primary" && got != "backup" {
					errs <- fmt.Errorf("DefaultProvider() = %q, want primary or backup", got)
					return
				}
				if _, err := reg.providerChain("primary"); err != nil {
					errs <- err
					return
				}
				if _, err := reg.providerChain("backup"); err != nil {
					errs <- err
					return
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent registry access failed: %v", err)
	}
}

func TestRegistryChatFallsBackOnTransientProviderError(t *testing.T) {
	t.Parallel()

	primary := &stubModelClient{
		err: providerAPIError("primary", 502, "", "bad gateway"),
	}
	backup := &stubModelClient{
		resp: &agent.ModelResponse{
			Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "backup-ok"},
		},
	}
	reg := &Registry{
		clients: map[string]agent.ModelClient{
			"primary": primary,
			"backup":  backup,
		},
		defaultName: "primary",
		fallbacks: map[string][]string{
			"primary": {"backup"},
		},
	}

	resp, err := reg.Chat(context.Background(), agent.ChatRequest{Model: "primary/gpt-4o"})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp == nil || resp.Message.Content != "backup-ok" {
		t.Fatalf("Chat() response = %#v", resp)
	}
	if primary.lastModel != "gpt-4o" {
		t.Fatalf("primary.lastModel = %q, want gpt-4o", primary.lastModel)
	}
	if backup.lastModel != "gpt-4o" {
		t.Fatalf("backup.lastModel = %q, want gpt-4o", backup.lastModel)
	}
}

func TestRegistryChatStreamFallsBackOnTransientProviderError(t *testing.T) {
	t.Parallel()

	primary := &stubStreamingModelClient{
		streamErr: providerAPIError("primary", 503, "", "temporarily unavailable"),
	}
	backup := &stubStreamingModelClient{
		streamResp: &agent.ModelResponse{
			Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "stream-backup-ok"},
		},
	}
	reg := &Registry{
		clients: map[string]agent.ModelClient{
			"primary": primary,
			"backup":  backup,
		},
		defaultName: "primary",
		fallbacks: map[string][]string{
			"primary": {"backup"},
		},
	}

	var deltas []string
	cb := &testStreamCallback{
		onTextDelta: func(_ context.Context, delta string) {
			deltas = append(deltas, delta)
		},
	}

	resp, err := reg.ChatStream(context.Background(), agent.ChatRequest{Model: "primary/gpt-4o-mini"}, cb)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if resp == nil || resp.Message.Content != "stream-backup-ok" {
		t.Fatalf("ChatStream() response = %#v", resp)
	}
	if len(deltas) != 1 || deltas[0] != "stream-backup-ok" {
		t.Fatalf("deltas = %#v", deltas)
	}
}

func TestRegistryChatStreamFallsBackToChatCallbacks(t *testing.T) {
	t.Parallel()

	client := &stubModelClient{
		resp: &agent.ModelResponse{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "fallback-stream",
			},
			ToolCalls: []agent.ToolCall{{
				ID:    "call-1",
				Name:  "fs.read",
				Input: map[string]any{"path": "main.go"},
			}},
		},
	}
	reg := &Registry{
		clients: map[string]agent.ModelClient{
			"openrouter": client,
		},
		defaultName: "openrouter",
	}

	var mu sync.Mutex
	var deltas []string
	var toolStarts []string
	var toolArgs []string
	completed := false
	cb := &testStreamCallback{
		onTextDelta: func(_ context.Context, delta string) {
			mu.Lock()
			deltas = append(deltas, delta)
			mu.Unlock()
		},
		onToolCallStart: func(_ context.Context, _ string, name string) {
			mu.Lock()
			toolStarts = append(toolStarts, name)
			mu.Unlock()
		},
		onToolCallDelta: func(_ context.Context, _ string, delta string) {
			mu.Lock()
			toolArgs = append(toolArgs, delta)
			mu.Unlock()
		},
		onComplete: func(context.Context) {
			mu.Lock()
			completed = true
			mu.Unlock()
		},
	}

	resp, err := reg.ChatStream(context.Background(), agent.ChatRequest{Model: "demo/model"}, cb)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if resp == nil || resp.Message.Content != "fallback-stream" {
		t.Fatalf("ChatStream() response = %#v", resp)
	}
	if client.lastModel != "demo/model" {
		t.Fatalf("client.lastModel = %q, want demo/model", client.lastModel)
	}
	if len(deltas) != 1 || deltas[0] != "fallback-stream" {
		t.Fatalf("text deltas = %#v", deltas)
	}
	if len(toolStarts) != 1 || toolStarts[0] != "fs.read" {
		t.Fatalf("tool starts = %#v", toolStarts)
	}
	if len(toolArgs) != 1 || toolArgs[0] != "{\"path\":\"main.go\"}" {
		t.Fatalf("tool args = %#v", toolArgs)
	}
	if !completed {
		t.Fatal("expected completion callback")
	}
}

func TestRegisterProviderClientBuilderExtendsFactoryWithoutSwitchEdit(t *testing.T) {
	t.Parallel()

	api := ProviderAPI("test-provider-api")
	if err := RegisterProviderClientBuilder(api, func(entry ProviderEntry) (agent.ModelClient, error) {
		return &stubModelClient{
			resp: &agent.ModelResponse{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: fmt.Sprintf("builder:%s", entry.DefaultModel),
				},
			},
		}, nil
	}); err != nil {
		t.Fatalf("RegisterProviderClientBuilder() error = %v", err)
	}

	reg, err := NewRegistry(map[string]ProviderEntry{
		"custom": {
			API:          api,
			DefaultModel: "demo-model",
		},
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	resp, err := reg.Chat(context.Background(), agent.ChatRequest{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "builder:demo-model" {
		t.Fatalf("resp.Message.Content = %q", resp.Message.Content)
	}
}

func TestNormalizeProviderAPIAliases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   ProviderAPI
		want ProviderAPI
	}{
		{name: "custom", in: "custom", want: APIOpenAICompletions},
		{name: "openai", in: "openai", want: APIOpenAICompletions},
		{name: "anthropic", in: "anthropic", want: APIAnthropicMessages},
		{name: "google", in: "google", want: APIGoogleGenerativeAI},
		{name: "bedrock", in: "bedrock", want: APIBedrockConverse},
		{name: "copilot", in: "copilot", want: APIGitHubCopilot},
		{name: "canonical", in: APIOpenAIResponses, want: APIOpenAIResponses},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeProviderAPI(tt.in); got != tt.want {
				t.Fatalf("NormalizeProviderAPI(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
