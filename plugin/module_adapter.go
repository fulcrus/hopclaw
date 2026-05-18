package plugin

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/internal/modules"
)

func (p LoadedPlugin) ModuleManifest() modules.Manifest {
	name := strings.TrimSpace(p.Manifest.Name)
	defaultEnabled := true
	if p.Manifest.EnabledByDefault != nil {
		defaultEnabled = *p.Manifest.EnabledByDefault
	}
	metadata := map[string]any{
		"author":          strings.TrimSpace(p.Manifest.Author),
		"dir":             strings.TrimSpace(p.Dir),
		"manifest_format": strings.TrimSpace(string(p.Manifest.Format)),
	}
	return modules.Manifest{
		ID:             "plugin:" + name,
		Name:           name,
		Version:        strings.TrimSpace(p.Manifest.Version),
		Description:    strings.TrimSpace(p.Manifest.Description),
		Kind:           "capability_pack",
		Source:         modules.SourcePlugin,
		Delivery:       modules.DeliveryManifest,
		Level:          p.moduleLevel(),
		Metadata:       metadata,
		DefaultEnabled: defaultEnabled,
	}
}

func (p LoadedPlugin) moduleLevel() modules.ModuleLevel {
	if p.isMinimalModule() {
		return modules.ModuleLevelMinimal
	}
	return modules.ModuleLevelDeclared
}

func (p LoadedPlugin) isMinimalModule() bool {
	if p.hasDeclaredAuthoringFeatures() {
		return false
	}
	return p.primaryContributionFamilyCount() == 1
}

func (p LoadedPlugin) hasDeclaredAuthoringFeatures() bool {
	if p.Manifest.EnabledByDefault != nil {
		return true
	}
	if len(p.Manifest.MCPServers) > 0 || len(p.Manifest.Agents) > 0 || len(p.Manifest.Commands) > 0 {
		return true
	}
	return len(p.compatibilityConfigMetadata()) > 0
}

func (p LoadedPlugin) primaryContributionFamilyCount() int {
	count := 0
	if len(p.Manifest.Providers) > 0 {
		count++
	}
	if len(p.Manifest.Channels) > 0 {
		count++
	}
	if len(p.Manifest.Tools) > 0 {
		count++
	}
	if len(p.ResolvedSkillsDirs()) > 0 {
		count++
	}
	if dir := p.ResolvedHooksDir(); dir != "" {
		count++
	}
	return count
}

func (p LoadedPlugin) ModuleContributions() modules.Contributions {
	out := modules.Contributions{}

	providerNames := make([]string, 0, len(p.Manifest.Providers))
	for name := range p.Manifest.Providers {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)
	for _, name := range providerNames {
		decl := p.Manifest.Providers[name]
		metadata := map[string]any{
			"api":                decl.API,
			"base_url":           decl.BaseURL,
			"region":             decl.Region,
			"default_model":      decl.DefaultModel,
			"timeout":            decl.Timeout.String(),
			"api_key_hint":       decl.APIKeyHint,
			"prefer_unscoped_id": decl.PreferUnscopedID,
			"has_credentials": strings.TrimSpace(decl.APIKey) != "" ||
				(strings.TrimSpace(decl.AccessKeyID) != "" && strings.TrimSpace(decl.SecretKey) != ""),
		}
		runtimeMetadata := map[string]any{
			"api_key":       decl.APIKey,
			"access_key_id": decl.AccessKeyID,
			"secret_key":    decl.SecretKey,
			"session_token": decl.SessionToken,
		}
		if len(decl.Headers) > 0 {
			runtimeMetadata["headers"] = cloneStringMap(decl.Headers)
		}
		if len(decl.EnvVars) > 0 {
			metadata["env_vars"] = append([]string(nil), decl.EnvVars...)
		}
		out.Providers = append(out.Providers, modules.Component{
			Kind:            modules.ComponentKindProvider,
			Name:            strings.TrimSpace(name),
			Description:     "Model provider",
			Metadata:        metadata,
			RuntimeMetadata: runtimeMetadata,
		})
	}

	channelNames := make([]string, 0, len(p.Manifest.Channels))
	for name := range p.Manifest.Channels {
		channelNames = append(channelNames, name)
	}
	sort.Strings(channelNames)
	for _, name := range channelNames {
		decl := p.Manifest.Channels[name]
		metadata := map[string]any{
			"type":         decl.Type,
			"callback_url": decl.CallbackURL,
			"command":      decl.Command,
			"args":         append([]string(nil), decl.Args...),
			"work_dir":     decl.WorkDir,
			"capabilities": append([]string(nil), decl.Capabilities...),
			"max_restarts": decl.MaxRestarts,
			"module_dir":   p.Dir,
		}
		runtimeMetadata := map[string]any{
			"secret": decl.Secret,
		}
		if len(decl.Env) > 0 {
			env := make(map[string]string, len(decl.Env))
			for key, value := range decl.Env {
				env[key] = value
			}
			runtimeMetadata["env"] = env
		}
		if len(decl.Config) > 0 {
			cfg := make(map[string]any, len(decl.Config))
			for key, value := range decl.Config {
				cfg[key] = value
			}
			runtimeMetadata["config"] = cfg
		}
		out.Channels = append(out.Channels, modules.Component{
			Kind:            modules.ComponentKindChannel,
			Name:            strings.TrimSpace(name),
			Description:     "Channel adapter",
			Metadata:        metadata,
			RuntimeMetadata: runtimeMetadata,
		})
	}

	for _, decl := range p.Manifest.Tools {
		metadata := map[string]any{
			"endpoint": decl.Endpoint,
			"timeout":  decl.Timeout.String(),
		}
		if len(decl.InputSchema) > 0 {
			schema := make(map[string]any, len(decl.InputSchema))
			for key, value := range decl.InputSchema {
				schema[key] = value
			}
			metadata["input_schema"] = schema
		}
		out.Tools = append(out.Tools, modules.Component{
			Kind:        modules.ComponentKindTool,
			Name:        strings.TrimSpace(decl.Name),
			Description: strings.TrimSpace(decl.Description),
			Metadata:    metadata,
		})
	}

	if metadata := p.compatibilityConfigMetadata(); len(metadata) > 0 {
		out.ConfigContracts = append(out.ConfigContracts, modules.Component{
			Kind:        modules.ComponentKindConfig,
			Name:        "compat",
			Description: "OpenClaw compatibility config contract",
			Metadata:    metadata,
		})
	}

	if bridge := p.OpenClawRuntimeBridge(); bridge != nil {
		out.RuntimeBridges = append(out.RuntimeBridges, modules.Component{
			Kind:        modules.ComponentKindRuntimeBridge,
			Name:        "openclaw-native-runtime",
			Description: "OpenClaw native runtime bridge",
			Metadata:    openClawRuntimeBridgeMetadata(*bridge),
		})
	}

	for _, dir := range p.ResolvedSkillsDirs() {
		out.SkillDirs = append(out.SkillDirs, modules.Component{
			Kind:        modules.ComponentKindSkillsDir,
			Name:        filepath.Base(dir),
			Description: "Skill package directory",
			Path:        dir,
		})
	}

	if dir := p.ResolvedHooksDir(); dir != "" {
		out.HookDirs = append(out.HookDirs, modules.Component{
			Kind:        modules.ComponentKindHooksDir,
			Name:        filepath.Base(dir),
			Description: "Hook package directory",
			Path:        dir,
		})
	}

	mcpNames := make([]string, 0, len(p.Manifest.MCPServers))
	for name := range p.Manifest.MCPServers {
		mcpNames = append(mcpNames, name)
	}
	sort.Strings(mcpNames)
	for _, name := range mcpNames {
		decl := p.Manifest.MCPServers[name]
		out.MCPServers = append(out.MCPServers, modules.Component{
			Kind:        modules.ComponentKindMCPServer,
			Name:        strings.TrimSpace(name),
			Description: strings.TrimSpace(decl.Description),
			Metadata: map[string]any{
				"command":    decl.Command,
				"args":       append([]string(nil), decl.Args...),
				"work_dir":   decl.WorkDir,
				"module_dir": p.Dir,
			},
			RuntimeMetadata: map[string]any{
				"env": cloneStringMap(decl.Env),
			},
		})
	}

	agentNames := make([]string, 0, len(p.Manifest.Agents))
	for name := range p.Manifest.Agents {
		agentNames = append(agentNames, name)
	}
	sort.Strings(agentNames)
	for _, name := range agentNames {
		decl := p.Manifest.Agents[name]
		out.Agents = append(out.Agents, modules.Component{
			Kind:        modules.ComponentKindAgent,
			Name:        strings.TrimSpace(name),
			Description: strings.TrimSpace(decl.Description),
			Metadata: map[string]any{
				"system_prompt": decl.SystemPrompt,
				"model":         decl.Model,
				"tools":         append([]string(nil), decl.Tools...),
				"skills":        append([]string(nil), decl.Skills...),
				"max_tokens":    decl.MaxTokens,
			},
		})
	}

	return out.Normalized()
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (p LoadedPlugin) Module() modules.StaticModule {
	return modules.StaticModule{
		ManifestValue:      p.ModuleManifest(),
		ContributionsValue: p.ModuleContributions(),
		HealthValue: modules.HealthReport{
			Status: modules.HealthUnknown,
		},
	}
}

func (m *Manager) Modules() []modules.StaticModule {
	if m == nil {
		return nil
	}
	names := m.Names()
	out := make([]modules.StaticModule, 0, len(names))
	for _, name := range names {
		loaded, ok := m.Get(name)
		if !ok {
			continue
		}
		out = append(out, loaded.Module())
	}
	return out
}
