package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/watch"
)

func TestWatchToolsLifecycle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	storePath := filepath.Join(t.TempDir(), "watch.json")
	if err := os.WriteFile(storePath, []byte(`{"version":1,"watches":[]}`), 0o644); err != nil {
		t.Fatalf("write watch store: %v", err)
	}
	store, err := watch.Load(storePath)
	if err != nil {
		t.Fatalf("watch.Load() error = %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{WatchService: watch.NewService(store, nil)})

	ctx := context.Background()
	run := &agent.Run{ID: "run-watch"}
	sess := &agent.Session{ID: "sess-watch"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	var addResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(exec("watch.add", map[string]any{
		"name":             "news-watch",
		"interval":         "5m",
		"source_kind":      "http",
		"source_url":       "https://example.com/news",
		"delivery_channel": "feishu",
		"delivery_target":  "oc_alerts",
		"prompt":           "Summarize changes",
	})), &addResp); err != nil {
		t.Fatalf("watch.add unmarshal: %v", err)
	}
	if addResp.ID == "" {
		t.Fatal("watch.add returned empty id")
	}

	var listResp struct {
		Watches []map[string]any `json:"watches"`
		Count   int              `json:"count"`
	}
	if err := json.Unmarshal([]byte(exec("watch.list", map[string]any{})), &listResp); err != nil {
		t.Fatalf("watch.list unmarshal: %v", err)
	}
	if listResp.Count != 1 {
		t.Fatalf("watch.list count = %d, want 1", listResp.Count)
	}

	var statusResp struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Interval        string `json:"interval"`
		SourceURL       string `json:"source_url"`
		DeliveryChannel string `json:"delivery_channel"`
		DeliveryTarget  string `json:"delivery_target"`
		LastStatus      string `json:"last_status"`
	}
	if err := json.Unmarshal([]byte(exec("watch.status", map[string]any{"id": addResp.ID})), &statusResp); err != nil {
		t.Fatalf("watch.status unmarshal: %v", err)
	}
	if statusResp.Name != "news-watch" {
		t.Fatalf("watch.status name = %q", statusResp.Name)
	}
	if statusResp.SourceURL != "https://example.com/news" {
		t.Fatalf("watch.status source_url = %q", statusResp.SourceURL)
	}
	if statusResp.DeliveryChannel != "feishu" || statusResp.DeliveryTarget != "oc_alerts" {
		t.Fatalf("watch.status delivery = %q %q", statusResp.DeliveryChannel, statusResp.DeliveryTarget)
	}

	var updateResp struct {
		Updated bool `json:"updated"`
	}
	if err := json.Unmarshal([]byte(exec("watch.update", map[string]any{
		"id":       addResp.ID,
		"interval": "10m",
		"enabled":  false,
	})), &updateResp); err != nil {
		t.Fatalf("watch.update unmarshal: %v", err)
	}
	if !updateResp.Updated {
		t.Fatal("watch.update updated=false")
	}

	item, err := store.Get(addResp.ID)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if item.Interval != "10m" {
		t.Fatalf("updated interval = %q, want 10m", item.Interval)
	}
	if item.Enabled {
		t.Fatal("updated watch should be disabled")
	}
	if item.Delivery == nil || item.Delivery.Channel != "feishu" || item.Delivery.Target != "oc_alerts" {
		t.Fatalf("updated delivery = %+v", item.Delivery)
	}

	var removeResp struct {
		Removed bool `json:"removed"`
	}
	if err := json.Unmarshal([]byte(exec("watch.remove", map[string]any{"id": addResp.ID})), &removeResp); err != nil {
		t.Fatalf("watch.remove unmarshal: %v", err)
	}
	if !removeResp.Removed {
		t.Fatal("watch.remove removed=false")
	}
	if _, err := store.Get(addResp.ID); err == nil {
		t.Fatal("expected removed watch to be absent")
	}
}
