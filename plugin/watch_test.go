package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherUsesInitialFingerprintToDetectPreStartChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	initialFingerprint, err := FingerprintDirs([]string{root})
	if err != nil {
		t.Fatalf("FingerprintDirs() error = %v", err)
	}

	pluginDir := filepath.Join(root, "compat-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, openClawManifestFile), []byte(`{"id":"compat-plugin"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(openClawManifestFile) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changed := make(chan struct{}, 1)
	go func() {
		_ = Watcher{
			Dirs:               []string{root},
			Interval:           10 * time.Millisecond,
			InitialFingerprint: initialFingerprint,
			OnChange: func() {
				select {
				case changed <- struct{}{}:
				default:
				}
			},
		}.Run(ctx)
	}()

	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watcher to detect pre-start plugin change")
	}
}
