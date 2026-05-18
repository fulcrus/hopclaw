package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/automation"
	"github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
)

func TestAutomationSearchAndStats(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})

	cronStorePath := filepath.Join(t.TempDir(), "cron.json")
	if err := os.WriteFile(cronStorePath, []byte(`{"version":1,"jobs":[]}`), 0o644); err != nil {
		t.Fatalf("write cron store: %v", err)
	}
	cronStore, err := cron.Load(cronStorePath)
	if err != nil {
		t.Fatalf("cron.Load() error = %v", err)
	}
	if err := cronStore.Add(cron.Job{
		ID:      "cron-beijing",
		Name:    "Beijing weather briefing",
		Enabled: true,
		Schedule: cron.Schedule{
			Kind:       cron.ScheduleKindCron,
			Expression: "0 8 * * *",
		},
		Payload:  cron.Payload{Content: "Collect Beijing weather and headlines"},
		Delivery: &cron.Delivery{Channel: "feishu", Target: "chat-beijing"},
		Notifications: automation.NotificationStats{
			TotalCount:      5,
			FailureCount:    1,
			TodayCount:      2,
			TodayDate:       time.Now().UTC().Format("2006-01-02"),
			LastStatus:      "delivered",
			LastAttemptAt:   time.Now().UTC(),
			LastDeliveredAt: time.Now().UTC(),
		},
	}); err != nil {
		t.Fatalf("cronStore.Add() error = %v", err)
	}
	bindings := BuiltinsBindings{
		CronService: cron.NewService(cronStore, nil, nil),
	}

	wakeupStorePath := filepath.Join(t.TempDir(), "wakeup.json")
	if err := os.WriteFile(wakeupStorePath, []byte(`{"version":1,"triggers":[]}`), 0o644); err != nil {
		t.Fatalf("write wakeup store: %v", err)
	}
	wakeupStore, err := wakeup.Load(wakeupStorePath)
	if err != nil {
		t.Fatalf("wakeup.Load() error = %v", err)
	}
	if err := wakeupStore.Add(wakeup.Trigger{
		ID:        "wake-sales",
		Name:      "Sales wakeup",
		Enabled:   true,
		Schedule:  "0 9 * * *",
		Message:   "Ping sales",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("wakeupStore.Add() error = %v", err)
	}
	bindings.WakeupService = wakeup.NewService(wakeupStore, nil)

	watchStorePath := filepath.Join(t.TempDir(), "watch.json")
	if err := os.WriteFile(watchStorePath, []byte(`{"version":1,"watches":[]}`), 0o644); err != nil {
		t.Fatalf("write watch store: %v", err)
	}
	watchStore, err := watch.Load(watchStorePath)
	if err != nil {
		t.Fatalf("watch.Load() error = %v", err)
	}
	if err := watchStore.Add(watch.Watch{
		ID:       "watch-foreign",
		Name:     "International news watch",
		Enabled:  true,
		Interval: "5m",
		Source: watch.Source{
			Kind: watch.SourceKindHTTP,
			HTTP: &watch.HTTPSource{URL: "https://example.com/international"},
		},
		Delivery: &automation.DeliveryTarget{Channel: "feishu", Target: "chat-ops"},
		Prompt:   "Only alert on major events",
		Notifications: automation.NotificationStats{
			TotalCount:   7,
			FailureCount: 0,
			TodayCount:   3,
			TodayDate:    time.Now().UTC().Format("2006-01-02"),
			LastStatus:   "delivered",
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("watchStore.Add() error = %v", err)
	}
	bindings.WatchService = watch.NewService(watchStore, nil)
	builtins.ApplyBindings(bindings)

	ctx := context.Background()
	run := &agent.Run{ID: "run-automation"}
	sess := &agent.Session{ID: "sess-automation"}

	exec := func(name string, input map[string]any, target any) {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		if err := json.Unmarshal([]byte(results[0].Content), target); err != nil {
			t.Fatalf("%s unmarshal: %v", name, err)
		}
	}

	var searchResp struct {
		Items []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"items"`
		Count int `json:"count"`
	}
	exec("automation.search", map[string]any{
		"query": "beijing",
	}, &searchResp)
	if searchResp.Count != 1 {
		t.Fatalf("automation.search count = %d, want 1", searchResp.Count)
	}
	if searchResp.Items[0].ID != "cron-beijing" || searchResp.Items[0].Kind != "cron" {
		t.Fatalf("automation.search item = %+v", searchResp.Items[0])
	}

	var statsResp struct {
		TotalCount               int `json:"total_count"`
		EnabledCount             int `json:"enabled_count"`
		CronCount                int `json:"cron_count"`
		WakeupCount              int `json:"wakeup_count"`
		WatchCount               int `json:"watch_count"`
		NotificationTodayCount   int `json:"notification_today_count"`
		NotificationFailureCount int `json:"notification_failure_count"`
	}
	exec("automation.stats", map[string]any{}, &statsResp)
	if statsResp.TotalCount != 3 {
		t.Fatalf("automation.stats total_count = %d, want 3", statsResp.TotalCount)
	}
	if statsResp.EnabledCount != 3 || statsResp.CronCount != 1 || statsResp.WakeupCount != 1 || statsResp.WatchCount != 1 {
		t.Fatalf("automation.stats kinds = %+v", statsResp)
	}
	if statsResp.NotificationTodayCount != 5 || statsResp.NotificationFailureCount != 1 {
		t.Fatalf("automation.stats notifications = %+v", statsResp)
	}
}
