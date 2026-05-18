package bootstrap

import (
	"context"
	"reflect"
	"testing"

	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/gateway"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/toolruntime"
)

type hostGatewayTargetStub struct {
	browserClient  *browserclient.Client
	desktopClient  *desktopclient.Client
	capabilities   *capregistry.Registry
	managedHelpers gateway.ManagedHelpersController
}

func (s *hostGatewayTargetStub) ApplyHostServices(services gateway.HostServices) {
	s.browserClient = services.BrowserClient
	s.desktopClient = services.DesktopClient
	s.capabilities = services.Capabilities
	s.managedHelpers = services.ManagedHelpers
}

func TestPreparedOperatorHostPackAppliesAcrossTargets(t *testing.T) {
	t.Parallel()

	browser := &browserclient.Client{}
	desktop := &desktopclient.Client{}
	capabilities := capregistry.New()
	helpers := &managedHelpers{Browser: &managedHelperSupervisor{name: "browser"}}
	pack := &preparedOperatorHostPack{
		managedHelpers: helpers,
		browserClient:  browser,
		desktopClient:  desktop,
		capabilities:   capabilities,
	}

	if got := firstPartyPackContributionIDs(pack); !reflect.DeepEqual(got, []string{firstPartyPackHost}) {
		t.Fatalf("firstPartyPackContributionIDs() = %#v", got)
	}

	target := &hostGatewayTargetStub{}
	pack.applyHostGateway(target)
	if target.browserClient != browser || target.desktopClient != desktop || target.capabilities != capabilities {
		t.Fatalf("gateway wiring = %#v", target)
	}
	if target.managedHelpers == nil {
		t.Fatal("expected managed helpers controller")
	}

	bindings := toolruntime.BuiltinsBindings{}
	pack.applyBuiltins(&bindings)
	if bindings.BrowserClient != browser || bindings.DesktopClient != desktop {
		t.Fatalf("builtin wiring = %#v", bindings)
	}

	module := pack.module()
	if module.Manifest().ID != firstPartyPackHost || module.Health(context.Background()).Status != modules.HealthReady {
		t.Fatalf("module = %#v health=%#v", module.Manifest(), module.Health(context.Background()))
	}
}
