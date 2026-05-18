package bootstrap

import (
	"github.com/fulcrus/hopclaw/gateway"
	"github.com/fulcrus/hopclaw/toolruntime"
)

type firstPartyPackContribution interface {
	packID() string
	applySurface(*preparedBootstrapSurface)
	applyGateway(*gateway.Gateway)
	applyBuiltins(*toolruntime.BuiltinsBindings)
}

func applyFirstPartyPackContribution(
	surface *preparedBootstrapSurface,
	gw *gateway.Gateway,
	builtins *toolruntime.Builtins,
	contribution firstPartyPackContribution,
) {
	if contribution == nil {
		return
	}
	contribution.applySurface(surface)
	contribution.applyGateway(gw)
	if builtins != nil {
		builtins.UpdateBindings(func(bindings *toolruntime.BuiltinsBindings) {
			contribution.applyBuiltins(bindings)
		})
	}
}

func firstPartyPackContributionIDs(contributions ...firstPartyPackContribution) []string {
	if len(contributions) == 0 {
		return nil
	}
	out := make([]string, 0, len(contributions))
	for _, contribution := range contributions {
		if contribution == nil {
			continue
		}
		out = append(out, contribution.packID())
	}
	return out
}
