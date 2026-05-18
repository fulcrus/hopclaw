package model

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/internal/metrics"
)

// ProviderAPI identifies the wire protocol a provider uses.
type ProviderAPI string

const (
	APIOpenAICompletions  ProviderAPI = "openai-completions"
	APIOpenAIResponses    ProviderAPI = "openai-responses"
	APIAnthropicMessages  ProviderAPI = "anthropic-messages"
	APIGoogleGenerativeAI ProviderAPI = "google-generative-ai"
	APIOllama             ProviderAPI = "ollama"
	APIBedrockConverse    ProviderAPI = "bedrock-converse"
	APIGitHubCopilot      ProviderAPI = "github-copilot"
)

// ProviderEntry is the config for one provider.
type ProviderEntry struct {
	API          ProviderAPI
	BaseURL      string
	Region       string
	APIKey       string
	APIKeys      []string // multiple keys for rotation; overrides APIKey
	Fallbacks    []string
	AccessKeyID  string
	SecretKey    string
	SessionToken string
	DefaultModel string
	Timeout      time.Duration
	Headers      map[string]string

	// OAuth fields — when set, the provider uses OAuth token exchange
	// instead of a static API key.
	OAuthTokenURL     string
	OAuthClientID     string
	OAuthClientSecret string
	OAuthRefreshToken string
	OAuthScopes       []string

	// RequestHooks can instrument or adjust outbound provider HTTP requests for
	// request/transport-based providers.
	RequestHooks []ProviderRequestHook
}

// ResolveKeys returns the effective list of API keys.
// If APIKeys is set, it's used; otherwise APIKey is used as a single-element list.
func (e ProviderEntry) ResolveKeys() []string {
	if len(e.APIKeys) > 0 {
		return e.APIKeys
	}
	if e.APIKey != "" {
		return []string{e.APIKey}
	}
	return nil
}

// Registry holds one ModelClient per provider name and routes
// Chat requests based on a "provider/model" model identifier.
// If the model string contains no slash, the "default" provider is used.
type Registry struct {
	mu          sync.RWMutex // guards defaultName and fallbacks
	clients     map[string]agent.ModelClient // provider name → client
	defaultName string
	fallbacks   map[string][]string
}

// ProviderClientBuilder constructs a model client for one provider entry and
// returns any configuration or transport setup error.
type ProviderClientBuilder func(ProviderEntry) (agent.ModelClient, error)

var (
	providerClientBuildersMu sync.RWMutex
	providerClientBuilders   = map[ProviderAPI]ProviderClientBuilder{
		APIOpenAICompletions:  buildOpenAICompatClient,
		APIOpenAIResponses:    buildOpenAIResponsesClient,
		APIOllama:             buildOpenAICompatClient,
		APIAnthropicMessages:  buildAnthropicProviderClient,
		APIGoogleGenerativeAI: buildGoogleProviderClient,
		APIBedrockConverse:    buildBedrockProviderClient,
		APIGitHubCopilot:      buildCopilotProviderClient,
	}
)

// NewRegistry creates a Registry from a map of provider configs.
func NewRegistry(providers map[string]ProviderEntry) (*Registry, error) {
	clients := make(map[string]agent.ModelClient, len(providers))
	fallbacks := make(map[string][]string, len(providers))
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)

	var defaultName string
	for _, name := range names {
		entry := providers[name]
		client, err := buildClient(entry)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}
		clients[name] = client
		if defaultName == "" {
			defaultName = name
		}
		if len(entry.Fallbacks) > 0 {
			fallbacks[name] = normalizeRegistryFallbacks(name, entry.Fallbacks)
		}
	}
	for name, chain := range fallbacks {
		validated, err := validateRegistryFallbacks(name, chain, clients)
		if err != nil {
			return nil, err
		}
		fallbacks[name] = validated
	}
	return &Registry{clients: clients, defaultName: defaultName, fallbacks: fallbacks}, nil
}

// buildClient creates a ModelClient for the given provider entry.
// If OAuth is configured, wraps with OAuthClient for automatic token management.
// If multiple API keys are configured, wraps with KeyRotatingClient.
func buildClient(entry ProviderEntry) (agent.ModelClient, error) {
	// OAuth providers use token exchange instead of static API keys.
	if entry.OAuthTokenURL != "" {
		return NewOAuthClient(OAuthConfig{
			TokenURL:       entry.OAuthTokenURL,
			ClientID:       entry.OAuthClientID,
			ClientSecret:   entry.OAuthClientSecret,
			RefreshToken:   entry.OAuthRefreshToken,
			Scopes:         entry.OAuthScopes,
			ProviderConfig: entry,
		})
	}

	keys := entry.ResolveKeys()
	if len(keys) > 1 {
		return NewKeyRotatingClient(keys, func(key string) (agent.ModelClient, error) {
			keyed := entry
			keyed.APIKey = key
			keyed.APIKeys = nil
			return newClientForAPI(keyed)
		})
	}
	// Single key or no key — use directly.
	if len(keys) == 1 {
		entry.APIKey = keys[0]
	}
	return newClientForAPI(entry)
}

// SetDefault sets the default provider name used when the model string has no provider prefix.
func (r *Registry) SetDefault(name string) error {
	if _, ok := r.clients[name]; !ok {
		return fmt.Errorf("unknown model provider %q", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultName = name
	return nil
}

// SetFallbacks configures fallback providers for a primary provider.
func (r *Registry) SetFallbacks(name string, fallbacks ...string) error {
	if _, ok := r.clients[name]; !ok {
		return fmt.Errorf("unknown model provider %q", name)
	}
	validated, err := validateRegistryFallbacks(name, normalizeRegistryFallbacks(name, fallbacks), r.clients)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fallbacks == nil {
		r.fallbacks = make(map[string][]string)
	}
	if len(validated) == 0 {
		delete(r.fallbacks, name)
		return nil
	}
	r.fallbacks[name] = append([]string(nil), validated...)
	return nil
}

// Chat routes the request to the appropriate provider client.
// The model field may use "provider/model" format (e.g., "anthropic/claude-sonnet-4-5").
// If no provider prefix, the default provider is used.
func (r *Registry) Chat(ctx context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	provider, model := r.resolveProviderModel(req.Model)
	return r.callWithFallback(ctx, provider, model, req, func(client agent.ModelClient, request agent.ChatRequest) (*agent.ModelResponse, error) {
		return client.Chat(ctx, request)
	})
}

// ChatStream routes the request to the provider and uses streaming callbacks
// if the provider supports StreamingModelClient. Otherwise falls back to Chat.
func (r *Registry) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	provider, model := r.resolveProviderModel(req.Model)
	return r.callWithFallback(ctx, provider, model, req, func(client agent.ModelClient, request agent.ChatRequest) (*agent.ModelResponse, error) {
		if sc, ok := client.(agent.StreamingModelClient); ok {
			return sc.ChatStream(ctx, request, cb)
		}
		return streamModelResponseFallback(ctx, client, request, cb)
	})
}

// ProviderNames returns the names of all configured providers.
func (r *Registry) ProviderNames() []string {
	names := make([]string, 0, len(r.clients))
	for name := range r.clients {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DefaultProvider returns the name of the default provider.
func (r *Registry) DefaultProvider() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultName
}

// resolveProviderModel splits "provider/model" only when the prefix matches a
// configured provider. This keeps model IDs like "org/model" usable with a
// default provider.
func (r *Registry) resolveProviderModel(s string) (string, string) {
	s = strings.TrimSpace(s)
	r.mu.RLock()
	defaultName := r.defaultName
	r.mu.RUnlock()
	providerNames := make([]string, 0, len(r.clients))
	for name := range r.clients {
		providerNames = append(providerNames, name)
	}
	if provider, model, ok := MatchProviderPrefix(s, providerNames); ok {
		return provider, model
	}
	return defaultName, s
}

func (r *Registry) callWithFallback(
	ctx context.Context,
	provider string,
	model string,
	req agent.ChatRequest,
	invoke func(agent.ModelClient, agent.ChatRequest) (*agent.ModelResponse, error),
) (*agent.ModelResponse, error) {
	chain, err := r.providerChain(provider)
	if err != nil {
		return nil, fmt.Errorf("%w (from model %q)", err, req.Model)
	}

	var lastErr error
	for idx, providerName := range chain {
		client := r.clients[providerName]
		attemptReq := req
		attemptReq.Model = model

		start := time.Now()
		resp, err := invoke(client, attemptReq)
		metrics.ModelCallDuration.WithLabelValues(providerName, model).Observe(time.Since(start).Seconds())
		if err == nil {
			return resp, nil
		}
		metrics.ModelCallErrors.WithLabelValues(providerName, model, errorClass(err)).Inc()
		lastErr = err
		if idx == len(chain)-1 || !shouldRegistryFailover(ctx, err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (r *Registry) providerChain(primary string) ([]string, error) {
	if _, ok := r.clients[primary]; !ok {
		return nil, fmt.Errorf("unknown model provider %q", primary)
	}
	r.mu.RLock()
	fallbacks := append([]string(nil), r.fallbacks[primary]...)
	r.mu.RUnlock()
	chain := []string{primary}
	if len(fallbacks) == 0 {
		return chain, nil
	}
	chain = append(chain, fallbacks...)
	return chain, nil
}

func normalizeRegistryFallbacks(primary string, fallbacks []string) []string {
	seen := make(map[string]struct{}, len(fallbacks))
	out := make([]string, 0, len(fallbacks))
	for _, name := range fallbacks {
		name = strings.TrimSpace(name)
		if name == "" || name == primary {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func validateRegistryFallbacks(primary string, fallbacks []string, clients map[string]agent.ModelClient) ([]string, error) {
	validated := make([]string, 0, len(fallbacks))
	for _, name := range fallbacks {
		if _, ok := clients[name]; !ok {
			return nil, fmt.Errorf("provider %q: unknown fallback provider %q", primary, name)
		}
		validated = append(validated, name)
	}
	return validated, nil
}

func shouldRegistryFailover(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if typed, ok := asProviderAPIError(err); ok {
		return typed.Class == ProviderErrorClassTransient
	}
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, os.ErrDeadlineExceeded) ||
		(errors.As(err, &netErr) && netErr.Timeout())
}

func newClientForAPI(entry ProviderEntry) (agent.ModelClient, error) {
	api := NormalizeProviderAPI(entry.API)
	if api == "" {
		api = APIOpenAICompletions
	}
	builder, ok := providerClientBuilder(api)
	if !ok {
		return nil, fmt.Errorf("unsupported API type %q", api)
	}
	return builder(entry)
}

// RegisterProviderClientBuilder overrides or adds the builder for a provider
// API. Both api and builder must be non-empty or a validation error is returned.
func RegisterProviderClientBuilder(api ProviderAPI, builder ProviderClientBuilder) error {
	api = ProviderAPI(strings.TrimSpace(string(api)))
	if api == "" {
		return fmt.Errorf("provider api is required")
	}
	if builder == nil {
		return fmt.Errorf("provider builder for %q is required", api)
	}
	providerClientBuildersMu.Lock()
	defer providerClientBuildersMu.Unlock()
	providerClientBuilders[api] = builder
	return nil
}

func providerClientBuilder(api ProviderAPI) (ProviderClientBuilder, bool) {
	providerClientBuildersMu.RLock()
	defer providerClientBuildersMu.RUnlock()
	builder, ok := providerClientBuilders[api]
	return builder, ok
}

func buildOpenAICompatClient(entry ProviderEntry) (agent.ModelClient, error) {
	baseURL := entry.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return NewOpenAICompatClient(OpenAICompatConfig{
		BaseURL:      baseURL,
		APIKey:       entry.APIKey,
		DefaultModel: entry.DefaultModel,
		Timeout:      entry.Timeout,
		Headers:      entry.Headers,
		RequestHooks: entry.RequestHooks,
	})
}

func buildAnthropicProviderClient(entry ProviderEntry) (agent.ModelClient, error) {
	baseURL := entry.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return NewAnthropicClient(AnthropicConfig{
		BaseURL:      baseURL,
		APIKey:       entry.APIKey,
		DefaultModel: entry.DefaultModel,
		Timeout:      entry.Timeout,
		Headers:      entry.Headers,
		RequestHooks: entry.RequestHooks,
	})
}

func buildGoogleProviderClient(entry ProviderEntry) (agent.ModelClient, error) {
	baseURL := entry.BaseURL
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	return NewGoogleClient(GoogleConfig{
		BaseURL:      baseURL,
		APIKey:       entry.APIKey,
		DefaultModel: entry.DefaultModel,
		Timeout:      entry.Timeout,
		Headers:      entry.Headers,
		RequestHooks: entry.RequestHooks,
	})
}

// errorClass extracts a short classification from an error for low-cardinality labels.
func errorClass(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "context canceled"):
		return "canceled"
	case strings.Contains(message, "context deadline exceeded"):
		return "timeout"
	case strings.Contains(message, "429") || strings.Contains(strings.ToLower(message), "rate limit"):
		return "rate_limited"
	case strings.Contains(message, "500") || strings.Contains(message, "502") || strings.Contains(message, "503"):
		return "server_error"
	case strings.Contains(message, "401") || strings.Contains(message, "403"):
		return "auth_error"
	default:
		return "other"
	}
}

func buildBedrockProviderClient(entry ProviderEntry) (agent.ModelClient, error) {
	return NewBedrockClient(BedrockConfig{
		Region:       entry.Region,
		AccessKeyID:  entry.AccessKeyID,
		SecretKey:    entry.SecretKey,
		SessionToken: entry.SessionToken,
		DefaultModel: entry.DefaultModel,
		Timeout:      entry.Timeout,
		Headers:      entry.Headers,
	})
}

func buildOpenAIResponsesClient(entry ProviderEntry) (agent.ModelClient, error) {
	baseURL := entry.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return NewOpenAIResponsesClient(OpenAIResponsesConfig{
		BaseURL:      baseURL,
		APIKey:       entry.APIKey,
		DefaultModel: entry.DefaultModel,
		Timeout:      entry.Timeout,
		Headers:      entry.Headers,
		RequestHooks: entry.RequestHooks,
	})
}

func buildCopilotProviderClient(entry ProviderEntry) (agent.ModelClient, error) {
	token := entry.APIKey
	if token == "" {
		for _, env := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
			if v := strings.TrimSpace(os.Getenv(env)); v != "" {
				token = v
				break
			}
		}
	}
	return NewCopilotClient(CopilotConfig{
		GitHubToken:  token,
		DefaultModel: entry.DefaultModel,
		Timeout:      entry.Timeout,
		Headers:      entry.Headers,
	})
}
