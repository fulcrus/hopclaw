package discovery

import (
	"context"
	"fmt"
)

// Resolver discovers HopClaw peers on the network.
type Resolver interface {
	// Discover returns all known peers, probing each for liveness.
	Discover(ctx context.Context) ([]Peer, error)

	// Announce advertises this instance to the network. Implementations
	// that rely on external infrastructure (Tailscale, static lists) treat
	// this as a no-op.
	Announce(ctx context.Context, self Peer) error

	// Stop releases any resources held by the resolver.
	Stop() error
}

// NewResolver creates a Resolver for the configured discovery method.
func NewResolver(cfg Config) (Resolver, error) {
	switch cfg.Method {
	case MethodStatic:
		return newStaticResolver(cfg), nil
	case MethodTailscale:
		return newTailscaleResolver(cfg), nil
	case MethodMDNS:
		return newMDNSResolver(cfg)
	default:
		return nil, fmt.Errorf("unsupported discovery method %q", cfg.Method)
	}
}
