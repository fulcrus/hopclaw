package bootstrap

import (
	"context"

	"github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/channels/pairing"
	channelregistry "github.com/fulcrus/hopclaw/channels/registry"
	"github.com/fulcrus/hopclaw/channels/webhook"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/gateway"
	gatewaynodes "github.com/fulcrus/hopclaw/gateway/nodes"
	"github.com/fulcrus/hopclaw/plugin"
)

type preparedOperatorBase struct {
	channelManager *channelmgr.Manager
	gateway        *gateway.Gateway
	nodeRegistry   *gatewaynodes.Registry
	webhooks       map[string]*webhook.Adapter
	deviceStore    *deviceauth.Store
	devicePairing  *deviceauth.PairingManager
	threadBindings *channels.ThreadBinding
	pairingManager *pairing.Manager
	installations  []channelregistry.Installation
	processManager *plugin.ProcessManager
}

func prepareOperatorBase(
	ctx context.Context,
	cfg config.Config,
	foundation *preparedBootstrapFoundation,
	runtimeCore *preparedBootstrapRuntimeCore,
) (*preparedOperatorBase, error) {
	channelManager := channelmgr.New()

	channelPairing, pairingErr := initChannelPairing(cfg)
	if pairingErr != nil {
		foundation.startupWarnings.Add("channel_pairing", pairingErr)
		log.Warn("channel pairing init failed", "error", pairingErr)
	}
	threadBindings := initChannelThreadBindings(cfg)

	gw := gateway.New(runtimeCore.runtimeRoutes.PublicHandler(), runtimeCore.runtimeRoutes.RuntimeHandler(), gateway.Config{
		Version:      cfg.Server.Version,
		AuthToken:    cfg.Server.AuthToken,
		AuthConfig:   cfg.Auth,
		AuthZConfig:  cfg.AuthZ,
		Diagnostics:  cfg.Diagnostics,
		Capabilities: runtimeCore.capabilities,
		Extensions:   runtimeCore.extensionRegistry,
		Runtime:      runtimeCore.runtime,
		Channels:     channelManager,
		Webhooks:     nil,
		Pairing:      channelPairing,
	})

	nodeRegistry := gatewaynodes.NewRegistry()

	deviceStore, devicePairing, deviceErr := initDeviceAuth(cfg)
	if deviceErr != nil {
		foundation.startupWarnings.Add("device_auth", deviceErr)
		log.Warn("device auth init failed", "error", deviceErr)
	}
	newPreparedOperatorKernelSurface(cfg, foundation, runtimeCore, gw, threadBindings, nodeRegistry, deviceStore, devicePairing).applyGatewaySurface(gw)

	chResult, err := channelregistry.BuildAll(ctx, newChannelRuntimeDeps(
		cfg,
		channelManager,
		runtimeCore.runtime,
		foundation.sessions,
		foundation.bus,
		runtimeCore.statusDelay,
		runtimeCore.moduleCatalog,
		channelPairing,
		threadBindings,
	))
	if err != nil {
		return nil, err
	}

	return &preparedOperatorBase{
		channelManager: channelManager,
		gateway:        gw,
		nodeRegistry:   nodeRegistry,
		webhooks:       chResult.WebhookAdapters,
		deviceStore:    deviceStore,
		devicePairing:  devicePairing,
		threadBindings: threadBindings,
		pairingManager: channelPairing,
		installations:  chResult.Installations,
		processManager: chResult.ProcessManager,
	}, nil
}
