package bootstrap

import (
	"context"

	"github.com/fulcrus/hopclaw/channels/health"
	"github.com/fulcrus/hopclaw/config"
)

func (a *App) rebuildChannelHealthLocked(ctx context.Context, cfg config.Config) {
	if a == nil {
		return
	}
	if !enabledOrDefault(cfg.ChannelHealth.Enabled, true) {
		a.applyChannelHealthMonitor(ctx, nil)
		return
	}
	monitor := health.NewMonitor(health.Config{
		CheckInterval:      cfg.ChannelHealth.CheckInterval,
		StaleSocketTimeout: cfg.ChannelHealth.StaleSocketTimeout,
		StuckRunTimeout:    cfg.ChannelHealth.StuckRunTimeout,
		StartupGrace:       cfg.ChannelHealth.StartupGrace,
		MaxRestartsPerHour: cfg.ChannelHealth.MaxRestartsPerHour,
	}, a.Channels, a.Bus)
	a.applyChannelHealthMonitor(ctx, monitor)
}
