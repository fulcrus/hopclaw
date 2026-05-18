package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/wakeup"
)

func TestWakeupToolsLifecycle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	storePath := filepath.Join(t.TempDir(), "wakeup.json")
	if err := os.WriteFile(storePath, []byte(`{"version":1,"triggers":[]}`), 0o644); err != nil {
		t.Fatalf("write wakeup store: %v", err)
	}
	store, err := wakeup.Load(storePath)
	if err != nil {
		t.Fatalf("wakeup.Load() error = %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{WakeupService: wakeup.NewService(store, nil)})

	ctx := context.Background()
	run := &agent.Run{ID: "run-wakeup"}
	sess := &agent.Session{ID: "sess-wakeup"}

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
	if err := json.Unmarshal([]byte(exec("wakeup.add", map[string]any{
		"name":        "morning-reminder",
		"schedule":    "0 8 * * *",
		"message":     "Send morning summary",
		"channel":     "feishu",
		"session_key": "feishu:chat-1",
		"enabled":     true,
	})), &addResp); err != nil {
		t.Fatalf("wakeup.add unmarshal: %v", err)
	}
	if addResp.ID == "" {
		t.Fatal("wakeup.add returned empty id")
	}

	var listResp struct {
		Triggers []map[string]any `json:"triggers"`
		Count    int              `json:"count"`
	}
	if err := json.Unmarshal([]byte(exec("wakeup.list", map[string]any{})), &listResp); err != nil {
		t.Fatalf("wakeup.list unmarshal: %v", err)
	}
	if listResp.Count != 1 {
		t.Fatalf("wakeup.list count = %d, want 1", listResp.Count)
	}

	var statusResp struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Schedule   string `json:"schedule"`
		SessionKey string `json:"session_key"`
		Channel    string `json:"channel"`
	}
	if err := json.Unmarshal([]byte(exec("wakeup.status", map[string]any{"id": addResp.ID})), &statusResp); err != nil {
		t.Fatalf("wakeup.status unmarshal: %v", err)
	}
	if statusResp.Name != "morning-reminder" {
		t.Fatalf("wakeup.status name = %q", statusResp.Name)
	}
	if statusResp.Schedule != "0 8 * * *" {
		t.Fatalf("wakeup.status schedule = %q", statusResp.Schedule)
	}
	if statusResp.SessionKey != "feishu:chat-1" || statusResp.Channel != "feishu" {
		t.Fatalf("wakeup.status routing = %q %q", statusResp.SessionKey, statusResp.Channel)
	}

	var updateResp struct {
		Updated bool `json:"updated"`
	}
	if err := json.Unmarshal([]byte(exec("wakeup.update", map[string]any{
		"id":       addResp.ID,
		"schedule": "0 9 * * *",
		"enabled":  false,
	})), &updateResp); err != nil {
		t.Fatalf("wakeup.update unmarshal: %v", err)
	}
	if !updateResp.Updated {
		t.Fatal("wakeup.update updated=false")
	}
	current, err := store.Get(addResp.ID)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if current.Schedule != "0 9 * * *" || current.Enabled {
		t.Fatalf("updated trigger = %+v", current)
	}

	var removeResp struct {
		Removed bool `json:"removed"`
	}
	if err := json.Unmarshal([]byte(exec("wakeup.remove", map[string]any{"id": addResp.ID})), &removeResp); err != nil {
		t.Fatalf("wakeup.remove unmarshal: %v", err)
	}
	if !removeResp.Removed {
		t.Fatal("wakeup.remove removed=false")
	}
	if _, err := store.Get(addResp.ID); err == nil {
		t.Fatal("expected removed trigger to be absent")
	}
}
