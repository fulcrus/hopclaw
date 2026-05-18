package cli

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/fulcrus/hopclaw/internal/diagnostics"
	"github.com/fulcrus/hopclaw/internal/update"
	"github.com/fulcrus/hopclaw/internal/version"
	"github.com/fulcrus/hopclaw/keychain"
	"gopkg.in/yaml.v3"

	"github.com/spf13/cobra"
)

const (
	defaultBugReportLogBytes = 512 * 1024
)

func newBugReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bug-report",
		Short: "Generate a redacted local bug report bundle",
		Long:  "Collect version, doctor, update, config, status, and log snapshots into a local ZIP bundle. The bundle can be reviewed locally and optionally submitted to a configured diagnostics endpoint.",
		RunE:  runBugReport,
	}
	cmd.Flags().StringP("output", "o", "", "write the report to this ZIP path")
	cmd.Flags().Bool("include-logs", true, "include redacted local log snapshots")
	cmd.Flags().Bool("submit", false, "submit the generated bundle to the configured diagnostics collector")
	cmd.Flags().String("submit-url", "", "override diagnostics upload URL for this submission")
	cmd.Flags().String("submit-token", "", "override diagnostics upload bearer token for this submission")
	return cmd
}

func runBugReport(cmd *cobra.Command, _ []string) error {
	output, _ := cmd.Flags().GetString("output")
	includeLogs, _ := cmd.Flags().GetBool("include-logs")
	submit, _ := cmd.Flags().GetBool("submit")
	submitURL, _ := cmd.Flags().GetString("submit-url")
	submitToken, _ := cmd.Flags().GetString("submit-token")

	diagCfg := loadDiagnosticsConfig()
	if output == "" {
		dir := strings.TrimSpace(diagCfg.BugReportDir)
		if dir == "" {
			dir = filepath.Join(daemon.StateDir(), "reports")
		}
		name := "hopclaw-bug-report-" + time.Now().UTC().Format("20060102-150405") + ".zip"
		output = filepath.Join(dir, name)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}

	report, err := buildBugReport(cmd.Context(), diagCfg, includeLogs)
	if err != nil {
		return err
	}
	if err := writeBugReportZIP(output, report); err != nil {
		return err
	}

	var submitResult diagnostics.SubmitResult
	if submit {
		envelope := diagnostics.NewEnvelope("bug-report", strings.Join(os.Args, " "))
		envelope.Metadata = map[string]any{
			"files":        len(report),
			"include_logs": includeLogs,
		}
		result, err := diagnostics.SubmitBundle(cmd.Context(), diagCfg, output, envelope, submitURL, submitToken)
		if err != nil {
			return fmt.Errorf("submit bug report: %w (local bundle kept at %s)", err, output)
		}
		submitResult = result
	}

	if flagJSON {
		payload := map[string]any{
			"path":  output,
			"files": len(report),
		}
		if submit {
			payload["submitted"] = true
			payload["report_id"] = submitResult.ReportID
			if strings.TrimSpace(submitResult.RequestID) != "" {
				payload["request_id"] = submitResult.RequestID
			}
		}
		return printJSON(payload)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Bug report written to %s\n", output)
	if submit {
		fmt.Fprintf(cmd.OutOrStdout(), "Submitted bug report as %s\n", submitResult.ReportID)
		if strings.TrimSpace(submitResult.RequestID) != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Collector request ID %s\n", submitResult.RequestID)
		}
	}
	return nil
}

func buildBugReport(ctx context.Context, diagCfg config.DiagnosticsConfig, includeLogs bool) (map[string][]byte, error) {
	report := map[string][]byte{}
	report["metadata.json"] = mustJSON(map[string]any{
		"generated_at": time.Now().UTC(),
		"product":      version.ProductName,
		"version":      version.Version,
		"channel":      version.Channel,
		"git_commit":   version.GitCommit,
		"build_date":   version.BuildDate,
		"go_version":   runtime.Version(),
		"os":           runtime.GOOS,
		"arch":         runtime.GOARCH,
		"website":      version.DefaultWebsiteURL,
		"repository":   version.DefaultRepository,
	})
	report["doctor.json"] = mustJSON(collectDoctorChecks())

	if result := update.LastCheckResult(); result != nil {
		report["update.json"] = mustJSON(result)
	}
	if status := collectGatewayStatus(ctx); len(status) > 0 {
		report["gateway-status.json"] = mustJSON(status)
	}
	if configPath := resolveConfigPath(); configPath != "" {
		if redacted, err := collectRedactedConfig(configPath, diagCfg.RedactPatterns); err == nil {
			report["config.redacted.yaml"] = redacted
		}
	}
	if includeLogs {
		for name, body := range collectLogSnapshots(diagCfg) {
			report[name] = body
		}
	}
	return report, nil
}

func writeBugReportZIP(path string, files map[string][]byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create zip: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			return fmt.Errorf("create zip entry %s: %w", name, err)
		}
		if _, err := w.Write(files[name]); err != nil {
			return fmt.Errorf("write zip entry %s: %w", name, err)
		}
	}
	return zw.Close()
}

func loadDiagnosticsConfig() config.DiagnosticsConfig {
	configPath := resolveConfigPath()
	if configPath == "" {
		return config.DiagnosticsConfig{}
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return config.DiagnosticsConfig{}
	}
	cfg.ResolveSecrets(keychain.ResolveField)
	return cfg.Diagnostics
}

func collectGatewayStatus(ctx context.Context) map[string]any {
	client, err := NewGatewayClient()
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	client.HTTP.Timeout = 3 * time.Second
	return collectGatewayStatusWithClient(ctx, client)
}

func collectGatewayStatusWithClient(parent context.Context, client *GatewayClient) map[string]any {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()

	body, statusCode, err := fetchOperatorStatus(ctx, client)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return map[string]any{
			"status_code": statusCode,
			"body":        redactRawText(string(body), nil),
		}
	}
	payload["status_code"] = statusCode
	return payload
}

func collectRedactedConfig(path string, customPatterns []string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return []byte(redactRawText(string(data), customPatterns)), nil
	}
	redacted := redactValue(raw, customPatterns)
	return yaml.Marshal(redacted)
}

func redactValue(v any, customPatterns []string) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, val := range typed {
			if shouldRedactKey(k) {
				out[k] = "[REDACTED]"
				continue
			}
			out[k] = redactValue(val, customPatterns)
		}
		return out
	case map[any]any:
		out := make(map[any]any, len(typed))
		for k, val := range typed {
			key := fmt.Sprint(k)
			if shouldRedactKey(key) {
				out[k] = "[REDACTED]"
				continue
			}
			out[k] = redactValue(val, customPatterns)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = redactValue(typed[i], customPatterns)
		}
		return out
	case []string:
		out := make([]string, len(typed))
		for i := range typed {
			out[i] = redactRawText(typed[i], customPatterns)
		}
		return out
	case string:
		return redactRawText(typed, customPatterns)
	default:
		return v
	}
}

func shouldRedactKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"token", "secret", "password", "api_key", "apikey", "auth_token", "bearer"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func collectLogSnapshots(diagCfg config.DiagnosticsConfig) map[string][]byte {
	maxBytes := diagCfg.MaxLogBytes
	if maxBytes <= 0 {
		maxBytes = defaultBugReportLogBytes
	}
	files := discoverLogFiles()
	out := make(map[string][]byte, len(files))
	for _, path := range files {
		body, err := tailBytes(path, maxBytes)
		if err != nil || len(body) == 0 {
			continue
		}
		name := filepath.Base(path)
		out[filepath.Join("logs", name)] = []byte(redactRawText(string(body), diagCfg.RedactPatterns))
	}
	return out
}

func discoverLogFiles() []string {
	seen := map[string]struct{}{}
	var files []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			seen[path] = struct{}{}
			files = append(files, path)
		}
	}

	if cfgPath := resolveConfigPath(); cfgPath != "" {
		if cfg, err := config.Load(cfgPath); err == nil {
			add(cfg.Logging.FilePath)
		}
	}
	entries, err := os.ReadDir(daemon.LogDir())
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			add(filepath.Join(daemon.LogDir(), entry.Name()))
		}
	}
	sort.Strings(files)
	return files
}

func tailBytes(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	offset := int64(0)
	if size > maxBytes {
		offset = size - maxBytes
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

func redactRawText(text string, customPatterns []string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)sk-[a-z0-9_-]+`),
		regexp.MustCompile(`(?i)xox[baprs]-[a-z0-9-]+`),
		regexp.MustCompile(`(?i)gh[pousr]_[a-z0-9_]+`),
		regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._-]+`),
		regexp.MustCompile(`(?i)api[_-]?key[\"'=:\s]+[A-Za-z0-9._-]+`),
		regexp.MustCompile(`(?i)auth[_-]?token[\"'=:\s]+[A-Za-z0-9._-]+`),
	}
	for _, raw := range customPatterns {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if compiled, err := regexp.Compile(raw); err == nil {
			patterns = append(patterns, compiled)
		}
	}
	for _, pattern := range patterns {
		text = pattern.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}

func mustJSON(v any) []byte {
	data, _ := json.MarshalIndent(v, "", "  ")
	return data
}
