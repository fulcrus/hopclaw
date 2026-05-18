package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/fulcrus/hopclaw/logging"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// health command
// ---------------------------------------------------------------------------

const healthTimeout = 5 * time.Second

func newHealthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check gateway health",
		Long:  "Perform a health check against the running HopClaw gateway.",
		RunE:  runHealth,
	}
}

func runHealth(cmd *cobra.Command, _ []string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}
	client.HTTP.Timeout = healthTimeout
	addr := gatewayAccessFromClient(client).Address

	healthy, err := runHealthWithClient(cmd.Context(), client, addr, cmd.OutOrStdout(), flagJSON)
	if err != nil {
		return err
	}
	if !healthy {
		return silentExitError(1)
	}
	return nil
}

func runHealthWithClient(parent context.Context, client *GatewayClient, addr string, out io.Writer, jsonOutput bool) (bool, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, healthTimeout)
	defer cancel()

	body, statusCode, err := fetchGatewayHealth(ctx, client)
	if err != nil {
		if jsonOutput {
			writeHealthJSON(out, healthCommandResponse{
				Healthy: false,
				Address: addr,
				Error:   err.Error(),
			})
			return false, nil
		}
		fmt.Fprintf(out, "UNHEALTHY: gateway at %s is not reachable: %v\n", addr, err)
		return false, nil
	}

	if statusCode >= 400 {
		errMessage := gatewayHTTPError(statusCode, body).Error()
		if jsonOutput {
			writeHealthJSON(out, healthCommandResponse{
				Healthy: false,
				Address: addr,
				Error:   errMessage,
			})
			return false, nil
		}
		fmt.Fprintf(out, "UNHEALTHY: %s\n", errMessage)
		return false, nil
	}

	status, err := decodeGatewayHealth(body)
	if err != nil {
		errMessage := fmt.Sprintf("cannot parse gateway health response: %v", err)
		if jsonOutput {
			writeHealthJSON(out, healthCommandResponse{
				Healthy: false,
				Address: addr,
				Error:   errMessage,
			})
			return false, nil
		}
		fmt.Fprintf(out, "UNHEALTHY: %s\n", errMessage)
		return false, nil
	}

	state := gatewayHealthLabel(status)
	summary := gatewayHealthSummary(status)
	healthy := state == "ready" && status.OK
	if jsonOutput {
		writeHealthJSON(out, healthCommandResponse{
			Healthy:  healthy,
			Address:  addr,
			State:    state,
			Summary:  summary,
			Warnings: append([]string(nil), status.Warnings...),
		})
		return healthy, nil
	}

	if healthy {
		fmt.Fprintf(out, "HEALTHY: gateway at %s is ready (HTTP %d)\n", addr, statusCode)
		return true, nil
	}
	if state == "degraded" {
		fmt.Fprintf(out, "DEGRADED: gateway at %s needs attention: %s\n", addr, summary)
		return false, nil
	}
	fmt.Fprintf(out, "UNHEALTHY: gateway at %s reported %s\n", addr, summary)
	return false, nil
}

type healthCommandResponse struct {
	Healthy  bool     `json:"healthy"`
	Address  string   `json:"address"`
	State    string   `json:"state,omitempty"`
	Summary  string   `json:"summary,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func writeHealthJSON(w io.Writer, payload healthCommandResponse) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	logging.LogIfErr(context.Background(), enc.Encode(payload), "encode health response failed")
}
