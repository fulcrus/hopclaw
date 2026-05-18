package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// seedStateDir populates a temporary state directory with realistic files for
// testing. Returns the stateDir path.
func seedStateDir(t *testing.T) string {
	t.Helper()
	stateDir := t.TempDir()

	// config.yaml
	writeTestFile(t, filepath.Join(stateDir, "config.yaml"), "server:\n  address: 127.0.0.1:16280\n")

	// cron/jobs.json (nested under a cron subdirectory)
	cronDir := filepath.Join(stateDir, "cron")
	mustMkdirAll(t, cronDir)
	writeTestFile(t, filepath.Join(cronDir, "jobs.json"), `{"version":1,"jobs":[]}`)

	// data directory with a JSONL event file
	dataDir := filepath.Join(stateDir, "data")
	mustMkdirAll(t, dataDir)
	writeTestFile(t, filepath.Join(dataDir, "events.jsonl"), "{\"id\":\"1\"}\n{\"id\":\"2\"}\n")

	// logs directory (should be skipped)
	logsDir := filepath.Join(stateDir, "logs")
	mustMkdirAll(t, logsDir)
	writeTestFile(t, filepath.Join(logsDir, "hopclaw.log"), "log line\n")

	// PID file (should be skipped by extension)
	writeTestFile(t, filepath.Join(stateDir, "hopclaw.pid"), "12345")

	return stateDir
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCreateProducesValidArchive(t *testing.T) {
	stateDir := seedStateDir(t)
	svc := NewService(stateDir)
	ctx := context.Background()

	result, err := svc.Create(ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Archive file should exist and be non-empty.
	info, err := os.Stat(result.Path)
	if err != nil {
		t.Fatalf("stat archive: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("archive is empty")
	}
	if result.Size != info.Size() {
		t.Fatalf("result.Size = %d, want %d", result.Size, info.Size())
	}

	// Should contain config.yaml, cron/jobs.json, data/events.jsonl but NOT
	// logs or PID files.
	if result.FileCount < 3 {
		t.Fatalf("file count = %d, want >= 3", result.FileCount)
	}

	// Verify the embedded manifest is readable.
	manifest, err := readManifestFromArchive(result.Path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if manifest.Version != manifestVersion {
		t.Fatalf("manifest version = %d, want %d", manifest.Version, manifestVersion)
	}
	if len(manifest.Files) != result.FileCount {
		t.Fatalf("manifest file count = %d, want %d", len(manifest.Files), result.FileCount)
	}

	// Verify that excluded files are absent.
	for _, fe := range manifest.Files {
		if strings.Contains(fe.Path, "logs") {
			t.Errorf("archive unexpectedly contains log file: %s", fe.Path)
		}
		if strings.HasSuffix(fe.Path, ".pid") {
			t.Errorf("archive unexpectedly contains PID file: %s", fe.Path)
		}
	}
}

func TestCreateSkipsLargeFiles(t *testing.T) {
	stateDir := t.TempDir()

	// Write a file that exceeds maxFileSize.
	largeFile := filepath.Join(stateDir, "huge.dat")
	if err := os.WriteFile(largeFile, make([]byte, maxFileSize+1), 0o644); err != nil {
		t.Fatalf("write large file: %v", err)
	}

	// Also write a small file so the backup is non-empty.
	writeTestFile(t, filepath.Join(stateDir, "small.txt"), "hello")

	svc := NewService(stateDir)
	result, err := svc.Create(context.Background())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, fe := range result.Path {
		_ = fe // result.Path is a string; we check the manifest instead.
	}

	manifest, err := readManifestFromArchive(result.Path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	for _, fe := range manifest.Files {
		if fe.Path == "huge.dat" {
			t.Error("archive unexpectedly contains the oversized file")
		}
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("file count = %d, want 1", len(manifest.Files))
	}
}

func TestListFindsCreatedBackups(t *testing.T) {
	stateDir := seedStateDir(t)
	svc := NewService(stateDir)
	ctx := context.Background()

	// Initially empty.
	list, err := svc.List()
	if err != nil {
		t.Fatalf("List (empty): %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 backups, got %d", len(list))
	}

	// Create two backups.
	r1, err := svc.Create(ctx)
	if err != nil {
		t.Fatalf("Create 1: %v", err)
	}

	// Ensure a distinct timestamp in the filename.
	time.Sleep(time.Second)

	r2, err := svc.Create(ctx)
	if err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	list, err = svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 backups, got %d", len(list))
	}

	// Newest first.
	if list[0].Path != r2.Path {
		t.Errorf("list[0].Path = %s, want %s", list[0].Path, r2.Path)
	}
	if list[1].Path != r1.Path {
		t.Errorf("list[1].Path = %s, want %s", list[1].Path, r1.Path)
	}
}

func TestRestoreCorrectlyRestoresFiles(t *testing.T) {
	stateDir := seedStateDir(t)
	svc := NewService(stateDir)
	ctx := context.Background()

	result, err := svc.Create(ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Modify a file so we can verify it gets restored.
	configPath := filepath.Join(stateDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("modified"), 0o644); err != nil {
		t.Fatalf("write modified config: %v", err)
	}

	rr, err := svc.Restore(ctx, result.Path)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if rr.FilesRestored != result.FileCount {
		t.Fatalf("restored %d files, want %d", rr.FilesRestored, result.FileCount)
	}

	// The original config should be backed up.
	bakPath := configPath + bakSuffix
	bakData, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("read .bak: %v", err)
	}
	if string(bakData) != "modified" {
		t.Fatalf("bak content = %q, want %q", string(bakData), "modified")
	}

	// The restored config should match the original.
	restored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if !strings.HasPrefix(string(restored), "server:") {
		t.Fatalf("restored config content = %q, expected original YAML", string(restored))
	}
}

func TestRoundTrip(t *testing.T) {
	stateDir := seedStateDir(t)
	svc := NewService(stateDir)
	ctx := context.Background()

	// Create backup.
	backupResult, err := svc.Create(ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Delete all non-backup files from state dir to simulate data loss.
	manifest, err := readManifestFromArchive(backupResult.Path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	for _, fe := range manifest.Files {
		_ = os.Remove(fe.OrigPath)
	}

	// Verify files are gone.
	configPath := filepath.Join(stateDir, "config.yaml")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatal("expected config.yaml to be deleted")
	}

	// Restore.
	restoreResult, err := svc.Restore(ctx, backupResult.Path)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restoreResult.FilesRestored != backupResult.FileCount {
		t.Fatalf("restored %d files, want %d", restoreResult.FilesRestored, backupResult.FileCount)
	}

	// Verify config is back.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if !strings.HasPrefix(string(data), "server:") {
		t.Fatalf("restored config = %q, want original YAML", string(data))
	}

	// Verify JSONL data is back.
	eventsPath := filepath.Join(stateDir, "data", "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("read restored events: %v", err)
	}
	if !strings.Contains(string(eventsData), `"id":"1"`) {
		t.Fatalf("restored events = %q, want original content", string(eventsData))
	}
}

func TestCreateRespectsContextCancellation(t *testing.T) {
	stateDir := seedStateDir(t)
	svc := NewService(stateDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := svc.Create(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestRestoreRespectsContextCancellation(t *testing.T) {
	stateDir := seedStateDir(t)
	svc := NewService(stateDir)

	// Create a valid backup first.
	result, err := svc.Create(context.Background())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.Restore(ctx, result.Path)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestListOnNonExistentDir(t *testing.T) {
	svc := NewService(filepath.Join(t.TempDir(), "nonexistent"))

	list, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 backups, got %d", len(list))
	}
}

func TestRestoreWithInvalidPath(t *testing.T) {
	svc := NewService(t.TempDir())

	_, err := svc.Restore(context.Background(), "/nonexistent/backup.tar.gz")
	if err == nil {
		t.Fatal("expected error for nonexistent backup path")
	}
}
