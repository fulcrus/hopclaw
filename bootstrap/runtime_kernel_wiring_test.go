package bootstrap

import (
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/gateway"
	gatewaynodes "github.com/fulcrus/hopclaw/gateway/nodes"
)

func TestWireGatewayRuntimeKernelLockedInstallsWSHandlerWhenMissing(t *testing.T) {
	t.Parallel()

	gw := gateway.New(http.NotFoundHandler(), http.NotFoundHandler(), gateway.Config{})
	app := &App{
		AppConfigState: AppConfigState{
			Config: config.Config{},
		},
		AppSurfaceState: AppSurfaceState{
			Gateway:      gw,
			NodeRegistry: gatewaynodes.NewRegistry(),
		},
		appInternalState: appInternalState{
			threadBindings: channels.NewThreadBinding(),
		},
	}

	if gw.WSHandler() != nil {
		t.Fatal("expected gateway websocket handler to start nil")
	}

	app.wireGatewayRuntimeKernelLocked(nil)

	if gw.WSHandler() == nil {
		t.Fatal("expected runtime kernel wiring to install websocket handler")
	}
}

func TestWireGatewayRuntimeKernelLockedPreservesExistingWSHandler(t *testing.T) {
	t.Parallel()

	gw := gateway.New(http.NotFoundHandler(), http.NotFoundHandler(), gateway.Config{})
	nodeRegistry := gatewaynodes.NewRegistry()
	existing := gateway.NewWSHandler(gw, nodeRegistry)
	gw.SetWSHandler(existing)

	app := &App{
		AppConfigState: AppConfigState{
			Config: config.Config{},
		},
		AppSurfaceState: AppSurfaceState{
			Gateway:      gw,
			NodeRegistry: nodeRegistry,
		},
		appInternalState: appInternalState{
			threadBindings: channels.NewThreadBinding(),
		},
	}

	app.wireGatewayRuntimeKernelLocked(nil)

	if gw.WSHandler() != existing {
		t.Fatal("expected runtime kernel wiring to preserve existing websocket handler")
	}
}
