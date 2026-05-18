package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/model"
	"github.com/fulcrus/hopclaw/plugin"
	"github.com/fulcrus/hopclaw/skill"
)

func TestUpdatePolicyConfigured(t *testing.T) {
	t.Parallel()

	if updatePolicyConfigured(config.UpdateConfig{}) {
		t.Fatal("zero update config should not be treated as configured")
	}
	if !updatePolicyConfigured(config.UpdateConfig{ManifestURL: "https://example.com/manifest.json"}) {
		t.Fatal("manifest override should mark update config as configured")
	}
}

func TestInitDiagnosticsCreatesBugReportDir(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "bugs")
	if err := initDiagnostics(config.DiagnosticsConfig{BugReportDir: dir}); err != nil {
		t.Fatalf("initDiagnostics() error = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("Stat(%s) error = %v", dir, err)
	}
}

func TestInitDiagnosticsCreatesCollectorDir(t *testing.T) {
	t.Parallel()

	enabled := true
	dir := filepath.Join(t.TempDir(), "collector")
	if err := initDiagnostics(config.DiagnosticsConfig{
		CollectorEnabled:   &enabled,
		CollectorDir:       dir,
		CollectorAuthToken: "collector-token",
	}); err != nil {
		t.Fatalf("initDiagnostics() error = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("Stat(%s) error = %v", dir, err)
	}
}

func TestInitDiagnosticsCollectorRequiresToken(t *testing.T) {
	t.Parallel()

	enabled := true
	err := initDiagnostics(config.DiagnosticsConfig{
		CollectorEnabled: &enabled,
		CollectorDir:     filepath.Join(t.TempDir(), "collector"),
	})
	if err == nil {
		t.Fatal("expected collector without auth token to fail")
	}
}

func TestInitTunnelSupportErrorsWhenEnabled(t *testing.T) {
	t.Parallel()

	enabled := true
	if err := initTunnelSupport(config.TunnelConfig{Enabled: &enabled}); err == nil {
		t.Fatal("expected enabled tunnel config to produce a startup warning error")
	}
}

func TestInitSkillsBuildsPluginRootsFromModuleCatalogProjections(t *testing.T) {
	t.Parallel()

	manager := plugin.NewManager()
	for _, loaded := range []plugin.LoadedPlugin{
		{
			Dir: filepath.Join(t.TempDir(), "zeta"),
			Manifest: plugin.Manifest{
				Name:      "zeta",
				SkillsDir: "skills",
			},
		},
		{
			Dir: filepath.Join(t.TempDir(), "alpha"),
			Manifest: plugin.Manifest{
				Name:       "alpha",
				SkillsDirs: []string{"skills", "extras"},
			},
		},
	} {
		if err := manager.Register(loaded); err != nil {
			t.Fatalf("Register(%s) error = %v", loaded.Manifest.Name, err)
		}
	}
	moduleCatalog := modules.NewStore(modules.BuildCatalog(manager.Modules()))

	service, stop, err := initSkills(context.Background(), config.SkillsConfig{}, t.TempDir(), moduleCatalog)
	if stop != nil {
		defer stop()
	}
	if err != nil {
		t.Fatalf("initSkills() error = %v", err)
	}
	if service == nil {
		t.Fatal("expected skill service")
	}

	roots := service.Watcher(nil).Roots
	got := make([]string, 0, len(roots))
	for _, root := range roots {
		if root.Kind != skill.SourcePlugin {
			continue
		}
		got = append(got, root.Path)
	}
	if want := pluginSkillDirPaths(moduleCatalog); !reflect.DeepEqual(got, want) {
		t.Fatalf("plugin roots = %v, want %v", got, want)
	}
}

func TestInitSkillsTreatsWorkspaceExtensionsAsBundledRoots(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	manager := plugin.NewManager()
	loaded := plugin.LoadedPlugin{
		Dir: filepath.Join(workspaceRoot, "extensions", "tools", "web-search"),
		Manifest: plugin.Manifest{
			Name:      "web-search",
			SkillsDir: ".",
		},
	}
	if err := manager.Register(loaded); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	moduleCatalog := modules.NewStore(modules.BuildCatalog(manager.Modules()))

	service, stop, err := initSkills(context.Background(), config.SkillsConfig{}, workspaceRoot, moduleCatalog)
	if stop != nil {
		defer stop()
	}
	if err != nil {
		t.Fatalf("initSkills() error = %v", err)
	}
	if service == nil {
		t.Fatal("expected skill service")
	}

	wantPath := filepath.Join(workspaceRoot, "extensions", "tools", "web-search")
	for _, root := range service.Watcher(nil).Roots {
		if root.Path != wantPath {
			continue
		}
		if root.Kind != skill.SourceBundled {
			t.Fatalf("root.Kind = %q, want %q", root.Kind, skill.SourceBundled)
		}
		if root.Priority != 200 {
			t.Fatalf("root.Priority = %d, want 200", root.Priority)
		}
		return
	}

	t.Fatalf("bundled root %q not found", wantPath)
}

func TestInitSkillsRefreshesModuleCatalogWithSkillModules(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	skillRoot := filepath.Join(workspaceRoot, "skills")
	writeSkillBundleWithRequirements(t, filepath.Join(skillRoot, "writer"), "writer", "writer.run", nil)

	moduleCatalog := modules.NewStore(modules.BuildCatalog([]modules.StaticModule{{
		ManifestValue: modules.Manifest{
			ID:   "builtin:core",
			Name: "core",
		},
	}}))

	service, stop, err := initSkills(context.Background(), config.SkillsConfig{
		Dirs:        []string{skillRoot},
		AutoRefresh: boolPtr(false),
	}, workspaceRoot, moduleCatalog)
	if stop != nil {
		defer stop()
	}
	if err != nil {
		t.Fatalf("initSkills() error = %v", err)
	}
	if service == nil {
		t.Fatal("expected skill service")
	}

	projections := moduleCatalog.SkillProjections()
	if len(projections) != 1 || projections[0].Name != "writer" {
		t.Fatalf("skill projections after init = %#v", projections)
	}
	if len(moduleCatalog.ToolProjections()) != 1 || moduleCatalog.ToolProjections()[0].Name != "writer.run" {
		t.Fatalf("tool projections after init = %#v", moduleCatalog.ToolProjections())
	}

	versionBefore := moduleCatalog.Version()
	writeSkillBundleWithRequirements(t, filepath.Join(skillRoot, "reviewer"), "reviewer", "reviewer.run", nil)
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("service.Refresh() error = %v", err)
	}
	if moduleCatalog.Version() == versionBefore {
		t.Fatalf("module catalog version unchanged after skill refresh: %q", versionBefore)
	}

	names := make([]string, 0, len(moduleCatalog.SkillProjections()))
	for _, projection := range moduleCatalog.SkillProjections() {
		names = append(names, projection.Name)
	}
	if want := []string{"reviewer", "writer"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("skill projection names = %v, want %v", names, want)
	}
	if _, ok := moduleCatalog.Find("builtin:core"); !ok {
		t.Fatal("expected builtin:core to be preserved after skill refresh")
	}
}

func TestBuildEmbeddingProviderUsesRegisteredBuilders(t *testing.T) {
	t.Parallel()

	client, err := buildEmbeddingProvider(config.EmbeddingProviderConfig{
		API:     string(model.EmbedGemini),
		BaseURL: "https://gemini.example.com",
		APIKey:  "gemini-key",
		Model:   "text-embedding-004",
	})
	if err != nil {
		t.Fatalf("buildEmbeddingProvider(gemini) error = %v", err)
	}
	if _, ok := client.(*model.GeminiEmbeddingClient); !ok {
		t.Fatalf("buildEmbeddingProvider(gemini) = %T, want *model.GeminiEmbeddingClient", client)
	}

	client, err = buildEmbeddingProvider(config.EmbeddingProviderConfig{
		BaseURL: "https://embeddings.example.com",
		APIKey:  "openai-key",
		Model:   "text-embedding-3-small",
	})
	if err != nil {
		t.Fatalf("buildEmbeddingProvider(default) error = %v", err)
	}
	if _, ok := client.(*model.EmbeddingClient); !ok {
		t.Fatalf("buildEmbeddingProvider(default) = %T, want *model.EmbeddingClient", client)
	}
}
