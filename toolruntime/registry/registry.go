package registry

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/toolruntime"
)

// ProviderDescriptor declares one tool provider source that can be built into
// the runtime tool surface. Callers register descriptors and then build the
// enabled providers into the single execution path used by the runtime.
type ProviderDescriptor struct {
	Name   string
	Source string
	Order  int
	After  []string
	Before []string
	Build  func(context.Context) (ProviderInstance, error)
}

// ProviderInstance is one realized provider contribution.
type ProviderInstance struct {
	Name     string
	Source   string
	Executor agent.ToolExecutor
	Metadata map[string]any
}

// BuildResult is the ordered provider set plus the composed executor.
type BuildResult struct {
	Executor  agent.ToolExecutor
	Providers []ProviderInstance
}

// Registry stores tool provider descriptors in registration order.
type Registry struct {
	descriptors []ProviderDescriptor
}

type providerBuildNode struct {
	descriptor ProviderDescriptor
	index      int
	indegree   int
	outbound   map[string]struct{}
}

func New() *Registry {
	return &Registry{}
}

func (r *Registry) Register(descriptor ProviderDescriptor) {
	if r == nil {
		return
	}
	r.descriptors = append(r.descriptors, descriptor)
}

func (r *Registry) Build(ctx context.Context) (BuildResult, error) {
	if r == nil || len(r.descriptors) == 0 {
		return BuildResult{}, nil
	}

	nodes := make(map[string]*providerBuildNode, len(r.descriptors))
	seen := make(map[string]struct{}, len(r.descriptors))
	for index, descriptor := range r.descriptors {
		name := strings.TrimSpace(descriptor.Name)
		if name == "" {
			return BuildResult{}, fmt.Errorf("tool provider name is required")
		}
		if _, exists := seen[name]; exists {
			return BuildResult{}, fmt.Errorf("tool provider %q already registered", name)
		}
		seen[name] = struct{}{}
		nodes[name] = &providerBuildNode{
			descriptor: descriptor,
			index:      index,
			outbound:   make(map[string]struct{}),
		}
	}
	for name, node := range nodes {
		for _, dep := range node.descriptor.After {
			addProviderDependency(nodes, strings.TrimSpace(dep), name)
		}
		for _, dep := range node.descriptor.Before {
			addProviderDependency(nodes, name, strings.TrimSpace(dep))
		}
	}
	descriptors, err := topoSortProviderDescriptors(nodes)
	if err != nil {
		return BuildResult{}, err
	}

	providers := make([]ProviderInstance, 0, len(descriptors))
	executors := make([]agent.ToolExecutor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		name := strings.TrimSpace(descriptor.Name)
		if descriptor.Build == nil {
			return BuildResult{}, fmt.Errorf("tool provider %q is missing build function", name)
		}
		instance, err := descriptor.Build(ctx)
		if err != nil {
			return BuildResult{}, fmt.Errorf("build tool provider %q: %w", name, err)
		}
		if instance.Name == "" {
			instance.Name = name
		}
		if instance.Source == "" {
			instance.Source = strings.TrimSpace(descriptor.Source)
		}
		providers = append(providers, instance)
		if instance.Executor != nil {
			executors = append(executors, instance.Executor)
		}
	}

	var executor agent.ToolExecutor
	switch len(executors) {
	case 0:
		executor = nil
	case 1:
		executor = executors[0]
	default:
		executor = toolruntime.NewComposite(toolruntime.CompositeConfig{}, executors...)
	}
	return BuildResult{
		Executor:  executor,
		Providers: providers,
	}, nil
}

func addProviderDependency(nodes map[string]*providerBuildNode, from, to string) {
	if from == "" || to == "" || from == to {
		return
	}
	source := nodes[from]
	target := nodes[to]
	if source == nil || target == nil {
		return
	}
	if _, exists := source.outbound[to]; exists {
		return
	}
	source.outbound[to] = struct{}{}
	target.indegree++
}

func topoSortProviderDescriptors(nodes map[string]*providerBuildNode) ([]ProviderDescriptor, error) {
	if len(nodes) == 0 {
		return nil, nil
	}
	ready := make([]*providerBuildNode, 0, len(nodes))
	for _, node := range nodes {
		if node.indegree == 0 {
			ready = append(ready, node)
		}
	}
	sortProviderNodes(ready)

	ordered := make([]ProviderDescriptor, 0, len(nodes))
	processed := make(map[string]struct{}, len(nodes))
	for len(ready) > 0 {
		node := ready[0]
		ready = ready[1:]
		name := strings.TrimSpace(node.descriptor.Name)
		ordered = append(ordered, node.descriptor)
		processed[name] = struct{}{}

		for targetName := range node.outbound {
			target := nodes[targetName]
			if target == nil {
				continue
			}
			target.indegree--
			if target.indegree == 0 {
				ready = append(ready, target)
				sortProviderNodes(ready)
			}
		}
	}
	if len(ordered) == len(nodes) {
		return ordered, nil
	}

	cycle := make([]string, 0, len(nodes)-len(processed))
	for name := range nodes {
		if _, ok := processed[name]; ok {
			continue
		}
		cycle = append(cycle, name)
	}
	sort.Strings(cycle)
	return nil, fmt.Errorf("tool provider dependency cycle: %s", strings.Join(cycle, ", "))
}

func sortProviderNodes(nodes []*providerBuildNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		left := nodes[i].descriptor
		right := nodes[j].descriptor
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		leftName := strings.TrimSpace(left.Name)
		rightName := strings.TrimSpace(right.Name)
		if leftName != rightName {
			return leftName < rightName
		}
		return nodes[i].index < nodes[j].index
	})
}

func (r BuildResult) Provider(name string) (ProviderInstance, bool) {
	for _, provider := range r.Providers {
		if strings.EqualFold(strings.TrimSpace(provider.Name), strings.TrimSpace(name)) {
			return provider, true
		}
	}
	return ProviderInstance{}, false
}
