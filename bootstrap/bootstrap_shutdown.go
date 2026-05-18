package bootstrap

import (
	"context"

	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
)

func (a *App) Close(ctx context.Context) error {
	if a == nil {
		return nil
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	a.closed = true
	a.pluginWatchGen++

	a.stopConfigWatcher()
	a.stopAutomationServices()
	a.stopRuntimeInfrastructure(ctx)
	a.stopDiscoveryAndSurface(ctx)
	a.stopManagedHelpers(ctx)
	a.stopDynamicServices(ctx)
	a.stopPersistence()
	return nil
}

func (a *App) stopConfigWatcher() {
	if a.configWatcherStop != nil {
		a.configWatcherStop()
		a.configWatcherStop = nil
	}
}

func (a *App) stopAutomationServices() {
	if a.CronService != nil {
		if err := a.CronService.Stop(); err != nil {
			log.Warn("cron service stop failed", "error", err)
		}
	}
	if a.WatchService != nil {
		if err := a.WatchService.Stop(); err != nil {
			log.Warn("watch service stop failed", "error", err)
		}
	}
	if a.HeartbeatService != nil {
		a.HeartbeatService.Stop()
	}
	if a.WakeupService != nil {
		a.WakeupService.Stop()
	}
	if a.HealthMonitor != nil {
		a.HealthMonitor.Stop()
	}
}

func (a *App) stopRuntimeInfrastructure(ctx context.Context) {
	if a.Runtime != nil {
		if err := a.Runtime.Close(ctx); err != nil {
			log.Warn("runtime service stop failed", "error", err)
		}
	}
	if a.ApprovalTimeout != nil {
		a.ApprovalTimeout.Stop()
	}
	if a.ArtifactPruner != nil {
		a.ArtifactPruner.Stop()
	}
	if a.StatePruner != nil {
		a.StatePruner.Stop()
	}
	a.stopGovernanceInfrastructure()
}

func (a *App) stopGovernanceInfrastructure() {
	if a == nil {
		return
	}
	var current *controlgov.ReliableDispatcher
	if a.governanceControl != nil {
		current = a.governanceControl.current()
		a.governanceControl.Stop()
	}
	if a.governanceDispatcher != nil && a.governanceDispatcher != current {
		a.governanceDispatcher.Stop()
	}
	a.governanceDispatcher = nil
}

func (a *App) stopDiscoveryAndSurface(ctx context.Context) {
	if a.DiscoveryResolver != nil {
		if err := a.DiscoveryResolver.Stop(); err != nil {
			log.Warn("discovery resolver stop failed", "error", err)
		}
	}
	if a.NodeRegistry != nil {
		a.NodeRegistry.Stop()
	}
	if a.DevicePairing != nil {
		a.DevicePairing.Stop()
	}
	if a.CanvasHost != nil {
		if err := a.CanvasHost.Stop(ctx); err != nil {
			log.Warn("canvas host stop failed", "error", err)
		}
	}
}

func (a *App) stopManagedHelpers(ctx context.Context) {
	if a.ManagedHelpers == nil {
		return
	}
	if a.ManagedHelpers.Browser != nil {
		if err := a.ManagedHelpers.Browser.Stop(ctx); err != nil {
			log.Warn("managed browser helper stop failed", "error", err)
		}
	}
	if a.ManagedHelpers.Desktop != nil {
		if err := a.ManagedHelpers.Desktop.Stop(ctx); err != nil {
			log.Warn("managed desktop helper stop failed", "error", err)
		}
	}
}

func (a *App) stopDynamicServices(ctx context.Context) {
	if a.keychainWatcher != nil {
		a.keychainWatcher.Stop()
	}
	for _, bridge := range a.channelBridges {
		bridge.bridge.Stop()
	}
	if a.pluginWatchStop != nil {
		a.pluginWatchStop()
		a.pluginWatchStop = nil
	}
	if a.skillWatchStop != nil {
		a.skillWatchStop()
		a.skillWatchStop = nil
	}
	if a.processManager != nil {
		a.processManager.Stop(ctx)
	}
	if a.mcpRuntime != nil {
		if err := a.mcpRuntime.Stop(); err != nil {
			log.Warn("plugin mcp stop failed", "error", err)
		}
	}
	if a.Channels != nil {
		if err := a.Channels.DisconnectAll(ctx); err != nil {
			log.Warn("channel disconnect failed", "error", err)
		}
	}
}

func (a *App) stopPersistence() {
	if a.runtimeDB != nil {
		if err := a.runtimeDB.Close(); err != nil {
			log.Warn("runtime db close failed", "error", err)
		}
	}
	if a.storeDB != nil {
		if err := a.storeDB.Close(); err != nil {
			log.Warn("store db close failed", "error", err)
		}
	}
	if a.knowledgeDB != nil {
		if err := a.knowledgeDB.Close(); err != nil {
			log.Warn("knowledge db close failed", "error", err)
		}
	}
	if a.auditDB != nil {
		if err := a.auditDB.Close(); err != nil {
			log.Warn("audit db close failed", "error", err)
		}
	}
	if a.governanceDeliveryDB != nil &&
		a.governanceDeliveryDB != a.runtimeDB &&
		a.governanceDeliveryDB != a.storeDB &&
		a.governanceDeliveryDB != a.knowledgeDB &&
		a.governanceDeliveryDB != a.auditDB {
		if err := a.governanceDeliveryDB.Close(); err != nil {
			log.Warn("governance delivery db close failed", "error", err)
		}
	}
}
