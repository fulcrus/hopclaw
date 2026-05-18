package bootstrap

import (
	"github.com/fulcrus/hopclaw/canvas"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/allowlist"
	"github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/channels/pairing"
	"github.com/fulcrus/hopclaw/channels/webhook"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/discovery"
	"github.com/fulcrus/hopclaw/gateway"
	gatewaynodes "github.com/fulcrus/hopclaw/gateway/nodes"
	"github.com/fulcrus/hopclaw/hooks"
	"github.com/fulcrus/hopclaw/isolation"
	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/plugin"
	"github.com/fulcrus/hopclaw/sandbox"
)

type appSurfaceSupportState struct {
	allowlistManager  *allowlist.Manager
	sandboxRunner     *sandbox.Runner
	isolationManager  *isolation.Manager
	healthMonitor     *health.Monitor
	hookExecutor      *hooks.Executor
	canvasHost        *canvas.Host
	discoveryResolver discovery.Resolver
	keychainWatcher   *keychain.Watcher
}

type appSurfaceIntegrationState struct {
	gateway         *gateway.Gateway
	channels        *channelmgr.Manager
	webhooks        map[string]*webhook.Adapter
	pluginInstaller *plugin.Installer
	processManager  *plugin.ProcessManager
}

type appSurfaceDeviceState struct {
	nodeRegistry   *gatewaynodes.Registry
	deviceStore    *deviceauth.Store
	devicePairing  *deviceauth.PairingManager
	threadBindings *channels.ThreadBinding
	pairingManager *pairing.Manager
	channelBridges []namedChannelBridge
}

func (s *preparedBootstrapSurface) automationState() appAutomationServices {
	if s == nil {
		return appAutomationServices{}
	}
	return appAutomationServices{
		cron:      s.cronService,
		watch:     s.watchService,
		heartbeat: s.heartbeatService,
		wire:      s.wireLogger,
		wakeup:    s.wakeupService,
		deliverer: s.automationDeliverer,
	}
}

func (s *preparedBootstrapSurface) supportState() appSurfaceSupportState {
	if s == nil {
		return appSurfaceSupportState{}
	}
	return appSurfaceSupportState{
		allowlistManager:  s.allowlistManager,
		sandboxRunner:     s.sandboxRunner,
		isolationManager:  s.isolationManager,
		healthMonitor:     s.healthMonitor,
		hookExecutor:      s.hookExecutor,
		canvasHost:        s.canvasHost,
		discoveryResolver: s.discoveryResolver,
		keychainWatcher:   s.keychainWatcher,
	}
}

func (s *preparedBootstrapSurface) integrationState() appSurfaceIntegrationState {
	if s == nil {
		return appSurfaceIntegrationState{}
	}
	return appSurfaceIntegrationState{
		gateway:         s.gateway,
		channels:        s.channels,
		webhooks:        s.webhooks,
		pluginInstaller: s.pluginInstaller,
		processManager:  s.processManager,
	}
}

func (s *preparedBootstrapSurface) deviceState() appSurfaceDeviceState {
	if s == nil {
		return appSurfaceDeviceState{}
	}
	return appSurfaceDeviceState{
		nodeRegistry:   s.nodeRegistry,
		deviceStore:    s.deviceStore,
		devicePairing:  s.devicePairing,
		threadBindings: s.threadBindings,
		pairingManager: s.pairingManager,
		channelBridges: append([]namedChannelBridge(nil), s.channelBridges...),
	}
}

func (a *App) applySurfaceAutomationState(services appAutomationServices) {
	if a == nil {
		return
	}
	a.CronService = services.cron
	a.WatchService = services.watch
	a.HeartbeatService = services.heartbeat
	a.WireLogger = services.wire
	a.WakeupService = services.wakeup
	a.automationDeliverer = services.deliverer
	if a.automationDeliverer != nil {
		a.automationDeliverer.SetChannels(a.Channels)
	}
}

func (a *App) applySurfaceSupportState(state appSurfaceSupportState) {
	if a == nil {
		return
	}
	a.AllowlistManager = state.allowlistManager
	a.SandboxRunner = state.sandboxRunner
	a.IsolationManager = state.isolationManager
	a.HealthMonitor = state.healthMonitor
	a.HookExecutor = state.hookExecutor
	a.CanvasHost = state.canvasHost
	a.DiscoveryResolver = state.discoveryResolver
	a.keychainWatcher = state.keychainWatcher
}

func (a *App) applySurfaceIntegrationState(state appSurfaceIntegrationState) {
	if a == nil {
		return
	}
	a.Gateway = state.gateway
	a.Channels = state.channels
	a.Webhooks = state.webhooks
	a.PluginInstaller = state.pluginInstaller
	a.processManager = state.processManager
	if a.automationDeliverer != nil {
		a.automationDeliverer.SetChannels(state.channels)
	}
	if state.gateway != nil {
		a.Handler = state.gateway.Handler()
	}
}

func (a *App) applySurfaceDeviceState(state appSurfaceDeviceState) {
	if a == nil {
		return
	}
	a.NodeRegistry = state.nodeRegistry
	a.DeviceStore = state.deviceStore
	a.DevicePairing = state.devicePairing
	a.threadBindings = state.threadBindings
	a.pairingManager = state.pairingManager
	a.channelBridges = state.channelBridges
}
