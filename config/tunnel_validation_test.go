package config

import (
	"strings"
	"testing"
)

func TestValidateRejectsEnabledTunnel(t *testing.T) {
	t.Parallel()

	enabled := true
	cfg := Config{}
	cfg.ApplyDefaults()
	cfg.Store.Backend = "memory"
	cfg.Tunnel.Enabled = &enabled

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected tunnel config validation error")
	}
	if !strings.Contains(err.Error(), "tunnel is enabled but neither host") {
		t.Fatalf("unexpected error: %v", err)
	}
}
