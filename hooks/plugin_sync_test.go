package hooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/plugin"
)

func TestSyncPluginHooksReconcilesStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hooksDir := filepath.Join(root, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "hopclaw.plugin.yaml"), []byte("name: demo\nhooks_dir: hooks\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "notify.yaml"), []byte(`
hooks:
  - name: plugin-notify
    trigger: run.completed
    kind: http
    url: https://example.com/hook
    async: true
`), 0o644); err != nil {
		t.Fatalf("WriteFile(hook) error = %v", err)
	}

	loaded, err := plugin.LoadForTest(root)
	if err != nil {
		t.Fatalf("LoadForTest() error = %v", err)
	}
	manager := plugin.NewManager()
	if err := manager.Register(loaded); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	store := NewInMemoryStore()
	if _, err := store.Add(context.Background(), Hook{
		Name:      "stale-plugin-hook",
		Source:    pluginHookSource,
		SourceRef: "old-plugin",
		Trigger:   TriggerRunCompleted,
		Kind:      KindHTTP,
		URL:       "https://example.com/old",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := SyncPluginHooks(context.Background(), store, manager); err != nil {
		t.Fatalf("SyncPluginHooks() error = %v", err)
	}

	items, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("store len = %d, want 1", len(items))
	}
	if items[0].Name != "plugin-notify" {
		t.Fatalf("hook name = %q", items[0].Name)
	}
	if items[0].Source != pluginHookSource || items[0].SourceRef != "demo" {
		t.Fatalf("unexpected source fields: %#v", items[0])
	}
}

func TestSyncModuleHooksReconcilesStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hooksDir := filepath.Join(root, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "notify.yaml"), []byte(`
hooks:
  - name: projected-notify
    trigger: run.completed
    kind: http
    url: https://example.com/projected
    async: true
`), 0o644); err != nil {
		t.Fatalf("WriteFile(hook) error = %v", err)
	}

	store := NewInMemoryStore()
	if _, err := store.Add(context.Background(), Hook{
		Name:      "stale-plugin-hook",
		Source:    pluginHookSource,
		SourceRef: "old-plugin",
		Trigger:   TriggerRunCompleted,
		Kind:      KindHTTP,
		URL:       "https://example.com/old",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	projections := []modules.DirectoryProjection{{
		Kind:       modules.ComponentKindHooksDir,
		Path:       hooksDir,
		ModuleID:   "plugin:demo",
		ModuleName: "demo",
		Source:     modules.SourcePlugin,
		Delivery:   modules.DeliveryManifest,
	}}
	if err := SyncModuleHooks(context.Background(), store, projections); err != nil {
		t.Fatalf("SyncModuleHooks() error = %v", err)
	}

	items, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("store len = %d, want 1", len(items))
	}
	if items[0].Name != "projected-notify" {
		t.Fatalf("hook name = %q", items[0].Name)
	}
	if items[0].Source != pluginHookSource || items[0].SourceRef != "demo" {
		t.Fatalf("unexpected source fields: %#v", items[0])
	}
}
