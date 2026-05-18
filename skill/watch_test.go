package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherRefreshesOnSkillAdd(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(root): %v", err)
	}

	service := NewService(ServiceConfig{
		Roots: []DiscoveryRoot{{Kind: SourceWorkspace, Path: root}},
	})
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh(): %v", err)
	}

	updates := make(chan RegistrySnapshot, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher := service.Watcher(func(snapshot RegistrySnapshot) {
		updates <- snapshot
	})
	watcher.Interval = time.Second

	go func() {
		_ = watcher.Run(ctx)
	}()

	select {
	case <-updates:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for initial watcher refresh")
	}

	mustWriteSkill(t, filepath.Join(root, "news"), "news", "news skill")

	deadline := time.After(5 * time.Second)
	for {
		select {
		case snapshot := <-updates:
			if _, ok := snapshot.Skills["news"]; ok {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for watcher to pick up added skill")
		}
	}
}

func TestRegistryFingerprintChangesWhenSkillContentChanges(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "skills")
	mustWriteSkill(t, filepath.Join(root, "alpha"), "alpha", "first description")

	reg := NewRegistry(FilesystemLoader{}, DefaultCompiler{})
	first, err := reg.Refresh(context.Background(), []DiscoveryRoot{{Kind: SourceWorkspace, Path: root}})
	if err != nil {
		t.Fatalf("Refresh(first): %v", err)
	}

	mustWriteSkill(t, filepath.Join(root, "alpha"), "alpha", "updated description")
	second, err := reg.Refresh(context.Background(), []DiscoveryRoot{{Kind: SourceWorkspace, Path: root}})
	if err != nil {
		t.Fatalf("Refresh(second): %v", err)
	}

	if first.Fingerprint == second.Fingerprint {
		t.Fatalf("expected fingerprint to change after skill content update: %q", first.Fingerprint)
	}
}
