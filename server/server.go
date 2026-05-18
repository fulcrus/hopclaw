package server

import (
	"net/http"

	"github.com/fulcrus/hopclaw/controlplane"
	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type Server struct {
	runtime                     *runtimesvc.Service
	config                      Config
	wsHub                       *WSHub
	approvalCallbackRateLimiter *approvalCallbackRateLimiter
}

// ---------------------------------------------------------------------------
// API response types
// ---------------------------------------------------------------------------

type healthResponse struct {
	OK       bool     `json:"ok"`
	State    string   `json:"state,omitempty"`
	Summary  string   `json:"summary,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type okResponse = apiresponse.OK

type listResponse = apiresponse.CountedList

type countedListResponse = listResponse

type cursorListResponse = apiresponse.CursorList

type Config struct {
	AuthToken                 string
	MaxEventResults           int
	WSHub                     *WSHub
	ApprovalCallbacks         map[string]controlplane.ApprovalCallbackAuthPolicy
	ApprovalCallbackRateLimit ApprovalCallbackRateLimitConfig
	OperationalWarnings       controlplane.OperationalWarningSource
}

func New(runtime *runtimesvc.Service, cfg Config) *Server {
	if cfg.MaxEventResults <= 0 {
		cfg.MaxEventResults = 200
	}
	return &Server{
		runtime:                     runtime,
		config:                      cfg,
		wsHub:                       cfg.WSHub,
		approvalCallbackRateLimiter: newApprovalCallbackRateLimiter(cfg.ApprovalCallbackRateLimit),
	}
}

func (s *Server) SetApprovalCallbacks(callbacks map[string]controlplane.ApprovalCallbackAuthPolicy) {
	if s == nil {
		return
	}
	if callbacks == nil {
		s.config.ApprovalCallbacks = nil
		return
	}
	cloned := make(map[string]controlplane.ApprovalCallbackAuthPolicy, len(callbacks))
	for key, value := range callbacks {
		cloned[key] = value
	}
	s.config.ApprovalCallbacks = cloned
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if s.wsHub != nil {
		mux.HandleFunc("GET "+RuntimeWebSocketPath, s.HandleWebSocket)
	}
	mux.Handle("/runtime/", s.withAuth(s.RuntimeHandler()))
	mux.Handle("/", s.PublicHandler())
	return mux
}

// PublicHandler exposes only the unauthenticated/public server routes.
// Callers such as the gateway compose this surface directly without also
// importing the runtime API auth wrapper.
func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	s.registerPublicRoutes(mux)
	return mux
}

// RuntimeHandler exposes the runtime API without the server-level auth wrapper.
// Callers such as the gateway apply their own auth boundary around this handler.
func (s *Server) RuntimeHandler() http.Handler {
	mux := http.NewServeMux()
	s.registerRuntimeRoutes(mux)
	return mux
}

func (s *Server) registerPublicRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("GET /", s.handleLandingPage)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	if s.wsHub != nil {
		mux.HandleFunc("GET "+RuntimeWebSocketPath, s.HandleWebSocket)
	}
}

func (s *Server) registerRuntimeRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("POST /runtime/interact", s.handleInteract)
	mux.HandleFunc("GET /runtime/config/effective", s.handleGetEffectiveConfig)
	mux.HandleFunc("GET /runtime/governance/deliveries", s.handleListGovernanceDeliveries)
	mux.HandleFunc("GET /runtime/governance/deliveries/stats", s.handleGetGovernanceDeliveryStats)
	mux.HandleFunc("GET /runtime/governance/health", s.handleGetGovernanceDeliveryHealth)
	mux.HandleFunc("POST /runtime/governance/deliveries/redrive", s.handleRedriveGovernanceDeliveries)
	mux.HandleFunc("GET /runtime/governance/deliveries/{id}", s.handleGetGovernanceDelivery)
	mux.HandleFunc("POST /runtime/governance/deliveries/{id}/redrive", s.handleRedriveGovernanceDelivery)
	mux.HandleFunc("GET /runtime/governance/events", s.handleListGovernanceEvents)
	mux.HandleFunc("POST /runtime/runs", s.handleSubmitRun)
	mux.HandleFunc("GET /runtime/tools", s.handleListTools)
	mux.HandleFunc("GET /runtime/runs/{id}", s.handleGetRun)
	mux.HandleFunc("GET /runtime/runs/{id}/governance", s.handleGetRunGovernance)
	mux.HandleFunc("GET /runtime/runs/{id}/completion", s.handleGetRunCompletion)
	mux.HandleFunc("GET /runtime/runs/{id}/result", s.handleGetRunResult)
	mux.HandleFunc("GET /runtime/runs/{id}/verification", s.handleGetRunVerification)
	mux.HandleFunc("GET /runtime/quality/summary", s.handleQualitySummary)
	mux.HandleFunc("GET /runtime/release-readiness", s.handleReleaseReadiness)
	mux.HandleFunc("GET /runtime/evals/suites", s.handleEvalSuites)
	mux.HandleFunc("POST /runtime/evals/run", s.handleEvalRun)
	mux.HandleFunc("POST /runtime/runs/{id}/resume", s.handleResumeRun)
	mux.HandleFunc("GET /runtime/approvals", s.handleListApprovals)
	mux.HandleFunc("GET /runtime/approvals/{id}", s.handleGetApproval)
	mux.HandleFunc("POST /runtime/approvals/{id}/resolve", s.handleResolveApproval)
	mux.HandleFunc("POST /runtime/approvals/callbacks/resolve", s.handleResolveApprovalCallback)
	mux.HandleFunc("POST /runtime/approvals/sync", s.handleSyncApprovals)
	mux.HandleFunc("GET /runtime/artifacts", s.handleListArtifacts)
	mux.HandleFunc("GET /runtime/artifacts/{id}", s.handleGetArtifact)
	mux.HandleFunc("GET /runtime/artifacts/{id}/content", s.handleReadArtifact)
	mux.HandleFunc("POST /runtime/artifacts/prune", s.handlePruneArtifacts)
	mux.HandleFunc("GET /runtime/runs", s.handleListRuns)
	mux.HandleFunc("GET /runtime/sessions", s.handleListSessions)
	mux.HandleFunc("GET /runtime/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("GET /runtime/sessions/{id}/messages", s.handleGetSessionMessages)
	mux.HandleFunc("POST /runtime/sessions/{id}/episode", s.handleStartSessionEpisode)
	mux.HandleFunc("POST /runtime/sessions/{id}/compact", s.handleCompactSession)
	mux.HandleFunc("DELETE /runtime/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("POST /runtime/runs/{id}/cancel", s.handleCancelRun)
	mux.HandleFunc("GET /runtime/events", s.handleListEvents)
	mux.HandleFunc("GET /runtime/events/stream", s.handleEventStream)
	// Keep the audit-prefixed path as a stable alias for older clients.
	mux.HandleFunc("GET /runtime/audit/events", s.handleListEvents)
	mux.HandleFunc("GET /runtime/memory/notebook", s.handleGetMemoryNotebook)
	mux.HandleFunc("GET /runtime/memory/status", s.handleGetMemoryStatus)
	mux.HandleFunc("POST /runtime/memory/index", s.handleReindexMemory)
	mux.HandleFunc("POST /runtime/memory/records", s.handleUpsertMemoryRecord)
	mux.HandleFunc("GET /runtime/memory/{key}", s.handleGetMemory)
	mux.HandleFunc("PUT /runtime/memory/{key}", s.handleSetMemory)
	mux.HandleFunc("DELETE /runtime/memory/{key}", s.handleDeleteMemory)
	mux.HandleFunc("GET /runtime/memory", s.handleSearchMemory)
	mux.HandleFunc("GET /runtime/projects", s.handleListProjects)
	mux.HandleFunc("GET /runtime/projects/{name}", s.handleGetProject)
	mux.HandleFunc("PATCH /runtime/projects/{name}", s.handleRenameProject)
	mux.HandleFunc("DELETE /runtime/projects/{name}", s.handleDeleteProject)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	projection := controlplane.ProjectOperationalHealth(s.config.OperationalWarnings)
	payload := healthResponse{
		OK:       projection.OK,
		State:    projection.State,
		Summary:  projection.Summary,
		Warnings: projection.Warnings,
	}
	writeJSON(w, http.StatusOK, payload)
}
