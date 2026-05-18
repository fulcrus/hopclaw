package overlay

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/store"
)

const defaultMutationTimeout = 5 * time.Second

var (
	ErrMutationUnavailable        = controlplane.ErrMutationUnavailable
	ErrEffectiveConfigUnavailable = controlplane.ErrEffectiveConfigUnavailable
)

var log = logging.WithSubsystem("controlplane.overlay")

type MutationService struct {
	store          *store.ConfigStore
	provider       Provider
	settings       *SettingPolicyRegistry
	refresh        func(context.Context) error
	mutationTimout time.Duration
}

type MutationOptions struct {
	Settings        *SettingPolicyRegistry
	Refresh         func(context.Context) error
	MutationTimeout time.Duration
}

func NewMutationService(store *store.ConfigStore, provider Provider, opts MutationOptions) *MutationService {
	settings := opts.Settings
	if settings == nil {
		settings = DefaultSettingPolicyRegistry()
	}
	timeout := opts.MutationTimeout
	if timeout <= 0 {
		timeout = defaultMutationTimeout
	}
	return &MutationService{
		store:          store,
		provider:       provider,
		settings:       settings,
		refresh:        opts.Refresh,
		mutationTimout: timeout,
	}
}

func (s *MutationService) SetProvider(provider Provider) {
	if s == nil {
		return
	}
	s.provider = provider
}

func (s *MutationService) Available() bool {
	return s != nil && s.store != nil
}

func (s *MutationService) PutSection(ctx context.Context, section string, sectionValue any) error {
	if !s.Available() {
		return ErrMutationUnavailable
	}
	current, ok := s.currentConfig()
	if !ok {
		return ErrEffectiveConfigUnavailable
	}
	next, normalizedSection, cleanup, err := config.NormalizeSectionForStore(current, section, sectionValue)
	if err != nil {
		return err
	}
	if diff := config.Diff(current, next); diff.Fatal {
		return fmt.Errorf("config section %q requires restart and cannot be mutated online", strings.TrimSpace(section))
	}

	key, err := s.settings.OverlayKeyForSection(section)
	if err != nil {
		return err
	}
	if err := s.writeSetting(ctx, key, normalizedSection); err != nil {
		return err
	}
	if err := config.CleanupManagedSecretRefs(cleanup); err != nil {
		log.Warn("cleanup section secrets failed", "section", section, "error", err)
	}
	return nil
}

func (s *MutationService) UpsertProvider(ctx context.Context, row store.ProviderConfigRow) error {
	if !s.Available() {
		return ErrMutationUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, s.mutationTimout)
	defer cancel()

	row.Name = strings.TrimSpace(row.Name)
	row.Source = store.ConfigSourceAPI
	row.YAMLHash = ""
	if row.Name == "" {
		return fmt.Errorf("provider name is required")
	}
	currentCfg := s.currentProviderConfig(row.Name)
	normalized, cleanup, err := config.NormalizeProviderConfigForStore(row.Name, currentCfg, config.RowToProviderConfig(&row))
	if err != nil {
		return err
	}
	headersJSON := "{}"
	if len(normalized.Headers) > 0 {
		data, marshalErr := json.Marshal(normalized.Headers)
		if marshalErr != nil {
			return fmt.Errorf("encode provider headers %q: %w", row.Name, marshalErr)
		}
		headersJSON = string(data)
	}
	row.API = normalized.API
	row.BaseURL = normalized.BaseURL
	row.Region = normalized.Region
	row.APIKey = normalized.APIKey
	row.APIKeys = append([]string(nil), normalized.APIKeys...)
	row.AccessKeyID = normalized.AccessKeyID
	row.SecretKey = normalized.SecretKey
	row.SessionToken = normalized.SessionToken
	row.DefaultModel = normalized.DefaultModel
	row.TimeoutSec = int(normalized.Timeout / time.Second)
	row.Headers = headersJSON
	if err := s.store.UpsertProvider(ctx, &row); err != nil {
		return err
	}
	if err := config.CleanupManagedSecretRefs(cleanup); err != nil {
		log.Warn("cleanup provider secrets failed", "provider", row.Name, "error", err)
	}
	return s.refreshEffectiveConfig(ctx)
}

func (s *MutationService) DeleteProvider(ctx context.Context, name string, basePresent bool) error {
	if !s.Available() {
		return ErrMutationUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, s.mutationTimout)
	defer cancel()

	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("provider name is required")
	}
	currentCfg := s.currentProviderConfig(name)
	cleanup := cleanupProviderSecrets(currentCfg)
	if basePresent {
		disabled := false
		row := store.ProviderConfigRow{
			Name:    name,
			Source:  store.ConfigSourceAPI,
			Enabled: &disabled,
		}
		if err := s.store.UpsertProvider(ctx, &row); err != nil {
			return err
		}
	} else {
		if err := s.store.DeleteProvider(ctx, name); err != nil {
			return err
		}
	}
	if err := config.CleanupManagedSecretRefs(cleanup); err != nil {
		log.Warn("cleanup provider secrets failed", "provider", name, "error", err)
	}
	return s.refreshEffectiveConfig(ctx)
}

func (s *MutationService) UpsertChannel(ctx context.Context, row store.ChannelConfigRow) error {
	if !s.Available() {
		return ErrMutationUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, s.mutationTimout)
	defer cancel()

	row.Name = strings.TrimSpace(row.Name)
	row.Source = store.ConfigSourceAPI
	row.YAMLHash = ""
	if row.Name == "" {
		return fmt.Errorf("channel name is required")
	}
	if strings.TrimSpace(row.Config) == "" {
		row.Config = "{}"
	}
	currentRaw := s.currentChannelConfig(row.Name)
	normalized, _, cleanup, err := config.NormalizeChannelConfigForStore(row.Name, json.RawMessage(currentRaw), json.RawMessage(row.Config))
	if err != nil {
		return err
	}
	row.Config = string(normalized)
	if err := s.store.UpsertChannel(ctx, &row); err != nil {
		return err
	}
	if err := config.CleanupManagedSecretRefs(cleanup); err != nil {
		log.Warn("cleanup channel secrets failed", "channel", row.Name, "error", err)
	}
	return s.refreshEffectiveConfig(ctx)
}

func (s *MutationService) DeleteChannel(ctx context.Context, name string, basePresent bool) error {
	if !s.Available() {
		return ErrMutationUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, s.mutationTimout)
	defer cancel()

	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("channel name is required")
	}
	currentRaw := s.currentChannelConfig(name)
	cleanup := cleanupChannelSecrets(name, currentRaw)
	if basePresent {
		disabled := false
		row := store.ChannelConfigRow{
			Name:    name,
			Config:  "{}",
			Enabled: &disabled,
			Source:  store.ConfigSourceAPI,
		}
		if err := s.store.UpsertChannel(ctx, &row); err != nil {
			return err
		}
	} else {
		if err := s.store.DeleteChannel(ctx, name); err != nil {
			return err
		}
	}
	if err := config.CleanupManagedSecretRefs(cleanup); err != nil {
		log.Warn("cleanup channel secrets failed", "channel", name, "error", err)
	}
	return s.refreshEffectiveConfig(ctx)
}

func (s *MutationService) writeSetting(ctx context.Context, key string, sectionValue any) error {
	ctx, cancel := context.WithTimeout(ctx, s.mutationTimout)
	defer cancel()

	if sectionValue == nil {
		err := s.store.DeleteSetting(ctx, key)
		if err != nil && err != store.ErrNotFound {
			return err
		}
		return s.refreshEffectiveConfig(ctx)
	}
	payload, err := json.Marshal(sectionValue)
	if err != nil {
		return fmt.Errorf("marshal setting %q: %w", key, err)
	}
	if err := s.store.UpsertSetting(ctx, &store.DynamicSettingRow{
		Key:    key,
		Value:  string(payload),
		Source: store.ConfigSourceAPI,
	}); err != nil {
		return err
	}
	return s.refreshEffectiveConfig(ctx)
}

func (s *MutationService) refreshEffectiveConfig(ctx context.Context) error {
	if s == nil || s.refresh == nil {
		return nil
	}
	return s.refresh(ctx)
}

func (s *MutationService) currentConfig() (config.Config, bool) {
	if s == nil || s.provider == nil {
		return config.Config{}, false
	}
	return s.provider.Current(), true
}

func (s *MutationService) currentProviderConfig(name string) config.ProviderConfig {
	current, ok := s.currentConfig()
	if !ok || current.Models.Providers == nil {
		return config.ProviderConfig{}
	}
	return current.Models.Providers[strings.TrimSpace(name)]
}

func (s *MutationService) currentChannelConfig(name string) string {
	if s == nil || s.provider == nil {
		return ""
	}
	name = strings.TrimSpace(name)
	for _, item := range s.provider.Channels() {
		if item.Name == name {
			return string(item.Config)
		}
	}
	return ""
}

func cleanupProviderSecrets(current config.ProviderConfig) []string {
	return config.ProviderSecretCleanupRefs(current)
}

func cleanupChannelSecrets(name string, currentRaw string) []string {
	_, _, cleanup, err := config.NormalizeChannelConfigForStore(name, json.RawMessage(currentRaw), json.RawMessage(`{}`))
	if err != nil {
		return nil
	}
	return cleanup
}
