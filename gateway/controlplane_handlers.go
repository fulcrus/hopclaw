package gateway

import (
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/audit"
	"github.com/fulcrus/hopclaw/authz"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/i18n"
	"github.com/fulcrus/hopclaw/policy"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type policyEnginesResponse struct {
	Policy policy.RuntimeSummary `json:"policy"`
}

type auditSinksResponse struct {
	Items []controlplane.AuditSinkSummary `json:"items"`
	Count int                             `json:"count"`
}

type governanceAdaptersResponse struct {
	Items []controlplane.GovernanceAdapterSummary `json:"items"`
	Count int                                     `json:"count"`
}

type controlPlaneAuthSummary struct {
	Configured bool   `json:"configured"`
	Ready      bool   `json:"ready"`
	InitError  string `json:"init_error,omitempty"`
}

type controlPlaneApprovalSummary struct {
	StoreAvailable bool                                   `json:"store_available"`
	Providers      []controlplane.ApprovalProviderSummary `json:"providers,omitempty"`
	ProviderCount  int                                    `json:"provider_count"`
}

type controlPlaneGovernanceSummary struct {
	Adapters           []controlplane.GovernanceAdapterSummary `json:"adapters,omitempty"`
	AdapterCount       int                                     `json:"adapter_count"`
	DeliveryController bool                                    `json:"delivery_controller"`
	DeliveryStats      *runtimesvc.GovernanceDeliveryStats     `json:"delivery_stats,omitempty"`
	DeliveryHealth     *runtimesvc.GovernanceDeliveryHealth    `json:"delivery_health,omitempty"`
}

type controlPlaneAuditSummary struct {
	Sinks              []controlplane.AuditSinkSummary `json:"sinks,omitempty"`
	SinkCount          int                             `json:"sink_count"`
	DeliveryController bool                            `json:"delivery_controller"`
	DeliveryStats      *audit.DeliveryStats            `json:"delivery_stats,omitempty"`
}

type controlPlaneI18NSummary struct {
	ConfiguredLocale   string   `json:"configured_locale,omitempty"`
	EffectiveLocale    string   `json:"effective_locale"`
	FallbackLocale     string   `json:"fallback_locale"`
	SupportedLocales   []string `json:"supported_locales"`
	ConsoleConfigPath  string   `json:"console_config_path"`
	ConsoleCatalogPath string   `json:"console_catalog_path"`
}

type controlPlaneSemanticSummary struct {
	SharedSignalEnabled                   bool     `json:"shared_signal_enabled"`
	LanguageProfileEnabled                bool     `json:"language_profile_enabled"`
	MainPath                              string   `json:"main_path"`
	MainPathLanguageFamilies              []string `json:"main_path_language_families,omitempty"`
	InteractionIngressConfigured          bool     `json:"interaction_ingress_configured"`
	LegacyInteractionClassifierConfigured bool     `json:"legacy_interaction_classifier_configured"`
	LegacyAutomationClassifierConfigured  bool     `json:"legacy_automation_classifier_configured"`
	RunTriageConfigured                   bool     `json:"run_triage_configured"`
	PreflightAnalyzerConfigured           bool     `json:"preflight_analyzer_configured"`
	TaskContractConfigured                bool     `json:"task_contract_analyzer_configured"`
}

type controlPlaneStatusResponse struct {
	OK                  bool                                  `json:"ok"`
	Issues              []string                              `json:"issues,omitempty"`
	Probes              []controlplane.ProbeResult            `json:"probes,omitempty"`
	OperationalWarnings []controlplane.OperationalWarning     `json:"operational_warnings,omitempty"`
	UserSurface         controlplane.UserSurfaceSummary       `json:"user_surface"`
	Auth                controlPlaneAuthSummary               `json:"auth"`
	AuthZ               authz.Summary                         `json:"authz"`
	Semantic            controlPlaneSemanticSummary           `json:"semantic"`
	Storage             controlplane.StorageSummary           `json:"storage"`
	Results             controlplane.ResultProjectionSummary  `json:"results"`
	Knowledge           controlplane.KnowledgeSummary         `json:"knowledge"`
	Policy              policy.RuntimeSummary                 `json:"policy"`
	Approvals           controlPlaneApprovalSummary           `json:"approvals"`
	Governance          controlPlaneGovernanceSummary         `json:"governance"`
	Audit               controlPlaneAuditSummary              `json:"audit"`
	I18N                controlPlaneI18NSummary               `json:"i18n"`
	Credentials         config.SecretRefInventory             `json:"credentials"`
	RuntimeFacts        controlplane.RuntimeFactsSummary      `json:"runtime_facts"`
	ChildEnvPolicy      controlplane.ChildEnvPolicySummary    `json:"child_env_policy"`
	EffectiveConfig     *controlplane.EffectiveConfigSnapshot `json:"effective_config,omitempty"`
}

func (g *Gateway) handlePolicyEngines(w http.ResponseWriter, _ *http.Request) {
	gwJSON(w, http.StatusOK, policyEnginesResponse{
		Policy: policy.DescribeEngine(g.policyEngine),
	})
}

func (g *Gateway) handleAuditSinksList(w http.ResponseWriter, _ *http.Request) {
	items := []controlplane.AuditSinkSummary{}
	if g.auditSinks != nil {
		if described := g.auditSinks.Describe(); described != nil {
			items = described
		}
	}
	gwJSON(w, http.StatusOK, auditSinksResponse{Items: items, Count: len(items)})
}

func (g *Gateway) handleGovernanceAdaptersList(w http.ResponseWriter, _ *http.Request) {
	items := []controlplane.GovernanceAdapterSummary{}
	if g.governanceAdapters != nil {
		if described := g.governanceAdapters.Describe(); described != nil {
			items = described
		}
	}
	gwJSON(w, http.StatusOK, governanceAdaptersResponse{Items: items, Count: len(items)})
}

func (g *Gateway) handleConfigCredentials(w http.ResponseWriter, _ *http.Request) {
	gwJSON(w, http.StatusOK, g.credentials)
}

func (g *Gateway) handleControlPlaneStatus(w http.ResponseWriter, r *http.Request) {
	var issues []string

	authReady := g.authConfigured() && g.authInitErr == nil
	if !g.authConfigured() {
		issues = append(issues, "auth is not configured")
	}
	if g.authInitErr != nil {
		issues = append(issues, "auth initialization failed: "+strings.TrimSpace(g.authInitErr.Error()))
	}
	if g.runtime == nil {
		issues = append(issues, "runtime is not available")
	}
	if g.authzDecider == nil {
		issues = append(issues, "authorization decider is not configured")
	}
	if g.policyEngine == nil {
		issues = append(issues, "policy engine is not configured")
	}
	if g.approvals == nil {
		issues = append(issues, "approval store is not available")
	}

	providers := g.describeApprovalProviders()
	adapters := g.describeGovernanceAdapters()
	sinks := g.describeAuditSinks()
	var auditStats *audit.DeliveryStats
	if g.auditDelivery != nil {
		if stats, err := g.auditDelivery.GetDeliveryStats(r.Context(), audit.DeliveryListFilter{}); err == nil {
			statsCopy := stats
			auditStats = &statsCopy
		}
	}
	var governanceStats *runtimesvc.GovernanceDeliveryStats
	var governanceHealth *runtimesvc.GovernanceDeliveryHealth
	if g.runtime != nil {
		if stats, err := g.runtime.GetGovernanceDeliveryStats(r.Context(), runtimesvc.GovernanceDeliveryFilter{}); err == nil && stats != nil {
			statsCopy := *stats
			governanceStats = &statsCopy
		}
		if health, err := g.runtime.GetGovernanceDeliveryHealth(r.Context(), runtimesvc.GovernanceDeliveryFilter{}); err == nil && health != nil {
			healthCopy := *health
			governanceHealth = &healthCopy
		}
	}

	var snapshot *controlplane.EffectiveConfigSnapshot
	if g.runtime != nil {
		snapshot = g.runtime.EffectiveConfigSnapshot()
	}
	if snapshot == nil {
		issues = append(issues, "effective config snapshot is not available")
	}
	userSurface := g.userSurfaceSummary()
	runtimeFacts := buildControlPlaneRuntimeFacts(g.skillRuntimeContext())
	childEnvPolicy := buildControlPlaneChildEnvPolicy()
	i18nSummary := buildControlPlaneI18NSummary(g)
	semanticSummary := buildControlPlaneSemanticSummary(g)
	storageSummary := buildControlPlaneStorageSummary(g)
	resultSummary := buildControlPlaneResultProjectionSummary()
	knowledgeSummary := buildControlPlaneKnowledgeSummary()
	operationalWarnings := buildControlPlaneOperationalWarnings(g)
	probes := controlplane.NewStatusProbeRegistry(controlplane.StatusProbeInput{
		Storage:             storageSummary,
		Results:             resultSummary,
		Knowledge:           knowledgeSummary,
		Credentials:         g.credentials,
		RuntimeFacts:        runtimeFacts,
		ChildEnvPolicy:      childEnvPolicy,
		OperationalWarnings: operationalWarnings,
	}).RunAll(r.Context())
	issues = append(issues, controlplane.ProbeIssues(probes)...)

	gwJSON(w, http.StatusOK, controlPlaneStatusResponse{
		OK:                  len(issues) == 0,
		Issues:              emptyIfZeroStrings(issues),
		Probes:              probes,
		OperationalWarnings: operationalWarnings,
		UserSurface:         userSurface,
		Auth: controlPlaneAuthSummary{
			Configured: g.authConfigured(),
			Ready:      authReady,
			InitError:  errorText(g.authInitErr),
		},
		AuthZ:           authz.Describe(g.authzDecider),
		Semantic:        semanticSummary,
		Storage:         storageSummary,
		Results:         resultSummary,
		Knowledge:       knowledgeSummary,
		Policy:          policy.DescribeEngine(g.policyEngine),
		Approvals:       controlPlaneApprovalSummary{StoreAvailable: g.approvals != nil, Providers: providers, ProviderCount: len(providers)},
		Governance:      controlPlaneGovernanceSummary{Adapters: adapters, AdapterCount: len(adapters), DeliveryController: g.runtime != nil && g.runtime.GetGovernanceDeliveryController() != nil, DeliveryStats: governanceStats, DeliveryHealth: governanceHealth},
		Audit:           controlPlaneAuditSummary{Sinks: sinks, SinkCount: len(sinks), DeliveryController: g.auditDelivery != nil, DeliveryStats: auditStats},
		I18N:            i18nSummary,
		Credentials:     g.credentials,
		RuntimeFacts:    runtimeFacts,
		ChildEnvPolicy:  childEnvPolicy,
		EffectiveConfig: snapshot,
	})
}

func buildControlPlaneI18NSummary(g *Gateway) controlPlaneI18NSummary {
	configuredLocale := ""
	if g != nil && g.effectiveCfg != nil {
		configuredLocale = strings.TrimSpace(g.effectiveCfg.Current().Locale)
	}
	return controlPlaneI18NSummary{
		ConfiguredLocale:   configuredLocale,
		EffectiveLocale:    string(g.resolveConsoleLocale("")),
		FallbackLocale:     string(i18n.Global().Fallback()),
		SupportedLocales:   i18n.SupportedLocaleStrings(),
		ConsoleConfigPath:  consoleBasePath + "/api/config",
		ConsoleCatalogPath: consoleBasePath + "/api/i18n",
	}
}

func buildControlPlaneSemanticSummary(g *Gateway) controlPlaneSemanticSummary {
	if g == nil || g.runtime == nil {
		return controlPlaneSemanticSummary{}
	}
	summary := g.runtime.SemanticIngressSummary()
	return controlPlaneSemanticSummary{
		SharedSignalEnabled:                   summary.SharedSignalEnabled,
		LanguageProfileEnabled:                summary.LanguageProfileEnabled,
		MainPath:                              summary.MainPath,
		MainPathLanguageFamilies:              append([]string(nil), summary.MainPathLanguageFamilies...),
		InteractionIngressConfigured:          summary.InteractionIngressConfigured,
		LegacyInteractionClassifierConfigured: summary.LegacyInteractionClassifierConfigured,
		LegacyAutomationClassifierConfigured:  summary.LegacyAutomationClassifierConfigured,
		RunTriageConfigured:                   summary.RunTriageConfigured,
		PreflightAnalyzerConfigured:           summary.PreflightAnalyzerConfigured,
		TaskContractConfigured:                summary.TaskContractConfigured,
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func emptyIfZeroStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	return items
}
