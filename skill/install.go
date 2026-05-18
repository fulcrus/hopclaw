package skill

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
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

	"github.com/fulcrus/hopclaw/internal/execenv"
	"github.com/fulcrus/hopclaw/internal/ssrf"
)

const (
	// maxSkillDownloadBytes caps how much data the skill installer is
	// willing to pull from a remote source. Prevents an oversized or
	// malicious archive from filling local disk during io.Copy.
	maxSkillDownloadBytes = int64(512 * 1024 * 1024)

	// maxSkillArchiveEntryBytes caps the uncompressed size of a single
	// file inside a skill archive (tar/zip). Stops decompression bombs.
	maxSkillArchiveEntryBytes = int64(256 * 1024 * 1024)
)

var errSkillDownloadTooLarge = fmt.Errorf("skill download exceeds %d byte cap", maxSkillDownloadBytes)
var errSkillArchiveEntryTooLarge = fmt.Errorf("skill archive entry exceeds %d byte cap", maxSkillArchiveEntryBytes)

// allowPrivateSkillDownloads is wired by the bootstrap layer when the
// operator runs against an internal skill server. By default the installer
// refuses to fetch from loopback or private IP space, blocking SSRF probes.
var allowPrivateSkillDownloads = false

// AllowPrivateDownloads enables fetching skill archives from RFC1918 or
// loopback addresses. Defaults to false so operator-driven installs can't
// be tricked into probing internal services.
func AllowPrivateDownloads(allow bool) { allowPrivateSkillDownloads = allow }

func validateSkillDownloadURL(rawURL string) error {
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
	if allowPrivateSkillDownloads {
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

type InstallExecutor struct {
	GOOS       string
	HTTPClient *http.Client
	LookPath   func(string) (string, error)
	RunCommand func(ctx context.Context, dir, name string, args []string, env []string) error
}

func DefaultInstallExecutor() InstallExecutor {
	return InstallExecutor{
		GOOS:       runtime.GOOS,
		HTTPClient: &http.Client{},
		LookPath:   exec.LookPath,
		RunCommand: runInstallCommand,
	}
}

func (s InstallSpec) ResolvedID(index int) string {
	id := strings.TrimSpace(s.ID)
	if id != "" {
		return id
	}
	kind := s.ResolvedKind()
	if kind == "" {
		kind = "install"
	}
	return fmt.Sprintf("%s-%d", kind, index)
}

func (s InstallSpec) ResolvedKind() string {
	if kind := strings.ToLower(strings.TrimSpace(s.Kind)); kind != "" {
		return kind
	}
	switch {
	case strings.TrimSpace(s.Script) != "":
		return "shell"
	case strings.TrimSpace(s.Formula) != "":
		return "brew"
	case strings.TrimSpace(s.Module) != "":
		return "go"
	case strings.TrimSpace(s.URL) != "":
		return "download"
	case strings.TrimSpace(s.Package) != "":
		return "node"
	default:
		return ""
	}
}

func (s InstallSpec) AppliesToOS(goos string) bool {
	if len(s.OS) == 0 {
		return true
	}
	for _, item := range s.OS {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(goos)) {
			return true
		}
	}
	return false
}

func (e InstallExecutor) Execute(ctx context.Context, rootDir string, specs []InstallSpec) ([]InstallStepResult, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	goos := e.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if e.LookPath == nil {
		e.LookPath = exec.LookPath
	}
	if e.RunCommand == nil {
		e.RunCommand = runInstallCommand
	}
	if e.HTTPClient == nil {
		e.HTTPClient = &http.Client{}
	}

	results := make([]InstallStepResult, 0, len(specs))
	for idx, spec := range specs {
		step := InstallStepResult{
			ID:    spec.ResolvedID(idx),
			Kind:  spec.ResolvedKind(),
			Label: strings.TrimSpace(spec.Label),
		}
		if step.Kind == "" {
			return results, fmt.Errorf("skill installer %q is missing a supported kind", step.ID)
		}
		if !spec.AppliesToOS(goos) {
			step.Status = InstallStepSkipped
			step.Reason = fmt.Sprintf("installer does not apply to %s", goos)
			results = append(results, step)
			continue
		}
		if len(spec.Bins) > 0 && allInstallBinsPresent(spec.Bins, e.LookPath) {
			step.Status = InstallStepSkipped
			step.Reason = "required binaries already available"
			results = append(results, step)
			continue
		}
		var err error
		switch step.Kind {
		case "shell":
			step.Command, err = commandForShellInstall(spec)
			if err == nil {
				err = e.RunCommand(ctx, rootDir, step.Command[0], step.Command[1:], envPairs(spec.Env))
			}
		case "brew":
			step.Command, err = commandForBrewInstall(spec)
			if err == nil {
				err = e.RunCommand(ctx, rootDir, step.Command[0], step.Command[1:], envPairs(spec.Env))
			}
		case "node":
			step.Command, err = commandForNodeInstall(spec, e.LookPath)
			if err == nil {
				err = e.RunCommand(ctx, rootDir, step.Command[0], step.Command[1:], envPairs(spec.Env))
			}
		case "go":
			step.Command, err = commandForGoInstall(spec)
			if err == nil {
				err = e.RunCommand(ctx, rootDir, step.Command[0], step.Command[1:], envPairs(spec.Env))
			}
		case "uv":
			step.Command, err = commandForUVInstall(spec)
			if err == nil {
				err = e.RunCommand(ctx, rootDir, step.Command[0], step.Command[1:], envPairs(spec.Env))
			}
		case "download":
			step.Path, err = e.executeDownload(ctx, rootDir, spec)
		default:
			err = fmt.Errorf("unsupported installer kind %q", step.Kind)
		}
		if err != nil {
			return results, fmt.Errorf("run installer %q: %w", step.ID, err)
		}
		step.Status = InstallStepRan
		results = append(results, step)
	}
	return results, nil
}

func allInstallBinsPresent(bins []string, lookPath func(string) (string, error)) bool {
	for _, bin := range bins {
		name := strings.TrimSpace(bin)
		if name == "" {
			continue
		}
		if _, err := lookPath(name); err != nil {
			return false
		}
	}
	return true
}

func commandForShellInstall(spec InstallSpec) ([]string, error) {
	script := strings.TrimSpace(spec.Script)
	if script == "" {
		return nil, fmt.Errorf("shell installer requires script")
	}
	shell := strings.TrimSpace(spec.Shell)
	if shell == "" {
		shell = "sh"
	}
	args := append([]string{"-c", script}, cleanInstallArgs(spec.Args)...)
	return append([]string{shell}, args...), nil
}

func commandForBrewInstall(spec InstallSpec) ([]string, error) {
	formula := strings.TrimSpace(spec.Formula)
	if formula == "" {
		return nil, fmt.Errorf("brew installer requires formula")
	}
	return append([]string{"brew", "install", formula}, cleanInstallArgs(spec.Args)...), nil
}

func commandForNodeInstall(spec InstallSpec, lookPath func(string) (string, error)) ([]string, error) {
	pkg := strings.TrimSpace(spec.Package)
	if pkg == "" {
		return nil, fmt.Errorf("node installer requires package")
	}
	candidates := [][]string{
		{"npm", "install", "-g", "--ignore-scripts", pkg},
		{"pnpm", "add", "-g", "--ignore-scripts", pkg},
		{"yarn", "global", "add", "--ignore-scripts", pkg},
		{"bun", "add", "-g", "--ignore-scripts", pkg},
	}
	for _, cmd := range candidates {
		if _, err := lookPath(cmd[0]); err == nil {
			return append(cmd, cleanInstallArgs(spec.Args)...), nil
		}
	}
	return nil, fmt.Errorf("node installer requires one of npm, pnpm, yarn, or bun")
}

func commandForGoInstall(spec InstallSpec) ([]string, error) {
	module := strings.TrimSpace(spec.Module)
	if module == "" {
		return nil, fmt.Errorf("go installer requires module")
	}
	return append([]string{"go", "install", module}, cleanInstallArgs(spec.Args)...), nil
}

func commandForUVInstall(spec InstallSpec) ([]string, error) {
	pkg := strings.TrimSpace(spec.Package)
	if pkg == "" {
		return nil, fmt.Errorf("uv installer requires package")
	}
	return append([]string{"uv", "tool", "install", pkg}, cleanInstallArgs(spec.Args)...), nil
}

func cleanInstallArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if trimmed := strings.TrimSpace(arg); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func envPairs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out = append(out, fmt.Sprintf("%s=%s", key, value))
	}
	return out
}

func runInstallCommand(ctx context.Context, dir, name string, args []string, env []string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmdEnv, err := buildInstallCommandEnv(env)
	if err != nil {
		return err
	}
	cmd.Env = cmdEnv
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, message)
	}
	return nil
}

func buildInstallCommandEnv(env []string) ([]string, error) {
	resolved, err := execenv.DefaultSecretResolver().ResolveMap(execenv.ParseEnvPairs(env))
	if err != nil {
		return nil, fmt.Errorf("resolve installer env: %w", err)
	}
	return execenv.BuildChildEnv(execenv.InstallerExecProfile, nil, resolved, nil, nil), nil
}

func (e InstallExecutor) executeDownload(ctx context.Context, rootDir string, spec InstallSpec) (string, error) {
	rawURL := strings.TrimSpace(spec.URL)
	if rawURL == "" {
		return "", fmt.Errorf("download installer requires url")
	}
	if err := validateSkillDownloadURL(rawURL); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Wrap the body in a limit reader so every downstream consumer (extract
	// archive, copy plain file) inherits the same maxSkillDownloadBytes cap.
	limited := io.LimitReader(resp.Body, maxSkillDownloadBytes+1)

	targetRoot := strings.TrimSpace(spec.TargetDir)
	if targetRoot == "" {
		targetRoot = filepath.Join(rootDir, ".skill-downloads", spec.ResolvedID(0))
	}
	if !filepath.IsAbs(targetRoot) {
		targetRoot = filepath.Join(rootDir, targetRoot)
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return "", err
	}

	extract := spec.Extract != nil && *spec.Extract
	if extract {
		archiveName := normalizedArchiveKind(spec)
		switch archiveName {
		case "zip":
			return targetRoot, extractZip(limited, targetRoot, spec.StripComponents)
		case "tar.gz", "tgz":
			return targetRoot, extractTarGz(limited, targetRoot, spec.StripComponents)
		default:
			return "", fmt.Errorf("unsupported archive format %q", archiveName)
		}
	}

	filename, err := targetFilename(rawURL)
	if err != nil {
		return "", err
	}
	targetPath := filepath.Join(targetRoot, filename)
	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer file.Close()
	written, err := io.Copy(file, limited)
	if err != nil {
		os.Remove(targetPath)
		return "", err
	}
	if written > maxSkillDownloadBytes {
		os.Remove(targetPath)
		return "", errSkillDownloadTooLarge
	}
	return targetPath, file.Close()
}

func normalizedArchiveKind(spec InstallSpec) string {
	archive := strings.ToLower(strings.TrimSpace(spec.Archive))
	if archive != "" {
		return archive
	}
	rawURL := strings.ToLower(strings.TrimSpace(spec.URL))
	switch {
	case strings.HasSuffix(rawURL, ".tar.gz"), strings.HasSuffix(rawURL, ".tgz"):
		return "tar.gz"
	case strings.HasSuffix(rawURL, ".zip"):
		return "zip"
	default:
		return ""
	}
}

func targetFilename(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	name := filepath.Base(parsed.Path)
	if strings.TrimSpace(name) == "" || name == "." || name == "/" {
		return "download.bin", nil
	}
	return name, nil
}

func extractZip(r io.Reader, targetDir string, stripComponents int) error {
	// Bound the in-memory read of the zip container. Without the cap a
	// malicious zip could exhaust process memory before we even inspect
	// the contents.
	data, err := io.ReadAll(io.LimitReader(r, maxSkillDownloadBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxSkillDownloadBytes {
		return errSkillDownloadTooLarge
	}
	reader := bytes.NewReader(data)
	zipReader, err := zip.NewReader(reader, int64(len(data)))
	if err != nil {
		return err
	}
	return extractZipReader(zipReader, targetDir, stripComponents)
}

func extractZipReader(reader *zip.Reader, targetDir string, stripComponents int) error {
	for _, file := range reader.File {
		trimmed := stripArchivePath(file.Name, stripComponents)
		if trimmed == "" {
			continue
		}
		target, err := safeArchiveTarget(targetDir, trimmed)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, file.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		// Cap how big a single extracted file can grow. Pairs with the
		// outer container size cap to neutralise classic zip bombs.
		written, copyErr := io.Copy(out, io.LimitReader(rc, maxSkillArchiveEntryBytes+1))
		if copyErr != nil {
			out.Close()
			rc.Close()
			os.Remove(target)
			return copyErr
		}
		if written > maxSkillArchiveEntryBytes {
			out.Close()
			rc.Close()
			os.Remove(target)
			return errSkillArchiveEntryTooLarge
		}
		if err := out.Close(); err != nil {
			rc.Close()
			return err
		}
		if err := rc.Close(); err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(r io.Reader, targetDir string, stripComponents int) error {
	gzipReader, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		trimmed := stripArchivePath(header.Name, stripComponents)
		if trimmed == "" {
			continue
		}
		target, err := safeArchiveTarget(targetDir, trimmed)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			// Cap per-entry size — the tar header's Size is attacker
			// controlled, so use the same limit as for zip entries.
			written, copyErr := io.Copy(out, io.LimitReader(tarReader, maxSkillArchiveEntryBytes+1))
			if copyErr != nil {
				out.Close()
				os.Remove(target)
				return copyErr
			}
			if written > maxSkillArchiveEntryBytes {
				out.Close()
				os.Remove(target)
				return errSkillArchiveEntryTooLarge
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
}

func stripArchivePath(path string, stripComponents int) string {
	path = strings.TrimPrefix(filepath.ToSlash(path), "./")
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	if stripComponents <= 0 {
		return filepath.FromSlash(strings.Join(parts, "/"))
	}
	if len(parts) <= stripComponents {
		return ""
	}
	return filepath.FromSlash(strings.Join(parts[stripComponents:], "/"))
}

func safeArchiveTarget(root, rel string) (string, error) {
	cleanRoot := filepath.Clean(root)
	target := filepath.Clean(filepath.Join(cleanRoot, rel))
	if target == cleanRoot {
		return target, nil
	}
	prefix := cleanRoot + string(os.PathSeparator)
	if !strings.HasPrefix(target, prefix) {
		return "", fmt.Errorf("archive entry escapes target dir: %q", rel)
	}
	return target, nil
}
