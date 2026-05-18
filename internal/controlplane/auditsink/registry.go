package auditsink

import (
	"strings"

	"github.com/fulcrus/hopclaw/controlplane"
)

type SinkDescriptor = controlplane.AuditSinkDescriptor
type SinkSummary = controlplane.AuditSinkSummary

type Registry struct {
	descriptors map[string]SinkDescriptor
	registered  map[string]bool
}

func NewRegistry(descriptors []SinkDescriptor, registeredNames ...string) *Registry {
	registry := &Registry{
		descriptors: make(map[string]SinkDescriptor, len(descriptors)),
		registered:  make(map[string]bool, len(registeredNames)),
	}
	for _, descriptor := range descriptors {
		name := normalizeSinkName(descriptor.Name)
		if name == "" {
			continue
		}
		descriptor.Name = strings.TrimSpace(descriptor.Name)
		descriptor.Type = strings.TrimSpace(descriptor.Type)
		descriptor.Target = strings.TrimSpace(descriptor.Target)
		registry.descriptors[name] = descriptor
	}
	for _, name := range registeredNames {
		normalized := normalizeSinkName(name)
		if normalized == "" {
			continue
		}
		registry.registered[normalized] = true
	}
	return registry
}

func (r *Registry) Describe() []SinkSummary {
	if r == nil {
		return nil
	}
	items := make([]SinkSummary, 0, len(r.descriptors)+len(r.registered))
	seen := make(map[string]struct{}, len(r.descriptors)+len(r.registered))
	for key, descriptor := range r.descriptors {
		items = append(items, SinkSummary{
			Name:       strings.TrimSpace(descriptor.Name),
			Type:       defaultString(strings.TrimSpace(descriptor.Type), "custom"),
			Enabled:    descriptor.Enabled,
			Registered: r.registered[key],
			Target:     strings.TrimSpace(descriptor.Target),
			Metadata:   cloneMetadata(descriptor.Metadata),
		})
		seen[key] = struct{}{}
	}
	for key := range r.registered {
		if _, ok := seen[key]; ok {
			continue
		}
		items = append(items, SinkSummary{
			Name:       key,
			Type:       "custom",
			Enabled:    true,
			Registered: true,
		})
	}
	sortSinkSummaries(items)
	if len(items) == 0 {
		return nil
	}
	return items
}

func normalizeSinkName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func cloneMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func sortSinkSummaries(items []SinkSummary) {
	if len(items) < 2 {
		return
	}
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if strings.ToLower(items[j].Name) < strings.ToLower(items[i].Name) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}
