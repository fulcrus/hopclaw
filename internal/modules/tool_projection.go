package modules

import (
	"sort"
	"strings"
	"time"
)

type ToolProjection struct {
	Name              string         `json:"name"`
	ModuleID          string         `json:"module_id"`
	ModuleName        string         `json:"module_name,omitempty"`
	Description       string         `json:"description,omitempty"`
	Source            Source         `json:"source,omitempty"`
	Delivery          Delivery       `json:"delivery,omitempty"`
	Level             ModuleLevel    `json:"level,omitempty"`
	ProjectionVersion string         `json:"projection_version,omitempty"`
	Endpoint          string         `json:"endpoint,omitempty"`
	Timeout           time.Duration  `json:"timeout,omitempty"`
	InputSchema       map[string]any `json:"input_schema,omitempty"`
}

func (c Catalog) ToolProjections() []ToolProjection {
	if len(c.items) == 0 {
		return nil
	}
	out := make([]ToolProjection, 0)
	for _, item := range c.items {
		manifest := item.Manifest()
		for _, component := range item.Contributions().Tools {
			name := strings.TrimSpace(component.Name)
			if name == "" {
				continue
			}
			out = append(out, ToolProjection{
				Name:        name,
				ModuleID:    strings.TrimSpace(manifest.ID),
				ModuleName:  strings.TrimSpace(manifest.Name),
				Description: strings.TrimSpace(component.Description),
				Source:      manifest.Source,
				Delivery:    manifest.Delivery,
				Level:       manifest.Level,
				Endpoint:    metadataString(component.Metadata, "endpoint"),
				Timeout:     metadataDuration(component.Metadata, "timeout"),
				InputSchema: metadataMap(component.Metadata, "input_schema"),
			})
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

func (s *Store) ToolProjections() []ToolProjection {
	snapshot := s.SnapshotState()
	out := snapshot.Catalog.ToolProjections()
	version := strings.TrimSpace(snapshot.Version)
	for i := range out {
		out[i].ProjectionVersion = version
	}
	return out
}

func metadataDuration(metadata map[string]any, key string) time.Duration {
	value := metadataString(metadata, key)
	if value == "" {
		return 0
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout < 0 {
		return 0
	}
	return timeout
}

func metadataMap(metadata map[string]any, key string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return cloneMetadata(typed)
	}
	return nil
}
