package cli

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const dashboardPath = "/dashboard/"

type dashboardOutput struct {
	URL       string `json:"url"`
	Gateway   string `json:"gateway"`
	AuthToken string `json:"auth_token,omitempty"`
}

func newDashboardCmd() *cobra.Command {
	var open bool
	cmd := &cobra.Command{
		Use:     "dashboard",
		Aliases: []string{"console"},
		Short:   "Show the dashboard URL for the current remote",
		Long:    "Print the HopClaw dashboard URL for the selected remote and optionally open it in your default browser.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDashboard(cmd, open)
		},
	}
	cmd.Flags().BoolVar(&open, "open", false, "open the dashboard in your default browser")
	return cmd
}

func runDashboard(cmd *cobra.Command, open bool) error {
	access, err := resolveDashboardAccess(cmd.Context(), flagRemote)
	if err != nil {
		return err
	}
	return runDashboardWithAccess(cmd, open, access)
}

func resolveDashboardAccess(ctx context.Context, targetName string) (gatewayAccess, error) {
	targetName = strings.TrimSpace(targetName)
	if targetName == "" {
		return resolveGatewayAccess()
	}
	return resolveGatewayAccessForTarget(ctx, targetName)
}

func runDashboardWithAccess(cmd *cobra.Command, open bool, access gatewayAccess) error {
	displayURL, openURL := dashboardURLs(access)
	output := dashboardOutput{
		URL:       displayURL,
		Gateway:   access.BaseURL,
		AuthToken: access.AuthToken,
	}

	if flagJSON {
		return printJSON(output)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Dashboard:  %s\n", output.URL)
	if output.AuthToken != "" {
		fmt.Fprintf(w, "Auth token: %s\n", maskDisplayToken(output.AuthToken))
	}

	if open {
		if err := openDashboardURL(openURL); err != nil {
			return err
		}
		fmt.Fprintln(w, "Opened the dashboard in your default browser.")
	}
	return nil
}

func openDashboardURL(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "linux":
		command = exec.Command("xdg-open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("opening URLs is not supported on %s", runtime.GOOS)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("open dashboard: %w", err)
	}
	return nil
}
