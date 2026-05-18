package modules

import (
	"sort"
	"strings"
	"time"
)

type ProviderProjection struct {
	Name              string            `json:"name"`
	LocalName         string            `json:"local_name,omitempty"`
	ModuleID          string            `json:"module_id"`
	ModuleName        string            `json:"module_name,omitempty"`
	Description       string            `json:"description,omitempty"`
	Source            Source            `json:"source,omitempty"`
	Delivery          Delivery          `json:"delivery,omitempty"`
	Level             ModuleLevel       `json:"level,omitempty"`
	ProjectionVersion string            `json:"projection_version,omitempty"`
	API               string            `json:"api,omitempty"`
	BaseURL           string            `json:"base_url,omitempty"`
	Region            string            `json:"region,omitempty"`
	DefaultModel      string            `json:"default_model,omitempty"`
	Timeout           time.Duration     `json:"timeout,omitempty"`
	APIKeys           []string          `json:"-"`
	Fallbacks         []string          `json:"fallbacks,omitempty"`
	EnvVars           []string          `json:"env_vars,omitempty"`
	APIKeyHint        string            `json:"api_key_hint,omitempty"`
	HasCredentials    bool              `json:"has_credentials,omitempty"`
	APIKey            string            `json:"-"`
	AccessKeyID       string            `json:"-"`
	SecretKey         string            `json:"-"`
	SessionToken      string            `json:"-"`
	Headers           map[string]string `json:"-"`
}

func (c Catalog) ProviderProjections() []ProviderProjection {
	if len(c.items) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	out := make([]ProviderProjection, 0)
	for _, item := range c.items {
		manifest := item.Manifest()
		for _, component := range item.Contributions().Providers {
			projection := providerProjectionFromComponent(manifest, component, seen)
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

func (s *Store) ProviderProjections() []ProviderProjection {
	snapshot := s.SnapshotState()
	out := snapshot.Catalog.ProviderProjections()
	version := strings.TrimSpace(snapshot.Version)
	for i := range out {
		out[i].ProjectionVersion = version
	}
	return out
}

func providerProjectionFromComponent(manifest Manifest, component Component, seen map[string]struct{}) ProviderProjection {
	localName := strings.TrimSpace(component.Name)
	if localName == "" {
		return ProviderProjection{}
	}
	name := effectiveProviderProjectionName(manifest, localName, component.Metadata, seen)
	if name == "" {
		return ProviderProjection{}
	}
	return ProviderProjection{
		Name:           name,
		LocalName:      localName,
		ModuleID:       strings.TrimSpace(manifest.ID),
		ModuleName:     strings.TrimSpace(manifest.Name),
		Description:    strings.TrimSpace(component.Description),
		Source:         manifest.Source,
		Delivery:       manifest.Delivery,
		Level:          manifest.Level,
		API:            metadataString(component.Metadata, "api"),
		BaseURL:        metadataString(component.Metadata, "base_url"),
		Region:         metadataString(component.Metadata, "region"),
		DefaultModel:   metadataString(component.Metadata, "default_model"),
		Timeout:        metadataDuration(component.Metadata, "timeout"),
		APIKeys:        runtimeMetadataStrings(component, "api_keys"),
		Fallbacks:      metadataStrings(component.Metadata, "fallbacks"),
		EnvVars:        metadataStrings(component.Metadata, "env_vars"),
		APIKeyHint:     metadataString(component.Metadata, "api_key_hint"),
		HasCredentials: metadataBool(component.Metadata, "has_credentials"),
		APIKey:         runtimeMetadataString(component, "api_key"),
		AccessKeyID:    runtimeMetadataString(component, "access_key_id"),
		SecretKey:      runtimeMetadataString(component, "secret_key"),
		SessionToken:   runtimeMetadataString(component, "session_token"),
		Headers:        runtimeMetadataStringMap(component, "headers"),
	}
}

func runtimeMetadataString(component Component, key string) string {
	if value := metadataString(component.RuntimeMetadata, key); value != "" {
		return value
	}
	return metadataString(component.Metadata, key)
}

func runtimeMetadataStrings(component Component, key string) []string {
	if values := metadataStrings(component.RuntimeMetadata, key); len(values) > 0 {
		return values
	}
	return metadataStrings(component.Metadata, key)
}

func runtimeMetadataStringMap(component Component, key string) map[string]string {
	if values := metadataStringMap(component.RuntimeMetadata, key); len(values) > 0 {
		return values
	}
	return metadataStringMap(component.Metadata, key)
}

func effectiveProviderProjectionName(manifest Manifest, localName string, metadata map[string]any, seen map[string]struct{}) string {
	localName = strings.TrimSpace(localName)
	if localName == "" {
		return ""
	}
	if manifest.Source == SourcePlugin {
		if metadataBool(metadata, "prefer_unscoped_id") {
			if _, ok := seen[localName]; !ok {
				seen[localName] = struct{}{}
				return localName
			}
		}
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

func moduleScopedComponentName(manifest Manifest, localName string) string {
	localName = strings.TrimSpace(localName)
	if localName == "" {
		return ""
	}
	prefix := strings.TrimSpace(manifest.Name)
	if prefix == "" {
		prefix = strings.TrimSpace(manifest.ID)
		prefix = strings.TrimPrefix(prefix, "plugin:")
	}
	if prefix == "" {
		return localName
	}
	return prefix + "/" + localName
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	}
	return ""
}

func metadataBool(metadata map[string]any, key string) bool {
	if len(metadata) == 0 {
		return false
	}
	value, ok := metadata[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	}
	return false
}

func metadataStrings(metadata map[string]any, key string) []string {
	if len(metadata) == 0 {
		return nil
	}
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return normalizeStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				continue
			}
			out = append(out, text)
		}
		return normalizeStrings(out)
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	}
	return nil
}
