package config

import (
	"strings"
	"testing"
)

func TestChangeSetHasChangesEmpty(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{}
	if cs.HasChanges() {
		t.Fatal("empty ChangeSet should have no changes")
	}
}

func TestChangeSetHasChangesNonEmpty(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{
		Changes: []Change{{Section: "agent", Kind: ChangeUpdated}},
	}
	if !cs.HasChanges() {
		t.Fatal("ChangeSet with entries should have changes")
	}
}

func TestChangeSetHasSection(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{
		Changes: []Change{
			{Section: "server", Kind: ChangeUpdated},
			{Section: "models", Kind: ChangeUpdated},
		},
	}

	if !cs.HasSection("server") {
		t.Fatal("expected HasSection(server) to be true")
	}
	if !cs.HasSection("models") {
		t.Fatal("expected HasSection(models) to be true")
	}
	if cs.HasSection("agent") {
		t.Fatal("expected HasSection(agent) to be false")
	}
}

func TestChangeSetSections(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{
		Changes: []Change{
			{Section: "agent", Kind: ChangeUpdated},
			{Section: "channels", Kind: ChangeAdded},
		},
	}

	sections := cs.Sections()
	if len(sections) != 2 {
		t.Fatalf("Sections() returned %d, want 2", len(sections))
	}
	if sections[0] != "agent" || sections[1] != "channels" {
		t.Fatalf("Sections() = %v, want [agent, channels]", sections)
	}
}

func TestChangeSetStringNoChanges(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{}
	if cs.String() != "no changes" {
		t.Fatalf("String() = %q, want %q", cs.String(), "no changes")
	}
}

func TestChangeSetStringWithChanges(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{
		Changes: []Change{
			{Section: "agent", Kind: ChangeUpdated},
			{Section: "models", Kind: ChangeAdded},
		},
	}

	s := cs.String()
	if !strings.Contains(s, "agent (updated)") {
		t.Fatalf("String() = %q, missing 'agent (updated)'", s)
	}
	if !strings.Contains(s, "models (added)") {
		t.Fatalf("String() = %q, missing 'models (added)'", s)
	}
}

func TestDiffIdenticalConfigs(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Agent: AgentConfig{DefaultModel: "gpt-4o"},
	}

	cs := Diff(cfg, cfg)
	if cs.HasChanges() {
		t.Fatalf("Diff of identical configs should have no changes, got %v", cs.Sections())
	}
	if cs.Fatal {
		t.Fatal("Diff of identical configs should not be fatal")
	}
}

func TestDiffDetectsAgentChange(t *testing.T) {
	t.Parallel()

	old := Config{Agent: AgentConfig{DefaultModel: "gpt-4o"}}
	newCfg := Config{Agent: AgentConfig{DefaultModel: "claude-sonnet-4-20250514"}}

	cs := Diff(old, newCfg)
	if !cs.HasChanges() {
		t.Fatal("expected changes when agent model differs")
	}
	if !cs.HasSection("agent") {
		t.Fatal("expected agent section in changes")
	}
	if cs.Fatal {
		t.Fatal("agent changes should not be fatal")
	}
}

func TestDiffDetectsServerAddressChangeFatal(t *testing.T) {
	t.Parallel()

	old := Config{Server: ServerConfig{Address: ":8080"}}
	newCfg := Config{Server: ServerConfig{Address: ":9090"}}

	cs := Diff(old, newCfg)
	if !cs.HasChanges() {
		t.Fatal("expected changes when server address differs")
	}
	if !cs.Fatal {
		t.Fatal("server address change should be fatal")
	}
}

func TestDiffDetectsStoreBackendChangeFatal(t *testing.T) {
	t.Parallel()

	old := Config{Store: StoreConfig{Backend: "memory"}}
	newCfg := Config{Store: StoreConfig{Backend: "jsonl"}}

	cs := Diff(old, newCfg)
	if !cs.HasChanges() {
		t.Fatal("expected changes when store backend differs")
	}
	if !cs.Fatal {
		t.Fatal("store backend change should be fatal")
	}
}

func TestDiffDetectsAdditionalTopLevelSectionChanges(t *testing.T) {
	t.Parallel()

	trueValue := true
	old := Config{}
	newCfg := Config{
		Update: UpdateConfig{
			Enabled: &trueValue,
			Channel: "beta",
		},
		Diagnostics: DiagnosticsConfig{
			BugReportDir: "/tmp/bugs",
		},
		Watch: WatchConfig{
			StorePath: "/tmp/watch.json",
		},
		Embedding: EmbeddingConfig{
			Provider: "openai",
		},
	}

	cs := Diff(old, newCfg)
	for _, section := range []string{"update", "diagnostics", "watch", "embedding"} {
		if !cs.HasSection(section) {
			t.Fatalf("expected %s section in changes, got %v", section, cs.Sections())
		}
	}
	if cs.Fatal {
		t.Fatal("additional top-level section changes should not be fatal")
	}
}

func TestDiffDetectsOtherOmittedSiblingSections(t *testing.T) {
	t.Parallel()

	old := Config{}
	newCfg := Config{
		Auth: AuthConfig{
			BearerToken: "secret",
		},
		Logging: LoggingConfig{
			Level: "debug",
		},
		Locale:        "zh-CN",
		UsageStorage:  "sqlite",
		MemoryStorage: "sqlite",
	}

	cs := Diff(old, newCfg)
	for _, section := range []string{"auth", "logging", "locale", "usage_storage", "memory_storage"} {
		if !cs.HasSection(section) {
			t.Fatalf("expected %s section in changes, got %v", section, cs.Sections())
		}
	}
}

func TestChangeKindConstants(t *testing.T) {
	t.Parallel()

	if ChangeAdded != "added" {
		t.Fatalf("ChangeAdded = %q", ChangeAdded)
	}
	if ChangeRemoved != "removed" {
		t.Fatalf("ChangeRemoved = %q", ChangeRemoved)
	}
	if ChangeUpdated != "updated" {
		t.Fatalf("ChangeUpdated = %q", ChangeUpdated)
	}
}
