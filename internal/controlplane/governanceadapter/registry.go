package governanceadapter

import (
	"reflect"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/controlplane"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type AdapterDescriptor = controlplane.GovernanceAdapterDescriptor
type AdapterSummary = controlplane.GovernanceAdapterSummary

type AdapterRegistry struct {
	descriptors map[string]AdapterDescriptor
	adapters    []Adapter
}

func NewAdapterRegistry(descriptors []AdapterDescriptor, adapters ...Adapter) *AdapterRegistry {
	registry := &AdapterRegistry{
		descriptors: make(map[string]AdapterDescriptor, len(descriptors)),
	}
	hasDescriptors := false
	for _, descriptor := range descriptors {
		name := normalizeAdapterName(descriptor.Name)
		if name == "" {
			continue
		}
		hasDescriptors = true
		descriptor.Name = strings.TrimSpace(descriptor.Name)
		descriptor.Type = strings.TrimSpace(descriptor.Type)
		descriptor.Kinds = cloneKinds(descriptor.Kinds)
		descriptor.Metadata = supportmaps.Clone(descriptor.Metadata)
		registry.descriptors[name] = descriptor
	}
	for _, adapter := range adapters {
		if isNilAdapter(adapter) {
			continue
		}
		name := normalizeAdapterName(adapterName(adapter))
		if hasDescriptors {
			if name == "" {
				continue
			}
			descriptor, ok := registry.descriptors[name]
			if !ok || !descriptor.Enabled {
				continue
			}
		}
		registry.adapters = append(registry.adapters, adapter)
	}
	return registry
}

func (r *AdapterRegistry) Adapters() []Adapter {
	if r == nil || len(r.adapters) == 0 {
		return nil
	}
	out := make([]Adapter, len(r.adapters))
	copy(out, r.adapters)
	return out
}

func (r *AdapterRegistry) EnabledAdapterNames() []string {
	if r == nil {
		return nil
	}
	if len(r.descriptors) > 0 {
		names := make([]string, 0, len(r.descriptors))
		for _, descriptor := range r.descriptors {
			if !descriptor.Enabled {
				continue
			}
			names = append(names, strings.TrimSpace(descriptor.Name))
		}
		sort.Strings(names)
		if len(names) == 0 {
			return nil
		}
		return names
	}
	names := make([]string, 0, len(r.adapters))
	for _, adapter := range r.adapters {
		if name := strings.TrimSpace(adapterName(adapter)); name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil
	}
	return names
}

func (r *AdapterRegistry) Describe() []AdapterSummary {
	if r == nil {
		return nil
	}
	items := make([]AdapterSummary, 0, len(r.descriptors)+len(r.adapters))
	seen := make(map[string]struct{}, len(r.descriptors)+len(r.adapters))
	for key, descriptor := range r.descriptors {
		items = append(items, AdapterSummary{
			Name:            strings.TrimSpace(descriptor.Name),
			Type:            normalize.FirstNonEmpty(strings.TrimSpace(descriptor.Type), "custom"),
			Enabled:         descriptor.Enabled,
			Registered:      r.hasAdapter(key),
			IncludeSnapshot: descriptor.IncludeSnapshot,
			Kinds:           cloneKinds(descriptor.Kinds),
			Metadata:        supportmaps.Clone(descriptor.Metadata),
		})
		seen[key] = struct{}{}
	}
	for _, adapter := range r.adapters {
		name := strings.TrimSpace(adapterName(adapter))
		key := normalizeAdapterName(name)
		if key == "" {
			name = adapterTypeName(adapter)
			key = normalizeAdapterName(name)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		items = append(items, AdapterSummary{
			Name:       name,
			Type:       "custom",
			Enabled:    true,
			Registered: true,
		})
	}
	sortAdapterSummaries(items)
	if len(items) == 0 {
		return nil
	}
	return items
}

func (r *AdapterRegistry) hasAdapter(name string) bool {
	if r == nil {
		return false
	}
	normalized := normalizeAdapterName(name)
	if normalized == "" {
		return false
	}
	for _, adapter := range r.adapters {
		if normalizeAdapterName(adapterName(adapter)) == normalized {
			return true
		}
	}
	return false
}

func adapterName(adapter Adapter) string {
	if named, ok := adapter.(NamedAdapter); ok {
		return named.Name()
	}
	return ""
}

func normalizeAdapterName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func adapterTypeName(adapter any) string {
	if adapter == nil {
		return ""
	}
	typ := reflect.TypeOf(adapter)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Name() != "" {
		return typ.Name()
	}
	return typ.String()
}

func cloneKinds(items []Kind) []Kind {
	if len(items) == 0 {
		return nil
	}
	out := make([]Kind, len(items))
	copy(out, items)
	return out
}

func sortAdapterSummaries(items []AdapterSummary) {
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
