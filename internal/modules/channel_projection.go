package modules

import (
	"sort"
	"strconv"
	"strings"
)

type ChannelProjection struct {
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
	Type              string            `json:"type,omitempty"`
	CallbackURL       string            `json:"callback_url,omitempty"`
	Secret            string            `json:"-"`
	Command           string            `json:"command,omitempty"`
	Args              []string          `json:"args,omitempty"`
	Env               map[string]string `json:"-"`
	WorkDir           string            `json:"work_dir,omitempty"`
	Capabilities      []string          `json:"capabilities,omitempty"`
	Config            map[string]any    `json:"-"`
	MaxRestarts       int               `json:"max_restarts,omitempty"`
}

func (c Catalog) ChannelProjections() []ChannelProjection {
	if len(c.items) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	out := make([]ChannelProjection, 0)
	for _, item := range c.items {
		manifest := item.Manifest()
		for _, component := range item.Contributions().Channels {
			projection := channelProjectionFromComponent(manifest, component, seen)
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

func (s *Store) ChannelProjections() []ChannelProjection {
	snapshot := s.SnapshotState()
	out := snapshot.Catalog.ChannelProjections()
	version := strings.TrimSpace(snapshot.Version)
	for i := range out {
		out[i].ProjectionVersion = version
	}
	return out
}

func channelProjectionFromComponent(manifest Manifest, component Component, seen map[string]struct{}) ChannelProjection {
	localName := strings.TrimSpace(component.Name)
	if localName == "" {
		return ChannelProjection{}
	}
	name := effectiveChannelProjectionName(manifest, localName, seen)
	if name == "" {
		return ChannelProjection{}
	}
	return ChannelProjection{
		Name:         name,
		LocalName:    localName,
		ModuleID:     strings.TrimSpace(manifest.ID),
		ModuleName:   strings.TrimSpace(manifest.Name),
		ModuleDir:    metadataString(component.Metadata, "module_dir"),
		Description:  strings.TrimSpace(component.Description),
		Source:       manifest.Source,
		Delivery:     manifest.Delivery,
		Level:        manifest.Level,
		Type:         metadataString(component.Metadata, "type"),
		CallbackURL:  metadataString(component.Metadata, "callback_url"),
		Secret:       runtimeMetadataString(component, "secret"),
		Command:      metadataString(component.Metadata, "command"),
		Args:         metadataStrings(component.Metadata, "args"),
		Env:          runtimeMetadataStringMap(component, "env"),
		WorkDir:      metadataString(component.Metadata, "work_dir"),
		Capabilities: metadataStrings(component.Metadata, "capabilities"),
		Config:       runtimeMetadataMap(component, "config"),
		MaxRestarts:  metadataInt(component.Metadata, "max_restarts"),
	}
}

func effectiveChannelProjectionName(manifest Manifest, localName string, seen map[string]struct{}) string {
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

func metadataStringMap(metadata map[string]any, key string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case map[string]string:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for envKey, envValue := range typed {
			envKey = strings.TrimSpace(envKey)
			if envKey == "" {
				continue
			}
			out[envKey] = envValue
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case map[string]any:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for envKey, envValue := range typed {
			envKey = strings.TrimSpace(envKey)
			if envKey == "" {
				continue
			}
			text, ok := envValue.(string)
			if !ok {
				continue
			}
			out[envKey] = text
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

func runtimeMetadataMap(component Component, key string) map[string]any {
	if values := metadataMap(component.RuntimeMetadata, key); len(values) > 0 {
		return values
	}
	return metadataMap(component.Metadata, key)
}

func metadataInt(metadata map[string]any, key string) int {
	if len(metadata) == 0 {
		return 0
	}
	value, ok := metadata[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return 0
}
