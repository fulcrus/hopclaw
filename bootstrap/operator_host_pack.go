package bootstrap

import (
	"strings"

	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/gateway"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/toolruntime"
)

const firstPartyPackHost = "builtin:host-pack"

type hostGatewayTarget interface {
	ApplyHostServices(gateway.HostServices)
}

type preparedOperatorHostPack struct {
	managedHelpers *managedHelpers
	browserClient  *browserclient.Client
	desktopClient  *desktopclient.Client
	capabilities   *capregistry.Registry
}

func newPreparedOperatorHostPack(runtimeCore *preparedBootstrapRuntimeCore) *preparedOperatorHostPack {
	if runtimeCore == nil {
		return nil
	}
	return &preparedOperatorHostPack{
		managedHelpers: runtimeCore.managedHelpers,
		browserClient:  runtimeCore.browserClient,
		desktopClient:  runtimeCore.desktopClient,
		capabilities:   runtimeCore.capabilities,
	}
}

func (a *App) hostPackForState() *preparedOperatorHostPack {
	if a == nil {
		return nil
	}
	return &preparedOperatorHostPack{
		managedHelpers: a.ManagedHelpers,
		browserClient:  a.browserClient,
		desktopClient:  a.desktopClient,
		capabilities:   a.Capabilities,
	}
}

func (a *App) wireHostPackLocked() {
	if a == nil {
		return
	}
	applyFirstPartyPackContribution(nil, a.Gateway, a.builtins, a.hostPackForState())
	a.wireExtensionRegistryLocked()
}

func (p *preparedOperatorHostPack) packID() string {
	if p == nil {
		return ""
	}
	return firstPartyPackHost
}

func (p *preparedOperatorHostPack) moduleExposed() bool {
	return p != nil && (p.browserClient != nil || p.desktopClient != nil)
}

func (p *preparedOperatorHostPack) module() modules.StaticModule {
	details := map[string]any{
		"browser_client":  p != nil && p.browserClient != nil,
		"desktop_client":  p != nil && p.desktopClient != nil,
		"capabilities":    p != nil && p.capabilities != nil,
		"managed_helpers": p != nil && p.managedHelpers != nil,
	}

	missing := make([]string, 0, 2)
	if p == nil || (p.browserClient == nil && p.desktopClient == nil) {
		missing = append(missing, "host-clients")
	}
	if p == nil || p.capabilities == nil {
		missing = append(missing, "capability-registry")
	}

	hosts := make([]string, 0, 2)
	if p != nil && p.browserClient != nil {
		hosts = append(hosts, "browser")
	}
	if p != nil && p.desktopClient != nil {
		hosts = append(hosts, "desktop")
	}

	health := modules.HealthReport{
		Status:  modules.HealthReady,
		Summary: "Host integrations wired: " + strings.Join(hosts, ", "),
		Details: details,
	}
	if len(missing) > 0 {
		health.Status = modules.HealthDegraded
		health.Summary = "Missing host surfaces: " + strings.Join(missing, ", ")
	}

	return staticFirstPartyPackModule(
		firstPartyPackHost,
		"host-pack",
		"First-party browser and desktop host integration pack.",
		health,
	)
}

func (p *preparedOperatorHostPack) applySurface(*preparedBootstrapSurface) {}

func (p *preparedOperatorHostPack) applyGateway(gw *gateway.Gateway) {
	if gw == nil {
		return
	}
	gw.ApplyHostServices(gateway.HostServices{
		BrowserClient:  p.browserClient,
		DesktopClient:  p.desktopClient,
		Capabilities:   p.capabilities,
		ManagedHelpers: managedHelpersControllerFor(p.managedHelpers),
	})
}

func (p *preparedOperatorHostPack) applyHostGateway(target hostGatewayTarget) {
	if p == nil || target == nil {
		return
	}
	target.ApplyHostServices(gateway.HostServices{
		BrowserClient:  p.browserClient,
		DesktopClient:  p.desktopClient,
		Capabilities:   p.capabilities,
		ManagedHelpers: managedHelpersControllerFor(p.managedHelpers),
	})
}

func (p *preparedOperatorHostPack) applyBuiltins(bindings *toolruntime.BuiltinsBindings) {
	if p == nil || bindings == nil {
		return
	}
	bindings.BrowserClient = p.browserClient
	bindings.DesktopClient = p.desktopClient
}
