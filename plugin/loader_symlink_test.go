package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverIncludesSymlinkedPluginDirectory(t *testing.T) {
	t.Parallel()

	sourceDir := filepath.Join(t.TempDir(), "demo-plugin-source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `name: demo-plugin
version: "1.0.0"
description: "demo"
`
	if err := os.WriteFile(filepath.Join(sourceDir, manifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "demo-plugin")
	if err := os.Symlink(sourceDir, linkDir); err != nil {
		t.Fatal(err)
	}

	plugins := Discover([]string{root})
	if len(plugins) != 1 {
		t.Fatalf("Discover() len = %d, want 1", len(plugins))
	}
	if plugins[0].Manifest.Name != "demo-plugin" {
		t.Fatalf("plugin name = %q, want demo-plugin", plugins[0].Manifest.Name)
	}
	if plugins[0].Dir != linkDir {
		t.Fatalf("plugin dir = %q, want %q", plugins[0].Dir, linkDir)
	}
}
