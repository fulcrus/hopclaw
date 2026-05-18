package registry

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/channels/registration"
)

type BridgeLifecycle = registration.BridgeLifecycle
type Descriptor = registration.Descriptor
type Installation = registration.Installation
type ManagedProcessPlan = registration.ManagedProcessPlan

// Registry stores channel descriptors and builds them in deterministic order.
type Registry struct {
	descriptors []Descriptor
}

func New() *Registry {
	return &Registry{}
}

func (r *Registry) Register(descriptor Descriptor) {
	if r == nil {
		return
	}
	r.descriptors = append(r.descriptors, descriptor)
}

func (r *Registry) Build(ctx context.Context) ([]Installation, error) {
	if r == nil || len(r.descriptors) == 0 {
		return nil, nil
	}
	descriptors := append([]Descriptor(nil), r.descriptors...)
	sort.SliceStable(descriptors, func(i, j int) bool {
		if descriptors[i].Order != descriptors[j].Order {
			return descriptors[i].Order < descriptors[j].Order
		}
		return strings.TrimSpace(descriptors[i].Name) < strings.TrimSpace(descriptors[j].Name)
	})

	seen := make(map[string]struct{})
	installations := make([]Installation, 0)
	for _, descriptor := range descriptors {
		name := strings.TrimSpace(descriptor.Name)
		if name == "" {
			return nil, fmt.Errorf("channel descriptor name is required")
		}
		if descriptor.Build == nil {
			return nil, fmt.Errorf("channel descriptor %q is missing build function", name)
		}
		items, err := descriptor.Build(ctx)
		if err != nil {
			return nil, fmt.Errorf("build channel descriptor %q: %w", name, err)
		}
		for _, item := range items {
			channelName := strings.TrimSpace(item.Name)
			if channelName == "" {
				return nil, fmt.Errorf("channel descriptor %q produced empty channel name", name)
			}
			if _, exists := seen[channelName]; exists {
				return nil, fmt.Errorf("channel %q already produced by another descriptor", channelName)
			}
			if item.Adapter == nil {
				return nil, fmt.Errorf("channel descriptor %q produced nil adapter for %q", name, channelName)
			}
			seen[channelName] = struct{}{}
			installations = append(installations, item)
		}
	}
	return installations, nil
}

func (r *Registry) RuntimeConfigs() map[string]any {
	if r == nil || len(r.descriptors) == 0 {
		return nil
	}
	descriptors := append([]Descriptor(nil), r.descriptors...)
	sort.SliceStable(descriptors, func(i, j int) bool {
		if descriptors[i].Order != descriptors[j].Order {
			return descriptors[i].Order < descriptors[j].Order
		}
		return strings.TrimSpace(descriptors[i].Name) < strings.TrimSpace(descriptors[j].Name)
	})

	runtimeConfigs := make(map[string]any, len(descriptors))
	for _, descriptor := range descriptors {
		name := strings.TrimSpace(descriptor.Name)
		if name == "" || descriptor.RuntimeConfig == nil {
			continue
		}
		runtimeConfigs[name] = descriptor.RuntimeConfig
	}
	if len(runtimeConfigs) == 0 {
		return nil
	}
	return runtimeConfigs
}
