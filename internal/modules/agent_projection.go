package modules

import (
	"sort"
	"strings"
)

type AgentProjection struct {
	Name              string      `json:"name"`
	LocalName         string      `json:"local_name,omitempty"`
	ModuleID          string      `json:"module_id"`
	ModuleName        string      `json:"module_name,omitempty"`
	Description       string      `json:"description,omitempty"`
	Source            Source      `json:"source,omitempty"`
	Delivery          Delivery    `json:"delivery,omitempty"`
	Level             ModuleLevel `json:"level,omitempty"`
	ProjectionVersion string      `json:"projection_version,omitempty"`
	SystemPrompt      string      `json:"system_prompt,omitempty"`
	Model             string      `json:"model,omitempty"`
	Tools             []string    `json:"tools,omitempty"`
	Skills            []string    `json:"skills,omitempty"`
	MaxTokens         int         `json:"max_tokens,omitempty"`
}

func (c Catalog) AgentProjections() []AgentProjection {
	if len(c.items) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	out := make([]AgentProjection, 0)
	for _, item := range c.items {
		manifest := item.Manifest()
		for _, component := range item.Contributions().Agents {
			projection := agentProjectionFromComponent(manifest, component, seen)
			if projection.Name == "" {
				continue
			}
			out = append(out, projection)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ModuleID < out[j].ModuleID
	})
	return out
}

func (s *Store) AgentProjections() []AgentProjection {
	snapshot := s.SnapshotState()
	out := snapshot.Catalog.AgentProjections()
	version := strings.TrimSpace(snapshot.Version)
	for i := range out {
		out[i].ProjectionVersion = version
	}
	return out
}

func agentProjectionFromComponent(manifest Manifest, component Component, seen map[string]struct{}) AgentProjection {
	localName := strings.TrimSpace(component.Name)
	if localName == "" {
		return AgentProjection{}
	}
	name := effectiveAgentProjectionName(manifest, localName, seen)
	if name == "" {
		return AgentProjection{}
	}
	return AgentProjection{
		Name:         name,
		LocalName:    localName,
		ModuleID:     strings.TrimSpace(manifest.ID),
		ModuleName:   strings.TrimSpace(manifest.Name),
		Description:  strings.TrimSpace(component.Description),
		Source:       manifest.Source,
		Delivery:     manifest.Delivery,
		Level:        manifest.Level,
		SystemPrompt: metadataString(component.Metadata, "system_prompt"),
		Model:        metadataString(component.Metadata, "model"),
		Tools:        metadataStrings(component.Metadata, "tools"),
		Skills:       metadataStrings(component.Metadata, "skills"),
		MaxTokens:    metadataInt(component.Metadata, "max_tokens"),
	}
}

func effectiveAgentProjectionName(manifest Manifest, localName string, seen map[string]struct{}) string {
	localName = strings.TrimSpace(localName)
	if localName == "" {
		return ""
	}
	if manifest.Source == SourcePlugin {
		scoped := moduleScopedComponentName(manifest, localName)
		if scoped != "" {
			if _, ok := seen[scoped]; !ok {
				seen[scoped] = struct{}{}
				return scoped
			}
		}
	}
	if _, ok := seen[localName]; !ok {
		seen[localName] = struct{}{}
		return localName
	}
	scoped := moduleScopedComponentName(manifest, localName)
	if scoped == "" {
		return ""
	}
	if _, ok := seen[scoped]; ok {
		return ""
	}
	seen[scoped] = struct{}{}
	return scoped
}
