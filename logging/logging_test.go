package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// initWithBuffer configures logging to write JSON to a buffer so tests can
// inspect structured output. Returns the buffer. It replicates the full
// handler chain used by Init: inner -> redact -> subsystem-level.
func initWithBuffer(t *testing.T, cfg LogConfig) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer

	level, err := parseLevel(cfg.Level)
	if err != nil {
		t.Fatalf("parseLevel(%q) error = %v", cfg.Level, err)
	}

	// Set global dynamic level.
	globalLevel.Set(level)

	// Parse and store subsystem levels.
	subsystemLevel.reset()
	for sub, raw := range cfg.SubsystemLevels {
		lvl, parseErr := parseLevel(raw)
		if parseErr != nil {
			t.Fatalf("parseLevel subsystem %q (%q) error = %v", sub, raw, parseErr)
		}
		subsystemLevel.set(sub, lvl)
	}

	// Use debug level for inner handler so our custom chain controls filtering.
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}

	format := strings.TrimSpace(strings.ToLower(cfg.Format))
	if format == "" {
		format = defaultFormat
	}

	var inner slog.Handler
	switch format {
	case "json":
		inner = slog.NewJSONHandler(&buf, opts)
	case "text":
		inner = slog.NewTextHandler(&buf, opts)
	default:
		t.Fatalf("unsupported format %q", format)
	}

	redactKeys := cfg.RedactKeys
	if len(redactKeys) == 0 {
		redactKeys = defaultRedactKeys
	}

	// Build handler chain: inner -> sampling (optional) -> redact -> subsystem level.
	handler := inner
	if cfg.Sampling.Enabled {
		handler = newSamplingHandler(handler, cfg.Sampling)
	}
	handler = newRedactHandler(handler, redactKeys)
	handler = newSubsystemLevelHandler(handler, &globalLevel, &subsystemLevel)

	slog.SetDefault(slog.New(handler))
	return &buf
}

// ---------------------------------------------------------------------------
// Tests - Basic formatting
// ---------------------------------------------------------------------------

func TestInitJSONFormat(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{Format: "json"})

	slog.Info("hello", "key", "value")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, buf.String())
	}
	if msg, ok := entry["msg"].(string); !ok || msg != "hello" {
		t.Fatalf("msg = %v, want %q", entry["msg"], "hello")
	}
	if v, ok := entry["key"].(string); !ok || v != "value" {
		t.Fatalf("key = %v, want %q", entry["key"], "value")
	}
}

func TestInitTextFormat(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{Format: "text"})

	slog.Info("hello world")

	out := buf.String()
	if !strings.Contains(out, "hello world") {
		t.Fatalf("text output missing message: %s", out)
	}
	if !strings.Contains(out, "INFO") {
		t.Fatalf("text output missing level: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Tests - Level filtering
// ---------------------------------------------------------------------------

func TestLevelFiltering(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{Format: "json", Level: "info"})

	slog.Debug("should not appear")
	slog.Info("should appear")

	out := buf.String()
	if strings.Contains(out, "should not appear") {
		t.Fatalf("debug message should be filtered at info level: %s", out)
	}
	if !strings.Contains(out, "should appear") {
		t.Fatalf("info message should be present: %s", out)
	}
}

func TestLevelDebug(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{Format: "json", Level: "debug"})

	slog.Debug("debug msg")

	out := buf.String()
	if !strings.Contains(out, "debug msg") {
		t.Fatalf("debug message should be present at debug level: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Tests - Subsystem level filtering
// ---------------------------------------------------------------------------

func TestSubsystemLevelFiltering(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{
		Format: "json",
		Level:  "info",
		SubsystemLevels: map[string]string{
			"agent": "debug",
			"model": "warn",
		},
	})

	// agent subsystem at debug level — debug messages should pass.
	agentLogger := WithSubsystem("agent")
	agentLogger.Debug("agent debug msg")

	// model subsystem at warn level — info messages should be blocked.
	modelLogger := WithSubsystem("model")
	modelLogger.Info("model info msg")

	// model subsystem at warn level — warn messages should pass.
	modelLogger.Warn("model warn msg")

	out := buf.String()
	if !strings.Contains(out, "agent debug msg") {
		t.Fatalf("agent debug message should pass at agent=debug level: %s", out)
	}
	if strings.Contains(out, "model info msg") {
		t.Fatalf("model info message should be blocked at model=warn level: %s", out)
	}
	if !strings.Contains(out, "model warn msg") {
		t.Fatalf("model warn message should pass at model=warn level: %s", out)
	}
}

func TestSubsystemLevelFallsBackToGlobal(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{
		Format: "json",
		Level:  "warn",
		SubsystemLevels: map[string]string{
			"agent": "debug",
		},
	})

	// No subsystem — global level (warn) applies.
	slog.Info("global info msg")
	slog.Warn("global warn msg")

	out := buf.String()
	if strings.Contains(out, "global info msg") {
		t.Fatalf("global info message should be blocked at global=warn level: %s", out)
	}
	if !strings.Contains(out, "global warn msg") {
		t.Fatalf("global warn message should pass at global=warn level: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Tests - Dynamic log level
// ---------------------------------------------------------------------------

func TestSetLevelRuntime(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{Format: "json", Level: "info"})

	slog.Debug("before change")

	if err := SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel() error = %v", err)
	}
	defer SetLevel("info") //nolint:errcheck

	slog.Debug("after change")

	out := buf.String()
	if strings.Contains(out, "before change") {
		t.Fatalf("debug message before level change should be filtered: %s", out)
	}
	if !strings.Contains(out, "after change") {
		t.Fatalf("debug message after level change should pass: %s", out)
	}
}

func TestGetLevel(t *testing.T) {
	_ = initWithBuffer(t, LogConfig{Format: "json", Level: "warn"})

	got := GetLevel()
	if got != "warn" {
		t.Fatalf("GetLevel() = %q, want %q", got, "warn")
	}
}

func TestSetSubsystemLevelRuntime(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{Format: "json", Level: "info"})

	modelLogger := WithSubsystem("model")
	modelLogger.Debug("before subsystem change")

	if err := SetSubsystemLevel("model", "debug"); err != nil {
		t.Fatalf("SetSubsystemLevel() error = %v", err)
	}
	defer SetSubsystemLevel("model", "") //nolint:errcheck

	modelLogger.Debug("after subsystem change")

	out := buf.String()
	if strings.Contains(out, "before subsystem change") {
		t.Fatalf("debug message before subsystem level change should be filtered: %s", out)
	}
	if !strings.Contains(out, "after subsystem change") {
		t.Fatalf("debug message after subsystem level change should pass: %s", out)
	}
}

func TestGetSubsystemLevel(t *testing.T) {
	_ = initWithBuffer(t, LogConfig{
		Format: "json",
		Level:  "info",
		SubsystemLevels: map[string]string{
			"agent": "debug",
		},
	})

	if got := GetSubsystemLevel("agent"); got != "debug" {
		t.Fatalf("GetSubsystemLevel(agent) = %q, want %q", got, "debug")
	}
	// Unset subsystem should fall back to global level.
	if got := GetSubsystemLevel("unknown"); got != "info" {
		t.Fatalf("GetSubsystemLevel(unknown) = %q, want %q", got, "info")
	}
}

func TestSetLevelInvalid(t *testing.T) {
	if err := SetLevel("trace"); err == nil {
		t.Fatal("SetLevel(trace) should fail for unsupported level")
	}
}

// ---------------------------------------------------------------------------
// Tests - Redaction
// ---------------------------------------------------------------------------

func TestRedaction(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{
		Format:     "json",
		RedactKeys: []string{"api_key", "token"},
	})

	slog.Info("credentials",
		"api_key", "sk-secret-123",
		"token", "tok-abc",
		"user", "alice",
	)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, buf.String())
	}
	if v := entry["api_key"]; v != redactedPlaceholder {
		t.Fatalf("api_key = %v, want %q", v, redactedPlaceholder)
	}
	if v := entry["token"]; v != redactedPlaceholder {
		t.Fatalf("token = %v, want %q", v, redactedPlaceholder)
	}
	if v := entry["user"]; v != "alice" {
		t.Fatalf("user = %v, want %q", v, "alice")
	}
}

func TestRedactionCaseInsensitive(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{
		Format:     "json",
		RedactKeys: []string{"API_KEY"},
	})

	slog.Info("test", "api_key", "secret-value")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, buf.String())
	}
	if v := entry["api_key"]; v != redactedPlaceholder {
		t.Fatalf("api_key = %v, want %q", v, redactedPlaceholder)
	}
}

func TestDefaultRedactKeysApplied(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{Format: "json"})

	slog.Info("default keys",
		"api_key", "key1",
		"apikey", "key2",
		"token", "tok1",
		"secret", "sec1",
		"password", "pass1",
		"authorization", "auth1",
		"safe_field", "visible",
	)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, buf.String())
	}

	for _, key := range defaultRedactKeys {
		if v := entry[key]; v != redactedPlaceholder {
			t.Errorf("default redact key %q = %v, want %q", key, v, redactedPlaceholder)
		}
	}
	if v := entry["safe_field"]; v != "visible" {
		t.Fatalf("safe_field = %v, want %q", v, "visible")
	}
}

func TestWithSubsystem(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{Format: "json"})

	logger := WithSubsystem("agent")
	logger.Info("started")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, buf.String())
	}
	if v := entry["subsystem"]; v != "agent" {
		t.Fatalf("subsystem = %v, want %q", v, "agent")
	}
	if v := entry["msg"]; v != "started" {
		t.Fatalf("msg = %v, want %q", v, "started")
	}
}

// ---------------------------------------------------------------------------
// Tests - File output and rotation
// ---------------------------------------------------------------------------

func TestFileOutput(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	err := Init(LogConfig{
		Format:   "json",
		Output:   "file",
		FilePath: logPath,
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer Close()

	slog.Info("file test", "key", "value")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "file test") {
		t.Fatalf("log file missing message: %s", string(data))
	}

	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, string(data))
	}
	if v := entry["key"]; v != "value" {
		t.Fatalf("key = %v, want %q", v, "value")
	}
}

func TestFileOutputMissingPath(t *testing.T) {
	err := Init(LogConfig{
		Output: "file",
	})
	if err == nil {
		t.Fatal("Init() should fail when output=file and file_path is empty")
	}
	if !strings.Contains(err.Error(), "file_path is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCloseWithoutInit(t *testing.T) {
	// Ensure Close() on a nil logFile returns no error.
	mu.Lock()
	saved := logFile
	logFile = nil
	mu.Unlock()

	defer func() {
		mu.Lock()
		logFile = saved
		mu.Unlock()
	}()

	if err := Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestCloseAfterFileInit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "close-test.log")

	err := Init(LogConfig{
		Format:   "text",
		Output:   "file",
		FilePath: logPath,
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	slog.Info("before close")

	if err := Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify the log file was written before closing.
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "before close") {
		t.Fatalf("log file missing message: %s", string(data))
	}
}

// ---------------------------------------------------------------------------
// Tests - Error cases
// ---------------------------------------------------------------------------

func TestUnsupportedLevel(t *testing.T) {
	err := Init(LogConfig{Level: "trace"})
	if err == nil {
		t.Fatal("Init() should fail for unsupported level")
	}
	if !strings.Contains(err.Error(), "unsupported log level") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnsupportedFormat(t *testing.T) {
	err := Init(LogConfig{Format: "xml"})
	if err == nil {
		t.Fatal("Init() should fail for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported log format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnsupportedOutput(t *testing.T) {
	err := Init(LogConfig{Output: "syslog"})
	if err == nil {
		t.Fatal("Init() should fail for unsupported output")
	}
	if !strings.Contains(err.Error(), "unsupported log output") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLogFileRotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "rotate.log")

	// Use a tiny max size to trigger rotation quickly.
	const tinyMaxSizeMB = 1
	err := Init(LogConfig{
		Format:    "text",
		Output:    "file",
		FilePath:  logPath,
		MaxSizeMB: tinyMaxSizeMB,
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer Close()

	// Write enough data to exceed 1 MB.
	bigMessage := strings.Repeat("x", 1024)
	const iterationsToExceed1MB = 1100
	for i := 0; i < iterationsToExceed1MB; i++ {
		slog.Info(bigMessage)
	}

	// The backup file should exist after rotation.
	backupPath := logPath + backupSuffix
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatalf("backup file %q should exist after rotation", backupPath)
	}

	// The main log file should exist and be smaller than the backup.
	mainInfo, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", logPath, err)
	}
	backupInfo, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", backupPath, err)
	}
	if mainInfo.Size() >= backupInfo.Size() {
		t.Fatalf("main file (%d bytes) should be smaller than backup (%d bytes)",
			mainInfo.Size(), backupInfo.Size())
	}
}

func TestRedactionWithSubsystem(t *testing.T) {
	buf := initWithBuffer(t, LogConfig{
		Format:     "json",
		RedactKeys: []string{"password"},
	})

	logger := WithSubsystem("auth")
	logger.Info("login attempt", "user", "bob", "password", "hunter2")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, buf.String())
	}
	if v := entry["password"]; v != redactedPlaceholder {
		t.Fatalf("password = %v, want %q", v, redactedPlaceholder)
	}
	if v := entry["subsystem"]; v != "auth" {
		t.Fatalf("subsystem = %v, want %q", v, "auth")
	}
}

func TestInitDefaultOutput(t *testing.T) {
	err := Init(LogConfig{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
}

func TestBuildWriterDefaultsToStderr(t *testing.T) {
	writer, err := buildWriter(LogConfig{})
	if err != nil {
		t.Fatalf("buildWriter() error = %v", err)
	}
	if writer != os.Stderr {
		t.Fatalf("writer = %#v, want os.Stderr", writer)
	}
}

// ---------------------------------------------------------------------------
// Tests - Parse level
// ---------------------------------------------------------------------------

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
		err   bool
	}{
		{"", slog.LevelInfo, false},
		{"info", slog.LevelInfo, false},
		{"INFO", slog.LevelInfo, false},
		{"debug", slog.LevelDebug, false},
		{"warn", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"  Debug  ", slog.LevelDebug, false},
		{"trace", slog.LevelInfo, true},
		{"fatal", slog.LevelInfo, true},
	}

	for _, tt := range tests {
		level, err := parseLevel(tt.input)
		if tt.err && err == nil {
			t.Errorf("parseLevel(%q): want error, got nil", tt.input)
		}
		if !tt.err && err != nil {
			t.Errorf("parseLevel(%q): unexpected error: %v", tt.input, err)
		}
		if !tt.err && level != tt.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tt.input, level, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests - Level to string
// ---------------------------------------------------------------------------

func TestLevelToString(t *testing.T) {
	tests := []struct {
		input slog.Level
		want  string
	}{
		{slog.LevelDebug, "debug"},
		{slog.LevelInfo, "info"},
		{slog.LevelWarn, "warn"},
		{slog.LevelError, "error"},
		{slog.Level(42), "info"}, // unknown falls back to info
	}

	for _, tt := range tests {
		got := levelToString(tt.input)
		if got != tt.want {
			t.Errorf("levelToString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests - Invalid subsystem level in Init
// ---------------------------------------------------------------------------

func TestInitInvalidSubsystemLevel(t *testing.T) {
	err := Init(LogConfig{
		SubsystemLevels: map[string]string{
			"agent": "trace",
		},
	})
	if err == nil {
		t.Fatal("Init() should fail for invalid subsystem level")
	}
	if !strings.Contains(err.Error(), "subsystem \"agent\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}
