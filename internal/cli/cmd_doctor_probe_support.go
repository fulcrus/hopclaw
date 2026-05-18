package cli

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
)

const (
	doctorProbeSecretInventory = "security.secret_inventory"
	doctorProbeRuntimeDB       = "storage.runtime_db"
	doctorProbeControlDB       = "storage.control_db"
	doctorProbeKnowledgeDB     = "storage.knowledge_db"
	doctorProbeAuditDB         = "storage.audit_db"
	doctorProbeKnowledgeIndex  = "storage.knowledge_indexes"
	doctorProbeDurableFacts    = "storage.durable_facts"
)

func doctorProbeRegistry(storage controlplane.StorageSummary, inventory config.SecretRefInventory) controlplane.ProbeRegistry {
	return controlplane.NewProbeRegistry(
		controlplane.ProbeDefinition{
			ID:       doctorProbeSecretInventory,
			Category: "security",
			Name:     "Secret exposure",
			Run: func(context.Context) controlplane.ProbeResult {
				return controlplane.ProbeSecretInventory(inventory)
			},
		},
		controlplane.ProbeDefinition{
			ID:       doctorProbeRuntimeDB,
			Category: "storage",
			Name:     "Runtime DB",
			Run: func(ctx context.Context) controlplane.ProbeResult {
				return controlplane.ProbeSQLiteDatabase(ctx, "Runtime DB", storage.Backend, storage.RuntimeDBPath)
			},
		},
		controlplane.ProbeDefinition{
			ID:       doctorProbeControlDB,
			Category: "storage",
			Name:     "Control DB",
			Run: func(ctx context.Context) controlplane.ProbeResult {
				return controlplane.ProbeSQLiteDatabase(ctx, "Control DB", storage.Backend, storage.ControlDBPath)
			},
		},
		controlplane.ProbeDefinition{
			ID:       doctorProbeKnowledgeDB,
			Category: "storage",
			Name:     "Knowledge DB",
			Run: func(ctx context.Context) controlplane.ProbeResult {
				return controlplane.ProbeSQLiteDatabase(ctx, "Knowledge DB", storage.Backend, storage.KnowledgeDBPath)
			},
		},
		controlplane.ProbeDefinition{
			ID:       doctorProbeAuditDB,
			Category: "storage",
			Name:     "Audit DB",
			Run: func(ctx context.Context) controlplane.ProbeResult {
				return controlplane.ProbeSQLiteDatabase(ctx, "Audit DB", storage.Backend, storage.AuditDBPath)
			},
		},
		controlplane.ProbeDefinition{
			ID:       doctorProbeKnowledgeIndex,
			Category: "storage",
			Name:     "Knowledge indexes",
			Run: func(ctx context.Context) controlplane.ProbeResult {
				return controlplane.ProbeKnowledgeIndexes(ctx, storage)
			},
		},
		controlplane.ProbeDefinition{
			ID:       doctorProbeDurableFacts,
			Category: "storage",
			Name:     "Durable facts",
			Run: func(ctx context.Context) controlplane.ProbeResult {
				return controlplane.ProbeDurableFacts(ctx, storage)
			},
		},
	)
}

func doctorCheckFromProbeResult(result controlplane.ProbeResult) checkResult {
	return checkResult{
		Category: strings.TrimSpace(result.Category),
		Name:     strings.TrimSpace(result.Name),
		Status:   strings.TrimSpace(string(result.Status)),
		Detail:   strings.TrimSpace(result.Detail),
		Fix:      strings.TrimSpace(result.Fix),
	}
}

func loadDoctorConfig() (config.Config, bool, error) {
	configPath := resolveConfigPath()
	if configPath == "" {
		return config.Config{}, false, nil
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return config.Config{}, true, err
	}
	return cfg, true, nil
}

func resolveDoctorStorageSummary() (controlplane.StorageSummary, error) {
	layout := doctorStorageLayout{
		root: filepath.Join(".hopclaw", "state"),
	}
	configPath := resolveConfigPath()
	if configPath != "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			return controlplane.StorageSummary{}, err
		}
		layout.backend = strings.TrimSpace(strings.ToLower(cfg.Store.Backend))
		if root := strings.TrimSpace(cfg.Store.Path); root != "" {
			layout.root = root
		}
	}
	layout.runtimeDBPath = filepath.Join(layout.root, "runtime.db")
	layout.controlDBPath = filepath.Join(layout.root, "control.db")
	layout.knowledgeDBPath = filepath.Join(layout.root, "knowledge.db")
	layout.auditDBPath = filepath.Join(layout.root, "audit.db")
	return controlplane.StorageSummary{
		Backend:         layout.backend,
		Root:            layout.root,
		SplitDatabases:  strings.EqualFold(layout.backend, "sqlite"),
		RuntimeDBPath:   layout.runtimeDBPath,
		ControlDBPath:   layout.controlDBPath,
		KnowledgeDBPath: layout.knowledgeDBPath,
		AuditDBPath:     layout.auditDBPath,
	}, nil
}

func runDoctorProbeByID(id string, storage controlplane.StorageSummary, inventory config.SecretRefInventory) checkResult {
	ctx, cancel := context.WithTimeout(context.Background(), doctorValidateTimeout)
	defer cancel()

	results := doctorProbeRegistry(storage, inventory).RunByID(ctx, id)
	if len(results) == 0 {
		return checkResult{
			Category: "storage",
			Name:     id,
			Status:   "warn",
			Detail:   "probe is not registered",
		}
	}
	return doctorCheckFromProbeResult(results[0])
}
