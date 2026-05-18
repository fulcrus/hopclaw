package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	autopkg "github.com/fulcrus/hopclaw/automation"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/hooks"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/server"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
)

func newTestAutomationGateway(t *testing.T) *Gateway {
	t.Helper()

	gw := newTestGatewayFull(t)

	cronPath := filepath.Join(t.TempDir(), "cron.json")
	if err := os.WriteFile(cronPath, []byte(`{"version":1,"jobs":[]}`), 0o644); err != nil {
		t.Fatalf("write cron file: %v", err)
	}
	cronStore, err := cron.Load(cronPath)
	if err != nil {
		t.Fatalf("cron.Load() error = %v", err)
	}
	gw.SetCron(cron.NewService(cronStore, nil, nil))

	watchPath := filepath.Join(t.TempDir(), "watch.json")
	if err := os.WriteFile(watchPath, []byte(`{"version":1,"watches":[]}`), 0o644); err != nil {
		t.Fatalf("write watch file: %v", err)
	}
	watchStore, err := watch.Load(watchPath)
	if err != nil {
		t.Fatalf("watch.Load() error = %v", err)
	}
	gw.SetWatch(watch.NewService(watchStore, nil))

	wakeupStore, err := wakeup.Load(filepath.Join(t.TempDir(), "wakeup.json"))
	if err != nil {
		t.Fatalf("wakeup.Load() error = %v", err)
	}
	gw.SetWakeup(wakeup.NewService(wakeupStore, func(_ context.Context, _ wakeup.Trigger) (*wakeup.ExecutionResult, error) { return nil, nil }))
	gw.SetHooks(hooks.NewExecutor(hooks.NewInMemoryStore()))
	return gw
}

func newTestAutomationGatewayWithRuntime(t *testing.T) (*Gateway, *agent.InMemorySessionStore, *agent.InMemoryRunStore) {
	t.Helper()

	runs := agent.NewInMemoryRunStore()
	sessions := agent.NewInMemorySessionStore()
	bus := eventbus.NewInMemoryBus()
	runtimeService := runtimepkg.NewService(nil, sessions, runs, nil, bus, nil)
	srv := server.New(runtimeService, server.Config{AuthToken: "test-token"})
	gw := gatewayFromServer(srv, Config{
		AuthToken: "test-token",
		Runtime:   runtimeService,
	})

	cronPath := filepath.Join(t.TempDir(), "cron.json")
	if err := os.WriteFile(cronPath, []byte(`{"version":1,"jobs":[]}`), 0o644); err != nil {
		t.Fatalf("write cron file: %v", err)
	}
	cronStore, err := cron.Load(cronPath)
	if err != nil {
		t.Fatalf("cron.Load() error = %v", err)
	}
	gw.SetCron(cron.NewService(cronStore, nil, nil))

	watchPath := filepath.Join(t.TempDir(), "watch.json")
	if err := os.WriteFile(watchPath, []byte(`{"version":1,"watches":[]}`), 0o644); err != nil {
		t.Fatalf("write watch file: %v", err)
	}
	watchStore, err := watch.Load(watchPath)
	if err != nil {
		t.Fatalf("watch.Load() error = %v", err)
	}
	gw.SetWatch(watch.NewService(watchStore, nil))

	wakeupStore, err := wakeup.Load(filepath.Join(t.TempDir(), "wakeup.json"))
	if err != nil {
		t.Fatalf("wakeup.Load() error = %v", err)
	}
	gw.SetWakeup(wakeup.NewService(wakeupStore, func(_ context.Context, _ wakeup.Trigger) (*wakeup.ExecutionResult, error) { return nil, nil }))
	gw.SetHooks(hooks.NewExecutor(hooks.NewInMemoryStore()))

	return gw, sessions, runs
}

func TestAutomationItemsEmpty(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/items", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationItemsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
	if payload.Services[string(autopkg.KindCron)].Available {
		t.Fatal("expected cron unavailable")
	}
}

func TestAutomationItemsApplyAuthenticatedScope(t *testing.T) {
	t.Parallel()

	gw := newTestAutomationGateway(t)
	now := time.Now().UTC()

	if err := gw.cron.Store().Add(cron.Job{
		ID:        "cron-a",
		Name:      "tenant a cron",
		Enabled:   true,
		Schedule:  cron.Schedule{Kind: cron.ScheduleKindEvery, Every: "1h"},
		Payload:   cron.Payload{Content: "tenant a"},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("cron add a: %v", err)
	}
	if err := gw.cron.Store().Add(cron.Job{
		ID:        "cron-b",
		Name:      "tenant b cron",
		Enabled:   true,
		Schedule:  cron.Schedule{Kind: cron.ScheduleKindEvery, Every: "1h"},
		Payload:   cron.Payload{Content: "tenant b"},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("cron add b: %v", err)
	}

	if _, err := gw.hooks.Store().Add(context.Background(), hooks.Hook{
		Name:    "hook-a",
		Enabled: true,
		Trigger: hooks.TriggerRunCompleted,
		Kind:    hooks.KindCommand,
		Command: "true",
	}); err != nil {
		t.Fatalf("hook add a: %v", err)
	}
	if _, err := gw.hooks.Store().Add(context.Background(), hooks.Hook{
		Name:    "hook-b",
		Enabled: true,
		Trigger: hooks.TriggerRunCompleted,
		Kind:    hooks.KindCommand,
		Command: "true",
	}); err != nil {
		t.Fatalf("hook add b: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/operator/automation/items?kinds=cron,hook", nil).
		WithContext(scopedAuthContext("actor-a"))
	rec := httptest.NewRecorder()
	http.HandlerFunc(gw.handleAutomationItems).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationItemsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 4 {
		t.Fatalf("count = %d, want 4", payload.Count)
	}
	if payload.Services[string(autopkg.KindCron)].Count != 2 {
		t.Fatalf("cron service count = %d, want 2", payload.Services[string(autopkg.KindCron)].Count)
	}
	if payload.Services[string(autopkg.KindHook)].Count != 2 {
		t.Fatalf("hook service count = %d, want 2", payload.Services[string(autopkg.KindHook)].Count)
	}
}

func TestAutomationTemplates(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/templates", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationTemplatesResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count == 0 || len(payload.Items) == 0 {
		t.Fatal("expected starter templates")
	}
	foundKinds := map[autopkg.TemplateKind]bool{}
	for _, item := range payload.Items {
		foundKinds[item.Kind] = true
		if item.ID == "" || item.Name == "" {
			t.Fatalf("template missing identity: %+v", item)
		}
	}
	for _, kind := range []autopkg.TemplateKind{
		autopkg.TemplateKindCron,
		autopkg.TemplateKindWakeup,
		autopkg.TemplateKindWatch,
		autopkg.TemplateKindHook,
	} {
		if !foundKinds[kind] {
			t.Fatalf("expected template kind %q", kind)
		}
	}
}

func TestAutomationTemplatesFilterByKind(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/templates?kind=watch", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationTemplatesResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count == 0 {
		t.Fatal("expected watch templates")
	}
	for _, item := range payload.Items {
		if item.Kind != autopkg.TemplateKindWatch {
			t.Fatalf("template kind = %q, want %q", item.Kind, autopkg.TemplateKindWatch)
		}
	}
}

func TestAutomationTemplatesIncludeScenarioStarters(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/templates", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationTemplatesResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	templatesByID := make(map[string]autopkg.StarterTemplate, len(payload.Items))
	for _, item := range payload.Items {
		templatesByID[item.ID] = item
	}

	cases := []struct {
		id          string
		kind        autopkg.TemplateKind
		field       string
		channel     string
		watchSource string
	}{
		{
			id:      "daily-news-weather-feishu",
			kind:    autopkg.TemplateKindCron,
			field:   "target",
			channel: "feishu",
		},
		{
			id:          "international-breaking-news-watch",
			kind:        autopkg.TemplateKindWatch,
			field:       "delivery_target",
			channel:     "feishu",
			watchSource: "http",
		},
		{
			id:      "browser-release-smoke-check",
			kind:    autopkg.TemplateKindCron,
			field:   "target",
			channel: "feishu",
		},
		{
			id:          "ecommerce-site-anomaly-watch",
			kind:        autopkg.TemplateKindWatch,
			field:       "delivery_target",
			channel:     "feishu",
			watchSource: "browser_snapshot",
		},
	}

	for _, check := range cases {
		item, ok := templatesByID[check.id]
		if !ok {
			t.Fatalf("missing scenario template %q", check.id)
		}
		if item.Kind != check.kind {
			t.Fatalf("template %q kind = %q, want %q", check.id, item.Kind, check.kind)
		}
		if len(item.RequiredFields) == 0 {
			t.Fatalf("template %q required_fields is empty", check.id)
		}
		foundField := false
		for _, field := range item.RequiredFields {
			if field.Field == check.field {
				foundField = true
				break
			}
		}
		if !foundField {
			t.Fatalf("template %q missing required field %q", check.id, check.field)
		}
		if item.CronDefaults != nil {
			if item.CronDefaults.Channel != check.channel {
				t.Fatalf("template %q cron channel = %q, want %q", check.id, item.CronDefaults.Channel, check.channel)
			}
		}
		if item.WatchDefaults != nil {
			if item.WatchDefaults.DeliveryChannel != check.channel {
				t.Fatalf("template %q delivery channel = %q, want %q", check.id, item.WatchDefaults.DeliveryChannel, check.channel)
			}
			if check.watchSource != "" && item.WatchDefaults.SourceKind != check.watchSource {
				t.Fatalf("template %q source_kind = %q, want %q", check.id, item.WatchDefaults.SourceKind, check.watchSource)
			}
		}
	}
}

func TestAutomationTemplatesIncludeGovernanceHookStarters(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/templates?kind=hook", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationTemplatesResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	templatesByID := make(map[string]autopkg.StarterTemplate, len(payload.Items))
	for _, item := range payload.Items {
		templatesByID[item.ID] = item
	}

	checks := []struct {
		id       string
		trigger  hooks.TriggerEvent
		kind     string
		required string
		filter   string
	}{
		{
			id:       "governance-dead-letter-webhook",
			trigger:  hooks.TriggerGovernanceDeliveryDeadLettered,
			kind:     "http",
			required: "url",
		},
		{
			id:       "governance-retry-escalation-hook",
			trigger:  hooks.TriggerGovernanceDeliveryRetryScheduled,
			kind:     "command",
			required: "command",
			filter:   "delivery_attempts >= 3",
		},
		{
			id:       "governance-redrive-audit-webhook",
			trigger:  hooks.TriggerGovernanceDeliveryRedriven,
			kind:     "http",
			required: "url",
		},
	}

	for _, check := range checks {
		item, ok := templatesByID[check.id]
		if !ok {
			t.Fatalf("missing governance template %q", check.id)
		}
		if item.Kind != autopkg.TemplateKindHook {
			t.Fatalf("template %q kind = %q, want %q", check.id, item.Kind, autopkg.TemplateKindHook)
		}
		if item.Category != "governance" {
			t.Fatalf("template %q category = %q, want governance", check.id, item.Category)
		}
		if item.HookDefaults == nil {
			t.Fatalf("template %q missing hook defaults", check.id)
		}
		if got := item.HookDefaults.Trigger; got != string(check.trigger) {
			t.Fatalf("template %q trigger = %q, want %q", check.id, got, check.trigger)
		}
		if got := item.HookDefaults.Kind; got != check.kind {
			t.Fatalf("template %q hook kind = %q, want %q", check.id, got, check.kind)
		}
		if got := item.HookDefaults.Filter; got != check.filter {
			t.Fatalf("template %q filter = %q, want %q", check.id, got, check.filter)
		}
		if len(item.RequiredFields) == 0 || item.RequiredFields[0].Field != check.required {
			t.Fatalf("template %q required field = %+v, want %q", check.id, item.RequiredFields, check.required)
		}
	}
}

func TestAutomationTemplatesIncludeEnterpriseHookStarters(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/templates?kind=hook", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationTemplatesResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	templatesByID := make(map[string]autopkg.StarterTemplate, len(payload.Items))
	for _, item := range payload.Items {
		templatesByID[item.ID] = item
	}

	checks := []struct {
		id       string
		trigger  hooks.TriggerEvent
		kind     string
		required string
		category string
	}{
		{
			id:       "slack-governance-dead-letter-command",
			trigger:  hooks.TriggerGovernanceDeliveryDeadLettered,
			kind:     "command",
			required: "command",
			category: "chatops",
		},
		{
			id:       "feishu-governance-dead-letter-command",
			trigger:  hooks.TriggerGovernanceDeliveryDeadLettered,
			kind:     "command",
			required: "command",
			category: "chatops",
		},
		{
			id:       "email-governance-dead-letter-command",
			trigger:  hooks.TriggerGovernanceDeliveryDeadLettered,
			kind:     "command",
			required: "command",
			category: "notification",
		},
		{
			id:       "ticket-governance-dead-letter-command",
			trigger:  hooks.TriggerGovernanceDeliveryDeadLettered,
			kind:     "command",
			required: "command",
			category: "itsm",
		},
		{
			id:       "approval-resolved-callback-webhook",
			trigger:  hooks.TriggerApprovalResolved,
			kind:     "http",
			required: "url",
			category: "approval",
		},
	}

	for _, check := range checks {
		item, ok := templatesByID[check.id]
		if !ok {
			t.Fatalf("missing enterprise template %q", check.id)
		}
		if item.Kind != autopkg.TemplateKindHook {
			t.Fatalf("template %q kind = %q, want %q", check.id, item.Kind, autopkg.TemplateKindHook)
		}
		if item.Category != check.category {
			t.Fatalf("template %q category = %q, want %q", check.id, item.Category, check.category)
		}
		if item.HookDefaults == nil {
			t.Fatalf("template %q missing hook defaults", check.id)
		}
		if got := item.HookDefaults.Trigger; got != string(check.trigger) {
			t.Fatalf("template %q trigger = %q, want %q", check.id, got, check.trigger)
		}
		if got := item.HookDefaults.Kind; got != check.kind {
			t.Fatalf("template %q hook kind = %q, want %q", check.id, got, check.kind)
		}
		if len(item.RequiredFields) == 0 || item.RequiredFields[0].Field != check.required {
			t.Fatalf("template %q required field = %+v, want %q", check.id, item.RequiredFields, check.required)
		}
	}
}

func TestAutomationItemsMergedKinds(t *testing.T) {
	t.Parallel()

	gw := newTestAutomationGateway(t)
	now := time.Now().UTC()

	if err := gw.cron.Store().Add(cron.Job{
		ID:                      "cron-1",
		Name:                    "daily report",
		Enabled:                 true,
		Schedule:                cron.Schedule{Kind: cron.ScheduleKindCron, Expression: "0 9 * * *"},
		Payload:                 cron.Payload{Content: "generate report"},
		SessionKey:              "ops:daily",
		Model:                   "gpt-4.1",
		LastRunAt:               now,
		LastRunID:               "run-cron-1",
		LastStatus:              cron.RunStatusOK,
		LastSummary:             "report delivered",
		LastVerificationStatus:  "passed",
		LastVerificationSummary: "checks passed",
		Notifications:           cron.NotificationStats{TotalCount: 3, TodayCount: 2, TodayDate: now.Format("2006-01-02"), LastStatus: "delivered", LastDeliveredAt: now},
		NextRunAt:               now.Add(time.Hour),
		CreatedAt:               now,
		UpdatedAt:               now,
	}); err != nil {
		t.Fatalf("cron add: %v", err)
	}
	if err := gw.cron.Store().Save(); err != nil {
		t.Fatalf("cron save: %v", err)
	}

	if err := gw.watch.Store().Add(watch.Watch{
		ID:                      "watch-1",
		Name:                    "homepage watch",
		Enabled:                 true,
		Interval:                "5m",
		Source:                  watch.Source{Kind: watch.SourceKindHTTP, HTTP: &watch.HTTPSource{URL: "https://example.com"}},
		Prompt:                  "summarize changes",
		SessionKey:              "watch:homepage",
		LastCheckedAt:           now,
		LastTriggeredAt:         now,
		LastRunID:               "run-watch-1",
		LastStatus:              watch.RunStatusTriggered,
		LastSummary:             "homepage changed",
		LastVerificationStatus:  "warning",
		LastVerificationSummary: "minor diff",
		Notifications:           watch.NotificationStats{TotalCount: 5, FailureCount: 1, TodayCount: 4, TodayDate: now.Format("2006-01-02"), LastStatus: "delivered", LastDeliveredAt: now},
		NextCheckAt:             now.Add(5 * time.Minute),
		CreatedAt:               now,
		UpdatedAt:               now,
	}); err != nil {
		t.Fatalf("watch add: %v", err)
	}
	if err := gw.watch.Store().Save(); err != nil {
		t.Fatalf("watch save: %v", err)
	}

	if err := gw.wakeup.Add(wakeup.Trigger{
		ID:         "wakeup-1",
		Name:       "morning brief",
		Schedule:   "0 9 * * *",
		SessionKey: "brief:morning",
		Message:    "send the morning brief",
		Enabled:    true,
		CreatedAt:  now,
		LastRunAt:  now,
		LastStatus: "triggered",
		NextRunAt:  now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("wakeup add: %v", err)
	}

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/items", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationItemsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 3 {
		t.Fatalf("count = %d, want 3", payload.Count)
	}
	if payload.Notifications.TotalCount != 8 || payload.Notifications.FailureCount != 1 || payload.Notifications.TodayCount != 6 {
		t.Fatalf("notifications = %+v", payload.Notifications)
	}
	if !payload.Services["cron"].Available || !payload.Services["watch"].Available || !payload.Services["wakeup"].Available {
		t.Fatal("expected all services available")
	}

	foundCron := false
	foundWatch := false
	foundWakeup := false
	for _, item := range payload.Items {
		switch item.Kind {
		case "cron":
			foundCron = true
			if item.LastExecution == nil || item.LastExecution.RunID != "run-cron-1" {
				t.Fatalf("cron last execution = %+v", item.LastExecution)
			}
			if item.Notifications == nil || item.Notifications.TotalCount != 3 || item.Notifications.TodayCount != 2 {
				t.Fatalf("cron notifications = %+v", item.Notifications)
			}
		case "watch":
			foundWatch = true
			if item.SourceLabel != "https://example.com" {
				t.Fatalf("watch source_label = %q", item.SourceLabel)
			}
			if item.Notifications == nil || item.Notifications.TotalCount != 5 || item.Notifications.FailureCount != 1 {
				t.Fatalf("watch notifications = %+v", item.Notifications)
			}
		case "wakeup":
			foundWakeup = true
			if item.SessionKey != "brief:morning" {
				t.Fatalf("wakeup session_key = %q", item.SessionKey)
			}
		}
	}
	if !foundCron || !foundWatch || !foundWakeup {
		t.Fatalf("missing items: cron=%v watch=%v wakeup=%v", foundCron, foundWatch, foundWakeup)
	}
}

func TestAutomationItemsKindsFilter(t *testing.T) {
	t.Parallel()

	gw := newTestAutomationGateway(t)
	if err := gw.wakeup.Add(wakeup.Trigger{
		ID:        "wakeup-1",
		Name:      "brief",
		Schedule:  "0 9 * * *",
		Message:   "hello",
		Enabled:   true,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("wakeup add: %v", err)
	}

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/items?kinds=wakeup", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload automationItemsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 || payload.Items[0].Kind != "wakeup" {
		t.Fatalf("items = %+v", payload.Items)
	}
}

func TestAutomationItemDetailIncludesLatestResult(t *testing.T) {
	t.Parallel()

	gw, sessions, runs := newTestAutomationGatewayWithRuntime(t)
	now := time.Now().UTC()
	ctx := context.Background()

	session, err := sessions.GetOrCreate(ctx, "ops:daily", "test-model", "sess-1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionID:  session.ID,
		SessionKey: session.Key,
		Content:    "generate report",
		Model:      "test-model",
	}, agent.AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	run.Status = agent.RunCompleted
	run.StartedAt = now
	run.UpdatedAt = now
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "generate report",
			CreatedAt: now,
			Metadata:  map[string]any{meta.KeyRunID: run.ID},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "report ready",
			CreatedAt: now,
			Metadata:  map[string]any{meta.KeyRunID: run.ID},
		},
	)
	session.UpdatedAt = now
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("sessions.Save() error = %v", err)
	}

	if err := gw.cron.Store().Add(cron.Job{
		ID:                      "cron-1",
		Name:                    "daily report",
		Enabled:                 true,
		Schedule:                cron.Schedule{Kind: cron.ScheduleKindCron, Expression: "0 9 * * *"},
		Payload:                 cron.Payload{Content: "generate report"},
		SessionKey:              "ops:daily",
		LastRunAt:               now,
		LastRunID:               run.ID,
		LastStatus:              cron.RunStatusOK,
		LastSummary:             "report ready",
		LastVerificationStatus:  "passed",
		LastVerificationSummary: "verification passed",
		NextRunAt:               now.Add(time.Hour),
		CreatedAt:               now,
		UpdatedAt:               now,
	}); err != nil {
		t.Fatalf("cron add: %v", err)
	}
	if err := gw.cron.Store().Save(); err != nil {
		t.Fatalf("cron save: %v", err)
	}

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/items/cron/cron-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationItemDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Item.Kind != autopkg.KindCron {
		t.Fatalf("kind = %q, want %q", payload.Item.Kind, autopkg.KindCron)
	}
	if len(payload.RecentExecutions) != 1 {
		t.Fatalf("recent_executions len = %d, want 1", len(payload.RecentExecutions))
	}
	if payload.LatestResult == nil {
		t.Fatal("expected latest_result")
	}
	if payload.LatestResult.RunID != run.ID {
		t.Fatalf("latest_result.run_id = %q, want %q", payload.LatestResult.RunID, run.ID)
	}
	if payload.LatestResult.Output != "report ready" {
		t.Fatalf("latest_result.output = %q", payload.LatestResult.Output)
	}
	if payload.RunPath == "" {
		t.Fatal("expected run_path")
	}
}

func TestAutomationItemDetailRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	gw := newTestAutomationGateway(t)
	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/items/unknown/example", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAutomationItemDetailAcceptsAuthContextWithoutScopeFiltering(t *testing.T) {
	t.Parallel()

	gw := newTestAutomationGateway(t)
	now := time.Now().UTC()
	if err := gw.cron.Store().Add(cron.Job{
		ID:        "cron-b",
		Name:      "beta cron",
		Enabled:   true,
		Schedule:  cron.Schedule{Kind: cron.ScheduleKindEvery, Every: "1h"},
		Payload:   cron.Payload{Content: "beta"},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("cron add b: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/operator/automation/items/cron/cron-b", nil).
		WithContext(scopedAuthContext("actor-a"))
	req.SetPathValue("kind", "cron")
	req.SetPathValue("id", "cron-b")

	rec := httptest.NewRecorder()
	http.HandlerFunc(gw.handleAutomationItemDetail).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAutomationItemsIncludeHooks(t *testing.T) {
	t.Parallel()

	gw := newTestAutomationGateway(t)
	ctx := context.Background()
	created, err := gw.hooks.Store().Add(ctx, hooks.Hook{
		Name:    "notify result",
		Enabled: true,
		Trigger: hooks.TriggerRunCompleted,
		Kind:    hooks.KindCommand,
		Command: "echo hook-fired",
	})
	if err != nil {
		t.Fatalf("hook add: %v", err)
	}
	gw.hooks.Fire(ctx, hooks.TriggerRunCompleted, hooks.HookPhasePost, map[string]any{"run_id": "run-hook-1"})

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/items?kinds=hook", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationItemsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
	if payload.Items[0].Kind != autopkg.KindHook {
		t.Fatalf("kind = %q, want %q", payload.Items[0].Kind, autopkg.KindHook)
	}
	if payload.Items[0].ID != created.ID {
		t.Fatalf("id = %q, want %q", payload.Items[0].ID, created.ID)
	}
	if payload.Items[0].LastExecution == nil {
		t.Fatal("expected hook last execution")
	}
}

func TestAutomationHookDetailIncludesRecentExecutions(t *testing.T) {
	t.Parallel()

	gw := newTestAutomationGateway(t)
	ctx := context.Background()
	created, err := gw.hooks.Store().Add(ctx, hooks.Hook{
		Name:       "notify result",
		Enabled:    true,
		Trigger:    hooks.TriggerRunCompleted,
		Kind:       hooks.KindCommand,
		Command:    "echo hook-fired",
		RetryCount: 1,
	})
	if err != nil {
		t.Fatalf("hook add: %v", err)
	}
	gw.hooks.Fire(ctx, hooks.TriggerRunCompleted, hooks.HookPhasePost, map[string]any{"run_id": "run-hook-1"})

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/items/hook/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationItemDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Item.Kind != autopkg.KindHook {
		t.Fatalf("kind = %q, want %q", payload.Item.Kind, autopkg.KindHook)
	}
	if payload.Item.SourceKind != string(hooks.KindCommand) {
		t.Fatalf("source_kind = %q", payload.Item.SourceKind)
	}
	if len(payload.RecentExecutions) == 0 {
		t.Fatal("expected recent executions")
	}
	if payload.LatestResult != nil {
		t.Fatal("expected latest_result to be nil for hook detail without runtime receipt")
	}
}

func TestAutomationHookDetailIncludesLatestResultFromRun(t *testing.T) {
	t.Parallel()

	gw, sessions, runs := newTestAutomationGatewayWithRuntime(t)
	now := time.Now().UTC()
	ctx := context.Background()

	session, err := sessions.GetOrCreate(ctx, "hooks:test", "test-model", "sess-hook-1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionID:  session.ID,
		SessionKey: session.Key,
		Content:    "trigger hook",
		Model:      "test-model",
	}, agent.AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	run.Status = agent.RunCompleted
	run.StartedAt = now
	run.UpdatedAt = now
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "trigger hook",
			CreatedAt: now,
			Metadata:  map[string]any{meta.KeyRunID: run.ID},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "hook-backed run ready",
			CreatedAt: now,
			Metadata:  map[string]any{meta.KeyRunID: run.ID},
		},
	)
	session.UpdatedAt = now
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("sessions.Save() error = %v", err)
	}

	hook, err := gw.hooks.Store().Add(ctx, hooks.Hook{
		Name:    "result-hook",
		Enabled: true,
		Trigger: hooks.TriggerRunCompleted,
		Kind:    hooks.KindCommand,
		Command: "echo hook-fired",
	})
	if err != nil {
		t.Fatalf("hook add: %v", err)
	}

	results := gw.hooks.Fire(ctx, hooks.TriggerRunCompleted, hooks.HookPhasePost, map[string]any{
		"run_id":     run.ID,
		"session_id": session.ID,
		"tool_name":  "notify.send",
	})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/items/hook/"+hook.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationItemDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Item.LastExecution == nil {
		t.Fatal("expected last execution")
	}
	if payload.Item.LastExecution.RunID != run.ID {
		t.Fatalf("run_id = %q, want %q", payload.Item.LastExecution.RunID, run.ID)
	}
	if payload.Item.LastExecution.ToolName != "notify.send" {
		t.Fatalf("tool_name = %q, want notify.send", payload.Item.LastExecution.ToolName)
	}
	if payload.LatestResult == nil {
		t.Fatal("expected latest_result")
	}
	if payload.RunPath == "" {
		t.Fatal("expected run_path")
	}
}

func TestAutomationHookDetailIncludesErrorSignaturesAndReplayState(t *testing.T) {
	t.Parallel()

	gw := newTestAutomationGateway(t)
	ctx := context.Background()
	hook, err := gw.hooks.Store().Add(ctx, hooks.Hook{
		Name:    "failing-hook",
		Enabled: true,
		Trigger: hooks.TriggerRunCompleted,
		Kind:    hooks.KindCommand,
		Command: "exit 7",
	})
	if err != nil {
		t.Fatalf("hook add: %v", err)
	}

	for range 2 {
		results := gw.hooks.Fire(ctx, hooks.TriggerRunCompleted, hooks.HookPhasePost, map[string]any{
			"run_id":     "run-fail",
			"session_id": "sess-fail",
			"tool_name":  "notify.send",
		})
		if len(results) != 1 {
			t.Fatalf("results len = %d, want 1", len(results))
		}
	}

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/automation/items/hook/"+hook.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload automationItemDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !payload.CanReplay {
		t.Fatal("expected can_replay")
	}
	if len(payload.ErrorSignatures) == 0 {
		t.Fatal("expected error signatures")
	}
	if payload.ErrorSignatures[0].Count != 2 {
		t.Fatalf("count = %d, want 2", payload.ErrorSignatures[0].Count)
	}
	if payload.LatestPayloadPreview == nil {
		t.Fatal("expected latest payload preview")
	}
}
