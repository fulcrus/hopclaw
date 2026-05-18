package skill

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ---------------------------------------------------------------------------
// Skill Dependency Auto-Installer
// ---------------------------------------------------------------------------

// InstallMethod defines how a dependency should be installed.
type InstallMethod string

const (
	InstallBrew     InstallMethod = "brew"
	InstallNPM      InstallMethod = "npm"
	InstallGo       InstallMethod = "go"
	InstallUV       InstallMethod = "uv"
	InstallPip      InstallMethod = "pip"
	InstallDownload InstallMethod = "download"
)

// SkillDependency declares a binary dependency for a skill.
type SkillDependency struct {
	Binary  string        `json:"binary" yaml:"binary"`
	Install InstallMethod `json:"install" yaml:"install"`
	Package string        `json:"package" yaml:"package"`
	Version string        `json:"version,omitempty" yaml:"version,omitempty"`
}

// AutoInstaller attempts to install missing skill dependencies.
type AutoInstaller struct {
	DryRun     bool
	InstallDir string
	HTTPClient *http.Client
	LookPath   func(string) (string, error)
}

// Install tries to install a missing dependency using the specified method.
// Returns the resolved binary path when one is available.
func (ai *AutoInstaller) Install(ctx context.Context, dep SkillDependency) (string, error) {
	lookPath := ai.lookPath()

	// Check if already installed.
	if dep.Binary != "" {
		if path, err := lookPath(dep.Binary); err == nil {
			return path, nil
		}
	}

	pkg := dep.Package
	if pkg == "" {
		pkg = dep.Binary
	}

	var cmd *exec.Cmd
	switch dep.Install {
	case InstallBrew:
		if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
			return "", fmt.Errorf("brew not available on %s", runtime.GOOS)
		}
		cmd = exec.CommandContext(ctx, "brew", "install", pkg)

	case InstallNPM:
		cmd = exec.CommandContext(ctx, "npm", "install", "-g", pkg)

	case InstallGo:
		installPath := pkg
		if dep.Version != "" {
			installPath = pkg + "@" + dep.Version
		} else {
			installPath = pkg + "@latest"
		}
		cmd = exec.CommandContext(ctx, "go", "install", installPath)

	case InstallUV:
		cmd = exec.CommandContext(ctx, "uv", "tool", "install", pkg)

	case InstallPip:
		cmd = exec.CommandContext(ctx, "pip", "install", pkg)

	case InstallDownload:
		installPath, err := ai.downloadTargetPath(dep, pkg)
		if err != nil {
			return "", err
		}
		if ai.DryRun {
			return installPath, nil
		}
		return ai.installDownload(ctx, pkg, installPath)

	default:
		return "", fmt.Errorf("unknown install method %q", dep.Install)
	}

	if ai.DryRun {
		return "", nil
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("install %q via %s: %s: %w", pkg, dep.Install, strings.TrimSpace(string(out)), err)
	}

	// Verify the binary is now available.
	if dep.Binary == "" {
		return "", nil
	}
	path, err := lookPath(dep.Binary)
	if err != nil {
		return "", fmt.Errorf("installed %q but binary %q not found in PATH", pkg, dep.Binary)
	}

	return path, nil
}

func (ai *AutoInstaller) lookPath() func(string) (string, error) {
	if ai != nil && ai.LookPath != nil {
		return ai.LookPath
	}
	return exec.LookPath
}

func (ai *AutoInstaller) httpClient() *http.Client {
	if ai != nil && ai.HTTPClient != nil {
		return ai.HTTPClient
	}
	return &http.Client{}
}

func (ai *AutoInstaller) installDownload(ctx context.Context, rawURL, installPath string) (string, error) {
	if err := validateSkillDownloadURL(rawURL); err != nil {
		return "", fmt.Errorf("download %q: %w", rawURL, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("download %q: %w", rawURL, err)
	}
	resp, err := ai.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("download %q: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download %q returned status %d", rawURL, resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(installPath), 0o755); err != nil {
		return "", fmt.Errorf("create install dir: %w", err)
	}

	tempPath := installPath + ".tmp"
	fileMode := os.FileMode(0o644)
	if runtime.GOOS != "windows" {
		fileMode = 0o755
	}
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode)
	if err != nil {
		return "", fmt.Errorf("create downloaded binary: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxSkillDownloadBytes+1))
	if copyErr != nil {
		file.Close()
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("write downloaded binary: %w", copyErr)
	}
	if written > maxSkillDownloadBytes {
		file.Close()
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("download %q: %w", rawURL, errSkillDownloadTooLarge)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("close downloaded binary: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tempPath, 0o755); err != nil {
			_ = os.Remove(tempPath)
			return "", fmt.Errorf("chmod downloaded binary: %w", err)
		}
	}
	if err := os.Rename(tempPath, installPath); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("finalize downloaded binary: %w", err)
	}
	return installPath, nil
}

func (ai *AutoInstaller) downloadTargetPath(dep SkillDependency, rawURL string) (string, error) {
	installDir, err := ai.installDir()
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(dep.Binary)
	if name == "" {
		name, err = targetFilename(rawURL)
		if err != nil {
			return "", err
		}
	}
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		if rawName, rawErr := targetFilename(rawURL); rawErr == nil {
			if ext := filepath.Ext(rawName); ext != "" {
				name += ext
			}
		}
	}
	return filepath.Join(installDir, name), nil
}

func (ai *AutoInstaller) installDir() (string, error) {
	if ai != nil && strings.TrimSpace(ai.InstallDir) != "" {
		return ai.InstallDir, nil
	}
	if envDir := strings.TrimSpace(os.Getenv("HOPCLAW_INSTALL_DIR")); envDir != "" {
		return envDir, nil
	}
	if runtime.GOOS == "windows" {
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			return filepath.Join(localAppData, "Programs", "HopClaw", "bin"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		if runtime.GOOS == "windows" {
			return filepath.Join(home, "AppData", "Local", "Programs", "HopClaw", "bin"), nil
		}
		return filepath.Join(home, ".local", "bin"), nil
	}
	if runtime.GOOS == "windows" {
		return filepath.Join("C:\\", "HopClaw", "bin"), nil
	}
	return filepath.Join(string(os.PathSeparator), "usr", "local", "bin"), nil
}
