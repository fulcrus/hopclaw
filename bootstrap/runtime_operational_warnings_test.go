package bootstrap

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
)

func TestDeliveryFailureWarningCollectorProjectsRecentChannelFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 14, 3, 0, 0, 0, time.UTC)
	collector := newDeliveryFailureWarningCollector(10 * time.Minute)
	collector.now = func() time.Time { return now }

	if err := collector.Handle(context.Background(), eventbus.NewDeliveryFailedEvent(
		"run-1",
		"sess-1",
		eventbus.DeliveryFailedPayload{
			Channel:    "slack",
			TargetID:   "C123",
			Attempts:   4,
			Error:      "rate limit retries exhausted",
			StatusKind: "chat_reply",
		},
		nil,
	)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	warnings := collector.OperationalWarnings()
	if len(warnings) != 1 {
		t.Fatalf("OperationalWarnings() = %#v, want 1 warning", warnings)
	}
	if warnings[0].Component != "channel/slack/delivery" {
		t.Fatalf("Component = %q, want channel/slack/delivery", warnings[0].Component)
	}
	if warnings[0].Summary != `Channel "slack" recently failed to deliver replies` {
		t.Fatalf("Summary = %q", warnings[0].Summary)
	}
	if got := warnings[0].Detail; got == "" || !containsAll(got, "1 recent delivery failure(s)", "target=C123", "attempts=4", "status=chat_reply", "run=run-1", "error=rate limit retries exhausted") {
		t.Fatalf("Detail = %q", got)
	}
}

func TestDeliveryFailureWarningCollectorExpiresWarnings(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 14, 3, 0, 0, 0, time.UTC)
	collector := newDeliveryFailureWarningCollector(2 * time.Minute)
	collector.now = func() time.Time { return now }

	if err := collector.Handle(context.Background(), eventbus.NewDeliveryFailedEvent(
		"run-1",
		"sess-1",
		eventbus.DeliveryFailedPayload{Channel: "discord", Error: "send failed"},
		nil,
	)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(collector.OperationalWarnings()) != 1 {
		t.Fatalf("OperationalWarnings() should include fresh warning")
	}

	now = now.Add(3 * time.Minute)
	if warnings := collector.OperationalWarnings(); len(warnings) != 0 {
		t.Fatalf("OperationalWarnings() after expiry = %#v, want empty", warnings)
	}
}

func TestCombinedOperationalWarningSourceMergesStartupAndRuntimeWarnings(t *testing.T) {
	t.Parallel()

	startup := newStartupWarningCollector()
	startup.AddDetailed("config_store", "Dynamic config store unavailable; using YAML-only mode", "control.db denied", "restore DB access")

	now := time.Date(2026, 4, 14, 3, 0, 0, 0, time.UTC)
	delivery := newDeliveryFailureWarningCollector(10 * time.Minute)
	delivery.now = func() time.Time { return now }
	if err := delivery.Handle(context.Background(), eventbus.NewDeliveryFailedEvent(
		"run-2",
		"sess-2",
		eventbus.DeliveryFailedPayload{Channel: "slack", Error: "send failed"},
		nil,
	)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	source := newCombinedOperationalWarningSource(startup, delivery)
	if source == nil {
		t.Fatal("newCombinedOperationalWarningSource() = nil")
	}

	app := &App{
		appInternalState: appInternalState{
			startupWarnings:     startup,
			operationalWarnings: source,
		},
	}
	warnings := app.OperationalWarnings()
	if len(warnings) != 2 {
		t.Fatalf("App.OperationalWarnings() = %#v, want 2 warnings", warnings)
	}
	if warnings[0].Component != "channel/slack/delivery" || warnings[1].Component != "config_store" {
		t.Fatalf("App.OperationalWarnings() = %#v", warnings)
	}
}

func TestRuntimeEventWarningCollectorProjectsApprovalTimeout(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 14, 4, 0, 0, 0, time.UTC)
	collector := newRuntimeEventWarningCollector(10 * time.Minute)
	collector.now = func() time.Time { return now }

	if err := collector.Handle(context.Background(), eventbus.NewApprovalTimedOutEvent(
		"run-approval",
		"sess-approval",
		eventbus.ApprovalEventAttrs{
			ApprovalID:    "approval-1",
			Status:        "timed_out",
			PolicySummary: "ops approval",
		},
		nil,
	)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	warning, ok := warningByComponent(collector.OperationalWarnings(), "approval_timeout/recent")
	if !ok {
		t.Fatalf("OperationalWarnings() missing approval timeout warning: %#v", collector.OperationalWarnings())
	}
	if warning.Summary != "Approvals recently timed out while runs were waiting" {
		t.Fatalf("Summary = %q", warning.Summary)
	}
	if !containsAll(warning.Detail, "1 recent timeout event(s)", "approval=approval-1", "run=run-approval", "status=timed_out", "policy=ops approval") {
		t.Fatalf("Detail = %q", warning.Detail)
	}
}

func TestRuntimeEventWarningCollectorProjectsGovernanceDeliveryIssues(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 14, 4, 5, 0, 0, time.UTC)
	collector := newRuntimeEventWarningCollector(10 * time.Minute)
	collector.now = func() time.Time { return now }

	if err := collector.Handle(context.Background(), eventbus.NewGovernanceDeliveryDeadLetteredEvent(
		"run-governance",
		"sess-governance",
		eventbus.GovernanceDeliveryAttrs{
			DeliveryID:          "delivery-1",
			AdapterName:         "slack-audit",
			DeliveryStatus:      "dead_lettered",
			DeliveryAttempts:    4,
			DeliveryMaxAttempts: 4,
			GovernanceKind:      "audit",
			SourceEventType:     "run.failed",
			NextAttemptAt:       now.Add(2 * time.Minute),
			Error:               "destination offline",
		},
		nil,
	)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	warning, ok := warningByComponent(collector.OperationalWarnings(), "governance/delivery")
	if !ok {
		t.Fatalf("OperationalWarnings() missing governance warning: %#v", collector.OperationalWarnings())
	}
	if warning.Summary != "Governance delivery recently dead-lettered events" {
		t.Fatalf("Summary = %q", warning.Summary)
	}
	if !containsAll(warning.Detail, "1 recent governance delivery issue(s)", "adapter=slack-audit", "delivery=delivery-1", "status=dead_lettered", "attempts=4/4", "run=run-governance", "kind=audit", "source=run.failed", "error=destination offline") {
		t.Fatalf("Detail = %q", warning.Detail)
	}
}

func TestRuntimeEventWarningCollectorProjectsRunFailureAndModelRetry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 14, 4, 10, 0, 0, time.UTC)
	collector := newRuntimeEventWarningCollector(10 * time.Minute)
	collector.now = func() time.Time { return now }

	if err := collector.Handle(context.Background(), eventbus.NewRunFailedEvent(
		"run-failed",
		"sess-failed",
		eventbus.RunStatusAttrs{
			Summary: "tool invocation failed",
			Error:   "subprocess exited 1",
		},
		nil,
	)); err != nil {
		t.Fatalf("Handle(run failed) error = %v", err)
	}
	if err := collector.Handle(context.Background(), eventbus.NewModelRetryEvent(
		"run-failed",
		"sess-failed",
		eventbus.ModelRetryAttrs{
			Model:         "gpt-5",
			Attempt:       2,
			MaxAttempts:   4,
			FailureReason: "rate_limit",
			Error:         "429 quota exceeded",
		},
		nil,
	)); err != nil {
		t.Fatalf("Handle(model retry) error = %v", err)
	}

	runWarning, ok := warningByComponent(collector.OperationalWarnings(), "runtime/run_failures")
	if !ok {
		t.Fatalf("OperationalWarnings() missing run failure warning: %#v", collector.OperationalWarnings())
	}
	if !containsAll(runWarning.Detail, "1 recent run failure(s)", "run=run-failed", "summary=tool invocation failed", "error=subprocess exited 1") {
		t.Fatalf("Detail = %q", runWarning.Detail)
	}

	modelWarning, ok := warningByComponent(collector.OperationalWarnings(), "model/gpt-5/retry")
	if !ok {
		t.Fatalf("OperationalWarnings() missing model retry warning: %#v", collector.OperationalWarnings())
	}
	if !containsAll(modelWarning.Detail, "1 recent retry event(s)", "run=run-failed", "attempt=2/4", "reason=rate_limit", "error=429 quota exceeded") {
		t.Fatalf("Detail = %q", modelWarning.Detail)
	}
}

func TestPrepareBootstrapFoundationSubscribesRuntimeWarnings(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent: config.AgentConfig{
			DefaultModel:  "test-model",
			MaxToolRounds: 2,
			QueueMode:     "enqueue",
		},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{
				Enabled:            boolPtr(false),
				Root:               ".",
				DefaultExecTimeout: 30 * time.Second,
				MaxReadBytes:       64 * 1024,
			},
			LocalExec: config.LocalExecConfig{
				Enabled:        boolPtr(false),
				DefaultTimeout: 30 * time.Second,
			},
		},
	}
	cfg.ApplyDefaults()

	foundation, err := prepareBootstrapFoundation(context.Background(), cfg)
	if err != nil {
		t.Fatalf("prepareBootstrapFoundation() error = %v", err)
	}

	if err := foundation.bus.Publish(context.Background(), eventbus.NewApprovalTimedOutEvent(
		"run-foundation",
		"sess-foundation",
		eventbus.ApprovalEventAttrs{
			ApprovalID:    "approval-foundation",
			Status:        "timed_out",
			PolicySummary: "ops approval",
		},
		nil,
	)); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	warning, ok := warningByComponent(foundation.operationalWarnings.OperationalWarnings(), "approval_timeout/recent")
	if !ok {
		t.Fatalf("OperationalWarnings() missing runtime warning: %#v", foundation.operationalWarnings.OperationalWarnings())
	}
	if !containsAll(warning.Detail, "approval=approval-foundation", "run=run-foundation") {
		t.Fatalf("Detail = %q", warning.Detail)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func warningByComponent(warnings []controlplane.OperationalWarning, component string) (controlplane.OperationalWarning, bool) {
	for _, warning := range warnings {
		if warning.Component == component {
			return warning, true
		}
	}
	return controlplane.OperationalWarning{}, false
}
