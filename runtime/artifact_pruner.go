package runtime

import (
	"context"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Artifact auto-pruner
// ---------------------------------------------------------------------------

const (
	defaultPruneInterval = 1 * time.Hour
)

// ArtifactPruner periodically calls PruneArtifacts on the runtime service
// to remove expired artifacts based on the configured retention policy.
type ArtifactPruner struct {
	service  *Service
	interval time.Duration
	mu       sync.Mutex
	cancel   context.CancelFunc
}

// NewArtifactPruner creates a new pruner. Interval is the time between prune
// cycles; zero uses the default of 1 hour.
func NewArtifactPruner(service *Service, interval time.Duration) *ArtifactPruner {
	if interval <= 0 {
		interval = defaultPruneInterval
	}
	return &ArtifactPruner{
		service:  service,
		interval: interval,
	}
}

// Start begins the background prune loop. Repeated calls while the loop is
// already running are ignored.
func (p *ArtifactPruner) Start(ctx context.Context) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.cancel != nil {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.mu.Unlock()
	go p.loop(ctx)
}

// Stop cancels the background prune loop.
func (p *ArtifactPruner) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (p *ArtifactPruner) loop(ctx context.Context) {
	ticker := p.service.runtimeClock().NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			result, err := p.service.PruneArtifacts(ctx, ArtifactPruneRequest{})
			if err != nil {
				log.Warn("artifact auto-prune failed", "error", err)
				continue
			}
			if result != nil && result.DeletedCount > 0 {
				log.Info("artifact auto-prune completed", "deleted", result.DeletedCount)
			}
		}
	}
}
