package overlay

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/store"
	"gopkg.in/yaml.v3"
)

type StoreReader interface {
	ListProviders(ctx context.Context) ([]store.ProviderConfigRow, error)
	ListChannels(ctx context.Context) ([]store.ChannelConfigRow, error)
	ListSettings(ctx context.Context) ([]store.DynamicSettingRow, error)
}

type SnapshotBuilder func(cfg config.Config, layers []controlsnapshot.Layer) *controlsnapshot.EffectiveConfigSnapshot

type Options struct {
	BaseLayers      []controlsnapshot.Layer
	SnapshotBuilder SnapshotBuilder
	SettingPolicies *SettingPolicyRegistry
}

type ProviderProjection = controlplane.ProviderProjection
type ChannelProjection = controlplane.ChannelProjection
type SettingProjection = controlplane.SettingProjection

type State struct {
	Config        config.Config
	RuntimeConfig config.Config
	Snapshot      *controlsnapshot.EffectiveConfigSnapshot
	Providers     []ProviderProjection
	Channels      []ChannelProjection
	Settings      []SettingProjection
	Layers        []controlsnapshot.Layer
	Version       string
	Diff          config.ChangeSet
}

type Resolver struct {
	mu            sync.RWMutex
	base          config.Config
	store         StoreReader
	baseLayers    []controlsnapshot.Layer
	buildSnapshot SnapshotBuilder
	settings      *SettingPolicyRegistry
	state         State
	previous      *State
}

func NewResolver(ctx context.Context, base config.Config, storeReader StoreReader, opts Options) (*Resolver, error) {
	resolver := &Resolver{
		base:          base,
		store:         storeReader,
		baseLayers:    cloneLayers(opts.BaseLayers),
		buildSnapshot: opts.SnapshotBuilder,
		settings:      opts.SettingPolicies,
	}
	if resolver.settings == nil {
		resolver.settings = DefaultSettingPolicyRegistry()
	}
	if err := resolver.refreshLocked(ctx); err != nil {
		return nil, err
	}
	return resolver, nil
}

func ResolveState(ctx context.Context, base config.Config, storeReader StoreReader, opts Options) (State, error) {
	resolver, err := NewResolver(ctx, base, storeReader, opts)
	if err != nil {
		return State{}, err
	}
	return resolver.stateClone(), nil
}

func (r *Resolver) SetBase(ctx context.Context, base config.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.base = base
	return r.refreshLocked(ctx)
}

func (r *Resolver) Reconfigure(ctx context.Context, base config.Config, opts Options) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.base = base
	r.baseLayers = cloneLayers(opts.BaseLayers)
	if opts.SnapshotBuilder != nil {
		r.buildSnapshot = opts.SnapshotBuilder
	}
	if opts.SettingPolicies != nil {
		r.settings = opts.SettingPolicies
	}
	return r.refreshLocked(ctx)
}

func (r *Resolver) Refresh(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refreshLocked(ctx)
}

func (r *Resolver) Current() config.Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state.Config
}

func (r *Resolver) RuntimeCurrent() config.Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state.RuntimeConfig
}

func (r *Resolver) Snapshot() *controlsnapshot.EffectiveConfigSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.state.Snapshot == nil {
		return nil
	}
	return r.state.Snapshot.Clone()
}

func (r *Resolver) Providers() []ProviderProjection {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneProviderProjections(r.state.Providers)
}

func (r *Resolver) Channels() []ChannelProjection {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneChannelProjections(r.state.Channels)
}

func (r *Resolver) Settings() []SettingProjection {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneSettingProjections(r.state.Settings)
}

func (r *Resolver) Version() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return strings.TrimSpace(r.state.Version)
}

func (r *Resolver) DiffSince(version string) (config.ChangeSet, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	version = strings.TrimSpace(version)
	if version == "" {
		return r.state.Diff, false
	}
	if version == strings.TrimSpace(r.state.Version) {
		return config.ChangeSet{}, true
	}
	if r.previous != nil && version == strings.TrimSpace(r.previous.Version) {
		return r.state.Diff, true
	}
	return config.ChangeSet{}, false
}

func (r *Resolver) stateClone() State {
	return cloneState(r.state)
}

func cloneState(state State) State {
	var snapshot *controlsnapshot.EffectiveConfigSnapshot
	if state.Snapshot != nil {
		snapshot = state.Snapshot.Clone()
	}
	return State{
		Config:        state.Config,
		RuntimeConfig: state.RuntimeConfig,
		Snapshot:      snapshot,
		Providers:     cloneProviderProjections(state.Providers),
		Channels:      cloneChannelProjections(state.Channels),
		Settings:      cloneSettingProjections(state.Settings),
		Layers:        cloneLayers(state.Layers),
		Version:       strings.TrimSpace(state.Version),
		Diff:          state.Diff,
	}
}

func (r *Resolver) refreshLocked(ctx context.Context) error {
	state, err := buildState(ctx, r.base, r.store, r.baseLayers, r.buildSnapshot, r.settings)
	if err != nil {
		return err
	}
	if r.state.Version != "" {
		previous := cloneState(r.state)
		r.previous = &previous
		state.Diff = config.Diff(previous.Config, state.Config)
	}
	r.state = state
	return nil
}

func buildState(ctx context.Context, base config.Config, storeReader StoreReader, baseLayers []controlsnapshot.Layer, buildSnapshot SnapshotBuilder, settingsRegistry *SettingPolicyRegistry) (State, error) {
	merged := base
	merged.Models.Providers = cloneProviderMap(base.Models.Providers)

	providerViews := make(map[string]ProviderProjection, len(merged.Models.Providers))
	for _, name := range sortedProviderKeys(merged.Models.Providers) {
		providerViews[name] = ProviderProjection{
			Name:        name,
			Config:      merged.Models.Providers[name],
			Source:      string(store.ConfigSourceYAML),
			BasePresent: true,
		}
	}

	channelViews := make(map[string]ChannelProjection)
	baseChannels := configuredBaseChannels(base.Channels)
	for _, name := range sortedAnyKeys(baseChannels) {
		payload, err := marshalChannelProjection(baseChannels[name])
		if err != nil {
			return State{}, fmt.Errorf("marshal base channel %q: %w", name, err)
		}
		channelViews[name] = ChannelProjection{
			Name:        name,
			Config:      payload,
			Source:      string(store.ConfigSourceYAML),
			Recognized:  true,
			BasePresent: true,
		}
	}

	var providerRows []store.ProviderConfigRow
	var channelRows []store.ChannelConfigRow
	var settingRows []store.DynamicSettingRow
	var err error
	if storeReader != nil {
		providerRows, err = storeReader.ListProviders(ctx)
		if err != nil {
			return State{}, fmt.Errorf("list provider overlays: %w", err)
		}
		channelRows, err = storeReader.ListChannels(ctx)
		if err != nil {
			return State{}, fmt.Errorf("list channel overlays: %w", err)
		}
		settingRows, err = storeReader.ListSettings(ctx)
		if err != nil {
			return State{}, fmt.Errorf("list settings overlays: %w", err)
		}
	}

	appliedProviderOverlay := false
	for _, row := range providerRows {
		if row.Source != store.ConfigSourceAPI {
			continue
		}
		appliedProviderOverlay = true
		view := ProviderProjection{
			Name:        row.Name,
			Config:      config.RowToProviderConfig(&row),
			Enabled:     cloneBoolPtr(row.Enabled),
			Source:      string(row.Source),
			BasePresent: providerViews[row.Name].BasePresent,
		}
		providerViews[row.Name] = view
		if row.Enabled != nil && !*row.Enabled {
			delete(merged.Models.Providers, row.Name)
			continue
		}
		merged.Models.Providers[row.Name] = view.Config
	}

	appliedChannelOverlay := false
	for _, row := range channelRows {
		if row.Source != store.ConfigSourceAPI {
			continue
		}
		appliedChannelOverlay = true
		recognized, err := applyChannelOverlay(&merged.Channels, row)
		if err != nil {
			return State{}, err
		}
		view := ChannelProjection{
			Name:        row.Name,
			Config:      channelConfigPayload(row),
			Enabled:     cloneBoolPtr(row.Enabled),
			Source:      string(row.Source),
			Recognized:  recognized,
			BasePresent: channelViews[row.Name].BasePresent,
		}
		channelViews[row.Name] = view
	}

	appliedSettingsOverlay := false
	settingViews := make([]SettingProjection, 0, len(settingRows))
	for _, row := range settingRows {
		if row.Source != store.ConfigSourceAPI {
			continue
		}
		result, err := applySettingOverlay(&merged, row, settingsRegistry)
		if err != nil {
			return State{}, err
		}
		if result.Applied {
			appliedSettingsOverlay = true
		}
		settingViews = append(settingViews, SettingProjection{
			Key:     strings.TrimSpace(row.Key),
			Value:   json.RawMessage(append([]byte(nil), row.Value...)),
			Source:  string(row.Source),
			Domain:  result.Domain,
			Applied: result.Applied,
			Legacy:  result.Legacy,
		})
	}

	merged.ApplyDefaults()
	normalizeModelSelection(&merged)
	if err := merged.Validate(); err != nil {
		return State{}, fmt.Errorf("validate effective config: %w", err)
	}
	runtimeCfg, err := deepCloneConfig(merged)
	if err != nil {
		return State{}, fmt.Errorf("clone runtime config: %w", err)
	}
	runtimeCfg.ResolveSecrets(keychain.ResolveField)

	layers := cloneLayers(baseLayers)
	if appliedProviderOverlay {
		layers = append(layers, controlsnapshot.Layer{
			Name:   "provider-overlay",
			Kind:   "overlay",
			Source: "config_store",
		})
	}
	if appliedChannelOverlay {
		layers = append(layers, controlsnapshot.Layer{
			Name:   "channel-overlay",
			Kind:   "overlay",
			Source: "config_store",
		})
	}
	if appliedSettingsOverlay {
		layers = append(layers, controlsnapshot.Layer{
			Name:   "settings-overlay",
			Kind:   "overlay",
			Source: "config_store",
		})
	}

	state := State{
		Config:        merged,
		RuntimeConfig: runtimeCfg,
		Providers:     orderedProviderProjections(providerViews),
		Channels:      orderedChannelProjections(channelViews),
		Settings:      orderedSettingProjections(settingViews),
		Layers:        layers,
	}
	if buildSnapshot != nil {
		state.Snapshot = buildSnapshot(merged, layers)
	}
	if state.Snapshot != nil {
		state.Version = strings.TrimSpace(state.Snapshot.ID)
	}
	if state.Version == "" {
		state.Version = "cfg-" + store.HashConfig(struct {
			Config config.Config
			Layers []controlsnapshot.Layer
		}{
			Config: merged,
			Layers: layers,
		})
	}
	return state, nil
}

func applyChannelOverlay(channels *config.ChannelsConfig, row store.ChannelConfigRow) (bool, error) {
	if channels == nil {
		return false, nil
	}
	value := reflect.ValueOf(channels).Elem()
	valueType := value.Type()
	targetName := strings.TrimSpace(row.Name)
	if targetName == "" {
		return false, nil
	}
	for index := 0; index < value.NumField(); index++ {
		fieldType := valueType.Field(index)
		tag := yamlTagName(fieldType.Tag.Get("yaml"))
		if !strings.EqualFold(tag, targetName) {
			continue
		}
		field := value.Field(index)
		holder := reflect.New(field.Type())
		payload := strings.TrimSpace(row.Config)
		if payload == "" {
			payload = "{}"
		}
		if err := yaml.Unmarshal([]byte(payload), holder.Interface()); err != nil {
			return true, fmt.Errorf("decode channel overlay %q: %w", row.Name, err)
		}
		setEnabledField(holder.Elem(), row.Enabled)
		field.Set(holder.Elem())
		return true, nil
	}
	return false, nil
}

func applySettingOverlay(cfg *config.Config, row store.DynamicSettingRow, registry *SettingPolicyRegistry) (SettingApplyResult, error) {
	if cfg == nil {
		return SettingApplyResult{}, nil
	}
	key := strings.TrimSpace(row.Key)
	if !strings.HasPrefix(key, "section.") {
		if registry == nil {
			return SettingApplyResult{}, nil
		}
		policy, ok := registry.Lookup(key)
		if !ok {
			return SettingApplyResult{}, nil
		}
		if err := policy.Apply(cfg, json.RawMessage(row.Value)); err != nil {
			return SettingApplyResult{}, err
		}
		return SettingApplyResult{
			Applied: true,
			Domain:  strings.TrimSpace(policy.Domain),
		}, nil
	}
	section := strings.TrimSpace(strings.TrimPrefix(key, "section."))
	if section == "" {
		return SettingApplyResult{}, nil
	}

	baseBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return SettingApplyResult{}, fmt.Errorf("marshal config for overlay %q: %w", key, err)
	}
	root := make(map[string]any)
	if err := yaml.Unmarshal(baseBytes, &root); err != nil {
		return SettingApplyResult{}, fmt.Errorf("decode config for overlay %q: %w", key, err)
	}

	var value any
	if err := json.Unmarshal([]byte(row.Value), &value); err != nil {
		return SettingApplyResult{}, fmt.Errorf("decode setting overlay %q: %w", key, err)
	}
	root[section] = value

	nextBytes, err := yaml.Marshal(root)
	if err != nil {
		return SettingApplyResult{}, fmt.Errorf("encode setting overlay %q: %w", key, err)
	}
	var next config.Config
	if err := yaml.Unmarshal(nextBytes, &next); err != nil {
		return SettingApplyResult{}, fmt.Errorf("apply setting overlay %q: %w", key, err)
	}
	*cfg = next
	return SettingApplyResult{
		Applied: true,
		Domain:  "legacy_section",
		Legacy:  true,
	}, nil
}

func setEnabledField(value reflect.Value, enabled *bool) {
	if enabled == nil || !value.IsValid() || value.Kind() != reflect.Struct {
		return
	}
	field := value.FieldByName("Enabled")
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.Ptr {
		return
	}
	field.Set(reflect.ValueOf(cloneBoolPtr(enabled)))
}

func yamlTagName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" || name == "-" {
		return ""
	}
	if cut, _, ok := strings.Cut(name, ","); ok {
		return strings.TrimSpace(cut)
	}
	return name
}

func deepCloneConfig(cfg config.Config) (config.Config, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return config.Config{}, err
	}
	var cloned config.Config
	if err := yaml.Unmarshal(data, &cloned); err != nil {
		return config.Config{}, err
	}
	return cloned, nil
}

func configuredBaseChannels(channels config.ChannelsConfig) map[string]any {
	result := make(map[string]any)
	value := reflect.ValueOf(channels)
	valueType := value.Type()
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		fieldType := valueType.Field(index)
		if !channelConfigured(field) {
			continue
		}
		name := yamlTagName(fieldType.Tag.Get("yaml"))
		if name == "" {
			continue
		}
		result[name] = field.Interface()
	}
	return result
}

func channelConfigured(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.Struct:
		commonType := reflect.TypeOf(config.CommonChannelConfig{})
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			fieldType := value.Type().Field(index)
			if fieldType.Anonymous && fieldType.Type == commonType {
				continue
			}
			if fieldType.Type == commonType {
				continue
			}
			if ignorableChannelPolicyField(fieldType.Name) {
				continue
			}
			if fieldType.Name == "Enabled" {
				if !field.IsNil() {
					return true
				}
				continue
			}
			if meaningfulValue(field) {
				return true
			}
		}
		return false
	default:
		return meaningfulValue(value)
	}
}

func ignorableChannelPolicyField(name string) bool {
	switch name {
	case "DMPolicy", "AllowFrom", "GroupPolicy", "GroupAllowFrom", "RequireMention", "GroupSessionScope", "ReplyInThread", "DedupeTTL", "DedupeDir", "Domain", "ConnectionMode":
		return true
	default:
		return false
	}
}

func meaningfulValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Ptr, reflect.Interface:
		return !value.IsNil()
	case reflect.String:
		return strings.TrimSpace(value.String()) != ""
	case reflect.Map, reflect.Slice:
		return !value.IsNil() && value.Len() > 0
	case reflect.Bool:
		return value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return value.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return value.Float() != 0
	case reflect.Struct:
		return channelConfigured(value)
	default:
		return false
	}
}

func normalizeModelSelection(cfg *config.Config) {
	if cfg == nil {
		return
	}
	names := make([]string, 0, len(cfg.Models.Providers)+1)
	if strings.TrimSpace(cfg.Models.OpenAICompat.BaseURL) != "" {
		names = append(names, "default")
	}
	for name := range cfg.Models.Providers {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		names = append(names, trimmed)
	}
	sort.Strings(names)
	if len(names) == 0 {
		cfg.Models.DefaultProvider = ""
		return
	}
	current := strings.TrimSpace(cfg.Models.DefaultProvider)
	if current != "" {
		for _, name := range names {
			if name == current {
				return
			}
		}
	}
	if len(names) == 1 {
		cfg.Models.DefaultProvider = names[0]
		return
	}
	if strings.Contains(strings.TrimSpace(cfg.Agent.DefaultModel), "/") {
		cfg.Models.DefaultProvider = ""
		return
	}
	cfg.Models.DefaultProvider = names[0]
}

func sortedProviderKeys(items map[string]config.ProviderConfig) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedAnyKeys(items map[string]any) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneProviderMap(items map[string]config.ProviderConfig) map[string]config.ProviderConfig {
	if len(items) == 0 {
		return nil
	}
	cloned := make(map[string]config.ProviderConfig, len(items))
	for key, value := range items {
		cloned[key] = value
	}
	return cloned
}

func orderedProviderProjections(items map[string]ProviderProjection) []ProviderProjection {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ProviderProjection, 0, len(keys))
	for _, key := range keys {
		out = append(out, items[key])
	}
	return out
}

func orderedChannelProjections(items map[string]ChannelProjection) []ChannelProjection {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ChannelProjection, 0, len(keys))
	for _, key := range keys {
		out = append(out, items[key])
	}
	return out
}

func orderedSettingProjections(items []SettingProjection) []SettingProjection {
	if len(items) == 0 {
		return nil
	}
	out := cloneSettingProjections(items)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	return out
}

func cloneProviderProjections(items []ProviderProjection) []ProviderProjection {
	if len(items) == 0 {
		return nil
	}
	out := make([]ProviderProjection, len(items))
	copy(out, items)
	for index := range out {
		out[index].Enabled = cloneBoolPtr(out[index].Enabled)
	}
	return out
}

func cloneChannelProjections(items []ChannelProjection) []ChannelProjection {
	if len(items) == 0 {
		return nil
	}
	out := make([]ChannelProjection, len(items))
	copy(out, items)
	for index := range out {
		out[index].Enabled = cloneBoolPtr(out[index].Enabled)
		if len(out[index].Config) > 0 {
			out[index].Config = append(json.RawMessage(nil), out[index].Config...)
		}
	}
	return out
}

func cloneSettingProjections(items []SettingProjection) []SettingProjection {
	if len(items) == 0 {
		return nil
	}
	out := make([]SettingProjection, len(items))
	for index, item := range items {
		out[index] = SettingProjection{
			Key:     item.Key,
			Value:   append(json.RawMessage(nil), item.Value...),
			Source:  item.Source,
			Domain:  item.Domain,
			Applied: item.Applied,
			Legacy:  item.Legacy,
		}
	}
	return out
}

func cloneLayers(items []controlsnapshot.Layer) []controlsnapshot.Layer {
	if len(items) == 0 {
		return nil
	}
	out := make([]controlsnapshot.Layer, len(items))
	copy(out, items)
	return out
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func marshalChannelProjection(value any) (json.RawMessage, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(payload), nil
}

func channelConfigPayload(row store.ChannelConfigRow) json.RawMessage {
	payload := strings.TrimSpace(row.Config)
	if payload == "" {
		payload = "{}"
	}
	return json.RawMessage([]byte(payload))
}
