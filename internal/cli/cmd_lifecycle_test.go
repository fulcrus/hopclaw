package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/internal/daemon"
)

type fakeServiceManager struct {
	status          daemon.ServiceStatus
	installCalled   bool
	uninstallCalled bool
	startCalled     bool
	stopCalled      bool
	restartCalled   bool
	installConfig   daemon.ServiceConfig
}

func (f *fakeServiceManager) Install(cfg daemon.ServiceConfig) error {
	f.installCalled = true
	f.installConfig = cfg
	f.status.Installed = true
	return nil
}

func (f *fakeServiceManager) Uninstall() error {
	f.uninstallCalled = true
	f.status.Installed = false
	f.status.Running = false
	return nil
}

func (f *fakeServiceManager) Start() error {
	f.startCalled = true
	f.status.Running = true
	return nil
}

func (f *fakeServiceManager) Stop() error {
	f.stopCalled = true
	f.status.Running = false
	return nil
}

func (f *fakeServiceManager) Restart() error {
	f.restartCalled = true
	f.status.Running = true
	return nil
}

func (f *fakeServiceManager) Status() (*daemon.ServiceStatus, error) {
	out := f.status
	return &out, nil
}

func TestRunDaemonInstallUsesAbsoluteConfigAndResolvedBinary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  address: \"127.0.0.1:16280\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	oldMgr := newServiceManager
	oldResolve := resolveExecutableForService
	oldConfig := flagConfig
	defer func() {
		newServiceManager = oldMgr
		resolveExecutableForService = oldResolve
		flagConfig = oldConfig
	}()

	fake := &fakeServiceManager{}
	newServiceManager = func() (daemon.ServiceManager, error) { return fake, nil }
	resolveExecutableForService = func() (string, error) {
		return filepath.Join(dir, "bin", "hopclaw"), nil
	}
	flagConfig = cfgPath

	if err := runDaemonInstall(nil, nil); err != nil {
		t.Fatalf("runDaemonInstall: %v", err)
	}
	if !fake.installCalled {
		t.Fatal("expected service install to be called")
	}
	if fake.installConfig.BinaryPath != filepath.Join(dir, "bin", "hopclaw") {
		t.Fatalf("binary path = %q", fake.installConfig.BinaryPath)
	}
	if fake.installConfig.ConfigPath != cfgPath {
		t.Fatalf("config path = %q, want %q", fake.installConfig.ConfigPath, cfgPath)
	}
	if fake.installConfig.LogPath != daemon.LogDir() {
		t.Fatalf("log path = %q, want %q", fake.installConfig.LogPath, daemon.LogDir())
	}
}

func TestRunUninstallKeepDataStopsDaemonAndBackgroundGateway(t *testing.T) {
	runBackgroundGatewayHelperIfRequested()

	dir := t.TempDir()
	t.Setenv("HOME", dir)

	stateDir := daemon.StateDir()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	cacheDir := platformCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	binaryPath := filepath.Join(binDir, "hopclaw")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	bg := backgroundGatewayHelperCommand(t)
	if err := bg.Start(); err != nil {
		t.Fatalf("start background helper: %v", err)
	}
	t.Cleanup(func() {
		if bg.Process != nil {
			_ = bg.Process.Kill()
			_, _ = bg.Process.Wait()
		}
	})

	if err := writeBackgroundGatewayPIDFile(backgroundGatewayPIDInfo{
		PID:        bg.Process.Pid,
		BinaryPath: binaryPath,
		ConfigPath: filepath.Join(stateDir, "config.yaml"),
	}); err != nil {
		t.Fatalf("write background pid file: %v", err)
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

	fake := &fakeServiceManager{status: daemon.ServiceStatus{Installed: true, Running: true}}
	newServiceManager = func() (daemon.ServiceManager, error) { return fake, nil }
	executablePathForUninstall = func() (string, error) { return binaryPath, nil }
	removeAllForUninstall = os.RemoveAll
	removeFileForUninstall = os.Remove

	cmd := newUninstallCmd()
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatalf("set yes: %v", err)
	}
	if err := cmd.Flags().Set("keep-data", "true"); err != nil {
		t.Fatalf("set keep-data: %v", err)
	}

	if err := runUninstall(cmd, nil); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	if !fake.stopCalled {
		t.Fatal("expected daemon stop to be called")
	}
	if !fake.uninstallCalled {
		t.Fatal("expected daemon uninstall to be called")
	}
	if _, err := os.Stat(stateDir); err != nil {
		t.Fatalf("expected state dir to be preserved, stat err=%v", err)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("expected cache dir to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		t.Fatalf("expected binary to be removed, stat err=%v", err)
	}
	if processExists(bg.Process.Pid) {
		t.Fatalf("expected background gateway pid %d to be gone", bg.Process.Pid)
	}
	if _, err := os.Stat(backgroundGatewayPIDFilePath()); !os.IsNotExist(err) {
		t.Fatalf("expected background gateway pid file removed, stat err=%v", err)
	}
}

func TestRunUninstallRemovesStateDirWithoutKeepData(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	stateDir := daemon.StateDir()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	binaryPath := filepath.Join(dir, "hopclaw")
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o644); err != nil {
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

	fake := &fakeServiceManager{}
	newServiceManager = func() (daemon.ServiceManager, error) { return fake, nil }
	executablePathForUninstall = func() (string, error) { return binaryPath, nil }
	removeAllForUninstall = os.RemoveAll
	removeFileForUninstall = os.Remove

	cmd := newUninstallCmd()
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatalf("set yes: %v", err)
	}

	if err := runUninstall(cmd, nil); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("expected state dir removed, stat err=%v", err)
	}
	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		t.Fatalf("expected binary removed, stat err=%v", err)
	}
}
