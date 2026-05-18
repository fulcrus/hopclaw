package diagnostics

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/fulcrus/hopclaw/internal/version"
)

const (
	defaultUploadTimeout           = 15 * time.Second
	defaultCollectorMaxUploadBytes = 16 << 20 // 16 MiB
	defaultCollectorDirName        = "diagnostics/reports"
	installIDFileName              = "install_id"
	multipartFieldEnvelope         = "envelope"
	multipartFieldBundle           = "bundle"
)

// Envelope describes one bug-report or crash-report submission.
type Envelope struct {
	ReportID    string         `json:"report_id,omitempty"`
	InstallID   string         `json:"install_id,omitempty"`
	Product     string         `json:"product,omitempty"`
	Version     string         `json:"version,omitempty"`
	Channel     string         `json:"channel,omitempty"`
	GitCommit   string         `json:"git_commit,omitempty"`
	BuildDate   string         `json:"build_date,omitempty"`
	GoVersion   string         `json:"go_version,omitempty"`
	OS          string         `json:"os,omitempty"`
	Arch        string         `json:"arch,omitempty"`
	Source      string         `json:"source,omitempty"`
	Command     string         `json:"command,omitempty"`
	Error       string         `json:"error,omitempty"`
	StackTop    string         `json:"stack_top,omitempty"`
	GeneratedAt time.Time      `json:"generated_at,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// SubmitResult is returned by both the upload client and the collector.
type SubmitResult struct {
	OK        bool   `json:"ok"`
	ReportID  string `json:"report_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// StoredBundle captures the collector-side storage result.
type StoredBundle struct {
	ReportID     string `json:"report_id"`
	BundlePath   string `json:"bundle_path"`
	ManifestPath string `json:"manifest_path"`
	SHA256       string `json:"sha256"`
}

type storedManifest struct {
	ReportID         string    `json:"report_id"`
	OriginalFilename string    `json:"original_filename,omitempty"`
	BundleFile       string    `json:"bundle_file"`
	BundleSizeBytes  int64     `json:"bundle_size_bytes"`
	SHA256           string    `json:"sha256"`
	RemoteAddr       string    `json:"remote_addr,omitempty"`
	UserAgent        string    `json:"user_agent,omitempty"`
	RequestID        string    `json:"request_id,omitempty"`
	StoredAt         time.Time `json:"stored_at"`
	Envelope         Envelope  `json:"envelope"`
}

// EnsureInstallID returns a stable install identifier, creating one on first use.
func EnsureInstallID() (string, error) {
	path := filepath.Join(daemon.DataDir(), installIDFileName)
	if id, err := readInstallID(path); err == nil && id != "" {
		return id, nil
	}
	if err := os.MkdirAll(daemon.DataDir(), 0o755); err != nil {
		return "", fmt.Errorf("create diagnostics data dir: %w", err)
	}
	id, err := randomPrefixedID("inst", 12)
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	switch {
	case err == nil:
		if _, writeErr := f.WriteString(id + "\n"); writeErr != nil {
			_ = f.Close()
			return "", fmt.Errorf("write install id: %w", writeErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			return "", fmt.Errorf("close install id file: %w", closeErr)
		}
		return id, nil
	case errors.Is(err, os.ErrExist):
		return readInstallID(path)
	default:
		return "", fmt.Errorf("create install id: %w", err)
	}
}

// NewEnvelope returns a diagnostics envelope seeded with build metadata.
func NewEnvelope(source, command string) Envelope {
	env := Envelope{
		Product:     version.ProductName,
		Version:     version.Version,
		Channel:     version.Channel,
		GitCommit:   version.GitCommit,
		BuildDate:   version.BuildDate,
		GoVersion:   runtime.Version(),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Source:      strings.TrimSpace(source),
		Command:     strings.TrimSpace(command),
		GeneratedAt: time.Now().UTC(),
	}
	if id, err := EnsureInstallID(); err == nil {
		env.InstallID = id
	}
	if reportID, err := randomPrefixedID("rpt", 12); err == nil {
		env.ReportID = reportID
	}
	return env
}

// SubmitBundle uploads a locally generated ZIP bundle to a diagnostics collector.
func SubmitBundle(ctx context.Context, cfg config.DiagnosticsConfig, bundlePath string, env Envelope, urlOverride, tokenOverride string) (SubmitResult, error) {
	uploadURL := strings.TrimSpace(urlOverride)
	if uploadURL == "" {
		uploadURL = strings.TrimSpace(cfg.UploadURL)
	}
	if uploadURL == "" {
		return SubmitResult{}, errors.New("diagnostics upload_url is not configured")
	}

	env = fillEnvelopeDefaults(env)

	bundle, err := os.Open(bundlePath)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("open diagnostics bundle: %w", err)
	}
	defer bundle.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	envelopeJSON, err := json.Marshal(env)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("marshal diagnostics envelope: %w", err)
	}
	if err := writer.WriteField(multipartFieldEnvelope, string(envelopeJSON)); err != nil {
		return SubmitResult{}, fmt.Errorf("write diagnostics envelope: %w", err)
	}

	part, err := writer.CreateFormFile(multipartFieldBundle, filepath.Base(bundlePath))
	if err != nil {
		return SubmitResult{}, fmt.Errorf("create diagnostics bundle part: %w", err)
	}
	if _, err := io.Copy(part, bundle); err != nil {
		return SubmitResult{}, fmt.Errorf("copy diagnostics bundle: %w", err)
	}
	if err := writer.Close(); err != nil {
		return SubmitResult{}, fmt.Errorf("close diagnostics multipart body: %w", err)
	}

	timeout := uploadTimeout(cfg)
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &body)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("create diagnostics upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "HopClaw/"+version.Version+" diagnostics")

	token := strings.TrimSpace(tokenOverride)
	if token == "" {
		token = strings.TrimSpace(cfg.UploadToken)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("submit diagnostics bundle: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return SubmitResult{}, fmt.Errorf("read diagnostics upload response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SubmitResult{}, fmt.Errorf("diagnostics collector returned %d: %s", resp.StatusCode, truncateForError(respBody))
	}

	var result SubmitResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return SubmitResult{}, fmt.Errorf("decode diagnostics upload response: %w", err)
	}
	if !result.OK {
		return SubmitResult{}, errors.New("diagnostics collector returned ok=false")
	}
	if strings.TrimSpace(result.ReportID) == "" {
		result.ReportID = env.ReportID
	}
	return result, nil
}

// CollectorEnabled reports whether the gateway-side collector should accept uploads.
func CollectorEnabled(cfg config.DiagnosticsConfig) bool {
	return cfg.CollectorEnabled != nil && *cfg.CollectorEnabled
}

// CollectorDir resolves the storage location for incoming diagnostics bundles.
func CollectorDir(cfg config.DiagnosticsConfig) string {
	if dir := strings.TrimSpace(cfg.CollectorDir); dir != "" {
		return dir
	}
	return filepath.Join(daemon.StateDir(), filepath.FromSlash(defaultCollectorDirName))
}

// CollectorMaxUploadBytes resolves the request size limit for collector uploads.
func CollectorMaxUploadBytes(cfg config.DiagnosticsConfig) int64 {
	if cfg.CollectorMaxUploadBytes > 0 {
		return cfg.CollectorMaxUploadBytes
	}
	return defaultCollectorMaxUploadBytes
}

// StoreBundle persists a collected bundle and a JSON manifest sidecar.
func StoreBundle(cfg config.DiagnosticsConfig, env Envelope, filename string, content []byte, remoteAddr, userAgent, requestID string) (StoredBundle, error) {
	if len(content) == 0 {
		return StoredBundle{}, errors.New("diagnostics bundle is empty")
	}

	env = fillEnvelopeDefaults(env)
	dayDir := filepath.Join(CollectorDir(cfg), time.Now().UTC().Format("20060102"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return StoredBundle{}, fmt.Errorf("create diagnostics collector dir: %w", err)
	}

	ext := filepath.Ext(strings.TrimSpace(filename))
	if ext == "" {
		ext = ".zip"
	}
	bundleFile := env.ReportID + ext
	bundlePath := filepath.Join(dayDir, bundleFile)
	if err := os.WriteFile(bundlePath, content, 0o644); err != nil {
		return StoredBundle{}, fmt.Errorf("write diagnostics bundle: %w", err)
	}

	sum := sha256.Sum256(content)
	manifestPath := filepath.Join(dayDir, env.ReportID+".json")
	manifest := storedManifest{
		ReportID:         env.ReportID,
		OriginalFilename: filepath.Base(strings.TrimSpace(filename)),
		BundleFile:       bundleFile,
		BundleSizeBytes:  int64(len(content)),
		SHA256:           hex.EncodeToString(sum[:]),
		RemoteAddr:       strings.TrimSpace(remoteAddr),
		UserAgent:        strings.TrimSpace(userAgent),
		RequestID:        strings.TrimSpace(requestID),
		StoredAt:         time.Now().UTC(),
		Envelope:         env,
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return StoredBundle{}, fmt.Errorf("marshal diagnostics manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, manifestJSON, 0o644); err != nil {
		return StoredBundle{}, fmt.Errorf("write diagnostics manifest: %w", err)
	}

	return StoredBundle{
		ReportID:     env.ReportID,
		BundlePath:   bundlePath,
		ManifestPath: manifestPath,
		SHA256:       manifest.SHA256,
	}, nil
}

func uploadTimeout(cfg config.DiagnosticsConfig) time.Duration {
	if cfg.UploadTimeout > 0 {
		return cfg.UploadTimeout
	}
	return defaultUploadTimeout
}

func fillEnvelopeDefaults(env Envelope) Envelope {
	if strings.TrimSpace(env.ReportID) == "" {
		if reportID, err := randomPrefixedID("rpt", 12); err == nil {
			env.ReportID = reportID
		}
	}
	if strings.TrimSpace(env.InstallID) == "" {
		if installID, err := EnsureInstallID(); err == nil {
			env.InstallID = installID
		}
	}
	if strings.TrimSpace(env.Product) == "" {
		env.Product = version.ProductName
	}
	if strings.TrimSpace(env.Version) == "" {
		env.Version = version.Version
	}
	if strings.TrimSpace(env.Channel) == "" {
		env.Channel = version.Channel
	}
	if strings.TrimSpace(env.GitCommit) == "" {
		env.GitCommit = version.GitCommit
	}
	if strings.TrimSpace(env.BuildDate) == "" {
		env.BuildDate = version.BuildDate
	}
	if strings.TrimSpace(env.GoVersion) == "" {
		env.GoVersion = runtime.Version()
	}
	if strings.TrimSpace(env.OS) == "" {
		env.OS = runtime.GOOS
	}
	if strings.TrimSpace(env.Arch) == "" {
		env.Arch = runtime.GOARCH
	}
	if env.GeneratedAt.IsZero() {
		env.GeneratedAt = time.Now().UTC()
	}
	return env
}

func readInstallID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", errors.New("install id is empty")
	}
	return id, nil
}

func randomPrefixedID(prefix string, bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate diagnostics id: %w", err)
	}
	return strings.TrimSpace(prefix) + "-" + hex.EncodeToString(buf), nil
}

func truncateForError(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) <= 240 {
		return text
	}
	return text[:240] + "..."
}
