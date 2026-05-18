package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/eventbus"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlaudit "github.com/fulcrus/hopclaw/internal/controlplane/auditsink"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	controlpolicy "github.com/fulcrus/hopclaw/internal/controlplane/policypack"
	"github.com/fulcrus/hopclaw/policy"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type auditSinkStartRecorder struct {
	startCtx context.Context
}

type auditApplyContextKey string

func (s *auditSinkStartRecorder) Start(ctx context.Context) {
	s.startCtx = ctx
}

func (*auditSinkStartRecorder) Stop() {}

func (*auditSinkStartRecorder) Handle(context.Context, eventbus.Event) error { return nil }

func TestApprovalDispatcherForRuntime(t *testing.T) {
	t.Parallel()

	runtime := &runtimesvc.Service{}
	provider := approvalTestProvider{name: "jira"}

	tests := []struct {
		name     string
		runtime  *runtimesvc.Service
		registry *controlapproval.ProviderRegistry
		wantNil  bool
	}{
		{
			name:    "nil runtime",
			runtime: nil,
			registry: controlapproval.NewProviderRegistry(nil,
				provider,
			),
			wantNil: true,
		},
		{
			name:     "nil registry",
			runtime:  runtime,
			registry: nil,
			wantNil:  true,
		},
		{
			name:    "registry without enabled providers",
			runtime: runtime,
			registry: controlapproval.NewProviderRegistry([]controlapproval.ProviderDescriptor{{
				Name:    "jira",
				Enabled: false,
			}}, provider),
			wantNil: true,
		},
		{
			name:     "enabled provider",
			runtime:  runtime,
			registry: controlapproval.NewProviderRegistry(nil, provider),
			wantNil:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dispatcher := approvalDispatcherForRuntime(tt.runtime, tt.registry)
			if gotNil := dispatcher == nil; gotNil != tt.wantNil {
				t.Fatalf("approvalDispatcherForRuntime() nil = %v, want %v", gotNil, tt.wantNil)
			}
		})
	}
}

func TestEffectiveSnapshotStateUpdateFromControlPlaneBuildsExpectedSnapshot(t *testing.T) {
	t.Parallel()

	approvalRegistry := controlapproval.NewProviderRegistry([]controlapproval.ProviderDescriptor{{
		Name:    "jira",
		Type:    "webhook",
		Enabled: true,
		CallbackAuth: controlapproval.CallbackAuthPolicy{
			Mode:  "token",
			Token: "secret-token",
		},
	}})
	governanceRegistry := controlgov.NewAdapterRegistry([]controlgov.AdapterDescriptor{{
		Name:    "audit-hub",
		Type:    "webhook",
		Enabled: true,
	}})
	auditRegistry := controlaudit.NewRegistry([]controlaudit.SinkDescriptor{{
		Name:    "jsonl",
		Type:    "file",
		Enabled: true,
	}})
	state := &controlPlaneRuntimeState{
		resolvedPolicy: controlpolicy.Resolved{
			ProfileID: "business-production",
			Packs: []controlpolicy.Pack{
				{ID: "base-core"},
				{ID: "business-default"},
				{ID: "production-default"},
			},
			Config: policy.Config{
				RequireApprovalForWrite:        true,
				AllowLocalWriteWithoutApproval: true,
				RequireApprovalCommunity:       true,
				DenyDestructive:                true,
				DefaultApprovalScope:           approval.ScopeOnce,
				MaxApprovalScope:               approval.ScopeSession,
			},
		},
		approvalRegistry:   approvalRegistry,
		governanceRegistry: governanceRegistry,
		auditRegistry:      auditRegistry,
	}

	snapshotState := newEffectiveSnapshotStateFromControlPlane(
		"enterprise",
		true,
		state,
	)
	if snapshotState == nil {
		t.Fatal("newEffectiveSnapshotStateFromControlPlane() returned nil")
	}

	cfg := config.Config{
		Skills: config.SkillsConfig{
			InstallPolicy: config.SkillInstallPolicyAsk,
		},
		Security: config.SecurityConfig{
			DangerousTools: []string{"exec", "rm"},
		},
		Tools: config.ToolsConfig{
			Capabilities: config.CapabilitiesConfig{
				Exec: config.ExecConstraints{
					Mode: "allowlist",
				},
			},
		},
	}
	snapshot := snapshotState.Build(cfg, nil)
	if snapshot == nil {
		t.Fatal("Build() returned nil")
	}
	if snapshot.Edition != "enterprise" {
		t.Fatalf("Edition = %q, want enterprise", snapshot.Edition)
	}
	if snapshot.PolicyProfileID != "business-production" {
		t.Fatalf("PolicyProfileID = %q, want business-production", snapshot.PolicyProfileID)
	}
	if !reflect.DeepEqual(snapshot.PolicyPackIDs, []string{"base-core", "business-default", "production-default"}) {
		t.Fatalf("PolicyPackIDs = %#v", snapshot.PolicyPackIDs)
	}
	if !reflect.DeepEqual(snapshot.GovernanceAdapterNames, []string{"audit-hub"}) {
		t.Fatalf("GovernanceAdapterNames = %#v", snapshot.GovernanceAdapterNames)
	}
	if snapshot.Approval.ExecMode != "allowlist" {
		t.Fatalf("Approval.ExecMode = %q", snapshot.Approval.ExecMode)
	}
	if snapshot.Approval.SkillInstallPolicy != config.SkillInstallPolicyAsk {
		t.Fatalf("Approval.SkillInstallPolicy = %q", snapshot.Approval.SkillInstallPolicy)
	}
	if snapshot.Approval.DangerousToolCount != 2 {
		t.Fatalf("Approval.DangerousToolCount = %d, want 2", snapshot.Approval.DangerousToolCount)
	}
	if !snapshot.Approval.RequireApprovalForWrite || !snapshot.Approval.AllowLocalWriteWithoutApproval || !snapshot.Approval.RequireApprovalCommunity || !snapshot.Approval.DenyDestructive {
		t.Fatalf("Approval policy flags = %#v", snapshot.Approval)
	}
	if snapshot.Approval.DefaultGrantScope != string(approval.ScopeOnce) {
		t.Fatalf("Approval.DefaultGrantScope = %q", snapshot.Approval.DefaultGrantScope)
	}
	if snapshot.Approval.MaxGrantScope != string(approval.ScopeSession) {
		t.Fatalf("Approval.MaxGrantScope = %q", snapshot.Approval.MaxGrantScope)
	}
	if !snapshot.Approval.HasPolicyOverlay {
		t.Fatalf("Approval.HasPolicyOverlay = %#v", snapshot.Approval)
	}
	if !reflect.DeepEqual(snapshot.Approval.ExternalProviderNames, []string{"jira"}) {
		t.Fatalf("Approval.ExternalProviderNames = %#v", snapshot.Approval.ExternalProviderNames)
	}
	if !reflect.DeepEqual(snapshot.Approval.CallbackProviderNames, []string{"jira"}) {
		t.Fatalf("Approval.CallbackProviderNames = %#v", snapshot.Approval.CallbackProviderNames)
	}
}

func TestEffectiveSnapshotStateUpdateFromControlPlaneClearsStateWhenNil(t *testing.T) {
	t.Parallel()

	snapshotState := newEffectiveSnapshotState(
		"enterprise",
		controlpolicy.Resolved{
			ProfileID: "default-production",
			Packs:     []controlpolicy.Pack{{ID: "base-core"}},
		},
		false,
		nil,
		controlapproval.NewProviderRegistry([]controlapproval.ProviderDescriptor{{
			Name:    "jira",
			Enabled: true,
		}}),
		controlgov.NewAdapterRegistry([]controlgov.AdapterDescriptor{{
			Name:    "audit-hub",
			Enabled: true,
		}}),
		controlaudit.NewRegistry([]controlaudit.SinkDescriptor{{
			Name:    "jsonl",
			Enabled: true,
		}}),
	)

	snapshotState.UpdateFromControlPlane("community", true, nil)
	snapshot := snapshotState.Build(config.Config{}, nil)
	if snapshot == nil {
		t.Fatal("Build() returned nil")
	}
	if snapshot.Edition != "community" {
		t.Fatalf("Edition = %q, want community", snapshot.Edition)
	}
	if snapshot.PolicyProfileID != "" {
		t.Fatalf("PolicyProfileID = %q, want empty", snapshot.PolicyProfileID)
	}
	if len(snapshot.PolicyPackIDs) != 0 {
		t.Fatalf("PolicyPackIDs = %#v, want empty", snapshot.PolicyPackIDs)
	}
	if len(snapshot.GovernanceAdapterNames) != 0 {
		t.Fatalf("GovernanceAdapterNames = %#v, want empty", snapshot.GovernanceAdapterNames)
	}
	if !snapshot.Approval.HasPolicyOverlay {
		t.Fatalf("Approval.HasPolicyOverlay = %#v", snapshot.Approval)
	}
	if len(snapshot.Approval.ExternalProviderNames) != 0 {
		t.Fatalf("Approval.ExternalProviderNames = %#v, want empty", snapshot.Approval.ExternalProviderNames)
	}
	if len(snapshot.Approval.CallbackProviderNames) != 0 {
		t.Fatalf("Approval.CallbackProviderNames = %#v, want empty", snapshot.Approval.CallbackProviderNames)
	}
}

func TestBuildRuntimeAuditSinkIncludesWebhookSinks(t *testing.T) {
	t.Parallel()

	received := make(chan eventbus.Event, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var event eventbus.Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		received <- event
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	sink, err := newRuntimeAuditSink(config.Config{
		Runtime: config.RuntimeConfig{
			Audit: config.AuditConfig{
				Enabled: true,
				Sinks: []config.AuditSinkConfig{{
					Name: "audit-hub",
					Webhook: config.AuditWebhookSinkConfig{
						URL:     server.URL,
						Timeout: time.Second,
					},
				}},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("newRuntimeAuditSink() error = %v", err)
	}
	if sink == nil {
		t.Fatal("newRuntimeAuditSink() returned nil")
	}
	if starter, ok := sink.(interface{ Start(context.Context) }); ok {
		starter.Start(context.Background())
		defer func() {
			if stopper, ok := sink.(interface{ Stop() }); ok {
				stopper.Stop()
			}
		}()
	}
	if err := sink.Handle(context.Background(), eventbus.Event{Type: eventbus.EventRunCompleted, RunID: "run-1"}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	got := <-received
	if got.RunID != "run-1" {
		t.Fatalf("received event = %+v", got)
	}
}

func TestBuildRuntimeAuditSinkIncludesElasticsearchSinks(t *testing.T) {
	t.Parallel()

	received := make(chan eventbus.Event, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hopclaw-audit/_doc/evt-es-1" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		defer r.Body.Close()
		var event eventbus.Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		received <- event
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	sink, err := newRuntimeAuditSink(config.Config{
		Runtime: config.RuntimeConfig{
			Audit: config.AuditConfig{
				Enabled: true,
				Sinks: []config.AuditSinkConfig{{
					Name: "corp-es",
					Elasticsearch: config.AuditElasticsearchSinkConfig{
						URL:     server.URL,
						Index:   "hopclaw-audit",
						Timeout: time.Second,
					},
				}},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("newRuntimeAuditSink() error = %v", err)
	}
	if sink == nil {
		t.Fatal("newRuntimeAuditSink() returned nil")
	}
	if starter, ok := sink.(interface{ Start(context.Context) }); ok {
		starter.Start(context.Background())
		defer func() {
			if stopper, ok := sink.(interface{ Stop() }); ok {
				stopper.Stop()
			}
		}()
	}
	if err := sink.Handle(context.Background(), eventbus.Event{ID: "evt-es-1", Type: eventbus.EventRunCompleted, RunID: "run-es-1"}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	got := <-received
	if got.RunID != "run-es-1" {
		t.Fatalf("received event = %+v", got)
	}
}

func TestApplyRuntimeAuditRegistryPropagatesContextToSinkStart(t *testing.T) {
	t.Parallel()

	recorder := &auditSinkStartRecorder{}
	app := &App{
		appInternalState: appInternalState{
			auditSink: newDynamicAuditSink(nil),
		},
	}
	ctx := context.WithValue(context.Background(), auditApplyContextKey("audit-trace"), "refresh-audit-sink")

	app.applyRuntimeAuditRegistry(ctx, controlaudit.NewRegistry(nil), recorder)

	if recorder.startCtx == nil {
		t.Fatal("sink Start() was not called")
	}
	if got := recorder.startCtx.Value(auditApplyContextKey("audit-trace")); got != "refresh-audit-sink" {
		t.Fatalf("start context value = %#v, want propagated trace id", got)
	}
}

func TestAuditSinkDescriptorsIncludeConnectorMetadata(t *testing.T) {
	t.Parallel()

	descriptors := auditSinkDescriptors(config.Config{
		Store: config.StoreConfig{Backend: "sqlite"},
		Runtime: config.RuntimeConfig{
			Audit: config.AuditConfig{
				Enabled: true,
				Sinks: []config.AuditSinkConfig{{
					Name: "corp-es",
					Elasticsearch: config.AuditElasticsearchSinkConfig{
						URL:   "https://es.example.com",
						Index: "hopclaw-audit",
					},
				}, {
					Name: "corp-splunk",
					SplunkHEC: config.AuditSplunkHECSinkConfig{
						URL:    "https://splunk.example.com/services/collector",
						Token:  "secret",
						Source: "hopclaw",
						Index:  "main",
					},
				}},
			},
		},
	})
	if len(descriptors) != 2 {
		t.Fatalf("len(descriptors) = %d, want 2", len(descriptors))
	}
	if descriptors[0].Type != "elasticsearch" || descriptors[0].Metadata["delivery_backend"] != "sqlite" || descriptors[0].Metadata["index"] != "hopclaw-audit" {
		t.Fatalf("elasticsearch descriptor = %+v", descriptors[0])
	}
	if descriptors[1].Type != "splunk_hec" || descriptors[1].Metadata["delivery_backend"] != "sqlite" || descriptors[1].Metadata["source"] != "hopclaw" {
		t.Fatalf("splunk descriptor = %+v", descriptors[1])
	}
}

type approvalTestProvider struct {
	name string
}

func (p approvalTestProvider) Name() string {
	return p.name
}

func (approvalTestProvider) SubmitApproval(context.Context, controlapproval.SubmitRequest) (*controlapproval.Submission, error) {
	return nil, nil
}

func (approvalTestProvider) UpdateApproval(context.Context, controlapproval.UpdateRequest) error {
	return nil
}
