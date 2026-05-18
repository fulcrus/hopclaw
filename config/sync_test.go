package config

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/store"
)

func TestDefaultSyncOptionsPreferYAMLAndPruneMissingRows(t *testing.T) {
	installConfigTestKeychainStore(t)

	db, err := store.OpenDB(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	defer db.Close()

	configStore, err := store.NewConfigStore(db)
	if err != nil {
		t.Fatalf("NewConfigStore() error = %v", err)
	}

	cfg := Config{
		Models: ModelsConfig{
			Providers: map[string]ProviderConfig{
				"main": {API: "openai", APIKey: "secret"},
			},
		},
		Channels: ChannelsConfig{
			Slack: SlackChannelConfig{Enabled: boolPtrConfig(true)},
		},
	}

	opts := DefaultSyncOptions()
	if opts.DefaultMerge != store.MergeYAMLWins {
		t.Fatalf("DefaultMerge = %q, want %q", opts.DefaultMerge, store.MergeYAMLWins)
	}

	syncer := NewConfigSyncer(configStore, opts)
	if _, err := syncer.Sync(context.Background(), &cfg); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if err := configStore.UpsertProvider(context.Background(), &store.ProviderConfigRow{
		Name:     "overlay",
		API:      "anthropic",
		APIKey:   "api",
		Source:   store.ConfigSourceAPI,
		YAMLHash: "",
	}); err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if err := configStore.UpsertChannel(context.Background(), &store.ChannelConfigRow{
		Name:   "discord",
		Config: `{"enabled":true}`,
		Source: store.ConfigSourceAPI,
	}); err != nil {
		t.Fatalf("UpsertChannel() error = %v", err)
	}

	if _, err := syncer.Sync(context.Background(), &cfg); err != nil {
		t.Fatalf("Sync() second error = %v", err)
	}

	if _, err := configStore.GetProvider(context.Background(), "overlay"); err != store.ErrNotFound {
		t.Fatalf("GetProvider(overlay) error = %v, want ErrNotFound", err)
	}
	if _, err := configStore.GetChannel(context.Background(), "discord"); err != store.ErrNotFound {
		t.Fatalf("GetChannel(discord) error = %v, want ErrNotFound", err)
	}
	if _, err := configStore.GetProvider(context.Background(), "main"); err != nil {
		t.Fatalf("GetProvider(main) error = %v", err)
	}
	if _, err := configStore.GetChannel(context.Background(), "slack"); err != nil {
		t.Fatalf("GetChannel(slack) error = %v", err)
	}
}

func TestConfigSyncReturnsAccumulatedErrors(t *testing.T) {
	original := keychain.CurrentStore()
	keychain.SetStore(failingConfigSyncKeychainStore{})
	t.Cleanup(func() { keychain.SetStore(original) })

	db, err := store.OpenDB(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	defer db.Close()

	configStore, err := store.NewConfigStore(db)
	if err != nil {
		t.Fatalf("NewConfigStore() error = %v", err)
	}

	cfg := Config{
		Models: ModelsConfig{
			Providers: map[string]ProviderConfig{
				"main": {API: "openai", APIKey: "secret"},
			},
		},
	}

	result, err := NewConfigSyncer(configStore, DefaultSyncOptions()).Sync(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected Sync() to return accumulated provider error")
	}
	if !strings.Contains(err.Error(), "save managed secret") {
		t.Fatalf("Sync() error = %v, want save managed secret failure", err)
	}
	if result == nil || len(result.Errors) != 1 {
		t.Fatalf("result.Errors = %#v, want exactly one accumulated error", result.Errors)
	}
	if _, getErr := configStore.GetProvider(context.Background(), "main"); !errors.Is(getErr, store.ErrNotFound) {
		t.Fatalf("GetProvider(main) error = %v, want ErrNotFound", getErr)
	}
}

func boolPtrConfig(v bool) *bool {
	return &v
}

type failingConfigSyncKeychainStore struct{}

func (failingConfigSyncKeychainStore) Get(service, key string) (string, error) {
	return "", keychain.ErrNotFound
}

func (failingConfigSyncKeychainStore) Set(service, key, value string) error {
	return errors.New("boom")
}

func (failingConfigSyncKeychainStore) Delete(service, key string) error {
	return keychain.ErrNotFound
}
