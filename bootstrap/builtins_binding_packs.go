package bootstrap

import (
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/toolruntime"
)

const (
	builtinBindingPackRuntime     = "builtin:runtime-pack"
	builtinBindingPackKnowledge   = "builtin:knowledge-pack"
	builtinBindingPackIntegration = "builtin:integration-pack"
	builtinBindingPackAutomation  = "builtin:automation-pack"
	builtinBindingPackUI          = "builtin:ui-pack"
	builtinBindingPackAgent       = "builtin:agent-pack"
)

type builtinsBindingPack struct {
	id    string
	apply func(*toolruntime.BuiltinsBindings)
}

func composeBuiltinsBindings(packs ...builtinsBindingPack) toolruntime.BuiltinsBindings {
	var out toolruntime.BuiltinsBindings
	for _, pack := range packs {
		if pack.apply == nil {
			continue
		}
		pack.apply(&out)
	}
	return out
}

func builtinsBindingPackIDs(packs []builtinsBindingPack) []string {
	if len(packs) == 0 {
		return nil
	}
	out := make([]string, 0, len(packs))
	for _, pack := range packs {
		out = append(out, pack.id)
	}
	return out
}

func builtinsBindingPackFromContribution(contribution firstPartyPackContribution) builtinsBindingPack {
	if contribution == nil {
		return builtinsBindingPack{}
	}
	return builtinsBindingPack{
		id: contribution.packID(),
		apply: func(bindings *toolruntime.BuiltinsBindings) {
			contribution.applyBuiltins(bindings)
		},
	}
}

func (a *App) builtinsBindingPacksForConfig(cfg config.Config) []builtinsBindingPack {
	if a == nil {
		return nil
	}

	packs := []builtinsBindingPack{
		{
			id: builtinBindingPackRuntime,
			apply: func(bindings *toolruntime.BuiltinsBindings) {
				bindings.Sessions = a.Sessions
				bindings.MemoryStore = a.memoryStore
				bindings.ArtifactStore = a.Artifacts
				bindings.ModuleCatalog = a.ModuleCatalog
			},
		},
		builtinsBindingPackFromContribution(a.knowledgePackForState()),
		builtinsBindingPackFromContribution(a.integrationPackForConfig(cfg)),
		builtinsBindingPackFromContribution(a.automationPackForState()),
		builtinsBindingPackFromContribution(a.hostPackForState()),
		builtinsBindingPackFromContribution(a.uiPackForState()),
		{
			id: builtinBindingPackAgent,
			apply: func(bindings *toolruntime.BuiltinsBindings) {
				bindings.Spawner = a.spawner
			},
		},
	}
	return packs
}
