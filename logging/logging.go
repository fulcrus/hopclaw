package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultLevel  = "info"
	defaultFormat = "text"
	defaultOutput = "stderr"

	defaultMaxSizeMB = 100

	// redactedPlaceholder replaces sensitive values in log output.
	redactedPlaceholder = "[REDACTED]"

	// backupSuffix is appended to the log file name during rotation.
	backupSuffix = ".1"

	// subsystemAttrKey is the attribute key used to tag log entries with a
	// subsystem name. The subsystem-level handler uses this to look up
	// per-subsystem log levels.
	subsystemAttrKey = "subsystem"
)

// defaultRedactKeys is used when LogConfig.RedactKeys is empty.
var defaultRedactKeys = []string{
	"api_key",
	"apikey",
	"token",
	"secret",
	"password",
	"authorization",
}

// ---------------------------------------------------------------------------
// LogConfig
// ---------------------------------------------------------------------------

// LogConfig is the configuration for the logging system.
type LogConfig struct {
	Level           string            `json:"level" yaml:"level"`                       // debug, info, warn, error (default: info)
	Format          string            `json:"format" yaml:"format"`                     // json or text (default: text)
	Output          string            `json:"output" yaml:"output"`                     // stderr, stdout, file, both (default: stderr)
	FilePath        string            `json:"file_path" yaml:"file_path"`               // log file path (when output is file/both)
	MaxSizeMB       int               `json:"max_size_mb" yaml:"max_size_mb"`           // max log file size before rotation (default: 100)
	RedactKeys      []string          `json:"redact_keys" yaml:"redact_keys"`           // keys to redact (e.g. ["api_key", "token", "secret", "password"])
	SubsystemLevels map[string]string `json:"subsystem_levels" yaml:"subsystem_levels"` // per-subsystem log levels (e.g. {"agent": "debug", "model": "warn"})
	ConsoleCapture  bool              `json:"console_capture" yaml:"console_capture"`   // capture stdout/stderr as log entries
	Sampling        SamplingConfig    `json:"sampling" yaml:"sampling"`                 // sampling config for rate-limiting repetitive entries
}

// ---------------------------------------------------------------------------
// Package-level state
// ---------------------------------------------------------------------------

var (
	mu             sync.Mutex // guards logFile, globalLevel, subsystemLevels, consoleCapture
	logFile        *os.File
	globalLevel    slog.LevelVar
	subsystemLevel subsystemLevelMap
	consoleCapture *ConsoleCapture
)

// subsystemLevelMap stores per-subsystem log levels with thread-safe access.
type subsystemLevelMap struct {
	mu     sync.RWMutex
	levels map[string]slog.Level
}

func (m *subsystemLevelMap) get(subsystem string) (slog.Level, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	level, ok := m.levels[subsystem]
	return level, ok
}

func (m *subsystemLevelMap) set(subsystem string, level slog.Level) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.levels == nil {
		m.levels = make(map[string]slog.Level)
	}
	m.levels[subsystem] = level
}

func (m *subsystemLevelMap) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.levels = make(map[string]slog.Level)
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

// Init configures the global slog logger based on the provided config.
func Init(cfg LogConfig) error {
	mu.Lock()
	defer mu.Unlock()

	level, err := parseLevel(cfg.Level)
	if err != nil {
		return err
	}

	// Set global dynamic level.
	globalLevel.Set(level)

	// Parse and store subsystem levels.
	subsystemLevel.reset()
	for sub, raw := range cfg.SubsystemLevels {
		lvl, parseErr := parseLevel(raw)
		if parseErr != nil {
			return fmt.Errorf("subsystem %q: %w", sub, parseErr)
		}
		subsystemLevel.set(sub, lvl)
	}

	writer, err := buildWriter(cfg)
	if err != nil {
		return err
	}

	// Use the lowest possible level for the inner handler so that our custom
	// handlers (subsystem level, sampling, redact) control filtering.
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}

	format := strings.TrimSpace(strings.ToLower(cfg.Format))
	if format == "" {
		format = defaultFormat
	}

	var inner slog.Handler
	switch format {
	case "json":
		inner = slog.NewJSONHandler(writer, opts)
	case "text":
		inner = slog.NewTextHandler(writer, opts)
	default:
		return fmt.Errorf("unsupported log format %q", format)
	}

	// Build handler chain: inner -> sampling -> redact -> subsystem level filter.
	// The outermost handler is what slog calls first.

	// Layer 1: sampling (optional).
	handler := inner
	if cfg.Sampling.Enabled {
		handler = newSamplingHandler(handler, cfg.Sampling)
	}

	// Layer 2: redaction.
	redactKeys := cfg.RedactKeys
	if len(redactKeys) == 0 {
		redactKeys = defaultRedactKeys
	}
	handler = newRedactHandler(handler, redactKeys)

	// Layer 3: subsystem-aware level filtering (outermost).
	handler = newSubsystemLevelHandler(handler, &globalLevel, &subsystemLevel)

	slog.SetDefault(slog.New(handler))

	// Start console capture if configured.
	if cfg.ConsoleCapture {
		cc := NewConsoleCapture(slog.Default())
		if startErr := cc.Start(); startErr != nil {
			return fmt.Errorf("failed to start console capture: %w", startErr)
		}
		consoleCapture = cc
	}

	return nil
}

// ---------------------------------------------------------------------------
// WithSubsystem
// ---------------------------------------------------------------------------

// WithSubsystem returns a slog.Logger tagged with the given subsystem name.
func WithSubsystem(subsystem string) *slog.Logger {
	return slog.Default().With(subsystemAttrKey, subsystem)
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

// Close flushes and closes any open log files and stops console capture.
// Call on shutdown.
func Close() error {
	mu.Lock()
	defer mu.Unlock()

	if consoleCapture != nil {
		_ = consoleCapture.Stop()
		consoleCapture = nil
	}

	if logFile != nil {
		err := logFile.Close()
		logFile = nil
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Dynamic log level
// ---------------------------------------------------------------------------

// SetLevel changes the global log level at runtime. The change takes effect
// immediately for all loggers using the global handler.
func SetLevel(level string) error {
	lvl, err := parseLevel(level)
	if err != nil {
		return err
	}
	globalLevel.Set(lvl)
	return nil
}

// GetLevel returns the current global log level as a lowercase string.
func GetLevel() string {
	return levelToString(globalLevel.Level())
}

// SetSubsystemLevel sets or updates the log level for a specific subsystem.
// Pass an empty level string to remove the subsystem override.
func SetSubsystemLevel(subsystem, level string) error {
	if strings.TrimSpace(level) == "" {
		// Remove override — handled by deleting from the map.
		subsystemLevel.mu.Lock()
		delete(subsystemLevel.levels, subsystem)
		subsystemLevel.mu.Unlock()
		return nil
	}
	lvl, err := parseLevel(level)
	if err != nil {
		return err
	}
	subsystemLevel.set(subsystem, lvl)
	return nil
}

// GetSubsystemLevel returns the current log level for a subsystem. If no
// subsystem-specific level is set, the global level is returned.
func GetSubsystemLevel(subsystem string) string {
	if lvl, ok := subsystemLevel.get(subsystem); ok {
		return levelToString(lvl)
	}
	return GetLevel()
}

// ---------------------------------------------------------------------------
// Level parsing and formatting
// ---------------------------------------------------------------------------

func parseLevel(raw string) (slog.Level, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", defaultLevel:
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unsupported log level %q", raw)
	}
}

func levelToString(level slog.Level) string {
	switch level {
	case slog.LevelDebug:
		return "debug"
	case slog.LevelInfo:
		return "info"
	case slog.LevelWarn:
		return "warn"
	case slog.LevelError:
		return "error"
	default:
		return "info"
	}
}

// ---------------------------------------------------------------------------
// Writer construction
// ---------------------------------------------------------------------------

func buildWriter(cfg LogConfig) (io.Writer, error) {
	output := strings.TrimSpace(strings.ToLower(cfg.Output))
	if output == "" {
		output = defaultOutput
	}

	switch output {
	case "stderr":
		return os.Stderr, nil

	case "stdout":
		return os.Stdout, nil

	case "file":
		f, err := openLogFile(cfg)
		if err != nil {
			return nil, err
		}
		return newRotatingWriter(f, cfg), nil

	case "both":
		f, err := openLogFile(cfg)
		if err != nil {
			return nil, err
		}
		rw := newRotatingWriter(f, cfg)
		return io.MultiWriter(os.Stderr, rw), nil

	default:
		return nil, fmt.Errorf("unsupported log output %q", output)
	}
}

func openLogFile(cfg LogConfig) (*os.File, error) {
	if strings.TrimSpace(cfg.FilePath) == "" {
		return nil, fmt.Errorf("file_path is required when output is %q", cfg.Output)
	}

	// Close any previously opened log file.
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}

	f, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %q: %w", cfg.FilePath, err)
	}
	logFile = f
	return f, nil
}

// ---------------------------------------------------------------------------
// Rotating writer
// ---------------------------------------------------------------------------

// rotatingWriter wraps an *os.File and rotates when the file exceeds the
// configured maximum size.
type rotatingWriter struct {
	mu       sync.Mutex // guards file and written
	file     *os.File
	path     string
	maxBytes int64
	written  int64
}

func newRotatingWriter(f *os.File, cfg LogConfig) *rotatingWriter {
	maxSizeMB := cfg.MaxSizeMB
	if maxSizeMB <= 0 {
		maxSizeMB = defaultMaxSizeMB
	}

	// Seed written with current file size so rotation respects existing content.
	var written int64
	if info, err := f.Stat(); err == nil {
		written = info.Size()
	}

	return &rotatingWriter{
		file:     f,
		path:     cfg.FilePath,
		maxBytes: int64(maxSizeMB) * 1024 * 1024,
		written:  written,
	}
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.written+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			// If rotation fails, continue writing to the current file
			// rather than losing the log entry.
			_ = err
		}
	}

	n, err := w.file.Write(p)
	w.written += int64(n)
	return n, err
}

func (w *rotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("failed to close log file for rotation: %w", err)
	}

	backupPath := w.path + backupSuffix
	// Remove old backup if it exists; ignore error.
	_ = os.Remove(backupPath)

	if err := os.Rename(w.path, backupPath); err != nil {
		return fmt.Errorf("failed to rename log file: %w", err)
	}

	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create new log file: %w", err)
	}

	w.file = f
	w.written = 0

	// Update global logFile reference so Close() targets the right file.
	mu.Lock()
	logFile = f
	mu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// Subsystem-level handler
// ---------------------------------------------------------------------------

// subsystemLevelHandler is the outermost handler in the chain. It checks
// whether a log record should be emitted based on the subsystem-specific
// level (if set) or the global dynamic level.
type subsystemLevelHandler struct {
	inner          slog.Handler
	globalLevel    *slog.LevelVar
	subsystemLevel *subsystemLevelMap

	// preAttrs caches attrs added via WithAttrs so we can inspect them for
	// the subsystem key when checking per-subsystem levels.
	preAttrs []slog.Attr
}

// Compile-time interface check.
var _ slog.Handler = (*subsystemLevelHandler)(nil)

func newSubsystemLevelHandler(inner slog.Handler, globalLvl *slog.LevelVar, subLvl *subsystemLevelMap) *subsystemLevelHandler {
	return &subsystemLevelHandler{
		inner:          inner,
		globalLevel:    globalLvl,
		subsystemLevel: subLvl,
	}
}

// Enabled returns true if the record might pass. We use the most permissive
// level (debug) here because the actual filtering happens in Handle where
// we have access to the full record including subsystem attrs.
func (h *subsystemLevelHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

// Handle checks the record's subsystem attr against per-subsystem levels.
// If no subsystem is found in the record or preAttrs, the global level applies.
func (h *subsystemLevelHandler) Handle(ctx context.Context, r slog.Record) error {
	subsystem := h.findSubsystem(r)
	threshold := h.globalLevel.Level()
	if subsystem != "" {
		if lvl, ok := h.subsystemLevel.get(subsystem); ok {
			threshold = lvl
		}
	}
	if r.Level < threshold {
		return nil
	}
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new handler with the given attrs. We cache them so
// Handle can check for a subsystem key.
func (h *subsystemLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.preAttrs)+len(attrs))
	merged = append(merged, h.preAttrs...)
	merged = append(merged, attrs...)
	return &subsystemLevelHandler{
		inner:          h.inner.WithAttrs(attrs),
		globalLevel:    h.globalLevel,
		subsystemLevel: h.subsystemLevel,
		preAttrs:       merged,
	}
}

// WithGroup returns a new handler with the given group.
func (h *subsystemLevelHandler) WithGroup(name string) slog.Handler {
	return &subsystemLevelHandler{
		inner:          h.inner.WithGroup(name),
		globalLevel:    h.globalLevel,
		subsystemLevel: h.subsystemLevel,
		preAttrs:       h.preAttrs,
	}
}

// findSubsystem searches for the subsystem attr in the record and preAttrs.
func (h *subsystemLevelHandler) findSubsystem(r slog.Record) string {
	// Check preAttrs first (set via WithSubsystem / With).
	for _, a := range h.preAttrs {
		if a.Key == subsystemAttrKey {
			return a.Value.String()
		}
	}
	// Check record attrs.
	var sub string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == subsystemAttrKey {
			sub = a.Value.String()
			return false
		}
		return true
	})
	return sub
}

// ---------------------------------------------------------------------------
// Redaction handler
// ---------------------------------------------------------------------------

// redactHandler wraps a slog.Handler and replaces values of sensitive keys
// with [REDACTED]. Key matching is case-insensitive.
type redactHandler struct {
	inner   slog.Handler
	keysMap map[string]struct{}
}

// Compile-time interface check.
var _ slog.Handler = (*redactHandler)(nil)

func newRedactHandler(inner slog.Handler, keys []string) *redactHandler {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[strings.ToLower(k)] = struct{}{}
	}
	return &redactHandler{inner: inner, keysMap: m}
}

// Enabled reports whether the inner handler handles records at the given level.
func (h *redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle redacts sensitive attrs and delegates to the inner handler.
func (h *redactHandler) Handle(ctx context.Context, r slog.Record) error {
	// Build a new record with redacted attributes.
	redacted := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		redacted.AddAttrs(h.redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, redacted)
}

// WithAttrs returns a new redactHandler wrapping the inner handler with the
// given attrs (after redaction).
func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = h.redactAttr(a)
	}
	return &redactHandler{
		inner:   h.inner.WithAttrs(redacted),
		keysMap: h.keysMap,
	}
}

// WithGroup returns a new redactHandler with the given group on the inner
// handler.
func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{
		inner:   h.inner.WithGroup(name),
		keysMap: h.keysMap,
	}
}

func (h *redactHandler) redactAttr(a slog.Attr) slog.Attr {
	if _, ok := h.keysMap[strings.ToLower(a.Key)]; ok {
		return slog.String(a.Key, redactedPlaceholder)
	}
	// Recurse into groups.
	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		redacted := make([]slog.Attr, len(attrs))
		for i, ga := range attrs {
			redacted[i] = h.redactAttr(ga)
		}
		return slog.Group(a.Key, attrsToAny(redacted)...)
	}
	return a
}

func attrsToAny(attrs []slog.Attr) []any {
	out := make([]any, len(attrs))
	for i, a := range attrs {
		out[i] = a
	}
	return out
}
