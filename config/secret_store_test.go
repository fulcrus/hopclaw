package config

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/store"
)

type configTestKeychainStore struct {
	mu      sync.Mutex
	secrets map[string]map[string]string
}

func newConfigTestKeychainStore() *configTestKeychainStore {
	return &configTestKeychainStore{secrets: map[string]map[string]string{}}
}

func (s *configTestKeychainStore) Get(service, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	group := s.secrets[service]
	if group == nil {
		return "", keychain.ErrNotFound
	}
	value, ok := group[key]
	if !ok {
		return "", keychain.ErrNotFound
	}
	return value, nil
}

func (s *configTestKeychainStore) Set(service, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	group := s.secrets[service]
	if group == nil {
		group = map[string]string{}
		s.secrets[service] = group
	}
	group[key] = value
	return nil
}

func (s *configTestKeychainStore) Delete(service, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	group := s.secrets[service]
	if group == nil {
		return keychain.ErrNotFound
	}
	if _, ok := group[key]; !ok {
		return keychain.ErrNotFound
	}
	delete(group, key)
	if len(group) == 0 {
		delete(s.secrets, service)
	}
	return nil
}

func installConfigTestKeychainStore(t *testing.T) *configTestKeychainStore {
	t.Helper()
	store := newConfigTestKeychainStore()
	original := keychain.CurrentStore()
	keychain.SetStore(store)
	t.Cleanup(func() { keychain.SetStore(original) })
	return store
}

func TestNormalizeProviderConfigForStoreStoresManagedRefs(t *testing.T) {
	store := installConfigTestKeychainStore(t)

	current := ProviderConfig{
		SecretKey: "keychain:config.providers.openai.secret_key",
	}
	next := ProviderConfig{
		APIKey:    "sk-live",
		SecretKey: "env:AWS_SECRET_KEY",
	}

	normalized, cleanup, err := NormalizeProviderConfigForStore("openai", current, next)
	if err != nil {
		t.Fatalf("NormalizeProviderConfigForStore() error = %v", err)
	}
	if normalized.APIKey != "keychain:config.providers.openai.api_key" {
		t.Fatalf("APIKey = %q", normalized.APIKey)
	}
	if normalized.SecretKey != "env:AWS_SECRET_KEY" {
		t.Fatalf("SecretKey = %q", normalized.SecretKey)
	}
	if got, err := store.Get(keychain.DefaultService(), "config.providers.openai.api_key"); err != nil || got != "sk-live" {
		t.Fatalf("stored api key = (%q, %v), want (sk-live, nil)", got, err)
	}
	wantCleanup := []string{"keychain:config.providers.openai.secret_key"}
	if !reflect.DeepEqual(cleanup, wantCleanup) {
		t.Fatalf("cleanup = %#v, want %#v", cleanup, wantCleanup)
	}
}

func TestNormalizeSectionForStorePreservesMaskedSecretRefs(t *testing.T) {
	current := Config{
		Auth: AuthConfig{
			BearerToken: "keychain:config.sections.auth.bearer_token",
		},
	}

	nextCfg, payload, cleanup, err := NormalizeSectionForStore(current, "auth", map[string]any{
		"bearer_token": "***",
	})
	if err != nil {
		t.Fatalf("NormalizeSectionForStore() error = %v", err)
	}
	if nextCfg.Auth.BearerToken != "keychain:config.sections.auth.bearer_token" {
		t.Fatalf("next bearer token = %q", nextCfg.Auth.BearerToken)
	}
	payloadMap, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", payload)
	}
	if got := payloadMap["bearer_token"]; got != "keychain:config.sections.auth.bearer_token" {
		t.Fatalf("payload bearer_token = %#v", got)
	}
	if len(cleanup) != 0 {
		t.Fatalf("cleanup = %#v, want empty", cleanup)
	}
}

func TestMergeChannelConfigPreservesUnknownSecretPlaceholders(t *testing.T) {
	merged, recognized, err := MergeChannelConfig("custom_bridge", json.RawMessage(`{
		"bot_token":"secret-token",
		"nested":{"refresh_token":"refresh-secret"},
		"base_url":"https://example.com"
	}`), json.RawMessage(`{
		"bot_token":"***",
		"nested":{"refresh_token":"***"},
		"base_url":"https://new.example.com"
	}`))
	if err != nil {
		t.Fatalf("MergeChannelConfig() error = %v", err)
	}
	if recognized {
		t.Fatal("recognized = true, want false for custom channel")
	}
	var payload map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("decode merged payload: %v", err)
	}
	if got := payload["bot_token"]; got != "secret-token" {
		t.Fatalf("bot_token = %#v", got)
	}
	nested, _ := payload["nested"].(map[string]any)
	if got := nested["refresh_token"]; got != "refresh-secret" {
		t.Fatalf("refresh_token = %#v", got)
	}
	if got := payload["base_url"]; got != "https://new.example.com" {
		t.Fatalf("base_url = %#v", got)
	}
}

func TestNormalizeChannelConfigForStoreRewritesUnknownSecrets(t *testing.T) {
	store := installConfigTestKeychainStore(t)

	normalized, recognized, cleanup, err := NormalizeChannelConfigForStore("custom_bridge", nil, json.RawMessage(`{
		"bot_token":"secret-token",
		"imei":"8675309",
		"base_url":"https://example.com",
		"nested":{"refresh_token":"refresh-secret"}
	}`))
	if err != nil {
		t.Fatalf("NormalizeChannelConfigForStore() error = %v", err)
	}
	if recognized {
		t.Fatal("recognized = true, want false for custom channel")
	}
	if len(cleanup) != 0 {
		t.Fatalf("cleanup = %#v, want empty", cleanup)
	}
	var payload map[string]any
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("decode normalized payload: %v", err)
	}
	if got := payload["bot_token"]; got != "keychain:config.channels.custom_bridge.bot_token" {
		t.Fatalf("bot_token = %#v", got)
	}
	if got := payload["imei"]; got != "keychain:config.channels.custom_bridge.imei" {
		t.Fatalf("imei = %#v", got)
	}
	if got := payload["base_url"]; got != "https://example.com" {
		t.Fatalf("base_url = %#v", got)
	}
	nested, _ := payload["nested"].(map[string]any)
	if got := nested["refresh_token"]; got != "keychain:config.channels.custom_bridge.nested.refresh_token" {
		t.Fatalf("refresh_token = %#v", got)
	}
	if got, err := store.Get(keychain.DefaultService(), "config.channels.custom_bridge.bot_token"); err != nil || got != "secret-token" {
		t.Fatalf("stored bot token = (%q, %v), want (secret-token, nil)", got, err)
	}
}

func TestMigrateConfigStoreSecretsRewritesExistingRows(t *testing.T) {
	keychainStore := installConfigTestKeychainStore(t)

	db, err := store.OpenDB(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	defer db.Close()

	configStore, err := store.NewConfigStore(db)
	if err != nil {
		t.Fatalf("NewConfigStore() error = %v", err)
	}

	ctx := context.Background()
	if err := configStore.UpsertProvider(ctx, &store.ProviderConfigRow{
		Name:         "openai",
		API:          "openai",
		APIKey:       "provider-secret",
		Headers:      `{"authorization":"Bearer provider-header"}`,
		Source:       store.ConfigSourceAPI,
		DefaultModel: "gpt-5",
	}); err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if err := configStore.UpsertChannel(ctx, &store.ChannelConfigRow{
		Name:   "custom_bridge",
		Config: `{"bot_token":"channel-secret","nested":{"refresh_token":"channel-refresh"},"base_url":"https://example.com"}`,
		Source: store.ConfigSourceAPI,
	}); err != nil {
		t.Fatalf("UpsertChannel() error = %v", err)
	}
	if err := configStore.UpsertSetting(ctx, &store.DynamicSettingRow{
		Key:    SectionOverlayKey("auth"),
		Value:  `{"bearer_token":"section-secret"}`,
		Source: store.ConfigSourceAPI,
	}); err != nil {
		t.Fatalf("UpsertSetting() error = %v", err)
	}

	if err := MigrateConfigStoreSecrets(ctx, configStore); err != nil {
		t.Fatalf("MigrateConfigStoreSecrets() error = %v", err)
	}
	if err := MigrateConfigStoreSecrets(ctx, configStore); err != nil {
		t.Fatalf("MigrateConfigStoreSecrets() second error = %v", err)
	}

	providerRow, err := configStore.GetProvider(ctx, "openai")
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	if providerRow.APIKey != "keychain:config.providers.openai.api_key" {
		t.Fatalf("provider APIKey = %q", providerRow.APIKey)
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(providerRow.Headers), &headers); err != nil {
		t.Fatalf("decode provider headers: %v", err)
	}
	if got := headers["Authorization"]; got != "keychain:config.providers.openai.headers.Authorization" {
		t.Fatalf("provider authorization header = %q", got)
	}

	channelRow, err := configStore.GetChannel(ctx, "custom_bridge")
	if err != nil {
		t.Fatalf("GetChannel() error = %v", err)
	}
	var channelConfig map[string]any
	if err := json.Unmarshal([]byte(channelRow.Config), &channelConfig); err != nil {
		t.Fatalf("decode channel config: %v", err)
	}
	if got := channelConfig["bot_token"]; got != "keychain:config.channels.custom_bridge.bot_token" {
		t.Fatalf("channel bot_token = %#v", got)
	}
	nested, _ := channelConfig["nested"].(map[string]any)
	if got := nested["refresh_token"]; got != "keychain:config.channels.custom_bridge.nested.refresh_token" {
		t.Fatalf("channel refresh_token = %#v", got)
	}
	if got := channelConfig["base_url"]; got != "https://example.com" {
		t.Fatalf("channel base_url = %#v", got)
	}

	settingRow, err := configStore.GetSetting(ctx, SectionOverlayKey("auth"))
	if err != nil {
		t.Fatalf("GetSetting() error = %v", err)
	}
	var settingValue map[string]any
	if err := json.Unmarshal([]byte(settingRow.Value), &settingValue); err != nil {
		t.Fatalf("decode setting value: %v", err)
	}
	if got := settingValue["bearer_token"]; got != "keychain:config.sections.auth.bearer_token" {
		t.Fatalf("setting bearer_token = %#v", got)
	}

	if got, err := keychainStore.Get(keychain.DefaultService(), "config.providers.openai.api_key"); err != nil || got != "provider-secret" {
		t.Fatalf("stored provider api key = (%q, %v), want (provider-secret, nil)", got, err)
	}
	if got, err := keychainStore.Get(keychain.DefaultService(), "config.providers.openai.headers.Authorization"); err != nil || got != "Bearer provider-header" {
		t.Fatalf("stored provider header = (%q, %v), want (Bearer provider-header, nil)", got, err)
	}
	if got, err := keychainStore.Get(keychain.DefaultService(), "config.channels.custom_bridge.bot_token"); err != nil || got != "channel-secret" {
		t.Fatalf("stored channel bot_token = (%q, %v), want (channel-secret, nil)", got, err)
	}
	if got, err := keychainStore.Get(keychain.DefaultService(), "config.channels.custom_bridge.nested.refresh_token"); err != nil || got != "channel-refresh" {
		t.Fatalf("stored channel refresh_token = (%q, %v), want (channel-refresh, nil)", got, err)
	}
	if got, err := keychainStore.Get(keychain.DefaultService(), "config.sections.auth.bearer_token"); err != nil || got != "section-secret" {
		t.Fatalf("stored section bearer_token = (%q, %v), want (section-secret, nil)", got, err)
	}
}
