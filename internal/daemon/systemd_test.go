//go:build linux

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemdStatusParsesActiveAndPID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	unitDir := userUnitDir()
	if err := EnsureStateDir(); err != nil {
		t.Fatalf("EnsureStateDir(): %v", err)
	}
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatalf("mkdir unit dir: %v", err)
	}
	unitPath := filepath.Join(unitDir, systemdServiceName)
	if err := os.WriteFile(unitPath, []byte("[Unit]\nDescription=HopClaw Gateway\n"), 0o644); err != nil {
		t.Fatalf("write unit: %v", err)
	}

	old := systemctlCombinedOutput
	defer func() { systemctlCombinedOutput = old }()
	systemctlCombinedOutput = func(args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "--user is-active hopclaw.service":
			return []byte("active\n"), nil
		case "--user show --property=MainPID hopclaw.service":
			return []byte("MainPID=4242\n"), nil
		default:
			return nil, nil
		}
	}

	status, err := (&systemdManager{}).Status()
	if err != nil {
		t.Fatalf("Status(): %v", err)
	}
	if !status.Installed || !status.Running || status.PID != 4242 {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestSystemctlUsesUserScope(t *testing.T) {
	var got []string
	old := systemctlCombinedOutput
	defer func() { systemctlCombinedOutput = old }()
	systemctlCombinedOutput = func(args ...string) ([]byte, error) {
		got = append([]string(nil), args...)
		return nil, nil
	}

	if err := systemctl("restart", systemdServiceName); err != nil {
		t.Fatalf("systemctl(): %v", err)
	}
	if strings.Join(got, " ") != "--user restart hopclaw.service" {
		t.Fatalf("systemctl args = %q", strings.Join(got, " "))
	}
}
