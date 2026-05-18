package bootstrap

import (
	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/config"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
)

func (a *App) currentEffectiveSnapshotLocked() *controlsnapshot.EffectiveConfigSnapshot {
	if a == nil {
		return nil
	}
	if a.Runtime != nil {
		if snapshot := a.Runtime.EffectiveConfigSnapshot(); snapshot != nil {
			return snapshot
		}
	}
	return a.buildEffectiveSnapshot(a.Config, a.effectiveLayers)
}

func (a *App) buildEffectiveSnapshot(cfg config.Config, layers []controlsnapshot.Layer) *controlsnapshot.EffectiveConfigSnapshot {
	if a == nil || a.snapshotBuilder == nil {
		return nil
	}
	return a.snapshotBuilder(cfg, layers)
}

func (a *App) runtimeComponent() *agent.AgentComponent {
	if a == nil || a.Runtime == nil {
		return nil
	}
	return a.Runtime.Agent()
}

func (a *App) EffectiveConfigResolver() *controloverlay.Resolver {
	if a == nil {
		return nil
	}
	return a.effectiveConfig
}
