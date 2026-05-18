package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// secureFileMode is the expected permission for config files (owner read/write).
	secureFileMode = 0600

	// secureDirMode is the expected permission for config/state directories (owner rwx).
	secureDirMode = 0700

	// authTokenBytes is the number of random bytes for generated auth tokens.
	authTokenBytes = 32

	// auditStatusPass indicates the check passed.
	auditStatusPass = "pass"

	// auditStatusWarn indicates a non-critical issue.
	auditStatusWarn = "warn"

	// auditStatusFail indicates a critical security issue.
	auditStatusFail = "fail"
)

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type auditResult struct {
	Check   string `json:"check"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type auditReport struct {
	Results []auditResult `json:"results"`
	Summary auditSummary  `json:"summary"`
}

type auditSummary struct {
	Total int `json:"total"`
	Pass  int `json:"pass"`
	Warn  int `json:"warn"`
	Fail  int `json:"fail"`
}

type rotateResponse struct {
	Token string `json:"token"`
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newSecurityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "security",
		Short: "Security audit and management",
		Long:  "Audit configuration for security issues and manage security settings.",
	}
	cmd.AddCommand(
		newSecurityAuditCmd(),
		newSecurityRotateCmd(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// security audit
// ---------------------------------------------------------------------------

func newSecurityAuditCmd() *cobra.Command {
	var fix bool

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Check for common security issues",
		Long: `Audit the local configuration for common security issues.

Checks performed:
  - Config file permissions (should be 600)
  - Auth token presence
  - API key storage method (keychain vs raw)
  - State directory permissions
  - Log file permissions
  - TLS configuration`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSecurityAudit(fix)
		},
	}

	cmd.Flags().BoolVar(&fix, "fix", false, "automatically fix issues where possible")

	return cmd
}

func runSecurityAudit(fix bool) error {
	configPath := resolveConfigPath()

	var results []auditResult

	results = append(results, auditConfigFilePerms(configPath, fix))
	results = append(results, auditAuthToken(configPath))
	results = append(results, auditAPIKeyStorage(configPath))
	results = append(results, auditStateDirPerms(configPath, fix))
	results = append(results, auditLogFilePerms(configPath, fix))
	results = append(results, auditTLSConfig(configPath))

	report := buildAuditReport(results)

	if flagJSON {
		return printJSON(report)
	}

	printAuditReport(report)
	return nil
}

// ---------------------------------------------------------------------------
// Audit checks
// ---------------------------------------------------------------------------

func auditConfigFilePerms(configPath string, fix bool) auditResult {
	if configPath == "" {
		return auditResult{
			Check:   "config_file_permissions",
			Status:  auditStatusWarn,
			Message: "no config file found; skipping permission check",
		}
	}

	info, err := os.Stat(configPath)
	if err != nil {
		return auditResult{
			Check:   "config_file_permissions",
			Status:  auditStatusWarn,
			Message: fmt.Sprintf("cannot stat config file: %s", err),
		}
	}

	mode := info.Mode().Perm()
	if mode == fs.FileMode(secureFileMode) {
		return auditResult{
			Check:   "config_file_permissions",
			Status:  auditStatusPass,
			Message: fmt.Sprintf("config file has correct permissions (%04o)", mode),
		}
	}

	if fix {
		if err := os.Chmod(configPath, fs.FileMode(secureFileMode)); err != nil {
			return auditResult{
				Check:   "config_file_permissions",
				Status:  auditStatusFail,
				Message: fmt.Sprintf("failed to fix permissions: %s", err),
			}
		}
		return auditResult{
			Check:   "config_file_permissions",
			Status:  auditStatusPass,
			Message: fmt.Sprintf("fixed config file permissions from %04o to %04o", mode, secureFileMode),
		}
	}

	return auditResult{
		Check:   "config_file_permissions",
		Status:  auditStatusFail,
		Message: fmt.Sprintf("config file has overly permissive mode %04o (expected %04o)", mode, secureFileMode),
	}
}

func auditAuthToken(configPath string) auditResult {
	if configPath == "" {
		return auditResult{
			Check:   "auth_token",
			Status:  auditStatusWarn,
			Message: "no config file found; cannot check auth token",
		}
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return auditResult{
			Check:   "auth_token",
			Status:  auditStatusWarn,
			Message: fmt.Sprintf("cannot load config: %s", err),
		}
	}

	if !cfg.HasAuth() {
		return auditResult{
			Check:   "auth_token",
			Status:  auditStatusWarn,
			Message: "gateway authentication is not configured; API is unprotected",
		}
	}

	return auditResult{
		Check:   "auth_token",
		Status:  auditStatusPass,
		Message: "gateway authentication is configured",
	}
}

func auditAPIKeyStorage(configPath string) auditResult {
	if configPath == "" {
		return auditResult{
			Check:   "api_key_storage",
			Status:  auditStatusWarn,
			Message: "no config file found; cannot check API key storage",
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return auditResult{
			Check:   "api_key_storage",
			Status:  auditStatusWarn,
			Message: fmt.Sprintf("cannot read config file: %s", err),
		}
	}

	content := string(data)
	rawKeyPatterns := []string{"api_key:", "auth_token:", "bot_token:", "password:", "secret:", "access_token:", "private_key:"}
	var exposedFields []string

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, pattern := range rawKeyPatterns {
			if !strings.Contains(trimmed, pattern) {
				continue
			}
			// Extract the value after the YAML key
			parts := strings.SplitN(trimmed, pattern, 2)
			if len(parts) < 2 {
				continue
			}
			value := strings.TrimSpace(parts[1])
			if value == "" || value == `""` || value == "''" {
				continue
			}
			// Values using keychain: or env: prefix are safe
			if strings.HasPrefix(value, "keychain:") || strings.HasPrefix(value, "${") || strings.HasPrefix(value, "$") {
				continue
			}
			exposedFields = append(exposedFields, pattern)
		}
	}

	if len(exposedFields) == 0 {
		return auditResult{
			Check:   "api_key_storage",
			Status:  auditStatusPass,
			Message: "no raw API keys detected in config (using keychain or env vars)",
		}
	}

	return auditResult{
		Check:   "api_key_storage",
		Status:  auditStatusWarn,
		Message: fmt.Sprintf("raw credentials found for: %s; consider using keychain: prefix", strings.Join(exposedFields, ", ")),
	}
}

func auditStateDirPerms(configPath string, fix bool) auditResult {
	stateDir := resolveStateDir(configPath)
	if stateDir == "" {
		return auditResult{
			Check:   "state_dir_permissions",
			Status:  auditStatusWarn,
			Message: "cannot determine state directory; skipping check",
		}
	}

	info, err := os.Stat(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return auditResult{
				Check:   "state_dir_permissions",
				Status:  auditStatusPass,
				Message: "state directory does not exist yet",
			}
		}
		return auditResult{
			Check:   "state_dir_permissions",
			Status:  auditStatusWarn,
			Message: fmt.Sprintf("cannot stat state directory: %s", err),
		}
	}

	mode := info.Mode().Perm()
	if mode == fs.FileMode(secureDirMode) {
		return auditResult{
			Check:   "state_dir_permissions",
			Status:  auditStatusPass,
			Message: fmt.Sprintf("state directory has correct permissions (%04o)", mode),
		}
	}

	if fix {
		if err := os.Chmod(stateDir, fs.FileMode(secureDirMode)); err != nil {
			return auditResult{
				Check:   "state_dir_permissions",
				Status:  auditStatusFail,
				Message: fmt.Sprintf("failed to fix state directory permissions: %s", err),
			}
		}
		return auditResult{
			Check:   "state_dir_permissions",
			Status:  auditStatusPass,
			Message: fmt.Sprintf("fixed state directory permissions from %04o to %04o", mode, secureDirMode),
		}
	}

	return auditResult{
		Check:   "state_dir_permissions",
		Status:  auditStatusFail,
		Message: fmt.Sprintf("state directory has overly permissive mode %04o (expected %04o)", mode, secureDirMode),
	}
}

func auditLogFilePerms(configPath string, fix bool) auditResult {
	stateDir := resolveStateDir(configPath)
	if stateDir == "" {
		return auditResult{
			Check:   "log_file_permissions",
			Status:  auditStatusWarn,
			Message: "cannot determine state directory; skipping log file check",
		}
	}

	logFile := filepath.Join(stateDir, "hopclaw.log")
	info, err := os.Stat(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return auditResult{
				Check:   "log_file_permissions",
				Status:  auditStatusPass,
				Message: "no log file found",
			}
		}
		return auditResult{
			Check:   "log_file_permissions",
			Status:  auditStatusWarn,
			Message: fmt.Sprintf("cannot stat log file: %s", err),
		}
	}

	mode := info.Mode().Perm()
	if mode == fs.FileMode(secureFileMode) {
		return auditResult{
			Check:   "log_file_permissions",
			Status:  auditStatusPass,
			Message: fmt.Sprintf("log file has correct permissions (%04o)", mode),
		}
	}

	if fix {
		if err := os.Chmod(logFile, fs.FileMode(secureFileMode)); err != nil {
			return auditResult{
				Check:   "log_file_permissions",
				Status:  auditStatusFail,
				Message: fmt.Sprintf("failed to fix log file permissions: %s", err),
			}
		}
		return auditResult{
			Check:   "log_file_permissions",
			Status:  auditStatusPass,
			Message: fmt.Sprintf("fixed log file permissions from %04o to %04o", mode, secureFileMode),
		}
	}

	return auditResult{
		Check:   "log_file_permissions",
		Status:  auditStatusFail,
		Message: fmt.Sprintf("log file has overly permissive mode %04o (expected %04o)", mode, secureFileMode),
	}
}

func auditTLSConfig(configPath string) auditResult {
	if configPath == "" {
		return auditResult{
			Check:   "tls_configuration",
			Status:  auditStatusWarn,
			Message: "no config file found; cannot check TLS configuration",
		}
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return auditResult{
			Check:   "tls_configuration",
			Status:  auditStatusWarn,
			Message: fmt.Sprintf("cannot load config: %s", err),
		}
	}

	addr := strings.TrimSpace(cfg.Server.Address)
	if addr == "" || addr == "localhost" || strings.HasPrefix(addr, "127.0.0.1") {
		return auditResult{
			Check:   "tls_configuration",
			Status:  auditStatusPass,
			Message: "server is listening on localhost only; TLS not required",
		}
	}

	return auditResult{
		Check:   "tls_configuration",
		Status:  auditStatusWarn,
		Message: "server is exposed on a non-localhost address without TLS; consider using a reverse proxy with TLS",
	}
}

// ---------------------------------------------------------------------------
// security rotate
// ---------------------------------------------------------------------------

func newSecurityRotateCmd() *cobra.Command {
	var authToken bool

	cmd := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate security credentials",
		Long: `Rotate security credentials such as the server auth token.

Examples:
  hopclaw security rotate --auth-token`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !authToken {
				return fmt.Errorf("specify what to rotate (e.g. --auth-token)")
			}
			return runSecurityRotateAuthToken()
		},
	}

	cmd.Flags().BoolVar(&authToken, "auth-token", false, "generate and set a new auth token")

	return cmd
}

func runSecurityRotateAuthToken() error {
	configPath := resolveConfigPath()
	if configPath == "" {
		return fmt.Errorf("no config file found; cannot rotate auth token")
	}

	token, err := generateSecureToken(authTokenBytes)
	if err != nil {
		return fmt.Errorf("generate token: %w", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	content := string(data)
	updated := replaceAuthToken(content, token)

	if err := os.WriteFile(configPath, []byte(updated), fs.FileMode(secureFileMode)); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	if flagJSON {
		return printJSON(rotateResponse{Token: token})
	}

	fmt.Printf("Auth token rotated successfully.\n")
	fmt.Printf("New token: %s\n", token)
	fmt.Println("Restart the gateway for the new token to take effect.")
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func resolveStateDir(configPath string) string {
	if configPath != "" {
		return filepath.Dir(configPath)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".hopclaw")
}

func generateSecureToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func replaceAuthToken(content, newToken string) string {
	var lines []string
	replaced := false

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !replaced && strings.HasPrefix(trimmed, "auth_token:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines = append(lines, indent+"auth_token: "+newToken)
			replaced = true
			continue
		}
		lines = append(lines, line)
	}

	if !replaced {
		lines = append(lines, "  auth_token: "+newToken)
	}

	return strings.Join(lines, "\n")
}

func buildAuditReport(results []auditResult) auditReport {
	var summary auditSummary
	summary.Total = len(results)
	for _, r := range results {
		switch r.Status {
		case auditStatusPass:
			summary.Pass++
		case auditStatusWarn:
			summary.Warn++
		case auditStatusFail:
			summary.Fail++
		}
	}
	return auditReport{
		Results: results,
		Summary: summary,
	}
}

func printAuditReport(report auditReport) {
	for _, r := range report.Results {
		icon := auditStatusIcon(r.Status)
		fmt.Printf("  %s  %-28s  %s\n", icon, r.Check, r.Message)
	}

	fmt.Println()
	fmt.Printf("Results: %d total, %d pass, %d warn, %d fail\n",
		report.Summary.Total,
		report.Summary.Pass,
		report.Summary.Warn,
		report.Summary.Fail,
	)
}

func auditStatusIcon(status string) string {
	switch status {
	case auditStatusPass:
		return "PASS"
	case auditStatusWarn:
		return "WARN"
	case auditStatusFail:
		return "FAIL"
	default:
		return "????"
	}
}
