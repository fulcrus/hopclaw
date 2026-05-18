package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
)

func TestBootstrapDiscoversOpenClawWorkspaceSkills(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillBundleWithRequirements(t,
		filepath.Join(root, ".openclaw", "workspace", "skills", "compat-skill"),
		"compat-skill",
		"compat.echo",
		map[string]any{
			"openclaw": map[string]any{
				"skillKey": "compat.skill",
			},
		},
	)

	app, err := New(context.Background(), testOpenClawCompatConfig(root, ""), Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	if app.SkillService == nil {
		t.Fatal("expected skill service")
	}
	if _, ok := app.SkillService.Snapshot().Skills["compat-skill"]; !ok {
		t.Fatalf("expected compat-skill in snapshot, got %#v", app.SkillService.Snapshot().Skills)
	}
}

func TestBootstrapStartsSkillWatcherByDefault(t *testing.T) {
	root := t.TempDir()
	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 4, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{
			AutoDetect:      true,
			RefreshInterval: 50 * time.Millisecond,
		},
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{
				Enabled:            boolPtr(false),
				Root:               root,
				DefaultExecTimeout: 30 * time.Second,
				MaxReadBytes:       64 * 1024,
			},
			LocalExec: config.LocalExecConfig{
				Enabled:        boolPtr(false),
				DefaultTimeout: 30 * time.Second,
			},
		},
	}, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	app.refreshMu.Lock()
	watchStarted := app.skillWatchStop != nil
	app.refreshMu.Unlock()
	if !watchStarted {
		t.Fatal("expected skill watcher to start by default")
	}

	if _, ok := app.SkillService.Snapshot().Skills["fresh-skill"]; ok {
		t.Fatal("fresh-skill unexpectedly present before file creation")
	}

	writeSkillBundleWithRequirements(t,
		filepath.Join(root, "skills", "fresh-skill"),
		"fresh-skill",
		"fresh.echo",
		map[string]any{
			"openclaw": map[string]any{
				"skillKey": "fresh.echo",
			},
		},
	)

	waitForSkillState(t, app, "fresh-skill", true)
}

func TestPluginWatcherHotReloadsOpenClawPluginSkills(t *testing.T) {
	root := t.TempDir()
	pluginRoot := filepath.Join(root, "extensions")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginRoot) error = %v", err)
	}
	app, err := New(context.Background(), testOpenClawCompatConfig(root, pluginRoot), Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())
	waitForPluginWatcherStarted(t, app)

	if _, ok := app.SkillService.Snapshot().Skills["plugin-compat-skill"]; ok {
		t.Fatal("plugin skill unexpectedly present before plugin install")
	}

	pluginDir := filepath.Join(pluginRoot, "compat-plugin")
	writeOpenClawPluginWithSkill(t, pluginDir, "plugin-compat-skill", "plugin.compat.echo")

	waitForSkillState(t, app, "plugin-compat-skill", true)

	removeOpenClawPlugin(t, pluginDir)
	waitForSkillState(t, app, "plugin-compat-skill", false)
}

func TestPluginWatcherIgnoresStaleGeneration(t *testing.T) {
	root := t.TempDir()
	pluginRoot := filepath.Join(root, "extensions")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginRoot) error = %v", err)
	}
	app, err := New(context.Background(), testOpenClawCompatConfig(root, pluginRoot), Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	app.refreshMu.Lock()
	if app.pluginWatchStop != nil {
		app.pluginWatchStop()
		app.pluginWatchStop = nil
	}
	currentGen := app.pluginWatchGen
	if currentGen == 0 {
		currentGen = 1
		app.pluginWatchGen = currentGen
	}
	staleGen := currentGen + 1
	app.refreshMu.Unlock()

	pluginDir := filepath.Join(pluginRoot, "compat-plugin")
	writeOpenClawPluginWithSkill(t, pluginDir, "plugin-compat-skill", "plugin.compat.echo")

	if err := app.refreshPluginsFromWatcher(context.Background(), staleGen); err != nil {
		t.Fatalf("refreshPluginsFromWatcher(stale) error = %v", err)
	}
	if _, ok := app.SkillService.Snapshot().Skills["plugin-compat-skill"]; ok {
		t.Fatal("stale watcher generation unexpectedly refreshed plugin skills")
	}

	if err := app.refreshPluginsFromWatcher(context.Background(), currentGen); err != nil {
		t.Fatalf("refreshPluginsFromWatcher(current) error = %v", err)
	}
	if _, ok := app.SkillService.Snapshot().Skills["plugin-compat-skill"]; !ok {
		t.Fatal("current watcher generation failed to refresh plugin skills")
	}
}

func TestPluginWatcherStopDoesNotBlockOnInflightRefresh(t *testing.T) {
	root := t.TempDir()
	pluginRoot := filepath.Join(root, "extensions")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginRoot) error = %v", err)
	}
	app, err := New(context.Background(), testOpenClawCompatConfig(root, pluginRoot), Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())
	waitForPluginWatcherStarted(t, app)

	app.refreshMu.Lock()
	pluginDir := filepath.Join(pluginRoot, "compat-plugin")
	writeOpenClawPluginWithSkill(t, pluginDir, "plugin-compat-skill", "plugin.compat.echo")
	time.Sleep(400 * time.Millisecond)

	stop := app.pluginWatchStop
	if stop == nil {
		app.refreshMu.Unlock()
		t.Fatal("expected plugin watcher stop function")
	}
	stopped := make(chan struct{})
	go func() {
		stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		app.refreshMu.Unlock()
		t.Fatal("plugin watcher stop blocked on in-flight refresh")
	}

	app.pluginWatchGen++
	app.pluginWatchStop = nil
	app.refreshMu.Unlock()
}

func TestPluginWatcherRestartFromCallbackContextIsDeferred(t *testing.T) {
	root := t.TempDir()
	pluginRoot := filepath.Join(root, "extensions")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginRoot) error = %v", err)
	}
	app, err := New(context.Background(), testOpenClawCompatConfig(root, pluginRoot), Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())
	waitForPluginWatcherStarted(t, app)

	app.refreshMu.Lock()
	initialGen := app.pluginWatchGen
	if initialGen == 0 {
		app.refreshMu.Unlock()
		t.Fatal("expected plugin watcher generation")
	}
	app.requestPluginWatcherRestartLocked(markPluginWatcherCallbackContext(context.Background(), initialGen))
	if !app.pluginWatchRestartQueued {
		app.refreshMu.Unlock()
		t.Fatal("expected deferred plugin watcher restart to be queued")
	}
	if app.pluginWatchGen != initialGen {
		app.refreshMu.Unlock()
		t.Fatal("plugin watcher restarted synchronously from callback context")
	}
	app.refreshMu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.refreshMu.Lock()
		queued := app.pluginWatchRestartQueued
		gen := app.pluginWatchGen
		app.refreshMu.Unlock()
		if !queued && gen > initialGen {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for deferred plugin watcher restart")
}

func testOpenClawCompatConfig(root, pluginRoot string) config.Config {
	enabled := false
	cfg := config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 4, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{
			AutoDetect:      true,
			RefreshInterval: 50 * time.Millisecond,
		},
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{
				Enabled:            &enabled,
				Root:               root,
				DefaultExecTimeout: 30 * time.Second,
				MaxReadBytes:       64 * 1024,
			},
			LocalExec: config.LocalExecConfig{
				Enabled:        &enabled,
				DefaultTimeout: 30 * time.Second,
			},
		},
		Plugins: config.PluginsConfig{
			Enabled: boolPtr(true),
			Dirs:    []string{pluginRoot},
		},
	}
	return cfg
}

func writeOpenClawPluginWithSkill(t *testing.T, dir, skillName, toolName string) {
	t.Helper()

	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("MkdirAll(parent): %v", err)
	}
	staging, err := os.MkdirTemp(parent, ".plugin-staging-*")
	if err != nil {
		t.Fatalf("MkdirTemp(): %v", err)
	}
	skillDir := filepath.Join(staging, "skills", skillName)
	writeSkillBundleWithRequirements(t, skillDir, skillName, toolName, map[string]any{
		"openclaw": map[string]any{
			"skillKey": toolName,
		},
	})
	manifest := `{
  "id": "compat-plugin",
  "skills": ["./skills"],
  "configSchema": {
    "type": "object",
    "additionalProperties": false,
    "properties": {}
  }
}`
	if err := os.WriteFile(filepath.Join(staging, "openclaw.plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile(openclaw.plugin.json): %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("RemoveAll(existing plugin): %v", err)
	}
	if err := os.Rename(staging, dir); err != nil {
		t.Fatalf("Rename(staging plugin): %v", err)
	}
}

func waitForSkillState(t *testing.T, app *App, name string, wantPresent bool) {
	t.Helper()

	timeout := 5 * time.Second
	if app != nil && app.Config.Skills.RefreshInterval > 0 {
		derived := 10*app.Config.Skills.RefreshInterval + time.Second
		if derived > timeout {
			timeout = derived
		}
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		app.refreshMu.Lock()
		svc := app.SkillService
		app.refreshMu.Unlock()
		if svc != nil {
			_, ok := svc.Snapshot().Skills[name]
			if ok == wantPresent {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if wantPresent {
		t.Fatalf("timed out waiting for skill %q to appear within %s", name, timeout)
	}
	t.Fatalf("timed out waiting for skill %q to disappear within %s", name, timeout)
}

func waitForPluginWatcherStarted(t *testing.T, app *App) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.refreshMu.Lock()
		started := app.pluginWatchStop != nil
		app.refreshMu.Unlock()
		if started {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for plugin watcher to start")
}

func removeOpenClawPlugin(t *testing.T, dir string) {
	t.Helper()

	parent := filepath.Dir(dir)
	trashRoot := filepath.Join(parent, ".disabled")
	if err := os.MkdirAll(trashRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(trashRoot): %v", err)
	}
	moved := filepath.Join(trashRoot, filepath.Base(dir))
	if err := os.RemoveAll(moved); err != nil {
		t.Fatalf("RemoveAll(stale moved plugin): %v", err)
	}
	if err := os.Rename(dir, moved); err != nil {
		t.Fatalf("Rename(plugin to trash): %v", err)
	}
	if err := os.RemoveAll(moved); err != nil {
		t.Fatalf("RemoveAll(moved plugin): %v", err)
	}
}
