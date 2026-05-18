package bootstrap

import (
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/plugin"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolruntime"
)

func newModuleCatalogStore(builtins *toolruntime.Builtins, plugins *plugin.Manager) *modules.Store {
	return modules.NewStore(buildRuntimeModuleCatalog(builtins, plugins))
}

func buildRuntimeModuleCatalog(builtins *toolruntime.Builtins, plugins *plugin.Manager) modules.Catalog {
	groups := make([][]modules.StaticModule, 0, 2)
	if builtins != nil {
		groups = append(groups, builtins.Modules())
	}
	if plugins != nil {
		groups = append(groups, plugins.Modules())
	}
	return modules.BuildCatalog(groups...)
}

func (a *App) refreshModuleCatalogLocked() {
	a.refreshModuleCatalogForConfigLocked(a.Config)
}

func (a *App) refreshModuleCatalogForConfigLocked(cfg config.Config) {
	if a == nil {
		return
	}
	if a.ModuleCatalog == nil {
		a.ModuleCatalog = modules.NewStore(modules.Catalog{})
	}
	base := modules.BuildCatalog(
		runtimeConfigModules(cfg),
		buildRuntimeModuleCatalog(a.builtins, a.Plugins).Modules(),
		a.firstPartyModulesForConfig(cfg),
	)
	a.ModuleCatalog.Swap(modules.WithSkillModules(base, currentSkillSnapshot(a.SkillService)))
	a.wireExtensionRegistryLocked()
}

func (a *App) ModuleCatalogSnapshot() modules.Catalog {
	if a == nil || a.ModuleCatalog == nil {
		return modules.Catalog{}
	}
	return a.ModuleCatalog.Snapshot()
}

func currentSkillSnapshot(service *skill.Service) skill.RegistrySnapshot {
	if service == nil {
		return skill.RegistrySnapshot{}
	}
	return service.Snapshot()
}
