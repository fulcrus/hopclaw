package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/spf13/cobra"
)

var newServiceManager = daemon.NewServiceManager
var resolveExecutableForService = resolveExecutable

// ---------------------------------------------------------------------------
// daemon command group
// ---------------------------------------------------------------------------

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the HopClaw system service",
	}

	cmd.AddCommand(
		newDaemonInstallCmd(),
		newDaemonUninstallCmd(),
		newDaemonStartCmd(),
		newDaemonStopCmd(),
		newDaemonRestartCmd(),
		newDaemonStatusCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// daemon install
// ---------------------------------------------------------------------------

func newDaemonInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install HopClaw as a system service",
		RunE:  runDaemonInstall,
	}
}

func runDaemonInstall(_ *cobra.Command, _ []string) error {
	mgr, err := newServiceManager()
	if err != nil {
		return err
	}

	binaryPath, err := resolveExecutableForService()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}

	configPath := resolveConfigPath()
	if configPath == "" {
		configPath = daemon.ConfigFilePath()
		// Ensure config exists.
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			return fmt.Errorf("no config file found at %s; run 'hopclaw setup' first", configPath)
		}
	}

	// Make configPath absolute.
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	cfg := daemon.ServiceConfig{
		BinaryPath: binaryPath,
		ConfigPath: absConfig,
		LogPath:    daemon.LogDir(),
	}

	if err := mgr.Install(cfg); err != nil {
		return fmt.Errorf("install service: %w", err)
	}

	fmt.Println("service installed successfully")
	fmt.Printf("  binary: %s\n", binaryPath)
	fmt.Printf("  config: %s\n", absConfig)
	fmt.Printf("  logs:   %s\n", daemon.LogDir())
	return nil
}

// ---------------------------------------------------------------------------
// daemon uninstall
// ---------------------------------------------------------------------------

func newDaemonUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the HopClaw system service",
		RunE:  runDaemonUninstall,
	}
}

func runDaemonUninstall(_ *cobra.Command, _ []string) error {
	mgr, err := newServiceManager()
	if err != nil {
		return err
	}
	if err := mgr.Uninstall(); err != nil {
		return fmt.Errorf("uninstall service: %w", err)
	}
	fmt.Println("service uninstalled successfully")
	return nil
}

// ---------------------------------------------------------------------------
// daemon start
// ---------------------------------------------------------------------------

func newDaemonStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the HopClaw system service",
		RunE:  runDaemonStart,
	}
}

func runDaemonStart(_ *cobra.Command, _ []string) error {
	mgr, err := newServiceManager()
	if err != nil {
		return err
	}
	if err := mgr.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	fmt.Println("service started")
	return nil
}

// ---------------------------------------------------------------------------
// daemon stop
// ---------------------------------------------------------------------------

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the HopClaw system service",
		RunE:  runDaemonStop,
	}
}

func runDaemonStop(_ *cobra.Command, _ []string) error {
	mgr, err := newServiceManager()
	if err != nil {
		return err
	}
	if err := mgr.Stop(); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	fmt.Println("service stopped")
	return nil
}

// ---------------------------------------------------------------------------
// daemon restart
// ---------------------------------------------------------------------------

func newDaemonRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the HopClaw system service",
		RunE:  runDaemonRestart,
	}
}

func runDaemonRestart(_ *cobra.Command, _ []string) error {
	mgr, err := newServiceManager()
	if err != nil {
		return err
	}
	if err := mgr.Restart(); err != nil {
		return fmt.Errorf("restart service: %w", err)
	}
	fmt.Println("service restarted")
	return nil
}

// ---------------------------------------------------------------------------
// daemon status
// ---------------------------------------------------------------------------

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the system service status",
		RunE:  runDaemonStatus,
	}
}

type daemonStatusOutput struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	PID       int    `json:"pid,omitempty"`
	Label     string `json:"label"`
}

func runDaemonStatus(_ *cobra.Command, _ []string) error {
	mgr, err := newServiceManager()
	if err != nil {
		return err
	}

	status, err := mgr.Status()
	if err != nil {
		return fmt.Errorf("query service status: %w", err)
	}

	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(daemonStatusOutput{
			Installed: status.Installed,
			Running:   status.Running,
			PID:       status.PID,
			Label:     status.Label,
		})
	}

	if !status.Installed {
		fmt.Println("Service: not installed")
		fmt.Println("Run 'hopclaw daemon install' to set up the system service.")
		return nil
	}

	runningText := "stopped"
	if status.Running {
		runningText = "running"
	}

	fmt.Printf("Service:   %s\n", status.Label)
	fmt.Printf("Status:    %s\n", runningText)
	if status.PID > 0 {
		fmt.Printf("PID:       %d\n", status.PID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func resolveExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	// Verify the resolved path actually exists.
	if _, err := exec.LookPath(exe); err != nil {
		return exe, nil // file exists but isn't in PATH — that's fine
	}
	return exe, nil
}
