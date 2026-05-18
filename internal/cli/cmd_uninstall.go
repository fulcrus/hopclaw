package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/daemon"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var executablePathForUninstall = os.Executable
var removeAllForUninstall = os.RemoveAll
var removeFileForUninstall = os.Remove

func newUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove HopClaw binaries, data, and system service",
		Long: `Uninstall HopClaw from this machine.

This command stops the daemon (if running), removes the system service,
deletes the data directory (~/.hopclaw), removes installed binaries, and
cleans up platform-specific caches.

Use --keep-data to preserve config and state files.`,
		RunE: runUninstall,
	}
	cmd.Flags().Bool("yes", false, "skip confirmation prompt")
	cmd.Flags().Bool("keep-data", false, "remove binaries and service but keep ~/.hopclaw data")
	return cmd
}

func runUninstall(cmd *cobra.Command, _ []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	keepData, _ := cmd.Flags().GetBool("keep-data")

	stateDir := daemon.StateDir()
	binaryPath, _ := executablePathForUninstall()
	if resolved, err := filepath.EvalSymlinks(binaryPath); err == nil {
		binaryPath = resolved
	}

	// Collect what will be removed.
	var targets []string
	targets = append(targets, fmt.Sprintf(itext("Binary: %s", "二进制：%s"), binaryPath))

	mgr, mgrErr := newServiceManager()
	if mgrErr == nil {
		if status, err := mgr.Status(); err == nil && status.Installed {
			targets = append(targets, itext("System service (daemon)", "后台服务 (daemon)"))
		}
	}

	if !keepData {
		targets = append(targets, fmt.Sprintf(itext("Data directory: %s", "数据目录：%s"), stateDir))
	}
	if _, err := os.Stat(backgroundGatewayPIDFilePath()); err == nil {
		targets = append(targets, itext("Temporary web-first gateway", "临时网页优先网关"))
	}

	cacheDir := platformCacheDir()
	if cacheDir != "" {
		if _, err := os.Stat(cacheDir); err == nil {
			targets = append(targets, fmt.Sprintf(itext("Cache: %s", "缓存：%s"), cacheDir))
		}
	}

	// Show what will be removed.
	fmt.Println(itext("The following will be removed:", "以下内容将被删除："))
	for _, t := range targets {
		fmt.Printf("  • %s\n", t)
	}
	if keepData {
		fmt.Printf("\n  %s\n", itext("(data directory preserved with --keep-data)", "（--keep-data 保留数据目录）"))
	}
	fmt.Println()

	// Check for active serve instances and require the user to stop them first.
	liveInstances := detectActiveServeInstances()
	if len(liveInstances) > 0 {
		fmt.Println(itext(
			"Active HopClaw serve instances detected:",
			"检测到正在运行的 HopClaw serve 实例：",
		))
		fmt.Println()
		for _, inst := range liveInstances {
			name := inst.Name
			if name == "" {
				name = inst.InstanceID
			}
			fmt.Printf("  • %s (PID %d, %s)\n", name, inst.PID, inst.BaseURL)
		}
		fmt.Println()
		fmt.Println(itext(
			"Please stop all serve instances before uninstalling:",
			"请先停止所有 serve 实例后再卸载：",
		))
		fmt.Println()
		for _, inst := range liveInstances {
			fmt.Printf("    kill %d\n", inst.PID)
		}
		fmt.Println()
		fmt.Println(itext(
			"Or if running as a daemon:",
			"如果是以后台服务运行：",
		))
		fmt.Println()
		fmt.Println("    hopclaw daemon stop")
		fmt.Println()
		fmt.Println(itext(
			"Then run 'hopclaw uninstall' again.",
			"然后重新运行 'hopclaw uninstall'。",
		))
		return nil
	}

	// Confirm.
	if !yes {
		var confirm bool
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(itext("Proceed with uninstall?", "确认卸载？")).
					Value(&confirm),
			),
		)
		if err := form.Run(); err != nil {
			return err
		}
		if !confirm {
			fmt.Println(itext("Cancelled.", "已取消。"))
			return nil
		}
	}

	// 1. Stop and uninstall daemon.
	if mgr != nil {
		if status, err := mgr.Status(); err == nil && status.Installed {
			fmt.Println(itext("Stopping and removing system service...", "正在停止并移除后台服务..."))
			pid := status.PID
			_ = mgr.Stop()
			if err := mgr.Uninstall(); err != nil {
				fmt.Printf(itext("  Warning: %v\n", "  警告：%v\n"), err)
			} else {
				fmt.Println(itext("  Service removed.", "  后台服务已移除。"))
			}
			// Wait for the process to exit after bootout.
			if pid > 0 {
				waitForProcessExit(pid, 5*time.Second)
			}
		}
	}
	if stopped, err := stopBackgroundGatewayProcess(5 * time.Second); err != nil {
		fmt.Printf(itext("  Warning: %v\n", "  警告：%v\n"), err)
	} else if stopped {
		fmt.Println(itext("  Temporary web-first gateway stopped.", "  临时网页优先网关已停止。"))
	}

	// 2. Remove data directory.
	if !keepData {
		if _, err := os.Stat(stateDir); err == nil {
			fmt.Printf(itext("Removing data directory %s...\n", "正在删除数据目录 %s...\n"), stateDir)
			if err := removeAllForUninstall(stateDir); err != nil {
				fmt.Printf(itext("  Warning: %v\n", "  警告：%v\n"), err)
			} else {
				fmt.Println(itext("  Data removed.", "  数据已删除。"))
			}
		}
	}

	// 3. Remove cache directory.
	if cacheDir != "" {
		if _, err := os.Stat(cacheDir); err == nil {
			fmt.Printf(itext("Removing cache %s...\n", "正在清除缓存 %s...\n"), cacheDir)
			if err := removeAllForUninstall(cacheDir); err != nil {
				fmt.Printf(itext("  Warning: %v\n", "  警告：%v\n"), err)
			}
		}
	}

	// 4. Remove binary (must be last — we're running from it).
	fmt.Printf(itext("Removing binary %s...\n", "正在删除二进制 %s...\n"), binaryPath)
	if err := removeFileForUninstall(binaryPath); err != nil {
		fmt.Printf(itext("  Warning: could not remove binary: %v\n", "  警告：无法删除二进制：%v\n"), err)
		fmt.Printf(itext("  Remove manually with: rm %s\n", "  请手动删除：rm %s\n"), binaryPath)
	} else {
		fmt.Println(itext("  Binary removed.", "  二进制已删除。"))
	}

	// Also try to remove companion binaries in the same directory.
	binDir := filepath.Dir(binaryPath)
	companions := []string{"openclaw", "hopclaw-browserd", "hopclaw-desktopd", "hopclaw-gateway"}
	for _, name := range companions {
		p := filepath.Join(binDir, name)
		if _, err := os.Stat(p); err == nil {
			_ = removeFileForUninstall(p)
		}
	}

	fmt.Println()
	fmt.Println(itext("HopClaw has been uninstalled.", "HopClaw 已卸载。"))
	return nil
}

// detectActiveServeInstances scans the local serve instance records and returns
// only live processes with fresh heartbeats.
func detectActiveServeInstances() []localServeInstanceRecord {
	entries, err := os.ReadDir(serveInstanceDir())
	if err != nil {
		return nil
	}

	live := make([]localServeInstanceRecord, 0, len(entries))
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		record, ok := loadLocalServeInstanceRecord(filepath.Join(serveInstanceDir(), entry.Name()))
		if !ok {
			continue
		}
		if now.Sub(record.LastHeartbeat) > serveInstanceStaleAfter {
			continue
		}
		if record.PID > 0 && processExists(record.PID) {
			live = append(live, record)
		}
	}
	return live
}

func waitForProcessExit(pid int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func stopBackgroundGatewayProcess(timeout time.Duration) (bool, error) {
	info, err := loadBackgroundGatewayPIDFile()
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read background gateway pid: %w", err)
	}
	if info.PID <= 0 {
		removeBackgroundGatewayPIDFile()
		return false, nil
	}
	if !processExists(info.PID) {
		removeBackgroundGatewayPIDFile()
		return false, nil
	}
	if err := terminateProcess(info.PID); err != nil {
		return false, fmt.Errorf("stop temporary web-first gateway pid %d: %w", info.PID, err)
	}
	waitForProcessExit(info.PID, timeout)
	if processExists(info.PID) {
		return false, fmt.Errorf("temporary web-first gateway pid %d did not exit within %s", info.PID, timeout)
	}
	removeBackgroundGatewayPIDFile()
	return true, nil
}

func platformCacheDir() string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Caches", "hopclaw")
	case "linux":
		if xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdg != "" {
			return filepath.Join(xdg, "hopclaw")
		}
		return filepath.Join(home, ".cache", "hopclaw")
	default:
		return ""
	}
}
