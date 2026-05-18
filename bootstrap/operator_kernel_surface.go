package bootstrap

import (
	"context"

	"github.com/fulcrus/hopclaw/audit"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/gateway"
	gatewaynodes "github.com/fulcrus/hopclaw/gateway/nodes"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlaudit "github.com/fulcrus/hopclaw/internal/controlplane/auditsink"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	"github.com/fulcrus/hopclaw/policy"
	"github.com/fulcrus/hopclaw/store"
)

type preparedOperatorKernelSurface struct {
	approvalRegistry   *controlapproval.ProviderRegistry
	governanceRegistry *controlgov.AdapterRegistry
	auditRegistry      *controlaudit.Registry
	auditDelivery      audit.DeliveryController
	policyEngine       policy.Engine
	credentials        config.SecretRefInventory
	threadBindings     *channels.ThreadBinding
	wsHandler          *gateway.WSHandler
	deviceStore        *deviceauth.Store
	devicePairing      *deviceauth.PairingManager
	configStore        *store.ConfigStore
	pluginRuntime      gateway.PluginRuntimeController
	effectiveConfig    controloverlay.Provider
	configMutation     *controloverlay.MutationService
	configReloader     func(context.Context) error
	operationalWarning controlplane.OperationalWarningSource
}

func newPreparedOperatorKernelSurface(
	cfg config.Config,
	foundation *preparedBootstrapFoundation,
	runtimeCore *preparedBootstrapRuntimeCore,
	gw *gateway.Gateway,
	threadBindings *channels.ThreadBinding,
	nodeRegistry *gatewaynodes.Registry,
	deviceStore *deviceauth.Store,
	devicePairing *deviceauth.PairingManager,
) *preparedOperatorKernelSurface {
	if runtimeCore == nil {
		return nil
	}
	var configStore *store.ConfigStore
	var auditDelivery audit.DeliveryController
	var operationalWarning controlplane.OperationalWarningSource
	if foundation != nil {
		configStore = foundation.configStore
		auditDelivery = foundation.auditSink
		operationalWarning = foundation.operationalWarnings
	}
	return &preparedOperatorKernelSurface{
		approvalRegistry:   runtimeCore.approvalRegistry,
		governanceRegistry: runtimeCore.governanceRegistry,
		auditRegistry:      runtimeCore.auditRegistry,
		auditDelivery:      auditDelivery,
		policyEngine:       runtimeCore.policyEngine,
		credentials:        currentEffectiveSecretInventory(cfg, runtimeCore.effectiveConfig),
		threadBindings:     threadBindings,
		wsHandler:          gateway.NewWSHandler(gw, nodeRegistry),
		deviceStore:        deviceStore,
		devicePairing:      devicePairing,
		configStore:        configStore,
		operationalWarning: operationalWarning,
	}
}

func (p *preparedOperatorKernelSurface) applyGatewaySurface(gw *gateway.Gateway) {
	if p == nil || gw == nil {
		return
	}
	gw.ApplyKernelServices(gateway.KernelServices{
		ApprovalProviders:   p.approvalRegistry,
		GovernanceAdapters:  p.governanceRegistry,
		AuditSinks:          p.auditRegistry,
		AuditDelivery:       p.auditDelivery,
		PolicyEngine:        p.policyEngine,
		Credentials:         p.credentials,
		ThreadBindings:      p.threadBindings,
		WSHandler:           p.wsHandler,
		DeviceStore:         p.deviceStore,
		DevicePairing:       p.devicePairing,
		ConfigStore:         p.configStore,
		PluginRuntime:       p.pluginRuntime,
		EffectiveConfig:     p.effectiveConfig,
		ConfigMutator:       p.configMutation,
		ConfigReloader:      p.configReloader,
		OperationalWarnings: p.operationalWarning,
	})
}
