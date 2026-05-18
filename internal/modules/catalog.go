package modules

import (
	"sort"
	"strings"
)

type Catalog struct {
	items []StaticModule
}

func BuildCatalog(groups ...[]StaticModule) Catalog {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	if total == 0 {
		return Catalog{}
	}

	seen := make(map[string]struct{}, total)
	items := make([]StaticModule, 0, total)
	for _, group := range groups {
		for _, item := range group {
			cloned := cloneStaticModule(item)
			id := cloned.ID()
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			items = append(items, cloned)
		}
	}
	sortStaticModules(items)
	return Catalog{items: items}
}

func (c Catalog) Len() int {
	return len(c.items)
}

func (c Catalog) Modules() []StaticModule {
	if len(c.items) == 0 {
		return nil
	}
	out := make([]StaticModule, 0, len(c.items))
	for _, item := range c.items {
		out = append(out, cloneStaticModule(item))
	}
	return out
}

func (c Catalog) Manifests() []Manifest {
	if len(c.items) == 0 {
		return nil
	}
	out := make([]Manifest, 0, len(c.items))
	for _, item := range c.items {
		out = append(out, item.Manifest())
	}
	return out
}

func (c Catalog) Contributions() Contributions {
	if len(c.items) == 0 {
		return Contributions{}
	}
	out := Contributions{}
	for _, item := range c.items {
		contrib := item.Contributions()
		out.Providers = append(out.Providers, cloneComponents(contrib.Providers)...)
		out.Channels = append(out.Channels, cloneComponents(contrib.Channels)...)
		out.Tools = append(out.Tools, cloneComponents(contrib.Tools)...)
		out.ConfigContracts = append(out.ConfigContracts, cloneComponents(contrib.ConfigContracts)...)
		out.RuntimeBridges = append(out.RuntimeBridges, cloneComponents(contrib.RuntimeBridges)...)
		out.SkillDirs = append(out.SkillDirs, cloneComponents(contrib.SkillDirs)...)
		out.HookDirs = append(out.HookDirs, cloneComponents(contrib.HookDirs)...)
		out.MCPServers = append(out.MCPServers, cloneComponents(contrib.MCPServers)...)
		out.Agents = append(out.Agents, cloneComponents(contrib.Agents)...)
	}
	return out.Normalized()
}

func (c Catalog) Find(id string) (StaticModule, bool) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return StaticModule{}, false
	}
	for _, item := range c.items {
		if strings.EqualFold(item.ID(), trimmed) {
			return cloneStaticModule(item), true
		}
	}
	return StaticModule{}, false
}

func cloneStaticModule(in StaticModule) StaticModule {
	return StaticModule{
		ManifestValue:      in.Manifest(),
		ContributionsValue: in.Contributions(),
		HealthValue:        normalizeHealthReport(in.HealthValue),
	}
}

func normalizeHealthReport(in HealthReport) HealthReport {
	in.Status = HealthStatus(strings.TrimSpace(string(in.Status)))
	in.Summary = strings.TrimSpace(in.Summary)
	if in.Status == "" {
		in.Status = HealthUnknown
	}
	in.Details = cloneMetadata(in.Details)
	return in
}

func sortStaticModules(items []StaticModule) {
	sort.Slice(items, func(i, j int) bool {
		left := items[i].Manifest()
		right := items[j].Manifest()
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.Version < right.Version
	})
}
