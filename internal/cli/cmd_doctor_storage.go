package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/durablefact"
	"github.com/fulcrus/hopclaw/internal/daemon"

	_ "modernc.org/sqlite"
)

const doctorAuditWarnBytes int64 = 1024 * 1024 * 1024

type doctorStorageLayout struct {
	backend         string
	root            string
	runtimeDBPath   string
	controlDBPath   string
	knowledgeDBPath string
	auditDBPath     string
}

type doctorDurableFactCounts struct {
	total          int
	reviewRequired int
}

type doctorKnowledgeIndexStats struct {
	sources        int
	erroredSources int
	documents      int
	chunks         int
	ftsRows        int
	vectors        int
	locales        int
}

func checkStateDir() checkResult {
	dir := daemon.StateDir()
	info, err := os.Stat(dir)
	if err != nil {
		return checkResult{
			Category: "storage",
			Name:     "State directory",
			Status:   "warn",
			Detail:   fmt.Sprintf("%s does not exist; run 'hopclaw setup'", dir),
			Fix:      "auto:mkdir_state_dir",
		}
	}
	if !info.IsDir() {
		return checkResult{
			Category: "storage",
			Name:     "State directory",
			Status:   "fail",
			Detail:   fmt.Sprintf("%s exists but is not a directory", dir),
		}
	}
	return checkResult{
		Category: "storage",
		Name:     "State directory",
		Status:   "ok",
		Detail:   dir,
	}
}

func checkRuntimeDatabase() checkResult {
	return checkStorageDatabase("Runtime DB", func(layout doctorStorageLayout) string { return layout.runtimeDBPath })
}

func checkControlDatabase() checkResult {
	return checkStorageDatabase("Control DB", func(layout doctorStorageLayout) string { return layout.controlDBPath })
}

func checkKnowledgeDatabase() checkResult {
	return checkStorageDatabase("Knowledge DB", func(layout doctorStorageLayout) string { return layout.knowledgeDBPath })
}

func checkKnowledgeIndexes() checkResult {
	storage, err := resolveDoctorStorageSummary()
	if err != nil {
		return checkResult{
			Category: "storage",
			Name:     "Knowledge indexes",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}
	return runDoctorProbeByID(doctorProbeKnowledgeIndex, storage, config.SecretRefInventory{})
}

func checkDurableFactsSummary() checkResult {
	storage, err := resolveDoctorStorageSummary()
	if err != nil {
		return checkResult{
			Category: "storage",
			Name:     "Durable facts",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}
	return runDoctorProbeByID(doctorProbeDurableFacts, storage, config.SecretRefInventory{})
}

func doctorSummarizeContextFacts(ctx context.Context, path string) (doctorDurableFactCounts, bool, error) {
	store, db, present, err := openDoctorDurableFactStore(ctx, path)
	if err != nil || !present {
		return doctorDurableFactCounts{}, present, err
	}
	defer db.Close()

	views, err := store.ListContextViews(ctx, durablefact.Filter{})
	if err != nil {
		return doctorDurableFactCounts{}, true, fmt.Errorf("list context durable facts: %w", err)
	}
	reviewRequired := true
	reviewViews, err := store.ListContextViews(ctx, durablefact.Filter{ReviewRequired: &reviewRequired})
	if err != nil {
		return doctorDurableFactCounts{}, true, fmt.Errorf("list context durable facts review backlog: %w", err)
	}
	return doctorDurableFactCounts{
		total:          len(views),
		reviewRequired: len(reviewViews),
	}, true, nil
}

func doctorSummarizeConfigFacts(ctx context.Context, path string) (doctorDurableFactCounts, bool, error) {
	store, db, present, err := openDoctorDurableFactStore(ctx, path)
	if err != nil || !present {
		return doctorDurableFactCounts{}, present, err
	}
	defer db.Close()

	views, err := store.ListConfigViews(ctx, durablefact.Filter{})
	if err != nil {
		return doctorDurableFactCounts{}, true, fmt.Errorf("list config durable facts: %w", err)
	}
	reviewRequired := true
	reviewViews, err := store.ListConfigViews(ctx, durablefact.Filter{ReviewRequired: &reviewRequired})
	if err != nil {
		return doctorDurableFactCounts{}, true, fmt.Errorf("list config durable facts review backlog: %w", err)
	}
	return doctorDurableFactCounts{
		total:          len(views),
		reviewRequired: len(reviewViews),
	}, true, nil
}

func openDoctorDurableFactStore(ctx context.Context, path string) (*durablefact.SQLiteStore, *sql.DB, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, false, nil
		}
		return nil, nil, false, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, nil, false, fmt.Errorf("%s is a directory, expected a sqlite database file", path)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, nil, false, fmt.Errorf("open %s: %w", path, err)
	}

	var tableCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'durable_facts'`).Scan(&tableCount); err != nil {
		_ = db.Close()
		return nil, nil, false, fmt.Errorf("query durable_facts schema in %s: %w", path, err)
	}
	if tableCount == 0 {
		_ = db.Close()
		return nil, nil, false, nil
	}

	factStore, err := durablefact.NewSQLiteStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, false, fmt.Errorf("open durable facts store in %s: %w", path, err)
	}
	return factStore, db, true, nil
}

func doctorSummarizeKnowledgeIndexes(ctx context.Context, path string) (doctorKnowledgeIndexStats, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doctorKnowledgeIndexStats{}, false, nil
		}
		return doctorKnowledgeIndexStats{}, false, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return doctorKnowledgeIndexStats{}, false, fmt.Errorf("%s is a directory, expected a sqlite database file", path)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return doctorKnowledgeIndexStats{}, false, fmt.Errorf("open %s: %w", path, err)
	}
	defer db.Close()

	for _, table := range []string{"knowledge_sources", "knowledge_documents", "knowledge_chunks", "knowledge_chunk_fts", "knowledge_chunk_vectors"} {
		present, err := doctorSQLiteTableExists(ctx, db, table)
		if err != nil {
			return doctorKnowledgeIndexStats{}, false, err
		}
		if !present {
			return doctorKnowledgeIndexStats{}, false, nil
		}
	}

	stats := doctorKnowledgeIndexStats{}
	for _, item := range []struct {
		dest  *int
		query string
	}{
		{&stats.sources, `SELECT COUNT(*) FROM knowledge_sources`},
		{&stats.erroredSources, `SELECT COUNT(*) FROM knowledge_sources WHERE TRIM(COALESCE(last_error, '')) <> '' OR LOWER(TRIM(COALESCE(status, ''))) IN ('degraded', 'blocked')`},
		{&stats.documents, `SELECT COUNT(*) FROM knowledge_documents`},
		{&stats.chunks, `SELECT COUNT(*) FROM knowledge_chunks`},
		{&stats.ftsRows, `SELECT COUNT(*) FROM knowledge_chunk_fts`},
		{&stats.vectors, `SELECT COUNT(*) FROM knowledge_chunk_vectors`},
		{&stats.locales, `SELECT COUNT(DISTINCT NULLIF(TRIM(locale), '')) FROM knowledge_chunks`},
	} {
		if err := db.QueryRowContext(ctx, item.query).Scan(item.dest); err != nil {
			return doctorKnowledgeIndexStats{}, true, fmt.Errorf("query knowledge index stats: %w", err)
		}
	}
	return stats, true, nil
}

func doctorSQLiteTableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		return false, fmt.Errorf("query sqlite schema for %s: %w", table, err)
	}
	return count > 0, nil
}

func checkAuditDatabase() checkResult {
	return checkStorageDatabase("Audit DB", func(layout doctorStorageLayout) string { return layout.auditDBPath })
}

func checkSessionLocks() checkResult {
	dir := daemon.StateDir()
	if _, err := os.Stat(dir); err != nil {
		return checkResult{
			Category: "storage",
			Name:     "Session locks",
			Status:   "ok",
			Detail:   "state directory not found; skipped",
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return checkResult{
			Category: "storage",
			Name:     "Session locks",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot read state dir: %v", err),
		}
	}

	now := time.Now()
	var stale []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), lockFileExtension) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > staleLockAge {
			stale = append(stale, e.Name())
		}
	}

	if len(stale) > 0 {
		return checkResult{
			Category: "storage",
			Name:     "Session locks",
			Status:   "warn",
			Detail:   fmt.Sprintf("%d stale lock file(s) found (older than %s)", len(stale), staleLockAge),
			Fix:      "auto:clear_stale_locks",
		}
	}

	return checkResult{
		Category: "storage",
		Name:     "Session locks",
		Status:   "ok",
		Detail:   "no stale lock files",
	}
}

func checkDiskSpace() checkResult {
	dir := daemon.StateDir()
	if _, err := os.Stat(dir); err != nil {
		return checkResult{
			Category: "storage",
			Name:     "Disk space",
			Status:   "ok",
			Detail:   "state directory not found; skipped",
		}
	}

	free, err := getFreeDiskSpace(dir)
	if err != nil {
		return checkResult{
			Category: "storage",
			Name:     "Disk space",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot determine free space: %v", err),
		}
	}

	freeMB := free / (1024 * 1024)
	if free < minFreeDiskBytes {
		return checkResult{
			Category: "storage",
			Name:     "Disk space",
			Status:   "warn",
			Detail:   fmt.Sprintf("only %d MB free in state dir partition", freeMB),
			Fix:      "free up disk space or move state dir",
		}
	}

	return checkResult{
		Category: "storage",
		Name:     "Disk space",
		Status:   "ok",
		Detail:   fmt.Sprintf("%d MB free", freeMB),
	}
}

func checkStateIntegrity() checkResult {
	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "storage",
			Name:     "State integrity",
			Status:   "ok",
			Detail:   "no config file; skipped",
		}
	}

	cfg, err := config.Load(p)
	if err != nil {
		return checkResult{
			Category: "storage",
			Name:     "State integrity",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}

	if strings.TrimSpace(strings.ToLower(cfg.Store.Backend)) != "jsonl" || strings.TrimSpace(cfg.Store.Path) == "" {
		return checkResult{
			Category: "storage",
			Name:     "State integrity",
			Status:   "ok",
			Detail:   "not using jsonl backend; skipped",
		}
	}

	storePath := cfg.Store.Path
	data, err := os.ReadFile(storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return checkResult{
				Category: "storage",
				Name:     "State integrity",
				Status:   "ok",
				Detail:   "store file not yet created",
			}
		}
		return checkResult{
			Category: "storage",
			Name:     "State integrity",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot read store file: %v", err),
		}
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return checkResult{
			Category: "storage",
			Name:     "State integrity",
			Status:   "ok",
			Detail:   "store file is empty",
		}
	}

	lines := strings.Split(content, "\n")
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	if !json.Valid([]byte(lastLine)) {
		return checkResult{
			Category: "storage",
			Name:     "State integrity",
			Status:   "warn",
			Detail:   "last line of JSONL store is not valid JSON (possible corruption)",
			Fix:      "check and repair the store file manually",
		}
	}

	return checkResult{
		Category: "storage",
		Name:     "State integrity",
		Status:   "ok",
		Detail:   fmt.Sprintf("jsonl store OK (%d lines)", len(lines)),
	}
}

func resolveDoctorStorageLayout() (doctorStorageLayout, error) {
	layout := doctorStorageLayout{
		root: filepath.Join(".hopclaw", "state"),
	}

	configPath := resolveConfigPath()
	if configPath != "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			return doctorStorageLayout{}, err
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
	return layout, nil
}

func checkStorageDatabase(name string, resolvePath func(doctorStorageLayout) string) checkResult {
	layout, err := resolveDoctorStorageLayout()
	if err != nil {
		return checkResult{
			Category: "storage",
			Name:     name,
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}
	storage, err := resolveDoctorStorageSummary()
	if err != nil {
		return checkResult{
			Category: "storage",
			Name:     name,
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}
	_ = resolvePath(layout)
	id := doctorProbeRuntimeDB
	switch name {
	case "Control DB":
		id = doctorProbeControlDB
	case "Knowledge DB":
		id = doctorProbeKnowledgeDB
	case "Audit DB":
		id = doctorProbeAuditDB
	}
	return runDoctorProbeByID(id, storage, config.SecretRefInventory{})
}

func doctorFormatBytes(size int64) string {
	value := float64(size)
	units := []string{"B", "KB", "MB", "GB", "TB"}
	unit := units[0]
	for i := 1; i < len(units) && value >= 1024; i++ {
		value /= 1024
		unit = units[i]
	}
	if unit == "B" {
		return fmt.Sprintf("%d %s", size, unit)
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}
