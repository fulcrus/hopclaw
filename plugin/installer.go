package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/ssrf"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// registryBaseURL is the default ClawHub registry endpoint.
	registryBaseURL = "https://registry.hopclaw.dev/v1"

	// downloadTimeout caps the time for fetching a plugin archive.
	downloadTimeout = 5 * time.Minute

	// registryTimeout caps the time for registry API calls.
	registryTimeout = 30 * time.Second

	// pluginsSubdir is the default subdirectory under the hopclaw home
	// for installed plugins.
	pluginsSubdir         = "plugins"
	disabledPluginsSubdir = ".disabled"

	// maxPluginDownloadBytes caps how much data the plugin installer is
	// willing to pull from a remote source. Lifts an unbounded io.Copy that
	// would otherwise let a malicious or runaway server fill local disk.
	maxPluginDownloadBytes = int64(512 * 1024 * 1024)
)

var errPluginDownloadTooLarge = fmt.Errorf("plugin download exceeds %d byte cap", maxPluginDownloadBytes)

// ---------------------------------------------------------------------------
// Installer
// ---------------------------------------------------------------------------

// Installer handles plugin installation from multiple sources.
type Installer struct {
	// PluginDir is the target directory for installed plugins.
	// Defaults to ~/.hopclaw/plugins.
	PluginDir string

	// RegistryURL overrides the default ClawHub registry URL.
	RegistryURL string

	// Manager is the plugin manager to register newly installed plugins.
	Manager *Manager

	// AllowPrivateDownloadHosts allows the installer to fetch from RFC1918,
	// loopback, or other private address ranges. Defaults to false; set true
	// when the plugin registry is self-hosted on an internal endpoint.
	AllowPrivateDownloadHosts bool
}

// validateDownloadURL refuses to follow obviously dangerous targets (non
// http(s) schemes, missing host, and private IPs unless explicitly allowed).
// It is a defense-in-depth check ahead of the actual HTTP request: callers
// can still expose the installer to internal hosts by opting in.
func (inst *Installer) validateDownloadURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return errors.New("url has no host")
	}
	if inst.AllowPrivateDownloadHosts {
		return nil
	}
	if ssrf.IsLoopbackHost(host) {
		return fmt.Errorf("refusing to download from loopback host %q", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if ssrf.IsPrivateIP(ip) {
			return fmt.Errorf("refusing to download from private ip %s", ip)
		}
	}
	return nil
}

// NewInstaller creates an installer with default paths.
func NewInstaller(manager *Manager) *Installer {
	home, _ := os.UserHomeDir()
	pluginDir := filepath.Join(home, ".hopclaw", pluginsSubdir)
	return &Installer{
		PluginDir:   pluginDir,
		RegistryURL: registryBaseURL,
		Manager:     manager,
	}
}

// InstallResult describes the outcome of an install operation.
type InstallResult struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Dir     string `json:"dir"`
	Source  string `json:"source"` // "registry", "github", "local"
}

// ---------------------------------------------------------------------------
// Install dispatches to the right method based on source format
// ---------------------------------------------------------------------------

// Install installs a plugin from a source string. The source can be:
//   - A short name: "mastodon" → fetched from the ClawHub registry
//   - A GitHub URL: "https://github.com/user/repo" → cloned
//   - A local path: "./my-plugin" or "/abs/path" → copied
func (inst *Installer) Install(ctx context.Context, source string) (*InstallResult, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, fmt.Errorf("plugin source is required")
	}

	switch {
	case strings.HasPrefix(source, "https://github.com/") || strings.HasPrefix(source, "git@"):
		return inst.installFromGitHub(ctx, source)
	case strings.HasPrefix(source, "/") || strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../"):
		return inst.installFromLocal(ctx, source)
	default:
		// Try registry first, fall back to GitHub org pattern.
		result, err := inst.installFromRegistry(ctx, source)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
}

// ---------------------------------------------------------------------------
// Method 1: Registry install (hopclaw plugins install mastodon)
// ---------------------------------------------------------------------------

// registryPluginInfo is the response from the registry lookup endpoint.
type registryPluginInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	DownloadURL string `json:"download_url"`
	Checksum    string `json:"checksum,omitempty"`
}

func (inst *Installer) installFromRegistry(ctx context.Context, name string) (*InstallResult, error) {
	regURL := inst.RegistryURL
	if regURL == "" {
		regURL = registryBaseURL
	}

	// Lookup plugin in registry.
	lookupURL := fmt.Sprintf("%s/plugins/%s?os=%s&arch=%s",
		strings.TrimRight(regURL, "/"), name, runtime.GOOS, runtime.GOARCH)

	reqCtx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, lookupURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create registry request: %w", err)
	}
	req.Header.Set("User-Agent", "hopclaw-installer/0.1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry lookup failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("plugin %q not found in registry", name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var info registryPluginInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode registry response: %w", err)
	}

	if strings.TrimSpace(info.DownloadURL) == "" {
		return nil, fmt.Errorf("registry did not return a download URL for %q", name)
	}

	// Download the plugin archive/binary.
	pluginDir := filepath.Join(inst.PluginDir, name)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return nil, fmt.Errorf("create plugin dir: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(pluginDir)
		}
	}()

	downloadedPath, err := inst.downloadFile(ctx, info.DownloadURL, pluginDir)
	if err != nil {
		return nil, fmt.Errorf("download plugin: %w", err)
	}
	if err := verifyDownloadedChecksum(downloadedPath, info.Checksum); err != nil {
		return nil, fmt.Errorf("verify plugin checksum: %w", err)
	}

	// Load and register the manifest.
	result, err := inst.loadAndRegister(pluginDir, "registry")
	if err != nil {
		return nil, err
	}
	cleanup = false
	result.Version = info.Version
	log.Info("plugin installed from registry", "name", result.Name, "version", result.Version)
	return result, nil
}

// ---------------------------------------------------------------------------
// Method 2: GitHub install (hopclaw plugins install https://github.com/user/repo)
// ---------------------------------------------------------------------------

func (inst *Installer) installFromGitHub(ctx context.Context, repoURL string) (*InstallResult, error) {
	// Derive a plugin name from the repo URL.
	name := filepath.Base(strings.TrimSuffix(repoURL, ".git"))
	name = strings.TrimPrefix(name, "hopclaw-plugin-")
	name = strings.TrimPrefix(name, "hopclaw-channel-")

	pluginDir := filepath.Join(inst.PluginDir, name)
	if err := os.MkdirAll(filepath.Dir(pluginDir), 0o755); err != nil {
		return nil, fmt.Errorf("create parent dir: %w", err)
	}

	// Clone or pull the repository.
	if _, err := os.Stat(filepath.Join(pluginDir, ".git")); err == nil {
		// Already cloned — pull latest.
		cmd := exec.CommandContext(ctx, "git", "-C", pluginDir, "pull", "--ff-only")
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("git pull: %w", err)
		}
	} else {
		cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", repoURL, pluginDir)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("git clone: %w", err)
		}
	}

	result, err := inst.loadAndRegister(pluginDir, "github")
	if err != nil {
		return nil, err
	}
	log.Info("plugin installed from github", "name", result.Name, "repo", repoURL)
	return result, nil
}

// ---------------------------------------------------------------------------
// Method 3: Local install (hopclaw plugins add ./my-plugin)
// ---------------------------------------------------------------------------

func (inst *Installer) installFromLocal(ctx context.Context, localPath string) (*InstallResult, error) {
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", absPath, err)
	}

	var pluginDir string

	if info.IsDir() {
		// If it's a directory, check for manifest in place.
		if _, err := os.Stat(filepath.Join(absPath, manifestFile)); err == nil {
			// Manifest exists — use the directory directly (symlink).
			name := filepath.Base(absPath)
			pluginDir = filepath.Join(inst.PluginDir, name)
			if err := os.MkdirAll(filepath.Dir(pluginDir), 0o755); err != nil {
				return nil, fmt.Errorf("create parent dir: %w", err)
			}
			// Remove existing symlink/dir if present.
			os.Remove(pluginDir)
			if err := os.Symlink(absPath, pluginDir); err != nil {
				return nil, fmt.Errorf("symlink plugin: %w", err)
			}
		} else {
			return nil, fmt.Errorf("no %s found in %s", manifestFile, absPath)
		}
	} else {
		// It's a single binary — create a directory and scaffold a manifest.
		name := strings.TrimSuffix(info.Name(), filepath.Ext(info.Name()))
		pluginDir = filepath.Join(inst.PluginDir, name)
		if err := os.MkdirAll(pluginDir, 0o755); err != nil {
			return nil, fmt.Errorf("create plugin dir: %w", err)
		}

		// Copy the binary.
		dstPath := filepath.Join(pluginDir, info.Name())
		if err := copyFile(absPath, dstPath); err != nil {
			return nil, fmt.Errorf("copy binary: %w", err)
		}
		if err := os.Chmod(dstPath, 0o755); err != nil {
			return nil, fmt.Errorf("chmod binary: %w", err)
		}

		// Generate a minimal manifest if none exists.
		manifestPath := filepath.Join(pluginDir, manifestFile)
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			manifest := fmt.Sprintf(`name: %s
version: "0.0.0"
description: "Locally installed channel plugin"

channels:
  %s:
    type: stdio
    command: "./%s"
`, name, name, info.Name())
			if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
				return nil, fmt.Errorf("write manifest: %w", err)
			}
		}
	}

	result, err := inst.loadAndRegister(pluginDir, "local")
	if err != nil {
		return nil, err
	}
	log.Info("plugin installed from local", "name", result.Name, "path", absPath)
	return result, nil
}

// ---------------------------------------------------------------------------
// Uninstall
// ---------------------------------------------------------------------------

// Uninstall removes an installed plugin by name.
func (inst *Installer) Uninstall(name string) error {
	pluginDir, _, err := inst.locatePluginDir(name)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(pluginDir); err != nil {
		return fmt.Errorf("remove plugin dir: %w", err)
	}
	if inst.Manager != nil {
		inst.Manager.Remove(name)
	}
	log.Info("plugin uninstalled", "name", name)
	return nil
}

// Enable moves a disabled plugin back into the active plugin directory.
func (inst *Installer) Enable(name string) error {
	pluginDir, enabled, err := inst.locatePluginDir(name)
	if err != nil {
		return err
	}
	if enabled {
		return nil
	}
	if err := os.MkdirAll(inst.PluginDir, 0o755); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}
	dest := filepath.Join(inst.PluginDir, filepath.Base(pluginDir))
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("plugin %q already enabled", name)
	}
	if err := os.Rename(pluginDir, dest); err != nil {
		return fmt.Errorf("enable plugin %q: %w", name, err)
	}
	log.Info("plugin enabled", "name", name)
	return nil
}

// Disable moves an enabled plugin into the hidden disabled directory.
func (inst *Installer) Disable(name string) error {
	pluginDir, enabled, err := inst.locatePluginDir(name)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	disabledRoot := inst.disabledDir()
	if err := os.MkdirAll(disabledRoot, 0o755); err != nil {
		return fmt.Errorf("create disabled plugin dir: %w", err)
	}
	dest := filepath.Join(disabledRoot, filepath.Base(pluginDir))
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("plugin %q already disabled", name)
	}
	if err := os.Rename(pluginDir, dest); err != nil {
		return fmt.Errorf("disable plugin %q: %w", name, err)
	}
	if inst.Manager != nil {
		inst.Manager.Remove(name)
	}
	log.Info("plugin disabled", "name", name)
	return nil
}

// ListInstalled returns enabled and disabled plugins discovered on disk.
func (inst *Installer) ListInstalled() (enabled []LoadedPlugin, disabled []LoadedPlugin) {
	enabled = Discover([]string{inst.PluginDir})
	disabled = Discover([]string{inst.disabledDir()})
	return enabled, disabled
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (inst *Installer) disabledDir() string {
	return filepath.Join(inst.PluginDir, disabledPluginsSubdir)
}

func (inst *Installer) locatePluginDir(name string) (string, bool, error) {
	name = strings.TrimSpace(name)
	for _, plugin := range Discover([]string{inst.PluginDir}) {
		if samePluginName(plugin, name) {
			return plugin.Dir, true, nil
		}
	}
	for _, plugin := range Discover([]string{inst.disabledDir()}) {
		if samePluginName(plugin, name) {
			return plugin.Dir, false, nil
		}
	}
	return "", false, fmt.Errorf("plugin %q not installed", name)
}

func samePluginName(plugin LoadedPlugin, name string) bool {
	if strings.EqualFold(strings.TrimSpace(plugin.Manifest.Name), name) {
		return true
	}
	return strings.EqualFold(filepath.Base(plugin.Dir), name)
}

func (inst *Installer) loadAndRegister(dir, source string) (*InstallResult, error) {
	p, err := loadManifest(dir)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}

	if inst.Manager != nil {
		// Ignore duplicate errors — re-install is fine.
		_ = inst.Manager.Register(p)
	}

	return &InstallResult{
		Name:    p.Manifest.Name,
		Version: p.Manifest.Version,
		Dir:     p.Dir,
		Source:  source,
	}, nil
}

func (inst *Installer) downloadFile(ctx context.Context, rawURL, destDir string) (string, error) {
	if err := inst.validateDownloadURL(rawURL); err != nil {
		return "", err
	}

	dlCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "hopclaw-installer/0.1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Pick a filename. Anything coming from the server may be hostile, so
	// constrain it to a single path component under destDir even if the
	// remote sends Content-Disposition: filename="../../etc/passwd".
	filename := filepath.Base(rawURL)
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if parts := strings.SplitN(cd, "filename=", 2); len(parts) == 2 {
			filename = strings.Trim(parts[1], `"' `)
		}
	}
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || filename == "." || filename == "/" || filename == string(os.PathSeparator) {
		return "", errors.New("download response did not provide a usable filename")
	}

	dstPath := filepath.Join(destDir, filename)
	f, err := os.Create(dstPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Cap the download. Reading maxPluginDownloadBytes+1 lets us distinguish
	// "exactly at cap" from "asked for more"; oversized payloads abort with a
	// clear error instead of silently filling local disk.
	written, err := io.Copy(f, io.LimitReader(resp.Body, maxPluginDownloadBytes+1))
	if err != nil {
		os.Remove(dstPath)
		return "", err
	}
	if written > maxPluginDownloadBytes {
		os.Remove(dstPath)
		return "", errPluginDownloadTooLarge
	}

	// Make executable if it looks like a binary.
	if !strings.HasSuffix(filename, ".tar.gz") && !strings.HasSuffix(filename, ".zip") {
		if err := os.Chmod(dstPath, 0o755); err != nil {
			return "", fmt.Errorf("chmod downloaded binary: %w", err)
		}
	}
	return dstPath, nil
}

func verifyDownloadedChecksum(path, expected string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	expected, err := normalizeSHA256Checksum(expected)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch")
	}
	return nil
}

func normalizeSHA256Checksum(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	lower := strings.ToLower(raw)
	for _, prefix := range []string{"sha256:", "sha256-", "sha256="} {
		if strings.HasPrefix(lower, prefix) {
			raw = strings.TrimSpace(raw[len(prefix):])
			break
		}
	}
	if len(raw) != sha256.Size*2 {
		return "", fmt.Errorf("invalid sha256 checksum %q", raw)
	}
	for _, ch := range raw {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return "", fmt.Errorf("invalid sha256 checksum %q", raw)
		}
	}
	return strings.ToLower(raw), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
