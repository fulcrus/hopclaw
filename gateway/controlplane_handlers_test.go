package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/audit"
	"github.com/fulcrus/hopclaw/config"
	authzrbac "github.com/fulcrus/hopclaw/contrib/authz-rbac"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlaudit "github.com/fulcrus/hopclaw/internal/controlplane/auditsink"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	"github.com/fulcrus/hopclaw/policy"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
)

func TestPolicyEnginesEndpoint(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	gw.SetPolicyEngine(policy.NewChainEngine(
		policy.Layer{Name: "base", Engine: policy.NewDefaultEngine(policy.Config{RequireApprovalForWrite: true})},
		policy.Layer{Name: "overlay", Engine: policy.NewDefaultEngine(policy.Config{DenyDestructive: true})},
	))

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/policy/engines", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload policyEnginesResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Policy.Kind != "chain" || payload.Policy.LayerCount != 2 {
		t.Fatalf("policy = %+v", payload.Policy)
	}
}

func TestConfigCredentialsEndpoint(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	gw.SetCredentialInventory(config.Config{
		Server: config.ServerConfig{AuthToken: "keychain:server-auth"},
		ExecApproval: config.ExecApprovalConfig{
			Providers: []config.ApprovalProviderConfig{{
				Webhook: config.ApprovalWebhookProviderConfig{Secret: "env:APPROVAL_SECRET"},
			}},
		},
	}.SecretInventory())

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/config/credentials", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload config.SecretRefInventory
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 2 {
		t.Fatalf("count = %d, want 2", payload.Count)
	}
}

func TestGovernanceAdaptersAndAuditSinksEndpoints(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	adapter, err := controlgov.NewWebhookAdapter(controlgov.WebhookAdapterConfig{
		Name: "corp-gov",
		URL:  "https://governance.example.com/webhook",
	})
	if err != nil {
		t.Fatalf("NewWebhookAdapter() error = %v", err)
	}
	gw.SetGovernanceAdapterRegistry(controlgov.NewAdapterRegistry([]controlgov.AdapterDescriptor{{
		Name:            "corp-gov",
		Type:            "webhook",
		Enabled:         true,
		IncludeSnapshot: true,
	}}, adapter))
	gw.SetAuditSinkRegistry(controlaudit.NewRegistry([]controlaudit.SinkDescriptor{{
		Name:    "jsonl",
		Type:    "jsonl",
		Enabled: true,
		Target:  ".hopclaw/audit.jsonl",
	}}, "jsonl"))

	adapterRec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/governance/adapters", "")
	if adapterRec.Code != http.StatusOK {
		t.Fatalf("governance adapters: status = %d body=%s", adapterRec.Code, adapterRec.Body.String())
	}
	var adapterPayload governanceAdaptersResponse
	if err := json.NewDecoder(adapterRec.Body).Decode(&adapterPayload); err != nil {
		t.Fatalf("Decode() adapters error = %v", err)
	}
	if adapterPayload.Count != 1 || !adapterPayload.Items[0].Registered {
		t.Fatalf("adapter payload = %+v", adapterPayload)
	}

	sinkRec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/audit/sinks", "")
	if sinkRec.Code != http.StatusOK {
		t.Fatalf("audit sinks: status = %d body=%s", sinkRec.Code, sinkRec.Body.String())
	}
	var sinkPayload auditSinksResponse
	if err := json.NewDecoder(sinkRec.Body).Decode(&sinkPayload); err != nil {
		t.Fatalf("Decode() sinks error = %v", err)
	}
	if sinkPayload.Count != 1 || sinkPayload.Items[0].Target != ".hopclaw/audit.jsonl" {
		t.Fatalf("sink payload = %+v", sinkPayload)
	}
}

func TestAuditDeliveryEndpoints(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	deliveryStore := audit.NewInMemoryDeliveryStore()
	controller := audit.NewReliableDispatcher(audit.DeliveryConfig{}, deliveryStore, testAuditDeliverySink{name: "siem"})
	now := time.Now().UTC()
	entry, err := deliveryStore.Enqueue(context.Background(), audit.DeliveryEntry{
		SinkName:      "siem",
		EventID:       "evt-audit-1",
		EventType:     eventbus.EventRunCompleted,
		RunID:         "run-1",
		SessionID:     "sess-1",
		Event:         eventbus.Event{ID: "evt-audit-1", Type: eventbus.EventRunCompleted, RunID: "run-1", SessionID: "sess-1", Time: now},
		Status:        audit.DeliveryStatusDeadLetter,
		Attempts:      2,
		MaxAttempts:   2,
		LastError:     "temporary sink failure",
		NextAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	gw.SetAuditDeliveryController(controller)

	listRec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/audit/deliveries?status=dead_letter", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("audit deliveries: status = %d body=%s", listRec.Code, listRec.Body.String())
	}

	statsRec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/audit/deliveries/stats", "")
	if statsRec.Code != http.StatusOK {
		t.Fatalf("audit delivery stats: status = %d body=%s", statsRec.Code, statsRec.Body.String())
	}
	var stats audit.DeliveryStats
	if err := json.NewDecoder(statsRec.Body).Decode(&stats); err != nil {
		t.Fatalf("Decode() stats error = %v", err)
	}
	if stats.DeadLetter != 1 {
		t.Fatalf("DeadLetter = %d, want 1", stats.DeadLetter)
	}

	getRec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/audit/deliveries/"+entry.ID, "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("audit delivery get: status = %d body=%s", getRec.Code, getRec.Body.String())
	}

	redriveRec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/audit/deliveries/"+entry.ID+"/redrive", `{"options":{"reset_attempts":true,"clear_error":true}}`)
	if redriveRec.Code != http.StatusOK {
		t.Fatalf("audit delivery redrive: status = %d body=%s", redriveRec.Code, redriveRec.Body.String())
	}
	updated, err := controller.GetDelivery(context.Background(), entry.ID)
	if err != nil {
		t.Fatalf("GetDelivery() error = %v", err)
	}
	if updated.Status != audit.DeliveryStatusPending || updated.Attempts != 0 || updated.LastError != "" {
		t.Fatalf("redriven delivery = %+v", updated)
	}
}

func TestControlPlaneStatusEndpoint(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")

	gw := newTestGatewayFull(t)
	storageRoot := t.TempDir()
	gw.runtime = runtimepkg.NewService((&agent.AgentComponent{}).
		WithRunTriage(semanticDiagnosticsRunTriage{}).
		WithPreflightAnalyzer(semanticDiagnosticsPreflight{}).
		WithTaskContractAnalyzer(semanticDiagnosticsTaskContract{}), nil, nil, nil, nil, nil).
		WithIngressClassifier(semanticDiagnosticsIngressClassifier{}).
		WithClassifier(semanticDiagnosticsInteractionClassifier{}).
		WithAutomationClassifier(semanticDiagnosticsAutomationClassifier{})
	gw.SetAuthorizationDecider(authzrbac.NewDefaultDecider())
	gw.SetApprovals(approval.NewInMemoryStore())
	gw.SetPolicyEngine(policy.NewDefaultEngine(policy.Config{RequireApprovalForWrite: true}))
	cfg := config.Config{
		Locale: "zh-CN",
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    storageRoot,
		},
		Server: config.ServerConfig{AuthToken: "keychain:server-auth"},
		Channels: config.ChannelsConfig{
			Slack: config.SlackChannelConfig{
				BotToken: "env:SLACK_BOT_TOKEN",
			},
		},
		Skills: config.SkillsConfig{
			Config: map[string]map[string]any{
				"demo.skill": {
					"feature": map[string]any{
						"enabled": true,
					},
				},
			},
		},
	}
	resolver, err := controloverlay.NewResolver(context.Background(), cfg, nil, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	gw.effectiveCfg = resolver
	gw.SetCredentialInventory(cfg.SecretInventory())
	gw.runtime.WithEffectiveConfigSnapshot(&controlsnapshot.EffectiveConfigSnapshot{
		ID:             "ecs-test-1",
		RuntimeProfile: "production",
		Approval: controlsnapshot.ApprovalPolicy{
			RequireApprovalForWrite:  true,
			RequireApprovalCommunity: true,
			DenyDestructive:          true,
			DefaultGrantScope:        "once",
			MaxGrantScope:            "session",
		},
	})

	provider, err := controlapproval.NewWebhookProvider(controlapproval.WebhookProviderConfig{
		Name:      "corp-approval",
		SubmitURL: "https://approval.example.com/submit",
	})
	if err != nil {
		t.Fatalf("NewWebhookProvider() error = %v", err)
	}
	gw.SetApprovalProviderRegistry(controlapproval.NewProviderRegistry([]controlapproval.ProviderDescriptor{{
		Name:          "corp-approval",
		Type:          "webhook",
		Enabled:       true,
		SubmitEnabled: true,
	}}, provider))
	gw.SetAuditSinkRegistry(controlaudit.NewRegistry([]controlaudit.SinkDescriptor{{
		Name:    "jsonl",
		Type:    "jsonl",
		Enabled: true,
		Target:  ".hopclaw/audit.jsonl",
	}}, "jsonl"))
	deliveryStore := audit.NewInMemoryDeliveryStore()
	controller := audit.NewReliableDispatcher(audit.DeliveryConfig{}, deliveryStore, testAuditDeliverySink{name: "jsonl"})
	now := time.Now().UTC()
	if _, err := deliveryStore.Enqueue(context.Background(), audit.DeliveryEntry{
		SinkName:      "jsonl",
		EventID:       "evt-audit-2",
		EventType:     eventbus.EventRunCompleted,
		Event:         eventbus.Event{ID: "evt-audit-2", Type: eventbus.EventRunCompleted, Time: now},
		Status:        audit.DeliveryStatusPending,
		MaxAttempts:   3,
		NextAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	gw.SetAuditDeliveryController(controller)
	governanceStore := controlgov.NewInMemoryDeliveryStore()
	governanceController := controlgov.NewReliableDispatcher(controlgov.DeliveryConfig{
		MaxAttempts:  3,
		BaseBackoff:  10 * time.Millisecond,
		MaxBackoff:   10 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
		BatchSize:    8,
	}, governanceStore)
	gw.runtime.WithGovernanceDelivery(controlgov.AdaptDeliveryController(governanceController))
	mustSeedGatewayGovernanceDelivery(t, context.Background(), governanceStore, controlgov.DeliveryEntry{
		ID:             "gdel-status-1",
		AdapterName:    "corp-gov",
		IdempotencyKey: "delivery:status-1",
		Status:         controlgov.DeliveryStatusPending,
		Attempts:       1,
		MaxAttempts:    3,
		NextAttemptAt:  now.Add(time.Minute),
		CreatedAt:      now.Add(-time.Minute),
		UpdatedAt:      now,
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-gov-status-1",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-status-1",
			SessionID: "sess-status-1",
			Summary:   "governance delivery pending",
		},
	})

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/controlplane/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload controlPlaneStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !payload.OK {
		t.Fatalf("status payload not ready: %+v", payload)
	}
	if !payload.Auth.Ready || payload.Policy.Kind != "single" {
		t.Fatalf("status payload = %+v", payload)
	}
	if payload.UserSurface.Mode != controlplane.UserSurfaceModeManaged || payload.UserSurface.StartupDiagnostics != controlplane.UserSurfaceStartupActionableOnly {
		t.Fatalf("user surface summary = %+v", payload.UserSurface)
	}
	if payload.UserSurface.Approval.LocalWrite != "confirm" || payload.UserSurface.Approval.ExternalWrite != "confirm" || payload.UserSurface.Approval.Destructive != "deny" || payload.UserSurface.Approval.DefaultGrantScope != "once" {
		t.Fatalf("user surface approval = %+v", payload.UserSurface.Approval)
	}
	if payload.AuthZ.Kind != "rbac" {
		t.Fatalf("authz summary = %+v", payload.AuthZ)
	}
	if payload.Semantic.MainPath != "interaction_ingress" {
		t.Fatalf("semantic main path = %+v", payload.Semantic)
	}
	if !payload.Semantic.SharedSignalEnabled || !payload.Semantic.LanguageProfileEnabled {
		t.Fatalf("semantic summary = %+v, want shared signal and language profile enabled", payload.Semantic)
	}
	if !payload.Semantic.RunTriageConfigured || !payload.Semantic.PreflightAnalyzerConfigured || !payload.Semantic.TaskContractConfigured {
		t.Fatalf("semantic summary = %+v, want triage/preflight/task-contract configured", payload.Semantic)
	}
	if !payload.Semantic.InteractionIngressConfigured || !payload.Semantic.LegacyInteractionClassifierConfigured || !payload.Semantic.LegacyAutomationClassifierConfigured {
		t.Fatalf("semantic summary = %+v, want all ingress classifiers configured", payload.Semantic)
	}
	if len(payload.Semantic.MainPathLanguageFamilies) == 0 || payload.Semantic.MainPathLanguageFamilies[0] != "ar" {
		t.Fatalf("semantic language families = %#v, want stable semantic families", payload.Semantic.MainPathLanguageFamilies)
	}
	if payload.Storage.Backend != "sqlite" || !payload.Storage.SplitDatabases || !payload.Storage.AppendOnlyTranscript {
		t.Fatalf("storage summary = %+v", payload.Storage)
	}
	if payload.Storage.Root != storageRoot {
		t.Fatalf("storage root = %q, want %q", payload.Storage.Root, storageRoot)
	}
	if payload.Storage.RuntimeDBPath != filepath.Join(storageRoot, "runtime.db") {
		t.Fatalf("runtime db path = %q", payload.Storage.RuntimeDBPath)
	}
	if payload.Storage.ControlDBPath != filepath.Join(storageRoot, "control.db") {
		t.Fatalf("control db path = %q", payload.Storage.ControlDBPath)
	}
	if payload.Storage.KnowledgeDBPath != filepath.Join(storageRoot, "knowledge.db") {
		t.Fatalf("knowledge db path = %q", payload.Storage.KnowledgeDBPath)
	}
	if payload.Storage.AuditDBPath != filepath.Join(storageRoot, "audit.db") {
		t.Fatalf("audit db path = %q", payload.Storage.AuditDBPath)
	}
	if payload.Storage.TranscriptEventTable != "transcript_events" {
		t.Fatalf("transcript event table = %q, want transcript_events", payload.Storage.TranscriptEventTable)
	}
	if len(payload.Storage.JSONLResponsibilities) != 3 || payload.Storage.JSONLResponsibilities[0] != "audit_trail" {
		t.Fatalf("jsonl responsibilities = %#v", payload.Storage.JSONLResponsibilities)
	}
	if !payload.Results.UnifiedEventLedger || !payload.Results.DeliveryEnvelope || !payload.Results.DeliveryOutbox || !payload.Results.IdempotencyKeyRequired {
		t.Fatalf("results summary = %+v", payload.Results)
	}
	if payload.Results.DeliveryOutboxTable != "delivery_outbox" || payload.Results.ReceiptSource != "governance_deliveries" {
		t.Fatalf("result delivery wiring = %+v", payload.Results)
	}
	if len(payload.Results.EventClasses) != 3 || payload.Results.EventClasses[0] != "evidence" || payload.Results.EventClasses[2] != "delivery" {
		t.Fatalf("result event classes = %#v", payload.Results.EventClasses)
	}
	if len(payload.Results.RunResultSources) != 3 || payload.Results.RunResultSources[0] != "transcript" || payload.Results.RunResultSources[2] != "event_ledger" {
		t.Fatalf("result sources = %#v", payload.Results.RunResultSources)
	}
	if !payload.Knowledge.TypedSourceMetadata || !payload.Knowledge.TypedDocumentMetadata || !payload.Knowledge.TypedChunkMetadata {
		t.Fatalf("knowledge metadata summary = %+v", payload.Knowledge)
	}
	if !payload.Knowledge.IncrementalSync || payload.Knowledge.SyncCursorField != "sync_cursor" {
		t.Fatalf("knowledge sync summary = %+v", payload.Knowledge)
	}
	if !payload.Knowledge.PersistentFTSIndex || !payload.Knowledge.PersistentVectorIndex || !payload.Knowledge.ProjectionOnly || !payload.Knowledge.LocaleAwareRetrieval {
		t.Fatalf("knowledge index summary = %+v", payload.Knowledge)
	}
	if len(payload.Knowledge.TruthTables) != 3 || payload.Knowledge.TruthTables[0] != "knowledge_sources" {
		t.Fatalf("knowledge truth tables = %#v", payload.Knowledge.TruthTables)
	}
	if len(payload.Knowledge.ProjectionTables) != 2 || payload.Knowledge.ProjectionTables[0] != "knowledge_chunk_fts" {
		t.Fatalf("knowledge projection tables = %#v", payload.Knowledge.ProjectionTables)
	}
	if payload.Approvals.ProviderCount != 1 || payload.Audit.SinkCount != 1 {
		t.Fatalf("status payload counts = %+v", payload)
	}
	if !payload.Governance.DeliveryController || payload.Governance.DeliveryStats == nil || payload.Governance.DeliveryStats.Total != 1 {
		t.Fatalf("governance delivery status = %+v", payload.Governance)
	}
	if payload.Governance.DeliveryHealth == nil || payload.Governance.DeliveryHealth.PendingCount != 1 || payload.Governance.DeliveryHealth.Status != "ok" {
		t.Fatalf("governance delivery health = %+v", payload.Governance.DeliveryHealth)
	}
	if !payload.Audit.DeliveryController || payload.Audit.DeliveryStats == nil || payload.Audit.DeliveryStats.Pending != 1 {
		t.Fatalf("audit status payload = %+v", payload.Audit)
	}
	if payload.I18N.ConfiguredLocale != "zh-CN" || payload.I18N.EffectiveLocale != "zh-CN" {
		t.Fatalf("i18n summary = %+v", payload.I18N)
	}
	if payload.I18N.ConsoleCatalogPath != consoleBasePath+"/api/i18n" || payload.I18N.ConsoleConfigPath != consoleBasePath+"/api/config" {
		t.Fatalf("i18n endpoints = %+v", payload.I18N)
	}
	if len(payload.I18N.SupportedLocales) != 4 || payload.I18N.SupportedLocales[0] != "en" {
		t.Fatalf("supported locales = %#v", payload.I18N.SupportedLocales)
	}
	if payload.EffectiveConfig == nil || payload.EffectiveConfig.ID != "ecs-test-1" {
		t.Fatalf("effective config = %+v", payload.EffectiveConfig)
	}
	if payload.RuntimeFacts.ContextFingerprint == "" {
		t.Fatalf("runtime facts fingerprint = %+v", payload.RuntimeFacts)
	}
	if payload.RuntimeFacts.ManagedSkillCount == 0 || payload.RuntimeFacts.ManagedInjectedEnvCount == 0 || payload.RuntimeFacts.ConfigTruthCount == 0 {
		t.Fatalf("runtime facts counts = %+v", payload.RuntimeFacts)
	}
	if payload.RuntimeFacts.ResolvedSecretPresenceCount == 0 {
		t.Fatalf("runtime facts secret presence count = %+v", payload.RuntimeFacts)
	}
	if !payload.ChildEnvPolicy.OverlaySupported || payload.ChildEnvPolicy.MutatesHostProcess || payload.ChildEnvPolicy.InheritsFullParentEnv {
		t.Fatalf("child env policy flags = %+v", payload.ChildEnvPolicy)
	}
	if !containsControlPlaneString(payload.ChildEnvPolicy.ModuleExecBaselineKeys, "PATH") || !containsControlPlaneString(payload.ChildEnvPolicy.InstallerExecBaselineKeys, "PATH") {
		t.Fatalf("child env policy baseline keys = %+v", payload.ChildEnvPolicy)
	}
	if len(payload.ChildEnvPolicy.InstallerExecBaselineKeys) <= len(payload.ChildEnvPolicy.ModuleExecBaselineKeys) {
		t.Fatalf("installer baseline keys should be broader than module baseline: %+v", payload.ChildEnvPolicy)
	}
}

func TestControlPlaneStatusEndpointReportsDeterministicOnlySemanticPathWithoutIngressClassifier(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	gw.runtime = runtimepkg.NewService((&agent.AgentComponent{}).WithRunTriage(semanticDiagnosticsRunTriage{}), nil, nil, nil, nil, nil)
	gw.authChain = nil
	gw.SetApprovals(approval.NewInMemoryStore())
	gw.SetPolicyEngine(policy.NewDefaultEngine(policy.Config{}))
	gw.SetAuthorizationDecider(authzrbac.NewDefaultDecider())
	gw.runtime.WithEffectiveConfigSnapshot(&controlsnapshot.EffectiveConfigSnapshot{
		ID:             "ecs-test-2",
		RuntimeProfile: config.RuntimeProfileTrustedDesktop,
		Approval: controlsnapshot.ApprovalPolicy{
			RequireApprovalForWrite:        true,
			AllowLocalWriteWithoutApproval: true,
			DefaultGrantScope:              "once",
			MaxGrantScope:                  "session",
		},
	})

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/controlplane/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload controlPlaneStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Semantic.MainPath != "deterministic_only" {
		t.Fatalf("semantic main path = %+v, want deterministic_only", payload.Semantic)
	}
	if payload.Semantic.InteractionIngressConfigured || payload.Semantic.LegacyInteractionClassifierConfigured || payload.Semantic.LegacyAutomationClassifierConfigured {
		t.Fatalf("semantic classifier flags = %+v, want all false", payload.Semantic)
	}
	if !payload.Semantic.RunTriageConfigured || payload.Semantic.PreflightAnalyzerConfigured || payload.Semantic.TaskContractConfigured {
		t.Fatalf("semantic analyzer flags = %+v, want only run triage configured", payload.Semantic)
	}
	if payload.UserSurface.Mode != controlplane.UserSurfaceModePersonalLocal || payload.UserSurface.StartupDiagnostics != controlplane.UserSurfaceStartupQuietWhenHealthy {
		t.Fatalf("user surface summary = %+v", payload.UserSurface)
	}
	if payload.UserSurface.Approval.LocalWrite != "auto_allow" || payload.UserSurface.Approval.ExternalWrite != "confirm" || payload.UserSurface.Approval.DefaultGrantScope != "once" || payload.UserSurface.Approval.MaxGrantScope != "session" {
		t.Fatalf("user surface approval = %+v", payload.UserSurface.Approval)
	}
}

func TestControlPlaneStatusEndpointProjectsOperationalWarnings(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	gw.SetAuthorizationDecider(authzrbac.NewDefaultDecider())
	gw.SetApprovals(approval.NewInMemoryStore())
	gw.SetPolicyEngine(policy.NewDefaultEngine(policy.Config{}))
	gw.ApplyKernelServices(KernelServices{
		OperationalWarnings: operationalWarningSourceStub{warnings: []controlplane.OperationalWarning{{
			Component: "config_store",
			Summary:   "Dynamic config store unavailable; using YAML-only mode",
			Detail:    "open control.db: permission denied",
		}}},
	})

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/controlplane/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload controlPlaneStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(payload.OperationalWarnings) != 1 {
		t.Fatalf("operational warnings = %#v", payload.OperationalWarnings)
	}
	if payload.OperationalWarnings[0].Summary != "Dynamic config store unavailable; using YAML-only mode" {
		t.Fatalf("warning summary = %q", payload.OperationalWarnings[0].Summary)
	}
	if !containsControlPlaneString(payload.Issues, "operational warnings: 1 operational warning(s): Dynamic config store unavailable; using YAML-only mode") {
		t.Fatalf("issues = %#v", payload.Issues)
	}
	found := false
	for _, probe := range payload.Probes {
		if probe.ID != "runtime.operational_warnings" {
			continue
		}
		found = true
		if probe.Status != controlplane.ProbeStatusWarn {
			t.Fatalf("probe = %+v, want warn", probe)
		}
		break
	}
	if !found {
		t.Fatalf("probes = %#v, want runtime.operational_warnings", payload.Probes)
	}
}

type testAuditDeliverySink struct {
	name string
}

func (s testAuditDeliverySink) Name() string { return s.name }

func (s testAuditDeliverySink) Deliver(context.Context, eventbus.Event) error { return nil }

func containsControlPlaneString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
