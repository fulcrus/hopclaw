package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/model"
	"github.com/fulcrus/hopclaw/modelrouter"
	"github.com/fulcrus/hopclaw/plugin"
	"github.com/fulcrus/hopclaw/store"
)

func TestOperatorListsUseEffectiveConfigResolver(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	base := config.Config{
		Server: config.ServerConfig{
			AuthToken: "test-token",
		},
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    ".hopclaw/state",
		},
		Runtime: config.RuntimeConfig{
			Profile: config.RuntimeProfileProduction,
			Audit: config.AuditConfig{
				Enabled: true,
			},
		},
		Agent: config.AgentConfig{
			DefaultModel: "openai/gpt-4o",
		},
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"openai": {
					API:          "openai-completions",
					BaseURL:      "https://api.openai.com/v1",
					APIKey:       "sk-openai",
					DefaultModel: "gpt-4o",
				},
			},
		},
		Channels: config.ChannelsConfig{
			Slack: config.SlackChannelConfig{
				Enabled:  boolPtrGateway(true),
				BotToken: "yaml-token",
			},
		},
	}
	base.ApplyDefaults()
	resolver, err := controloverlay.NewResolver(context.Background(), base, gatewayStaticStoreReader{
		providers: []store.ProviderConfigRow{{
			Name:         "anthropic",
			API:          "anthropic-messages",
			APIKey:       "sk-anthropic",
			DefaultModel: "claude-sonnet-4-5",
			Source:       store.ConfigSourceAPI,
		}},
		channels: []store.ChannelConfigRow{{
			Name:    "discord",
			Config:  `{"bot_token":"discord-token"}`,
			Enabled: boolPtrGateway(true),
			Source:  store.ConfigSourceAPI,
		}},
	}, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	gw.SetEffectiveConfigResolver(resolver)

	modelsRec := doRequest(t, gw.Handler(), "GET", "/operator/models", "")
	if modelsRec.Code != 200 {
		t.Fatalf("GET /operator/models status = %d body=%s", modelsRec.Code, modelsRec.Body.String())
	}
	var modelsPayload modelsListResponse
	if err := json.Unmarshal(modelsRec.Body.Bytes(), &modelsPayload); err != nil {
		t.Fatalf("json.Unmarshal(models) error = %v", err)
	}
	if modelsPayload.Count != 2 {
		t.Fatalf("models count = %d, want 2", modelsPayload.Count)
	}
	if !hasProviderSource(modelsPayload.Providers, "openai", "yaml") || !hasProviderSource(modelsPayload.Providers, "anthropic", "api") {
		t.Fatalf("providers = %#v", modelsPayload.Providers)
	}
	if got := providerCapabilityMatrix(modelsPayload.Providers, "anthropic"); got.ProviderAPI != model.APIAnthropicMessages || !got.SupportsTools {
		t.Fatalf("anthropic capability matrix = %+v", got)
	}
	if got := providerCapabilityMatrix(modelsPayload.Providers, "openai"); got.ProviderAPI != model.APIOpenAICompletions || !got.SupportsStreaming {
		t.Fatalf("openai capability matrix = %+v", got)
	}

	channelsRec := doRequest(t, gw.Handler(), "GET", "/operator/channels", "")
	if channelsRec.Code != 200 {
		t.Fatalf("GET /operator/channels status = %d body=%s", channelsRec.Code, channelsRec.Body.String())
	}
	var channelsPayload channelsListResponse
	if err := json.Unmarshal(channelsRec.Body.Bytes(), &channelsPayload); err != nil {
		t.Fatalf("json.Unmarshal(channels) error = %v", err)
	}
	if channelsPayload.Count != 2 {
		t.Fatalf("channels count = %d, want 2", channelsPayload.Count)
	}
	if !hasChannelSource(channelsPayload.Items, "slack", "yaml") || !hasChannelSource(channelsPayload.Items, "discord", "api") {
		t.Fatalf("channels = %#v", channelsPayload.Items)
	}
	if channelsPayload.Items[0].Name != "slack" || channelsPayload.Items[1].Name != "discord" {
		t.Fatalf("channels order = %#v", channelsPayload.Items)
	}
}

func TestOperatorModelsListUsesStableGlobalOrderingAcrossSources(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	base := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"openai": {
					API:          "openai-completions",
					BaseURL:      "https://api.openai.com/v1",
					APIKey:       "sk-openai",
					DefaultModel: "gpt-4o",
				},
			},
		},
	}
	base.ApplyDefaults()

	resolver, err := controloverlay.NewResolver(context.Background(), base, gatewayStaticStoreReader{
		providers: []store.ProviderConfigRow{{
			Name:         "anthropic",
			API:          "anthropic-messages",
			APIKey:       "sk-anthropic",
			DefaultModel: "claude-sonnet-4-5",
			Source:       store.ConfigSourceAPI,
		}},
	}, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	gw.SetEffectiveConfigResolver(resolver)

	manager := plugin.NewManager()
	if err := manager.Register(plugin.LoadedPlugin{
		Dir: t.TempDir(),
		Manifest: plugin.Manifest{
			Name: "plugin-pack",
			Providers: map[string]plugin.ProviderDecl{
				"azure": {
					API:          "openai-completions",
					BaseURL:      "https://azure.example.com/v1",
					DefaultModel: "gpt-4.1",
				},
				"cohere": {
					API:          "openai-completions",
					BaseURL:      "https://cohere.example.com/v1",
					DefaultModel: "command-r",
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register(plugin) error = %v", err)
	}
	gw.SetModuleCatalog(modules.NewStore(modules.BuildCatalog(manager.Modules())))

	rec := doRequest(t, gw.Handler(), "GET", "/operator/models", "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/models status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload modelsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(models) error = %v", err)
	}
	if payload.Count != 4 {
		t.Fatalf("models count = %d, want 4", payload.Count)
	}
	got := []string{
		payload.Providers[0].Name,
		payload.Providers[1].Name,
		payload.Providers[2].Name,
		payload.Providers[3].Name,
	}
	want := []string{"anthropic", "openai", "plugin-pack/azure", "plugin-pack/cohere"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("provider order = %v, want %v", got, want)
	}
}

func providerCapabilityMatrix(items []providerInfo, name string) model.CapabilityMatrix {
	for _, item := range items {
		if item.Name == name {
			return item.CapabilityMatrix
		}
	}
	return model.CapabilityMatrix{}
}

func TestOperatorConfigSectionUsesOverlayStoreAndEffectiveResolver(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	base := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "sqlite", Path: ".hopclaw/state"},
		Runtime: config.RuntimeConfig{
			Profile: config.RuntimeProfileProduction,
			Audit:   config.AuditConfig{Enabled: true},
		},
		Agent: config.AgentConfig{DefaultModel: "openai/gpt-4o"},
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"openai": {
					API:          "openai-completions",
					BaseURL:      "https://api.openai.com/v1",
					APIKey:       "sk-openai",
					DefaultModel: "gpt-4o",
				},
			},
		},
	}
	base.ApplyDefaults()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	configStore, err := store.NewConfigStore(db)
	if err != nil {
		t.Fatalf("store.NewConfigStore() error = %v", err)
	}
	resolver, err := controloverlay.NewResolver(context.Background(), base, configStore, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	gw.SetConfigStore(configStore)
	gw.SetEffectiveConfigResolver(resolver)
	gw.SetConfigMutationService(controloverlay.NewMutationService(configStore, resolver, controloverlay.MutationOptions{
		Refresh: resolver.Refresh,
	}))

	rec := doRequest(t, gw.Handler(), "PUT", "/operator/config/agent", `{"default_model":"openai/gpt-4.1"}`)
	if rec.Code != 200 {
		t.Fatalf("PUT /operator/config/agent status = %d body=%s", rec.Code, rec.Body.String())
	}

	row, err := configStore.GetSetting(context.Background(), config.SectionOverlayKey("agent"))
	if err != nil {
		t.Fatalf("GetSetting(config.section.agent) error = %v", err)
	}
	if row.Source != store.ConfigSourceAPI {
		t.Fatalf("row.Source = %q, want %q", row.Source, store.ConfigSourceAPI)
	}

	sectionRec := doRequest(t, gw.Handler(), "GET", "/operator/config/agent", "")
	if sectionRec.Code != 200 {
		t.Fatalf("GET /operator/config/agent status = %d body=%s", sectionRec.Code, sectionRec.Body.String())
	}
	var payload config.AgentConfig
	if err := json.Unmarshal(sectionRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(section) error = %v", err)
	}
	if got := payload.DefaultModel; got != "openai/gpt-4.1" {
		t.Fatalf("DefaultModel = %#v, want %q", got, "openai/gpt-4.1")
	}
}

func TestOperatorModelsListIncludesOpenAICompatRuntimeDefault(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	base := config.Config{
		Server: config.ServerConfig{
			AuthToken: "test-token",
		},
		Store: config.StoreConfig{
			Backend: "memory",
		},
		Agent: config.AgentConfig{
			DefaultModel: "gpt-4o",
		},
		Models: config.ModelsConfig{
			OpenAICompat: config.OpenAICompatConfig{
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "sk-openai",
				Model:   "gpt-4o",
				Timeout: 45 * time.Second,
				Headers: map[string]string{
					"X-Test": "1",
				},
			},
		},
	}
	base.ApplyDefaults()

	resolver, err := controloverlay.NewResolver(context.Background(), base, nil, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	gw.SetEffectiveConfigResolver(resolver)

	rec := doRequest(t, gw.Handler(), "GET", "/operator/models", "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/models status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload modelsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(models) error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("models count = %d, want 1", payload.Count)
	}
	if payload.DefaultProvider != "default" {
		t.Fatalf("DefaultProvider = %q, want default", payload.DefaultProvider)
	}
	if payload.AgentDefaultModel != "gpt-4o" {
		t.Fatalf("AgentDefaultModel = %q, want gpt-4o", payload.AgentDefaultModel)
	}

	item := payload.Providers[0]
	if item.Name != "default" {
		t.Fatalf("provider name = %q, want default", item.Name)
	}
	if item.Source != "openai_compat" {
		t.Fatalf("provider source = %q, want openai_compat", item.Source)
	}
	if item.Mutable {
		t.Fatal("expected synthetic openai_compat provider to be immutable")
	}
	if item.ConfigScope != "openai_compat" {
		t.Fatalf("ConfigScope = %q, want openai_compat", item.ConfigScope)
	}
	if !item.HasKey {
		t.Fatal("expected provider to report configured credentials")
	}
	if item.Timeout != "45s" {
		t.Fatalf("Timeout = %q, want 45s", item.Timeout)
	}
	if item.HeaderCount != 1 {
		t.Fatalf("HeaderCount = %d, want 1", item.HeaderCount)
	}

	routerRec := doRequest(t, gw.Handler(), "GET", "/operator/models/router", "")
	if routerRec.Code != 200 {
		t.Fatalf("GET /operator/models/router status = %d body=%s", routerRec.Code, routerRec.Body.String())
	}
	var routerPayload modelsRouterResponse
	if err := json.Unmarshal(routerRec.Body.Bytes(), &routerPayload); err != nil {
		t.Fatalf("json.Unmarshal(router) error = %v", err)
	}
	if routerPayload.DefaultProvider != "default" {
		t.Fatalf("router default provider = %q, want default", routerPayload.DefaultProvider)
	}
	if !hasRouterProfile(routerPayload.Profiles, "gpt-4o", "default") {
		t.Fatalf("expected gpt-4o default router profile in %#v", routerPayload.Profiles)
	}
}

func TestOperatorModelsListIncludesPluginProvidersAsReadOnlyEntries(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	base := config.Config{
		Server: config.ServerConfig{
			AuthToken: "test-token",
		},
		Store: config.StoreConfig{
			Backend: "memory",
		},
		Agent: config.AgentConfig{
			DefaultModel: "demo/copilot/gpt-4o",
		},
	}
	base.ApplyDefaults()

	resolver, err := controloverlay.NewResolver(context.Background(), base, nil, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	gw.SetEffectiveConfigResolver(resolver)

	manager := plugin.NewManager()
	if err := manager.Register(plugin.LoadedPlugin{
		Manifest: plugin.Manifest{
			Name: "demo",
			Providers: map[string]plugin.ProviderDecl{
				"copilot": {
					API:          "github-copilot",
					APIKey:       "ghp_test",
					DefaultModel: "gpt-4o",
				},
			},
		},
		Dir: t.TempDir(),
	}); err != nil {
		t.Fatalf("manager.Register() error = %v", err)
	}
	gw.SetModuleCatalog(modules.NewStore(modules.BuildCatalog(manager.Modules())))

	rec := doRequest(t, gw.Handler(), "GET", "/operator/models", "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/models status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload modelsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(models) error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("models count = %d, want 1", payload.Count)
	}
	if payload.DefaultProvider != "demo/copilot" {
		t.Fatalf("DefaultProvider = %q, want demo/copilot", payload.DefaultProvider)
	}

	item := payload.Providers[0]
	if item.Name != "demo/copilot" {
		t.Fatalf("provider name = %q, want demo/copilot", item.Name)
	}
	if item.Source != "plugin" {
		t.Fatalf("provider source = %q, want plugin", item.Source)
	}
	if item.Mutable {
		t.Fatal("expected plugin provider to be immutable")
	}
	if item.ConfigScope != "plugin" {
		t.Fatalf("ConfigScope = %q, want plugin", item.ConfigScope)
	}
	if !item.HasKey {
		t.Fatal("expected plugin provider to report configured credentials")
	}
	if item.CapabilityMatrix.ProviderAPI != model.APIGitHubCopilot {
		t.Fatalf("capability matrix = %+v", item.CapabilityMatrix)
	}

	routerRec := doRequest(t, gw.Handler(), "GET", "/operator/models/router", "")
	if routerRec.Code != 200 {
		t.Fatalf("GET /operator/models/router status = %d body=%s", routerRec.Code, routerRec.Body.String())
	}
	var routerPayload modelsRouterResponse
	if err := json.Unmarshal(routerRec.Body.Bytes(), &routerPayload); err != nil {
		t.Fatalf("json.Unmarshal(router) error = %v", err)
	}
	if routerPayload.DefaultProvider != "demo/copilot" {
		t.Fatalf("router default provider = %q, want demo/copilot", routerPayload.DefaultProvider)
	}
	if !hasRouterProfile(routerPayload.Profiles, "gpt-4o", "demo/copilot") {
		t.Fatalf("expected plugin router profile in %#v", routerPayload.Profiles)
	}
}

func TestOperatorConfigGetMasksLiteralSecrets(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	base := config.Config{
		Server: config.ServerConfig{AuthToken: "server-secret"},
		Store:  config.StoreConfig{Backend: "memory"},
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"openai": {
					API:          "openai-completions",
					APIKey:       "sk-openai",
					DefaultModel: "gpt-4o",
				},
			},
		},
		Channels: config.ChannelsConfig{
			Slack: config.SlackChannelConfig{
				Enabled:  boolPtrGateway(true),
				BotToken: "slack-secret",
			},
		},
	}
	base.ApplyDefaults()

	resolver, err := controloverlay.NewResolver(context.Background(), base, nil, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	gw.SetEffectiveConfigResolver(resolver)

	rec := doRequest(t, gw.Handler(), "GET", "/operator/config", "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/config status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "sk-openai") || strings.Contains(body, "slack-secret") || strings.Contains(body, "server-secret") {
		t.Fatalf("operator config leaked plaintext secret: %s", body)
	}
	if !strings.Contains(body, config.SecretPlaceholder) {
		t.Fatalf("operator config missing placeholder: %s", body)
	}
}

func TestOperatorChannelsListMasksKnownAndUnknownSecrets(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	base := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "sqlite", Path: ".hopclaw/state"},
		Channels: config.ChannelsConfig{
			Slack: config.SlackChannelConfig{
				Enabled:  boolPtrGateway(true),
				BotToken: "slack-secret",
			},
		},
	}
	base.ApplyDefaults()

	resolver, err := controloverlay.NewResolver(context.Background(), base, gatewayStaticStoreReader{
		channels: []store.ChannelConfigRow{{
			Name:   "custom_bridge",
			Config: `{"bot_token":"custom-secret","base_url":"https://example.com","nested":{"refresh_token":"refresh-secret"}}`,
			Source: store.ConfigSourceAPI,
		}},
	}, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	gw.SetEffectiveConfigResolver(resolver)

	rec := doRequest(t, gw.Handler(), "GET", "/operator/channels", "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/channels status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "slack-secret") || strings.Contains(body, "custom-secret") || strings.Contains(body, "refresh-secret") {
		t.Fatalf("operator channels leaked plaintext secret: %s", body)
	}
	if !strings.Contains(body, config.SecretPlaceholder) {
		t.Fatalf("operator channels missing placeholder: %s", body)
	}
	if !strings.Contains(body, "https://example.com") {
		t.Fatalf("operator channels unexpectedly masked non-secret fields: %s", body)
	}
}

type gatewayStaticStoreReader struct {
	providers []store.ProviderConfigRow
	channels  []store.ChannelConfigRow
	settings  []store.DynamicSettingRow
}

func (s gatewayStaticStoreReader) ListProviders(context.Context) ([]store.ProviderConfigRow, error) {
	return append([]store.ProviderConfigRow(nil), s.providers...), nil
}

func (s gatewayStaticStoreReader) ListChannels(context.Context) ([]store.ChannelConfigRow, error) {
	return append([]store.ChannelConfigRow(nil), s.channels...), nil
}

func (s gatewayStaticStoreReader) ListSettings(context.Context) ([]store.DynamicSettingRow, error) {
	return append([]store.DynamicSettingRow(nil), s.settings...), nil
}

func hasProviderSource(items []providerInfo, name, source string) bool {
	for _, item := range items {
		if item.Name == name && item.Source == source {
			return true
		}
	}
	return false
}

func hasChannelSource(items []channelInfo, name, source string) bool {
	for _, item := range items {
		if item.Name == name && item.Source == source {
			return true
		}
	}
	return false
}

func hasRouterProfile(items []modelrouter.ProfileView, id, provider string) bool {
	for _, item := range items {
		if item.ID == id && item.Provider == provider {
			return true
		}
	}
	return false
}

func boolPtrGateway(value bool) *bool {
	return &value
}
