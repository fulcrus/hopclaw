package plugin

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadManifestRejectsEscapingSkillsDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data := `name: bad-plugin
skills_dir: ../outside
`
	if err := os.WriteFile(filepath.Join(dir, manifestFile), []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := loadManifest(dir); err == nil {
		t.Fatal("expected escaping skills_dir validation error")
	}
}

func TestLoadedPluginComponents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plugin := LoadedPlugin{
		Dir: dir,
		Manifest: Manifest{
			Name:        "demo",
			Description: "demo plugin",
			Providers: map[string]ProviderDecl{
				"openai": {API: "openai", BaseURL: "https://api.example.com"},
			},
			Channels: map[string]ChannelDecl{
				"slack": {Type: "webhook"},
			},
			Tools: []ToolDecl{
				{Name: "notify.send", Description: "Send a notification", Endpoint: "https://tools.example.com/notify"},
			},
			Commands: []CommandDecl{
				{Name: "inspect", Description: "Inspect plugin", Exec: "./bin/inspect"},
			},
			SkillsDir: "skills",
			HooksDir:  "hooks",
			MCPServers: map[string]MCPServerDecl{
				"browser": {Description: "Browser bridge"},
			},
			Agents: map[string]AgentDecl{
				"ops": {Description: "Ops preset", Model: "gpt-5"},
			},
		},
	}

	components := plugin.Components()
	if len(components) != 8 {
		t.Fatalf("components len = %d, want 8", len(components))
	}
	counts := plugin.ComponentCounts()
	for _, key := range []string{"provider", "channel", "tool", "command", "skills_dir", "hooks_dir", "mcp_server", "agent"} {
		if counts[key] != 1 {
			t.Fatalf("component count %s = %d, want 1", key, counts[key])
		}
	}
	for _, component := range components {
		if component.Kind != ComponentKindCommand {
			continue
		}
		if component.Path != filepath.Join(dir, "bin", "inspect") {
			t.Fatalf("command path = %q, want %q", component.Path, filepath.Join(dir, "bin", "inspect"))
		}
		if component.Metadata["exec"] != "./bin/inspect" {
			t.Fatalf("command exec metadata = %#v", component.Metadata["exec"])
		}
		return
	}
	t.Fatal("expected command component")
}

func TestLoadedPluginOpenClawRuntimeBridge(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plugin := LoadedPlugin{
		Dir: dir,
		Manifest: Manifest{
			Name:                           "compat-demo",
			Format:                         ManifestFormatOpenClawJSON,
			OpenClawPackageName:            "@openclaw/compat-demo",
			OpenClawPackageVersion:         "2026.4.10",
			OpenClawProviderDiscoveryEntry: "./provider-discovery.ts",
			OpenClawExtensions:             []string{"./index.ts"},
			OpenClawSetupEntry:             "./setup-entry.ts",
			OpenClawChannelMetadata: map[string]any{
				"id":    "slack",
				"label": "Slack",
				"configuredState": map[string]any{
					"specifier":  "./configured-state",
					"exportName": "hasSlackConfiguredState",
				},
				"persistedAuthState": map[string]any{
					"specifier":  "./auth-presence",
					"exportName": "hasAnySlackAuth",
				},
			},
		},
	}

	bridge := plugin.OpenClawRuntimeBridge()
	if bridge == nil {
		t.Fatal("OpenClawRuntimeBridge() = nil, want bridge spec")
	}
	if bridge.Status != OpenClawRuntimeBridgeStatusDiscoveredNotLoaded {
		t.Fatalf("bridge status = %q, want %q", bridge.Status, OpenClawRuntimeBridgeStatusDiscoveredNotLoaded)
	}
	if bridge.ProviderDiscoveryEntry == nil || bridge.ProviderDiscoveryEntry.Path != filepath.Join(dir, "provider-discovery.ts") {
		t.Fatalf("bridge provider discovery entry = %#v", bridge.ProviderDiscoveryEntry)
	}
	if len(bridge.RuntimeEntries) != 1 || bridge.RuntimeEntries[0].Path != filepath.Join(dir, "index.ts") {
		t.Fatalf("bridge runtime entries = %#v", bridge.RuntimeEntries)
	}
	if bridge.SetupEntry == nil || bridge.SetupEntry.Path != filepath.Join(dir, "setup-entry.ts") {
		t.Fatalf("bridge setup entry = %#v", bridge.SetupEntry)
	}
	if bridge.Channel == nil || bridge.Channel.ConfiguredState == nil || bridge.Channel.PersistedAuthState == nil {
		t.Fatalf("bridge channel = %#v", bridge.Channel)
	}

	components := plugin.Components()
	for _, component := range components {
		if component.Kind != ComponentKindRuntimeBridge {
			continue
		}
		if component.Name != "openclaw-native-runtime" {
			t.Fatalf("runtime bridge component name = %q", component.Name)
		}
		if component.Metadata["status"] != string(OpenClawRuntimeBridgeStatusDiscoveredNotLoaded) {
			t.Fatalf("runtime bridge status = %#v", component.Metadata["status"])
		}
		if component.Metadata["package_name"] != "@openclaw/compat-demo" {
			t.Fatalf("runtime bridge package_name = %#v", component.Metadata["package_name"])
		}
		entries, ok := component.Metadata["runtime_entries"].([]map[string]any)
		if !ok || len(entries) != 1 || entries[0]["path"] != filepath.Join(dir, "index.ts") {
			t.Fatalf("runtime bridge runtime_entries = %#v", component.Metadata["runtime_entries"])
		}
		channel, ok := component.Metadata["channel"].(map[string]any)
		if !ok || channel["id"] != "slack" {
			t.Fatalf("runtime bridge channel = %#v", component.Metadata["channel"])
		}
		if !reflect.DeepEqual(channel["configured_state"], map[string]any{
			"specifier":   "./configured-state",
			"export_name": "hasSlackConfiguredState",
			"path":        filepath.Join(dir, "configured-state"),
		}) {
			t.Fatalf("runtime bridge configured_state = %#v", channel["configured_state"])
		}
		return
	}
	t.Fatal("expected runtime bridge component")
}
