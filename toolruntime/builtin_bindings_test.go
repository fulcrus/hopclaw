package toolruntime

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
	"github.com/fulcrus/hopclaw/isolation"
	"github.com/fulcrus/hopclaw/skill"
)

func TestBuiltinsApplyBindingsSnapshotsInputs(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	sessions := agent.NewInMemorySessionStore()
	memory := agent.NewInMemoryKVStore()
	hub := skill.NewFileClawHubClient(t.TempDir())
	channels := channelmgr.New()
	extensions := extregistry.New(extregistry.Options{})
	spawner := isolation.NewSpawner(func(_ context.Context, _, _ string) (string, error) {
		return "run-1", nil
	})
	catalog := DestinationCatalog{
		ChannelAccounts: map[string][]DestinationAccount{
			"feishu": {{
				ID: "default",
				Metadata: map[string]any{
					"domain": "https://open.feishu.test",
				},
			}},
		},
	}

	builtins.ApplyBindings(BuiltinsBindings{
		Sessions:           sessions,
		MemoryStore:        memory,
		ClawHub:            hub,
		ChannelManager:     channels,
		ExtensionRegistry:  extensions,
		Spawner:            spawner,
		DestinationCatalog: catalog,
	})

	catalog.ChannelAccounts["feishu"][0].Metadata["domain"] = "https://mutated.example"
	snapshot := builtins.Bindings()
	if snapshot.Sessions != sessions {
		t.Fatal("expected session store binding to match input")
	}
	if snapshot.MemoryStore != memory {
		t.Fatal("expected memory store binding to match input")
	}
	if snapshot.ClawHub != hub {
		t.Fatal("expected clawhub binding to match input")
	}
	if snapshot.ChannelManager != channels {
		t.Fatal("expected channel manager binding to match input")
	}
	if snapshot.ExtensionRegistry != extensions {
		t.Fatal("expected extension registry binding to match input")
	}
	if snapshot.Spawner != spawner {
		t.Fatal("expected spawner binding to match input")
	}
	gotDomain := snapshot.DestinationCatalog.ChannelAccounts["feishu"][0].Metadata["domain"]
	if gotDomain != "https://open.feishu.test" {
		t.Fatalf("destination catalog domain = %#v, want original cloned value", gotDomain)
	}
}

func TestBuiltinsUpdateBindingsPreservesExistingBindings(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	sessions := agent.NewInMemorySessionStore()
	catalog := DestinationCatalog{
		ChannelAccounts: map[string][]DestinationAccount{
			"feishu": {{ID: "alpha"}},
		},
	}

	builtins.ApplyBindings(BuiltinsBindings{
		Sessions:           sessions,
		DestinationCatalog: catalog,
	})

	manager := channelmgr.New()
	builtins.UpdateBindings(func(bindings *BuiltinsBindings) {
		bindings.ChannelManager = manager
	})

	snapshot := builtins.Bindings()
	if snapshot.Sessions != sessions {
		t.Fatal("expected existing session binding to be preserved")
	}
	if snapshot.ChannelManager != manager {
		t.Fatal("expected updated channel manager binding")
	}
	if len(snapshot.DestinationCatalog.ChannelAccounts["feishu"]) != 1 {
		t.Fatalf("destination catalog = %#v, want preserved account", snapshot.DestinationCatalog)
	}
}
