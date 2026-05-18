package plugin

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/mcp"
)

var log = logging.WithSubsystem("plugin")

// Manager discovers, loads, and indexes plugins.
// After loading, callers read the aggregated providers, tools,
// channels, and skill dirs to wire them into the runtime.
type Manager struct {
	mu      sync.RWMutex
	plugins map[string]LoadedPlugin // name → plugin
}

// NewManager creates an empty manager.
func NewManager() *Manager {
	return &Manager{plugins: make(map[string]LoadedPlugin)}
}

// LoadAll discovers plugins from dirs and loads their manifests.
func (m *Manager) LoadAll(dirs []string) error {
	found := Discover(dirs)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range found {
		name := strings.TrimSpace(p.Manifest.Name)
		if _, exists := m.plugins[name]; exists {
			log.Warn("plugin name conflict, skipping duplicate", "name", name, "dir", p.Dir)
			continue
		}
		m.plugins[name] = p
	}
	log.Info("plugins loaded", "count", len(m.plugins))
	return nil
}

// Register adds a plugin from an already-parsed manifest.
func (m *Manager) Register(p LoadedPlugin) error {
	name := strings.TrimSpace(p.Manifest.Name)
	if name == "" {
		return fmt.Errorf("plugin name is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.plugins[name]; exists {
		return fmt.Errorf("plugin %q already registered", name)
	}
	m.plugins[name] = p
	return nil
}

// Providers returns all provider declarations across all loaded plugins.
// Keys are prefixed with the plugin name to avoid collisions:
// "plugin-name/provider-name".
func (m *Manager) Providers() map[string]ProviderDecl {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]ProviderDecl)
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	seen := make(map[string]struct{})
	for _, pluginName := range names {
		p := m.plugins[pluginName]
		providerNames := sortedProviderNames(p.Manifest.Providers)
		for _, provName := range providerNames {
			key := effectiveProviderKey(p, pluginName, provName, seen)
			out[key] = p.Manifest.Providers[provName]
		}
	}
	return out
}

// Tools returns all tool declarations across all loaded plugins.
func (m *Manager) Tools() []ToolDecl {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []ToolDecl
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, m.plugins[name].Manifest.Tools...)
	}
	return out
}

// Commands returns all plugin-declared CLI commands keyed by "plugin/name".
func (m *Manager) Commands() map[string]CommandDecl {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]CommandDecl)
	for pluginName, p := range m.plugins {
		for _, decl := range p.Manifest.Commands {
			out[pluginName+"/"+strings.TrimSpace(decl.Name)] = decl
		}
	}
	return out
}

// Channels returns all channel declarations across all loaded plugins.
func (m *Manager) Channels() map[string]ChannelDecl {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]ChannelDecl)
	for pluginName, p := range m.plugins {
		for chName, ch := range p.Manifest.Channels {
			key := pluginName + "/" + chName
			out[key] = ch
		}
	}
	return out
}

// SkillDirs returns absolute paths to skill directories declared by plugins.
func (m *Manager) SkillDirs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var dirs []string
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, dir := range m.plugins[name].ResolvedSkillsDirs() {
			if strings.TrimSpace(dir) == "" {
				continue
			}
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// HookDirs returns absolute paths to hook directories declared by plugins.
func (m *Manager) HookDirs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var dirs []string
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if dir := m.plugins[name].ResolvedHooksDir(); strings.TrimSpace(dir) != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// MCPServers returns all plugin-declared MCP servers keyed by "plugin/name".
func (m *Manager) MCPServers() map[string]MCPServerDecl {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]MCPServerDecl)
	for pluginName, p := range m.plugins {
		for name, decl := range p.Manifest.MCPServers {
			out[pluginName+"/"+name] = decl
		}
	}
	return out
}

// MCPServerConfigs returns normalized MCP server configs contributed by plugins.
func (m *Manager) MCPServerConfigs() []mcp.ServerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []mcp.ServerConfig
	for _, name := range names {
		out = append(out, m.plugins[name].ResolvedMCPServerConfigs()...)
	}
	return out
}

// Agents returns all plugin-declared agent presets keyed by "plugin/name".
func (m *Manager) Agents() map[string]AgentDecl {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]AgentDecl)
	for pluginName, p := range m.plugins {
		for name, decl := range p.Manifest.Agents {
			out[pluginName+"/"+name] = decl
		}
	}
	return out
}

// Names returns the list of loaded plugin names.
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Get returns a loaded plugin by name.
func (m *Manager) Get(name string) (LoadedPlugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.plugins[name]
	return p, ok
}

// GetByChannel returns the plugin that owns the given channel key.
// Channel keys are formatted as "pluginName/channelName".
func (m *Manager) GetByChannel(channelKey string) (LoadedPlugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	parts := strings.SplitN(channelKey, "/", 2)
	if len(parts) < 2 {
		return LoadedPlugin{}, false
	}
	p, ok := m.plugins[parts[0]]
	return p, ok
}

// Remove deletes a loaded plugin from the in-memory registry.
func (m *Manager) Remove(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.plugins, strings.TrimSpace(name))
}

func effectiveProviderKey(p LoadedPlugin, pluginName, providerName string, seen map[string]struct{}) string {
	pluginName = strings.TrimSpace(pluginName)
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return pluginName
	}
	if p.Manifest.Format == ManifestFormatOpenClawJSON && p.Manifest.Providers[providerName].PreferUnscopedID {
		if _, ok := seen[providerName]; !ok {
			seen[providerName] = struct{}{}
			return providerName
		}
	}
	key := pluginName
	if key == "" {
		key = providerName
	} else {
		key += "/" + providerName
	}
	seen[key] = struct{}{}
	return key
}

func sortedProviderNames(items map[string]ProviderDecl) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	sort.Strings(keys)
	return keys
}
