package bootstrap

import (
	"context"

	"github.com/fulcrus/hopclaw/audit"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/gateway"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlaudit "github.com/fulcrus/hopclaw/internal/controlplane/auditsink"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	"github.com/fulcrus/hopclaw/policy"
	"github.com/fulcrus/hopclaw/server"
	"github.com/fulcrus/hopclaw/store"
)

func newGatewayMutationService(configStore *store.ConfigStore, resolver controloverlay.Provider, refresh func(context.Context) error) *controloverlay.MutationService {
	return controloverlay.NewMutationService(
		configStore,
		resolver,
		controloverlay.MutationOptions{Refresh: refresh},
	)
}

func currentGatewayWSHandler(gw *gateway.Gateway) *gateway.WSHandler {
	if gw == nil {
		return nil
	}
	return gw.WSHandler()
}

func (a *App) gatewayKernelSurfaceLocked(
	cfg config.Config,
	policyEngine policy.Engine,
	approvalRegistry *controlapproval.ProviderRegistry,
	governanceRegistry *controlgov.AdapterRegistry,
	auditRegistry *controlaudit.Registry,
	auditDelivery audit.DeliveryController,
	resolver controloverlay.Provider,
	configStore *store.ConfigStore,
) *preparedOperatorKernelSurface {
	if a == nil || a.Gateway == nil {
		return nil
	}
	current := effectiveSnapshotRuntimeState{}
	if a.snapshotState != nil {
		current = a.snapshotState.current()
	}
	if policyEngine == nil {
		policyEngine = current.policyEngine
	}
	if approvalRegistry == nil {
		approvalRegistry = current.approvalRegistry
	}
	if governanceRegistry == nil {
		governanceRegistry = current.governanceRegistry
	}
	if auditRegistry == nil {
		auditRegistry = current.auditRegistry
	}
	if resolver == nil {
		resolver = a.effectiveConfig
	}
	if configStore == nil {
		configStore = a.ConfigStore
	}

	wsHandler := currentGatewayWSHandler(a.Gateway)
	if wsHandler == nil {
		wsHandler = gateway.NewWSHandler(a.Gateway, a.NodeRegistry)
	}

	return &preparedOperatorKernelSurface{
		approvalRegistry:   approvalRegistry,
		governanceRegistry: governanceRegistry,
		auditRegistry:      auditRegistry,
		auditDelivery:      auditDelivery,
		policyEngine:       policyEngine,
		credentials:        currentEffectiveSecretInventory(cfg, resolver),
		threadBindings:     a.threadBindings,
		wsHandler:          wsHandler,
		deviceStore:        a.DeviceStore,
		devicePairing:      a.DevicePairing,
		configStore:        configStore,
		pluginRuntime:      a,
		effectiveConfig:    resolver,
		configMutation:     newGatewayMutationService(configStore, resolver, a.RefreshEffectiveConfig),
		configReloader:     a.RefreshEffectiveConfig,
		operationalWarning: a.operationalWarnings,
	}
}

func currentEffectiveSecretInventory(cfg config.Config, resolver controloverlay.Provider) config.SecretRefInventory {
	if resolver == nil {
		return cfg.SecretInventory()
	}
	if typed, ok := resolver.(*controloverlay.Resolver); ok && typed == nil {
		return cfg.SecretInventory()
	}
	return resolver.Current().SecretInventory()
}

func (a *App) wireGatewayRuntimeKernelLocked(configStore *store.ConfigStore) {
	surface := a.gatewayKernelSurfaceLocked(a.Config, nil, nil, nil, nil, a.auditSink, a.effectiveConfig, configStore)
	if surface == nil {
		return
	}
	surface.applyGatewaySurface(a.Gateway)
}

func (a *App) wireGatewayControlPlaneLocked(
	policyEngine policy.Engine,
	approvalRegistry *controlapproval.ProviderRegistry,
	governanceRegistry *controlgov.AdapterRegistry,
	auditRegistry *controlaudit.Registry,
) {
	surface := a.gatewayKernelSurfaceLocked(a.Config, policyEngine, approvalRegistry, governanceRegistry, auditRegistry, a.auditSink, a.effectiveConfig, a.ConfigStore)
	if surface == nil {
		return
	}
	surface.applyGatewaySurface(a.Gateway)
}

func (a *App) wireGatewayEffectiveConfigLocked(cfg config.Config, resolver controloverlay.Provider) {
	surface := a.gatewayKernelSurfaceLocked(cfg, nil, nil, nil, nil, a.auditSink, resolver, a.ConfigStore)
	if surface == nil {
		return
	}
	surface.applyGatewaySurface(a.Gateway)
}

func wireRuntimeRoutesApprovalCallbacks(runtimeRoutes *server.Server, approvalRegistry *controlapproval.ProviderRegistry) {
	if runtimeRoutes == nil || approvalRegistry == nil {
		return
	}
	runtimeRoutes.SetApprovalCallbacks(approvalRegistry.CallbackPolicies())
}

func (a *App) wireGatewayConfigWatcherLocked(watcher *config.Watcher, path string) {
	if a == nil || a.Gateway == nil {
		return
	}
	a.Gateway.ApplyConfigServices(gateway.ConfigServices{
		Watcher: watcher,
		Path:    path,
	})
}
