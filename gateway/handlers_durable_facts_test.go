package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/durablefact"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/server"
	"github.com/fulcrus/hopclaw/store"
)

func TestHandleDurableFactsListAggregatesContextAndConfigViews(t *testing.T) {
	t.Parallel()

	gw, runtimeService, configStore, controlDB := newDurableFactsGateway(t)
	defer controlDB.Close()

	if _, err := runtimeService.UpsertMemoryRecord(context.Background(), agent.MemoryRecord{
		Namespace: "profile",
		ScopeKey:  "user",
		Field:     "reply_language",
		Value:     "zh-CN",
		Source:    agent.MemorySourceUser,
	}); err != nil {
		t.Fatalf("UpsertMemoryRecord() error = %v", err)
	}
	if err := configStore.UpsertProvider(context.Background(), &store.ProviderConfigRow{
		Name:         "openai",
		API:          "openai-responses",
		DefaultModel: "gpt-4.1",
		Source:       store.ConfigSourceAPI,
	}); err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/durable-facts", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/durable-facts status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload durableFactsOperatorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.View != "operator" || payload.Count != 2 || payload.ContextCount != 1 || payload.ConfigCount != 1 {
		t.Fatalf("unexpected payload meta: %#v", payload)
	}
	if payload.Items[0].ViewType != durablefact.ViewTypeConfigProvider || payload.Items[1].ViewType != durablefact.ViewTypeContext {
		t.Fatalf("unexpected item order: %#v", payload.Items)
	}
}

func TestHandleDurableFactsListSupportsViewAndFilterQueries(t *testing.T) {
	t.Parallel()

	gw, runtimeService, _, controlDB := newDurableFactsGateway(t)
	defer controlDB.Close()

	if _, err := runtimeService.UpsertMemoryRecord(context.Background(), agent.MemoryRecord{
		Namespace: "profile",
		ScopeKey:  "user",
		Field:     "timezone",
		Value:     "Asia/Shanghai",
		Source:    agent.MemorySourceUser,
	}); err != nil {
		t.Fatalf("UpsertMemoryRecord(profile) error = %v", err)
	}
	if _, err := runtimeService.UpsertMemoryRecord(context.Background(), agent.MemoryRecord{
		Namespace: "workspace",
		ScopeKey:  "repo",
		Field:     "name",
		Value:     "HopClaw",
		Source:    agent.MemorySourceUser,
	}); err != nil {
		t.Fatalf("UpsertMemoryRecord(workspace) error = %v", err)
	}

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/durable-facts?view=context&namespace=profile&q=timezone", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/durable-facts?view=context status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload durableFactsContextResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.View != "context" || payload.Count != 1 || payload.Items[0].Field != "timezone" {
		t.Fatalf("unexpected context payload: %#v", payload)
	}
}

func TestHandleDurableFactsListSupportsReviewRequiredConfigFilter(t *testing.T) {
	t.Parallel()

	gw, _, _, controlDB := newDurableFactsGateway(t)
	defer controlDB.Close()

	facts, err := durablefact.NewSQLiteStore(controlDB)
	if err != nil {
		t.Fatalf("durablefact.NewSQLiteStore() error = %v", err)
	}
	if _, err := facts.Upsert(context.Background(), durablefact.Fact{
		Key:            "config.setting.section.agent",
		FactClass:      durablefact.FactClassSystemConfig,
		ViewType:       durablefact.ViewTypeConfigSetting,
		Namespace:      "setting",
		Name:           "section.agent",
		Value:          `{"default_model":"gpt-4.1"}`,
		ValueType:      durablefact.ValueTypeJSON,
		ReviewRequired: true,
	}); err != nil {
		t.Fatalf("facts.Upsert() error = %v", err)
	}

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/durable-facts?view=config&review_required=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/durable-facts?view=config status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload durableFactsConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.View != "config" || payload.Count != 1 || payload.ReviewRequiredCount != 1 || !payload.Items[0].ReviewRequired {
		t.Fatalf("unexpected config payload: %#v", payload)
	}
}

func TestHandleDurableFactsListRejectsInvalidFilter(t *testing.T) {
	t.Parallel()

	gw, _, _, controlDB := newDurableFactsGateway(t)
	defer controlDB.Close()

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/durable-facts?review_required=definitely-not-bool", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /operator/durable-facts invalid filter status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDurableFactsListMasksStoredConfigSecrets(t *testing.T) {
	t.Parallel()

	gw, _, configStore, controlDB := newDurableFactsGateway(t)
	defer controlDB.Close()

	if err := configStore.UpsertProvider(context.Background(), &store.ProviderConfigRow{
		Name:         "openai",
		API:          "openai-responses",
		APIKey:       "provider-secret",
		SecretKey:    "provider-secret-key",
		Headers:      `{"Authorization":"Bearer provider-header-secret"}`,
		DefaultModel: "gpt-4.1",
		Source:       store.ConfigSourceAPI,
	}); err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if err := configStore.UpsertChannel(context.Background(), &store.ChannelConfigRow{
		Name:   "custom_bridge",
		Config: `{"bot_token":"channel-secret","nested":{"refresh_token":"channel-refresh"},"base_url":"https://example.com"}`,
		Source: store.ConfigSourceAPI,
	}); err != nil {
		t.Fatalf("UpsertChannel() error = %v", err)
	}
	if err := configStore.UpsertSetting(context.Background(), &store.DynamicSettingRow{
		Key:    config.SectionOverlayKey("auth"),
		Value:  `{"bearer_token":"section-secret"}`,
		Source: store.ConfigSourceAPI,
	}); err != nil {
		t.Fatalf("UpsertSetting() error = %v", err)
	}

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/durable-facts?view=config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/durable-facts?view=config status = %d body=%s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{"provider-secret", "provider-secret-key", "provider-header-secret", "channel-secret", "channel-refresh", "section-secret"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("config durable facts leaked %q in body=%s", secret, rec.Body.String())
		}
	}

	var configPayload durableFactsConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &configPayload); err != nil {
		t.Fatalf("json.Unmarshal(config) error = %v", err)
	}

	seenProvider := false
	seenChannel := false
	seenSetting := false
	for _, item := range configPayload.Items {
		switch {
		case item.Kind == durablefact.ConfigViewKindProvider && item.Name == "openai":
			seenProvider = true
			var payload durableFactProviderPayload
			if err := json.Unmarshal([]byte(item.Payload), &payload); err != nil {
				t.Fatalf("decode provider payload: %v", err)
			}
			if payload.APIKey != config.SecretPlaceholder || payload.SecretKey != config.SecretPlaceholder {
				t.Fatalf("unexpected sanitized provider payload: %#v", payload)
			}
			var headers map[string]string
			if err := json.Unmarshal([]byte(payload.Headers), &headers); err != nil {
				t.Fatalf("decode provider headers: %v", err)
			}
			if headers["Authorization"] != config.SecretPlaceholder {
				t.Fatalf("provider authorization header = %q, want %q", headers["Authorization"], config.SecretPlaceholder)
			}
		case item.Kind == durablefact.ConfigViewKindChannel && item.Name == "custom_bridge":
			seenChannel = true
			var payload durableFactChannelPayload
			if err := json.Unmarshal([]byte(item.Payload), &payload); err != nil {
				t.Fatalf("decode channel payload: %v", err)
			}
			var channelConfig map[string]any
			if err := json.Unmarshal([]byte(payload.Config), &channelConfig); err != nil {
				t.Fatalf("decode channel config: %v", err)
			}
			if got := channelConfig["bot_token"]; got != config.SecretPlaceholder {
				t.Fatalf("channel bot_token = %#v, want %q", got, config.SecretPlaceholder)
			}
			nested, _ := channelConfig["nested"].(map[string]any)
			if got := nested["refresh_token"]; got != config.SecretPlaceholder {
				t.Fatalf("channel refresh_token = %#v, want %q", got, config.SecretPlaceholder)
			}
			if got := channelConfig["base_url"]; got != "https://example.com" {
				t.Fatalf("channel base_url = %#v, want https://example.com", got)
			}
		case item.Kind == durablefact.ConfigViewKindSetting && item.Name == config.SectionOverlayKey("auth"):
			seenSetting = true
			var payload durableFactSettingPayload
			if err := json.Unmarshal([]byte(item.Payload), &payload); err != nil {
				t.Fatalf("decode setting payload: %v", err)
			}
			var settingValue map[string]any
			if err := json.Unmarshal([]byte(payload.Value), &settingValue); err != nil {
				t.Fatalf("decode setting value: %v", err)
			}
			if got := settingValue["bearer_token"]; got != config.SecretPlaceholder {
				t.Fatalf("setting bearer_token = %#v, want %q", got, config.SecretPlaceholder)
			}
		}
	}
	if !seenProvider || !seenChannel || !seenSetting {
		t.Fatalf("missing sanitized config items provider=%v channel=%v setting=%v payload=%#v", seenProvider, seenChannel, seenSetting, configPayload.Items)
	}

	rec = doRequest(t, gw.Handler(), http.MethodGet, "/operator/durable-facts", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/durable-facts status = %d body=%s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{"provider-secret", "provider-secret-key", "provider-header-secret", "channel-secret", "channel-refresh", "section-secret"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("operator durable facts leaked %q in body=%s", secret, rec.Body.String())
		}
	}
}

func newDurableFactsGateway(t *testing.T) (*Gateway, *runtimepkg.Service, *store.ConfigStore, *sql.DB) {
	t.Helper()

	memoryStore, err := agent.NewSQLiteKVStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("agent.NewSQLiteKVStore() error = %v", err)
	}
	t.Cleanup(func() { _ = memoryStore.Close() })

	runtimeService := runtimepkg.NewService(nil, agent.NewInMemorySessionStore(), agent.NewInMemoryRunStore(), nil, nil, nil).
		WithMemoryStore(agent.NewGovernedMemoryStore(memoryStore))
	gw := gatewayFromServer(server.New(runtimeService, server.Config{AuthToken: "test-token"}), Config{
		AuthToken: "test-token",
		Runtime:   runtimeService,
	})

	controlDB, err := store.OpenDB(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatalf("store.OpenDB() error = %v", err)
	}
	configStore, err := store.NewConfigStore(controlDB)
	if err != nil {
		_ = controlDB.Close()
		t.Fatalf("store.NewConfigStore() error = %v", err)
	}
	gw.SetConfigStore(configStore)
	return gw, runtimeService, configStore, controlDB
}
