package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateDir(t *testing.T) {
	dir := StateDir()
	if !strings.HasSuffix(dir, ".hopclaw") {
		t.Errorf("StateDir() = %q, want suffix .hopclaw", dir)
	}
}

func TestLogDir(t *testing.T) {
	dir := LogDir()
	if !strings.HasSuffix(dir, filepath.Join(".hopclaw", "logs")) {
		t.Errorf("LogDir() = %q, want suffix .hopclaw/logs", dir)
	}
}

func TestDataDir(t *testing.T) {
	dir := DataDir()
	if !strings.HasSuffix(dir, filepath.Join(".hopclaw", "data")) {
		t.Errorf("DataDir() = %q, want suffix .hopclaw/data", dir)
	}
}

func TestConfigFilePath(t *testing.T) {
	p := ConfigFilePath()
	if !strings.HasSuffix(p, filepath.Join(".hopclaw", "config.yaml")) {
		t.Errorf("ConfigFilePath() = %q, want suffix .hopclaw/config.yaml", p)
	}
}

func TestEnsureStateDir(t *testing.T) {
	// Use a temp dir as HOME.
	tmp := t.TempDir()
	orig := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer os.Setenv("HOME", orig)

	if err := EnsureStateDir(); err != nil {
		t.Fatalf("EnsureStateDir() error: %v", err)
	}

	// Verify directories were created.
	for _, sub := range []string{"", "logs", "data"} {
		dir := filepath.Join(tmp, ".hopclaw", sub)
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("directory %s not created: %v", dir, err)
		} else if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}
}
