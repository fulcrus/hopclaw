package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/fulcrus/hopclaw/internal/diagnostics"
)

func handleRootPanic() {
	recovered := recover()
	if recovered == nil {
		return
	}

	stack := debug.Stack()
	output, submitResult, reportErr := collectCrashReport(recovered, stack)
	if strings.TrimSpace(output) != "" {
		fmt.Fprintf(os.Stderr, "Crash report written to %s\n", output)
	}
	if strings.TrimSpace(submitResult.ReportID) != "" {
		fmt.Fprintf(os.Stderr, "Crash report submitted as %s\n", submitResult.ReportID)
		if strings.TrimSpace(submitResult.RequestID) != "" {
			fmt.Fprintf(os.Stderr, "Collector request ID %s\n", submitResult.RequestID)
		}
	}
	if reportErr != nil {
		fmt.Fprintf(os.Stderr, "Crash report export failed: %v\n", reportErr)
	}
	fmt.Fprintf(os.Stderr, "panic: %v\n%s", recovered, stack)
	os.Exit(1)
}

func collectCrashReport(recovered any, stack []byte) (string, diagnostics.SubmitResult, error) {
	diagCfg := loadDiagnosticsConfig()
	if !enabledDiagnosticsCrashReports(diagCfg) {
		return "", diagnostics.SubmitResult{}, nil
	}

	report, err := buildBugReport(context.Background(), diagCfg, includeLogsForDiagnostics(diagCfg))
	if err != nil {
		report = map[string][]byte{
			"bug-report-build-error.txt": []byte(err.Error() + "\n"),
		}
	}
	report["crash.json"] = mustJSON(map[string]any{
		"generated_at": time.Now().UTC(),
		"command":      strings.Join(os.Args, " "),
		"args":         os.Args[1:],
		"panic":        fmt.Sprint(recovered),
		"stack":        string(stack),
	})

	output := crashReportPath(diagCfg)
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return "", diagnostics.SubmitResult{}, fmt.Errorf("create crash report directory: %w", err)
	}
	if err := writeBugReportZIP(output, report); err != nil {
		return "", diagnostics.SubmitResult{}, fmt.Errorf("write crash report bundle: %w", err)
	}

	if strings.TrimSpace(diagCfg.UploadURL) == "" {
		return output, diagnostics.SubmitResult{}, nil
	}

	envelope := diagnostics.NewEnvelope("panic", strings.Join(os.Args, " "))
	envelope.Error = fmt.Sprint(recovered)
	envelope.StackTop = stackTop(string(stack))
	envelope.Metadata = map[string]any{
		"args":        os.Args[1:],
		"bundle_kind": "panic",
	}
	result, err := diagnostics.SubmitBundle(context.Background(), diagCfg, output, envelope, "", "")
	return output, result, err
}

func enabledDiagnosticsCrashReports(cfg config.DiagnosticsConfig) bool {
	return cfg.CrashReportsEnabled != nil && *cfg.CrashReportsEnabled
}

func includeLogsForDiagnostics(cfg config.DiagnosticsConfig) bool {
	if cfg.IncludeLogs != nil {
		return *cfg.IncludeLogs
	}
	return true
}

func crashReportPath(cfg config.DiagnosticsConfig) string {
	dir := strings.TrimSpace(cfg.BugReportDir)
	if dir == "" {
		dir = filepath.Join(daemon.StateDir(), "reports")
	}
	name := "hopclaw-crash-" + time.Now().UTC().Format("20060102-150405") + ".zip"
	return filepath.Join(dir, name)
}

func stackTop(stack string) string {
	lines := strings.Split(strings.TrimSpace(stack), "\n")
	if len(lines) > 8 {
		lines = lines[:8]
	}
	return strings.Join(lines, "\n")
}
