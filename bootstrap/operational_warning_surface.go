package bootstrap

import "github.com/fulcrus/hopclaw/controlplane"

func (a *App) OperationalWarnings() []controlplane.OperationalWarning {
	if a == nil {
		return nil
	}
	if a.operationalWarnings != nil {
		return a.operationalWarnings.OperationalWarnings()
	}
	if a.startupWarnings == nil {
		return nil
	}
	return a.startupWarnings.OperationalWarnings()
}
