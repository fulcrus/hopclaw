package bootstrap

import (
	"context"

	"github.com/fulcrus/hopclaw/config"
)

func (a *App) runRefreshPostCommitActionsLocked(ctx context.Context, plan RefreshPlan, cfg config.Config) error {
	if a == nil {
		return nil
	}
	for _, action := range plan.PostCommit {
		switch action {
		case ReloadPostCommitRebuildChannelHealth:
			a.rebuildChannelHealthLocked(ctx, cfg)
		case ReloadPostCommitRefreshPlugins:
			if err := a.refreshPluginsLocked(ctx, true); err != nil {
				return err
			}
		case ReloadPostCommitRestartPluginWatcher:
			a.requestPluginWatcherRestartLocked(ctx)
		}
	}
	return nil
}
