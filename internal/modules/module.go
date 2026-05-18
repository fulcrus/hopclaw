package modules

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
)

type Source string

const (
	SourceBuiltin  Source = "builtin"
	SourcePlugin   Source = "plugin"
	SourceExternal Source = "external"
)

type Delivery string

const (
	DeliveryEmbedded Delivery = "embedded"
	DeliveryBundled  Delivery = "bundled"
	DeliveryManifest Delivery = "manifest"
	DeliveryProcess  Delivery = "process"
	DeliveryWebhook  Delivery = "webhook"
	DeliveryMCP      Delivery = "mcp"
	DeliveryHost     Delivery = "host"
)

type ModuleLevel string

const (
	ModuleLevelMinimal  ModuleLevel = "minimal"
	ModuleLevelDeclared ModuleLevel = "declared"
	ModuleLevelManaged  ModuleLevel = "managed"
)

type ReloadAction string

const (
	ReloadActionNoop    ReloadAction = "noop"
	ReloadActionHot     ReloadAction = "hot"
	ReloadActionRestart ReloadAction = "restart"
)

type HealthStatus string

const (
	HealthUnknown  HealthStatus = "unknown"
	HealthReady    HealthStatus = "ready"
	HealthDegraded HealthStatus = "degraded"
	HealthFailed   HealthStatus = "failed"
)

type ComponentKind string

const (
	ComponentKindProvider      ComponentKind = "provider"
	ComponentKindChannel       ComponentKind = "channel"
	ComponentKindTool          ComponentKind = "tool"
	ComponentKindConfig        ComponentKind = "config_contract"
	ComponentKindRuntimeBridge ComponentKind = "runtime_bridge"
	ComponentKindSkillsDir     ComponentKind = "skills_dir"
	ComponentKindHooksDir      ComponentKind = "hooks_dir"
	ComponentKindMCPServer     ComponentKind = "mcp_server"
	ComponentKindAgent         ComponentKind = "agent"
)

type Manifest struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Version        string         `json:"version,omitempty"`
	Description    string         `json:"description,omitempty"`
	Kind           string         `json:"kind,omitempty"`
	Source         Source         `json:"source,omitempty"`
	Delivery       Delivery       `json:"delivery,omitempty"`
	Level          ModuleLevel    `json:"level,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	DefaultEnabled bool           `json:"default_enabled,omitempty"`
	Dependencies   []string       `json:"dependencies,omitempty"`
}

type ReloadPlan struct {
	Action  ReloadAction `json:"action"`
	Reasons []string     `json:"reasons,omitempty"`
}

type HealthReport struct {
	Status  HealthStatus   `json:"status"`
	Summary string         `json:"summary,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

type Component struct {
	Kind        ComponentKind  `json:"kind"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Path        string         `json:"path,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	// RuntimeMetadata carries internal-only execution data that must not leak
	// through inventory snapshots or JSON contracts.
	RuntimeMetadata map[string]any `json:"-"`
}

type Contributions struct {
	Providers       []Component `json:"providers,omitempty"`
	Channels        []Component `json:"channels,omitempty"`
	Tools           []Component `json:"tools,omitempty"`
	ConfigContracts []Component `json:"config_contracts,omitempty"`
	RuntimeBridges  []Component `json:"runtime_bridges,omitempty"`
	SkillDirs       []Component `json:"skill_dirs,omitempty"`
	HookDirs        []Component `json:"hook_dirs,omitempty"`
	MCPServers      []Component `json:"mcp_servers,omitempty"`
	Agents          []Component `json:"agents,omitempty"`
}

type Host interface{}

type Module interface {
	ID() string
	Manifest() Manifest
	Contributions() Contributions
	Validate() error
	PlanReload() ReloadPlan
	Start(context.Context, Host) error
	Stop(context.Context) error
	Health(context.Context) HealthReport
}

type StaticModule struct {
	ManifestValue      Manifest
	ContributionsValue Contributions
	HealthValue        HealthReport
}

func (m StaticModule) ID() string {
	return normalizeManifest(m.ManifestValue).ID
}

func (m StaticModule) Manifest() Manifest {
	return normalizeManifest(m.ManifestValue)
}

func (m StaticModule) Contributions() Contributions {
	return m.ContributionsValue.Normalized()
}

func (m StaticModule) Validate() error {
	return nil
}

func (m StaticModule) PlanReload() ReloadPlan {
	if m.Contributions().Count() == 0 {
		return ReloadPlan{Action: ReloadActionNoop}
	}
	return ReloadPlan{Action: ReloadActionHot}
}

func (m StaticModule) Start(context.Context, Host) error {
	return nil
}

func (m StaticModule) Stop(context.Context) error {
	return nil
}

func (m StaticModule) Health(context.Context) HealthReport {
	report := m.HealthValue
	if report.Status == "" {
		report.Status = HealthUnknown
	}
	if report.Details == nil {
		report.Details = nil
	}
	return report
}

func (c Contributions) Count() int {
	return len(c.Components())
}

func (c Contributions) Components() []Component {
	items := make([]Component, 0,
		len(c.Providers)+len(c.Channels)+len(c.Tools)+len(c.ConfigContracts)+len(c.RuntimeBridges)+len(c.SkillDirs)+len(c.HookDirs)+len(c.MCPServers)+len(c.Agents),
	)
	items = append(items, cloneComponents(c.Providers)...)
	items = append(items, cloneComponents(c.Channels)...)
	items = append(items, cloneComponents(c.Tools)...)
	items = append(items, cloneComponents(c.ConfigContracts)...)
	items = append(items, cloneComponents(c.RuntimeBridges)...)
	items = append(items, cloneComponents(c.SkillDirs)...)
	items = append(items, cloneComponents(c.HookDirs)...)
	items = append(items, cloneComponents(c.MCPServers)...)
	items = append(items, cloneComponents(c.Agents)...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Path < items[j].Path
	})
	return items
}

func (c Contributions) Normalized() Contributions {
	return Contributions{
		Providers:       normalizeComponents(cloneComponents(c.Providers), ComponentKindProvider),
		Channels:        normalizeComponents(cloneComponents(c.Channels), ComponentKindChannel),
		Tools:           normalizeComponents(cloneComponents(c.Tools), ComponentKindTool),
		ConfigContracts: normalizeComponents(cloneComponents(c.ConfigContracts), ComponentKindConfig),
		RuntimeBridges:  normalizeComponents(cloneComponents(c.RuntimeBridges), ComponentKindRuntimeBridge),
		SkillDirs:       normalizeOrderedComponents(cloneComponents(c.SkillDirs), ComponentKindSkillsDir),
		HookDirs:        normalizeOrderedComponents(cloneComponents(c.HookDirs), ComponentKindHooksDir),
		MCPServers:      normalizeComponents(cloneComponents(c.MCPServers), ComponentKindMCPServer),
		Agents:          normalizeComponents(cloneComponents(c.Agents), ComponentKindAgent),
	}
}

func cloneComponents(items []Component) []Component {
	if len(items) == 0 {
		return nil
	}
	out := make([]Component, 0, len(items))
	for _, item := range items {
		item.Kind = ComponentKind(strings.TrimSpace(string(item.Kind)))
		item.Name = strings.TrimSpace(item.Name)
		item.Description = strings.TrimSpace(item.Description)
		item.Path = normalizeComponentPath(item.Path)
		item.Metadata = cloneMetadata(item.Metadata)
		item.RuntimeMetadata = cloneMetadata(item.RuntimeMetadata)
		if item.Name == "" && item.Path == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func normalizeComponents(items []Component, kind ComponentKind) []Component {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]Component, 0, len(items))
	for _, item := range items {
		if item.Kind == "" {
			item.Kind = kind
		}
		key := strings.TrimSpace(string(item.Kind)) + "\x00" + item.Name + "\x00" + item.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func normalizeOrderedComponents(items []Component, kind ComponentKind) []Component {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]Component, 0, len(items))
	for _, item := range items {
		if item.Kind == "" {
			item.Kind = kind
		}
		key := strings.TrimSpace(string(item.Kind)) + "\x00" + item.Name + "\x00" + item.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizeManifest(in Manifest) Manifest {
	in.ID = strings.TrimSpace(in.ID)
	in.Name = strings.TrimSpace(in.Name)
	in.Version = strings.TrimSpace(in.Version)
	in.Description = strings.TrimSpace(in.Description)
	in.Kind = strings.TrimSpace(in.Kind)
	in.Level = normalizeModuleLevel(in.Level)
	in.Metadata = cloneMetadata(in.Metadata)
	if in.ID == "" {
		in.ID = in.Name
	}
	if in.Kind == "" {
		in.Kind = "capability_pack"
	}
	in.Dependencies = normalizeStrings(in.Dependencies)
	return in
}

func normalizeModuleLevel(level ModuleLevel) ModuleLevel {
	switch strings.ToLower(strings.TrimSpace(string(level))) {
	case "", string(ModuleLevelMinimal):
		return ModuleLevelMinimal
	case string(ModuleLevelDeclared):
		return ModuleLevelDeclared
	case string(ModuleLevelManaged):
		return ModuleLevelManaged
	default:
		return ModuleLevelMinimal
	}
}

func normalizeStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func normalizeComponentPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func cloneMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
