// Package plugin provides the public contract for HopClaw plugin development.
//
// A plugin is a directory containing a manifest file (hopclaw.plugin.yaml or
// openclaw.plugin.json) that declares extensions it provides. See the Manifest
// type for the full declaration surface.
//
// Plugins can contribute:
//   - Model providers (ProviderDecl)
//   - Channel adapters (ChannelDecl)
//   - External tools (ToolDecl)
//   - Skill directories (SKILL.md collections)
//   - Hook assets
//   - MCP servers (MCPServerDecl)
//   - Agent presets (AgentDecl)
//   - CLI command declarations (CommandDecl)
package plugin

import "time"

type ManifestFormat string

const (
	ManifestFormatHopClawYAML  ManifestFormat = "hopclaw.plugin.yaml"
	ManifestFormatOpenClawJSON ManifestFormat = "openclaw.plugin.json"
)

// Manifest describes a plugin's capabilities and the extensions it provides.
// Plugins are discovered from YAML manifest files (hopclaw.plugin.yaml).
type Manifest struct {
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description" json:"description"`
	Author      string `yaml:"author,omitempty" json:"author,omitempty"`

	// ConfigSchema preserves OpenClaw-compatible plugin configuration metadata.
	ConfigSchema map[string]any `yaml:"config_schema,omitempty" json:"config_schema,omitempty"`

	// UIHints preserves OpenClaw-compatible field labels/help text.
	UIHints map[string]ConfigUIHint `yaml:"ui_hints,omitempty" json:"ui_hints,omitempty"`

	// ProviderAuthEnvVars preserves OpenClaw provider auth environment hints.
	ProviderAuthEnvVars map[string][]string `yaml:"provider_auth_env_vars,omitempty" json:"provider_auth_env_vars,omitempty"`

	// Providers declares model providers this plugin contributes.
	Providers map[string]ProviderDecl `yaml:"providers,omitempty" json:"providers,omitempty"`

	// Channels declares channel adapters this plugin contributes.
	Channels map[string]ChannelDecl `yaml:"channels,omitempty" json:"channels,omitempty"`

	// Tools declares external HTTP tools this plugin contributes.
	Tools []ToolDecl `yaml:"tools,omitempty" json:"tools,omitempty"`

	// SkillsDir is a relative path (to the manifest) containing SKILL.md files.
	SkillsDir string `yaml:"skills_dir,omitempty" json:"skills_dir,omitempty"`

	// SkillsDirs optionally declares multiple relative skill directories.
	// Native HopClaw manifests typically use SkillsDir; OpenClaw-compatible
	// manifests may expand to one or more skill roots.
	SkillsDirs []string `yaml:"skills_dirs,omitempty" json:"skills_dirs,omitempty"`

	// HooksDir is a relative path (to the manifest) containing hook assets or manifests.
	HooksDir string `yaml:"hooks_dir,omitempty" json:"hooks_dir,omitempty"`

	// MCPServers declares MCP servers that ship with this plugin.
	MCPServers map[string]MCPServerDecl `yaml:"mcp_servers,omitempty" json:"mcp_servers,omitempty"`

	// Agents declares reusable agent presets contributed by this plugin.
	Agents map[string]AgentDecl `yaml:"agents,omitempty" json:"agents,omitempty"`

	// Commands declares plugin-owned CLI command metadata for future runtime wiring.
	Commands []CommandDecl `yaml:"commands,omitempty" json:"commands,omitempty"`

	// Format tracks the source manifest syntax that produced this manifest.
	Format ManifestFormat `yaml:"-" json:"-"`

	// UnsupportedProviders tracks declared OpenClaw providers that HopClaw can
	// discover but cannot translate into a native provider runtime yet.
	UnsupportedProviders []string `yaml:"-" json:"-"`

	// OpenClawID preserves the source plugin id when a compatibility manifest
	// also declares a separate human-readable name.
	OpenClawID string `yaml:"-" json:"-"`

	// EnabledByDefault preserves OpenClaw manifest default-enable semantics.
	EnabledByDefault *bool `yaml:"-" json:"-"`

	// LegacyPluginIDs preserves OpenClaw plugin id aliases.
	LegacyPluginIDs []string `yaml:"-" json:"-"`

	// AutoEnableWhenConfiguredProviders preserves OpenClaw auto-enable hints.
	AutoEnableWhenConfiguredProviders []string `yaml:"-" json:"-"`

	// OpenClawKinds preserves compatibility plugin kind metadata.
	OpenClawKinds []string `yaml:"-" json:"-"`

	// OpenClawChannels preserves manifest-declared compatibility channel ids.
	OpenClawChannels []string `yaml:"-" json:"-"`

	// OpenClawProviderDiscoveryEntry preserves the lightweight discovery module path.
	OpenClawProviderDiscoveryEntry string `yaml:"-" json:"-"`

	// OpenClawModelSupport preserves model-family ownership hints.
	OpenClawModelSupport OpenClawModelSupport `yaml:"-" json:"-"`

	// OpenClawCLIBackends preserves plugin-owned CLI backend ids.
	OpenClawCLIBackends []string `yaml:"-" json:"-"`

	// OpenClawProviderAuthAliases preserves provider auth alias mappings.
	OpenClawProviderAuthAliases map[string]string `yaml:"-" json:"-"`

	// OpenClawChannelEnvVars preserves cheap channel auth/env hints.
	OpenClawChannelEnvVars map[string][]string `yaml:"-" json:"-"`

	// OpenClawProviderAuthChoices preserves onboarding/auth-choice metadata.
	OpenClawProviderAuthChoices []OpenClawProviderAuthChoice `yaml:"-" json:"-"`

	// OpenClawContracts preserves capability contract ownership declarations.
	OpenClawContracts map[string][]string `yaml:"-" json:"-"`

	// OpenClawConfigContracts preserves manifest-owned config behaviors.
	OpenClawConfigContracts map[string]any `yaml:"-" json:"-"`

	// OpenClawPackageName preserves the upstream package name when present.
	OpenClawPackageName string `yaml:"-" json:"-"`

	// OpenClawPackageVersion preserves the upstream package version when present.
	OpenClawPackageVersion string `yaml:"-" json:"-"`

	// OpenClawPackageMetadata preserves the raw package.json openclaw block.
	OpenClawPackageMetadata map[string]any `yaml:"-" json:"-"`

	// OpenClawExtensions preserves package.json openclaw.extensions entries.
	OpenClawExtensions []string `yaml:"-" json:"-"`

	// OpenClawSetupEntry preserves package.json openclaw.setupEntry.
	OpenClawSetupEntry string `yaml:"-" json:"-"`

	// OpenClawChannelMetadata preserves package.json openclaw.channel metadata.
	OpenClawChannelMetadata map[string]any `yaml:"-" json:"-"`
}

// ProviderDecl declares a model provider contributed by a plugin.
type ProviderDecl struct {
	API          string            `yaml:"api" json:"api"`
	BaseURL      string            `yaml:"base_url" json:"base_url"`
	Region       string            `yaml:"region,omitempty" json:"region,omitempty"`
	APIKey       string            `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	AccessKeyID  string            `yaml:"access_key_id,omitempty" json:"access_key_id,omitempty"`
	SecretKey    string            `yaml:"secret_key,omitempty" json:"secret_key,omitempty"`
	SessionToken string            `yaml:"session_token,omitempty" json:"session_token,omitempty"`
	DefaultModel string            `yaml:"default_model,omitempty" json:"default_model,omitempty"`
	Timeout      time.Duration     `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Headers      map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	EnvVars      []string          `yaml:"env_vars,omitempty" json:"env_vars,omitempty"`
	APIKeyHint   string            `yaml:"api_key_hint,omitempty" json:"api_key_hint,omitempty"`

	// PreferUnscopedID keeps OpenClaw-compatible provider IDs user-facing when
	// there is no collision, so a translated provider like "openai" does not
	// become "openai/openai" in HopClaw UX by default.
	PreferUnscopedID bool `yaml:"-" json:"-"`
}

// ConfigUIHint preserves OpenClaw manifest field presentation metadata.
type ConfigUIHint struct {
	Label       string `yaml:"label,omitempty" json:"label,omitempty"`
	Help        string `yaml:"help,omitempty" json:"help,omitempty"`
	Advanced    bool   `yaml:"advanced,omitempty" json:"advanced,omitempty"`
	Sensitive   bool   `yaml:"sensitive,omitempty" json:"sensitive,omitempty"`
	Placeholder string `yaml:"placeholder,omitempty" json:"placeholder,omitempty"`
}

// OpenClawModelSupport preserves compatibility model-family ownership hints.
type OpenClawModelSupport struct {
	ModelPrefixes []string `yaml:"model_prefixes,omitempty" json:"modelPrefixes,omitempty"`
	ModelPatterns []string `yaml:"model_patterns,omitempty" json:"modelPatterns,omitempty"`
}

// OpenClawProviderAuthChoice preserves compatibility onboarding/auth-choice metadata.
type OpenClawProviderAuthChoice struct {
	Provider            string   `yaml:"provider,omitempty" json:"provider,omitempty"`
	Method              string   `yaml:"method,omitempty" json:"method,omitempty"`
	ChoiceID            string   `yaml:"choice_id,omitempty" json:"choiceId,omitempty"`
	ChoiceLabel         string   `yaml:"choice_label,omitempty" json:"choiceLabel,omitempty"`
	ChoiceHint          string   `yaml:"choice_hint,omitempty" json:"choiceHint,omitempty"`
	AssistantPriority   *int     `yaml:"assistant_priority,omitempty" json:"assistantPriority,omitempty"`
	AssistantVisibility string   `yaml:"assistant_visibility,omitempty" json:"assistantVisibility,omitempty"`
	DeprecatedChoiceIDs []string `yaml:"deprecated_choice_ids,omitempty" json:"deprecatedChoiceIds,omitempty"`
	GroupID             string   `yaml:"group_id,omitempty" json:"groupId,omitempty"`
	GroupLabel          string   `yaml:"group_label,omitempty" json:"groupLabel,omitempty"`
	GroupHint           string   `yaml:"group_hint,omitempty" json:"groupHint,omitempty"`
	OptionKey           string   `yaml:"option_key,omitempty" json:"optionKey,omitempty"`
	CLIFlag             string   `yaml:"cli_flag,omitempty" json:"cliFlag,omitempty"`
	CLIOption           string   `yaml:"cli_option,omitempty" json:"cliOption,omitempty"`
	CLIDescription      string   `yaml:"cli_description,omitempty" json:"cliDescription,omitempty"`
	OnboardingScopes    []string `yaml:"onboarding_scopes,omitempty" json:"onboardingScopes,omitempty"`
}

// OpenClawRuntimeBridgeStatus describes the state of an upstream native runtime.
type OpenClawRuntimeBridgeStatus string

const (
	OpenClawRuntimeBridgeStatusDiscoveredNotLoaded OpenClawRuntimeBridgeStatus = "discovered_not_loaded"
)

// OpenClawRuntimeBridgeEntry describes one upstream module specifier plus its
// resolved on-disk path inside the plugin root.
type OpenClawRuntimeBridgeEntry struct {
	Specifier string `yaml:"specifier,omitempty" json:"specifier,omitempty"`
	Path      string `yaml:"path,omitempty" json:"path,omitempty"`
}

// OpenClawChannelRuntimeState describes one channel-owned state probe module.
type OpenClawChannelRuntimeState struct {
	Specifier  string `yaml:"specifier,omitempty" json:"specifier,omitempty"`
	ExportName string `yaml:"export_name,omitempty" json:"export_name,omitempty"`
	Path       string `yaml:"path,omitempty" json:"path,omitempty"`
}

// OpenClawChannelRuntimeBridge describes the upstream channel runtime surface.
type OpenClawChannelRuntimeBridge struct {
	ID                 string                       `yaml:"id,omitempty" json:"id,omitempty"`
	Label              string                       `yaml:"label,omitempty" json:"label,omitempty"`
	ConfiguredState    *OpenClawChannelRuntimeState `yaml:"configured_state,omitempty" json:"configuredState,omitempty"`
	PersistedAuthState *OpenClawChannelRuntimeState `yaml:"persisted_auth_state,omitempty" json:"persistedAuthState,omitempty"`
}

// OpenClawRuntimeBridgeSpec describes the external runtime surface that HopClaw
// discovered from an OpenClaw-compatible package but does not natively load.
type OpenClawRuntimeBridgeSpec struct {
	PackageName            string                        `yaml:"package_name,omitempty" json:"package_name,omitempty"`
	PackageVersion         string                        `yaml:"package_version,omitempty" json:"package_version,omitempty"`
	ProviderDiscoveryEntry *OpenClawRuntimeBridgeEntry   `yaml:"provider_discovery_entry,omitempty" json:"provider_discovery_entry,omitempty"`
	RuntimeEntries         []OpenClawRuntimeBridgeEntry  `yaml:"runtime_entries,omitempty" json:"runtime_entries,omitempty"`
	SetupEntry             *OpenClawRuntimeBridgeEntry   `yaml:"setup_entry,omitempty" json:"setup_entry,omitempty"`
	Channel                *OpenClawChannelRuntimeBridge `yaml:"channel,omitempty" json:"channel,omitempty"`
	Status                 OpenClawRuntimeBridgeStatus   `yaml:"status,omitempty" json:"status,omitempty"`
	Reason                 string                        `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// ChannelDecl declares a channel adapter contributed by a plugin.
// Supported types: "webhook" (HTTP callback) and "stdio" (JSON-RPC subprocess).
type ChannelDecl struct {
	Type        string `yaml:"type" json:"type"` // "webhook" or "stdio"
	CallbackURL string `yaml:"callback_url,omitempty" json:"callback_url,omitempty"`
	Secret      string `yaml:"secret,omitempty" json:"secret,omitempty"`

	// Stdio-specific fields (type: "stdio").
	Command      string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args         []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env          map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	WorkDir      string            `yaml:"work_dir,omitempty" json:"work_dir,omitempty"`
	Capabilities []string          `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Config       map[string]any    `yaml:"config,omitempty" json:"config,omitempty"`
	MaxRestarts  int               `yaml:"max_restarts,omitempty" json:"max_restarts,omitempty"`
}

// ToolDecl declares an external HTTP tool contributed by a plugin.
type ToolDecl struct {
	Name        string         `yaml:"name" json:"name"`
	Description string         `yaml:"description" json:"description"`
	Endpoint    string         `yaml:"endpoint" json:"endpoint"`
	Timeout     time.Duration  `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	InputSchema map[string]any `yaml:"input_schema,omitempty" json:"input_schema,omitempty"`
}

// ServerConfig describes how to spawn and connect to an external MCP server.
type ServerConfig struct {
	Name    string            `json:"name" yaml:"name"`
	Command string            `json:"command" yaml:"command"`
	Args    []string          `json:"args,omitempty" yaml:"args"`
	Env     map[string]string `json:"env,omitempty" yaml:"env"`
	WorkDir string            `json:"work_dir,omitempty" yaml:"work_dir"`
}

// MCPServerDecl declares an MCP server packaged with the plugin.
type MCPServerDecl struct {
	Description  string `yaml:"description,omitempty" json:"description,omitempty"`
	ServerConfig `yaml:",inline" json:",inline"`
}

// AgentDecl declares an inspectable agent preset packaged with the plugin.
type AgentDecl struct {
	Description  string   `yaml:"description,omitempty" json:"description,omitempty"`
	SystemPrompt string   `yaml:"system_prompt,omitempty" json:"system_prompt,omitempty"`
	Model        string   `yaml:"model,omitempty" json:"model,omitempty"`
	Tools        []string `yaml:"tools,omitempty" json:"tools,omitempty"`
	Skills       []string `yaml:"skills,omitempty" json:"skills,omitempty"`
	MaxTokens    int      `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
}

// CommandDecl declares a plugin-managed CLI command contract.
type CommandDecl struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Exec        string `yaml:"exec" json:"exec"`
}

// ComponentKind identifies one plugin-contributed extension type.
type ComponentKind string

const (
	ComponentKindProvider      ComponentKind = "provider"
	ComponentKindChannel       ComponentKind = "channel"
	ComponentKindTool          ComponentKind = "tool"
	ComponentKindCommand       ComponentKind = "command"
	ComponentKindConfig        ComponentKind = "config_contract"
	ComponentKindRuntimeBridge ComponentKind = "runtime_bridge"
	ComponentKindSkillsDir     ComponentKind = "skills_dir"
	ComponentKindHooksDir      ComponentKind = "hooks_dir"
	ComponentKindMCPServer     ComponentKind = "mcp_server"
	ComponentKindAgent         ComponentKind = "agent"
)
