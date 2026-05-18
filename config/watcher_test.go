package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewWatcherDefaults(t *testing.T) {
	t.Parallel()

	initial := Config{Agent: AgentConfig{DefaultModel: "gpt-4o"}}
	w := NewWatcher("/nonexistent/config.yaml", initial, 0)
	if w == nil {
		t.Fatal("NewWatcher returned nil")
	}
	if w.interval != 5*time.Second {
		t.Fatalf("interval = %v, want 5s", w.interval)
	}
}

func TestNewWatcherCustomInterval(t *testing.T) {
	t.Parallel()

	initial := Config{}
	w := NewWatcher("/nonexistent/config.yaml", initial, 10*time.Second)
	if w.interval != 10*time.Second {
		t.Fatalf("interval = %v, want 10s", w.interval)
	}
}

func TestWatcherCurrent(t *testing.T) {
	t.Parallel()

	initial := Config{Agent: AgentConfig{DefaultModel: "test-model"}}
	w := NewWatcher("/nonexistent/config.yaml", initial, time.Hour)

	current := w.Current()
	if current.Agent.DefaultModel != "test-model" {
		t.Fatalf("Current().Agent.Model = %q, want %q", current.Agent.DefaultModel, "test-model")
	}
}

func TestWatcherOnReloadRegisters(t *testing.T) {
	t.Parallel()

	w := NewWatcher("/nonexistent/config.yaml", Config{}, time.Hour)

	called := false
	w.OnReload(func(old, new Config) error {
		called = true
		return nil
	})

	if len(w.callbacks) != 1 {
		t.Fatalf("len(callbacks) = %d, want 1", len(w.callbacks))
	}
	// Verify the callback is callable.
	_ = w.callbacks[0](Config{}, Config{})
	if !called {
		t.Fatal("callback was not invoked")
	}
}

func TestWatcherOnReloadV2Registers(t *testing.T) {
	t.Parallel()

	w := NewWatcher("/nonexistent/config.yaml", Config{}, time.Hour)

	called := false
	w.OnReloadV2(func(old, new Config, changes ChangeSet) error {
		called = true
		return nil
	})

	if len(w.callbacksV2) != 1 {
		t.Fatalf("len(callbacksV2) = %d, want 1", len(w.callbacksV2))
	}
	_ = w.callbacksV2[0](Config{}, Config{}, ChangeSet{})
	if !called {
		t.Fatal("v2 callback was not invoked")
	}
}

func TestHashFileNonexistent(t *testing.T) {
	t.Parallel()

	hash := hashFile("/nonexistent/path/config.yaml")
	var zero [32]byte
	if hash != zero {
		t.Fatal("hashFile for nonexistent path should return zero hash")
	}
}

func TestHashFileConsistent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte("key: value\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h1 := hashFile(path)
	h2 := hashFile(path)
	if h1 != h2 {
		t.Fatal("hashFile should return consistent results for the same content")
	}

	var zero [32]byte
	if h1 == zero {
		t.Fatal("hashFile for existing file should not be zero")
	}
}

func TestHashFileChangesOnWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte("key: value1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h1 := hashFile(path)

	if err := os.WriteFile(path, []byte("key: value2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h2 := hashFile(path)
	if h1 == h2 {
		t.Fatal("hashFile should return different results for different content")
	}
}

func TestWatcherReload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("agent:\n  default_model: model-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	initial, err := Load(path)
	if err != nil {
		t.Fatalf("Load(initial) error = %v", err)
	}
	w := NewWatcher(path, initial, time.Hour)

	called := false
	w.OnReload(func(old, new Config) error {
		called = true
		if old.Agent.DefaultModel != "model-a" {
			t.Fatalf("old model = %q", old.Agent.DefaultModel)
		}
		if new.Agent.DefaultModel != "model-b" {
			t.Fatalf("new model = %q", new.Agent.DefaultModel)
		}
		return nil
	})

	if err := os.WriteFile(path, []byte("agent:\n  default_model: model-b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if !called {
		t.Fatal("expected reload callback to be called")
	}
	if got := w.Current().Agent.DefaultModel; got != "model-b" {
		t.Fatalf("Current().Agent.DefaultModel = %q, want model-b", got)
	}
}

func TestWatcherReloadRejectsFatalChanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  address: 127.0.0.1:16280\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	initial, err := Load(path)
	if err != nil {
		t.Fatalf("Load(initial) error = %v", err)
	}
	w := NewWatcher(path, initial, time.Hour)

	if err := os.WriteFile(path, []byte("server:\n  address: 127.0.0.1:17280\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = w.Reload()
	if err == nil {
		t.Fatal("expected Reload() to reject fatal changes")
	}
	if got := w.Current().Server.Address; got != "127.0.0.1:16280" {
		t.Fatalf("Current().Server.Address = %q, want original value", got)
	}
}
