package gateway

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"gopkg.in/yaml.v3"
)

func (g *Gateway) fileBackedConfig() bool {
	return g != nil && strings.TrimSpace(g.configPath) != ""
}

// modifyConfigFile safely modifies the YAML config file with locking and backup.
// The mutate function receives the parsed root map and should modify it in place.
// This is used for legacy YAML-based config modifications.
func (g *Gateway) modifyConfigFile(mutate func(root map[string]any) error) error {
	g.configMu.Lock()
	defer g.configMu.Unlock()

	data, err := os.ReadFile(g.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}
	if root == nil {
		root = make(map[string]any)
	}

	if err := mutate(root); err != nil {
		return err
	}

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	bakPath := g.configPath + ".bak"
	if err := os.WriteFile(bakPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write backup file: %w", err)
	}

	if err := os.WriteFile(g.configPath, out, 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func (g *Gateway) writeConfigSection(section string, value any) error {
	section = strings.TrimSpace(section)
	if section == "" {
		return fmt.Errorf("section is required")
	}
	return g.modifyConfigFile(func(root map[string]any) error {
		root[section] = value
		return nil
	})
}

func (g *Gateway) upsertProviderInFile(name string, cfg config.ProviderConfig) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("provider name is required")
	}
	payload := providerConfigFilePayload(name, cfg)
	return g.modifyConfigFile(func(root map[string]any) error {
		modelsNode := ensureObjectMap(root["models"])
		providersNode := ensureObjectMap(modelsNode["providers"])
		providersNode[name] = payload
		modelsNode["providers"] = providersNode
		root["models"] = modelsNode
		return nil
	})
}

func (g *Gateway) deleteProviderInFile(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("provider name is required")
	}
	return g.modifyConfigFile(func(root map[string]any) error {
		modelsNode := ensureObjectMap(root["models"])
		providersNode := ensureObjectMap(modelsNode["providers"])
		delete(providersNode, name)
		modelsNode["providers"] = providersNode
		root["models"] = modelsNode
		return nil
	})
}

func (g *Gateway) upsertChannelInFile(name string, raw json.RawMessage, enabled *bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("channel name is required")
	}
	canonicalName, ok := canonicalFileBackedChannelName(name)
	if !ok {
		return invalidFileBackedChannelName(name)
	}
	payload, err := decodeFileBackedChannelConfig(canonicalName, raw, enabled)
	if err != nil {
		return err
	}
	return g.modifyConfigFile(func(root map[string]any) error {
		channelsNode := ensureObjectMap(root["channels"])
		channelsNode[canonicalName] = payload
		root["channels"] = channelsNode
		return nil
	})
}

func (g *Gateway) deleteChannelInFile(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("channel name is required")
	}
	canonicalName, ok := canonicalFileBackedChannelName(name)
	if !ok {
		return invalidFileBackedChannelName(name)
	}
	return g.modifyConfigFile(func(root map[string]any) error {
		channelsNode := ensureObjectMap(root["channels"])
		delete(channelsNode, canonicalName)
		root["channels"] = channelsNode
		return nil
	})
}

func decodeFileBackedChannelConfig(name string, raw json.RawMessage, enabled *bool) (map[string]any, error) {
	payload := make(map[string]any)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("invalid channel config json: %w", err)
		}
	}
	if rawType, ok := payload["type"]; ok {
		channelType, err := decodeFileBackedChannelType(rawType)
		if err != nil {
			return nil, err
		}
		if channelType != "" && channelType != name {
			return nil, fmt.Errorf("channel name %q must match config.type %q for file-backed config", name, channelType)
		}
		delete(payload, "type")
	}
	if enabled != nil {
		payload["enabled"] = *enabled
	}
	return payload, nil
}

func validateFileBackedChannelInputType(name string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	payload := make(map[string]any)
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("invalid channel config json: %w", err)
	}
	rawType, ok := payload["type"]
	if !ok {
		return nil
	}
	channelType, err := decodeFileBackedChannelType(rawType)
	if err != nil {
		return err
	}
	if channelType != "" && channelType != name {
		return fmt.Errorf("channel name %q must match config.type %q for file-backed config", name, channelType)
	}
	return nil
}

func canonicalFileBackedChannelName(name string) (string, bool) {
	target := normalizeFileBackedChannelName(name)
	if target == "" {
		return "", false
	}
	typ := reflect.TypeOf(config.ChannelsConfig{})
	for index := 0; index < typ.NumField(); index++ {
		tag := typ.Field(index).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		fieldName, _, _ := strings.Cut(tag, ",")
		canonical := strings.TrimSpace(fieldName)
		if normalizeFileBackedChannelName(canonical) == target {
			return canonical, true
		}
	}
	return "", false
}

func normalizeFileBackedChannelName(name string) string {
	normalized := strings.TrimSpace(strings.ToLower(name))
	return strings.ReplaceAll(normalized, "-", "_")
}

func invalidFileBackedChannelName(name string) error {
	return fmt.Errorf("channel %q is not file-backed; use a canonical channel id from /operator/setup/catalog (for example %q)", strings.TrimSpace(name), "slack")
}

func decodeFileBackedChannelType(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return "", nil
		}
		canonical, ok := canonicalFileBackedChannelName(trimmed)
		if !ok {
			return "", fmt.Errorf("channel config.type %q is not file-backed; use a canonical channel id from /operator/setup/catalog", trimmed)
		}
		return canonical, nil
	default:
		return "", fmt.Errorf("channel config.type must be a string")
	}
}

func providerConfigFilePayload(name string, cfg config.ProviderConfig) map[string]any {
	cfg = config.NormalizeProviderConfig(name, cfg)
	payload := make(map[string]any)
	if cfg.API != "" {
		payload["api"] = cfg.API
	}
	if cfg.BaseURL != "" {
		payload["base_url"] = cfg.BaseURL
	}
	if cfg.Region != "" {
		payload["region"] = cfg.Region
	}
	if cfg.APIKey != "" {
		payload["api_key"] = cfg.APIKey
	}
	if len(cfg.APIKeys) > 0 {
		payload["api_keys"] = append([]string(nil), cfg.APIKeys...)
	}
	if cfg.AccessKeyID != "" {
		payload["access_key_id"] = cfg.AccessKeyID
	}
	if cfg.SecretKey != "" {
		payload["secret_key"] = cfg.SecretKey
	}
	if cfg.SessionToken != "" {
		payload["session_token"] = cfg.SessionToken
	}
	if cfg.DefaultModel != "" {
		payload["default_model"] = cfg.DefaultModel
	}
	if cfg.Timeout > 0 {
		payload["timeout"] = cfg.Timeout.String()
	}
	if len(cfg.Headers) > 0 {
		headers := make(map[string]string, len(cfg.Headers))
		for key, value := range cfg.Headers {
			headers[key] = value
		}
		payload["headers"] = headers
	}
	return payload
}
