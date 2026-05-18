package bootstrap

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	channelregistry "github.com/fulcrus/hopclaw/channels/registry"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/model"
)

const (
	configProviderModulePrefix     = "config:provider:"
	configExternalToolModulePrefix = "config:tool:"
	configChannelModulePrefix      = "config:channel:"
)

func runtimeConfigModules(cfg config.Config) []modules.StaticModule {
	out := make([]modules.StaticModule, 0)
	out = append(out, configProviderModules(cfg.Models)...)
	out = append(out, configExternalToolModules(cfg.Tools.External)...)
	out = append(out, configChannelModules(cfg)...)
	return out
}

func configProviderModules(cfg config.ModelsConfig) []modules.StaticModule {
	out := make([]modules.StaticModule, 0, len(cfg.Providers)+1)
	if entry, ok := config.OpenAICompatProviderEntry(cfg.OpenAICompat); ok {
		out = append(out, configProviderModule(configProviderModulePrefix+"default-openai-compat", "default", "models.openai_compat", entry))
	}

	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, strings.TrimSpace(name))
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "" {
			continue
		}
		entry := config.ProviderEntryFromConfig(name, cfg.Providers[name])
		out = append(out, configProviderModule(configProviderModulePrefix+name, name, "models.providers."+name, entry))
	}
	return out
}

func configProviderModule(id, name, configKey string, entry model.ProviderEntry) modules.StaticModule {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" || name == "" {
		return modules.StaticModule{}
	}
	metadata := map[string]any{
		"config_key": strings.TrimSpace(configKey),
	}
	componentMetadata := map[string]any{
		"api":             strings.TrimSpace(string(entry.API)),
		"base_url":        strings.TrimSpace(entry.BaseURL),
		"region":          strings.TrimSpace(entry.Region),
		"default_model":   strings.TrimSpace(entry.DefaultModel),
		"has_credentials": configProviderEntryHasCredentials(entry),
	}
	runtimeMetadata := map[string]any{
		"api_key":       strings.TrimSpace(entry.APIKey),
		"access_key_id": strings.TrimSpace(entry.AccessKeyID),
		"secret_key":    strings.TrimSpace(entry.SecretKey),
		"session_token": strings.TrimSpace(entry.SessionToken),
	}
	if entry.Timeout > 0 {
		componentMetadata["timeout"] = entry.Timeout.String()
	}
	if len(entry.APIKeys) > 0 {
		runtimeMetadata["api_keys"] = append([]string(nil), entry.APIKeys...)
	}
	if len(entry.Fallbacks) > 0 {
		componentMetadata["fallbacks"] = append([]string(nil), entry.Fallbacks...)
	}
	if len(entry.Headers) > 0 {
		runtimeMetadata["headers"] = cloneStringMap(entry.Headers)
	}
	return modules.StaticModule{
		ManifestValue: modules.Manifest{
			ID:             id,
			Name:           name,
			Description:    "Configured model provider",
			Kind:           "provider",
			Source:         modules.SourceExternal,
			Delivery:       modules.DeliveryWebhook,
			Level:          modules.ModuleLevelManaged,
			Metadata:       metadata,
			DefaultEnabled: true,
		},
		ContributionsValue: modules.Contributions{
			Providers: []modules.Component{{
				Kind:            modules.ComponentKindProvider,
				Name:            name,
				Description:     "Configured model provider",
				Metadata:        componentMetadata,
				RuntimeMetadata: runtimeMetadata,
			}},
		},
		HealthValue: modules.HealthReport{
			Status: modules.HealthUnknown,
		},
	}
}

func configProviderEntryHasCredentials(entry model.ProviderEntry) bool {
	return strings.TrimSpace(entry.APIKey) != "" ||
		len(entry.APIKeys) > 0 ||
		(strings.TrimSpace(entry.AccessKeyID) != "" && strings.TrimSpace(entry.SecretKey) != "")
}

func configExternalToolModules(tools []config.ExternalToolConfig) []modules.StaticModule {
	if len(tools) == 0 {
		return nil
	}
	out := make([]modules.StaticModule, 0, len(tools))
	for _, tool := range tools {
		module := configExternalToolModule(tool)
		if module.ID() == "" {
			continue
		}
		out = append(out, module)
	}
	return out
}

func configExternalToolModule(tool config.ExternalToolConfig) modules.StaticModule {
	name := strings.TrimSpace(tool.Name)
	endpoint := strings.TrimSpace(tool.Endpoint)
	if name == "" || endpoint == "" {
		return modules.StaticModule{}
	}
	timeout, _ := time.ParseDuration(strings.TrimSpace(tool.Timeout))
	componentMetadata := map[string]any{
		"endpoint": endpoint,
	}
	if timeout > 0 {
		componentMetadata["timeout"] = timeout.String()
	}
	if len(tool.InputSchema) > 0 {
		componentMetadata["input_schema"] = cloneMetadataMap(tool.InputSchema)
	}
	return modules.StaticModule{
		ManifestValue: modules.Manifest{
			ID:             configExternalToolModulePrefix + name,
			Name:           name,
			Version:        "",
			Description:    strings.TrimSpace(tool.Description),
			Kind:           "external_tool",
			Source:         modules.SourceExternal,
			Delivery:       modules.DeliveryWebhook,
			Level:          modules.ModuleLevelManaged,
			Metadata:       map[string]any{"config_key": "tools.external"},
			DefaultEnabled: true,
		},
		ContributionsValue: modules.Contributions{
			Tools: []modules.Component{{
				Kind:        modules.ComponentKindTool,
				Name:        name,
				Description: strings.TrimSpace(tool.Description),
				Metadata:    componentMetadata,
			}},
		},
		HealthValue: modules.HealthReport{
			Status: modules.HealthUnknown,
		},
	}
}

func configChannelModules(cfg config.Config) []modules.StaticModule {
	runtimeConfigs := channelregistry.BuiltinRuntimeChannelConfigs(cfg)
	if len(runtimeConfigs) == 0 {
		return nil
	}
	names := make([]string, 0, len(runtimeConfigs))
	for name := range runtimeConfigs {
		names = append(names, strings.TrimSpace(name))
	}
	sort.Strings(names)
	out := make([]modules.StaticModule, 0, len(names))
	for _, name := range names {
		module := configChannelModule(name, runtimeConfigs[name])
		if module.ID() == "" {
			continue
		}
		out = append(out, module)
	}
	return out
}

func configChannelModule(name string, runtimeConfig any) modules.StaticModule {
	name = strings.TrimSpace(name)
	if name == "" {
		return modules.StaticModule{}
	}
	configMap := configValueMap(runtimeConfig)
	componentMetadata := map[string]any{
		"type": builtinChannelModuleType(name),
	}
	runtimeMetadata := map[string]any{
		"config": configMap,
	}
	if callbackURL := configValueString(configMap, "callback_url"); callbackURL != "" {
		componentMetadata["callback_url"] = callbackURL
	}
	if secret := configValueString(configMap, "secret"); secret != "" {
		runtimeMetadata["secret"] = secret
	}
	return modules.StaticModule{
		ManifestValue: modules.Manifest{
			ID:             configChannelModulePrefix + name,
			Name:           name,
			Description:    "Configured builtin channel",
			Kind:           "channel",
			Source:         modules.SourceBuiltin,
			Delivery:       modules.DeliveryEmbedded,
			Level:          modules.ModuleLevelManaged,
			Metadata:       map[string]any{"config_key": "channels." + builtinChannelModuleType(name)},
			DefaultEnabled: true,
		},
		ContributionsValue: modules.Contributions{
			Channels: []modules.Component{{
				Kind:            modules.ComponentKindChannel,
				Name:            name,
				Description:     "Configured builtin channel",
				Metadata:        componentMetadata,
				RuntimeMetadata: runtimeMetadata,
			}},
		},
		HealthValue: modules.HealthReport{
			Status: modules.HealthUnknown,
		},
	}
}

func builtinChannelModuleType(name string) string {
	name = strings.TrimSpace(name)
	if idx := strings.Index(name, ":"); idx >= 0 {
		return strings.TrimSpace(name[:idx])
	}
	return name
}

func cloneMetadataMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = cloneMetadataValue(value)
	}
	return out
}

func cloneMetadataValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMetadataMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneMetadataValue(item))
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}

func configValueMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil
	}
	return out
}

func configValueString(config map[string]any, key string) string {
	if len(config) == 0 {
		return ""
	}
	value, ok := config[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}
