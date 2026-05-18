package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// status command
// ---------------------------------------------------------------------------

const statusTimeout = 5 * time.Second

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the running gateway status",
		Long:  "Query the local HopClaw gateway's /operator/status endpoint.",
		RunE:  runStatus,
	}
}

func runStatus(cmd *cobra.Command, _ []string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	client.HTTP.Timeout = statusTimeout
	addr := gatewayAccessFromClient(client).Address
	return runStatusWithClient(cmd.Context(), client, addr, cmd.OutOrStdout(), flagJSON)
}

func runStatusWithClient(parent context.Context, client *GatewayClient, addr string, out io.Writer, jsonOutput bool) error {
	ctx, cancel := context.WithTimeout(parent, statusTimeout)
	defer cancel()

	body, statusCode, err := fetchOperatorStatus(ctx, client)
	if err != nil {
		return fmt.Errorf("gateway is not running at %s: %w", addr, err)
	}
	if statusCode >= 400 {
		return fmt.Errorf("gateway at %s: %w", addr, gatewayHTTPError(statusCode, body))
	}

	if jsonOutput {
		if _, err := out.Write(body); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
		if len(body) == 0 || body[len(body)-1] != '\n' {
			fmt.Fprintln(out)
		}
		return nil
	}

	// Pretty-print the status.
	status, err := decodeOperatorStatus(body)
	if err != nil {
		// Not JSON — just print raw.
		if _, writeErr := out.Write(body); writeErr != nil {
			return fmt.Errorf("write response: %w", writeErr)
		}
		if len(body) == 0 || body[len(body)-1] != '\n' {
			fmt.Fprintln(out)
		}
		return nil
	}

	fmt.Fprintf(out, "Gateway: %s\n", addr)
	fmt.Fprintf(out, "Status:  %s\n", operatorStatusLabel(status))
	if status.Version != "" {
		fmt.Fprintf(out, "Version: %s\n", status.Version)
	}
	if status.Uptime != "" {
		fmt.Fprintf(out, "Uptime:  %s\n", status.Uptime)
	}
	if status.CapabilityCount > 0 {
		fmt.Fprintf(out, "Caps:    %d\n", status.CapabilityCount)
	}
	if status.ActiveRuns > 0 || status.QueuedRuns > 0 {
		fmt.Fprintf(out, "Runs:    %d active, %d queued\n", status.ActiveRuns, status.QueuedRuns)
	}
	if len(status.Channels) > 0 {
		fmt.Fprintf(out, "Channels:\n")
		for _, ch := range status.Channels {
			fmt.Fprintf(out, "  %-16s %s\n", ch.Name, ch.Status)
		}
	}
	if len(status.Update) > 0 {
		if current, _ := status.Update["current_channel"].(string); current != "" {
			fmt.Fprintf(out, "Channel: %s\n", current)
		}
		if upToDate, ok := status.Update["up_to_date"].(bool); ok && !upToDate {
			if latest, _ := status.Update["latest_version"].(string); latest != "" {
				fmt.Fprintf(out, "Update:  %s available\n", latest)
			} else {
				fmt.Fprintf(out, "Update:  available\n")
			}
			if url, _ := status.Update["update_url"].(string); url != "" {
				fmt.Fprintf(out, "Release: %s\n", url)
			}
		}
	}
	return nil
}

const defaultGatewayAddr = config.DefaultGatewayAddress

// resolveGatewayAddr returns the address to connect to. It tries:
// 1. Config file (if found) for server.address
// 2. Default address 127.0.0.1:16280
func resolveGatewayAddr() string {
	configPath := resolveConfigPath()
	if configPath == "" {
		return defaultGatewayAddr
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return defaultGatewayAddr
	}
	if cfg.Server.Address != "" {
		return cfg.Server.Address
	}
	return defaultGatewayAddr
}
