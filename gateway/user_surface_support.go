package gateway

import "github.com/fulcrus/hopclaw/controlplane"

func (g *Gateway) userSurfaceSummary() controlplane.UserSurfaceSummary {
	if g == nil {
		return controlplane.UserSurfaceSummary{}
	}
	var snapshot *controlplane.EffectiveConfigSnapshot
	if g.runtime != nil {
		snapshot = g.runtime.EffectiveConfigSnapshot()
	}
	return controlplane.BuildUserSurfaceSummary(
		snapshot,
		g.authConfigured(),
		len(g.describeApprovalProviders()),
		len(g.describeGovernanceAdapters()),
		len(g.describeAuditSinks()),
	)
}

func (g *Gateway) describeApprovalProviders() []controlplane.ApprovalProviderSummary {
	if g == nil || g.approvalProviders == nil {
		return nil
	}
	return g.approvalProviders.Describe()
}

func (g *Gateway) describeGovernanceAdapters() []controlplane.GovernanceAdapterSummary {
	if g == nil || g.governanceAdapters == nil {
		return nil
	}
	return g.governanceAdapters.Describe()
}

func (g *Gateway) describeAuditSinks() []controlplane.AuditSinkSummary {
	if g == nil || g.auditSinks == nil {
		return nil
	}
	return g.auditSinks.Describe()
}
