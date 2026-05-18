package gateway

import (
	"net/http"

	"github.com/fulcrus/hopclaw/server"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type operatorRouteSurface interface {
	RegisterRoutes(*http.ServeMux, func(*http.ServeMux, string, func(http.ResponseWriter, *http.Request)))
}

func (g *Gateway) wrapHTTPAppMiddleware(handler http.Handler) http.Handler {
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	handler = RequestID(handler)
	handler = CORS(g.config.CORS)(handler)
	handler = RateLimit(g.config.RateLimit)(handler)
	handler = SecurityHeaders(handler)
	handler = MetricsMiddleware(handler)
	return handler
}

func (g *Gateway) mountAuthedFunc(mux *http.ServeMux, pattern string, handler func(http.ResponseWriter, *http.Request)) {
	if mux == nil || handler == nil {
		return
	}
	mux.Handle(pattern, g.withAuth(http.HandlerFunc(handler)))
}

func (g *Gateway) registerAuthSessionRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("GET /auth/login", g.handleAuthLogin)
	mux.HandleFunc("GET /auth/callback", g.handleAuthCallback)
	mux.Handle("POST /auth/logout", g.authenticatedHandler(http.HandlerFunc(g.handleAuthLogout), false))
}

func (g *Gateway) registerConsoleUIRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("GET /{$}", g.handleConsoleRedirect)

	mux.HandleFunc("GET /dashboard", g.handleDashboardIndexRedirect)
	mux.HandleFunc("GET /dashboard/api/config", g.handleWebChatConfig)
	mux.HandleFunc("GET /dashboard/api/i18n", g.handleWebChatCatalog)
	g.mountAuthedFunc(mux, "GET /dashboard/sse", g.handleWebChatSSE)
	g.mountAuthedFunc(mux, "POST /dashboard/upload", g.handleWebChatUpload)
	mux.Handle("GET /dashboard/", http.StripPrefix("/dashboard/", g.consoleUIHandler()))

	mux.HandleFunc("GET /webchat", g.handleLegacyConsoleRedirect)
	mux.HandleFunc("GET /webchat/api/config", g.handleWebChatConfig)
	mux.HandleFunc("GET /webchat/api/i18n", g.handleWebChatCatalog)
	g.mountAuthedFunc(mux, "GET /webchat/sse", g.handleWebChatSSE)
	g.mountAuthedFunc(mux, "POST /webchat/upload", g.handleWebChatUpload)
	mux.Handle("GET /webchat/", http.HandlerFunc(g.handleLegacyConsoleRedirect))
}

func (g *Gateway) registerOperatorAPIRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	g.registerOperatorCoreRoutes(mux)
	g.registerOperatorAutomationRoutes(mux)
	g.registerOperatorControlPlaneRoutes(mux)
	g.registerOperatorAssetRoutes(mux)
	g.registerOperatorChannelRoutes(mux)
	g.registerOperatorKnowledgeRoutes(mux)
	g.registerOperatorSurfaces(
		mux,
		newOperatorHookSurface(hookOperatorDepsFromGateway(g)),
		newOperatorPluginSurface(pluginOperatorDepsFromGateway(g)),
		newOperatorUsageSurface(g.usageStore),
		newOperatorQualitySurface(g.runtime),
		newOperatorDiscoverySurface(g.discovery),
	)
}

func (g *Gateway) registerOperatorCoreRoutes(mux *http.ServeMux) {
	g.mountAuthedFunc(mux, "GET /operator/authz", g.handleAuthorizationSummary)
	g.mountAuthedFunc(mux, "GET /operator/status", g.handleStatus)
	g.mountAuthedFunc(mux, "GET /operator/extensions", g.handleExtensions)
	g.mountAuthedFunc(mux, "GET /operator/capabilities", g.handleCapabilities)
	g.mountAuthedFunc(mux, "GET /operator/capabilities/{name}/sessions", g.handleCapabilitySessions)
	g.mountAuthedFunc(mux, "DELETE /operator/capabilities/{name}/sessions/{id}", g.handleCloseCapabilitySession)
	g.mountAuthedFunc(mux, "GET /operator/browser/sessions", g.handleBrowserSessions)
	g.mountAuthedFunc(mux, "DELETE /operator/browser/sessions/{id}", g.handleCloseBrowserSession)
	g.mountAuthedFunc(mux, "GET /operator/browser/profiles", g.handleBrowserProfilesList)
	g.mountAuthedFunc(mux, "POST /operator/browser/profiles", g.handleBrowserProfilesCreate)
	g.mountAuthedFunc(mux, "DELETE /operator/browser/profiles/{name}", g.handleBrowserProfilesDelete)
	g.mountAuthedFunc(mux, "GET /operator/helpers/status", g.handleHelpersStatus)
	g.mountAuthedFunc(mux, "POST /operator/helpers/reclaim", g.handleHelpersReclaim)
	g.mountAuthedFunc(mux, "GET /operator/agents", g.handleAgentsList)
	g.mountAuthedFunc(mux, "GET /operator/agents/{name}", g.handleAgentGet)
	g.mountAuthedFunc(mux, "GET /operator/nodes", g.handleNodesList)
	g.mountAuthedFunc(mux, "GET /operator/nodes/{id}", g.handleNodeGet)
	g.mountAuthedFunc(mux, "GET /operator/instances", g.handleInstancesList)
	g.mountAuthedFunc(mux, "GET /operator/devices", g.handleDevicesList)
	g.mountAuthedFunc(mux, "POST /operator/devices/pair", g.handleDevicesPairCreate)
	g.mountAuthedFunc(mux, "POST /operator/devices/pair/approve", g.handleDevicesPairApprove)
	mux.HandleFunc("POST /device/pair/claim", g.handleDevicePairClaim)
	g.mountAuthedFunc(mux, "POST /operator/devices/pair/reject", g.handleDevicesPairReject)
	g.mountAuthedFunc(mux, "POST /operator/devices/{id}/trust", g.handleDevicesTrust)
	g.mountAuthedFunc(mux, "POST /operator/devices/{id}/revoke", g.handleDevicesRevoke)
	g.mountAuthedFunc(mux, "POST /operator/devices/{id}/tokens/rotate", g.handleDevicesTokenRotate)
	g.mountAuthedFunc(mux, "POST /operator/devices/{id}/tokens/revoke", g.handleDevicesTokenRevoke)
	g.mountAuthedFunc(mux, "GET /operator/heartbeat", g.handleHeartbeat)
	g.mountAuthedFunc(mux, "GET /operator/wire/entries", g.handleWireEntries)
	g.mountAuthedFunc(mux, "GET /operator/wire/stats", g.handleWireStats)
	g.mountAuthedFunc(mux, "DELETE /operator/wire/entries", g.handleWireClear)
	g.mountAuthedFunc(mux, "POST /operator/sandbox/exec", g.handleSandboxExec)
	g.mountAuthedFunc(mux, "GET /operator/sandbox/status", g.handleSandboxStatus)
	g.mountAuthedFunc(mux, "GET /operator/metrics", promhttp.Handler().ServeHTTP)
}

func (g *Gateway) registerOperatorAutomationRoutes(mux *http.ServeMux) {
	g.mountAuthedFunc(mux, "GET /operator/automation/templates", g.handleAutomationTemplates)
	g.mountAuthedFunc(mux, "GET /operator/automation/items", g.handleAutomationItems)
	g.mountAuthedFunc(mux, "GET /operator/automation/items/{kind}/{id}", g.handleAutomationItemDetail)
	newOperatorCronSurface(g.cron).RegisterRoutes(mux, g.mountAuthedFunc)
	g.mountAuthedFunc(mux, "GET /operator/watch/items", g.handleWatchList)
	g.mountAuthedFunc(mux, "POST /operator/watch/items", g.handleWatchCreate)
	g.mountAuthedFunc(mux, "GET /operator/watch/items/{id}", g.handleWatchGet)
	g.mountAuthedFunc(mux, "PATCH /operator/watch/items/{id}", g.handleWatchUpdate)
	g.mountAuthedFunc(mux, "DELETE /operator/watch/items/{id}", g.handleWatchDelete)
	g.mountAuthedFunc(mux, "POST /operator/watch/items/{id}/run", g.handleWatchTrigger)
	g.mountAuthedFunc(mux, "GET /operator/watch/status", g.handleWatchStatus)
	g.mountAuthedFunc(mux, "GET /operator/wakeup/triggers", g.handleWakeupList)
	g.mountAuthedFunc(mux, "POST /operator/wakeup/triggers", g.handleWakeupCreate)
	g.mountAuthedFunc(mux, "GET /operator/wakeup/triggers/{id}", g.handleWakeupGet)
	g.mountAuthedFunc(mux, "PATCH /operator/wakeup/triggers/{id}", g.handleWakeupUpdate)
	g.mountAuthedFunc(mux, "DELETE /operator/wakeup/triggers/{id}", g.handleWakeupDelete)
}

func (g *Gateway) registerOperatorControlPlaneRoutes(mux *http.ServeMux) {
	g.mountAuthedFunc(mux, "GET /operator/approvals", g.handleApprovalsList)
	g.mountAuthedFunc(mux, "GET /operator/approvals/providers", g.handleApprovalProvidersList)
	g.mountAuthedFunc(mux, "POST /operator/approvals/{id}/resolve", g.handleApprovalsResolve)
	g.mountAuthedFunc(mux, "POST /operator/approvals/{id}/cancel", g.handleApprovalsCancel)
	g.mountAuthedFunc(mux, "GET /operator/policy/engines", g.handlePolicyEngines)
	g.mountAuthedFunc(mux, "GET /operator/controlplane/status", g.handleControlPlaneStatus)
	g.mountAuthedFunc(mux, "GET /operator/governance/adapters", g.handleGovernanceAdaptersList)
	g.mountAuthedFunc(mux, "GET /operator/governance/deliveries", g.handleGovernanceDeliveriesList)
	g.mountAuthedFunc(mux, "GET /operator/governance/deliveries/stats", g.handleGovernanceDeliveriesStats)
	g.mountAuthedFunc(mux, "GET /operator/governance/health", g.handleGovernanceHealth)
	g.mountAuthedFunc(mux, "POST /operator/governance/deliveries/redrive", g.handleGovernanceDeliveriesRedrive)
	g.mountAuthedFunc(mux, "GET /operator/governance/deliveries/{id}", g.handleGovernanceDeliveryGet)
	g.mountAuthedFunc(mux, "POST /operator/governance/deliveries/{id}/redrive", g.handleGovernanceDeliveryRedrive)
	g.mountAuthedFunc(mux, "GET /operator/governance/events", g.handleGovernanceEvents)
	g.mountAuthedFunc(mux, "GET /operator/audit/sinks", g.handleAuditSinksList)
	g.mountAuthedFunc(mux, "GET /operator/audit/deliveries", g.handleAuditDeliveriesList)
	g.mountAuthedFunc(mux, "GET /operator/audit/deliveries/stats", g.handleAuditDeliveriesStats)
	g.mountAuthedFunc(mux, "POST /operator/audit/deliveries/redrive", g.handleAuditDeliveriesRedrive)
	g.mountAuthedFunc(mux, "GET /operator/audit/deliveries/{id}", g.handleAuditDeliveryGet)
	g.mountAuthedFunc(mux, "POST /operator/audit/deliveries/{id}/redrive", g.handleAuditDeliveryRedrive)
	g.mountAuthedFunc(mux, "GET /operator/audit/events", g.handleAuditEvents)
	g.mountAuthedFunc(mux, "GET /operator/setup/catalog", g.handleSetupCatalog)
	g.mountAuthedFunc(mux, "GET /operator/setup/status", g.handleSetupStatus)
	g.mountAuthedFunc(mux, "GET /operator/durable-facts", g.handleDurableFactsList)
	g.mountAuthedFunc(mux, "POST /operator/models/validate", g.handleModelsValidate)
	g.mountAuthedFunc(mux, "POST /operator/models/test-chat", g.handleModelsTestChat)
	g.mountAuthedFunc(mux, "GET /operator/config", g.handleConfigGet)
	g.mountAuthedFunc(mux, "GET /operator/config/credentials", g.handleConfigCredentials)
	g.mountAuthedFunc(mux, "GET /operator/config/{section}", g.handleConfigGetSection)
	g.mountAuthedFunc(mux, "PUT /operator/config/{section}", g.handleConfigPutSection)
	g.mountAuthedFunc(mux, "POST /operator/config/validate", g.handleConfigValidate)
	g.mountAuthedFunc(mux, "POST /operator/config/preview", g.handleConfigPreview)
	g.mountAuthedFunc(mux, "GET /operator/models", g.handleModelsList)
	g.mountAuthedFunc(mux, "GET /operator/models/router", g.handleModelsRouter)
	g.mountAuthedFunc(mux, "POST /operator/models", g.handleModelsCreate)
	g.mountAuthedFunc(mux, "PUT /operator/models/{name}", g.handleModelsUpdate)
	g.mountAuthedFunc(mux, "DELETE /operator/models/{name}", g.handleModelsDelete)
}

func (g *Gateway) registerOperatorAssetRoutes(mux *http.ServeMux) {
	g.mountAuthedFunc(mux, "GET /operator/artifacts", g.handleOperatorArtifacts)
	g.mountAuthedFunc(mux, "GET /operator/artifacts/{id}/preview", g.handleArtifactPreview)
	g.mountAuthedFunc(mux, "GET /operator/pairing", g.handlePairingList)
	g.mountAuthedFunc(mux, "POST /operator/pairing/initiate", g.handlePairingInitiate)
	g.mountAuthedFunc(mux, "POST /operator/pairing/verify", g.handlePairingVerify)
	g.mountAuthedFunc(mux, "DELETE /operator/pairing/{channel}/{user_id}", g.handlePairingRevoke)
	g.mountAuthedFunc(mux, "GET /operator/allowlist", g.handleAllowlistList)
	g.mountAuthedFunc(mux, "GET /operator/allowlist/{channel}", g.handleAllowlistGet)
	g.mountAuthedFunc(mux, "PUT /operator/allowlist/{channel}", g.handleAllowlistSet)
	g.mountAuthedFunc(mux, "DELETE /operator/allowlist/{channel}", g.handleAllowlistDelete)
}

func (g *Gateway) registerOperatorChannelRoutes(mux *http.ServeMux) {
	g.mountAuthedFunc(mux, "GET /operator/channels/health", g.handleChannelHealth)
	g.mountAuthedFunc(mux, "GET /operator/channels/matrix", g.handleChannelMatrix)
	g.mountAuthedFunc(mux, "GET /operator/channels/thread-bindings", g.handleChannelThreadBindings)
	g.mountAuthedFunc(mux, "DELETE /operator/channels/thread-bindings/{channel}/{thread_id}", g.handleChannelThreadBindingDelete)
	g.mountAuthedFunc(mux, "GET /operator/channels", g.handleChannelsCRUDList)
	g.mountAuthedFunc(mux, "POST /operator/channels", g.handleChannelsCRUDCreate)
	g.mountAuthedFunc(mux, "PUT /operator/channels/{name}", g.handleChannelsCRUDUpdate)
	g.mountAuthedFunc(mux, "DELETE /operator/channels/{name}", g.handleChannelsCRUDDelete)
	g.mountAuthedFunc(mux, "POST /operator/channels/validate", g.handleChannelsValidate)
	g.mountAuthedFunc(mux, "POST /operator/channels/detect", g.handleChannelsDetect)
	g.mountAuthedFunc(mux, "POST /operator/channels/test-message", g.handleChannelsTestMessage)
	g.mountAuthedFunc(mux, "POST /operator/tools/test", g.handleToolsTest)
}

func (g *Gateway) registerOperatorKnowledgeRoutes(mux *http.ServeMux) {
	g.mountAuthedFunc(mux, "GET /operator/skills", g.handleSkillsList)
	g.mountAuthedFunc(mux, "GET /operator/skills/catalog", g.handleSkillsCatalog)
	g.mountAuthedFunc(mux, "GET /operator/skills/catalog/{id}", g.handleSkillsCatalogGet)
	g.mountAuthedFunc(mux, "GET /operator/skills/{name}", g.handleSkillsGet)
	g.mountAuthedFunc(mux, "POST /operator/skills/install", g.handleSkillsInstall)
	g.mountAuthedFunc(mux, "DELETE /operator/skills/{name}", g.handleSkillsDelete)
	g.mountAuthedFunc(mux, "PUT /operator/skills/{name}/config", g.handleSkillsUpdateConfig)
	g.mountAuthedFunc(mux, "POST /operator/skills/preflight", g.handleSkillsPreflight)
	g.mountAuthedFunc(mux, "GET /operator/knowledge/sources", g.handleKnowledgeSourcesList)
	g.mountAuthedFunc(mux, "POST /operator/knowledge/sources", g.handleKnowledgeSourcesCreate)
	g.mountAuthedFunc(mux, "GET /operator/knowledge/sources/{id}", g.handleKnowledgeSourcesGet)
	g.mountAuthedFunc(mux, "PATCH /operator/knowledge/sources/{id}", g.handleKnowledgeSourcesUpdate)
	g.mountAuthedFunc(mux, "DELETE /operator/knowledge/sources/{id}", g.handleKnowledgeSourcesDelete)
	g.mountAuthedFunc(mux, "POST /operator/knowledge/sources/{id}/sync", g.handleKnowledgeSourcesSync)
	g.mountAuthedFunc(mux, "GET /operator/knowledge/search", g.handleKnowledgeSearch)
}

func (g *Gateway) registerOperatorSurfaces(mux *http.ServeMux, surfaces ...operatorRouteSurface) {
	for _, surface := range surfaces {
		if surface == nil {
			continue
		}
		surface.RegisterRoutes(mux, g.mountAuthedFunc)
	}
}

func (g *Gateway) registerTransportRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	g.mountAuthedFunc(mux, "POST /v1/chat/completions", g.handleChatCompletions)
	mux.Handle("GET "+operatorWebSocketPath, g.withWSAuth(http.HandlerFunc(g.handleWebSocket)))
}

func (g *Gateway) registerChannelRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	g.mountAuthedFunc(mux, "GET /channels", g.handleListChannels)
	mux.HandleFunc("POST /channels/webhook/{id}/inbound", g.handleWebhookInbound)
	mux.HandleFunc("/channels/", g.handleChannelInbound)
}

func (g *Gateway) registerRuntimeAPIRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.Handle("GET "+server.RuntimeWebSocketPath, g.publicSurfaceHandler())
	runtimeHandler := g.runtimeSurfaceHandler()
	mux.Handle("POST /runtime/approvals/callbacks/resolve", runtimeHandler)
	mux.Handle("/runtime/", g.withAuth(runtimeHandler))
}

func (g *Gateway) registerPublicServerRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.Handle("/", g.publicSurfaceHandler())
}
