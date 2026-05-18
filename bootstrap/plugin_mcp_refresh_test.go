package bootstrap

import (
	"context"
	"reflect"
	"testing"

	"github.com/fulcrus/hopclaw/mcp"
)

func TestPluginMCPMutationApplyReconcilesDesiredConfigs(t *testing.T) {
	t.Parallel()

	runtime := newStubMutablePluginMCPRuntime([]mcp.ServerConfig{
		{Name: "demo.alpha", Command: "alpha-old"},
		{Name: "demo.beta", Command: "beta"},
		{Name: "demo.gamma", Command: "gamma"},
	})
	desired := []mcp.ServerConfig{
		{Name: "demo.alpha", Command: "alpha-new"},
		{Name: "demo.beta", Command: "beta"},
		{Name: "demo.delta", Command: "delta"},
	}

	mutation := newPluginMCPMutation(runtime, runtime.ServerConfigs(), desired)
	if err := mutation.Apply(context.Background()); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if got := runtime.ServerConfigs(); !reflect.DeepEqual(got, desired) {
		t.Fatalf("ServerConfigs() = %#v, want %#v", got, desired)
	}
	if !reflect.DeepEqual(runtime.removeCalls, []string{"demo.alpha", "demo.gamma"}) {
		t.Fatalf("removeCalls = %#v, want [demo.alpha demo.gamma]", runtime.removeCalls)
	}
	if !reflect.DeepEqual(runtime.addCalls, []string{"demo.alpha", "demo.delta"}) {
		t.Fatalf("addCalls = %#v, want [demo.alpha demo.delta]", runtime.addCalls)
	}

	mutation.Commit(context.Background())
	if got := runtime.ServerConfigs(); !reflect.DeepEqual(got, desired) {
		t.Fatalf("ServerConfigs() after Commit = %#v, want %#v", got, desired)
	}
}

func TestPluginMCPMutationApplyRollsBackOnAddFailure(t *testing.T) {
	t.Parallel()

	initial := []mcp.ServerConfig{
		{Name: "demo.alpha", Command: "alpha"},
		{Name: "demo.beta", Command: "beta"},
	}
	runtime := newStubMutablePluginMCPRuntime(initial)
	runtime.failAdd = "demo.delta"

	mutation := newPluginMCPMutation(runtime, runtime.ServerConfigs(), []mcp.ServerConfig{
		{Name: "demo.alpha", Command: "alpha"},
		{Name: "demo.delta", Command: "delta"},
	})
	if err := mutation.Apply(context.Background()); err == nil {
		t.Fatal("Apply() error = nil, want failure")
	}

	if got := runtime.ServerConfigs(); !reflect.DeepEqual(got, initial) {
		t.Fatalf("ServerConfigs() after rollback = %#v, want %#v", got, initial)
	}
	if !reflect.DeepEqual(runtime.removeCalls, []string{"demo.beta"}) {
		t.Fatalf("removeCalls = %#v, want [demo.beta]", runtime.removeCalls)
	}
	if !reflect.DeepEqual(runtime.addCalls, []string{"demo.beta"}) {
		t.Fatalf("addCalls = %#v, want [demo.beta] from rollback restore", runtime.addCalls)
	}
}
