package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	nodesDiscoveryPath = "/operator/discovery/peers"
	nodeStatusPath     = "/operator/status"
)

// ---------------------------------------------------------------------------
// Response types (mirror the gateway JSON shapes)
// ---------------------------------------------------------------------------

type nodeInfo struct {
	Address  string    `json:"address"`
	Status   string    `json:"status"`
	Version  string    `json:"version,omitempty"`
	Uptime   string    `json:"uptime,omitempty"`
	LastSeen time.Time `json:"last_seen,omitempty"`
}

type nodesListResponse struct {
	Items []nodeInfo `json:"items"`
	Nodes []nodeInfo `json:"nodes,omitempty"`
	Count int        `json:"count"`
}

type nodeStatusResponse struct {
	Address string `json:"address"`
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Uptime  string `json:"uptime,omitempty"`
}

type nodePingResponse struct {
	Address string `json:"address"`
	Status  string `json:"status"`
	Latency string `json:"latency"`
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newNodesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "nodes",
		Short: "Manage cluster nodes",
		Long:  "List, inspect, and ping peer nodes in the cluster.",
	}
	cmd.AddCommand(
		newNodesListCmd(),
		newNodesStatusCmd(),
		newNodesPingCmd(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// nodes list
// ---------------------------------------------------------------------------

func newNodesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List cluster nodes",
		Long:  "List all discovered peer nodes in the cluster.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runNodesList(cmd.Context())
		},
	}
}

func runNodesList(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp nodesListResponse
	if err := client.Get(ctx, nodesDiscoveryPath, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	nodes := resp.Items
	if len(nodes) == 0 {
		nodes = resp.Nodes
	}
	if len(nodes) == 0 {
		fmt.Println("no nodes found")
		return nil
	}

	fmt.Printf("%-30s  %-10s  %-12s  %s\n", "ADDRESS", "STATUS", "VERSION", "LAST SEEN")
	fmt.Printf("%-30s  %-10s  %-12s  %s\n", "-------", "------", "-------", "---------")
	for _, n := range nodes {
		version := "-"
		if n.Version != "" {
			version = n.Version
		}
		lastSeen := "-"
		if !n.LastSeen.IsZero() {
			lastSeen = formatTime(n.LastSeen)
		}
		fmt.Printf("%-30s  %-10s  %-12s  %s\n",
			truncate(n.Address, 30),
			n.Status,
			truncate(version, 12),
			lastSeen,
		)
	}
	fmt.Printf("\nTotal: %d nodes\n", resp.Count)
	return nil
}

// ---------------------------------------------------------------------------
// nodes status
// ---------------------------------------------------------------------------

func newNodesStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <address>",
		Short: "Show node status",
		Long:  "Get detailed status information from a specific peer node.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNodesStatus(cmd.Context(), args[0])
		},
	}
}

func runNodesStatus(ctx context.Context, address string) error {
	peerClient := &GatewayClient{
		BaseURL: "http://" + address,
		HTTP:    &http.Client{Timeout: gatewayClientTimeout},
	}

	var resp nodeStatusResponse
	if err := peerClient.Get(ctx, nodeStatusPath, &resp); err != nil {
		return fmt.Errorf("failed to reach node %s: %w", address, err)
	}
	resp.Address = address

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Address: %s\n", resp.Address)
	fmt.Printf("Status:  %s\n", resp.Status)
	if resp.Version != "" {
		fmt.Printf("Version: %s\n", resp.Version)
	}
	if resp.Uptime != "" {
		fmt.Printf("Uptime:  %s\n", resp.Uptime)
	}
	return nil
}

// ---------------------------------------------------------------------------
// nodes ping
// ---------------------------------------------------------------------------

func newNodesPingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ping <address>",
		Short: "Ping a node",
		Long:  "Send an HTTP ping to a peer node and display the round-trip latency.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNodesPing(cmd.Context(), args[0])
		},
	}
}

func runNodesPing(ctx context.Context, address string) error {
	peerClient := &GatewayClient{
		BaseURL: "http://" + address,
		HTTP:    &http.Client{Timeout: gatewayClientTimeout},
	}

	start := time.Now()
	var resp nodeStatusResponse
	err := peerClient.Get(ctx, nodeStatusPath, &resp)
	elapsed := time.Since(start)

	if err != nil {
		return fmt.Errorf("failed to ping node %s: %w", address, err)
	}

	pingResp := nodePingResponse{
		Address: address,
		Status:  resp.Status,
		Latency: elapsed.String(),
	}

	if flagJSON {
		return printJSON(pingResp)
	}

	fmt.Printf("Ping %s: %s (status: %s)\n", address, elapsed, resp.Status)
	return nil
}
