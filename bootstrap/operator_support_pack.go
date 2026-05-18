package bootstrap

import (
	"fmt"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/channels/allowlist"
	"github.com/fulcrus/hopclaw/channels/health"
	"github.com/fulcrus/hopclaw/discovery"
	"github.com/fulcrus/hopclaw/gateway"
	"github.com/fulcrus/hopclaw/hooks"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/isolation"
	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/sandbox"
	"github.com/fulcrus/hopclaw/toolruntime"
	"github.com/fulcrus/hopclaw/usage"
)

const firstPartyPackOperatorSupport = "builtin:operator-support-pack"

type supportGatewayTarget interface {
	ApplySupportServices(gateway.SupportServices)
}

type preparedOperatorSupportPack struct {
	approvals        approval.Store
	grantStore       *approval.GrantStore
	allowlistManager *allowlist.Manager
	sandboxRunner    *sandbox.Runner
	isolationManager *isolation.Manager
	healthMonitor    *health.Monitor
	hookExecutor     *hooks.Executor
	discovery        discovery.Resolver
	keychainWatcher  *keychain.Watcher
	usageStore       usage.Store
}

func newPreparedOperatorSupportPack(
	foundation *preparedBootstrapFoundation,
	runtimeCore *preparedBootstrapRuntimeCore,
	addons *preparedOperatorAddons,
) *preparedOperatorSupportPack {
	if addons == nil {
		return nil
	}
	var approvals approval.Store
	var grantStore *approval.GrantStore
	var usageStore usage.Store
	if foundation != nil {
		approvals = foundation.approvals
	}
	if runtimeCore != nil {
		grantStore = runtimeCore.grantStore
		usageStore = runtimeCore.usageStore
	}
	return &preparedOperatorSupportPack{
		approvals:        approvals,
		grantStore:       grantStore,
		allowlistManager: addons.allowlistManager,
		sandboxRunner:    addons.sandboxRunner,
		isolationManager: addons.isolationManager,
		healthMonitor:    addons.healthMonitor,
		hookExecutor:     addons.hookExecutor,
		discovery:        addons.discoveryResolver,
		keychainWatcher:  addons.keychainWatcher,
		usageStore:       usageStore,
	}
}

func (a *App) supportPackForState() *preparedOperatorSupportPack {
	if a == nil {
		return nil
	}
	services := a.supportServices()
	return &preparedOperatorSupportPack{
		approvals:        services.approvals,
		grantStore:       services.grantStore,
		allowlistManager: a.AllowlistManager,
		sandboxRunner:    a.SandboxRunner,
		isolationManager: a.IsolationManager,
		healthMonitor:    a.HealthMonitor,
		hookExecutor:     services.hooks,
		discovery:        services.discovery,
		keychainWatcher:  a.keychainWatcher,
		usageStore:       services.usageStore,
	}
}

func (a *App) wireSupportGatewayLocked() {
	if a == nil {
		return
	}
	applyFirstPartyPackContribution(nil, a.Gateway, nil, a.supportPackForState())
	a.wireExtensionRegistryLocked()
}

func (p *preparedOperatorSupportPack) packID() string {
	if p == nil {
		return ""
	}
	return firstPartyPackOperatorSupport
}

func (p *preparedOperatorSupportPack) moduleExposed() bool {
	return p != nil && (p.approvals != nil || p.grantStore != nil || p.allowlistManager != nil || p.sandboxRunner != nil || p.isolationManager != nil || p.healthMonitor != nil || p.hookExecutor != nil || p.discovery != nil || p.keychainWatcher != nil || p.usageStore != nil)
}

func (p *preparedOperatorSupportPack) module() modules.StaticModule {
	details := map[string]any{
		"approvals":        p != nil && p.approvals != nil,
		"grant_store":      p != nil && p.grantStore != nil,
		"allowlist":        p != nil && p.allowlistManager != nil,
		"sandbox":          p != nil && p.sandboxRunner != nil,
		"isolation":        p != nil && p.isolationManager != nil,
		"channel_health":   p != nil && p.healthMonitor != nil,
		"hooks":            p != nil && p.hookExecutor != nil,
		"discovery":        p != nil && p.discovery != nil,
		"keychain_watcher": p != nil && p.keychainWatcher != nil,
		"usage_store":      p != nil && p.usageStore != nil,
	}

	present := 0
	for _, enabled := range []bool{
		p != nil && p.approvals != nil,
		p != nil && p.grantStore != nil,
		p != nil && p.allowlistManager != nil,
		p != nil && p.sandboxRunner != nil,
		p != nil && p.isolationManager != nil,
		p != nil && p.healthMonitor != nil,
		p != nil && p.hookExecutor != nil,
		p != nil && p.discovery != nil,
		p != nil && p.keychainWatcher != nil,
		p != nil && p.usageStore != nil,
	} {
		if enabled {
			present++
		}
	}

	health := modules.HealthReport{
		Status:  modules.HealthReady,
		Summary: fmt.Sprintf("%d operator support surfaces wired.", present),
		Details: details,
	}
	if p == nil || p.approvals == nil {
		health.Status = modules.HealthDegraded
		health.Summary = "Operator support pack is missing approval storage."
	}

	return staticFirstPartyPackModule(
		firstPartyPackOperatorSupport,
		"operator-support-pack",
		"First-party operator support pack for approvals, hooks, discovery, isolation, and diagnostics support services.",
		health,
	)
}

func (p *preparedOperatorSupportPack) applySurface(surface *preparedBootstrapSurface) {
	if p == nil || surface == nil {
		return
	}
	surface.allowlistManager = p.allowlistManager
	surface.sandboxRunner = p.sandboxRunner
	surface.isolationManager = p.isolationManager
	surface.healthMonitor = p.healthMonitor
	surface.hookExecutor = p.hookExecutor
	surface.discoveryResolver = p.discovery
	surface.keychainWatcher = p.keychainWatcher
}

func (p *preparedOperatorSupportPack) applyGateway(gw *gateway.Gateway) {
	if gw == nil {
		return
	}
	gw.ApplySupportServices(gateway.SupportServices{
		Approvals:     p.approvals,
		GrantStore:    p.grantStore,
		Allowlist:     p.allowlistManager,
		Sandbox:       p.sandboxRunner,
		Discovery:     p.discovery,
		ChannelHealth: p.healthMonitor,
		Hooks:         p.hookExecutor,
		UsageStore:    p.usageStore,
	})
}

func (p *preparedOperatorSupportPack) applySupportGateway(target supportGatewayTarget) {
	if p == nil || target == nil {
		return
	}
	target.ApplySupportServices(gateway.SupportServices{
		Approvals:     p.approvals,
		GrantStore:    p.grantStore,
		Allowlist:     p.allowlistManager,
		Sandbox:       p.sandboxRunner,
		Discovery:     p.discovery,
		ChannelHealth: p.healthMonitor,
		Hooks:         p.hookExecutor,
		UsageStore:    p.usageStore,
	})
}

func (p *preparedOperatorSupportPack) applyBuiltins(*toolruntime.BuiltinsBindings) {}
