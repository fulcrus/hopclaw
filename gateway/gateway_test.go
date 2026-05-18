package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	browsercap "github.com/fulcrus/hopclaw/capabilities/browser"
	runtimecap "github.com/fulcrus/hopclaw/capabilities/runtime"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/allowlist"
	"github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/cron"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/heartbeat"
	"github.com/fulcrus/hopclaw/hooks"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlaudit "github.com/fulcrus/hopclaw/internal/controlplane/auditsink"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/policy"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/sandbox"
	"github.com/fulcrus/hopclaw/server"
	"github.com/fulcrus/hopclaw/store"
	"github.com/fulcrus/hopclaw/usage"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
	"github.com/fulcrus/hopclaw/wire"
)

func TestGatewayRootRedirectsToDashboard(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{AuthToken: "secret"}).Handler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("GET / status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc != "/dashboard/" {
		t.Fatalf("GET / Location = %q, want /dashboard/", loc)
	}
}

func TestGatewayDashboardRedirectsToDashboardIndex(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{AuthToken: "secret"}).Handler()

	// /dashboard should redirect to the canonical dashboard index (301).
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("GET /dashboard status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	loc := rec.Header().Get("Location")
	if loc != "/dashboard/" {
		t.Fatalf("GET /dashboard Location = %q, want /dashboard/", loc)
	}
}

func TestGatewayLegacyWebchatRedirectsToDashboardIndex(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{AuthToken: "secret"}).Handler()

	for _, path := range []string{"/webchat", "/webchat/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("GET %s status = %d, want %d", path, rec.Code, http.StatusMovedPermanently)
		}
		loc := rec.Header().Get("Location")
		if loc != "/dashboard/" {
			t.Fatalf("GET %s Location = %q, want /dashboard/", path, loc)
		}
		if got := rec.Header().Get("Deprecation"); got != "true" {
			t.Fatalf("GET %s Deprecation = %q, want true", path, got)
		}
		if got := rec.Header().Get("Sunset"); got != "2027-01-01" {
			t.Fatalf("GET %s Sunset = %q, want 2027-01-01", path, got)
		}
		if got := rec.Header().Get("Link"); got != `</dashboard/>; rel="successor-version"` {
			t.Fatalf("GET %s Link = %q, want successor-version link", path, got)
		}
	}
}

func TestGatewayHealthzRemainsOnPublicServerSurface(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{AuthToken: "secret"}).Handler()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayOperatorEndpointsRequireAuth(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{
		AuthToken:    "secret",
		Capabilities: testRegistry(t),
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /operator/status without token status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/status with token status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayStatusIncludesRunAndChannelSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runs := agent.NewInMemoryRunStore()
	sessions := agent.NewInMemorySessionStore()
	svc := runtimepkg.NewService(nil, sessions, runs, nil, eventbus.NewInMemoryBus(), nil)

	running, err := runs.Create(ctx, "sess-1", agent.IncomingMessage{Content: "running"}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create(running) error = %v", err)
	}
	running.Status = agent.RunRunning
	if err := runs.Update(ctx, running); err != nil {
		t.Fatalf("Update(running) error = %v", err)
	}

	if _, err := runs.Create(ctx, "sess-2", agent.IncomingMessage{Content: "queued"}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue}); err != nil {
		t.Fatalf("Create(queued) error = %v", err)
	}

	completed, err := runs.Create(ctx, "sess-3", agent.IncomingMessage{Content: "completed"}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create(completed) error = %v", err)
	}
	completed.Status = agent.RunCompleted
	if err := runs.Update(ctx, completed); err != nil {
		t.Fatalf("Update(completed) error = %v", err)
	}

	manager := channelmgr.New()
	if err := manager.Register("slack", &stubChannelAdapter{status: channels.StatusConnected}); err != nil {
		t.Fatalf("Register(slack) error = %v", err)
	}
	if err := manager.Register("discord", &stubChannelAdapter{status: channels.StatusDisconnected}); err != nil {
		t.Fatalf("Register(discord) error = %v", err)
	}

	srv := server.New(svc, server.Config{AuthToken: "secret"})
	gw := gatewayFromServer(srv, Config{AuthToken: "secret", Runtime: svc, Channels: manager})

	req := httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	gw.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/status status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload statusResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.ActiveRuns != 1 {
		t.Fatalf("ActiveRuns = %d, want 1", payload.ActiveRuns)
	}
	if payload.QueuedRuns != 1 {
		t.Fatalf("QueuedRuns = %d, want 1", payload.QueuedRuns)
	}
	if len(payload.ConnectedChannels) != 2 {
		t.Fatalf("len(ConnectedChannels) = %d, want 2", len(payload.ConnectedChannels))
	}
	if payload.ConnectedChannels[0].Name != "discord" || payload.ConnectedChannels[0].Status != "disconnected" {
		t.Fatalf("ConnectedChannels[0] = %+v", payload.ConnectedChannels[0])
	}
	if payload.ConnectedChannels[1].Name != "slack" || payload.ConnectedChannels[1].Status != "connected" {
		t.Fatalf("ConnectedChannels[1] = %+v", payload.ConnectedChannels[1])
	}
}

func TestGatewayStatusProjectsOperationalWarningsAsDegraded(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	svc := runtimepkg.NewService(nil, sessions, runs, nil, eventbus.NewInMemoryBus(), nil)
	srv := server.New(svc, server.Config{AuthToken: "secret"})
	gw := gatewayFromServer(srv, Config{AuthToken: "secret", Runtime: svc})
	gw.ApplyKernelServices(KernelServices{
		OperationalWarnings: operationalWarningSourceStub{warnings: []controlplane.OperationalWarning{{
			Component: "config_store",
			Summary:   "Dynamic config store unavailable; using YAML-only mode",
		}}},
	})

	req := httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	gw.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/status status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload statusResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.OK {
		t.Fatalf("payload.OK = true, want degraded status: %+v", payload)
	}
	if payload.State != "degraded" {
		t.Fatalf("payload.State = %q, want degraded", payload.State)
	}
	if payload.Summary != "Dynamic config store unavailable; using YAML-only mode" {
		t.Fatalf("payload.Summary = %q", payload.Summary)
	}
	if len(payload.Warnings) != 1 || payload.Warnings[0] != "Dynamic config store unavailable; using YAML-only mode" {
		t.Fatalf("payload.Warnings = %#v", payload.Warnings)
	}
}

func TestGatewayRuntimeUsesGatewayAuthBoundaryOnly(t *testing.T) {
	t.Parallel()

	runs := agent.NewInMemoryRunStore()
	sessions := agent.NewInMemorySessionStore()
	svc := runtimepkg.NewService(nil, sessions, runs, nil, eventbus.NewInMemoryBus(), nil)
	srv := server.New(svc, server.Config{AuthToken: "server-secret"})
	handler := gatewayFromServer(srv, Config{
		AuthToken: "gateway-secret",
		Runtime:   svc,
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/runtime/events", nil)
	req.Header.Set("Authorization", "Bearer gateway-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/events with gateway token status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/runtime/events", nil)
	req.Header.Set("Authorization", "Bearer server-secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /runtime/events with server token status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayValidateIntegrityReportsMissingWiring(t *testing.T) {
	t.Parallel()

	gw := New(nil, nil, Config{})
	err := gw.ValidateIntegrity()
	if err == nil {
		t.Fatal("expected integrity error")
	}
	if !strings.Contains(err.Error(), "runtime service missing") {
		t.Fatalf("error = %q", err)
	}
	if !strings.Contains(err.Error(), "websocket handler missing") {
		t.Fatalf("error = %q", err)
	}
}

func TestGatewayApprovalCallbackBypassesGatewayAuth(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
	}, sessions, runs, agent.NewInMemoryCoordinator(), nil, nil, nil, nil).
		WithApprovals(approvals)
	svc := runtimepkg.NewService(component, sessions, runs, approvals, eventbus.NewInMemoryBus(), nil)
	srv := server.New(svc, server.Config{
		AuthToken: "server-secret",
		ApprovalCallbacks: map[string]controlapproval.CallbackAuthPolicy{
			"jira": {HeaderName: "X-HopClaw-Approval-Token", Token: "jira-secret"},
		},
	})
	handler := gatewayFromServer(srv, Config{
		AuthToken: "gateway-secret",
		Runtime:   svc,
	}).Handler()

	session, err := sessions.GetOrCreate(context.Background(), "callback-session", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(context.Background(), session.ID, agent.IncomingMessage{
		SessionKey:      "callback-session",
		ExternalEventID: "evt-callback",
		Content:         "await approval",
	}, agent.AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	ticket, err := approvals.Create(context.Background(), approval.Ticket{
		RunID:     run.ID,
		SessionID: session.ID,
		Kind:      approval.KindToolCalls,
	})
	if err != nil {
		t.Fatalf("approvals.Create() error = %v", err)
	}
	run.Status = agent.RunWaitingApproval
	run.ApprovalID = ticket.ID
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	body, err := json.Marshal(controlapproval.ResolveCallbackRequest{
		Provider: "jira",
		TicketID: ticket.ID,
		Status:   "denied",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/callbacks/resolve", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HopClaw-Approval-Token", "jira-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST callback status = %d body=%s", rec.Code, rec.Body.String())
	}

	var view runtimepkg.ApprovalView
	if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if view.Status != approval.StatusDenied {
		t.Fatalf("view.Status = %q, want denied", view.Status)
	}
	if view.ResolvedBy != "provider:jira" {
		t.Fatalf("view.ResolvedBy = %q, want provider:jira", view.ResolvedBy)
	}
}

func TestGatewayCapabilitiesEndpointReturnsReports(t *testing.T) {
	t.Parallel()

	browserd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer browserd.Close()

	reg := capregistry.New()
	if err := reg.Register(runtimecap.New()); err != nil {
		t.Fatalf("Register(runtime) error = %v", err)
	}
	if err := reg.Register(browsercap.New(browsercap.Config{BaseURL: browserd.URL})); err != nil {
		t.Fatalf("Register(browser) error = %v", err)
	}

	handler := newTestGateway(t, Config{
		AuthToken:    "secret",
		Capabilities: reg,
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/capabilities", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/capabilities status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []struct {
			Manifest struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
			} `json:"manifest"`
			Health struct {
				Status string `json:"status"`
			} `json:"health"`
		} `json:"items"`
		Count int `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 2 {
		t.Fatalf("payload.Count = %d", payload.Count)
	}
	if payload.Items[0].Manifest.Name != "browser" && payload.Items[1].Manifest.Name != "browser" {
		t.Fatalf("capability items = %#v", payload.Items)
	}
}

func TestGatewayDispatchesChannelHTTPInbound(t *testing.T) {
	t.Parallel()

	mgr := channelmgr.New()
	adapter := &stubHTTPInboundAdapter{}
	if err := mgr.Register("line", adapter); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	handler := newTestGateway(t, Config{Channels: mgr}).Handler()
	req := httptest.NewRequest(http.MethodPost, "/channels/line/inbound", strings.NewReader(`{"hello":"world"}`))
	req.Header.Set("X-Test", "value")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /channels/line/inbound status = %d body=%s", rec.Code, rec.Body.String())
	}
	if adapter.lastReq.Method != http.MethodPost {
		t.Fatalf("adapter.lastReq.Method = %q", adapter.lastReq.Method)
	}
	if string(adapter.lastReq.Body) != `{"hello":"world"}` {
		t.Fatalf("adapter.lastReq.Body = %q", string(adapter.lastReq.Body))
	}
}

func TestGatewayApplyServiceGroups(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)

	cronService := &cron.Service{}
	watchService := &watch.Service{}
	heartbeatService := &heartbeat.Service{}
	wireLogger := &wire.Logger{}
	wakeupService := &wakeup.Service{}
	approvals := approval.NewInMemoryStore()
	grantStore := approval.NewGrantStore()
	allowlistManager := allowlist.NewManager(nil)
	sandboxRunner := &sandbox.Runner{}
	discoveryResolver := stubDiscoveryResolver{}
	channelHealth := &health.Monitor{}
	hookExecutor := hooks.NewExecutor(hooks.NewInMemoryStore())
	usageStore := usage.NewInMemoryStore()
	approvalProviders := controlapproval.NewProviderRegistry(nil)
	governanceAdapters := controlgov.NewAdapterRegistry(nil)
	auditSinks := controlaudit.NewRegistry(nil)
	channelManager := channelmgr.New()
	extensions := extregistry.New(extregistry.Options{})
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()
	configStore, err := store.NewConfigStore(db)
	if err != nil {
		t.Fatalf("store.NewConfigStore() error = %v", err)
	}
	configMutator := controloverlay.NewMutationService(configStore, nil, controloverlay.MutationOptions{})
	knowledgeService := &knowledge.Service{}
	browserClient := &browserclient.Client{}
	desktopClient := &desktopclient.Client{}
	capabilities := capregistry.New()
	configWatcher := config.NewWatcher(t.TempDir()+"/config.yaml", config.Config{}, time.Hour)

	gw.ApplyAutomationServices(AutomationServices{
		Cron:      cronService,
		Watch:     watchService,
		Heartbeat: heartbeatService,
		Wire:      wireLogger,
		Wakeup:    wakeupService,
	})
	gw.ApplySupportServices(SupportServices{
		Approvals:     approvals,
		GrantStore:    grantStore,
		Allowlist:     allowlistManager,
		Sandbox:       sandboxRunner,
		Discovery:     discoveryResolver,
		ChannelHealth: channelHealth,
		Hooks:         hookExecutor,
		UsageStore:    usageStore,
	})
	gw.ApplyKernelServices(KernelServices{
		ApprovalProviders:  approvalProviders,
		GovernanceAdapters: governanceAdapters,
		AuditSinks:         auditSinks,
		PolicyEngine:       policy.NewDefaultEngine(policy.Config{}),
		ThreadBindings:     channels.NewThreadBinding(),
		ConfigStore:        configStore,
		ConfigMutator:      configMutator,
	})
	gw.ApplyIntegrationServices(IntegrationServices{
		Channels:   channelManager,
		Extensions: extensions,
	})
	gw.ApplyHostServices(HostServices{
		BrowserClient: browserClient,
		DesktopClient: desktopClient,
		Capabilities:  capabilities,
	})
	gw.ApplyKnowledgeServices(KnowledgeServices{Knowledge: knowledgeService})
	gw.ApplyConfigServices(ConfigServices{Watcher: configWatcher, Path: "/tmp/config.yaml"})

	if gw.cron != cronService || gw.watch != watchService || gw.heartbeat != heartbeatService || gw.wire != wireLogger || gw.wakeup != wakeupService {
		t.Fatalf("automation services not applied: %#v", gw)
	}
	if gw.approvals != approvals || gw.grantStore != grantStore || gw.allowlist != allowlistManager || gw.sandbox != sandboxRunner || gw.discovery == nil || gw.channelHealth != channelHealth || gw.hooks != hookExecutor || gw.usageStore != usageStore {
		t.Fatalf("support services not applied: %#v", gw)
	}
	if gw.approvalProviders != approvalProviders || gw.governanceAdapters != governanceAdapters || gw.auditSinks != auditSinks || gw.threadBindings == nil || gw.configStore != configStore || gw.configMutator != configMutator {
		t.Fatalf("kernel services not applied: %#v", gw)
	}
	if gw.channels != channelManager || gw.extensions != extensions {
		t.Fatalf("integration services not applied: %#v", gw)
	}
	if gw.browserClient != browserClient || gw.desktopClient != desktopClient || gw.capabilities != capabilities {
		t.Fatalf("host services not applied: %#v", gw)
	}
	if gw.knowledge != knowledgeService {
		t.Fatalf("knowledge services not applied: %#v", gw)
	}
	if gw.configWatcher != configWatcher || gw.configPath != "/tmp/config.yaml" {
		t.Fatalf("config services not applied: %#v", gw)
	}
	if gw.policyEngine == nil {
		t.Fatal("expected policy engine to be applied")
	}
}

func TestGatewayHTTPStatusForError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "wrapped cron not found", err: fmt.Errorf("lookup: %w", cron.ErrNotFound), want: http.StatusNotFound},
		{name: "approval already resolved", err: approval.ErrAlreadyResolved, want: http.StatusConflict},
		{name: "hook replay conflict", err: hooks.ErrNoReplayPayload, want: http.StatusConflict},
		{name: "approval syncer unavailable", err: runtimepkg.ErrApprovalSyncerNil, want: http.StatusServiceUnavailable},
		{name: "rate limited", err: runtimepkg.ErrRateLimited, want: http.StatusTooManyRequests},
		{name: "fallback invalid", err: errors.New("invalid request payload"), want: http.StatusBadRequest},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := gatewayHTTPStatusForError(tc.err, http.StatusInternalServerError); got != tc.want {
				t.Fatalf("gatewayHTTPStatusForError(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestGatewayChannelHTTPInboundSurfacesAdapterStatus(t *testing.T) {
	t.Parallel()

	mgr := channelmgr.New()
	if err := mgr.Register("line", &stubHTTPInboundAdapter{
		err: channels.NewHTTPInboundError(http.StatusUnauthorized, "bad signature"),
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	handler := newTestGateway(t, Config{Channels: mgr}).Handler()
	req := httptest.NewRequest(http.MethodPost, "/channels/line/inbound", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /channels/line/inbound status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func newTestGateway(t *testing.T, cfg Config) *Gateway {
	t.Helper()

	runs := agent.NewInMemoryRunStore()
	sessions := agent.NewInMemorySessionStore()
	svc := runtimepkg.NewService(nil, sessions, runs, nil, eventbus.NewInMemoryBus(), nil)
	srv := server.New(svc, server.Config{AuthToken: cfg.AuthToken})
	if cfg.Runtime == nil {
		cfg.Runtime = svc
	}
	return gatewayFromServer(srv, cfg)
}

func testRegistry(t *testing.T) *capregistry.Registry {
	t.Helper()

	reg := capregistry.New()
	if err := reg.Register(runtimecap.New()); err != nil {
		t.Fatalf("Register(runtime) error = %v", err)
	}
	return reg
}

type stubHTTPInboundAdapter struct {
	lastReq channels.HTTPInboundRequest
	err     error
}

func (a *stubHTTPInboundAdapter) Connect(context.Context) error    { return nil }
func (a *stubHTTPInboundAdapter) Disconnect(context.Context) error { return nil }
func (a *stubHTTPInboundAdapter) Send(context.Context, channels.OutboundMessage) error {
	return nil
}
func (a *stubHTTPInboundAdapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.ChannelCapabilityDescriptor{}
}
func (a *stubHTTPInboundAdapter) Status() channels.Status { return channels.StatusConnected }
func (a *stubHTTPInboundAdapter) SubscribeEvents() <-chan channels.InboundMessage {
	return make(chan channels.InboundMessage)
}
func (a *stubHTTPInboundAdapter) HandleHTTPInbound(_ context.Context, req channels.HTTPInboundRequest) (*channels.HTTPInboundResponse, error) {
	a.lastReq = req
	if a.err != nil {
		return nil, a.err
	}
	return &channels.HTTPInboundResponse{
		StatusCode: http.StatusAccepted,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte(`{"ok":true}`),
	}, nil
}

func TestGatewayOperatorArtifactsEndpoint(t *testing.T) {
	t.Parallel()

	// Create gateway with an artifact store containing one item.
	runs := agent.NewInMemoryRunStore()
	sessions := agent.NewInMemorySessionStore()
	artStore := artifact.NewInMemoryStore()
	svc := runtimepkg.NewService(nil, sessions, runs, nil, eventbus.NewInMemoryBus(), artStore)
	srv := server.New(svc, server.Config{AuthToken: "secret"})

	_, _ = artStore.Put(context.Background(), artifact.PutRequest{
		Kind:        "browser.screenshot",
		ContentType: "image/png",
		Body:        []byte("fake-png"),
	})

	gw := gatewayFromServer(srv, Config{AuthToken: "secret", Runtime: svc})
	handler := gw.Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/artifacts", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/artifacts status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []struct {
			Kind        string `json:"kind"`
			ContentType string `json:"content_type"`
		} `json:"items"`
		Count int `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("payload.Count = %d, want 1", payload.Count)
	}
	if payload.Items[0].Kind != "browser.screenshot" {
		t.Fatalf("artifact kind = %q, want browser.screenshot", payload.Items[0].Kind)
	}
}

func TestGatewayArtifactPreviewServesContent(t *testing.T) {
	t.Parallel()

	runs := agent.NewInMemoryRunStore()
	sessions := agent.NewInMemorySessionStore()
	artStore := artifact.NewInMemoryStore()
	svc := runtimepkg.NewService(nil, sessions, runs, nil, eventbus.NewInMemoryBus(), artStore)
	srv := server.New(svc, server.Config{AuthToken: "secret"})

	blob, _ := artStore.Put(context.Background(), artifact.PutRequest{
		Kind:        "tool_output",
		ContentType: "text/plain; charset=utf-8",
		Body:        []byte("hello world"),
	})

	gw := gatewayFromServer(srv, Config{AuthToken: "secret", Runtime: svc})
	handler := gw.Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/artifacts/"+blob.ID+"/preview", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET preview status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Content-Disposition") != "inline" {
		t.Fatalf("Content-Disposition = %q, want inline", rec.Header().Get("Content-Disposition"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", rec.Header().Get("X-Content-Type-Options"))
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	if rec.Body.String() != "hello world" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestGatewayArtifactPreviewForcesAttachmentForHTML(t *testing.T) {
	t.Parallel()

	runs := agent.NewInMemoryRunStore()
	sessions := agent.NewInMemorySessionStore()
	artStore := artifact.NewInMemoryStore()
	svc := runtimepkg.NewService(nil, sessions, runs, nil, eventbus.NewInMemoryBus(), artStore)
	srv := server.New(svc, server.Config{AuthToken: "secret"})

	blob, _ := artStore.Put(context.Background(), artifact.PutRequest{
		Kind:        "tool_output",
		ContentType: "text/html; charset=utf-8",
		Body:        []byte("<script>alert(1)</script>"),
	})

	gw := gatewayFromServer(srv, Config{AuthToken: "secret", Runtime: svc})
	handler := gw.Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/artifacts/"+blob.ID+"/preview", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET html preview status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/html" {
		t.Fatalf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Content-Disposition") != "attachment" {
		t.Fatalf("Content-Disposition = %q, want attachment", rec.Header().Get("Content-Disposition"))
	}
}

func TestGatewayBrowserSessionsEmpty(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{
		AuthToken:    "secret",
		Capabilities: testRegistry(t),
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/browser/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/browser/sessions status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []any `json:"items"`
		Count int   `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("payload.Count = %d, want 0", payload.Count)
	}
}

func TestGatewayCapabilitySessionsEndpoint(t *testing.T) {
	t.Parallel()

	reg := testRegistry(t)
	if err := reg.Register(&stubSessionCapability{
		manifest: captypes.Manifest{Name: "desktop", Kind: captypes.KindSession},
		sessions: []*captypes.SessionHandle{{
			ID:         "desktop-session-1",
			Capability: "desktop",
			CreatedAt:  time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC),
		}},
	}); err != nil {
		t.Fatalf("Register(desktop) error = %v", err)
	}

	handler := newTestGateway(t, Config{
		AuthToken:    "secret",
		Capabilities: reg,
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/capabilities/desktop/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/capabilities/desktop/sessions status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []captypes.SessionHandle `json:"items"`
		Count int                      `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 || payload.Items[0].ID != "desktop-session-1" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestGatewayReportsUseRequestContext(t *testing.T) {
	t.Parallel()

	reg := capregistry.New()
	if err := reg.Register(runtimecap.New()); err != nil {
		t.Fatalf("Register(runtime) error = %v", err)
	}

	gw := newTestGateway(t, Config{Capabilities: reg})
	reports := gw.capabilityReports(context.Background())
	if len(reports) != 1 {
		t.Fatalf("len(reports) = %d", len(reports))
	}
}

type stubSessionCapability struct {
	manifest captypes.Manifest
	sessions []*captypes.SessionHandle
}

func (s *stubSessionCapability) Manifest() captypes.Manifest { return s.manifest }

func (s *stubSessionCapability) Health(context.Context) captypes.Health {
	return captypes.Health{Status: captypes.StatusReady}
}

func (s *stubSessionCapability) Invoke(context.Context, captypes.InvokeRequest) (*captypes.InvokeResult, error) {
	return &captypes.InvokeResult{OK: true}, nil
}

func (s *stubSessionCapability) OpenSession(context.Context, map[string]any) (*captypes.SessionHandle, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *stubSessionCapability) CloseSession(context.Context, string) error {
	return nil
}

func (s *stubSessionCapability) ListSessions() []*captypes.SessionHandle {
	out := make([]*captypes.SessionHandle, len(s.sessions))
	copy(out, s.sessions)
	return out
}
