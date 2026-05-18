package bootstrap

import (
	"context"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/i18n"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
)

type effectiveConfigRefreshExecution struct {
	app   *App
	plan  RefreshPlan
	txn   RefreshApplyTransaction
	state *controlPlaneRuntimeState

	oldBase      config.Config
	nextBase     config.Config
	oldCfg       config.Config
	nextCfg      config.Config
	oldSnapshot  *controlsnapshot.EffectiveConfigSnapshot
	nextSnapshot *controlsnapshot.EffectiveConfigSnapshot
	oldResolver  *controloverlay.Resolver
	nextResolver *controloverlay.Resolver
	oldLayers    []controlsnapshot.Layer
	nextLayers   []controlsnapshot.Layer
}

func (e *effectiveConfigRefreshExecution) restoreConfigView() {
	if e == nil || e.app == nil {
		return
	}
	a := e.app
	a.BaseConfig = e.oldBase
	a.Config = e.oldCfg
	a.effectiveLayers = e.oldLayers
	a.effectiveConfig = e.oldResolver
	i18n.ApplyConfiguredLocale(e.oldCfg.Locale)
	if a.Runtime != nil && e.plan.RefreshRuntimeConfig {
		a.Runtime.WithEffectiveConfigSnapshot(e.oldSnapshot)
	}
	if e.plan.RefreshCredentials {
		a.wireGatewayEffectiveConfigLocked(e.oldCfg, e.oldResolver)
	}
}

func (e *effectiveConfigRefreshExecution) applyConfigView() {
	if e == nil || e.app == nil {
		return
	}
	a := e.app
	a.BaseConfig = e.nextBase
	a.Config = e.nextCfg
	a.effectiveLayers = e.nextLayers
	if e.nextResolver != nil {
		a.effectiveConfig = e.nextResolver
	}
	i18n.ApplyConfiguredLocale(e.nextCfg.Locale)
	if a.Runtime != nil && e.plan.RefreshRuntimeConfig {
		a.Runtime.WithEffectiveConfigSnapshot(e.nextSnapshot)
	}
	if e.plan.RefreshCredentials {
		a.wireGatewayEffectiveConfigLocked(e.nextCfg, e.nextResolver)
	}
}

func (e *effectiveConfigRefreshExecution) validate(ctx context.Context) error {
	if e == nil {
		return nil
	}
	return validateEffectiveConfigRefresh(ctx, e.app)
}

func (e *effectiveConfigRefreshExecution) commit(ctx context.Context) {
	if e == nil || e.app == nil {
		return
	}
	e.txn.Commit(ctx)
	e.app.updateSnapshotStateFromControlPlane(e.state)
}

func (e *effectiveConfigRefreshExecution) rollback(ctx context.Context) {
	if e == nil {
		return
	}
	e.txn.Rollback(ctx)
	e.restoreConfigView()
}

func (e *effectiveConfigRefreshExecution) run(ctx context.Context) error {
	if e == nil {
		return nil
	}
	e.applyConfigView()
	if err := e.validate(ctx); err != nil {
		e.rollback(ctx)
		return err
	}
	e.commit(ctx)
	if err := e.app.runRefreshPostCommitActionsLocked(ctx, e.plan, e.nextCfg); err != nil {
		return wrapCommittedRefreshError(err)
	}
	return nil
}
