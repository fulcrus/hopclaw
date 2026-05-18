package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/model"
	"github.com/fulcrus/hopclaw/store"
)

type providerConnectionInput struct {
	Provider        string            `json:"provider"`
	CatalogProvider string            `json:"catalog_provider,omitempty"`
	API             string            `json:"api"`
	BaseURL         string            `json:"base_url"`
	Region          string            `json:"region,omitempty"`
	APIKey          string            `json:"api_key"`
	APIKeys         []string          `json:"api_keys,omitempty"`
	AccessKeyID     string            `json:"access_key_id,omitempty"`
	SecretKey       string            `json:"secret_key,omitempty"`
	SessionToken    string            `json:"session_token,omitempty"`
	DefaultModel    string            `json:"default_model"`
	Timeout         string            `json:"timeout,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
}

type providerMutationRequest struct {
	Name         *string            `json:"name,omitempty"`
	API          *string            `json:"api,omitempty"`
	BaseURL      *string            `json:"base_url,omitempty"`
	Region       *string            `json:"region,omitempty"`
	APIKey       *string            `json:"api_key,omitempty"`
	APIKeys      *[]string          `json:"api_keys,omitempty"`
	AccessKeyID  *string            `json:"access_key_id,omitempty"`
	SecretKey    *string            `json:"secret_key,omitempty"`
	SessionToken *string            `json:"session_token,omitempty"`
	DefaultModel *string            `json:"default_model,omitempty"`
	Timeout      *string            `json:"timeout,omitempty"`
	Headers      *map[string]string `json:"headers,omitempty"`
}

type providerOperatorState struct {
	Infos              []providerInfo
	Entries            map[string]model.ProviderEntry
	CapabilityMatrices map[string]model.CapabilityMatrix
	DefaultProvider    string
	AgentDefaultModel  string
}

type modelMetaJSON struct {
	Provider      string                  `json:"provider"`
	Model         string                  `json:"model"`
	DisplayName   string                  `json:"display_name"`
	ContextWindow int                     `json:"context_window"`
	MaxOutput     int                     `json:"max_output"`
	Capabilities  []model.ModelCapability `json:"capabilities"`
}

type providerConfigPatch struct {
	API          *string
	BaseURL      *string
	Region       *string
	APIKey       *string
	APIKeys      *[]string
	AccessKeyID  *string
	SecretKey    *string
	SessionToken *string
	DefaultModel *string
	Timeout      *time.Duration
	Headers      *map[string]string
}

func (req providerMutationRequest) providerConfig(name string) (config.ProviderConfig, error) {
	patch, err := req.configPatch()
	if err != nil {
		return config.ProviderConfig{}, err
	}
	return patch.apply(name, config.ProviderConfig{}), nil
}

func (req providerMutationRequest) mergeProviderConfig(name string, base config.ProviderConfig) (config.ProviderConfig, error) {
	patch, err := req.configPatch()
	if err != nil {
		return config.ProviderConfig{}, err
	}
	return patch.apply(name, base), nil
}

func (req providerMutationRequest) configPatch() (providerConfigPatch, error) {
	patch := providerConfigPatch{
		API:          req.API,
		BaseURL:      req.BaseURL,
		Region:       req.Region,
		APIKey:       req.APIKey,
		APIKeys:      req.APIKeys,
		AccessKeyID:  req.AccessKeyID,
		SecretKey:    req.SecretKey,
		SessionToken: req.SessionToken,
		DefaultModel: req.DefaultModel,
		Headers:      req.Headers,
	}
	if req.Timeout != nil {
		timeout, err := parseProviderTimeout(*req.Timeout)
		if err != nil {
			return providerConfigPatch{}, err
		}
		patch.Timeout = &timeout
	}
	return patch, nil
}

func (req providerMutationRequest) resolveName(routeName string) (string, error) {
	bodyName := ""
	if req.Name != nil {
		bodyName = strings.TrimSpace(*req.Name)
	}
	routeName = strings.TrimSpace(routeName)
	switch {
	case req.Name != nil && bodyName == "":
		return "", fmt.Errorf("name is required")
	case routeName == "" && bodyName == "":
		return "", fmt.Errorf("name is required")
	case routeName == "":
		return bodyName, nil
	case bodyName == "":
		return routeName, nil
	case bodyName != routeName:
		return "", fmt.Errorf("body name %q does not match route name %q", bodyName, routeName)
	default:
		return routeName, nil
	}
}

func (input providerConnectionInput) providerName() string {
	return strings.TrimSpace(input.Provider)
}

func (input providerConnectionInput) catalogProviderName() string {
	return strings.TrimSpace(input.CatalogProvider)
}

func (input providerConnectionInput) providerConfig(name string) (config.ProviderConfig, error) {
	timeout, err := parseProviderTimeout(input.Timeout)
	if err != nil {
		return config.ProviderConfig{}, err
	}
	return config.NormalizeProviderConfig(name, config.ProviderConfig{
		API:          canonicalProviderAPI(input.API),
		BaseURL:      strings.TrimSpace(input.BaseURL),
		Region:       strings.TrimSpace(input.Region),
		APIKey:       strings.TrimSpace(input.APIKey),
		APIKeys:      append([]string(nil), input.APIKeys...),
		AccessKeyID:  strings.TrimSpace(input.AccessKeyID),
		SecretKey:    strings.TrimSpace(input.SecretKey),
		SessionToken: strings.TrimSpace(input.SessionToken),
		DefaultModel: strings.TrimSpace(input.DefaultModel),
		Timeout:      timeout,
		Headers:      cloneStringMap(input.Headers),
	}), nil
}

func (input providerConnectionInput) mergeProviderConfig(name string, base config.ProviderConfig) (config.ProviderConfig, error) {
	next := base
	if value := canonicalProviderAPI(input.API); value != "" {
		next.API = value
	}
	if value := strings.TrimSpace(input.BaseURL); value != "" {
		next.BaseURL = value
	}
	if value := strings.TrimSpace(input.Region); value != "" {
		next.Region = value
	}
	if value := strings.TrimSpace(input.APIKey); value != "" {
		next.APIKey = value
	}
	if input.APIKeys != nil {
		next.APIKeys = append([]string(nil), input.APIKeys...)
	}
	if value := strings.TrimSpace(input.AccessKeyID); value != "" {
		next.AccessKeyID = value
	}
	if value := strings.TrimSpace(input.SecretKey); value != "" {
		next.SecretKey = value
	}
	if value := strings.TrimSpace(input.SessionToken); value != "" {
		next.SessionToken = value
	}
	if value := strings.TrimSpace(input.DefaultModel); value != "" {
		next.DefaultModel = value
	}
	if raw := strings.TrimSpace(input.Timeout); raw != "" {
		timeout, err := parseProviderTimeout(raw)
		if err != nil {
			return config.ProviderConfig{}, err
		}
		next.Timeout = timeout
	}
	if input.Headers != nil {
		next.Headers = cloneStringMap(input.Headers)
	}
	return config.NormalizeProviderConfig(name, next), nil
}

func (patch providerConfigPatch) apply(name string, base config.ProviderConfig) config.ProviderConfig {
	next := base
	if patch.API != nil {
		next.API = canonicalProviderAPI(strings.TrimSpace(*patch.API))
	}
	if patch.BaseURL != nil {
		next.BaseURL = strings.TrimSpace(*patch.BaseURL)
	}
	if patch.Region != nil {
		next.Region = strings.TrimSpace(*patch.Region)
	}
	if patch.APIKey != nil {
		next.APIKey = preserveProviderSecretString(base.APIKey, *patch.APIKey)
	}
	if patch.APIKeys != nil {
		next.APIKeys = preserveProviderSecretSlice(base.APIKeys, *patch.APIKeys)
	}
	if patch.AccessKeyID != nil {
		next.AccessKeyID = strings.TrimSpace(*patch.AccessKeyID)
	}
	if patch.SecretKey != nil {
		next.SecretKey = preserveProviderSecretString(base.SecretKey, *patch.SecretKey)
	}
	if patch.SessionToken != nil {
		next.SessionToken = preserveProviderSecretString(base.SessionToken, *patch.SessionToken)
	}
	if patch.DefaultModel != nil {
		next.DefaultModel = strings.TrimSpace(*patch.DefaultModel)
	}
	if patch.Timeout != nil {
		next.Timeout = *patch.Timeout
	}
	if patch.Headers != nil {
		next.Headers = preserveProviderHeaderPatch(base.Headers, *patch.Headers)
	}
	return config.NormalizeProviderConfig(name, next)
}

func preserveProviderSecretString(current string, next string) string {
	if config.IsSecretPlaceholder(next) {
		return strings.TrimSpace(current)
	}
	return strings.TrimSpace(next)
}

func preserveProviderSecretSlice(current []string, next []string) []string {
	if next == nil {
		return nil
	}
	out := make([]string, 0, len(next))
	for index, value := range next {
		if config.IsSecretPlaceholder(value) {
			if index < len(current) {
				out = append(out, strings.TrimSpace(current[index]))
			}
			continue
		}
		out = append(out, strings.TrimSpace(value))
	}
	return out
}

func preserveProviderHeaderPatch(current map[string]string, next map[string]string) map[string]string {
	if next == nil {
		return nil
	}
	out := make(map[string]string, len(next))
	for key, value := range next {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if name == "" {
			continue
		}
		if config.IsSecretPlaceholder(value) {
			out[name] = strings.TrimSpace(current[name])
			continue
		}
		out[name] = strings.TrimSpace(value)
	}
	return out
}

func providerEntryFromConnectionInput(input providerConnectionInput) (model.ProviderEntry, error) {
	providerName := input.providerName()
	cfg, err := input.providerConfig(providerName)
	if err != nil {
		return model.ProviderEntry{}, err
	}
	return tempProviderEntry(providerName, input.catalogProviderName(), cfg), nil
}

func buildTempRegistry(input providerConnectionInput) (*model.Registry, error) {
	providerName := input.providerName()
	if providerName == "" {
		return nil, fmt.Errorf("provider is required")
	}
	cfg, err := input.providerConfig(providerName)
	if err != nil {
		return nil, err
	}
	entry := tempProviderEntry(providerName, input.catalogProviderName(), cfg)
	return model.NewRegistry(map[string]model.ProviderEntry{
		providerName: entry,
	})
}

func tempProviderEntry(providerName, catalogProvider string, cfg config.ProviderConfig) model.ProviderEntry {
	entry := config.ProviderEntryFromConfig(providerName, cfg)
	hydrateTempProviderEntry(&entry, providerName, catalogProvider)
	return entry
}

func hydrateTempProviderEntry(entry *model.ProviderEntry, providerName, catalogProvider string) {
	for _, candidate := range []string{strings.TrimSpace(catalogProvider), strings.TrimSpace(providerName)} {
		if candidate == "" {
			continue
		}
		if catalog, ok := model.CatalogLookup(candidate); ok {
			mergeTempProviderCatalogEntry(entry, catalog.Provider)
			return
		}
	}
	if entry.API == "" {
		return
	}
	if value := config.ProviderAPIFieldDefault(string(entry.API), "base_url"); entry.BaseURL == "" && value != "" {
		entry.BaseURL = value
	}
	if value := config.ProviderAPIFieldDefault(string(entry.API), "default_model"); entry.DefaultModel == "" && value != "" {
		entry.DefaultModel = value
	}
}

func mergeTempProviderCatalogEntry(entry *model.ProviderEntry, defaults model.ProviderEntry) {
	if entry.API == "" {
		entry.API = model.NormalizeProviderAPI(defaults.API)
	}
	if entry.BaseURL == "" {
		entry.BaseURL = defaults.BaseURL
	}
	if entry.Region == "" {
		entry.Region = defaults.Region
	}
	if entry.DefaultModel == "" {
		entry.DefaultModel = defaults.DefaultModel
	}
}

func executeTempProviderChat(ctx context.Context, input providerConnectionInput, message string) (*agent.ModelResponse, time.Duration, error) {
	registry, err := buildTempRegistry(input)
	if err != nil {
		return nil, 0, err
	}

	start := time.Now()
	resp, err := registry.Chat(ctx, agent.ChatRequest{
		Model: strings.TrimSpace(input.DefaultModel),
		Messages: []contextengine.Message{
			{Role: contextengine.RoleUser, Content: strings.TrimSpace(message)},
		},
	})
	return resp, time.Since(start), err
}

func openAICompatConfigured(cfg config.Config) bool {
	entry, ok := config.OpenAICompatProviderEntry(cfg.Models.OpenAICompat)
	return ok && strings.TrimSpace(entry.DefaultModel) != ""
}

func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "***" + key[len(key)-4:]
}

func toModelMetaJSON(metas []model.ModelMeta) []modelMetaJSON {
	out := make([]modelMetaJSON, len(metas))
	for i, m := range metas {
		out[i] = modelMetaJSON{
			Provider:      m.Provider,
			Model:         m.Model,
			DisplayName:   m.DisplayName,
			ContextWindow: m.ContextWindow,
			MaxOutput:     m.MaxOutput,
			Capabilities:  m.Capabilities,
		}
	}
	return out
}

// GetEffectiveProviders returns merged provider configs from SQLite.
// This is used by other components that need the effective configuration.
func (g *Gateway) GetEffectiveProviders(ctx context.Context) (map[string]config.ProviderConfig, error) {
	if g == nil || g.effectiveCfg == nil {
		return nil, fmt.Errorf("effective config is not available")
	}
	return g.effectiveCfg.RuntimeCurrent().Models.Providers, nil
}

func (g *Gateway) currentProviderProjections() ([]controlplane.ProviderProjection, bool) {
	if g == nil || g.effectiveCfg == nil {
		return nil, false
	}
	return g.effectiveCfg.Providers(), true
}

func (g *Gateway) providerProjection(name string) (controlplane.ProviderProjection, bool) {
	projections, ok := g.currentProviderProjections()
	if !ok {
		return controlplane.ProviderProjection{}, false
	}
	name = strings.TrimSpace(name)
	for _, item := range projections {
		if item.Name == name {
			return item, true
		}
	}
	return controlplane.ProviderProjection{}, false
}

func (g *Gateway) currentProviderOperatorState() (providerOperatorState, bool) {
	cfg, ok := g.currentOperatorConfig()
	if !ok {
		return providerOperatorState{}, false
	}
	projections, ok := g.currentProviderProjections()
	if !ok {
		return providerOperatorState{}, false
	}

	state := providerOperatorState{
		Infos:              make([]providerInfo, 0, len(projections)+1),
		Entries:            make(map[string]model.ProviderEntry, len(projections)+1),
		CapabilityMatrices: make(map[string]model.CapabilityMatrix, len(projections)+1),
		AgentDefaultModel:  strings.TrimSpace(cfg.Agent.DefaultModel),
	}

	hasDefaultProjection := false
	for _, item := range projections {
		if item.Name == "default" {
			hasDefaultProjection = true
			break
		}
	}
	if openAICompatConfigured(cfg) && !hasDefaultProjection {
		info := providerInfoFromOpenAICompat(cfg.Models.OpenAICompat)
		state.Infos = append(state.Infos, info)
		if entry, ok := config.OpenAICompatProviderEntry(cfg.Models.OpenAICompat); ok {
			state.Entries["default"] = entry
			state.CapabilityMatrices["default"] = info.CapabilityMatrix
		}
	}
	for _, item := range projections {
		info := providerInfoFromConfig(item.Name, item.Config, item.Enabled, item.Source)
		state.Infos = append(state.Infos, info)
		state.Entries[item.Name] = config.ProviderEntryFromConfig(item.Name, item.Config)
		state.CapabilityMatrices[item.Name] = info.CapabilityMatrix
	}
	appendModuleProviderOperatorState(&state, g.moduleCatalog)
	sortProviderInfos(state.Infos)
	state.DefaultProvider = model.ResolveDefaultProvider(state.Entries, cfg.Models.DefaultProvider)
	return state, true
}

func appendModuleProviderOperatorState(state *providerOperatorState, catalog *modules.Store) bool {
	if state == nil || catalog == nil {
		return false
	}
	projections := catalog.ProviderProjections()
	if len(projections) == 0 {
		return false
	}
	appended := false
	for _, projection := range projections {
		if _, exists := state.Entries[projection.Name]; exists {
			continue
		}
		entry := providerEntryFromModuleProjection(projection)
		info := providerInfoFromModuleProjection(projection, entry)
		state.Infos = append(state.Infos, info)
		state.Entries[projection.Name] = entry
		state.CapabilityMatrices[projection.Name] = info.CapabilityMatrix
		appended = true
	}
	return appended
}

func (g *Gateway) ensureProviderMutationAvailable() error {
	if !g.fileBackedConfig() && g.configMutator == nil {
		return controlplane.ErrMutationUnavailable
	}
	return nil
}

func (g *Gateway) upsertProviderConfig(ctx context.Context, name string, cfg config.ProviderConfig, enabled *bool) error {
	cfg = config.NormalizeProviderConfig(name, cfg)
	if g.fileBackedConfig() {
		if err := g.upsertProviderInFile(name, cfg); err != nil {
			return err
		}
		return g.triggerConfigReload()
	}
	return g.configMutator.UpsertProvider(ctx, providerRowFromConfig(name, cfg, enabled))
}

func (g *Gateway) deleteProviderConfig(ctx context.Context, name string, basePresent bool) error {
	if g.fileBackedConfig() {
		if err := g.deleteProviderInFile(name); err != nil {
			return err
		}
		return g.triggerConfigReload()
	}
	return g.configMutator.DeleteProvider(ctx, name, basePresent)
}

func providerRowFromConfig(name string, cfg config.ProviderConfig, enabled *bool) store.ProviderConfigRow {
	headersJSON, _ := json.Marshal(cfg.Headers)
	return store.ProviderConfigRow{
		Name:         strings.TrimSpace(name),
		API:          canonicalProviderAPI(cfg.API),
		BaseURL:      cfg.BaseURL,
		Region:       cfg.Region,
		APIKey:       cfg.APIKey,
		APIKeys:      append([]string(nil), cfg.APIKeys...),
		AccessKeyID:  cfg.AccessKeyID,
		SecretKey:    cfg.SecretKey,
		SessionToken: cfg.SessionToken,
		DefaultModel: cfg.DefaultModel,
		TimeoutSec:   int(cfg.Timeout / time.Second),
		Headers:      string(headersJSON),
		Enabled:      cloneBoolPtrGateway(enabled),
	}
}

func providerInfoFromOpenAICompat(cfg config.OpenAICompatConfig) providerInfo {
	entry, _ := config.OpenAICompatProviderEntry(cfg)
	return providerInfo{
		Name:             "default",
		API:              string(entry.API),
		BaseURL:          entry.BaseURL,
		DefaultModel:     entry.DefaultModel,
		HasKey:           strings.TrimSpace(cfg.APIKey) != "",
		Timeout:          durationString(cfg.Timeout),
		HeaderCount:      len(cfg.Headers),
		Source:           "openai_compat",
		Mutable:          false,
		ConfigScope:      "openai_compat",
		CapabilityMatrix: model.CapabilityMatrixForProvider("default", entry),
	}
}

func providerInfoFromConfig(name string, cfg config.ProviderConfig, enabled *bool, source string) providerInfo {
	entry := config.ProviderEntryFromConfig(name, cfg)
	return providerInfo{
		Name:             name,
		API:              string(entry.API),
		BaseURL:          entry.BaseURL,
		Region:           entry.Region,
		AccessKeyID:      entry.AccessKeyID,
		DefaultModel:     entry.DefaultModel,
		Models:           toModelMetaJSON(model.ModelsForProvider(name)),
		HasKey:           config.ProviderConfigHasCredentials(cfg),
		APIKeysCount:     len(cfg.APIKeys),
		Enabled:          enabled,
		Timeout:          durationString(cfg.Timeout),
		HeaderCount:      len(cfg.Headers),
		Source:           normalizeProviderSource(source),
		Mutable:          true,
		ConfigScope:      "providers",
		CapabilityMatrix: model.CapabilityMatrixForProvider(strings.TrimSpace(name), entry),
	}
}

func providerInfoFromModuleProjection(projection modules.ProviderProjection, entry model.ProviderEntry) providerInfo {
	source := strings.TrimSpace(string(projection.Source))
	if source == "" {
		source = "module"
	}
	configScope := "module"
	if projection.Source == modules.SourcePlugin {
		configScope = "plugin"
	}
	return providerInfo{
		Name:             strings.TrimSpace(projection.Name),
		API:              string(model.NormalizeProviderAPI(entry.API)),
		BaseURL:          strings.TrimSpace(entry.BaseURL),
		Region:           strings.TrimSpace(entry.Region),
		DefaultModel:     strings.TrimSpace(entry.DefaultModel),
		Models:           toModelMetaJSON(model.ModelsForProvider(strings.TrimSpace(projection.Name))),
		HasKey:           projection.HasCredentials,
		Source:           source,
		Mutable:          false,
		ConfigScope:      configScope,
		CapabilityMatrix: model.CapabilityMatrixForProvider(strings.TrimSpace(projection.Name), entry),
	}
}

func providerEntryFromModuleProjection(projection modules.ProviderProjection) model.ProviderEntry {
	return model.ProviderEntry{
		API:          model.ProviderAPI(strings.TrimSpace(projection.API)),
		BaseURL:      strings.TrimSpace(projection.BaseURL),
		Region:       strings.TrimSpace(projection.Region),
		DefaultModel: strings.TrimSpace(projection.DefaultModel),
	}
}

func normalizeProviderSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "yaml"
	}
	return source
}

func durationString(value time.Duration) string {
	if value <= 0 {
		return ""
	}
	return value.String()
}

func parseProviderTimeout(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout %q", raw)
	}
	if value < 0 {
		return 0, fmt.Errorf("timeout must be >= 0")
	}
	return value, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortProviderInfos(items []providerInfo) {
	if len(items) < 2 {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		if items[i].Source != items[j].Source {
			return items[i].Source < items[j].Source
		}
		return items[i].API < items[j].API
	})
}
