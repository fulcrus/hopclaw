package bootstrap

import (
	"context"

	"github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	channelregistry "github.com/fulcrus/hopclaw/channels/registry"
	"github.com/fulcrus/hopclaw/plugin"
)

func activateOperatorChannels(
	ctx context.Context,
	channelManager *channelmgr.Manager,
	processManager *plugin.ProcessManager,
	installations []channelregistry.Installation,
	healthMonitor *health.Monitor,
	warnings *startupWarningCollector,
) []namedChannelBridge {
	activeBridges := make([]namedChannelBridge, 0, len(installations))
	if processManager != nil {
		for _, installation := range installations {
			if installation.ManagedProcess == nil {
				continue
			}
			if err := processManager.Supervise(installation.ManagedProcess.Config, installation.ManagedProcess.Spawn); err != nil {
				log.Warn("plugin channel supervise failed", "channel", installation.Name, "error", err)
			}
		}
	}
	if len(channelManager.Names()) > 0 {
		report := channelManager.ConnectAll(ctx)
		for name, err := range report.Failed {
			log.Error("channel connect failed", "channel", name, "error", err)
			recordChannelConnectWarning(warnings, name, err)
		}
		for _, installation := range installations {
			if !report.IsConnected(installation.Name) {
				continue
			}
			clearChannelConnectWarning(warnings, installation.Name)
			installation.Bridge.Start(ctx)
			activeBridges = append(activeBridges, namedChannelBridge{
				name:   installation.Name,
				bridge: installation.Bridge,
			})
		}
		if len(report.Connected) > 0 {
			log.Info("channels active", "count", len(report.Connected))
		}
	}

	if healthMonitor != nil {
		if err := healthMonitor.Start(ctx); err != nil {
			recordChannelHealthMonitorWarning(warnings, err)
			log.Warn("channel health monitor start failed", "error", err)
		} else if warnings != nil {
			warnings.Clear(channelHealthWarningComponent)
		}
	}
	return activeBridges
}
