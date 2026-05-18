package bootstrap

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/config"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	"github.com/fulcrus/hopclaw/store"
	"github.com/fulcrus/hopclaw/usage"
	"github.com/fulcrus/hopclaw/watch"
)

type sqliteDomainStores struct {
	runtime   *sql.DB
	control   *sql.DB
	knowledge *sql.DB
	audit     *sql.DB
}

func initStores(cfg config.StoreConfig, stateCfg config.StatusConfig, layout storageLayout) (agent.SessionStore, agent.RunStore, approval.Store, sqliteDomainStores, error) {
	switch strings.TrimSpace(strings.ToLower(cfg.Backend)) {
	case "memory":
		return agent.NewInMemorySessionStore(), agent.NewInMemoryRunStore(), &approval.MetricsStore{Inner: approval.NewInMemoryStore()}, sqliteDomainStores{}, nil
	case "jsonl":
		opts := store.JSONLStoreOptions{StartupLimit: stateCfg.JSONLStartupLimit}
		sessions, err := store.NewJSONLSessionStoreWithOptions(cfg.Path, opts)
		if err != nil {
			return nil, nil, nil, sqliteDomainStores{}, err
		}
		runs, err := store.NewJSONLRunStoreWithOptions(cfg.Path, opts)
		if err != nil {
			return nil, nil, nil, sqliteDomainStores{}, err
		}
		approvals, err := store.NewJSONLApprovalStore(cfg.Path)
		if err != nil {
			return nil, nil, nil, sqliteDomainStores{}, err
		}
		return sessions, runs, &approval.MetricsStore{Inner: approvals}, sqliteDomainStores{}, nil
	case "sqlite":
		if err := os.MkdirAll(layout.root, 0o755); err != nil {
			return nil, nil, nil, sqliteDomainStores{}, fmt.Errorf("create store directory: %w", err)
		}
		runtimeDB, err := store.OpenDB(layout.runtimeDBPath)
		if err != nil {
			return nil, nil, nil, sqliteDomainStores{}, fmt.Errorf("open runtime sqlite store: %w", err)
		}
		controlDB, err := store.OpenDB(layout.controlDBPath)
		if err != nil {
			_ = runtimeDB.Close()
			return nil, nil, nil, sqliteDomainStores{}, fmt.Errorf("open control sqlite store: %w", err)
		}
		knowledgeDB, err := store.OpenDB(layout.knowledgeDBPath)
		if err != nil {
			_ = runtimeDB.Close()
			_ = controlDB.Close()
			return nil, nil, nil, sqliteDomainStores{}, fmt.Errorf("open knowledge sqlite store: %w", err)
		}
		auditDB, err := store.OpenDB(layout.auditDBPath)
		if err != nil {
			_ = runtimeDB.Close()
			_ = controlDB.Close()
			_ = knowledgeDB.Close()
			return nil, nil, nil, sqliteDomainStores{}, fmt.Errorf("open audit sqlite store: %w", err)
		}
		sessions := store.NewSQLiteSessionStore(runtimeDB)
		runs := store.NewSQLiteRunStore(runtimeDB)
		approvals := store.NewSQLiteApprovalStore(runtimeDB)
		return sessions, runs, &approval.MetricsStore{Inner: approvals}, sqliteDomainStores{
			runtime:   runtimeDB,
			control:   controlDB,
			knowledge: knowledgeDB,
			audit:     auditDB,
		}, nil
	default:
		return nil, nil, nil, sqliteDomainStores{}, fmt.Errorf("unsupported store backend %q", cfg.Backend)
	}
}

func governanceDeliveryConfig(cfg config.GovernanceDeliveryConfig) controlgov.DeliveryConfig {
	return controlgov.DeliveryConfig{
		MaxAttempts:  cfg.MaxAttempts,
		BaseBackoff:  cfg.BaseBackoff,
		MaxBackoff:   cfg.MaxBackoff,
		PollInterval: cfg.PollInterval,
		BatchSize:    cfg.BatchSize,
	}
}

func buildGovernanceDeliveryStore(cfg config.Config, sharedDB *sql.DB) (controlgov.DeliveryStore, *sql.DB, error) {
	delivery := cfg.Runtime.Governance.Delivery
	switch strings.TrimSpace(strings.ToLower(delivery.Backend)) {
	case "", "memory":
		return controlgov.NewInMemoryDeliveryStore(), nil, nil
	case "jsonl":
		deliveryStore, err := controlgov.NewJSONLDeliveryStore(governanceDeliveryRoot(cfg.Store, delivery))
		if err != nil {
			return nil, nil, err
		}
		return deliveryStore, nil, nil
	case "sqlite":
		if sharedDB != nil && strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "sqlite") {
			deliveryStore, err := controlgov.NewSQLiteDeliveryStore(sharedDB)
			if err != nil {
				return nil, nil, err
			}
			return deliveryStore, nil, nil
		}
		root := governanceDeliveryRoot(cfg.Store, delivery)
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, nil, fmt.Errorf("create governance delivery directory: %w", err)
		}
		db, err := store.OpenDB(filepath.Join(root, "governance_delivery.db"))
		if err != nil {
			return nil, nil, fmt.Errorf("open governance delivery sqlite store: %w", err)
		}
		deliveryStore, err := controlgov.NewSQLiteDeliveryStore(db)
		if err != nil {
			db.Close()
			return nil, nil, err
		}
		return deliveryStore, db, nil
	default:
		return nil, nil, fmt.Errorf("unsupported governance delivery backend %q", delivery.Backend)
	}
}

func governanceDeliveryRoot(storeCfg config.StoreConfig, delivery config.GovernanceDeliveryConfig) string {
	if root := strings.TrimSpace(delivery.Path); root != "" {
		return root
	}
	if root := strings.TrimSpace(storeCfg.Path); root != "" {
		return root
	}
	return ".hopclaw/state"
}

func initUsageStore(storagePath string) (usage.Store, error) {
	storagePath = strings.TrimSpace(storagePath)
	if storagePath == "" || storagePath == "memory" {
		return usage.NewInMemoryStore(), nil
	}
	return usage.NewJSONLStore(storagePath)
}

func sessionInboxReader(store agent.SessionStore) watch.SessionInboxReader {
	return agent.SessionKeyReaderCapability(store)
}
