package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
)

func TestBootstrapModuleCatalogIncludesBuiltinAndPluginModules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pluginRoot := filepath.Join(root, "plugins")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginRoot): %v", err)
	}
	writeHopClawTestPlugin(t, filepath.Join(pluginRoot, "demo-pack"), "demo-pack", "echo.reply")

	app, err := New(context.Background(), testModuleCatalogConfig(root, pluginRoot, true), Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	snapshot := app.ModuleCatalogSnapshot()
	if _, ok := snapshot.Find("builtin:core"); !ok {
		t.Fatal("expected builtin:core in module catalog")
	}
	if _, ok := snapshot.Find(builtinBindingPackIntegration); !ok {
		t.Fatal("expected builtin:integration-pack in module catalog")
	}
	if _, ok := snapshot.Find(firstPartyPackOperatorSupport); !ok {
		t.Fatal("expected builtin:operator-support-pack in module catalog")
	}
	integrationModule, _ := snapshot.Find(builtinBindingPackIntegration)
	if integrationModule.Health(context.Background()).Status != modules.HealthReady {
		t.Fatalf("integration pack health = %#v", integrationModule.Health(context.Background()))
	}
	supportModule, _ := snapshot.Find(firstPartyPackOperatorSupport)
	if supportModule.Health(context.Background()).Status != modules.HealthReady {
		t.Fatalf("support pack health = %#v", supportModule.Health(context.Background()))
	}
	pluginModule, ok := snapshot.Find("plugin:demo-pack")
	if !ok {
		t.Fatal("expected plugin:demo-pack in module catalog")
	}
	if pluginModule.Manifest().Name != "demo-pack" {
		t.Fatalf("plugin module name = %q, want %q", pluginModule.Manifest().Name, "demo-pack")
	}
	if pluginModule.Manifest().Level != modules.ModuleLevelMinimal {
		t.Fatalf("plugin module level = %q, want %q", pluginModule.Manifest().Level, modules.ModuleLevelMinimal)
	}

	contrib := snapshot.Contributions()
	if !hasModuleTool(contrib.Tools, "fs.list") {
		t.Fatal("expected builtin tool fs.list in aggregated module contributions")
	}
	if !hasModuleTool(contrib.Tools, "echo.reply") {
		t.Fatal("expected plugin tool echo.reply in aggregated module contributions")
	}
}

func TestBootstrapModuleCatalogIncludesSkillModules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillBundleWithRequirements(t, filepath.Join(root, "skills", "writer"), "writer", "writer.run", nil)

	cfg := testModuleCatalogConfig(root, "", false)
	cfg.Skills.Dirs = []string{filepath.Join(root, "skills")}
	cfg.Skills.AutoRefresh = boolPtr(false)

	app, err := New(context.Background(), cfg, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	projections := app.ModuleCatalogSnapshot().SkillProjections()
	if len(projections) != 1 {
		t.Fatalf("len(skill projections) = %d, want 1", len(projections))
	}
	if projections[0].Name != "writer" || projections[0].ToolCount != 1 {
		t.Fatalf("skill projection = %#v", projections[0])
	}

	contrib := app.ModuleCatalogSnapshot().Contributions()
	if !hasModuleTool(contrib.Tools, "writer.run") {
		t.Fatal("expected writer.run in aggregated module contributions")
	}
}

func TestBootstrapModuleCatalogIncludesConfiguredProviderAndExternalToolModules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := testModuleCatalogConfig(root, "", false)
	cfg.Models.Providers = map[string]config.ProviderConfig{
		"openai": {
			API:          "openai-completions",
			BaseURL:      "https://config.example/v1",
			DefaultModel: "gpt-4o",
		},
	}
	cfg.Tools.External = []config.ExternalToolConfig{{
		Name:        "web.lookup",
		Description: "Lookup URLs",
		Endpoint:    "https://tools.example/lookup",
		Timeout:     "15s",
	}}
	cfg.Channels.Slack = config.SlackChannelConfig{
		Enabled:  boolPtr(true),
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
	}

	app, err := New(context.Background(), cfg, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	snapshot := app.ModuleCatalogSnapshot()
	if _, ok := snapshot.Find(configProviderModulePrefix + "openai"); !ok {
		t.Fatal("expected configured provider module in catalog")
	}
	if _, ok := snapshot.Find(configExternalToolModulePrefix + "web.lookup"); !ok {
		t.Fatal("expected configured external tool module in catalog")
	}
	if _, ok := snapshot.Find(configChannelModulePrefix + "slack"); !ok {
		t.Fatal("expected configured channel module in catalog")
	}

	providers := snapshot.ProviderProjections()
	if len(providers) == 0 || providers[0].Name != "openai" {
		t.Fatalf("provider projections = %#v", providers)
	}
	if !hasModuleTool(snapshot.Contributions().Tools, "web.lookup") {
		t.Fatal("expected configured external tool in aggregated contributions")
	}
	if len(snapshot.ChannelProjections()) == 0 || snapshot.ChannelProjections()[0].Name != "slack" {
		t.Fatalf("channel projections = %#v", snapshot.ChannelProjections())
	}
}

func TestBootstrapModuleCatalogRefreshesAfterPluginReload(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pluginRoot := filepath.Join(root, "plugins")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginRoot): %v", err)
	}

	app, err := New(context.Background(), testModuleCatalogConfig(root, pluginRoot, false), Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())
	initialVersion := app.ModuleCatalog.Version()

	if _, ok := app.ModuleCatalogSnapshot().Find("plugin:dynamic-pack"); ok {
		t.Fatal("plugin:dynamic-pack unexpectedly present before reload")
	}

	pluginDir := filepath.Join(pluginRoot, "dynamic-pack")
	writeHopClawTestPlugin(t, pluginDir, "dynamic-pack", "dynamic.echo")
	if err := app.RefreshPlugins(context.Background()); err != nil {
		t.Fatalf("RefreshPlugins(add) error = %v", err)
	}
	addedVersion := app.ModuleCatalog.Version()
	if addedVersion == initialVersion {
		t.Fatalf("module catalog version unchanged after plugin add: %q", addedVersion)
	}
	module, ok := app.ModuleCatalogSnapshot().Find("plugin:dynamic-pack")
	if !ok {
		t.Fatal("expected plugin:dynamic-pack after plugin refresh")
	}
	if module.Manifest().Level != modules.ModuleLevelMinimal {
		t.Fatalf("dynamic pack level = %q, want %q", module.Manifest().Level, modules.ModuleLevelMinimal)
	}

	if err := os.RemoveAll(pluginDir); err != nil {
		t.Fatalf("RemoveAll(pluginDir): %v", err)
	}
	if err := app.RefreshPlugins(context.Background()); err != nil {
		t.Fatalf("RefreshPlugins(remove) error = %v", err)
	}
	removedVersion := app.ModuleCatalog.Version()
	if removedVersion == addedVersion {
		t.Fatalf("module catalog version unchanged after plugin removal: %q", removedVersion)
	}
	if _, ok := app.ModuleCatalogSnapshot().Find("plugin:dynamic-pack"); ok {
		t.Fatal("plugin:dynamic-pack still present after plugin removal")
	}
}

func TestBootstrapModuleCatalogRefreshesAfterApplyBaseConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	app, err := New(context.Background(), testModuleCatalogConfig(root, "", false), Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	snapshot := app.ModuleCatalogSnapshot()
	initialVersion := app.ModuleCatalog.Version()
	if _, ok := snapshot.Find("builtin:core"); ok {
		t.Fatal("builtin:core unexpectedly present before enabling builtin tools")
	}
	if _, ok := snapshot.Find(builtinBindingPackIntegration); !ok {
		t.Fatal("expected builtin:integration-pack before enabling builtin tools")
	}
	if _, ok := snapshot.Find(firstPartyPackOperatorSupport); !ok {
		t.Fatal("expected builtin:operator-support-pack before enabling builtin tools")
	}

	next := app.BaseConfig
	next.Tools.Builtins.Enabled = boolPtr(true)
	next.Tools.LocalExec.Enabled = boolPtr(true)
	next.Tools.Builtins.Root = root
	next.Tools.Builtins.DefaultExecTimeout = 30 * time.Second
	next.Tools.Builtins.MaxReadBytes = 64 * 1024
	next.Tools.LocalExec.DefaultTimeout = 30 * time.Second

	if err := app.ApplyBaseConfig(context.Background(), next); err != nil {
		t.Fatalf("ApplyBaseConfig() error = %v", err)
	}
	if app.ModuleCatalog.Version() == initialVersion {
		t.Fatalf("module catalog version unchanged after ApplyBaseConfig: %q", initialVersion)
	}

	if _, ok := app.ModuleCatalogSnapshot().Find("builtin:core"); !ok {
		t.Fatal("expected builtin:core after enabling builtin tools")
	}
	if _, ok := app.ModuleCatalogSnapshot().Find(builtinBindingPackKnowledge); !ok {
		t.Fatal("expected builtin:knowledge-pack after enabling builtin tools")
	}
}

func TestBootstrapModuleCatalogRefreshesAfterChannelOnlyConfigReload(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	app, err := New(context.Background(), testModuleCatalogConfig(root, "", false), Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	initialVersion := app.ModuleCatalog.Version()
	initialModule, ok := app.ModuleCatalogSnapshot().Find(builtinBindingPackIntegration)
	if !ok {
		t.Fatal("expected builtin:integration-pack before channel reload")
	}
	if initialModule.Health(context.Background()).Details["channel_count"] != 0 {
		t.Fatalf("initial channel_count = %#v", initialModule.Health(context.Background()).Details["channel_count"])
	}

	next := app.BaseConfig
	next.Channels.Slack.Enabled = boolPtr(true)
	next.Channels.Slack.BotToken = "xoxb-test"
	next.Channels.Slack.AppToken = "xapp-test"

	if err := app.ApplyBaseConfig(context.Background(), next); err != nil {
		t.Fatalf("ApplyBaseConfig() error = %v", err)
	}
	if app.ModuleCatalog.Version() == initialVersion {
		t.Fatalf("module catalog version unchanged after channel-only reload: %q", initialVersion)
	}

	updatedModule, ok := app.ModuleCatalogSnapshot().Find(builtinBindingPackIntegration)
	if !ok {
		t.Fatal("expected builtin:integration-pack after channel reload")
	}
	if updatedModule.Health(context.Background()).Details["channel_count"] != 1 {
		t.Fatalf("updated channel_count = %#v", updatedModule.Health(context.Background()).Details["channel_count"])
	}
}

func TestBootstrapExtensionRegistrySnapshotIncludesFirstPartyPackModules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	app, err := New(context.Background(), testModuleCatalogConfig(root, "", false), Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	snapshot := app.ExtensionRegistry.Snapshot(context.Background(), nil)
	if snapshot.Counts.ModuleCount == 0 {
		t.Fatal("expected extension registry module snapshot to be non-empty")
	}
	if !hasModuleEntry(snapshot.Modules, builtinBindingPackIntegration) {
		t.Fatal("expected builtin:integration-pack in extension registry snapshot")
	}
	if !hasModuleEntry(snapshot.Modules, firstPartyPackOperatorSupport) {
		t.Fatal("expected builtin:operator-support-pack in extension registry snapshot")
	}
	if !hasModuleHealthStatus(snapshot.Modules, builtinBindingPackIntegration, modules.HealthReady) {
		t.Fatal("expected builtin:integration-pack health to be ready in extension registry snapshot")
	}
	if !hasModuleHealthStatus(snapshot.Modules, firstPartyPackOperatorSupport, modules.HealthReady) {
		t.Fatal("expected builtin:operator-support-pack health to be ready in extension registry snapshot")
	}
}

func testModuleCatalogConfig(root, pluginRoot string, builtinsEnabled bool) config.Config {
	return config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 4, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{
			RefreshInterval: 50 * time.Millisecond,
		},
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{
				Enabled:            boolPtr(builtinsEnabled),
				Root:               root,
				DefaultExecTimeout: 30 * time.Second,
				MaxReadBytes:       64 * 1024,
			},
			LocalExec: config.LocalExecConfig{
				Enabled:        boolPtr(builtinsEnabled),
				DefaultTimeout: 30 * time.Second,
			},
		},
		Plugins: config.PluginsConfig{
			Enabled: boolPtr(true),
			Dirs:    []string{pluginRoot},
		},
	}
}

func writeHopClawTestPlugin(t *testing.T, dir, name, toolName string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	manifest := "name: " + name + "\n" +
		"description: test capability pack\n" +
		"tools:\n" +
		"  - name: " + toolName + "\n" +
		"    description: test tool\n" +
		"    endpoint: https://example.com/" + toolName + "\n"
	if err := os.WriteFile(filepath.Join(dir, "hopclaw.plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile(hopclaw.plugin.yaml): %v", err)
	}
}

func hasModuleTool(list []modules.Component, name string) bool {
	for _, item := range list {
		if item.Name == name {
			return true
		}
	}
	return false
}

func hasModuleEntry(list []extregistry.ModuleEntry, id string) bool {
	for _, item := range list {
		if item.ID == id {
			return true
		}
	}
	return false
}

func hasModuleHealthStatus(list []extregistry.ModuleEntry, id string, status modules.HealthStatus) bool {
	for _, item := range list {
		if item.ID == id && item.Health.Status == status {
			return true
		}
	}
	return false
}
