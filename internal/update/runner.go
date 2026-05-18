package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/fulcrus/hopclaw/internal/version"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("update")

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	downloadBufferSize = 32 * 1024
	binaryPermissions  = 0o755
)

// RunOptions controls channel/version selection for updates.
type RunOptions struct {
	Policy        Policy
	Version       string
	RestartDaemon bool
}

// RunUpdate downloads the selected release binary, replaces the current
// executable, and optionally restarts the daemon if installed.
func RunUpdate(ctx context.Context, opts RunOptions) error {
	if version.Version == "dev" || version.Version == "" {
		return fmt.Errorf("cannot update a dev build; install from a release binary")
	}
	opts.Policy = normalizePolicy(opts.Policy)

	// Check for latest version.
	result, err := CheckWithPolicy(ctx, opts.Policy)
	if err != nil && strings.TrimSpace(opts.Version) == "" {
		return fmt.Errorf("check for updates: %w", err)
	}

	if strings.TrimSpace(opts.Version) == "" && result != nil && result.UpToDate {
		return fmt.Errorf("already up to date (%s)", result.CurrentVersion)
	}

	release, _, err := resolveLatestRelease(ctx, opts.Policy, strings.TrimSpace(opts.Version))
	if err != nil {
		return fmt.Errorf("fetch release info: %w", err)
	}

	asset, err := findAsset(release)
	if err != nil {
		return fmt.Errorf("no suitable binary found for %s/%s in release %s",
			runtime.GOOS, runtime.GOARCH, release.Version)
	}

	// Download to a temp file.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	tmpPath := exe + ".update"
	if err := downloadBinary(ctx, asset, tmpPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("download binary: %w", err)
	}

	// Replace the current binary: rename old → .bak, new → exe.
	bakPath := exe + ".bak"
	os.Remove(bakPath) // ignore error on missing backup
	if err := os.Rename(exe, bakPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("backup current binary: %w", err)
	}
	if err := os.Rename(tmpPath, exe); err != nil {
		// Try to restore the backup.
		logging.DebugIfErr(os.Rename(bakPath, exe), "restore backup binary failed")
		return fmt.Errorf("install new binary: %w", err)
	}
	os.Remove(bakPath) // clean up backup

	// Restart daemon if requested.
	if opts.RestartDaemon {
		mgr, err := daemon.NewServiceManager()
		if err == nil {
			status, err := mgr.Status()
			if err == nil && status.Installed && status.Running {
				logging.LogIfErr(context.Background(), mgr.Restart(), "restart service after update failed")
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Asset selection
// ---------------------------------------------------------------------------

func findAsset(release releaseInfo) (assetInfo, error) {
	os := runtime.GOOS
	arch := runtime.GOARCH

	// Map GOARCH to common naming conventions.
	archAliases := map[string][]string{
		"amd64": {"amd64", "x86_64", "x64"},
		"arm64": {"arm64", "aarch64"},
		"386":   {"386", "i386", "x86"},
	}

	aliases, ok := archAliases[arch]
	if !ok {
		aliases = []string{arch}
	}

	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		assetOS := strings.ToLower(strings.TrimSpace(asset.OS))
		if assetOS == "" && !strings.Contains(name, os) {
			continue
		}
		if assetOS != "" && assetOS != os {
			continue
		}
		for _, a := range aliases {
			assetArch := strings.ToLower(strings.TrimSpace(asset.Arch))
			if (assetArch == "" && strings.Contains(name, a)) || assetArch == a {
				return asset, nil
			}
		}
	}

	return assetInfo{}, fmt.Errorf("asset not found")
}

// ---------------------------------------------------------------------------
// Download
// ---------------------------------------------------------------------------

func downloadBinary(ctx context.Context, asset assetInfo, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", version.ProductName+"/"+version.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, binaryPermissions)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, downloadBufferSize)
	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)
	if _, err := io.CopyBuffer(writer, resp.Body, buf); err != nil {
		return err
	}
	if expected := strings.TrimSpace(asset.SHA256); expected != "" {
		actual := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actual, expected) {
			return fmt.Errorf("checksum mismatch for %s: got %s want %s", asset.Name, actual, expected)
		}
	}

	return nil
}
