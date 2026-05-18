package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/fulcrus/hopclaw/internal/diagnostics"
	"github.com/fulcrus/hopclaw/internal/version"
	"github.com/fulcrus/hopclaw/logging"
)

const (
	defaultTimeout                 = 5 * time.Second
	defaultCollectorMaxUploadBytes = 4 << 20 // 4 MiB
	defaultCollectorDirName        = "diagnostics/telemetry"
	defaultMarkerLockTTL           = time.Minute
	envTelemetryEnabled            = "HOPCLAW_TELEMETRY_ENABLED"
	envTelemetryEndpoint           = "HOPCLAW_TELEMETRY_ENDPOINT"
	envTelemetryToken              = "HOPCLAW_TELEMETRY_TOKEN"
	envTelemetryTimeout            = "HOPCLAW_TELEMETRY_TIMEOUT"
	envTelemetryDebugLog           = "HOPCLAW_TELEMETRY_DEBUG_LOG"
)

var log = logging.WithSubsystem("telemetry")

type Event struct {
	ID         string         `json:"id,omitempty"`
	Event      string         `json:"event"`
	InstallID  string         `json:"install_id,omitempty"`
	Product    string         `json:"product,omitempty"`
	Version    string         `json:"version,omitempty"`
	Channel    string         `json:"channel,omitempty"`
	GitCommit  string         `json:"git_commit,omitempty"`
	BuildDate  string         `json:"build_date,omitempty"`
	GoVersion  string         `json:"go_version,omitempty"`
	OS         string         `json:"os,omitempty"`
	Arch       string         `json:"arch,omitempty"`
	OccurredAt time.Time      `json:"occurred_at,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

type Batch struct {
	Events []Event `json:"events"`
}

type SubmitResult struct {
	OK        bool   `json:"ok"`
	Accepted  int    `json:"accepted,omitempty"`
	BatchID   string `json:"batch_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

type StoredBatch struct {
	BatchID  string `json:"batch_id"`
	Path     string `json:"path"`
	Accepted int    `json:"accepted"`
}

type storedBatchFile struct {
	BatchID    string    `json:"batch_id"`
	Accepted   int       `json:"accepted"`
	RemoteAddr string    `json:"remote_addr,omitempty"`
	UserAgent  string    `json:"user_agent,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
	StoredAt   time.Time `json:"stored_at"`
	Batch      Batch     `json:"batch"`
}

type Client struct {
	cfg        config.DiagnosticsConfig
	httpClient *http.Client
	now        func() time.Time
	stateDir   string
}

type Option func(*Client)

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithNow(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

func WithStateDir(dir string) Option {
	return func(c *Client) {
		if strings.TrimSpace(dir) != "" {
			c.stateDir = strings.TrimSpace(dir)
		}
	}
}

func NewClient(cfg config.DiagnosticsConfig, opts ...Option) *Client {
	client := &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: Timeout(cfg)},
		now:        func() time.Time { return time.Now().UTC() },
		stateDir:   filepath.Join(daemon.DataDir(), "telemetry"),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client
}

func Enabled(cfg config.DiagnosticsConfig) bool {
	if cfg.TelemetryEnabled != nil {
		return *cfg.TelemetryEnabled
	}
	return parseTruthyEnv(envTelemetryEnabled)
}

func Endpoint(cfg config.DiagnosticsConfig) string {
	if value := strings.TrimSpace(cfg.TelemetryEndpoint); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(envTelemetryEndpoint))
}

func Token(cfg config.DiagnosticsConfig) string {
	if value := strings.TrimSpace(cfg.TelemetryToken); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(envTelemetryToken))
}

func Timeout(cfg config.DiagnosticsConfig) time.Duration {
	if cfg.TelemetryTimeout > 0 {
		return cfg.TelemetryTimeout
	}
	if value := strings.TrimSpace(os.Getenv(envTelemetryTimeout)); value != "" {
		if duration, err := time.ParseDuration(value); err == nil && duration > 0 {
			return duration
		}
	}
	return defaultTimeout
}

func DebugLogEnabled(cfg config.DiagnosticsConfig) bool {
	if cfg.TelemetryDebugLog != nil {
		return *cfg.TelemetryDebugLog
	}
	return parseTruthyEnv(envTelemetryDebugLog)
}

func DebugLog(cfg config.DiagnosticsConfig, message string, attrs ...any) {
	if !DebugLogEnabled(cfg) {
		return
	}
	log.Debug(message, attrs...)
}

func CollectorEnabled(cfg config.DiagnosticsConfig) bool {
	return cfg.TelemetryCollectorEnabled != nil && *cfg.TelemetryCollectorEnabled
}

func CollectorDir(cfg config.DiagnosticsConfig) string {
	if dir := strings.TrimSpace(cfg.TelemetryCollectorDir); dir != "" {
		return dir
	}
	return filepath.Join(daemon.StateDir(), filepath.FromSlash(defaultCollectorDirName))
}

func CollectorMaxUploadBytes(cfg config.DiagnosticsConfig) int64 {
	if cfg.TelemetryCollectorMaxUploadBytes > 0 {
		return cfg.TelemetryCollectorMaxUploadBytes
	}
	return defaultCollectorMaxUploadBytes
}

func (c *Client) Enabled() bool {
	if c == nil {
		return false
	}
	return Enabled(c.cfg)
}

func (c *Client) Track(ctx context.Context, event string, props map[string]any) error {
	if c == nil || !c.Enabled() {
		return nil
	}
	if strings.TrimSpace(Endpoint(c.cfg)) == "" {
		if DebugLogEnabled(c.cfg) {
			log.Debug("telemetry skipped: endpoint not configured", "event", strings.TrimSpace(event))
		}
		return nil
	}
	return c.submit(ctx, Batch{
		Events: []Event{c.newEvent(event, props)},
	})
}

func (c *Client) TrackOnce(ctx context.Context, key, event string, props map[string]any) error {
	if c == nil || !c.Enabled() {
		return nil
	}
	if strings.TrimSpace(Endpoint(c.cfg)) == "" {
		return nil
	}
	donePath := c.markerPath(key)
	claimed, lockPath, err := c.claimMarkerLock(donePath)
	if err != nil || !claimed {
		return err
	}
	defer os.Remove(lockPath)
	if err := c.Track(ctx, event, props); err != nil {
		return err
	}
	return os.WriteFile(donePath, []byte(c.now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

func (c *Client) TrackDaily(ctx context.Context, key, event string, props map[string]any) error {
	if c == nil {
		return nil
	}
	day := c.now().UTC().Format("2006-01-02")
	merged := cloneProperties(props)
	merged["active_day"] = day
	return c.TrackOnce(ctx, key+":"+day, event, merged)
}

func RecordInstall(ctx context.Context, cfg config.DiagnosticsConfig, activationSource, profile string) error {
	props := map[string]any{
		"activation_source": strings.TrimSpace(activationSource),
	}
	if trimmed := strings.TrimSpace(profile); trimmed != "" {
		props["profile"] = trimmed
	}
	return NewClient(cfg).TrackOnce(ctx, "install.completed", "install.completed", props)
}

func RecordOnboardCompleted(ctx context.Context, cfg config.DiagnosticsConfig, interactive bool, provider string, daemonInstalled bool, skillsSelected int, reusedExistingConfig bool) error {
	return NewClient(cfg).TrackOnce(ctx, "onboard.completed", "onboard.completed", map[string]any{
		"interactive":            interactive,
		"provider_selected":      strings.TrimSpace(provider),
		"daemon_installed":       daemonInstalled,
		"skills_selected_count":  skillsSelected,
		"reused_existing_config": reusedExistingConfig,
	})
}

func RecordRuntimeActive(ctx context.Context, cfg config.DiagnosticsConfig, profile, surface string) error {
	props := map[string]any{
		"surface": strings.TrimSpace(surface),
	}
	if trimmed := strings.TrimSpace(profile); trimmed != "" {
		props["profile"] = trimmed
	}
	return NewClient(cfg).TrackDaily(ctx, "runtime.active", "runtime.active", props)
}

func RecordPluginInstalled(ctx context.Context, cfg config.DiagnosticsConfig, name, version, source string) error {
	return NewClient(cfg).Track(ctx, "plugin.installed", map[string]any{
		"plugin_name":    strings.TrimSpace(name),
		"plugin_version": strings.TrimSpace(version),
		"source_kind":    strings.TrimSpace(source),
	})
}

func RecordSkillInstalled(ctx context.Context, cfg config.DiagnosticsConfig, skillID, version, source string) error {
	return NewClient(cfg).Track(ctx, "skill.installed", map[string]any{
		"skill_id":      strings.TrimSpace(skillID),
		"skill_version": strings.TrimSpace(version),
		"source_kind":   strings.TrimSpace(source),
	})
}

func SubmitEvents(ctx context.Context, cfg config.DiagnosticsConfig, batch Batch) (SubmitResult, error) {
	client := NewClient(cfg)
	if !client.Enabled() {
		return SubmitResult{}, nil
	}
	return client.submitWithResult(ctx, normalizeBatch(batch, true))
}

func StoreBatch(cfg config.DiagnosticsConfig, batch Batch, remoteAddr, userAgent, requestID string) (StoredBatch, error) {
	batch = normalizeBatch(batch, false)
	if len(batch.Events) == 0 {
		return StoredBatch{}, errors.New("telemetry batch is empty")
	}
	if err := os.MkdirAll(CollectorDir(cfg), 0o755); err != nil {
		return StoredBatch{}, fmt.Errorf("create telemetry collector dir: %w", err)
	}
	dayDir := filepath.Join(CollectorDir(cfg), time.Now().UTC().Format("20060102"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return StoredBatch{}, fmt.Errorf("create telemetry collector day dir: %w", err)
	}
	batchID, err := randomPrefixedID("tlm", 12)
	if err != nil {
		return StoredBatch{}, err
	}
	path := filepath.Join(dayDir, batchID+".json")
	payload := storedBatchFile{
		BatchID:    batchID,
		Accepted:   len(batch.Events),
		RemoteAddr: strings.TrimSpace(remoteAddr),
		UserAgent:  strings.TrimSpace(userAgent),
		RequestID:  strings.TrimSpace(requestID),
		StoredAt:   time.Now().UTC(),
		Batch:      batch,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return StoredBatch{}, fmt.Errorf("marshal telemetry batch: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return StoredBatch{}, fmt.Errorf("write telemetry batch: %w", err)
	}
	return StoredBatch{
		BatchID:  batchID,
		Path:     path,
		Accepted: len(batch.Events),
	}, nil
}

func (c *Client) submit(ctx context.Context, batch Batch) error {
	_, err := c.submitWithResult(ctx, normalizeBatch(batch, true))
	return err
}

func (c *Client) submitWithResult(ctx context.Context, batch Batch) (SubmitResult, error) {
	if c == nil || !c.Enabled() {
		return SubmitResult{}, nil
	}
	if len(batch.Events) == 0 {
		return SubmitResult{}, nil
	}
	endpoint := strings.TrimSpace(Endpoint(c.cfg))
	if endpoint == "" {
		if DebugLogEnabled(c.cfg) {
			log.Debug("telemetry skipped: endpoint not configured")
		}
		return SubmitResult{}, nil
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("marshal telemetry batch: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return SubmitResult{}, fmt.Errorf("create telemetry request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "HopClaw/"+version.Version+" telemetry")
	if token := strings.TrimSpace(Token(c.cfg)); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("submit telemetry batch: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return SubmitResult{}, fmt.Errorf("read telemetry response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SubmitResult{}, fmt.Errorf("telemetry collector returned %d: %s", resp.StatusCode, truncateForError(respBody))
	}
	if len(bytes.TrimSpace(respBody)) == 0 {
		return SubmitResult{OK: true, Accepted: len(batch.Events)}, nil
	}
	var result SubmitResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return SubmitResult{}, fmt.Errorf("decode telemetry response: %w", err)
	}
	if !result.OK {
		return SubmitResult{}, errors.New("telemetry collector returned ok=false")
	}
	if result.Accepted == 0 {
		result.Accepted = len(batch.Events)
	}
	if DebugLogEnabled(c.cfg) {
		log.Debug("telemetry batch submitted", "accepted", result.Accepted, "endpoint", endpoint)
	}
	return result, nil
}

func (c *Client) newEvent(name string, props map[string]any) Event {
	event := Event{
		Event:      strings.TrimSpace(name),
		Product:    version.ProductName,
		Version:    version.Version,
		Channel:    version.Channel,
		GitCommit:  version.GitCommit,
		BuildDate:  version.BuildDate,
		GoVersion:  runtime.Version(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		OccurredAt: c.now().UTC(),
		Properties: cloneProperties(props),
	}
	if id, err := randomPrefixedID("evt", 12); err == nil {
		event.ID = id
	}
	if installID, err := diagnostics.EnsureInstallID(); err == nil {
		event.InstallID = installID
	}
	return event
}

func (c *Client) markerPath(key string) string {
	return filepath.Join(c.stateDir, "markers", sanitizeKey(key)+".done")
}

func (c *Client) claimMarkerLock(donePath string) (bool, string, error) {
	if donePath == "" {
		return false, "", errors.New("telemetry marker path is empty")
	}
	if _, err := os.Stat(donePath); err == nil {
		return false, "", nil
	}
	lockPath := donePath + ".lock"
	if err := os.MkdirAll(filepath.Dir(donePath), 0o755); err != nil {
		return false, lockPath, fmt.Errorf("create telemetry marker dir: %w", err)
	}
	if info, err := os.Stat(lockPath); err == nil {
		if c.now().UTC().Sub(info.ModTime()) <= defaultMarkerLockTTL {
			return false, lockPath, nil
		}
		_ = os.Remove(lockPath)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	switch {
	case err == nil:
		if _, writeErr := f.WriteString(c.now().UTC().Format(time.RFC3339) + "\n"); writeErr != nil {
			_ = f.Close()
			_ = os.Remove(lockPath)
			return false, lockPath, fmt.Errorf("write telemetry marker lock: %w", writeErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			_ = os.Remove(lockPath)
			return false, lockPath, fmt.Errorf("close telemetry marker lock: %w", closeErr)
		}
		return true, lockPath, nil
	case errors.Is(err, os.ErrExist):
		return false, lockPath, nil
	default:
		return false, lockPath, fmt.Errorf("create telemetry marker lock: %w", err)
	}
}

func normalizeBatch(batch Batch, fillInstallID bool) Batch {
	out := Batch{Events: make([]Event, 0, len(batch.Events))}
	for _, event := range batch.Events {
		name := strings.TrimSpace(event.Event)
		if name == "" {
			continue
		}
		normalized := event
		normalized.Event = name
		if strings.TrimSpace(normalized.ID) == "" {
			if id, err := randomPrefixedID("evt", 12); err == nil {
				normalized.ID = id
			}
		}
		if normalized.OccurredAt.IsZero() {
			normalized.OccurredAt = time.Now().UTC()
		}
		if strings.TrimSpace(normalized.Product) == "" {
			normalized.Product = version.ProductName
		}
		if strings.TrimSpace(normalized.Version) == "" {
			normalized.Version = version.Version
		}
		if strings.TrimSpace(normalized.Channel) == "" {
			normalized.Channel = version.Channel
		}
		if strings.TrimSpace(normalized.GitCommit) == "" {
			normalized.GitCommit = version.GitCommit
		}
		if strings.TrimSpace(normalized.BuildDate) == "" {
			normalized.BuildDate = version.BuildDate
		}
		if strings.TrimSpace(normalized.GoVersion) == "" {
			normalized.GoVersion = runtime.Version()
		}
		if strings.TrimSpace(normalized.OS) == "" {
			normalized.OS = runtime.GOOS
		}
		if strings.TrimSpace(normalized.Arch) == "" {
			normalized.Arch = runtime.GOARCH
		}
		if fillInstallID && strings.TrimSpace(normalized.InstallID) == "" {
			if installID, err := diagnostics.EnsureInstallID(); err == nil {
				normalized.InstallID = installID
			}
		}
		normalized.Properties = cloneProperties(normalized.Properties)
		out.Events = append(out.Events, normalized)
	}
	return out
}

func cloneProperties(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		out[trimmed] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "event"
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func parseTruthyEnv(name string) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func randomPrefixedID(prefix string, bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate telemetry id: %w", err)
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
