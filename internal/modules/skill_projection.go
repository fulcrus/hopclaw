package modules

import (
	"sort"
	"strings"
)

type SkillProjection struct {
	ID                     string      `json:"id"`
	Name                   string      `json:"name"`
	ModuleID               string      `json:"module_id"`
	ModuleName             string      `json:"module_name,omitempty"`
	Version                string      `json:"version,omitempty"`
	Description            string      `json:"description,omitempty"`
	Source                 Source      `json:"source,omitempty"`
	Delivery               Delivery    `json:"delivery,omitempty"`
	Level                  ModuleLevel `json:"level,omitempty"`
	ProjectionVersion      string      `json:"projection_version,omitempty"`
	Kind                   string      `json:"kind,omitempty"`
	Status                 string      `json:"status,omitempty"`
	Trust                  string      `json:"trust,omitempty"`
	SourceKind             string      `json:"source_kind,omitempty"`
	SourceDir              string      `json:"source_dir,omitempty"`
	SourceRoot             string      `json:"source_root,omitempty"`
	ConfigKey              string      `json:"config_key,omitempty"`
	UserInvocable          bool        `json:"user_invocable,omitempty"`
	DisableModelInvocation bool        `json:"disable_model_invocation,omitempty"`
	Blocked                bool        `json:"blocked,omitempty"`
	ToolNames              []string    `json:"tool_names,omitempty"`
	ToolCount              int         `json:"tool_count,omitempty"`
}

func (c Catalog) SkillProjections() []SkillProjection {
	if len(c.items) == 0 {
		return nil
	}
	out := make([]SkillProjection, 0)
	for _, item := range c.items {
		manifest := item.Manifest()
		if !isSkillModuleManifest(manifest) {
			continue
		}
		toolNames := make([]string, 0, len(item.Contributions().Tools))
		for _, component := range item.Contributions().Tools {
			name := strings.TrimSpace(component.Name)
			if name == "" {
				continue
			}
			toolNames = append(toolNames, name)
		}
		sort.Strings(toolNames)
		out = append(out, SkillProjection{
			ID:                     metadataString(manifest.Metadata, skillMetadataID),
			Name:                   strings.TrimSpace(manifest.Name),
			ModuleID:               strings.TrimSpace(manifest.ID),
			ModuleName:             strings.TrimSpace(manifest.Name),
			Version:                strings.TrimSpace(manifest.Version),
			Description:            strings.TrimSpace(manifest.Description),
			Source:                 manifest.Source,
			Delivery:               manifest.Delivery,
			Level:                  manifest.Level,
			Kind:                   metadataString(manifest.Metadata, skillMetadataKind),
			Status:                 metadataString(manifest.Metadata, skillMetadataStatus),
			Trust:                  metadataString(manifest.Metadata, skillMetadataTrust),
			SourceKind:             metadataString(manifest.Metadata, skillMetadataSourceKind),
			SourceDir:              metadataString(manifest.Metadata, skillMetadataSourceDir),
			SourceRoot:             metadataString(manifest.Metadata, skillMetadataSourceRoot),
			ConfigKey:              metadataString(manifest.Metadata, skillMetadataConfigKey),
			UserInvocable:          metadataBool(manifest.Metadata, skillMetadataUserInvocable),
			DisableModelInvocation: metadataBool(manifest.Metadata, skillMetadataDisableModelInvocation),
			Blocked:                metadataBool(manifest.Metadata, skillMetadataBlocked),
			ToolNames:              toolNames,
			ToolCount:              len(toolNames),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ModuleID < out[j].ModuleID
	})
	return out
}

func (s *Store) SkillProjections() []SkillProjection {
	snapshot := s.SnapshotState()
	out := snapshot.Catalog.SkillProjections()
	version := strings.TrimSpace(snapshot.Version)
	for i := range out {
		out[i].ProjectionVersion = version
	}
	return out
}
