package extensions

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	"github.com/fulcrus/hopclaw/channels"
	channelhealth "github.com/fulcrus/hopclaw/channels/health"
	modtypes "github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/toolspec"
)

type ToolInventory interface {
	ToolDefinitions(session *agent.Session) []agent.ToolDefinition
}

type CapabilityInventory interface {
	Reports(ctx context.Context) []captypes.Report
	ListCapabilitySessions(capName string) []*captypes.SessionHandle
}

type ChannelInventory interface {
	Names() []string
	Get(name string) (channels.Adapter, bool)
}

type ChannelHealthReader interface {
	Status() []channelhealth.ChannelHealth
}

type ModuleInventory interface {
	Manifests() []modtypes.Manifest
}

type StaticModuleInventory interface {
	Modules() []modtypes.StaticModule
}

type VersionedModuleInventory interface {
	Version() string
}

type Options struct {
	Tools         ToolInventory
	Capabilities  CapabilityInventory
	Channels      ChannelInventory
	ChannelHealth ChannelHealthReader
	Modules       ModuleInventory
}

type Registry struct {
	mu            sync.RWMutex
	tools         ToolInventory
	capabilities  CapabilityInventory
	channels      ChannelInventory
	channelHealth ChannelHealthReader
	modules       ModuleInventory
}

type Snapshot struct {
	GeneratedAt       time.Time         `json:"generated_at"`
	ProjectionVersion string            `json:"projection_version,omitempty"`
	Counts            SnapshotCounts    `json:"counts"`
	Capabilities      []CapabilityEntry `json:"capabilities,omitempty"`
	Channels          []ChannelEntry    `json:"channels,omitempty"`
	Tools             []ToolEntry       `json:"tools,omitempty"`
	Modules           []ModuleEntry     `json:"modules,omitempty"`
}

type SnapshotCounts struct {
	CapabilityCount int `json:"capability_count"`
	ChannelCount    int `json:"channel_count"`
	ToolCount       int `json:"tool_count"`
	ModuleCount     int `json:"module_count"`
}

type CapabilityEntry struct {
	Name         string            `json:"name"`
	Family       string            `json:"family,omitempty"`
	Source       string            `json:"source,omitempty"`
	Manifest     captypes.Manifest `json:"manifest"`
	Health       captypes.Health   `json:"health"`
	SessionCount int               `json:"session_count,omitempty"`
}

type ChannelEntry struct {
	Name             string                       `json:"name"`
	Family           string                       `json:"family,omitempty"`
	Source           string                       `json:"source,omitempty"`
	Status           string                       `json:"status"`
	Capabilities     channels.Capabilities        `json:"capabilities"`
	CapabilityMatrix channels.CapabilityMatrix    `json:"capability_matrix"`
	SupportsAction   bool                         `json:"supports_action,omitempty"`
	Health           *channelhealth.ChannelHealth `json:"health,omitempty"`
}

type ToolEntry struct {
	Name       string               `json:"name"`
	Family     string               `json:"family,omitempty"`
	Descriptor agent.ToolDefinition `json:"descriptor"`
}

type ModuleEntry struct {
	ID             string                    `json:"id"`
	Name           string                    `json:"name"`
	Version        string                    `json:"version,omitempty"`
	Description    string                    `json:"description,omitempty"`
	Kind           string                    `json:"kind,omitempty"`
	Source         modtypes.Source           `json:"source,omitempty"`
	Delivery       modtypes.Delivery         `json:"delivery,omitempty"`
	Level          modtypes.ModuleLevel      `json:"level,omitempty"`
	Health         modtypes.HealthReport     `json:"health"`
	Contributions  ModuleContributionSummary `json:"contributions"`
	DefaultEnabled bool                      `json:"default_enabled,omitempty"`
	Dependencies   []string                  `json:"dependencies,omitempty"`
}

type ModuleContributionSummary struct {
	TotalCount          int      `json:"total_count,omitempty"`
	ProviderCount       int      `json:"provider_count,omitempty"`
	ProviderNames       []string `json:"provider_names,omitempty"`
	ChannelCount        int      `json:"channel_count,omitempty"`
	ChannelNames        []string `json:"channel_names,omitempty"`
	ToolCount           int      `json:"tool_count,omitempty"`
	ToolNames           []string `json:"tool_names,omitempty"`
	ConfigContractCount int      `json:"config_contract_count,omitempty"`
	ConfigContractNames []string `json:"config_contract_names,omitempty"`
	RuntimeBridgeCount  int      `json:"runtime_bridge_count,omitempty"`
	RuntimeBridgeNames  []string `json:"runtime_bridge_names,omitempty"`
	SkillDirCount       int      `json:"skill_dir_count,omitempty"`
	HookDirCount        int      `json:"hook_dir_count,omitempty"`
	MCPServerCount      int      `json:"mcp_server_count,omitempty"`
	MCPServerNames      []string `json:"mcp_server_names,omitempty"`
	AgentCount          int      `json:"agent_count,omitempty"`
	AgentNames          []string `json:"agent_names,omitempty"`
}

func New(opts Options) *Registry {
	return &Registry{
		tools:         opts.Tools,
		capabilities:  opts.Capabilities,
		channels:      opts.Channels,
		channelHealth: opts.ChannelHealth,
		modules:       opts.Modules,
	}
}

func (r *Registry) SetTools(v ToolInventory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = v
}

func (r *Registry) SetCapabilities(v CapabilityInventory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capabilities = v
}

func (r *Registry) SetChannels(v ChannelInventory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channels = v
}

func (r *Registry) SetChannelHealth(v ChannelHealthReader) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channelHealth = v
}

func (r *Registry) SetModules(v ModuleInventory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modules = v
}

func (r *Registry) Snapshot(ctx context.Context, session *agent.Session) Snapshot {
	capabilities := r.Capabilities(ctx)
	channels := r.Channels()
	tools := r.Tools(session)
	modules := r.Modules()
	projectionVersion := ""
	if r != nil {
		r.mu.RLock()
		modulesInventory := r.modules
		r.mu.RUnlock()
		projectionVersion = moduleProjectionVersion(modulesInventory)
	}
	return Snapshot{
		GeneratedAt:       time.Now().UTC(),
		ProjectionVersion: projectionVersion,
		Counts: SnapshotCounts{
			CapabilityCount: len(capabilities),
			ChannelCount:    len(channels),
			ToolCount:       len(tools),
			ModuleCount:     len(modules),
		},
		Capabilities: capabilities,
		Channels:     channels,
		Tools:        tools,
		Modules:      modules,
	}
}

func (r *Registry) Capabilities(ctx context.Context) []CapabilityEntry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	capabilities := r.capabilities
	r.mu.RUnlock()
	if isNilReference(capabilities) {
		return nil
	}
	reports := capabilities.Reports(ctx)
	items := make([]CapabilityEntry, 0, len(reports))
	for _, report := range reports {
		name := strings.TrimSpace(report.Manifest.Name)
		items = append(items, CapabilityEntry{
			Name:         name,
			Family:       extensionFamily(name),
			Source:       extensionSource(name, "builtin"),
			Manifest:     report.Manifest,
			Health:       report.Health,
			SessionCount: len(capabilities.ListCapabilitySessions(name)),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}

func (r *Registry) Channels() []ChannelEntry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	channelsInventory := r.channels
	channelHealth := r.channelHealth
	r.mu.RUnlock()
	if isNilReference(channelsInventory) {
		return nil
	}

	healthByName := make(map[string]channelhealth.ChannelHealth)
	if !isNilReference(channelHealth) {
		for _, item := range channelHealth.Status() {
			healthByName[strings.TrimSpace(item.Name)] = item
		}
	}

	names := append([]string(nil), channelsInventory.Names()...)
	sort.Strings(names)
	items := make([]ChannelEntry, 0, len(names))
	for _, name := range names {
		adapter, ok := channelsInventory.Get(name)
		if !ok {
			continue
		}
		entry := ChannelEntry{
			Name:             name,
			Family:           extensionFamily(name),
			Source:           extensionSource(name, "builtin"),
			Status:           string(adapter.Status()),
			Capabilities:     adapter.Capabilities(),
			CapabilityMatrix: channels.MatrixForAdapter(adapter),
			SupportsAction:   supportsChannelAction(adapter),
		}
		if health, ok := healthByName[name]; ok {
			clone := health
			entry.Health = &clone
		}
		items = append(items, entry)
	}
	return items
}

func supportsChannelAction(adapter channels.Adapter) bool {
	_, ok := adapter.(channels.ActionExecutor)
	return ok
}

func (r *Registry) Channel(name string) (ChannelEntry, bool) {
	if r == nil {
		return ChannelEntry{}, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ChannelEntry{}, false
	}
	for _, entry := range r.Channels() {
		if entry.Name == name {
			return entry, true
		}
	}
	return ChannelEntry{}, false
}

func (r *Registry) Tools(session *agent.Session) []ToolEntry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	toolsInventory := r.tools
	r.mu.RUnlock()
	if isNilReference(toolsInventory) {
		return nil
	}
	definitions := toolsInventory.ToolDefinitions(session)
	items := make([]ToolEntry, 0, len(definitions))
	for _, definition := range definitions {
		normalized := toolspec.NormalizeDefinition(definition)
		if normalized.Name == "" {
			continue
		}
		items = append(items, ToolEntry{
			Name:       normalized.Name,
			Family:     extensionFamily(normalized.Name),
			Descriptor: normalized,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}

func (r *Registry) Modules() []ModuleEntry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	modulesInventory := r.modules
	r.mu.RUnlock()
	if isNilReference(modulesInventory) {
		return nil
	}

	modulesList := inventoryModules(modulesInventory)
	items := make([]ModuleEntry, 0, len(modulesList))
	for _, module := range modulesList {
		manifest := module.Manifest()
		if manifest.ID == "" {
			continue
		}
		items = append(items, ModuleEntry{
			ID:             manifest.ID,
			Name:           manifest.Name,
			Version:        manifest.Version,
			Description:    manifest.Description,
			Kind:           manifest.Kind,
			Source:         manifest.Source,
			Delivery:       manifest.Delivery,
			Level:          manifest.Level,
			Health:         module.Health(context.Background()),
			Contributions:  summarizeModuleContributions(module.Contributions()),
			DefaultEnabled: manifest.DefaultEnabled,
			Dependencies:   append([]string(nil), manifest.Dependencies...),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	return items
}

func moduleProjectionVersion(inventory ModuleInventory) string {
	if inventory == nil {
		return ""
	}
	if versioned, ok := inventory.(VersionedModuleInventory); ok && !isNilReference(versioned) {
		return strings.TrimSpace(versioned.Version())
	}
	return ""
}

func inventoryModules(inventory ModuleInventory) []modtypes.StaticModule {
	if inventory == nil {
		return nil
	}
	if modulesInventory, ok := inventory.(StaticModuleInventory); ok && !isNilReference(modulesInventory) {
		if items := cloneStaticModules(modulesInventory.Modules()); len(items) > 0 {
			return items
		}
	}
	manifests := inventory.Manifests()
	if len(manifests) == 0 {
		return nil
	}
	items := make([]modtypes.StaticModule, 0, len(manifests))
	for _, manifest := range manifests {
		items = append(items, modtypes.StaticModule{
			ManifestValue: manifest,
			HealthValue: modtypes.HealthReport{
				Status: modtypes.HealthUnknown,
			},
		})
	}
	return items
}

func cloneStaticModules(items []modtypes.StaticModule) []modtypes.StaticModule {
	if len(items) == 0 {
		return nil
	}
	out := make([]modtypes.StaticModule, 0, len(items))
	for _, item := range items {
		out = append(out, modtypes.StaticModule{
			ManifestValue:      item.Manifest(),
			ContributionsValue: item.Contributions(),
			HealthValue:        item.Health(context.Background()),
		})
	}
	return out
}

func summarizeModuleContributions(contributions modtypes.Contributions) ModuleContributionSummary {
	normalized := contributions.Normalized()
	return ModuleContributionSummary{
		TotalCount:          normalized.Count(),
		ProviderCount:       len(normalized.Providers),
		ProviderNames:       componentSummaryNames(normalized.Providers),
		ChannelCount:        len(normalized.Channels),
		ChannelNames:        componentSummaryNames(normalized.Channels),
		ToolCount:           len(normalized.Tools),
		ToolNames:           componentSummaryNames(normalized.Tools),
		ConfigContractCount: len(normalized.ConfigContracts),
		ConfigContractNames: componentSummaryNames(normalized.ConfigContracts),
		RuntimeBridgeCount:  len(normalized.RuntimeBridges),
		RuntimeBridgeNames:  componentSummaryNames(normalized.RuntimeBridges),
		SkillDirCount:       len(normalized.SkillDirs),
		HookDirCount:        len(normalized.HookDirs),
		MCPServerCount:      len(normalized.MCPServers),
		MCPServerNames:      componentSummaryNames(normalized.MCPServers),
		AgentCount:          len(normalized.Agents),
		AgentNames:          componentSummaryNames(normalized.Agents),
	}
}

func componentSummaryNames(items []modtypes.Component) []string {
	if len(items) == 0 {
		return nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = strings.TrimSpace(item.Path)
		}
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func (r *Registry) ChannelHealth() []channelhealth.ChannelHealth {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	channelHealth := r.channelHealth
	r.mu.RUnlock()
	if isNilReference(channelHealth) {
		return nil
	}
	items := append([]channelhealth.ChannelHealth(nil), channelHealth.Status()...)
	sort.Slice(items, func(i, j int) bool {
		return strings.TrimSpace(items[i].Name) < strings.TrimSpace(items[j].Name)
	})
	return items
}

func extensionFamily(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if head, _, ok := strings.Cut(name, ":"); ok && strings.TrimSpace(head) != "" {
		return strings.TrimSpace(head)
	}
	if head, _, ok := strings.Cut(name, "."); ok && strings.TrimSpace(head) != "" {
		return strings.TrimSpace(head)
	}
	return name
}

func extensionSource(name, fallback string) string {
	name = strings.TrimSpace(name)
	switch {
	case strings.HasPrefix(name, "plugin:"):
		return "plugin"
	case strings.HasPrefix(name, "webhook:"):
		return "webhook"
	case strings.TrimSpace(fallback) != "":
		return strings.TrimSpace(fallback)
	default:
		return "builtin"
	}
}

func isNilReference(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return rv.IsNil()
	default:
		return false
	}
}
