package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ---------------------------------------------------------------------------
// Tailscale Integration — Expose Gateway via Tailscale serve/funnel
// ---------------------------------------------------------------------------

// Mode defines how the gateway is exposed via Tailscale.
type Mode string

const (
	ModeServe  Mode = "serve"  // Tailscale network only
	ModeFunnel Mode = "funnel" // Public internet (HTTPS)
)

// Exposer manages Tailscale serve/funnel for the Gateway.
type Exposer struct {
	mode Mode
	port int
}

// NewExposer creates a new Tailscale exposer.
func NewExposer(mode Mode, port int) *Exposer {
	return &Exposer{mode: mode, port: port}
}

// IsAvailable checks if Tailscale is installed and connected.
func IsAvailable(ctx context.Context) bool {
	return exec.CommandContext(ctx, "tailscale", "status").Run() == nil
}

// Start begins exposing the gateway via Tailscale.
func (t *Exposer) Start(ctx context.Context) error {
	if !IsAvailable(ctx) {
		return fmt.Errorf("tailscale is not connected")
	}

	cmd := string(t.mode)
	err := exec.CommandContext(ctx, "tailscale", cmd,
		fmt.Sprintf("https+insecure://localhost:%d", t.port),
	).Start()
	if err != nil {
		return fmt.Errorf("tailscale %s: %w", cmd, err)
	}

	return nil
}

// Stop halts the Tailscale exposure.
func (t *Exposer) Stop(ctx context.Context) error {
	cmd := string(t.mode)
	return exec.CommandContext(ctx, "tailscale", cmd, "off").Run()
}

// GetURL returns the Tailscale MagicDNS URL for the exposed service.
func (t *Exposer) GetURL(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil {
		return "", fmt.Errorf("tailscale status: %w", err)
	}

	var status struct {
		Self struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return "", fmt.Errorf("tailscale: parse status: %w", err)
	}

	dnsName := strings.TrimSuffix(status.Self.DNSName, ".")
	if dnsName == "" {
		return "", fmt.Errorf("tailscale: no DNS name found")
	}

	scheme := "https"
	if t.mode == ModeServe {
		scheme = "https" // Tailscale serve always uses HTTPS
	}
	return fmt.Sprintf("%s://%s", scheme, dnsName), nil
}

// Status returns the current Tailscale exposure status.
func (t *Exposer) Status() map[string]any {
	ctx := context.Background()
	return map[string]any{
		"mode":      string(t.mode),
		"port":      t.port,
		"available": IsAvailable(ctx),
	}
}

// FunnelURL attempts to get the funnel URL by parsing tailscale serve status.
func FunnelURL(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "tailscale", "serve", "status").Output()
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") {
			return line, nil
		}
	}
	return "", fmt.Errorf("no funnel URL found")
}
