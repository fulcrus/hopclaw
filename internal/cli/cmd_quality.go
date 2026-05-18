package cli

import (
	"context"
	"fmt"
	"strings"

	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	"github.com/spf13/cobra"
)

func newQualityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quality",
		Short: "Quality assessment and release readiness",
	}
	cmd.AddCommand(
		newQualitySummaryCmd(),
		newQualityReadinessCmd(),
	)
	return cmd
}

func newQualitySummaryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "summary",
		Short: "Show quality summary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runQualitySummary(cmd.Context())
		},
	}
}

func runQualitySummary(ctx context.Context) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	summary, err := loadQualitySummary(ctx, client)
	if err != nil {
		return err
	}
	if flagJSON {
		return printJSON(summary)
	}

	fmt.Printf("Run Count:            %d\n", summary.RunCount)
	fmt.Printf("Terminal Runs:        %d\n", summary.TerminalRunCount)
	fmt.Printf("Task Success:         %s\n", formatQualityRate(summary.TaskSuccess))
	fmt.Printf("False Success:        %s\n", formatQualityRate(summary.FalseSuccess))
	fmt.Printf("Verification Failure: %s\n", formatQualityRate(summary.VerificationFailure))
	fmt.Printf("Trace Count:          %d\n", summary.TraceCount)
	return nil
}

func newQualityReadinessCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "readiness",
		Short: "Show release readiness assessment",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runQualityReadiness(cmd.Context())
		},
	}
}

func runQualityReadiness(ctx context.Context) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	report, err := loadReleaseReadiness(ctx, client)
	if err != nil {
		return err
	}
	if flagJSON {
		return printJSON(report)
	}

	ready := "no"
	if report.Ready {
		ready = "yes"
	}
	fmt.Printf("Ready:    %s\n", ready)
	fmt.Printf("Checks:   %d\n", len(report.Checks))
	fmt.Printf("Blockers: %d\n", len(report.Blockers))
	for _, blocker := range report.Blockers {
		fmt.Printf("  - %s: %s\n", blocker.ID, blocker.Summary)
	}
	return nil
}

func formatQualityRate(rate runtimepkg.QualityRate) string {
	if rate.Total == 0 {
		return "n/a (0/0)"
	}
	return fmt.Sprintf("%.1f%% (%d/%d)", rate.Rate*100, rate.Count, rate.Total)
}

func fetchQualitySnapshot(ctx context.Context, client *GatewayClient) (*replpkg.QualitySnapshot, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}

	summary, err := loadQualitySummary(ctx, client)
	if err != nil {
		return nil, err
	}
	report, err := loadReleaseReadiness(ctx, client)
	if err != nil {
		return nil, err
	}
	return qualitySnapshotFromReports(summary, report), nil
}

func qualitySnapshotFromReports(summary *runtimepkg.QualitySummary, report *runtimepkg.ReleaseReadinessReport) *replpkg.QualitySnapshot {
	if summary == nil {
		summary = &runtimepkg.QualitySummary{}
	}
	if report == nil {
		report = &runtimepkg.ReleaseReadinessReport{}
	}

	blockers := make([]string, 0, len(report.Blockers))
	for _, blocker := range report.Blockers {
		text := strings.TrimSpace(blocker.Summary)
		if text == "" {
			text = strings.TrimSpace(blocker.ID)
		}
		if text != "" {
			blockers = append(blockers, text)
		}
	}

	warnings := make([]string, 0, len(report.Checks))
	lastCheck := ""
	for _, check := range report.Checks {
		if trimmed := strings.TrimSpace(check.ID); trimmed != "" {
			lastCheck = trimmed
		}
		switch strings.ToLower(strings.TrimSpace(check.Status)) {
		case "", "passed", "ok", "blocked":
			continue
		}
		text := strings.TrimSpace(check.Summary)
		if text == "" {
			text = strings.TrimSpace(check.ID)
		}
		if text != "" {
			warnings = append(warnings, text)
		}
	}

	status := "ok"
	if len(blockers) > 0 || len(warnings) > 0 || !report.Ready {
		status = "warn"
	}

	return &replpkg.QualitySnapshot{
		RunCount:            summary.RunCount,
		TerminalRunCount:    summary.TerminalRunCount,
		Status:              status,
		TaskSuccess:         formatQualityRate(summary.TaskSuccess),
		FalseSuccess:        formatQualityRate(summary.FalseSuccess),
		VerificationFailure: formatQualityRate(summary.VerificationFailure),
		TraceCount:          summary.TraceCount,
		Ready:               report.Ready,
		CheckCount:          len(report.Checks),
		BlockerCount:        len(report.Blockers),
		Blockers:            blockers,
		Warnings:            warnings,
		LastCheck:           lastCheck,
	}
}
