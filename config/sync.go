package config

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/store"
)

// ---------------------------------------------------------------------------
// Config Sync
// ---------------------------------------------------------------------------

// SyncOptions controls how YAML config is synced to SQLite.
type SyncOptions struct {
	// DefaultMerge is the default merge strategy (default: yaml_wins).
	DefaultMerge store.MergeStrategy

	// ProviderMerges overrides merge strategy per provider name.
	ProviderMerges map[string]store.MergeStrategy

	// ChannelMerges overrides merge strategy per channel name.
	ChannelMerges map[string]store.MergeStrategy

	// Logger for sync operations.
	Logger *slog.Logger
}

// DefaultSyncOptions returns default sync options.
func DefaultSyncOptions() SyncOptions {
	return SyncOptions{
		DefaultMerge:   store.MergeYAMLWins,
		ProviderMerges: make(map[string]store.MergeStrategy),
		ChannelMerges:  make(map[string]store.MergeStrategy),
	}
}

// SyncResult contains the results of a config sync operation.
type SyncResult struct {
	ProvidersImported []string
	ProvidersUpdated  []string
	ProvidersSkipped  []string
	ChannelsImported  []string
	ChannelsUpdated   []string
	ChannelsSkipped   []string
	Errors            []error
}

// ConfigSyncer synchronizes YAML config to SQLite.
type ConfigSyncer struct {
	store *store.ConfigStore
	opts  SyncOptions
	log   *slog.Logger
}

// NewConfigSyncer creates a new config syncer.
func NewConfigSyncer(s *store.ConfigStore, opts SyncOptions) *ConfigSyncer {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &ConfigSyncer{
		store: s,
		opts:  opts,
		log:   log,
	}
}

// Sync synchronizes the given YAML config to SQLite.
func (s *ConfigSyncer) Sync(ctx context.Context, cfg *Config) (*SyncResult, error) {
	result := &SyncResult{}
	providerNames := make(map[string]struct{})
	var channelNames map[string]struct{}

	// Sync providers
	if cfg.Models.Providers != nil {
		for name, pc := range cfg.Models.Providers {
			providerNames[name] = struct{}{}
			if err := s.syncProvider(ctx, name, pc, result); err != nil {
				result.Errors = append(result.Errors, err)
			}
		}
	}

	// Sync channels
	channelNames = s.syncChannels(ctx, cfg, result)

	if s.opts.DefaultMerge == store.MergeYAMLWins {
		if err := s.pruneProviders(ctx, providerNames); err != nil {
			result.Errors = append(result.Errors, err)
		}
		if err := s.pruneChannels(ctx, channelNames); err != nil {
			result.Errors = append(result.Errors, err)
		}
	}

	if len(result.Errors) > 0 {
		return result, errors.Join(result.Errors...)
	}
	return result, nil
}

// syncProvider syncs a single provider config.
func (s *ConfigSyncer) syncProvider(ctx context.Context, name string, pc ProviderConfig, result *SyncResult) error {
	yamlHash := store.HashConfig(pc)
	currentCfg := ProviderConfig{}

	// Check if exists in SQLite
	existing, err := s.store.GetProvider(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		// New provider, import from YAML
		normalized, cleanup, normErr := NormalizeProviderConfigForStore(name, currentCfg, pc)
		if normErr != nil {
			return normErr
		}
		row := providerConfigToRow(name, normalized, store.ConfigSourceYAML, yamlHash)
		if err := s.store.UpsertProvider(ctx, row); err != nil {
			return err
		}
		if err := CleanupManagedSecretRefs(cleanup); err != nil {
			s.log.Warn("config sync: cleanup provider secrets failed", "name", name, "error", err)
		}
		result.ProvidersImported = append(result.ProvidersImported, name)
		s.log.Info("config sync: imported provider from YAML", "name", name)
		return nil
	}
	if err != nil {
		return err
	}
	currentCfg = RowToProviderConfig(existing)

	// Determine merge strategy
	merge := s.opts.DefaultMerge
	if m, ok := s.opts.ProviderMerges[name]; ok {
		merge = m
	}

	switch merge {
	case store.MergeYAMLWins:
		// YAML always overwrites
		normalized, cleanup, normErr := NormalizeProviderConfigForStore(name, currentCfg, pc)
		if normErr != nil {
			return normErr
		}
		row := providerConfigToRow(name, normalized, store.ConfigSourceYAML, yamlHash)
		if err := s.store.UpsertProvider(ctx, row); err != nil {
			return err
		}
		if err := CleanupManagedSecretRefs(cleanup); err != nil {
			s.log.Warn("config sync: cleanup provider secrets failed", "name", name, "error", err)
		}
		result.ProvidersUpdated = append(result.ProvidersUpdated, name)
		s.log.Info("config sync: updated provider from YAML (yaml_wins)", "name", name)

	case store.MergeYAMLInitOnly:
		// Never update after initial import
		result.ProvidersSkipped = append(result.ProvidersSkipped, name)
		s.log.Debug("config sync: skipped provider (yaml_init_only)", "name", name)

	case store.MergeSQLiteWins:
		fallthrough
	default:
		// SQLite wins: only update if source=yaml AND yaml changed
		if existing.Source == store.ConfigSourceYAML && existing.YAMLHash != yamlHash {
			normalized, cleanup, normErr := NormalizeProviderConfigForStore(name, currentCfg, pc)
			if normErr != nil {
				return normErr
			}
			row := providerConfigToRow(name, normalized, store.ConfigSourceYAML, yamlHash)
			if err := s.store.UpsertProvider(ctx, row); err != nil {
				return err
			}
			if err := CleanupManagedSecretRefs(cleanup); err != nil {
				s.log.Warn("config sync: cleanup provider secrets failed", "name", name, "error", err)
			}
			result.ProvidersUpdated = append(result.ProvidersUpdated, name)
			s.log.Info("config sync: updated provider (YAML changed)", "name", name)
		} else {
			result.ProvidersSkipped = append(result.ProvidersSkipped, name)
			s.log.Debug("config sync: skipped provider (sqlite_wins)", "name", name, "source", existing.Source)
		}
	}

	return nil
}

// syncChannels syncs channel configs.
func (s *ConfigSyncer) syncChannels(ctx context.Context, cfg *Config, result *SyncResult) map[string]struct{} {
	channels := extractChannelConfigs(cfg)
	names := make(map[string]struct{}, len(channels))

	for name, channelCfg := range channels {
		names[name] = struct{}{}
		if err := s.syncChannel(ctx, name, channelCfg, result); err != nil {
			result.Errors = append(result.Errors, err)
		}
	}
	return names
}

// syncChannel syncs a single channel config.
func (s *ConfigSyncer) syncChannel(ctx context.Context, name string, cfg any, result *SyncResult) error {
	rawConfigJSON, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	yamlHash := store.HashConfig(cfg)
	currentRaw := json.RawMessage(nil)

	existing, err := s.store.GetChannel(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		// New channel, import from YAML
		configJSON, _, cleanup, normErr := NormalizeChannelConfigForStore(name, currentRaw, rawConfigJSON)
		if normErr != nil {
			return normErr
		}
		row := &store.ChannelConfigRow{
			Name:     name,
			Config:   string(configJSON),
			Source:   store.ConfigSourceYAML,
			YAMLHash: yamlHash,
		}
		if err := s.store.UpsertChannel(ctx, row); err != nil {
			return err
		}
		if err := CleanupManagedSecretRefs(cleanup); err != nil {
			s.log.Warn("config sync: cleanup channel secrets failed", "name", name, "error", err)
		}
		result.ChannelsImported = append(result.ChannelsImported, name)
		s.log.Info("config sync: imported channel from YAML", "name", name)
		return nil
	}
	if err != nil {
		return err
	}
	currentRaw = json.RawMessage(existing.Config)

	// Determine merge strategy
	merge := s.opts.DefaultMerge
	if m, ok := s.opts.ChannelMerges[name]; ok {
		merge = m
	}

	switch merge {
	case store.MergeYAMLWins:
		configJSON, _, cleanup, normErr := NormalizeChannelConfigForStore(name, currentRaw, rawConfigJSON)
		if normErr != nil {
			return normErr
		}
		row := &store.ChannelConfigRow{
			Name:     name,
			Config:   string(configJSON),
			Source:   store.ConfigSourceYAML,
			YAMLHash: yamlHash,
		}
		if err := s.store.UpsertChannel(ctx, row); err != nil {
			return err
		}
		if err := CleanupManagedSecretRefs(cleanup); err != nil {
			s.log.Warn("config sync: cleanup channel secrets failed", "name", name, "error", err)
		}
		result.ChannelsUpdated = append(result.ChannelsUpdated, name)
		s.log.Info("config sync: updated channel from YAML (yaml_wins)", "name", name)

	case store.MergeYAMLInitOnly:
		result.ChannelsSkipped = append(result.ChannelsSkipped, name)

	case store.MergeSQLiteWins:
		fallthrough
	default:
		if existing.Source == store.ConfigSourceYAML && existing.YAMLHash != yamlHash {
			configJSON, _, cleanup, normErr := NormalizeChannelConfigForStore(name, currentRaw, rawConfigJSON)
			if normErr != nil {
				return normErr
			}
			row := &store.ChannelConfigRow{
				Name:     name,
				Config:   string(configJSON),
				Source:   store.ConfigSourceYAML,
				YAMLHash: yamlHash,
			}
			if err := s.store.UpsertChannel(ctx, row); err != nil {
				return err
			}
			if err := CleanupManagedSecretRefs(cleanup); err != nil {
				s.log.Warn("config sync: cleanup channel secrets failed", "name", name, "error", err)
			}
			result.ChannelsUpdated = append(result.ChannelsUpdated, name)
			s.log.Info("config sync: updated channel (YAML changed)", "name", name)
		} else {
			result.ChannelsSkipped = append(result.ChannelsSkipped, name)
		}
	}

	return nil
}

func (s *ConfigSyncer) pruneProviders(ctx context.Context, keep map[string]struct{}) error {
	if s == nil || s.store == nil {
		return nil
	}
	rows, err := s.store.ListProviders(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		if _, ok := keep[name]; ok {
			continue
		}
		if err := s.store.DeleteProvider(ctx, name); err != nil && err != store.ErrNotFound {
			return err
		}
		if err := CleanupManagedSecretRefs(managedSecretCleanupRefs(providerSecretValues(RowToProviderConfig(&row)), nil)); err != nil {
			s.log.Warn("config sync: cleanup pruned provider secrets failed", "name", name, "error", err)
		}
		s.log.Info("config sync: pruned provider missing from YAML", "name", name)
	}
	return nil
}

func (s *ConfigSyncer) pruneChannels(ctx context.Context, keep map[string]struct{}) error {
	if s == nil || s.store == nil {
		return nil
	}
	rows, err := s.store.ListChannels(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		if _, ok := keep[name]; ok {
			continue
		}
		if err := s.store.DeleteChannel(ctx, name); err != nil && err != store.ErrNotFound {
			return err
		}
		if _, _, cleanup, err := NormalizeChannelConfigForStore(name, json.RawMessage(row.Config), json.RawMessage(`{}`)); err == nil {
			if cleanupErr := CleanupManagedSecretRefs(cleanup); cleanupErr != nil {
				s.log.Warn("config sync: cleanup pruned channel secrets failed", "name", name, "error", cleanupErr)
			}
		}
		s.log.Info("config sync: pruned channel missing from YAML", "name", name)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// providerConfigToRow converts a ProviderConfig to a store row.
func providerConfigToRow(name string, pc ProviderConfig, source store.ConfigSource, yamlHash string) *store.ProviderConfigRow {
	pc = NormalizeProviderConfig(name, pc)
	headersJSON := "{}"
	if pc.Headers != nil {
		if data, err := json.Marshal(pc.Headers); err == nil {
			headersJSON = string(data)
		}
	}

	return &store.ProviderConfigRow{
		Name:         name,
		API:          pc.API,
		BaseURL:      pc.BaseURL,
		Region:       pc.Region,
		APIKey:       pc.APIKey,
		APIKeys:      pc.APIKeys,
		AccessKeyID:  pc.AccessKeyID,
		SecretKey:    pc.SecretKey,
		SessionToken: pc.SessionToken,
		DefaultModel: pc.DefaultModel,
		TimeoutSec:   int(pc.Timeout.Seconds()),
		Headers:      headersJSON,
		Source:       source,
		YAMLHash:     yamlHash,
	}
}

// RowToProviderConfig converts a store row to a ProviderConfig.
func RowToProviderConfig(row *store.ProviderConfigRow) ProviderConfig {
	var headers map[string]string
	if row.Headers != "" && row.Headers != "{}" {
		json.Unmarshal([]byte(row.Headers), &headers)
	}

	return NormalizeProviderConfig(row.Name, ProviderConfig{
		API:          row.API,
		BaseURL:      row.BaseURL,
		Region:       row.Region,
		APIKey:       row.APIKey,
		APIKeys:      row.APIKeys,
		AccessKeyID:  row.AccessKeyID,
		SecretKey:    row.SecretKey,
		SessionToken: row.SessionToken,
		DefaultModel: row.DefaultModel,
		Timeout:      time.Duration(row.TimeoutSec) * time.Second,
		Headers:      headers,
	})
}

// extractChannelConfigs extracts non-empty channel configs from the main config.
func extractChannelConfigs(cfg *Config) map[string]any {
	channels := make(map[string]any)

	// Use reflection to iterate over ChannelsConfig fields
	v := reflect.ValueOf(cfg.Channels)
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		// Get the yaml tag as the channel name
		yamlTag := fieldType.Tag.Get("yaml")
		if yamlTag == "" || yamlTag == "-" {
			continue
		}

		// Check if the field has any non-zero values
		if !isZeroValue(field) {
			channels[yamlTag] = field.Interface()
		}
	}

	return channels
}

// isZeroValue checks if a reflect.Value is the zero value for its type.
func isZeroValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !isZeroValue(v.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	case reflect.Slice, reflect.Map:
		return v.IsNil() || v.Len() == 0
	case reflect.String:
		return v.String() == ""
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	default:
		return false
	}
}
