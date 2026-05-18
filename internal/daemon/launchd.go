//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

// ---------------------------------------------------------------------------
// macOS launchd service manager
// ---------------------------------------------------------------------------

const (
	launchdLabel    = "com.hopclaw.gateway"
	plistFileName   = launchdLabel + ".plist"
	throttleSeconds = 1
)

var launchctlCombinedOutput = func(args ...string) ([]byte, error) {
	return runCombinedOutput("launchctl", args...)
}

func newPlatformManager() (ServiceManager, error) {
	return &launchdManager{}, nil
}

type launchdManager struct{}

func (m *launchdManager) Install(cfg ServiceConfig) error {
	if err := EnsureStateDir(); err != nil {
		return fmt.Errorf("launchd: create state dir: %w", err)
	}

	plist, err := buildPlist(cfg)
	if err != nil {
		return fmt.Errorf("launchd: build plist: %w", err)
	}

	path := plistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("launchd: create launch agents dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("launchd: write plist: %w", err)
	}

	// Bootstrap the agent so launchd knows about it.
	if out, err := launchctlCombinedOutput("bootstrap",
		fmt.Sprintf("gui/%d", os.Getuid()), path); err != nil {
		// If already bootstrapped, ignore the error.
		if !strings.Contains(string(out), "already bootstrapped") &&
			!strings.Contains(string(out), "service already loaded") {
			return fmt.Errorf("launchd: bootstrap: %s: %w", strings.TrimSpace(string(out)), err)
		}
	}

	return nil
}

func (m *launchdManager) Uninstall() error {
	path := plistPath()

	// Bootout the agent.
	domain := fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel)
	if out, err := launchctlCombinedOutput("bootout", domain); err != nil {
		// If not loaded, ignore the error.
		if !strings.Contains(string(out), "not find") &&
			!strings.Contains(string(out), "No such process") {
			return fmt.Errorf("launchd: bootout: %s: %w", strings.TrimSpace(string(out)), err)
		}
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("launchd: remove plist: %w", err)
	}
	return nil
}

func (m *launchdManager) Stop() error {
	// Bootout fully unloads the service from launchd, preventing KeepAlive
	// from restarting the process after a SIGTERM kill.
	domain := fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel)
	out, err := launchctlCombinedOutput("bootout", domain)
	if err != nil {
		output := string(out)
		if strings.Contains(output, "not find") ||
			strings.Contains(output, "No such process") {
			return nil // already stopped
		}
		return fmt.Errorf("launchd: stop: %s: %w", strings.TrimSpace(output), err)
	}
	return nil
}

func (m *launchdManager) Start() error {
	path := plistPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("launchd: plist not found at %s; run 'hopclaw daemon install' first", path)
	}

	// Bootstrap loads the service back into launchd after a Stop (bootout).
	target := fmt.Sprintf("gui/%d", os.Getuid())
	out, err := launchctlCombinedOutput("bootstrap", target, path)
	if err != nil {
		output := string(out)
		if strings.Contains(output, "already bootstrapped") ||
			strings.Contains(output, "service already loaded") {
			// Already loaded — kickstart to ensure it is running.
			domain := fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel)
			if out2, err2 := launchctlCombinedOutput("kickstart", "-k", domain); err2 != nil {
				return fmt.Errorf("launchd: kickstart: %s: %w", strings.TrimSpace(string(out2)), err2)
			}
			return nil
		}
		return fmt.Errorf("launchd: start: %s: %w", strings.TrimSpace(output), err)
	}
	return nil
}

func (m *launchdManager) Restart() error {
	// Stop fully unloads, Start re-bootstraps.
	_ = m.Stop() // ignore error — may already be stopped
	return m.Start()
}

func (m *launchdManager) Status() (*ServiceStatus, error) {
	status := &ServiceStatus{Label: launchdLabel}

	// Check if plist exists.
	if _, err := os.Stat(plistPath()); os.IsNotExist(err) {
		return status, nil
	}
	status.Installed = true

	// Query launchctl for service info.
	domain := fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel)
	out, err := launchctlCombinedOutput("print", domain)
	if err != nil {
		return status, nil // not running or not loaded
	}

	output := string(out)
	status.Running = !strings.Contains(output, "state = not running")

	// Try to extract PID.
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "pid = ") {
			pidStr := strings.TrimPrefix(line, "pid = ")
			if pid, err := strconv.Atoi(strings.TrimSpace(pidStr)); err == nil {
				status.PID = pid
			}
		}
	}

	return status, nil
}

// ---------------------------------------------------------------------------
// plist generation
// ---------------------------------------------------------------------------

var plistTemplate = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>

	<key>ProgramArguments</key>
	<array>
		<string>{{.BinaryPath}}</string>
		<string>serve</string>
		<string>--config</string>
		<string>{{.ConfigPath}}</string>
	</array>

	<key>RunAtLoad</key>
	<true/>

	<key>KeepAlive</key>
	<true/>

	<key>ThrottleInterval</key>
	<integer>{{.ThrottleInterval}}</integer>

	<key>StandardOutPath</key>
	<string>{{.StdoutPath}}</string>

	<key>StandardErrorPath</key>
	<string>{{.StderrPath}}</string>

	<key>WorkingDirectory</key>
	<string>{{.WorkDir}}</string>
</dict>
</plist>
`))

type plistData struct {
	Label            string
	BinaryPath       string
	ConfigPath       string
	ThrottleInterval int
	StdoutPath       string
	StderrPath       string
	WorkDir          string
}

func buildPlist(cfg ServiceConfig) (string, error) {
	logDir := cfg.LogPath
	if logDir == "" {
		logDir = LogDir()
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", fmt.Errorf("create log dir: %w", err)
	}

	data := plistData{
		Label:            launchdLabel,
		BinaryPath:       cfg.BinaryPath,
		ConfigPath:       cfg.ConfigPath,
		ThrottleInterval: throttleSeconds,
		StdoutPath:       filepath.Join(logDir, "hopclaw.stdout.log"),
		StderrPath:       filepath.Join(logDir, "hopclaw.stderr.log"),
		WorkDir:          homeDir(),
	}

	var buf strings.Builder
	if err := plistTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func plistPath() string {
	return filepath.Join(homeDir(), "Library", "LaunchAgents", plistFileName)
}
