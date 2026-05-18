package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// tailscaleBinary is the expected name of the Tailscale CLI binary.
const tailscaleBinary = "tailscale"

// tailscaleResolver discovers peers by querying the local Tailscale daemon
// for other nodes on the tailnet, then probing each for a running HopClaw
// instance.
type tailscaleResolver struct {
	service string
}

func newTailscaleResolver(cfg Config) *tailscaleResolver {
	svc := cfg.Service
	if svc == "" {
		svc = defaultServiceName
	}
	return &tailscaleResolver{
		service: svc,
	}
}

// ---------------------------------------------------------------------------
// tailscale status JSON types
// ---------------------------------------------------------------------------

// tailscaleStatus represents the top-level output of `tailscale status --json`.
type tailscaleStatus struct {
	Peer map[string]tailscaleNode `json:"Peer"` // Tailscale API uses PascalCase
}

// tailscaleNode represents a single peer in the tailscale status output.
type tailscaleNode struct {
	ID           string   `json:"ID"` // Tailscale API uses PascalCase
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
	OS           string   `json:"OS"`
}

// ---------------------------------------------------------------------------
// Resolver interface
// ---------------------------------------------------------------------------

// Discover queries the local Tailscale daemon for peers, then probes each
// reachable peer for a running HopClaw instance on /operator/status.
func (t *tailscaleResolver) Discover(ctx context.Context) ([]Peer, error) {
	nodes, err := t.tailscaleNodes(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]Peer, 0, len(nodes))
	now := time.Now().UTC()

	for _, node := range nodes {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		if !node.Online || len(node.TailscaleIPs) == 0 {
			result = append(result, Peer{
				ID:      fmt.Sprintf("ts-%s", node.ID),
				Name:    node.HostName,
				Address: node.DNSName,
				Status:  StatusOffline,
				SeenAt:  now,
			})
			continue
		}

		// Probe on the default HopClaw port using the first Tailscale IP.
		addr := fmt.Sprintf("%s:%s", node.TailscaleIPs[0], defaultProbePort)
		status, version := probePeer(ctx, addr)

		result = append(result, Peer{
			ID:      fmt.Sprintf("ts-%s", node.ID),
			Name:    node.HostName,
			Address: addr,
			Version: version,
			Status:  status,
			SeenAt:  now,
		})
	}

	return result, nil
}

// Announce is a no-op — Tailscale handles network presence automatically.
func (t *tailscaleResolver) Announce(_ context.Context, _ Peer) error {
	return nil
}

// Stop is a no-op for the Tailscale resolver.
func (t *tailscaleResolver) Stop() error {
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// defaultProbePort is the HopClaw HTTP port probed on discovered Tailscale
// nodes. Matches the default server address in config.
const defaultProbePort = "16280"

// tailscaleNodes shells out to `tailscale status --json` and parses the
// peer list.
func (t *tailscaleResolver) tailscaleNodes(ctx context.Context) ([]tailscaleNode, error) {
	binPath, err := exec.LookPath(tailscaleBinary)
	if err != nil {
		return nil, fmt.Errorf("tailscale binary not found: %w", err)
	}

	cmd := exec.CommandContext(ctx, binPath, "status", "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tailscale status failed: %w", err)
	}

	var ts tailscaleStatus
	if err := json.Unmarshal(out, &ts); err != nil {
		return nil, fmt.Errorf("failed to parse tailscale status: %w", err)
	}

	nodes := make([]tailscaleNode, 0, len(ts.Peer))
	for _, node := range ts.Peer {
		nodes = append(nodes, node)
	}
	return nodes, nil
}
