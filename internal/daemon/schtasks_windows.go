//go:build windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fulcrus/hopclaw/logging"
)

// ---------------------------------------------------------------------------
// Windows schtasks service manager
// ---------------------------------------------------------------------------

const schtasksTaskName = "HopClaw Gateway"

func newPlatformManager() (ServiceManager, error) {
	return &schtasksManager{}, nil
}

type schtasksManager struct{}

func (m *schtasksManager) Install(cfg ServiceConfig) error {
	if err := EnsureStateDir(); err != nil {
		return fmt.Errorf("schtasks: create state dir: %w", err)
	}

	// Create a scheduled task that runs at logon.
	args := []string{
		"/Create",
		"/TN", schtasksTaskName,
		"/TR", fmt.Sprintf(`"%s" serve --config "%s"`, cfg.BinaryPath, cfg.ConfigPath),
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/F", // force overwrite if exists
	}

	out, err := exec.Command("schtasks.exe", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks: create: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (m *schtasksManager) Uninstall() error {
	logging.DebugIfErr(m.Stop(), "stop windows scheduled task failed")

	out, err := exec.Command("schtasks.exe", "/Delete", "/TN", schtasksTaskName, "/F").CombinedOutput()
	if err != nil {
		output := string(out)
		if strings.Contains(output, "does not exist") ||
			strings.Contains(output, "cannot find") {
			return nil
		}
		return fmt.Errorf("schtasks: delete: %s: %w", strings.TrimSpace(output), err)
	}
	return nil
}

func (m *schtasksManager) Start() error {
	out, err := exec.Command("schtasks.exe", "/Run", "/TN", schtasksTaskName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks: run: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (m *schtasksManager) Stop() error {
	out, err := exec.Command("schtasks.exe", "/End", "/TN", schtasksTaskName).CombinedOutput()
	if err != nil {
		output := string(out)
		if strings.Contains(output, "not currently running") ||
			strings.Contains(output, "does not exist") {
			return nil
		}
		return fmt.Errorf("schtasks: end: %s: %w", strings.TrimSpace(output), err)
	}
	return nil
}

func (m *schtasksManager) Restart() error {
	if err := m.Stop(); err != nil {
		return err
	}
	return m.Start()
}

func (m *schtasksManager) Status() (*ServiceStatus, error) {
	status := &ServiceStatus{Label: schtasksTaskName}

	out, err := exec.Command("schtasks.exe", "/Query", "/TN", schtasksTaskName, "/FO", "LIST").CombinedOutput()
	if err != nil {
		return status, nil // task does not exist
	}

	output := string(out)
	status.Installed = true

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Status:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Status:"))
			status.Running = val == "Running"
		}
	}

	// schtasks doesn't expose PID directly; leave PID=0.
	// The user can find it via tasklist if needed.

	return status, nil
}

func init() {
	// Ensure HOME is set on Windows for path helpers.
	if os.Getenv("HOME") == "" {
		if up := os.Getenv("USERPROFILE"); up != "" {
			os.Setenv("HOME", up)
		}
	}
}
