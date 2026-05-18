package plugin

import (
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/internal/modules"
)

func TestLoadedPluginModuleManifestAndContributions(t *testing.T) {
	pluginDir := t.TempDir()
	loaded := LoadedPlugin{
		Dir: pluginDir,
		Manifest: Manifest{
			Name:        "demo-pack",
			Version:     "1.2.3",
			Description: "Demo capability pack",
			Providers: map[string]ProviderDecl{
				"openai": {API: "openai", BaseURL: "https://api.example.com/v1"},
			},
			Channels: map[string]ChannelDecl{
				"echo": {Type: "stdio", Command: "./echo.py"},
			},
			Tools: []ToolDecl{
				{Name: "weather.lookup", Description: "Lookup weather", Endpoint: "https://tools.example.com/invoke"},
			},
			SkillsDir: "skills",
			HooksDir:  "hooks",
			Agents: map[string]AgentDecl{
				"assistant": {Description: "Preset assistant", Model: "gpt-4.1-mini"},
			},
		},
	}

	manifest := loaded.ModuleManifest()
	if manifest.ID != "plugin:demo-pack" {
		t.Fatalf("manifest.ID = %q, want %q", manifest.ID, "plugin:demo-pack")
	}
	if manifest.Source != modules.SourcePlugin {
		t.Fatalf("manifest.Source = %q, want %q", manifest.Source, modules.SourcePlugin)
	}
	if manifest.Level != modules.ModuleLevelDeclared {
		t.Fatalf("manifest.Level = %q, want %q", manifest.Level, modules.ModuleLevelDeclared)
	}

	contrib := loaded.ModuleContributions()
	if len(contrib.Providers) != 1 {
		t.Fatalf("len(contrib.Providers) = %d, want 1", len(contrib.Providers))
	}
	if len(contrib.Channels) != 1 {
		t.Fatalf("len(contrib.Channels) = %d, want 1", len(contrib.Channels))
	}
	if len(contrib.Tools) != 1 {
		t.Fatalf("len(contrib.Tools) = %d, want 1", len(contrib.Tools))
	}
	if len(contrib.SkillDirs) != 1 || contrib.SkillDirs[0].Path != filepath.Join(pluginDir, "skills") {
		t.Fatalf("skill dir = %#v, want %q", contrib.SkillDirs, filepath.Join(pluginDir, "skills"))
	}
	if len(contrib.HookDirs) != 1 || contrib.HookDirs[0].Path != filepath.Join(pluginDir, "hooks") {
		t.Fatalf("hook dir = %#v, want %q", contrib.HookDirs, filepath.Join(pluginDir, "hooks"))
	}
	if len(contrib.Agents) != 1 {
		t.Fatalf("len(contrib.Agents) = %d, want 1", len(contrib.Agents))
	}
}

func TestLoadedPluginModuleManifestUsesMinimalLevelForSingleSurfacePlugin(t *testing.T) {
	loaded := LoadedPlugin{
		Dir: t.TempDir(),
		Manifest: Manifest{
			Name:        "hello-tool",
			Version:     "1.0.0",
			Description: "Level 0 tool plugin",
			Tools: []ToolDecl{{
				Name:        "hello.say",
				Description: "Return a greeting",
				Endpoint:    "inline://hello.say",
			}},
		},
	}

	manifest := loaded.ModuleManifest()
	if manifest.Level != modules.ModuleLevelMinimal {
		t.Fatalf("manifest.Level = %q, want %q", manifest.Level, modules.ModuleLevelMinimal)
	}
}

func TestManagerModulesReturnsSortedModules(t *testing.T) {
	manager := NewManager()
	if err := manager.Register(LoadedPlugin{Dir: t.TempDir(), Manifest: Manifest{Name: "zeta"}}); err != nil {
		t.Fatalf("Register(zeta): %v", err)
	}
	if err := manager.Register(LoadedPlugin{Dir: t.TempDir(), Manifest: Manifest{Name: "alpha"}}); err != nil {
		t.Fatalf("Register(alpha): %v", err)
	}

	modulesList := manager.Modules()
	if len(modulesList) != 2 {
		t.Fatalf("len(modulesList) = %d, want 2", len(modulesList))
	}
	if modulesList[0].Manifest().Name != "alpha" || modulesList[1].Manifest().Name != "zeta" {
		t.Fatalf("module order = %q, %q; want alpha, zeta", modulesList[0].Manifest().Name, modulesList[1].Manifest().Name)
	}
}

func TestLoadedPluginModuleContributionsExposeOpenClawCompatibility(t *testing.T) {
	pluginDir := t.TempDir()
	loaded := LoadedPlugin{
		Dir: pluginDir,
		Manifest: Manifest{
			Name:                   "compat-pack",
			Format:                 ManifestFormatOpenClawJSON,
			ConfigSchema:           map[string]any{"type": "object", "properties": map[string]any{}},
			OpenClawID:             "compat-pack",
			OpenClawPackageName:    "@openclaw/compat-pack",
			OpenClawPackageVersion: "2026.4.10",
			OpenClawExtensions:     []string{"./index.ts"},
			OpenClawSetupEntry:     "./setup-entry.ts",
		},
	}

	contrib := loaded.ModuleContributions()
	if len(contrib.ConfigContracts) != 1 {
		t.Fatalf("len(contrib.ConfigContracts) = %d, want 1", len(contrib.ConfigContracts))
	}
	if contrib.ConfigContracts[0].Kind != modules.ComponentKindConfig {
		t.Fatalf("config contract kind = %q", contrib.ConfigContracts[0].Kind)
	}
	if len(contrib.RuntimeBridges) != 1 {
		t.Fatalf("len(contrib.RuntimeBridges) = %d, want 1", len(contrib.RuntimeBridges))
	}
	if contrib.RuntimeBridges[0].Kind != modules.ComponentKindRuntimeBridge {
		t.Fatalf("runtime bridge kind = %q", contrib.RuntimeBridges[0].Kind)
	}
	if contrib.RuntimeBridges[0].Metadata["status"] != string(OpenClawRuntimeBridgeStatusDiscoveredNotLoaded) {
		t.Fatalf("runtime bridge status = %#v", contrib.RuntimeBridges[0].Metadata["status"])
	}
	if manifest := loaded.ModuleManifest(); manifest.Level != modules.ModuleLevelDeclared {
		t.Fatalf("manifest.Level = %q, want %q", manifest.Level, modules.ModuleLevelDeclared)
	}
}
