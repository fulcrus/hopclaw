package modules

import (
	"path/filepath"
	"sort"
	"strings"
)

type MCPServerProjection struct {
	Name              string            `json:"name"`
	LocalName         string            `json:"local_name,omitempty"`
	ModuleID          string            `json:"module_id"`
	ModuleName        string            `json:"module_name,omitempty"`
	ModuleDir         string            `json:"module_dir,omitempty"`
	Description       string            `json:"description,omitempty"`
	Source            Source            `json:"source,omitempty"`
	Delivery          Delivery          `json:"delivery,omitempty"`
	Level             ModuleLevel       `json:"level,omitempty"`
	ProjectionVersion string            `json:"projection_version,omitempty"`
	Command           string            `json:"command,omitempty"`
	Args              []string          `json:"args,omitempty"`
	Env               map[string]string `json:"-"`
	WorkDir           string            `json:"work_dir,omitempty"`
}

func (c Catalog) MCPServerProjections() []MCPServerProjection {
	if len(c.items) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	out := make([]MCPServerProjection, 0)
	for _, item := range c.items {
		manifest := item.Manifest()
		for _, component := range item.Contributions().MCPServers {
			projection := mcpServerProjectionFromComponent(manifest, component, seen)
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

func (s *Store) MCPServerProjections() []MCPServerProjection {
	snapshot := s.SnapshotState()
	out := snapshot.Catalog.MCPServerProjections()
	version := strings.TrimSpace(snapshot.Version)
	for i := range out {
		out[i].ProjectionVersion = version
	}
	return out
}

func mcpServerProjectionFromComponent(manifest Manifest, component Component, seen map[string]struct{}) MCPServerProjection {
	localName := strings.TrimSpace(component.Name)
	if localName == "" {
		return MCPServerProjection{}
	}
	name := effectiveMCPServerProjectionName(manifest, localName, seen)
	if name == "" {
		return MCPServerProjection{}
	}
	return MCPServerProjection{
		Name:        name,
		LocalName:   localName,
		ModuleID:    strings.TrimSpace(manifest.ID),
		ModuleName:  strings.TrimSpace(manifest.Name),
		ModuleDir:   metadataString(component.Metadata, "module_dir"),
		Description: strings.TrimSpace(component.Description),
		Source:      manifest.Source,
		Delivery:    manifest.Delivery,
		Level:       manifest.Level,
		Command:     metadataString(component.Metadata, "command"),
		Args:        metadataStrings(component.Metadata, "args"),
		Env:         runtimeMetadataStringMap(component, "env"),
		WorkDir:     metadataString(component.Metadata, "work_dir"),
	}
}

func effectiveMCPServerProjectionName(manifest Manifest, localName string, seen map[string]struct{}) string {
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

func (p MCPServerProjection) RuntimeName() string {
	localName := strings.TrimSpace(p.LocalName)
	if localName == "" {
		localName = strings.TrimSpace(p.Name)
	}
	if localName == "" {
		return ""
	}
	prefix := strings.TrimSpace(p.ModuleName)
	if prefix == "" {
		return localName
	}
	return prefix + "." + localName
}

func (p MCPServerProjection) ResolvedCommand() string {
	command := strings.TrimSpace(p.Command)
	if command == "" {
		return ""
	}
	moduleDir := strings.TrimSpace(p.ModuleDir)
	if moduleDir == "" {
		return command
	}
	return resolveModuleRelativePath(moduleDir, command, false)
}

func (p MCPServerProjection) ResolvedWorkDir() string {
	workDir := strings.TrimSpace(p.WorkDir)
	if workDir == "" {
		return ""
	}
	moduleDir := strings.TrimSpace(p.ModuleDir)
	if moduleDir == "" {
		return workDir
	}
	return resolveModuleRelativePath(moduleDir, workDir, true)
}

func resolveModuleRelativePath(moduleDir, value string, forceRelative bool) string {
	value = strings.TrimSpace(value)
	moduleDir = strings.TrimSpace(moduleDir)
	if value == "" || moduleDir == "" {
		return value
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	if !forceRelative && !strings.HasPrefix(value, ".") && !strings.Contains(value, string(filepath.Separator)) {
		return value
	}
	return filepath.Join(moduleDir, filepath.Clean(value))
}
