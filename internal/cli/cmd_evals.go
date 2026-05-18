package cli

import (
	"context"
	"fmt"
	"strings"

	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	"github.com/spf13/cobra"
)

type evalSuitesResponse struct {
	Items []runtimepkg.EvalSuite `json:"items"`
	Count int                    `json:"count"`
}

func newEvalsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evals",
		Short: "Evaluation suite management",
	}
	cmd.AddCommand(
		newEvalsListCmd(),
		newEvalsRunCmd(),
	)
	return cmd
}

func newEvalsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List evaluation suites",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runEvalsList(cmd.Context())
		},
	}
}

func runEvalsList(ctx context.Context) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	items, err := loadEvalSuites(ctx, client)
	if err != nil {
		return err
	}
	if flagJSON {
		return printJSON(evalSuitesResponse{Items: items, Count: len(items)})
	}
	if len(items) == 0 {
		fmt.Println("No evaluation suites configured.")
		return nil
	}
	for _, suite := range items {
		name := suite.Name
		if name == "" {
			name = suite.ID
		}
		surface := suite.Surface
		if surface == "" {
			surface = "-"
		}
		fmt.Printf("  %s | %s | %d cases\n", name, surface, len(suite.Cases))
	}
	return nil
}

func newEvalsRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <suite>",
		Short: "Run an evaluation suite",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEvalsRun(cmd.Context(), args[0])
		},
	}
}

func runEvalsRun(ctx context.Context, suiteID string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	report, err := runEvalSuiteReport(ctx, client, suiteID)
	if err != nil {
		return err
	}
	if flagJSON {
		return printJSON(report)
	}
	fmt.Printf("Suite:   %s\n", report.Suite.ID)
	fmt.Printf("Status:  %s\n", report.Status)
	fmt.Printf("Cases:   %d\n", report.CaseCount)
	fmt.Printf("Passed:  %d\n", report.Passed)
	fmt.Printf("Failed:  %d\n", report.Failed)
	fmt.Printf("Errored: %d\n", report.Errored)
	return nil
}

func fetchEvalSuiteSummaries(ctx context.Context, client *GatewayClient) ([]replpkg.EvalSuiteSummary, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}

	items, err := loadEvalSuites(ctx, client)
	if err != nil {
		return nil, err
	}

	out := make([]replpkg.EvalSuiteSummary, 0, len(items))
	for _, suite := range items {
		name := strings.TrimSpace(suite.Name)
		if name == "" {
			name = strings.TrimSpace(suite.ID)
		}
		out = append(out, replpkg.EvalSuiteSummary{
			ID:        strings.TrimSpace(suite.ID),
			Name:      name,
			Surface:   strings.TrimSpace(suite.Surface),
			CaseCount: len(suite.Cases),
		})
	}
	return out, nil
}

func runEvalSuiteSummary(ctx context.Context, client *GatewayClient, suiteID string) (*replpkg.EvalRunSummary, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}

	report, err := runEvalSuiteReport(ctx, client, suiteID)
	if err != nil {
		return nil, err
	}

	return &replpkg.EvalRunSummary{
		SuiteID:   strings.TrimSpace(report.Suite.ID),
		Status:    strings.TrimSpace(report.Status),
		CaseCount: report.CaseCount,
		Passed:    report.Passed,
		Failed:    report.Failed,
		Errored:   report.Errored,
	}, nil
}
