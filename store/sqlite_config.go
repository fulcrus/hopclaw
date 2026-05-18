package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/durablefact"
)

type MergeStrategy string

const (
	MergeSQLiteWins   MergeStrategy = "sqlite_wins"
	MergeYAMLWins     MergeStrategy = "yaml_wins"
	MergeYAMLInitOnly MergeStrategy = "yaml_init_only"
)

type ConfigSource string

const (
	ConfigSourceYAML ConfigSource = "yaml"
	ConfigSourceAPI  ConfigSource = "api"
)

type ProviderConfigRow struct {
	Name         string       `json:"name"`
	API          string       `json:"api"`
	BaseURL      string       `json:"base_url,omitempty"`
	Region       string       `json:"region,omitempty"`
	APIKey       string       `json:"api_key,omitempty"`
	APIKeys      []string     `json:"api_keys,omitempty"`
	AccessKeyID  string       `json:"access_key_id,omitempty"`
	SecretKey    string       `json:"secret_key,omitempty"`
	SessionToken string       `json:"session_token,omitempty"`
	DefaultModel string       `json:"default_model,omitempty"`
	TimeoutSec   int          `json:"timeout_sec,omitempty"`
	Headers      string       `json:"headers,omitempty"`
	Enabled      *bool        `json:"enabled,omitempty"`
	Source       ConfigSource `json:"source"`
	YAMLHash     string       `json:"yaml_hash,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type ChannelConfigRow struct {
	Name      string       `json:"name"`
	Config    string       `json:"config"`
	Enabled   *bool        `json:"enabled,omitempty"`
	Source    ConfigSource `json:"source"`
	YAMLHash  string       `json:"yaml_hash,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type DynamicSettingRow struct {
	Key       string       `json:"key"`
	Value     string       `json:"value"`
	Source    ConfigSource `json:"source"`
	YAMLHash  string       `json:"yaml_hash,omitempty"`
	UpdatedAt time.Time    `json:"updated_at"`
}

const configProviderSchema = `
CREATE TABLE IF NOT EXISTS config_providers (
	name          TEXT PRIMARY KEY,
	api           TEXT NOT NULL DEFAULT '',
	base_url      TEXT NOT NULL DEFAULT '',
	region        TEXT NOT NULL DEFAULT '',
	api_key       TEXT NOT NULL DEFAULT '',
	api_keys      TEXT NOT NULL DEFAULT '[]',
	access_key_id TEXT NOT NULL DEFAULT '',
	secret_key    TEXT NOT NULL DEFAULT '',
	session_token TEXT NOT NULL DEFAULT '',
	default_model TEXT NOT NULL DEFAULT '',
	timeout_sec   INTEGER NOT NULL DEFAULT 0,
	headers       TEXT NOT NULL DEFAULT '{}',
	enabled       INTEGER,
	source        TEXT NOT NULL DEFAULT 'yaml',
	yaml_hash     TEXT NOT NULL DEFAULT '',
	created_at    TEXT NOT NULL,
	updated_at    TEXT NOT NULL
)`

const configChannelSchema = `
CREATE TABLE IF NOT EXISTS config_channels (
	name       TEXT PRIMARY KEY,
	config     TEXT NOT NULL DEFAULT '{}',
	enabled    INTEGER,
	source     TEXT NOT NULL DEFAULT 'yaml',
	yaml_hash  TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
)`

const dynamicSettingSchema = `
CREATE TABLE IF NOT EXISTS dynamic_settings (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL DEFAULT '{}',
	source     TEXT NOT NULL DEFAULT 'yaml',
	yaml_hash  TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL
)`

func EnsureConfigSchema(db *sql.DB) error {
	for _, schema := range []string{configProviderSchema, configChannelSchema, dynamicSettingSchema} {
		if _, err := db.Exec(schema); err != nil {
			return fmt.Errorf("failed to create config schema: %w", err)
		}
	}
	return nil
}

type ConfigStore struct {
	db    *sql.DB
	facts *durablefact.SQLiteStore
}

func NewConfigStore(db *sql.DB) (*ConfigStore, error) {
	if err := EnsureConfigSchema(db); err != nil {
		return nil, err
	}
	facts, err := durablefact.NewSQLiteStore(db)
	if err != nil {
		return nil, err
	}
	store := &ConfigStore{db: db, facts: facts}
	if err := store.migrateLegacyConfig(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *ConfigStore) ListConfigViews(ctx context.Context, filter durablefact.Filter) ([]durablefact.ConfigView, error) {
	return s.facts.ListConfigViews(ctx, filter)
}

func (s *ConfigStore) ListOperatorViews(ctx context.Context, filter durablefact.Filter) ([]durablefact.OperatorView, error) {
	facts, err := s.facts.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	views := make([]durablefact.OperatorView, 0, len(facts))
	for _, fact := range facts {
		if _, ok := durablefact.ToConfigView(fact); !ok {
			continue
		}
		views = append(views, durablefact.ToOperatorView(fact))
	}
	return views, nil
}

type providerFactPayload struct {
	API          string   `json:"api,omitempty"`
	BaseURL      string   `json:"base_url,omitempty"`
	Region       string   `json:"region,omitempty"`
	APIKey       string   `json:"api_key,omitempty"`
	APIKeys      []string `json:"api_keys,omitempty"`
	AccessKeyID  string   `json:"access_key_id,omitempty"`
	SecretKey    string   `json:"secret_key,omitempty"`
	SessionToken string   `json:"session_token,omitempty"`
	DefaultModel string   `json:"default_model,omitempty"`
	TimeoutSec   int      `json:"timeout_sec,omitempty"`
	Headers      string   `json:"headers,omitempty"`
	Enabled      *bool    `json:"enabled,omitempty"`
	YAMLHash     string   `json:"yaml_hash,omitempty"`
}

type channelFactPayload struct {
	Config   string `json:"config"`
	Enabled  *bool  `json:"enabled,omitempty"`
	YAMLHash string `json:"yaml_hash,omitempty"`
}

type settingFactPayload struct {
	Value    string `json:"value"`
	YAMLHash string `json:"yaml_hash,omitempty"`
}

func (s *ConfigStore) GetProvider(ctx context.Context, name string) (*ProviderConfigRow, error) {
	fact, err := s.facts.Get(ctx, providerFactKey(name))
	if err != nil {
		return nil, err
	}
	if fact == nil {
		return nil, ErrNotFound
	}
	return providerRowFromFact(*fact)
}

func (s *ConfigStore) ListProviders(ctx context.Context) ([]ProviderConfigRow, error) {
	facts, err := s.facts.List(ctx, durablefact.Filter{ViewType: durablefact.ViewTypeConfigProvider})
	if err != nil {
		return nil, fmt.Errorf("failed to list providers: %w", err)
	}
	items := make([]ProviderConfigRow, 0, len(facts))
	for _, fact := range facts {
		row, err := providerRowFromFact(fact)
		if err != nil {
			return nil, err
		}
		items = append(items, *row)
	}
	return items, nil
}

func (s *ConfigStore) UpsertProvider(ctx context.Context, p *ProviderConfigRow) error {
	if p == nil {
		return fmt.Errorf("provider config is required")
	}
	row := *p
	row.Name = strings.TrimSpace(row.Name)
	if row.Name == "" {
		return fmt.Errorf("provider name is required")
	}
	existing, err := s.GetProvider(ctx, row.Name)
	if err != nil && err != ErrNotFound {
		return err
	}
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		if existing != nil {
			row.CreatedAt = existing.CreatedAt
		} else {
			row.CreatedAt = now
		}
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = now
	}
	return s.persistProviderFact(ctx, row)
}

func (s *ConfigStore) DeleteProvider(ctx context.Context, name string) error {
	key := providerFactKey(name)
	fact, err := s.facts.Get(ctx, key)
	if err != nil {
		return err
	}
	if fact == nil {
		return ErrNotFound
	}
	return s.facts.Delete(ctx, key)
}

func (s *ConfigStore) GetChannel(ctx context.Context, name string) (*ChannelConfigRow, error) {
	fact, err := s.facts.Get(ctx, channelFactKey(name))
	if err != nil {
		return nil, err
	}
	if fact == nil {
		return nil, ErrNotFound
	}
	return channelRowFromFact(*fact)
}

func (s *ConfigStore) ListChannels(ctx context.Context) ([]ChannelConfigRow, error) {
	facts, err := s.facts.List(ctx, durablefact.Filter{ViewType: durablefact.ViewTypeConfigChannel})
	if err != nil {
		return nil, fmt.Errorf("failed to list channels: %w", err)
	}
	items := make([]ChannelConfigRow, 0, len(facts))
	for _, fact := range facts {
		row, err := channelRowFromFact(fact)
		if err != nil {
			return nil, err
		}
		items = append(items, *row)
	}
	return items, nil
}

func (s *ConfigStore) UpsertChannel(ctx context.Context, c *ChannelConfigRow) error {
	if c == nil {
		return fmt.Errorf("channel config is required")
	}
	row := *c
	row.Name = strings.TrimSpace(row.Name)
	if row.Name == "" {
		return fmt.Errorf("channel name is required")
	}
	existing, err := s.GetChannel(ctx, row.Name)
	if err != nil && err != ErrNotFound {
		return err
	}
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		if existing != nil {
			row.CreatedAt = existing.CreatedAt
		} else {
			row.CreatedAt = now
		}
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = now
	}
	if strings.TrimSpace(row.Config) == "" {
		row.Config = "{}"
	}
	return s.persistChannelFact(ctx, row)
}

func (s *ConfigStore) DeleteChannel(ctx context.Context, name string) error {
	key := channelFactKey(name)
	fact, err := s.facts.Get(ctx, key)
	if err != nil {
		return err
	}
	if fact == nil {
		return ErrNotFound
	}
	return s.facts.Delete(ctx, key)
}

func (s *ConfigStore) GetSetting(ctx context.Context, key string) (*DynamicSettingRow, error) {
	fact, err := s.facts.Get(ctx, settingFactKey(key))
	if err != nil {
		return nil, err
	}
	if fact == nil {
		return nil, ErrNotFound
	}
	return settingRowFromFact(*fact)
}

func (s *ConfigStore) ListSettings(ctx context.Context) ([]DynamicSettingRow, error) {
	facts, err := s.facts.List(ctx, durablefact.Filter{ViewType: durablefact.ViewTypeConfigSetting})
	if err != nil {
		return nil, fmt.Errorf("failed to list settings: %w", err)
	}
	items := make([]DynamicSettingRow, 0, len(facts))
	for _, fact := range facts {
		row, err := settingRowFromFact(fact)
		if err != nil {
			return nil, err
		}
		items = append(items, *row)
	}
	return items, nil
}

func (s *ConfigStore) UpsertSetting(ctx context.Context, d *DynamicSettingRow) error {
	if d == nil {
		return fmt.Errorf("dynamic setting is required")
	}
	row := *d
	row.Key = strings.TrimSpace(row.Key)
	if row.Key == "" {
		return fmt.Errorf("setting key is required")
	}
	if strings.TrimSpace(row.Value) == "" {
		row.Value = "{}"
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = time.Now().UTC()
	}
	return s.persistSettingFact(ctx, row)
}

func (s *ConfigStore) DeleteSetting(ctx context.Context, key string) error {
	factKey := settingFactKey(key)
	fact, err := s.facts.Get(ctx, factKey)
	if err != nil {
		return err
	}
	if fact == nil {
		return ErrNotFound
	}
	return s.facts.Delete(ctx, factKey)
}

func (s *ConfigStore) persistProviderFact(ctx context.Context, row ProviderConfigRow) error {
	fact, err := providerFactFromRow(row)
	if err != nil {
		return err
	}
	_, err = s.facts.Upsert(ctx, fact)
	if err != nil {
		return fmt.Errorf("failed to upsert provider %q: %w", row.Name, err)
	}
	return nil
}

func (s *ConfigStore) persistChannelFact(ctx context.Context, row ChannelConfigRow) error {
	fact, err := channelFactFromRow(row)
	if err != nil {
		return err
	}
	_, err = s.facts.Upsert(ctx, fact)
	if err != nil {
		return fmt.Errorf("failed to upsert channel %q: %w", row.Name, err)
	}
	return nil
}

func (s *ConfigStore) persistSettingFact(ctx context.Context, row DynamicSettingRow) error {
	fact, err := settingFactFromRow(row)
	if err != nil {
		return err
	}
	_, err = s.facts.Upsert(ctx, fact)
	if err != nil {
		return fmt.Errorf("failed to upsert setting %q: %w", row.Key, err)
	}
	return nil
}

func providerFactFromRow(row ProviderConfigRow) (durablefact.Fact, error) {
	payload := providerFactPayload{
		API:          row.API,
		BaseURL:      row.BaseURL,
		Region:       row.Region,
		APIKey:       row.APIKey,
		APIKeys:      append([]string(nil), row.APIKeys...),
		AccessKeyID:  row.AccessKeyID,
		SecretKey:    row.SecretKey,
		SessionToken: row.SessionToken,
		DefaultModel: row.DefaultModel,
		TimeoutSec:   row.TimeoutSec,
		Headers:      row.Headers,
		Enabled:      cloneBoolPtr(row.Enabled),
		YAMLHash:     row.YAMLHash,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return durablefact.Fact{}, fmt.Errorf("encode provider %q: %w", row.Name, err)
	}
	return durablefact.NormalizeFact(durablefact.Fact{
		Key:        providerFactKey(row.Name),
		FactClass:  durablefact.FactClassSystemConfig,
		ViewType:   durablefact.ViewTypeConfigProvider,
		Namespace:  "provider",
		Name:       strings.TrimSpace(row.Name),
		Label:      strings.TrimSpace(row.Name),
		Value:      string(body),
		ValueType:  durablefact.ValueTypeJSON,
		Source:     strings.TrimSpace(string(row.Source)),
		Managed:    true,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
		Confidence: 1,
	}), nil
}

func channelFactFromRow(row ChannelConfigRow) (durablefact.Fact, error) {
	payload := channelFactPayload{
		Config:   strings.TrimSpace(row.Config),
		Enabled:  cloneBoolPtr(row.Enabled),
		YAMLHash: row.YAMLHash,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return durablefact.Fact{}, fmt.Errorf("encode channel %q: %w", row.Name, err)
	}
	return durablefact.NormalizeFact(durablefact.Fact{
		Key:        channelFactKey(row.Name),
		FactClass:  durablefact.FactClassSystemConfig,
		ViewType:   durablefact.ViewTypeConfigChannel,
		Namespace:  "channel",
		Name:       strings.TrimSpace(row.Name),
		Label:      strings.TrimSpace(row.Name),
		Value:      string(body),
		ValueType:  durablefact.ValueTypeJSON,
		Source:     strings.TrimSpace(string(row.Source)),
		Managed:    true,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
		Confidence: 1,
	}), nil
}

func settingFactFromRow(row DynamicSettingRow) (durablefact.Fact, error) {
	payload := settingFactPayload{
		Value:    strings.TrimSpace(row.Value),
		YAMLHash: row.YAMLHash,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return durablefact.Fact{}, fmt.Errorf("encode setting %q: %w", row.Key, err)
	}
	return durablefact.NormalizeFact(durablefact.Fact{
		Key:        settingFactKey(row.Key),
		FactClass:  durablefact.FactClassSystemConfig,
		ViewType:   durablefact.ViewTypeConfigSetting,
		Namespace:  "setting",
		Name:       strings.TrimSpace(row.Key),
		Label:      strings.TrimSpace(row.Key),
		Value:      string(body),
		ValueType:  durablefact.ValueTypeJSON,
		Source:     strings.TrimSpace(string(row.Source)),
		Managed:    true,
		CreatedAt:  row.UpdatedAt,
		UpdatedAt:  row.UpdatedAt,
		Confidence: 1,
	}), nil
}

func providerRowFromFact(fact durablefact.Fact) (*ProviderConfigRow, error) {
	view, ok := durablefact.ToConfigView(fact)
	if !ok || view.Kind != durablefact.ConfigViewKindProvider {
		return nil, fmt.Errorf("durable fact %q is not a provider config view", fact.Key)
	}
	var payload providerFactPayload
	if err := json.Unmarshal([]byte(view.Payload), &payload); err != nil {
		return nil, fmt.Errorf("decode provider %q: %w", view.Name, err)
	}
	return &ProviderConfigRow{
		Name:         view.Name,
		API:          payload.API,
		BaseURL:      payload.BaseURL,
		Region:       payload.Region,
		APIKey:       payload.APIKey,
		APIKeys:      append([]string(nil), payload.APIKeys...),
		AccessKeyID:  payload.AccessKeyID,
		SecretKey:    payload.SecretKey,
		SessionToken: payload.SessionToken,
		DefaultModel: payload.DefaultModel,
		TimeoutSec:   payload.TimeoutSec,
		Headers:      payload.Headers,
		Enabled:      cloneBoolPtr(payload.Enabled),
		Source:       ConfigSource(view.Source),
		YAMLHash:     payload.YAMLHash,
		CreatedAt:    view.CreatedAt,
		UpdatedAt:    view.UpdatedAt,
	}, nil
}

func channelRowFromFact(fact durablefact.Fact) (*ChannelConfigRow, error) {
	view, ok := durablefact.ToConfigView(fact)
	if !ok || view.Kind != durablefact.ConfigViewKindChannel {
		return nil, fmt.Errorf("durable fact %q is not a channel config view", fact.Key)
	}
	var payload channelFactPayload
	if err := json.Unmarshal([]byte(view.Payload), &payload); err != nil {
		return nil, fmt.Errorf("decode channel %q: %w", view.Name, err)
	}
	return &ChannelConfigRow{
		Name:      view.Name,
		Config:    payload.Config,
		Enabled:   cloneBoolPtr(payload.Enabled),
		Source:    ConfigSource(view.Source),
		YAMLHash:  payload.YAMLHash,
		CreatedAt: view.CreatedAt,
		UpdatedAt: view.UpdatedAt,
	}, nil
}

func settingRowFromFact(fact durablefact.Fact) (*DynamicSettingRow, error) {
	view, ok := durablefact.ToConfigView(fact)
	if !ok || view.Kind != durablefact.ConfigViewKindSetting {
		return nil, fmt.Errorf("durable fact %q is not a setting config view", fact.Key)
	}
	var payload settingFactPayload
	if err := json.Unmarshal([]byte(view.Payload), &payload); err != nil {
		return nil, fmt.Errorf("decode setting %q: %w", view.Name, err)
	}
	return &DynamicSettingRow{
		Key:       view.Name,
		Value:     payload.Value,
		Source:    ConfigSource(view.Source),
		YAMLHash:  payload.YAMLHash,
		UpdatedAt: view.UpdatedAt,
	}, nil
}

func (s *ConfigStore) migrateLegacyConfig(ctx context.Context) error {
	providers, err := s.listLegacyProviders(ctx)
	if err != nil {
		return err
	}
	for _, row := range providers {
		current, err := s.GetProvider(ctx, row.Name)
		if err != nil && err != ErrNotFound {
			return err
		}
		if current != nil && !row.UpdatedAt.After(current.UpdatedAt) {
			continue
		}
		if err := s.persistProviderFact(ctx, row); err != nil {
			return err
		}
	}

	channels, err := s.listLegacyChannels(ctx)
	if err != nil {
		return err
	}
	for _, row := range channels {
		current, err := s.GetChannel(ctx, row.Name)
		if err != nil && err != ErrNotFound {
			return err
		}
		if current != nil && !row.UpdatedAt.After(current.UpdatedAt) {
			continue
		}
		if err := s.persistChannelFact(ctx, row); err != nil {
			return err
		}
	}

	settings, err := s.listLegacySettings(ctx)
	if err != nil {
		return err
	}
	for _, row := range settings {
		current, err := s.GetSetting(ctx, row.Key)
		if err != nil && err != ErrNotFound {
			return err
		}
		if current != nil && !row.UpdatedAt.After(current.UpdatedAt) {
			continue
		}
		if err := s.persistSettingFact(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

func (s *ConfigStore) listLegacyProviders(ctx context.Context) ([]ProviderConfigRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, api, base_url, region, api_key, api_keys,
		       access_key_id, secret_key, session_token, default_model,
		       timeout_sec, headers, enabled, source, yaml_hash, created_at, updated_at
		FROM config_providers ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("failed to list providers: %w", err)
	}
	defer rows.Close()

	var providers []ProviderConfigRow
	for rows.Next() {
		var p ProviderConfigRow
		var apiKeysJSON string
		var enabled sql.NullInt64
		var createdAt string
		var updatedAt string
		if err := rows.Scan(
			&p.Name, &p.API, &p.BaseURL, &p.Region, &p.APIKey, &apiKeysJSON,
			&p.AccessKeyID, &p.SecretKey, &p.SessionToken, &p.DefaultModel,
			&p.TimeoutSec, &p.Headers, &enabled, &p.Source, &p.YAMLHash, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan provider: %w", err)
		}
		keys, err := decodeJSONStringSliceField(apiKeysJSON, "sqlite config_providers", p.Name, "api_keys")
		if err != nil {
			return nil, err
		}
		p.APIKeys = keys
		if enabled.Valid {
			value := enabled.Int64 != 0
			p.Enabled = &value
		}
		p.CreatedAt, err = parseTime(createdAt, "sqlite config_providers", p.Name, "created_at")
		if err != nil {
			return nil, err
		}
		p.UpdatedAt, err = parseTime(updatedAt, "sqlite config_providers", p.Name, "updated_at")
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

func (s *ConfigStore) listLegacyChannels(ctx context.Context) ([]ChannelConfigRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, config, enabled, source, yaml_hash, created_at, updated_at
		FROM config_channels ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("failed to list channels: %w", err)
	}
	defer rows.Close()

	var items []ChannelConfigRow
	for rows.Next() {
		var row ChannelConfigRow
		var enabled sql.NullInt64
		var createdAt string
		var updatedAt string
		if err := rows.Scan(&row.Name, &row.Config, &enabled, &row.Source, &row.YAMLHash, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan channel: %w", err)
		}
		if enabled.Valid {
			value := enabled.Int64 != 0
			row.Enabled = &value
		}
		var err error
		row.CreatedAt, err = parseTime(createdAt, "sqlite config_channels", row.Name, "created_at")
		if err != nil {
			return nil, err
		}
		row.UpdatedAt, err = parseTime(updatedAt, "sqlite config_channels", row.Name, "updated_at")
		if err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func (s *ConfigStore) listLegacySettings(ctx context.Context) ([]DynamicSettingRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, value, source, yaml_hash, updated_at
		FROM dynamic_settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("failed to list settings: %w", err)
	}
	defer rows.Close()

	var items []DynamicSettingRow
	for rows.Next() {
		var row DynamicSettingRow
		var updatedAt string
		if err := rows.Scan(&row.Key, &row.Value, &row.Source, &row.YAMLHash, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan setting: %w", err)
		}
		var err error
		row.UpdatedAt, err = parseTime(updatedAt, "sqlite dynamic_settings", row.Key, "updated_at")
		if err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func providerFactKey(name string) string {
	return "config.provider." + strings.TrimSpace(name)
}

func channelFactKey(name string) string {
	return "config.channel." + strings.TrimSpace(name)
}

func settingFactKey(key string) string {
	return "config.setting." + strings.TrimSpace(key)
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func HashConfig(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

var ErrNotFound = errors.New("not found")
