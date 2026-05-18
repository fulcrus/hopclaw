package bootstrap

import (
	"context"

	"github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/channels/webhook"
	"github.com/fulcrus/hopclaw/plugin"
)

func (a *App) applyPreparedSkillService(prepared *preparedSkillService) {
	if a == nil || prepared == nil {
		return
	}
	a.SkillService = prepared.service
	a.skillWatchStop = prepared.watchStop
	if a.skillBinder != nil {
		a.skillBinder.Swap(prepared.service)
	}
}

func (a *App) applyHostRuntime(prepared *preparedHostRuntime) {
	if a == nil || prepared == nil {
		return
	}
	a.ManagedHelpers = prepared.managedHelpers
	a.browserClient = prepared.browserClient
	a.desktopClient = prepared.desktopClient
	a.Capabilities = prepared.capabilities
}

func (a *App) applyChannelRuntimeState(
	manager *channelmgr.Manager,
	webhooks map[string]*webhook.Adapter,
	bridges []namedChannelBridge,
	processManager *plugin.ProcessManager,
) {
	if a == nil {
		return
	}
	a.Channels = manager
	a.Webhooks = cloneWebhookAdapters(webhooks)
	a.channelBridges = append([]namedChannelBridge(nil), bridges...)
	a.processManager = processManager
	if a.automationDeliverer != nil {
		a.automationDeliverer.SetChannels(manager)
	}
}

func (a *App) applyChannelHealthMonitor(ctx context.Context, monitor *health.Monitor) {
	if a == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if a.HealthMonitor != nil && a.HealthMonitor != monitor {
		a.HealthMonitor.Stop()
	}
	a.HealthMonitor = monitor
	a.wireSupportGatewayLocked()
	if monitor != nil {
		if err := monitor.Start(ctx); err != nil {
			log.Warn("channel health monitor start failed", "error", err)
		}
	}
}
