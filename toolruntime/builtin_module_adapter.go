package toolruntime

import (
	"strings"

	"github.com/fulcrus/hopclaw/internal/modules"
)

func BuiltinModules(cfg BuiltinsConfig) []modules.StaticModule {
	catalog := builtinCategoryCatalog()
	if len(catalog) == 0 {
		return nil
	}

	out := make([]modules.StaticModule, 0, len(catalog))
	for _, category := range catalog {
		out = append(out, category.module(cfg))
	}
	return out
}

func (b *Builtins) Modules() []modules.StaticModule {
	if b == nil {
		return nil
	}
	return BuiltinModules(b.config)
}

func (d builtinCategoryDescriptor) module(cfg BuiltinsConfig) modules.StaticModule {
	return modules.StaticModule{
		ManifestValue:      d.moduleManifest(),
		ContributionsValue: d.moduleContributions(cfg),
		HealthValue: modules.HealthReport{
			Status: modules.HealthUnknown,
		},
	}
}

func (d builtinCategoryDescriptor) moduleManifest() modules.Manifest {
	name := strings.TrimSpace(d.Name)
	return modules.Manifest{
		ID:             builtinModuleID(name),
		Name:           name,
		Description:    builtinModuleDescription(d),
		Kind:           "capability_pack",
		Source:         modules.SourceBuiltin,
		Delivery:       modules.DeliveryEmbedded,
		Level:          modules.ModuleLevelManaged,
		DefaultEnabled: true,
	}
}

func (d builtinCategoryDescriptor) moduleContributions(cfg BuiltinsConfig) modules.Contributions {
	name := strings.TrimSpace(d.Name)
	moduleID := builtinModuleID(name)
	defs := d.Load(cfg)
	if len(defs) == 0 {
		return modules.Contributions{}
	}

	out := modules.Contributions{
		Tools: make([]modules.Component, 0, len(defs)),
	}
	for _, def := range defs {
		toolName := strings.TrimSpace(def.Manifest.Name)
		if toolName == "" {
			continue
		}
		metadata := map[string]any{
			"category":          name,
			"module_id":         moduleID,
			"side_effect_class": def.Manifest.SideEffectClass,
			"idempotent":        def.Manifest.Idempotent,
			"requires_approval": def.Manifest.RequiresApproval,
		}
		if def.Manifest.ExecutionKey != "" {
			metadata["execution_key"] = def.Manifest.ExecutionKey
		}
		if len(def.Manifest.Aliases) > 0 {
			metadata["aliases"] = append([]string(nil), def.Manifest.Aliases...)
		}
		if def.Manifest.Timeout > 0 {
			metadata["timeout"] = def.Manifest.Timeout.String()
		}
		if hiddenBuiltinToolName(toolName) {
			metadata["hidden"] = true
		}
		if def.Manifest.Runtime.Entry != "" || def.Manifest.Runtime.Shell != "" {
			metadata["runtime"] = map[string]any{
				"entry": def.Manifest.Runtime.Entry,
				"shell": def.Manifest.Runtime.Shell,
			}
		}
		out.Tools = append(out.Tools, modules.Component{
			Kind:        modules.ComponentKindTool,
			Name:        toolName,
			Description: strings.TrimSpace(def.Manifest.Description),
			Metadata:    metadata,
		})
	}
	return out.Normalized()
}

func builtinModuleID(name string) string {
	return "builtin:" + strings.TrimSpace(name)
}

func builtinModuleDescription(category builtinCategoryDescriptor) string {
	if description := strings.TrimSpace(category.Description); description != "" {
		return description
	}
	name := strings.ReplaceAll(strings.TrimSpace(category.Name), "_", " ")
	if name == "" {
		return "Bundled builtin tools."
	}
	return "Bundled builtin tools for " + name + "."
}
