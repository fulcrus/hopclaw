package plugin

import sdk "github.com/fulcrus/hopclaw/sdk/plugin"

type ManifestFormat = sdk.ManifestFormat

const (
	ManifestFormatHopClawYAML  = sdk.ManifestFormatHopClawYAML
	ManifestFormatOpenClawJSON = sdk.ManifestFormatOpenClawJSON
)

type Manifest = sdk.Manifest
type ProviderDecl = sdk.ProviderDecl
type ConfigUIHint = sdk.ConfigUIHint
type OpenClawModelSupport = sdk.OpenClawModelSupport
type OpenClawProviderAuthChoice = sdk.OpenClawProviderAuthChoice
type OpenClawRuntimeBridgeStatus = sdk.OpenClawRuntimeBridgeStatus
type OpenClawRuntimeBridgeEntry = sdk.OpenClawRuntimeBridgeEntry
type OpenClawChannelRuntimeState = sdk.OpenClawChannelRuntimeState
type OpenClawChannelRuntimeBridge = sdk.OpenClawChannelRuntimeBridge
type OpenClawRuntimeBridgeSpec = sdk.OpenClawRuntimeBridgeSpec
type ChannelDecl = sdk.ChannelDecl
type ToolDecl = sdk.ToolDecl
type ServerConfig = sdk.ServerConfig
type MCPServerDecl = sdk.MCPServerDecl
type AgentDecl = sdk.AgentDecl
type CommandDecl = sdk.CommandDecl

const (
	OpenClawRuntimeBridgeStatusDiscoveredNotLoaded = sdk.OpenClawRuntimeBridgeStatusDiscoveredNotLoaded
)

// LoadedPlugin is a manifest plus its resolved filesystem path.
type LoadedPlugin struct {
	Manifest Manifest
	Dir      string // absolute path to the plugin directory
}
