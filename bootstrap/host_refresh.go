package bootstrap

import (
	"context"

	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	"github.com/fulcrus/hopclaw/config"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
)

type preparedHostRuntime struct {
	managedHelpers *managedHelpers
	browserClient  *browserclient.Client
	desktopClient  *desktopclient.Client
	capabilities   *capregistry.Registry
}

func (p *preparedHostRuntime) cleanup(ctx context.Context) error {
	if p == nil {
		return nil
	}
	return stopManagedHelpers(ctx, p.managedHelpers)
}

func (a *App) prepareHostRuntimeLocked(cfg config.Config) (*preparedHostRuntime, error) {
	if a == nil {
		return nil, nil
	}
	managedHosts := initManagedHelpers(cfg)
	browserHostClient := newBrowserHostClient(cfg.Hosts.Browser, managedHosts.Browser)
	desktopHostClient := newDesktopHostClient(cfg.Hosts.Desktop, managedHosts.Desktop)
	return &preparedHostRuntime{
		managedHelpers: managedHosts,
		browserClient:  browserHostClient,
		desktopClient:  desktopHostClient,
		capabilities:   initCapabilities(browserHostClient, desktopHostClient),
	}, nil
}

func managedHelpersControllerFor(helpers *managedHelpers) *ManagedHelpersController {
	if helpers == nil || (helpers.Browser == nil && helpers.Desktop == nil) {
		return nil
	}
	return &ManagedHelpersController{Helpers: helpers}
}

func stopManagedHelpers(ctx context.Context, helpers *managedHelpers) error {
	if helpers == nil {
		return nil
	}
	if helpers.Browser != nil {
		if err := helpers.Browser.Stop(ctx); err != nil {
			return err
		}
	}
	if helpers.Desktop != nil {
		if err := helpers.Desktop.Stop(ctx); err != nil {
			return err
		}
	}
	return nil
}
