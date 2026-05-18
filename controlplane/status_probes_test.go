package controlplane

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/knowledge"
)

func TestBuildStorageSummary(t *testing.T) {
	t.Parallel()

	summary := BuildStorageSummary(config.Config{
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    "/tmp/hopclaw-state",
		},
	})
	if summary.Backend != "sqlite" || !summary.SplitDatabases {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.RuntimeDBPath != filepath.Join("/tmp/hopclaw-state", "runtime.db") {
		t.Fatalf("runtime db path = %q", summary.RuntimeDBPath)
	}
}

func TestProbeSecretInventoryWarnsOnLiteralSecrets(t *testing.T) {
	t.Parallel()

	result := ProbeSecretInventory(config.SecretRefInventory{
		Count: 3,
		ByKind: map[string]int{
			string(config.SecretRefKindEnv):      1,
			string(config.SecretRefKindKeychain): 1,
			string(config.SecretRefKindLiteral):  1,
		},
	})
	if result.Status != ProbeStatusWarn {
		t.Fatalf("result = %+v, want warn", result)
	}
	if !strings.Contains(result.Fix, "replace literal secrets") {
		t.Fatalf("result = %+v", result)
	}
}

func TestProbeOperationalWarningsReportsWarn(t *testing.T) {
	t.Parallel()

	result := ProbeOperationalWarnings([]OperationalWarning{{
		Component: "config_store",
		Summary:   "Dynamic config store unavailable; using YAML-only mode",
		Fix:       "restore control DB access",
	}})
	if result.Status != ProbeStatusWarn {
		t.Fatalf("result = %+v, want warn", result)
	}
	if !strings.Contains(result.Detail, "Dynamic config store unavailable; using YAML-only mode") {
		t.Fatalf("result.Detail = %q", result.Detail)
	}
	if result.Fix != "restore control DB access" {
		t.Fatalf("result.Fix = %q", result.Fix)
	}
}

func TestProbeRegistryRunByCategoryNormalizesMetadata(t *testing.T) {
	t.Parallel()

	registry := NewProbeRegistry(
		ProbeDefinition{
			ID:       "storage.example",
			Category: "storage",
			Name:     "Example",
			Run: func(context.Context) ProbeResult {
				return ProbeResult{Status: ProbeStatusOK, Detail: "healthy"}
			},
		},
		ProbeDefinition{
			ID:       "security.example",
			Category: "security",
			Name:     "Secret exposure",
			Run: func(context.Context) ProbeResult {
				return ProbeResult{Status: ProbeStatusWarn, Detail: "literal secret"}
			},
		},
	)
	results := registry.RunByCategory(context.Background(), "security")
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].ID != "security.example" || results[0].Category != "security" || results[0].Name != "Secret exposure" {
		t.Fatalf("result = %+v", results[0])
	}
}

func TestProbeKnowledgeIndexesReportsHealthyProjection(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "knowledge.db")
	store, err := knowledge.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	if _, err := store.UpsertSource(context.Background(), knowledge.Source{
		ID:      "source-1",
		Name:    "Docs",
		Kind:    knowledge.SourceKindLocalDir,
		Enabled: true,
	}); err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	result := ProbeKnowledgeIndexes(context.Background(), StorageSummary{
		Backend:         "sqlite",
		KnowledgeDBPath: dbPath,
	})
	if result.Status != ProbeStatusOK {
		t.Fatalf("result = %+v, want ok", result)
	}
	if !strings.Contains(result.Detail, "1 source(s)") {
		t.Fatalf("result.Detail = %q, want source count", result.Detail)
	}
}
