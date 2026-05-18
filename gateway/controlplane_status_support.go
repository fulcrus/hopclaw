package gateway

import (
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/skill"
)

func buildControlPlaneRuntimeFacts(runtimeCtx skill.RuntimeContext) controlplane.RuntimeFactsSummary {
	return controlplane.BuildRuntimeFactsSummary(runtimeCtx)
}

func buildControlPlaneChildEnvPolicy() controlplane.ChildEnvPolicySummary {
	return controlplane.BuildChildEnvPolicySummary()
}

func buildControlPlaneStorageSummary(g *Gateway) controlplane.StorageSummary {
	cfg := config.Config{}
	if g != nil && g.effectiveCfg != nil {
		cfg = g.effectiveCfg.Current()
	}
	return controlplane.BuildStorageSummary(cfg)
}

func buildControlPlaneOperationalWarnings(g *Gateway) []controlplane.OperationalWarning {
	if g == nil || g.operationalWarning == nil {
		return nil
	}
	return g.operationalWarning.OperationalWarnings()
}

func buildControlPlaneResultProjectionSummary() controlplane.ResultProjectionSummary {
	return controlplane.BuildResultProjectionSummary()
}

func buildControlPlaneKnowledgeSummary() controlplane.KnowledgeSummary {
	return controlplane.BuildKnowledgeSummary()
}
