//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlist(t *testing.T) {
	cfg := ServiceConfig{
		BinaryPath: "/usr/local/bin/hopclaw",
		ConfigPath: "/home/user/.hopclaw/config.yaml",
		LogPath:    t.TempDir(),
	}

	plist, err := buildPlist(cfg)
	if err != nil {
		t.Fatalf("buildPlist() error: %v", err)
	}

	checks := []string{
		"<string>com.hopclaw.gateway</string>",
		"<string>/usr/local/bin/hopclaw</string>",
		"<string>serve</string>",
		"<string>--config</string>",
		"<string>/home/user/.hopclaw/config.yaml</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>KeepAlive</key>",
		"<true/>",
		"<key>ThrottleInterval</key>",
		"hopclaw.stdout.log",
		"hopclaw.stderr.log",
	}

	for _, check := range checks {
		if !strings.Contains(plist, check) {
			t.Errorf("plist missing %q", check)
		}
	}
}

func TestPlistPath(t *testing.T) {
	p := plistPath()
	if !strings.Contains(p, "Library/LaunchAgents") {
		t.Errorf("plistPath() = %q, want to contain Library/LaunchAgents", p)
	}
	if !strings.HasSuffix(p, ".plist") {
		t.Errorf("plistPath() = %q, want .plist suffix", p)
	}
}

func TestLaunchdStartKickstartsWhenAlreadyBootstrapped(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsDir := filepath.Join(tmp, "Library", "LaunchAgents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir launch agents: %v", err)
	}
	if err := os.WriteFile(plistPath(), []byte("plist"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	var calls [][]string
	old := launchctlCombinedOutput
	defer func() { launchctlCombinedOutput = old }()
	launchctlCombinedOutput = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "bootstrap" {
			return []byte("already bootstrapped"), fmt.Errorf("bootstrap failed")
		}
		return nil, nil
	}

	if err := (&launchdManager{}).Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 launchctl calls, got %d", len(calls))
	}
	if got := strings.Join(calls[0], " "); !strings.Contains(got, "bootstrap") {
		t.Fatalf("first call = %q", got)
	}
	if got := strings.Join(calls[1], " "); !strings.Contains(got, "kickstart -k") {
		t.Fatalf("second call = %q", got)
	}
}

func TestLaunchdUninstallIgnoresMissingBootout(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsDir := filepath.Join(tmp, "Library", "LaunchAgents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir launch agents: %v", err)
	}
	if err := os.WriteFile(plistPath(), []byte("plist"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	old := launchctlCombinedOutput
	defer func() { launchctlCombinedOutput = old }()
	launchctlCombinedOutput = func(args ...string) ([]byte, error) {
		return []byte("No such process"), fmt.Errorf("bootout failed")
	}

	if err := (&launchdManager{}).Uninstall(); err != nil {
		t.Fatalf("Uninstall() error: %v", err)
	}
	if _, err := os.Stat(plistPath()); !os.IsNotExist(err) {
		t.Fatalf("expected plist to be removed, stat err=%v", err)
	}
}

func TestLaunchdInstallCreatesLaunchAgentsDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	old := launchctlCombinedOutput
	defer func() { launchctlCombinedOutput = old }()
	launchctlCombinedOutput = func(args ...string) ([]byte, error) {
		return nil, nil
	}

	cfg := ServiceConfig{
		BinaryPath: "/usr/local/bin/hopclaw",
		ConfigPath: filepath.Join(tmp, ".hopclaw", "config.yaml"),
		LogPath:    filepath.Join(tmp, ".hopclaw", "logs"),
	}

	if err := (&launchdManager{}).Install(cfg); err != nil {
		t.Fatalf("Install() error: %v", err)
	}
	if _, err := os.Stat(plistPath()); err != nil {
		t.Fatalf("expected plist to exist, stat err=%v", err)
	}
}
