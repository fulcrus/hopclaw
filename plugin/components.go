package plugin

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/mcp"
	sdk "github.com/fulcrus/hopclaw/sdk/plugin"
)

type ComponentKind = sdk.ComponentKind

const (
	ComponentKindProvider      = sdk.ComponentKindProvider
	ComponentKindChannel       = sdk.ComponentKindChannel
	ComponentKindTool          = sdk.ComponentKindTool
	ComponentKindCommand       = sdk.ComponentKindCommand
	ComponentKindConfig        = sdk.ComponentKindConfig
	ComponentKindRuntimeBridge = sdk.ComponentKindRuntimeBridge
	ComponentKindSkillsDir     = sdk.ComponentKindSkillsDir
	ComponentKindHooksDir      = sdk.ComponentKindHooksDir
	ComponentKindMCPServer     = sdk.ComponentKindMCPServer
	ComponentKindAgent         = sdk.ComponentKindAgent
)

// ComponentDescriptor describes a single inspectable plugin component.
type ComponentDescriptor struct {
	Kind        ComponentKind  `json:"kind"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Path        string         `json:"path,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ResolvedSkillsDir returns the absolute skills directory path.
func (p LoadedPlugin) ResolvedSkillsDir() string {
	dirs := p.ResolvedSkillsDirs()
	if len(dirs) == 0 {
		return ""
	}
	return dirs[0]
}

// ResolvedSkillsDirs returns the absolute skill directory paths.
func (p LoadedPlugin) ResolvedSkillsDirs() []string {
	rawDirs := append([]string(nil), p.Manifest.SkillsDirs...)
	if len(rawDirs) == 0 && strings.TrimSpace(p.Manifest.SkillsDir) != "" {
		rawDirs = append(rawDirs, p.Manifest.SkillsDir)
	}
	if len(rawDirs) == 0 {
		return nil
	}
	out := make([]string, 0, len(rawDirs))
	seen := make(map[string]struct{}, len(rawDirs))
	for _, raw := range rawDirs {
		dir := p.resolveLocalDir(raw)
		if dir == "" {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		out = append(out, dir)
	}
	return out
}

// ResolvedHooksDir returns the absolute hooks directory path.
func (p LoadedPlugin) ResolvedHooksDir() string {
	return p.resolveLocalDir(p.Manifest.HooksDir)
}

// ResolvedMCPServerConfigs returns normalized MCP server configs contributed by
// this plugin. Relative command/work_dir entries are resolved against plugin root.
func (p LoadedPlugin) ResolvedMCPServerConfigs() []mcp.ServerConfig {
	if len(p.Manifest.MCPServers) == 0 {
		return nil
	}
	names := make([]string, 0, len(p.Manifest.MCPServers))
	for name := range p.Manifest.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]mcp.ServerConfig, 0, len(names))
	for _, name := range names {
		decl := p.Manifest.MCPServers[name]
		cfg := toMCPServerConfig(decl.ServerConfig)
		cfg.Name = scopedPluginComponentName(strings.TrimSpace(p.Manifest.Name), name)
		cfg.Command = p.resolveCommandPath(cfg.Command)
		cfg.WorkDir = p.resolveLocalDir(cfg.WorkDir)
		out = append(out, cfg)
	}
	return out
}

// Components returns all plugin-contributed components in stable order.
func (p LoadedPlugin) Components() []ComponentDescriptor {
	items := make([]ComponentDescriptor, 0)
	for name, decl := range p.Manifest.Providers {
		metadata := map[string]any{
			"api":           decl.API,
			"base_url":      decl.BaseURL,
			"default_model": decl.DefaultModel,
		}
		if len(decl.EnvVars) > 0 {
			metadata["env_vars"] = append([]string(nil), decl.EnvVars...)
		}
		if hint := strings.TrimSpace(decl.APIKeyHint); hint != "" {
			metadata["api_key_hint"] = hint
		}
		items = append(items, ComponentDescriptor{
			Kind:        ComponentKindProvider,
			Name:        name,
			Description: "Model provider",
			Metadata:    metadata,
		})
	}
	for name, decl := range p.Manifest.Channels {
		items = append(items, ComponentDescriptor{
			Kind:        ComponentKindChannel,
			Name:        name,
			Description: "Channel adapter",
			Metadata: map[string]any{
				"type":         decl.Type,
				"work_dir":     decl.WorkDir,
				"capabilities": append([]string(nil), decl.Capabilities...),
			},
		})
	}
	for _, decl := range p.Manifest.Tools {
		items = append(items, ComponentDescriptor{
			Kind:        ComponentKindTool,
			Name:        decl.Name,
			Description: decl.Description,
			Metadata: map[string]any{
				"endpoint": decl.Endpoint,
				"timeout":  decl.Timeout.String(),
			},
		})
	}
	for _, decl := range p.Manifest.Commands {
		items = append(items, ComponentDescriptor{
			Kind:        ComponentKindCommand,
			Name:        decl.Name,
			Description: decl.Description,
			Path:        p.resolveCommandPath(decl.Exec),
			Metadata: map[string]any{
				"exec": decl.Exec,
			},
		})
	}
	if metadata := p.compatibilityConfigMetadata(); len(metadata) > 0 {
		items = append(items, ComponentDescriptor{
			Kind:        ComponentKindConfig,
			Name:        "compat",
			Description: "OpenClaw compatibility config contract",
			Metadata:    metadata,
		})
	}
	if bridge := p.OpenClawRuntimeBridge(); bridge != nil {
		items = append(items, ComponentDescriptor{
			Kind:        ComponentKindRuntimeBridge,
			Name:        "openclaw-native-runtime",
			Description: "OpenClaw native runtime bridge",
			Metadata:    openClawRuntimeBridgeMetadata(*bridge),
		})
	}
	for _, dir := range p.ResolvedSkillsDirs() {
		items = append(items, ComponentDescriptor{
			Kind:        ComponentKindSkillsDir,
			Name:        filepath.Base(dir),
			Description: "Skill package directory",
			Path:        dir,
		})
	}
	if dir := p.ResolvedHooksDir(); dir != "" {
		items = append(items, ComponentDescriptor{
			Kind:        ComponentKindHooksDir,
			Name:        filepath.Base(dir),
			Description: "Hook package directory",
			Path:        dir,
		})
	}
	for name, decl := range p.Manifest.MCPServers {
		items = append(items, ComponentDescriptor{
			Kind:        ComponentKindMCPServer,
			Name:        name,
			Description: decl.Description,
			Metadata: map[string]any{
				"command":  decl.Command,
				"args":     append([]string(nil), decl.Args...),
				"work_dir": decl.WorkDir,
			},
		})
	}
	for name, decl := range p.Manifest.Agents {
		items = append(items, ComponentDescriptor{
			Kind:        ComponentKindAgent,
			Name:        name,
			Description: decl.Description,
			Metadata: map[string]any{
				"model":      decl.Model,
				"tools":      append([]string(nil), decl.Tools...),
				"skills":     append([]string(nil), decl.Skills...),
				"max_tokens": decl.MaxTokens,
			},
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].Name < items[j].Name
	})
	return items
}

// ComponentCounts returns summary counts by component kind.
func (p LoadedPlugin) ComponentCounts() map[string]int {
	counts := map[string]int{}
	for _, item := range p.Components() {
		counts[string(item.Kind)]++
	}
	return counts
}

func (p LoadedPlugin) resolveLocalDir(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	return filepath.Join(p.Dir, filepath.Clean(raw))
}

func (p LoadedPlugin) resolveCommandPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || filepath.IsAbs(raw) {
		return raw
	}
	if strings.HasPrefix(raw, ".") || strings.Contains(raw, string(filepath.Separator)) {
		return filepath.Join(p.Dir, filepath.Clean(raw))
	}
	candidate := filepath.Join(p.Dir, raw)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return raw
}

func toMCPServerConfig(cfg ServerConfig) mcp.ServerConfig {
	out := mcp.ServerConfig{
		Name:    cfg.Name,
		Command: cfg.Command,
		WorkDir: cfg.WorkDir,
	}
	if len(cfg.Args) > 0 {
		out.Args = append([]string(nil), cfg.Args...)
	}
	if len(cfg.Env) > 0 {
		out.Env = make(map[string]string, len(cfg.Env))
		for key, value := range cfg.Env {
			out.Env[key] = value
		}
	}
	return out
}

func scopedPluginComponentName(pluginName, localName string) string {
	pluginName = strings.TrimSpace(pluginName)
	localName = strings.TrimSpace(localName)
	switch {
	case pluginName == "":
		return localName
	case localName == "":
		return pluginName
	default:
		return pluginName + "." + localName
	}
}

func (p LoadedPlugin) compatibilityConfigMetadata() map[string]any {
	if p.Manifest.Format != ManifestFormatOpenClawJSON &&
		strings.TrimSpace(p.Manifest.OpenClawID) == "" &&
		len(p.Manifest.ConfigSchema) == 0 &&
		len(p.Manifest.UIHints) == 0 &&
		len(p.Manifest.ProviderAuthEnvVars) == 0 &&
		len(p.Manifest.LegacyPluginIDs) == 0 &&
		len(p.Manifest.AutoEnableWhenConfiguredProviders) == 0 &&
		len(p.Manifest.OpenClawKinds) == 0 &&
		len(p.Manifest.OpenClawChannels) == 0 &&
		strings.TrimSpace(p.Manifest.OpenClawProviderDiscoveryEntry) == "" &&
		len(p.Manifest.OpenClawModelSupport.ModelPrefixes) == 0 &&
		len(p.Manifest.OpenClawModelSupport.ModelPatterns) == 0 &&
		len(p.Manifest.OpenClawCLIBackends) == 0 &&
		len(p.Manifest.OpenClawProviderAuthAliases) == 0 &&
		len(p.Manifest.OpenClawChannelEnvVars) == 0 &&
		len(p.Manifest.OpenClawProviderAuthChoices) == 0 &&
		len(p.Manifest.OpenClawContracts) == 0 &&
		len(p.Manifest.OpenClawConfigContracts) == 0 &&
		strings.TrimSpace(p.Manifest.OpenClawPackageName) == "" &&
		strings.TrimSpace(p.Manifest.OpenClawPackageVersion) == "" &&
		len(p.Manifest.OpenClawPackageMetadata) == 0 &&
		len(p.Manifest.OpenClawExtensions) == 0 &&
		strings.TrimSpace(p.Manifest.OpenClawSetupEntry) == "" &&
		len(p.Manifest.OpenClawChannelMetadata) == 0 &&
		len(p.Manifest.UnsupportedProviders) == 0 {
		return nil
	}

	metadata := map[string]any{
		"format": string(p.Manifest.Format),
	}
	if id := strings.TrimSpace(p.Manifest.OpenClawID); id != "" {
		metadata["id"] = id
	}
	if name := strings.TrimSpace(p.Manifest.OpenClawPackageName); name != "" {
		metadata["package_name"] = name
	}
	if version := strings.TrimSpace(p.Manifest.OpenClawPackageVersion); version != "" {
		metadata["package_version"] = version
	}
	if p.Manifest.EnabledByDefault != nil {
		metadata["enabled_by_default"] = *p.Manifest.EnabledByDefault
	}
	if ids := append([]string(nil), p.Manifest.LegacyPluginIDs...); len(ids) > 0 {
		sort.Strings(ids)
		metadata["legacy_plugin_ids"] = ids
	}
	if providers := append([]string(nil), p.Manifest.AutoEnableWhenConfiguredProviders...); len(providers) > 0 {
		sort.Strings(providers)
		metadata["auto_enable_when_configured_providers"] = providers
	}
	if kinds := append([]string(nil), p.Manifest.OpenClawKinds...); len(kinds) > 0 {
		sort.Strings(kinds)
		metadata["kinds"] = kinds
	}
	if channels := append([]string(nil), p.Manifest.OpenClawChannels...); len(channels) > 0 {
		sort.Strings(channels)
		metadata["channels"] = channels
	}
	if entry := strings.TrimSpace(p.Manifest.OpenClawProviderDiscoveryEntry); entry != "" {
		metadata["provider_discovery_entry"] = entry
	}
	if modelSupport := normalizeOpenClawModelSupport(p.Manifest.OpenClawModelSupport); len(modelSupport.ModelPrefixes) > 0 || len(modelSupport.ModelPatterns) > 0 {
		metadata["model_support"] = modelSupport
	}
	if backends := append([]string(nil), p.Manifest.OpenClawCLIBackends...); len(backends) > 0 {
		sort.Strings(backends)
		metadata["cli_backends"] = backends
	}
	if paths := configSchemaPaths(p.Manifest.ConfigSchema); len(paths) > 0 {
		metadata["config_paths"] = paths
	}
	if keys := sortedConfigHintKeys(p.Manifest.UIHints); len(keys) > 0 {
		metadata["ui_hint_paths"] = keys
	}
	if envVars := cloneProviderAuthEnvVars(p.Manifest.ProviderAuthEnvVars); len(envVars) > 0 {
		metadata["provider_auth_env_vars"] = envVars
	}
	if aliases := cloneStringMap(p.Manifest.OpenClawProviderAuthAliases); len(aliases) > 0 {
		metadata["provider_auth_aliases"] = aliases
	}
	if envVars := normalizeStringSliceMap(p.Manifest.OpenClawChannelEnvVars); len(envVars) > 0 {
		metadata["channel_env_vars"] = envVars
	}
	if choices := cloneOpenClawProviderAuthChoices(p.Manifest.OpenClawProviderAuthChoices); len(choices) > 0 {
		metadata["provider_auth_choices"] = choices
	}
	if contracts := normalizeStringSliceMap(p.Manifest.OpenClawContracts); len(contracts) > 0 {
		metadata["contracts"] = contracts
	}
	if contracts := cloneMapAny(p.Manifest.OpenClawConfigContracts); len(contracts) > 0 {
		metadata["config_contracts"] = contracts
	}
	if keys := sortedMetadataKeys(p.Manifest.OpenClawPackageMetadata); len(keys) > 0 {
		metadata["package_openclaw_keys"] = keys
	}
	if unsupported := append([]string(nil), p.Manifest.UnsupportedProviders...); len(unsupported) > 0 {
		sort.Strings(unsupported)
		metadata["unsupported_providers"] = unsupported
	}
	return metadata
}

// OpenClawRuntimeBridge reports the upstream native runtime surface that HopClaw
// discovered from package metadata but does not execute in-process.
func (p LoadedPlugin) OpenClawRuntimeBridge() *OpenClawRuntimeBridgeSpec {
	if !openClawRuntimeBridgeRequired(p.Manifest) {
		return nil
	}
	bridge := &OpenClawRuntimeBridgeSpec{
		PackageName:    strings.TrimSpace(p.Manifest.OpenClawPackageName),
		PackageVersion: strings.TrimSpace(p.Manifest.OpenClawPackageVersion),
		Status:         OpenClawRuntimeBridgeStatusDiscoveredNotLoaded,
		Reason:         "requires an external OpenClaw runtime bridge",
	}
	if entry := p.openClawRuntimeBridgeEntry(p.Manifest.OpenClawProviderDiscoveryEntry); entry != nil {
		bridge.ProviderDiscoveryEntry = entry
	}
	for _, raw := range p.Manifest.OpenClawExtensions {
		if entry := p.openClawRuntimeBridgeEntry(raw); entry != nil {
			bridge.RuntimeEntries = append(bridge.RuntimeEntries, *entry)
		}
	}
	if entry := p.openClawRuntimeBridgeEntry(p.Manifest.OpenClawSetupEntry); entry != nil {
		bridge.SetupEntry = entry
	}
	if channel := p.openClawChannelRuntimeBridge(); channel != nil {
		bridge.Channel = channel
	}
	return bridge
}

func cloneOpenClawProviderAuthChoices(src []OpenClawProviderAuthChoice) []OpenClawProviderAuthChoice {
	if len(src) == 0 {
		return nil
	}
	return normalizeOpenClawProviderAuthChoices(src)
}

func sortedMetadataKeys(values map[string]any) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	return keys
}

func openClawRuntimeBridgeRequired(manifest Manifest) bool {
	return len(manifest.OpenClawExtensions) > 0 ||
		strings.TrimSpace(manifest.OpenClawSetupEntry) != "" ||
		len(manifest.OpenClawChannelMetadata) > 0
}

func (p LoadedPlugin) openClawRuntimeBridgeEntry(raw string) *OpenClawRuntimeBridgeEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return &OpenClawRuntimeBridgeEntry{
		Specifier: raw,
		Path:      p.resolveOpenClawRuntimePath(raw),
	}
}

func (p LoadedPlugin) resolveOpenClawRuntimePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	return filepath.Join(p.Dir, filepath.Clean(raw))
}

func (p LoadedPlugin) openClawChannelRuntimeBridge() *OpenClawChannelRuntimeBridge {
	if len(p.Manifest.OpenClawChannelMetadata) == 0 {
		return nil
	}
	bridge := &OpenClawChannelRuntimeBridge{
		ID:    stringValue(p.Manifest.OpenClawChannelMetadata["id"]),
		Label: stringValue(p.Manifest.OpenClawChannelMetadata["label"]),
	}
	if state := p.openClawChannelRuntimeState("configuredState"); state != nil {
		bridge.ConfiguredState = state
	}
	if state := p.openClawChannelRuntimeState("persistedAuthState"); state != nil {
		bridge.PersistedAuthState = state
	}
	if bridge.ID == "" && bridge.Label == "" && bridge.ConfiguredState == nil && bridge.PersistedAuthState == nil {
		return nil
	}
	return bridge
}

func (p LoadedPlugin) openClawChannelRuntimeState(key string) *OpenClawChannelRuntimeState {
	state := mapValue(p.Manifest.OpenClawChannelMetadata[key])
	if len(state) == 0 {
		return nil
	}
	specifier := stringValue(state["specifier"])
	exportName := stringValue(state["exportName"])
	if specifier == "" && exportName == "" {
		return nil
	}
	return &OpenClawChannelRuntimeState{
		Specifier:  specifier,
		ExportName: exportName,
		Path:       p.resolveOpenClawRuntimePath(specifier),
	}
}

func openClawRuntimeBridgeMetadata(spec OpenClawRuntimeBridgeSpec) map[string]any {
	metadata := map[string]any{
		"status": string(spec.Status),
	}
	if reason := strings.TrimSpace(spec.Reason); reason != "" {
		metadata["reason"] = reason
	}
	if name := strings.TrimSpace(spec.PackageName); name != "" {
		metadata["package_name"] = name
	}
	if version := strings.TrimSpace(spec.PackageVersion); version != "" {
		metadata["package_version"] = version
	}
	if entry := openClawRuntimeBridgeEntryMetadata(spec.ProviderDiscoveryEntry); len(entry) > 0 {
		metadata["provider_discovery_entry"] = entry
	}
	if entries := openClawRuntimeBridgeEntriesMetadata(spec.RuntimeEntries); len(entries) > 0 {
		metadata["runtime_entries"] = entries
	}
	if entry := openClawRuntimeBridgeEntryMetadata(spec.SetupEntry); len(entry) > 0 {
		metadata["setup_entry"] = entry
	}
	if channel := openClawChannelRuntimeBridgeMetadata(spec.Channel); len(channel) > 0 {
		metadata["channel"] = channel
	}
	return metadata
}

func openClawRuntimeBridgeEntryMetadata(entry *OpenClawRuntimeBridgeEntry) map[string]any {
	if entry == nil {
		return nil
	}
	metadata := map[string]any{}
	if specifier := strings.TrimSpace(entry.Specifier); specifier != "" {
		metadata["specifier"] = specifier
	}
	if path := strings.TrimSpace(entry.Path); path != "" {
		metadata["path"] = path
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func openClawRuntimeBridgeEntriesMetadata(entries []OpenClawRuntimeBridgeEntry) []map[string]any {
	if len(entries) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if metadata := openClawRuntimeBridgeEntryMetadata(&entry); len(metadata) > 0 {
			out = append(out, metadata)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func openClawChannelRuntimeBridgeMetadata(channel *OpenClawChannelRuntimeBridge) map[string]any {
	if channel == nil {
		return nil
	}
	metadata := map[string]any{}
	if id := strings.TrimSpace(channel.ID); id != "" {
		metadata["id"] = id
	}
	if label := strings.TrimSpace(channel.Label); label != "" {
		metadata["label"] = label
	}
	if state := openClawChannelRuntimeStateMetadata(channel.ConfiguredState); len(state) > 0 {
		metadata["configured_state"] = state
	}
	if state := openClawChannelRuntimeStateMetadata(channel.PersistedAuthState); len(state) > 0 {
		metadata["persisted_auth_state"] = state
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func openClawChannelRuntimeStateMetadata(state *OpenClawChannelRuntimeState) map[string]any {
	if state == nil {
		return nil
	}
	metadata := map[string]any{}
	if specifier := strings.TrimSpace(state.Specifier); specifier != "" {
		metadata["specifier"] = specifier
	}
	if exportName := strings.TrimSpace(state.ExportName); exportName != "" {
		metadata["export_name"] = exportName
	}
	if path := strings.TrimSpace(state.Path); path != "" {
		metadata["path"] = path
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func configSchemaPaths(schema map[string]any) []string {
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) == 0 {
		return nil
	}
	out := make([]string, 0)
	collectConfigSchemaPaths("", properties, &out)
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func collectConfigSchemaPaths(prefix string, properties map[string]any, out *[]string) {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		*out = append(*out, path)
		child, _ := properties[key].(map[string]any)
		if child == nil {
			continue
		}
		nested, _ := child["properties"].(map[string]any)
		if len(nested) == 0 {
			continue
		}
		collectConfigSchemaPaths(path, nested, out)
	}
}

func sortedConfigHintKeys(hints map[string]ConfigUIHint) []string {
	if len(hints) == 0 {
		return nil
	}
	keys := make([]string, 0, len(hints))
	for key := range hints {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	return keys
}

func cloneProviderAuthEnvVars(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]string, len(src))
	for key, values := range src {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if len(values) == 0 {
			continue
		}
		out[key] = append([]string(nil), values...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
