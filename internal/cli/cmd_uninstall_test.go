package cli

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/internal/daemon"
)

func backgroundGatewayHelperCommand(t *testing.T) *exec.Cmd {
	t.Helper()
	runBackgroundGatewayHelperIfRequested()
	cmd := exec.Command(os.Args[0], "-test.run=TestStopBackgroundGatewayProcessTerminatesHelper")
	cmd.Env = append(os.Environ(), "GO_WANT_HOPCLAW_BACKGROUND_HELPER=1")
	return cmd
}

func runBackgroundGatewayHelperIfRequested() {
	if os.Getenv("GO_WANT_HOPCLAW_BACKGROUND_HELPER") == "1" {
		for {
			time.Sleep(250 * time.Millisecond)
		}
	}
}

func TestStopBackgroundGatewayProcessTerminatesHelper(t *testing.T) {
	runBackgroundGatewayHelperIfRequested()

	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cmd := backgroundGatewayHelperCommand(t)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	if err := writeBackgroundGatewayPIDFile(backgroundGatewayPIDInfo{
		PID:        cmd.Process.Pid,
		BinaryPath: os.Args[0],
		ConfigPath: "/tmp/test-config.yaml",
	}); err != nil {
		t.Fatalf("writeBackgroundGatewayPIDFile: %v", err)
	}

	stopped, err := stopBackgroundGatewayProcess(3 * time.Second)
	if err != nil {
		t.Fatalf("stopBackgroundGatewayProcess: %v", err)
	}
	if !stopped {
		t.Fatal("expected background gateway process to be stopped")
	}
	if processExists(cmd.Process.Pid) {
		t.Fatalf("expected helper pid %d to be gone", cmd.Process.Pid)
	}
	if _, err := os.Stat(backgroundGatewayPIDFilePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected pid file to be removed, stat err=%v", err)
	}
}

func TestStopBackgroundGatewayProcessRemovesStalePIDFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := writeBackgroundGatewayPIDFile(backgroundGatewayPIDInfo{PID: 999999}); err != nil {
		t.Fatalf("writeBackgroundGatewayPIDFile: %v", err)
	}

	stopped, err := stopBackgroundGatewayProcess(500 * time.Millisecond)
	if err != nil {
		t.Fatalf("stopBackgroundGatewayProcess: %v", err)
	}
	if stopped {
		t.Fatal("expected stale pid file to be treated as not running")
	}
	if _, err := os.Stat(backgroundGatewayPIDFilePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale pid file to be removed, stat err=%v", err)
	}
}

func TestUninstallBlocksOnActiveServeInstance(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv(installLangEnv, "en")

	stateDir := daemon.StateDir()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.MkdirAll(serveInstanceDir(), 0o755); err != nil {
		t.Fatalf("mkdir instances dir: %v", err)
	}

	record := localServeInstanceRecord{
		InstanceID:    "inst-active",
		Name:          "local-16280",
		BaseURL:       "http://127.0.0.1:16280",
		PID:           os.Getpid(),
		StartedAt:     time.Now().UTC(),
		LastHeartbeat: time.Now().UTC(),
	}
	if err := writeServeInstanceRecordForTest(filepath.Join(serveInstanceDir(), "inst-active.json"), record); err != nil {
		t.Fatalf("write serve instance record: %v", err)
	}

	binaryPath := filepath.Join(dir, "hopclaw")
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	oldMgr := newServiceManager
	oldExe := executablePathForUninstall
	oldRemoveAll := removeAllForUninstall
	oldRemoveFile := removeFileForUninstall
	defer func() {
		newServiceManager = oldMgr
		executablePathForUninstall = oldExe
		removeAllForUninstall = oldRemoveAll
		removeFileForUninstall = oldRemoveFile
	}()

	newServiceManager = func() (daemon.ServiceManager, error) { return &fakeServiceManager{}, nil }
	executablePathForUninstall = func() (string, error) { return binaryPath, nil }
	removeAllForUninstall = os.RemoveAll
	removeFileForUninstall = os.Remove

	cmd := newUninstallCmd()
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatalf("set yes: %v", err)
	}

	restore := captureStdout(t)
	if err := runUninstall(cmd, nil); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	output := restore()

	if _, err := os.Stat(stateDir); err != nil {
		t.Fatalf("expected state dir to be preserved, stat err=%v", err)
	}
	if _, err := os.Stat(binaryPath); err != nil {
		t.Fatalf("expected binary to be preserved, stat err=%v", err)
	}
	if !strings.Contains(output, "Active HopClaw serve instances detected:") {
		t.Fatalf("expected active instance warning in output, got %q", output)
	}
	if !strings.Contains(output, "kill "+strconv.Itoa(record.PID)) {
		t.Fatalf("expected kill command in output, got %q", output)
	}
	if strings.Contains(output, "HopClaw has been uninstalled.") {
		t.Fatalf("expected uninstall to be blocked, got %q", output)
	}
}

func TestUninstallProceedsWhenNoActiveInstances(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv(installLangEnv, "en")

	stateDir := daemon.StateDir()
	if err := os.MkdirAll(filepath.Join(stateDir, "instances"), 0o755); err != nil {
		t.Fatalf("mkdir instances dir: %v", err)
	}
	binaryPath := filepath.Join(dir, "hopclaw")
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	oldMgr := newServiceManager
	oldExe := executablePathForUninstall
	oldRemoveAll := removeAllForUninstall
	oldRemoveFile := removeFileForUninstall
	defer func() {
		newServiceManager = oldMgr
		executablePathForUninstall = oldExe
		removeAllForUninstall = oldRemoveAll
		removeFileForUninstall = oldRemoveFile
	}()

	newServiceManager = func() (daemon.ServiceManager, error) { return &fakeServiceManager{}, nil }
	executablePathForUninstall = func() (string, error) { return binaryPath, nil }
	removeAllForUninstall = os.RemoveAll
	removeFileForUninstall = os.Remove

	cmd := newUninstallCmd()
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatalf("set yes: %v", err)
	}

	restore := captureStdout(t)
	if err := runUninstall(cmd, nil); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	output := restore()

	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("expected state dir removed, stat err=%v", err)
	}
	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		t.Fatalf("expected binary removed, stat err=%v", err)
	}
	if strings.Contains(output, "Active HopClaw serve instances detected:") {
		t.Fatalf("expected no active instance warning, got %q", output)
	}
	if !strings.Contains(output, "HopClaw has been uninstalled.") {
		t.Fatalf("expected uninstall success output, got %q", output)
	}
}

func TestUninstallIgnoresStaleInstances(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv(installLangEnv, "en")

	stateDir := daemon.StateDir()
	if err := os.MkdirAll(serveInstanceDir(), 0o755); err != nil {
		t.Fatalf("mkdir instances dir: %v", err)
	}

	record := localServeInstanceRecord{
		InstanceID:    "inst-stale",
		Name:          "local-16280",
		BaseURL:       "http://127.0.0.1:16280",
		PID:           999999,
		StartedAt:     time.Now().UTC(),
		LastHeartbeat: time.Now().UTC(),
	}
	if err := writeServeInstanceRecordForTest(filepath.Join(serveInstanceDir(), "inst-stale.json"), record); err != nil {
		t.Fatalf("write stale serve instance record: %v", err)
	}

	binaryPath := filepath.Join(dir, "hopclaw")
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	oldMgr := newServiceManager
	oldExe := executablePathForUninstall
	oldRemoveAll := removeAllForUninstall
	oldRemoveFile := removeFileForUninstall
	defer func() {
		newServiceManager = oldMgr
		executablePathForUninstall = oldExe
		removeAllForUninstall = oldRemoveAll
		removeFileForUninstall = oldRemoveFile
	}()

	newServiceManager = func() (daemon.ServiceManager, error) { return &fakeServiceManager{}, nil }
	executablePathForUninstall = func() (string, error) { return binaryPath, nil }
	removeAllForUninstall = os.RemoveAll
	removeFileForUninstall = os.Remove

	cmd := newUninstallCmd()
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatalf("set yes: %v", err)
	}

	restore := captureStdout(t)
	if err := runUninstall(cmd, nil); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	output := restore()

	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("expected state dir removed, stat err=%v", err)
	}
	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		t.Fatalf("expected binary removed, stat err=%v", err)
	}
	if strings.Contains(output, "Active HopClaw serve instances detected:") {
		t.Fatalf("expected stale instance to be ignored, got %q", output)
	}
	if !strings.Contains(output, "HopClaw has been uninstalled.") {
		t.Fatalf("expected uninstall success output, got %q", output)
	}
}

func writeServeInstanceRecordForTest(path string, record localServeInstanceRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
