//go:build linux

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("daemon")
var systemctlCombinedOutput = func(args ...string) ([]byte, error) {
	return runCombinedOutput("systemctl", args...)
}

// ---------------------------------------------------------------------------
// Linux systemd user service manager
// ---------------------------------------------------------------------------

const (
	systemdServiceName = "hopclaw.service"
	restartSeconds     = 5
)

func newPlatformManager() (ServiceManager, error) {
	return &systemdManager{}, nil
}

type systemdManager struct{}

func (m *systemdManager) Install(cfg ServiceConfig) error {
	if err := EnsureStateDir(); err != nil {
		return fmt.Errorf("systemd: create state dir: %w", err)
	}

	unit, err := buildUnit(cfg)
	if err != nil {
		return fmt.Errorf("systemd: build unit: %w", err)
	}

	dir := userUnitDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("systemd: create unit dir: %w", err)
	}

	path := filepath.Join(dir, systemdServiceName)
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("systemd: write unit: %w", err)
	}

	// Reload daemon and enable the service.
	if err := systemctl("daemon-reload"); err != nil {
		return fmt.Errorf("systemd: daemon-reload: %w", err)
	}
	if err := systemctl("enable", systemdServiceName); err != nil {
		return fmt.Errorf("systemd: enable: %w", err)
	}
	return nil
}

func (m *systemdManager) Uninstall() error {
	// Stop and disable first (ignore errors if not running).
	logging.DebugIfErr(systemctl("stop", systemdServiceName), "stop systemd service failed")
	logging.DebugIfErr(systemctl("disable", systemdServiceName), "disable systemd service failed")

	path := filepath.Join(userUnitDir(), systemdServiceName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("systemd: remove unit: %w", err)
	}

	return systemctl("daemon-reload")
}

func (m *systemdManager) Start() error {
	return systemctl("start", systemdServiceName)
}

func (m *systemdManager) Stop() error {
	return systemctl("stop", systemdServiceName)
}

func (m *systemdManager) Restart() error {
	return systemctl("restart", systemdServiceName)
}

func (m *systemdManager) Status() (*ServiceStatus, error) {
	status := &ServiceStatus{Label: systemdServiceName}

	// Check if unit file exists.
	path := filepath.Join(userUnitDir(), systemdServiceName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return status, nil
	}
	status.Installed = true

	// Query systemctl for active state.
	out, err := systemctlCombinedOutput("--user", "is-active", systemdServiceName)
	if err == nil && strings.TrimSpace(string(out)) == "active" {
		status.Running = true
	}

	// Try to get PID from show.
	pidOut, err := systemctlCombinedOutput("--user", "show", "--property=MainPID", systemdServiceName)
	if err == nil {
		line := strings.TrimSpace(string(pidOut))
		if strings.HasPrefix(line, "MainPID=") {
			pidStr := strings.TrimPrefix(line, "MainPID=")
			if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
				status.PID = pid
			}
		}
	}

	return status, nil
}

// ---------------------------------------------------------------------------
// unit file generation
// ---------------------------------------------------------------------------

var unitTemplate = template.Must(template.New("unit").Parse(`[Unit]
Description=HopClaw Gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} serve --config {{.ConfigPath}}
Restart=always
RestartSec={{.RestartSec}}
StandardOutput=append:{{.StdoutPath}}
StandardError=append:{{.StderrPath}}
WorkingDirectory={{.WorkDir}}

[Install]
WantedBy=default.target
`))

type unitData struct {
	BinaryPath string
	ConfigPath string
	RestartSec int
	StdoutPath string
	StderrPath string
	WorkDir    string
}

func buildUnit(cfg ServiceConfig) (string, error) {
	logDir := cfg.LogPath
	if logDir == "" {
		logDir = LogDir()
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", fmt.Errorf("create log dir: %w", err)
	}

	data := unitData{
		BinaryPath: cfg.BinaryPath,
		ConfigPath: cfg.ConfigPath,
		RestartSec: restartSeconds,
		StdoutPath: filepath.Join(logDir, "hopclaw.stdout.log"),
		StderrPath: filepath.Join(logDir, "hopclaw.stderr.log"),
		WorkDir:    homeDir(),
	}

	var buf strings.Builder
	if err := unitTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func userUnitDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "systemd", "user")
	}
	return filepath.Join(homeDir(), ".config", "systemd", "user")
}

func systemctl(args ...string) error {
	cmdArgs := append([]string{"--user"}, args...)
	out, err := systemctlCombinedOutput(cmdArgs...)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
