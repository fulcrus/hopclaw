package controlplane

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/durablefact"
	"github.com/fulcrus/hopclaw/internal/execenv"
	"github.com/fulcrus/hopclaw/skill"

	_ "modernc.org/sqlite"
)

type ProbeStatus string

const (
	ProbeStatusOK   ProbeStatus = "ok"
	ProbeStatusWarn ProbeStatus = "warn"
	ProbeStatusFail ProbeStatus = "fail"
)

type ProbeResult struct {
	ID       string      `json:"id"`
	Category string      `json:"category,omitempty"`
	Name     string      `json:"name"`
	Status   ProbeStatus `json:"status"`
	Detail   string      `json:"detail,omitempty"`
	Fix      string      `json:"fix,omitempty"`
}

type ProbeDefinition struct {
	ID       string
	Category string
	Name     string
	Run      func(context.Context) ProbeResult
}

type ProbeRegistry struct {
	probes []ProbeDefinition
}

type RuntimeFactsSummary struct {
	ContextFingerprint          string `json:"context_fingerprint"`
	ManagedSkillCount           int    `json:"managed_skill_count"`
	ManagedInjectedEnvCount     int    `json:"managed_injected_env_count"`
	ConfigTruthCount            int    `json:"config_truth_count"`
	ResolvedSecretPresenceCount int    `json:"resolved_secret_presence_count"`
}

type ChildEnvPolicySummary struct {
	ModuleExecBaselineKeys    []string `json:"module_exec_baseline_keys"`
	InstallerExecBaselineKeys []string `json:"installer_exec_baseline_keys"`
	OverlaySupported          bool     `json:"overlay_supported"`
	MutatesHostProcess        bool     `json:"mutates_host_process"`
	InheritsFullParentEnv     bool     `json:"inherits_full_parent_env"`
}

type StorageSummary struct {
	Backend               string   `json:"backend"`
	Root                  string   `json:"root,omitempty"`
	SplitDatabases        bool     `json:"split_databases"`
	RuntimeDBPath         string   `json:"runtime_db_path,omitempty"`
	ControlDBPath         string   `json:"control_db_path,omitempty"`
	KnowledgeDBPath       string   `json:"knowledge_db_path,omitempty"`
	AuditDBPath           string   `json:"audit_db_path,omitempty"`
	AppendOnlyTranscript  bool     `json:"append_only_transcript"`
	TranscriptEventTable  string   `json:"transcript_event_table,omitempty"`
	JSONLResponsibilities []string `json:"jsonl_responsibilities,omitempty"`
}

type ResultProjectionSummary struct {
	UnifiedEventLedger     bool     `json:"unified_event_ledger"`
	EventClasses           []string `json:"event_classes,omitempty"`
	RunResultSources       []string `json:"run_result_sources,omitempty"`
	DeliveryEnvelope       bool     `json:"delivery_envelope"`
	DeliveryOutbox         bool     `json:"delivery_outbox"`
	DeliveryOutboxTable    string   `json:"delivery_outbox_table,omitempty"`
	ReceiptSource          string   `json:"receipt_source,omitempty"`
	IdempotencyKeyRequired bool     `json:"idempotency_key_required"`
}

type KnowledgeSummary struct {
	TypedSourceMetadata   bool     `json:"typed_source_metadata"`
	TypedDocumentMetadata bool     `json:"typed_document_metadata"`
	TypedChunkMetadata    bool     `json:"typed_chunk_metadata"`
	IncrementalSync       bool     `json:"incremental_sync"`
	SyncCursorField       string   `json:"sync_cursor_field,omitempty"`
	PersistentFTSIndex    bool     `json:"persistent_fts_index"`
	PersistentVectorIndex bool     `json:"persistent_vector_index"`
	ProjectionOnly        bool     `json:"projection_only"`
	LocaleAwareRetrieval  bool     `json:"locale_aware_retrieval"`
	TruthTables           []string `json:"truth_tables,omitempty"`
	ProjectionTables      []string `json:"projection_tables,omitempty"`
}

type StatusProbeInput struct {
	Storage             StorageSummary
	Results             ResultProjectionSummary
	Knowledge           KnowledgeSummary
	Credentials         config.SecretRefInventory
	RuntimeFacts        RuntimeFactsSummary
	ChildEnvPolicy      ChildEnvPolicySummary
	OperationalWarnings []OperationalWarning
}

type durableFactCounts struct {
	total          int
	reviewRequired int
}

type knowledgeIndexStats struct {
	sources        int
	erroredSources int
	documents      int
	chunks         int
	ftsRows        int
	vectors        int
	locales        int
}

const auditWarnBytes int64 = 1024 * 1024 * 1024

func NewProbeRegistry(probes ...ProbeDefinition) ProbeRegistry {
	cloned := make([]ProbeDefinition, 0, len(probes))
	for _, probe := range probes {
		if strings.TrimSpace(probe.ID) == "" || probe.Run == nil {
			continue
		}
		cloned = append(cloned, probe)
	}
	return ProbeRegistry{probes: cloned}
}

func (r ProbeRegistry) RunAll(ctx context.Context) []ProbeResult {
	out := make([]ProbeResult, 0, len(r.probes))
	for _, probe := range r.probes {
		out = append(out, normalizeProbeResult(probe, probe.Run(ctx)))
	}
	return out
}

func (r ProbeRegistry) RunByCategory(ctx context.Context, category string) []ProbeResult {
	category = strings.TrimSpace(category)
	out := make([]ProbeResult, 0, len(r.probes))
	for _, probe := range r.probes {
		if category != "" && !strings.EqualFold(strings.TrimSpace(probe.Category), category) {
			continue
		}
		out = append(out, normalizeProbeResult(probe, probe.Run(ctx)))
	}
	return out
}

func (r ProbeRegistry) RunByID(ctx context.Context, ids ...string) []ProbeResult {
	allowed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	out := make([]ProbeResult, 0, len(allowed))
	for _, probe := range r.probes {
		if _, ok := allowed[probe.ID]; !ok {
			continue
		}
		out = append(out, normalizeProbeResult(probe, probe.Run(ctx)))
	}
	return out
}

func NewStatusProbeRegistry(input StatusProbeInput) ProbeRegistry {
	return NewProbeRegistry(
		ProbeDefinition{
			ID:       "storage.layout",
			Category: "storage",
			Name:     "Storage layout",
			Run: func(context.Context) ProbeResult {
				return ProbeStorageLayout(input.Storage)
			},
		},
		ProbeDefinition{
			ID:       "security.secret_inventory",
			Category: "security",
			Name:     "Secret exposure",
			Run: func(context.Context) ProbeResult {
				return ProbeSecretInventory(input.Credentials)
			},
		},
		ProbeDefinition{
			ID:       "runtime.runtime_facts",
			Category: "runtime",
			Name:     "Runtime facts",
			Run: func(context.Context) ProbeResult {
				return ProbeRuntimeFacts(input.RuntimeFacts)
			},
		},
		ProbeDefinition{
			ID:       "runtime.child_env_policy",
			Category: "runtime",
			Name:     "Child environment policy",
			Run: func(context.Context) ProbeResult {
				return ProbeChildEnvPolicy(input.ChildEnvPolicy)
			},
		},
		ProbeDefinition{
			ID:       "runtime.operational_warnings",
			Category: "runtime",
			Name:     "Operational warnings",
			Run: func(context.Context) ProbeResult {
				return ProbeOperationalWarnings(input.OperationalWarnings)
			},
		},
		ProbeDefinition{
			ID:       "results.projection",
			Category: "results",
			Name:     "Result projections",
			Run: func(context.Context) ProbeResult {
				return ProbeResultProjection(input.Results)
			},
		},
		ProbeDefinition{
			ID:       "knowledge.contract",
			Category: "knowledge",
			Name:     "Knowledge contract",
			Run: func(context.Context) ProbeResult {
				return ProbeKnowledgeContract(input.Knowledge)
			},
		},
	)
}

func ProbeIssues(results []ProbeResult) []string {
	out := make([]string, 0, len(results))
	for _, result := range results {
		switch result.Status {
		case ProbeStatusWarn, ProbeStatusFail:
			if detail := strings.TrimSpace(result.Detail); detail != "" {
				out = append(out, strings.ToLower(strings.TrimSpace(result.Name))+": "+detail)
			}
		}
	}
	return out
}

func BuildRuntimeFactsSummary(runtimeCtx skill.RuntimeContext) RuntimeFactsSummary {
	configTruthCount := len(runtimeCtx.ConfigTruth)
	managedInjectedEnvCount := 0
	resolvedSecretPresences := 0

	for _, status := range runtimeCtx.SecretPresence {
		if status.Resolved {
			resolvedSecretPresences++
		}
	}
	for _, entry := range runtimeCtx.Managed {
		managedInjectedEnvCount += len(entry.InjectedEnv)
		configTruthCount += len(entry.ConfigTruth)
		for _, status := range entry.InjectedEnv {
			if status.Resolved {
				resolvedSecretPresences++
			}
		}
	}

	return RuntimeFactsSummary{
		ContextFingerprint:          skill.FingerprintRuntimeContext(runtimeCtx),
		ManagedSkillCount:           len(runtimeCtx.Managed),
		ManagedInjectedEnvCount:     managedInjectedEnvCount,
		ConfigTruthCount:            configTruthCount,
		ResolvedSecretPresenceCount: resolvedSecretPresences,
	}
}

func BuildChildEnvPolicySummary() ChildEnvPolicySummary {
	return ChildEnvPolicySummary{
		ModuleExecBaselineKeys:    execenv.BaselineKeys(execenv.ModuleExecProfile),
		InstallerExecBaselineKeys: execenv.BaselineKeys(execenv.InstallerExecProfile),
		OverlaySupported:          true,
		MutatesHostProcess:        false,
		InheritsFullParentEnv:     false,
	}
}

func BuildStorageSummary(cfg config.Config) StorageSummary {
	cfg.ApplyDefaults()
	backend := strings.TrimSpace(strings.ToLower(cfg.Store.Backend))
	if backend == "" {
		backend = "sqlite"
	}
	root := strings.TrimSpace(cfg.Store.Path)
	summary := StorageSummary{
		Backend:              backend,
		Root:                 root,
		AppendOnlyTranscript: strings.EqualFold(backend, "sqlite"),
		JSONLResponsibilities: []string{
			"audit_trail",
			"export_import",
			"debug_wire_log",
		},
	}
	if !strings.EqualFold(backend, "sqlite") || root == "" {
		return summary
	}
	summary.SplitDatabases = true
	summary.RuntimeDBPath = filepath.Join(root, "runtime.db")
	summary.ControlDBPath = filepath.Join(root, "control.db")
	summary.KnowledgeDBPath = filepath.Join(root, "knowledge.db")
	summary.AuditDBPath = filepath.Join(root, "audit.db")
	summary.TranscriptEventTable = "transcript_events"
	return summary
}

func BuildResultProjectionSummary() ResultProjectionSummary {
	return ResultProjectionSummary{
		UnifiedEventLedger:     true,
		EventClasses:           []string{"evidence", "audit", "delivery"},
		RunResultSources:       []string{"transcript", "task_outcomes", "event_ledger"},
		DeliveryEnvelope:       true,
		DeliveryOutbox:         true,
		DeliveryOutboxTable:    "delivery_outbox",
		ReceiptSource:          "governance_deliveries",
		IdempotencyKeyRequired: true,
	}
}

func BuildKnowledgeSummary() KnowledgeSummary {
	return KnowledgeSummary{
		TypedSourceMetadata:   true,
		TypedDocumentMetadata: true,
		TypedChunkMetadata:    true,
		IncrementalSync:       true,
		SyncCursorField:       "sync_cursor",
		PersistentFTSIndex:    true,
		PersistentVectorIndex: true,
		ProjectionOnly:        true,
		LocaleAwareRetrieval:  true,
		TruthTables: []string{
			"knowledge_sources",
			"knowledge_documents",
			"knowledge_chunks",
		},
		ProjectionTables: []string{
			"knowledge_chunk_fts",
			"knowledge_chunk_vectors",
		},
	}
}

func ProbeStorageLayout(summary StorageSummary) ProbeResult {
	if strings.TrimSpace(summary.Backend) == "" {
		return ProbeResult{
			Status: ProbeStatusWarn,
			Detail: "storage backend is not configured",
			Fix:    "configure store.backend and initialize persistent state",
		}
	}
	detail := fmt.Sprintf("backend=%s", summary.Backend)
	if root := strings.TrimSpace(summary.Root); root != "" {
		detail += " root=" + root
	}
	if summary.SplitDatabases {
		detail += " split_databases=true"
	}
	return ProbeResult{Status: ProbeStatusOK, Detail: detail}
}

func ProbeSecretInventory(inventory config.SecretRefInventory) ProbeResult {
	if inventory.Count == 0 {
		return ProbeResult{
			Status: ProbeStatusOK,
			Detail: "no secret-bearing fields configured",
		}
	}
	literal := inventory.ByKind[string(config.SecretRefKindLiteral)]
	envRefs := inventory.ByKind[string(config.SecretRefKindEnv)]
	keychainRefs := inventory.ByKind[string(config.SecretRefKindKeychain)]
	detail := fmt.Sprintf("%d secret field(s): %d env, %d keychain, %d literal", inventory.Count, envRefs, keychainRefs, literal)
	if literal > 0 {
		return ProbeResult{
			Status: ProbeStatusWarn,
			Detail: detail,
			Fix:    "replace literal secrets with env:VAR or keychain:service/item references",
		}
	}
	return ProbeResult{Status: ProbeStatusOK, Detail: detail}
}

func ProbeRuntimeFacts(summary RuntimeFactsSummary) ProbeResult {
	if strings.TrimSpace(summary.ContextFingerprint) == "" {
		return ProbeResult{
			Status: ProbeStatusWarn,
			Detail: "runtime facts are empty or unavailable",
			Fix:    "rebuild runtime facts from a committed effective config snapshot",
		}
	}
	return ProbeResult{
		Status: ProbeStatusOK,
		Detail: fmt.Sprintf("%d managed skill(s), %d resolved secret presence(s), fingerprint=%s",
			summary.ManagedSkillCount, summary.ResolvedSecretPresenceCount, summary.ContextFingerprint),
	}
}

func ProbeChildEnvPolicy(summary ChildEnvPolicySummary) ProbeResult {
	if summary.MutatesHostProcess || summary.InheritsFullParentEnv {
		return ProbeResult{
			Status: ProbeStatusFail,
			Detail: "child execution policy is inheriting mutable host state",
			Fix:    "disable host process mutation and full parent env inheritance for child execution",
		}
	}
	if !summary.OverlaySupported {
		return ProbeResult{
			Status: ProbeStatusWarn,
			Detail: "overlay environment support is disabled",
			Fix:    "enable child env overlays before executing managed modules",
		}
	}
	return ProbeResult{
		Status: ProbeStatusOK,
		Detail: fmt.Sprintf("module_baseline=%d installer_baseline=%d overlay_supported=%t",
			len(summary.ModuleExecBaselineKeys), len(summary.InstallerExecBaselineKeys), summary.OverlaySupported),
	}
}

func ProbeResultProjection(summary ResultProjectionSummary) ProbeResult {
	if !summary.UnifiedEventLedger || !summary.DeliveryEnvelope || !summary.DeliveryOutbox || !summary.IdempotencyKeyRequired {
		return ProbeResult{
			Status: ProbeStatusWarn,
			Detail: "result projection contract is incomplete",
			Fix:    "restore event-ledger, delivery envelope, outbox, and idempotency guarantees",
		}
	}
	return ProbeResult{
		Status: ProbeStatusOK,
		Detail: fmt.Sprintf("event_classes=%d result_sources=%d receipt_source=%s",
			len(summary.EventClasses), len(summary.RunResultSources), summary.ReceiptSource),
	}
}

func ProbeKnowledgeContract(summary KnowledgeSummary) ProbeResult {
	if !summary.TypedSourceMetadata || !summary.TypedDocumentMetadata || !summary.TypedChunkMetadata ||
		!summary.IncrementalSync || !summary.PersistentFTSIndex || !summary.PersistentVectorIndex {
		return ProbeResult{
			Status: ProbeStatusWarn,
			Detail: "knowledge contract is missing one or more typed or projected guarantees",
			Fix:    "restore typed metadata, incremental sync, and persistent index projections",
		}
	}
	return ProbeResult{
		Status: ProbeStatusOK,
		Detail: fmt.Sprintf("truth_tables=%d projection_tables=%d sync_cursor=%s",
			len(summary.TruthTables), len(summary.ProjectionTables), summary.SyncCursorField),
	}
}

func ProbeSQLiteDatabase(ctx context.Context, name, backend, path string) ProbeResult {
	if strings.TrimSpace(path) == "" {
		return ProbeResult{
			Status: ProbeStatusOK,
			Detail: "sqlite domain store not configured",
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		switch {
		case os.IsNotExist(err) && strings.EqualFold(strings.TrimSpace(backend), "sqlite"):
			return ProbeResult{
				Status: ProbeStatusWarn,
				Detail: fmt.Sprintf("%s missing at %s", filepath.Base(path), path),
				Fix:    "start the gateway once to initialize sqlite storage",
			}
		case os.IsNotExist(err):
			currentBackend := strings.TrimSpace(backend)
			if currentBackend == "" {
				currentBackend = "default"
			}
			return ProbeResult{
				Status: ProbeStatusOK,
				Detail: fmt.Sprintf("store backend=%s; sqlite domain store not configured", currentBackend),
			}
		default:
			return ProbeResult{
				Status: ProbeStatusWarn,
				Detail: fmt.Sprintf("cannot stat %s: %v", path, err),
			}
		}
	}
	if info.IsDir() {
		return ProbeResult{
			Status: ProbeStatusFail,
			Detail: fmt.Sprintf("%s is a directory, expected a sqlite database file", path),
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return ProbeResult{
			Status: ProbeStatusFail,
			Detail: fmt.Sprintf("open %s: %v", path, err),
		}
	}
	defer db.Close()

	var quickCheck string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&quickCheck); err != nil {
		return ProbeResult{
			Status: ProbeStatusFail,
			Detail: fmt.Sprintf("integrity check failed for %s: %v", filepath.Base(path), err),
		}
	}
	if !strings.EqualFold(strings.TrimSpace(quickCheck), "ok") {
		return ProbeResult{
			Status: ProbeStatusFail,
			Detail: fmt.Sprintf("%s integrity check returned %q", filepath.Base(path), quickCheck),
			Fix:    "restore the database from backup or rebuild the local state",
		}
	}

	status := ProbeStatusOK
	detail := fmt.Sprintf("%s integrity OK (%s)", filepath.Base(path), formatBytes(info.Size()))
	fix := ""
	if name == "Audit DB" && info.Size() >= auditWarnBytes {
		status = ProbeStatusWarn
		detail = fmt.Sprintf("%s integrity OK (%s, consider pruning)", filepath.Base(path), formatBytes(info.Size()))
		fix = "prune or rotate audit history to keep audit.db compact"
	}
	return ProbeResult{Status: status, Detail: detail, Fix: fix}
}

func ProbeKnowledgeIndexes(ctx context.Context, storage StorageSummary) ProbeResult {
	path := strings.TrimSpace(storage.KnowledgeDBPath)
	if path == "" {
		return ProbeResult{
			Status: ProbeStatusOK,
			Detail: "knowledge store path is not configured",
		}
	}
	stats, present, err := summarizeKnowledgeIndexes(ctx, path)
	if err != nil {
		return ProbeResult{
			Status: ProbeStatusFail,
			Detail: fmt.Sprintf("inspect knowledge indexes: %v", err),
		}
	}
	if !present {
		return ProbeResult{
			Status: ProbeStatusOK,
			Detail: "knowledge store not initialized; skipped",
		}
	}

	detail := fmt.Sprintf("%d source(s), %d document(s), %d chunk(s), FTS=%d, vectors=%d, locales=%d",
		stats.sources, stats.documents, stats.chunks, stats.ftsRows, stats.vectors, stats.locales)
	if stats.sources == 0 && stats.documents == 0 && stats.chunks == 0 {
		return ProbeResult{
			Status: ProbeStatusOK,
			Detail: detail + " (no knowledge sources indexed yet)",
		}
	}

	status := ProbeStatusOK
	fixParts := make([]string, 0, 2)
	if stats.chunks > 0 && stats.ftsRows < stats.chunks {
		status = ProbeStatusFail
		detail += fmt.Sprintf(", missing %d FTS projection(s)", stats.chunks-stats.ftsRows)
		fixParts = append(fixParts, "re-sync affected knowledge sources or rebuild knowledge.db to restore missing FTS rows")
	}
	if stats.vectors > 0 && stats.vectors < stats.chunks && status != ProbeStatusFail {
		status = ProbeStatusWarn
		detail += fmt.Sprintf(", semantic vectors incomplete (%d/%d)", stats.vectors, stats.chunks)
		fixParts = append(fixParts, "re-run knowledge sync after embedding is configured to finish semantic vector projection")
	}
	if stats.vectors == 0 && stats.chunks > 0 {
		detail += ", semantic vectors not projected"
	}
	if stats.erroredSources > 0 && status != ProbeStatusFail {
		status = ProbeStatusWarn
	}
	if stats.erroredSources > 0 {
		detail += fmt.Sprintf(", %d source(s) reporting sync errors", stats.erroredSources)
		fixParts = append(fixParts, "inspect /operator/knowledge/sources and re-run the failing source syncs")
	}

	return ProbeResult{
		Status: status,
		Detail: detail,
		Fix:    strings.Join(fixParts, "; "),
	}
}

func ProbeDurableFacts(ctx context.Context, storage StorageSummary) ProbeResult {
	contextCounts, contextPresent, err := summarizeContextFacts(ctx, storage.KnowledgeDBPath)
	if err != nil {
		return ProbeResult{
			Status: ProbeStatusFail,
			Detail: fmt.Sprintf("read knowledge durable facts: %v", err),
		}
	}
	configCounts, configPresent, err := summarizeConfigFacts(ctx, storage.ControlDBPath)
	if err != nil {
		return ProbeResult{
			Status: ProbeStatusFail,
			Detail: fmt.Sprintf("read control durable facts: %v", err),
		}
	}
	if !contextPresent && !configPresent {
		if strings.EqualFold(strings.TrimSpace(storage.Backend), "sqlite") {
			return ProbeResult{
				Status: ProbeStatusWarn,
				Detail: fmt.Sprintf("no durable_facts table found in %s or %s",
					filepath.Base(storage.KnowledgeDBPath), filepath.Base(storage.ControlDBPath)),
				Fix: "start the gateway once to initialize durable state",
			}
		}
		backend := strings.TrimSpace(storage.Backend)
		if backend == "" {
			backend = "default"
		}
		return ProbeResult{
			Status: ProbeStatusOK,
			Detail: fmt.Sprintf("store backend=%s; durable facts summary skipped", backend),
		}
	}

	status := ProbeStatusOK
	fix := ""
	totalFacts := contextCounts.total + configCounts.total
	reviewBacklog := contextCounts.reviewRequired + configCounts.reviewRequired
	if reviewBacklog > 0 {
		status = ProbeStatusWarn
		fix = "review ambiguous durable facts and reclassify or rewrite them via the memory/config operator surfaces"
	}
	return ProbeResult{
		Status: status,
		Detail: fmt.Sprintf("%d durable fact(s) (context=%d, config=%d), %d review_required",
			totalFacts, contextCounts.total, configCounts.total, reviewBacklog),
		Fix: fix,
	}
}

func normalizeProbeResult(def ProbeDefinition, result ProbeResult) ProbeResult {
	if strings.TrimSpace(result.ID) == "" {
		result.ID = def.ID
	}
	if strings.TrimSpace(result.Category) == "" {
		result.Category = def.Category
	}
	if strings.TrimSpace(result.Name) == "" {
		result.Name = def.Name
	}
	if result.Status == "" {
		result.Status = ProbeStatusWarn
	}
	result.Detail = strings.TrimSpace(result.Detail)
	result.Fix = strings.TrimSpace(result.Fix)
	return result
}

func summarizeContextFacts(ctx context.Context, path string) (durableFactCounts, bool, error) {
	store, db, present, err := openDurableFactStore(ctx, path)
	if err != nil || !present {
		return durableFactCounts{}, present, err
	}
	defer db.Close()

	views, err := store.ListContextViews(ctx, durablefact.Filter{})
	if err != nil {
		return durableFactCounts{}, true, fmt.Errorf("list context durable facts: %w", err)
	}
	reviewRequired := true
	reviewViews, err := store.ListContextViews(ctx, durablefact.Filter{ReviewRequired: &reviewRequired})
	if err != nil {
		return durableFactCounts{}, true, fmt.Errorf("list context durable facts review backlog: %w", err)
	}
	return durableFactCounts{total: len(views), reviewRequired: len(reviewViews)}, true, nil
}

func summarizeConfigFacts(ctx context.Context, path string) (durableFactCounts, bool, error) {
	store, db, present, err := openDurableFactStore(ctx, path)
	if err != nil || !present {
		return durableFactCounts{}, present, err
	}
	defer db.Close()

	views, err := store.ListConfigViews(ctx, durablefact.Filter{})
	if err != nil {
		return durableFactCounts{}, true, fmt.Errorf("list config durable facts: %w", err)
	}
	reviewRequired := true
	reviewViews, err := store.ListConfigViews(ctx, durablefact.Filter{ReviewRequired: &reviewRequired})
	if err != nil {
		return durableFactCounts{}, true, fmt.Errorf("list config durable facts review backlog: %w", err)
	}
	return durableFactCounts{total: len(views), reviewRequired: len(reviewViews)}, true, nil
}

func openDurableFactStore(ctx context.Context, path string) (*durablefact.SQLiteStore, *sql.DB, bool, error) {
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

func summarizeKnowledgeIndexes(ctx context.Context, path string) (knowledgeIndexStats, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return knowledgeIndexStats{}, false, nil
		}
		return knowledgeIndexStats{}, false, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return knowledgeIndexStats{}, false, fmt.Errorf("%s is a directory, expected a sqlite database file", path)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return knowledgeIndexStats{}, false, fmt.Errorf("open %s: %w", path, err)
	}
	defer db.Close()

	for _, table := range []string{"knowledge_sources", "knowledge_documents", "knowledge_chunks", "knowledge_chunk_fts", "knowledge_chunk_vectors"} {
		present, err := sqliteTableExists(ctx, db, table)
		if err != nil {
			return knowledgeIndexStats{}, false, err
		}
		if !present {
			return knowledgeIndexStats{}, false, nil
		}
	}

	stats := knowledgeIndexStats{}
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
			return knowledgeIndexStats{}, true, fmt.Errorf("query knowledge index stats: %w", err)
		}
	}
	return stats, true, nil
}

func sqliteTableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		return false, fmt.Errorf("query sqlite schema for %s: %w", table, err)
	}
	return count > 0, nil
}

func formatBytes(size int64) string {
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
