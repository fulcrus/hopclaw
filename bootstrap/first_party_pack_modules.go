package bootstrap

import (
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
)

type firstPartyModulePack interface {
	moduleExposed() bool
	module() modules.StaticModule
}

func (a *App) firstPartyModulesForConfig(cfg config.Config) []modules.StaticModule {
	if a == nil {
		return nil
	}
	return collectFirstPartyModules(
		a.integrationPackForConfig(cfg),
		a.knowledgePackForState(),
		a.hostPackForState(),
		a.automationPackForState(),
		a.supportPackForState(),
		a.uiPackForState(),
	)
}

func collectFirstPartyModules(packs ...firstPartyModulePack) []modules.StaticModule {
	if len(packs) == 0 {
		return nil
	}
	mods := make([]modules.StaticModule, 0, len(packs))
	for _, pack := range packs {
		if pack == nil || !pack.moduleExposed() {
			continue
		}
		module := pack.module()
		if module.ID() == "" {
			continue
		}
		mods = append(mods, module)
	}
	if len(mods) == 0 {
		return nil
	}
	return mods
}

func staticFirstPartyPackModule(id, name, description string, health modules.HealthReport) modules.StaticModule {
	return modules.StaticModule{
		ManifestValue: modules.Manifest{
			ID:             id,
			Name:           name,
			Description:    description,
			Kind:           "capability_pack",
			Source:         modules.SourceBuiltin,
			Delivery:       modules.DeliveryEmbedded,
			Level:          modules.ModuleLevelManaged,
			DefaultEnabled: true,
		},
		HealthValue: health,
	}
}
