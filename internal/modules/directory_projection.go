package modules

import "strings"

type DirectoryProjection struct {
	Kind              ComponentKind `json:"kind"`
	Name              string        `json:"name,omitempty"`
	Path              string        `json:"path"`
	ModuleID          string        `json:"module_id"`
	ModuleName        string        `json:"module_name,omitempty"`
	Source            Source        `json:"source,omitempty"`
	Delivery          Delivery      `json:"delivery,omitempty"`
	Level             ModuleLevel   `json:"level,omitempty"`
	ProjectionVersion string        `json:"projection_version,omitempty"`
}

func (c Catalog) SkillDirProjections() []DirectoryProjection {
	return c.directoryProjections(ComponentKindSkillsDir)
}

func (c Catalog) HookDirProjections() []DirectoryProjection {
	return c.directoryProjections(ComponentKindHooksDir)
}

func (s *Store) SkillDirProjections() []DirectoryProjection {
	snapshot := s.SnapshotState()
	out := snapshot.Catalog.SkillDirProjections()
	version := strings.TrimSpace(snapshot.Version)
	for i := range out {
		out[i].ProjectionVersion = version
	}
	return out
}

func (s *Store) HookDirProjections() []DirectoryProjection {
	snapshot := s.SnapshotState()
	out := snapshot.Catalog.HookDirProjections()
	version := strings.TrimSpace(snapshot.Version)
	for i := range out {
		out[i].ProjectionVersion = version
	}
	return out
}

func (c Catalog) directoryProjections(kind ComponentKind) []DirectoryProjection {
	if len(c.items) == 0 {
		return nil
	}
	out := make([]DirectoryProjection, 0)
	for _, item := range c.items {
		manifest := item.Manifest()
		for _, component := range directoryComponents(item.Contributions(), kind) {
			path := strings.TrimSpace(component.Path)
			if path == "" {
				continue
			}
			out = append(out, DirectoryProjection{
				Kind:       kind,
				Name:       strings.TrimSpace(component.Name),
				Path:       path,
				ModuleID:   strings.TrimSpace(manifest.ID),
				ModuleName: strings.TrimSpace(manifest.Name),
				Source:     manifest.Source,
				Delivery:   manifest.Delivery,
				Level:      manifest.Level,
			})
		}
	}
	return out
}

func directoryComponents(contributions Contributions, kind ComponentKind) []Component {
	switch kind {
	case ComponentKindSkillsDir:
		return contributions.SkillDirs
	case ComponentKindHooksDir:
		return contributions.HookDirs
	default:
		return nil
	}
}
