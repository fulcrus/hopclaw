package registry

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

func TestRegistryBuildOrdersProvidersAndPreservesMetadata(t *testing.T) {
	t.Parallel()

	reg := New()
	reg.Register(ProviderDescriptor{
		Name:  "z-last",
		Order: 20,
		Build: func(context.Context) (ProviderInstance, error) {
			return ProviderInstance{Name: "z-last", Executor: stubExecutorAdapter{}}, nil
		},
	})
	reg.Register(ProviderDescriptor{
		Name:  "a-first",
		Order: 10,
		Build: func(context.Context) (ProviderInstance, error) {
			return ProviderInstance{
				Name:     "a-first",
				Executor: stubExecutorAdapter{},
				Metadata: map[string]any{"kind": "builtin"},
			}, nil
		},
	})

	result, err := reg.Build(context.Background())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(result.Providers) != 2 {
		t.Fatalf("len(Providers) = %d, want 2", len(result.Providers))
	}
	if result.Providers[0].Name != "a-first" || result.Providers[1].Name != "z-last" {
		t.Fatalf("provider order = %#v", result.Providers)
	}
	if got := result.Providers[0].Metadata["kind"]; got != "builtin" {
		t.Fatalf("metadata kind = %#v, want builtin", got)
	}
	if result.Executor == nil {
		t.Fatal("expected composed executor")
	}
}

func TestRegistryBuildHonorsExplicitProviderDependencies(t *testing.T) {
	t.Parallel()

	reg := New()
	reg.Register(ProviderDescriptor{
		Name:  "operator",
		Order: 100,
		After: []string{"layer2"},
		Build: func(context.Context) (ProviderInstance, error) {
			return ProviderInstance{Name: "operator", Executor: stubExecutorAdapter{}}, nil
		},
	})
	reg.Register(ProviderDescriptor{
		Name:  "builtin",
		Order: 100,
		After: []string{"hostbridge"},
		Build: func(context.Context) (ProviderInstance, error) {
			return ProviderInstance{Name: "builtin", Executor: stubExecutorAdapter{}}, nil
		},
	})
	reg.Register(ProviderDescriptor{
		Name:   "hostbridge",
		Order:  100,
		After:  []string{"capability"},
		Before: []string{"builtin"},
		Build: func(context.Context) (ProviderInstance, error) {
			return ProviderInstance{Name: "hostbridge", Executor: stubExecutorAdapter{}}, nil
		},
	})
	reg.Register(ProviderDescriptor{
		Name:  "capability",
		Order: 100,
		Build: func(context.Context) (ProviderInstance, error) {
			return ProviderInstance{Name: "capability", Executor: stubExecutorAdapter{}}, nil
		},
	})
	reg.Register(ProviderDescriptor{
		Name:  "layer2",
		Order: 100,
		After: []string{"services"},
		Build: func(context.Context) (ProviderInstance, error) {
			return ProviderInstance{Name: "layer2", Executor: stubExecutorAdapter{}}, nil
		},
	})
	reg.Register(ProviderDescriptor{
		Name:  "services",
		Order: 100,
		After: []string{"builtin"},
		Build: func(context.Context) (ProviderInstance, error) {
			return ProviderInstance{Name: "services", Executor: stubExecutorAdapter{}}, nil
		},
	})

	result, err := reg.Build(context.Background())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	got := make([]string, 0, len(result.Providers))
	for _, provider := range result.Providers {
		got = append(got, provider.Name)
	}
	want := []string{"capability", "hostbridge", "builtin", "services", "layer2", "operator"}
	if len(got) != len(want) {
		t.Fatalf("provider count = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("provider order = %#v, want %#v", got, want)
		}
	}
}

func TestRegistryBuildRejectsDependencyCycles(t *testing.T) {
	t.Parallel()

	reg := New()
	reg.Register(ProviderDescriptor{
		Name:  "builtin",
		After: []string{"services"},
		Build: func(context.Context) (ProviderInstance, error) {
			return ProviderInstance{Name: "builtin", Executor: stubExecutorAdapter{}}, nil
		},
	})
	reg.Register(ProviderDescriptor{
		Name:  "services",
		After: []string{"builtin"},
		Build: func(context.Context) (ProviderInstance, error) {
			return ProviderInstance{Name: "services", Executor: stubExecutorAdapter{}}, nil
		},
	})

	if _, err := reg.Build(context.Background()); err == nil {
		t.Fatal("expected dependency cycle error")
	}
}

type stubExecutorAdapter struct{}

func (stubExecutorAdapter) ExecuteBatch(context.Context, *agent.Run, *agent.Session, []agent.ToolCall) ([]contextengine.ToolResult, error) {
	return nil, nil
}
