package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	dnsDiscoveryPeersPath = "/operator/discovery/peers"
	dnsDefaultTimeout     = 5
)

// ---------------------------------------------------------------------------
// Response types (mirror the gateway JSON shapes)
// ---------------------------------------------------------------------------

type dnsPeer struct {
	Address string `json:"address"`
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Latency string `json:"latency,omitempty"`
}

type dnsDiscoverResponse struct {
	Items []dnsPeer `json:"items"`
	Peers []dnsPeer `json:"peers,omitempty"`
	Count int       `json:"count"`
}

type dnsStatusResponse struct {
	Tailscale   bool     `json:"tailscale"`
	MDNS        bool     `json:"mdns"`
	StaticPeers []string `json:"static_peers,omitempty"`
}

type dnsSetupRequest struct {
	Tailscale   *bool    `json:"tailscale,omitempty"`
	StaticPeers []string `json:"static_peers,omitempty"`
}

type dnsSetupResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newDNSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dns",
		Short: "Manage DNS and peer discovery",
		Long:  "Check DNS/discovery status, discover peers, and configure discovery settings.",
	}
	cmd.AddCommand(
		newDNSStatusCmd(),
		newDNSDiscoverCmd(),
		newDNSSetupCmd(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// dns status
// ---------------------------------------------------------------------------

func newDNSStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show DNS/discovery status",
		Long:  "Show DNS/discovery configuration status including Tailscale and mDNS availability.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDNSStatus(cmd.Context())
		},
	}
}

func runDNSStatus(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp dnsStatusResponse
	if err := client.Get(ctx, dnsDiscoveryPeersPath+"/status", &resp); err != nil {
		// Fall back to local checks if gateway is unavailable.
		return runDNSStatusLocal()
	}

	if flagJSON {
		return printJSON(resp)
	}

	printDNSStatusInfo(resp)
	return nil
}

func runDNSStatusLocal() error {
	tsAvailable := isTailscaleAvailable()

	status := dnsStatusResponse{
		Tailscale: tsAvailable,
	}

	if flagJSON {
		return printJSON(status)
	}

	printDNSStatusInfo(status)
	return nil
}

func printDNSStatusInfo(status dnsStatusResponse) {
	tailscaleStr := "not available"
	if status.Tailscale {
		tailscaleStr = "available"
		if ver := getTailscaleVersion(); ver != "" {
			tailscaleStr = fmt.Sprintf("available (%s)", ver)
		}
	}
	fmt.Printf("Tailscale: %s\n", tailscaleStr)

	mdnsStr := "not configured"
	if status.MDNS {
		mdnsStr = "configured"
	}
	fmt.Printf("mDNS:      %s\n", mdnsStr)

	if len(status.StaticPeers) > 0 {
		fmt.Printf("Static peers: %d\n", len(status.StaticPeers))
		for _, p := range status.StaticPeers {
			fmt.Printf("  %s\n", p)
		}
	} else {
		fmt.Println("Static peers: none")
	}
}

// ---------------------------------------------------------------------------
// dns discover
// ---------------------------------------------------------------------------

func newDNSDiscoverCmd() *cobra.Command {
	var timeout int

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover peers",
		Long:  "Trigger peer discovery using configured resolvers and display discovered peers.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDNSDiscover(cmd.Context(), timeout)
		},
	}

	cmd.Flags().IntVar(&timeout, "timeout", dnsDefaultTimeout, "discovery timeout in seconds")

	return cmd
}

func runDNSDiscover(ctx context.Context, timeout int) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	path := fmt.Sprintf("%s?timeout=%d", dnsDiscoveryPeersPath, timeout)
	var resp dnsDiscoverResponse
	if err := client.Get(ctx, path, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	peers := resp.Items
	if len(peers) == 0 {
		peers = resp.Peers
	}
	if len(peers) == 0 {
		fmt.Println("no peers discovered")
		return nil
	}

	fmt.Printf("%-30s  %-10s  %-12s  %s\n", "ADDRESS", "STATUS", "VERSION", "LATENCY")
	fmt.Printf("%-30s  %-10s  %-12s  %s\n", "-------", "------", "-------", "-------")
	for _, p := range peers {
		version := "-"
		if p.Version != "" {
			version = p.Version
		}
		latency := "-"
		if p.Latency != "" {
			latency = p.Latency
		}
		fmt.Printf("%-30s  %-10s  %-12s  %s\n",
			truncate(p.Address, 30),
			p.Status,
			truncate(version, 12),
			latency,
		)
	}
	fmt.Printf("\nDiscovered: %d peers\n", resp.Count)
	return nil
}

// ---------------------------------------------------------------------------
// dns setup
// ---------------------------------------------------------------------------

func newDNSSetupCmd() *cobra.Command {
	var (
		tailscale bool
		static    string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure DNS/discovery settings",
		Long: `Configure DNS/discovery settings in the gateway config.

Examples:
  hopclaw dns setup --tailscale
  hopclaw dns setup --static "peer1:8080,peer2:8080"
  hopclaw dns setup --tailscale --static "peer1:8080"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tsFlag := cmd.Flags().Changed("tailscale")
			return runDNSSetup(cmd.Context(), tailscale, tsFlag, static)
		},
	}

	cmd.Flags().BoolVar(&tailscale, "tailscale", false, "enable Tailscale-based discovery")
	cmd.Flags().StringVar(&static, "static", "", "comma-separated list of static peer addresses")

	return cmd
}

func runDNSSetup(ctx context.Context, tailscale, tailscaleSet bool, static string) error {
	if tailscaleSet && tailscale {
		if !isTailscaleAvailable() {
			return fmt.Errorf("tailscale is not installed or not accessible")
		}
	}

	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	reqBody := dnsSetupRequest{}
	if tailscaleSet {
		reqBody.Tailscale = &tailscale
	}
	if static != "" {
		peers := strings.Split(static, ",")
		trimmed := make([]string, 0, len(peers))
		for _, p := range peers {
			if s := strings.TrimSpace(p); s != "" {
				trimmed = append(trimmed, s)
			}
		}
		reqBody.StaticPeers = trimmed
	}

	var resp dnsSetupResponse
	if err := client.Post(ctx, dnsDiscoveryPeersPath+"/setup", reqBody, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if resp.Message != "" {
		fmt.Println(resp.Message)
	} else {
		fmt.Println("DNS/discovery configuration updated")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func isTailscaleAvailable() bool {
	_, err := exec.LookPath("tailscale")
	return err == nil
}

func getTailscaleVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "tailscale", "version").Output()
	if err != nil {
		return ""
	}
	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}
