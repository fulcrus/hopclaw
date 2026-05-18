package discovery

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"
)

// staticResolver discovers peers from a fixed list of addresses provided in
// the configuration. Each address is probed via HTTP GET /operator/status to
// determine liveness.
type staticResolver struct {
	peers []string
}

func newStaticResolver(cfg Config) *staticResolver {
	return &staticResolver{
		peers: cfg.Peers,
	}
}

// Discover probes every configured peer address and returns the results.
func (s *staticResolver) Discover(ctx context.Context) ([]Peer, error) {
	result := make([]Peer, 0, len(s.peers))
	now := time.Now().UTC()

	for _, addr := range s.peers {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		status, version := probePeer(ctx, addr)

		result = append(result, Peer{
			ID:      staticPeerID(addr),
			Name:    addr,
			Address: addr,
			Version: version,
			Status:  status,
			SeenAt:  now,
		})
	}

	return result, nil
}

// Announce is a no-op for static peer lists.
func (s *staticResolver) Announce(_ context.Context, _ Peer) error {
	return nil
}

// Stop is a no-op for static peer lists.
func (s *staticResolver) Stop() error {
	return nil
}

// staticPeerID derives a deterministic ID from a peer address using a
// truncated SHA-256 hash.
func staticPeerID(address string) string {
	h := sha256.Sum256([]byte(address))
	return fmt.Sprintf("static-%x", h[:8])
}
