package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/plugin"
)

func TestHandlePluginsInstallRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw, inst, _ := newPluginTestGateway(t)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/plugins", `{"source":"demo"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/plugins status = %d body=%s", rec.Code, rec.Body.String())
	}

	enabled, disabled := inst.ListInstalled()
	if len(enabled) != 0 || len(disabled) != 0 {
		t.Fatalf("plugins unexpectedly installed: enabled=%d disabled=%d", len(enabled), len(disabled))
	}
}

func TestHandlePluginsLifecycle(t *testing.T) {
	t.Parallel()

	gw, _, refresh := newPluginTestGateway(t)
	sourceDir := writeTestPluginSource(t, "demo-plugin")

	install := doRequest(t, gw.Handler(), http.MethodPost, "/operator/plugins", `{"source":"`+sourceDir+`"}`)
	if install.Code != http.StatusCreated {
		t.Fatalf("POST /operator/plugins status = %d body=%s", install.Code, install.Body.String())
	}

	list := doRequest(t, gw.Handler(), http.MethodGet, "/operator/plugins", "")
	if list.Code != http.StatusOK {
		t.Fatalf("GET /operator/plugins status = %d body=%s", list.Code, list.Body.String())
	}
	var listPayload pluginsListResponse
	if err := json.Unmarshal(list.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listPayload.Count != 1 || listPayload.Items[0].Name != "demo-plugin" || !listPayload.Items[0].Enabled {
		t.Fatalf("unexpected list payload: %#v", listPayload)
	}
	if listPayload.Items[0].RuntimeModule == nil {
		t.Fatalf("expected runtime module in list payload: %#v", listPayload.Items[0])
	}
	if listPayload.Items[0].RuntimeModule.Level != modules.ModuleLevelDeclared {
		t.Fatalf("runtime module level = %q, want %q", listPayload.Items[0].RuntimeModule.Level, modules.ModuleLevelDeclared)
	}

	get := doRequest(t, gw.Handler(), http.MethodGet, "/operator/plugins/demo-plugin", "")
	if get.Code != http.StatusOK {
		t.Fatalf("GET /operator/plugins/{name} status = %d body=%s", get.Code, get.Body.String())
	}
	var getPayload pluginGetResponse
	if err := json.Unmarshal(get.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getPayload.Plugin.Name != "demo-plugin" || getPayload.Tools != 1 {
		t.Fatalf("unexpected get payload: %#v", getPayload)
	}
	if getPayload.Plugin.RuntimeModule == nil {
		t.Fatalf("expected runtime module in get payload: %#v", getPayload.Plugin)
	}
	if getPayload.Plugin.RuntimeModule.Health == nil || getPayload.Plugin.RuntimeModule.Health.Status != modules.HealthUnknown {
		t.Fatalf("unexpected runtime module health: %#v", getPayload.Plugin.RuntimeModule)
	}
	if _, ok := getPayload.Channels["echo"]; !ok {
		t.Fatalf("expected echo channel in %#v", getPayload.Channels)
	}

	disable := doRequest(t, gw.Handler(), http.MethodPost, "/operator/plugins/demo-plugin/disable", "")
	if disable.Code != http.StatusOK {
		t.Fatalf("POST /operator/plugins/{name}/disable status = %d body=%s", disable.Code, disable.Body.String())
	}
	listAfterDisable := doRequest(t, gw.Handler(), http.MethodGet, "/operator/plugins", "")
	var disabledPayload pluginsListResponse
	if err := json.Unmarshal(listAfterDisable.Body.Bytes(), &disabledPayload); err != nil {
		t.Fatalf("decode disabled list response: %v", err)
	}
	if disabledPayload.Count != 1 || disabledPayload.Items[0].Enabled {
		t.Fatalf("expected disabled plugin, got %#v", disabledPayload)
	}

	enable := doRequest(t, gw.Handler(), http.MethodPost, "/operator/plugins/demo-plugin/enable", "")
	if enable.Code != http.StatusOK {
		t.Fatalf("POST /operator/plugins/{name}/enable status = %d body=%s", enable.Code, enable.Body.String())
	}

	remove := doRequest(t, gw.Handler(), http.MethodDelete, "/operator/plugins/demo-plugin", "")
	if remove.Code != http.StatusOK {
		t.Fatalf("DELETE /operator/plugins/{name} status = %d body=%s", remove.Code, remove.Body.String())
	}

	finalList := doRequest(t, gw.Handler(), http.MethodGet, "/operator/plugins", "")
	var finalPayload pluginsListResponse
	if err := json.Unmarshal(finalList.Body.Bytes(), &finalPayload); err != nil {
		t.Fatalf("decode final list response: %v", err)
	}
	if finalPayload.Count != 0 {
		t.Fatalf("expected no plugins after delete, got %#v", finalPayload)
	}
	if refresh.calls != 4 {
		t.Fatalf("refresh calls = %d, want 4", refresh.calls)
	}
}

func TestHandlePluginsLifecycleShowsMinimalRuntimeModuleForLevel0Plugin(t *testing.T) {
	t.Parallel()

	gw, _, _ := newPluginTestGateway(t)
	sourceDir := writeMinimalPluginSource(t, "hello-tool")

	install := doRequest(t, gw.Handler(), http.MethodPost, "/operator/plugins", `{"source":"`+sourceDir+`"}`)
	if install.Code != http.StatusCreated {
		t.Fatalf("POST /operator/plugins status = %d body=%s", install.Code, install.Body.String())
	}

	list := doRequest(t, gw.Handler(), http.MethodGet, "/operator/plugins", "")
	if list.Code != http.StatusOK {
		t.Fatalf("GET /operator/plugins status = %d body=%s", list.Code, list.Body.String())
	}

	var payload pluginsListResponse
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if payload.Count != 1 || payload.Items[0].RuntimeModule == nil {
		t.Fatalf("unexpected list payload: %#v", payload)
	}
	if payload.Items[0].RuntimeModule.Level != modules.ModuleLevelMinimal {
		t.Fatalf("runtime module level = %q, want %q", payload.Items[0].RuntimeModule.Level, modules.ModuleLevelMinimal)
	}
}

func TestHandlePluginsInstallRollsBackWhenRuntimeRefreshFails(t *testing.T) {
	t.Parallel()

	gw, inst, refresh := newPluginTestGateway(t)
	refresh.fail = true
	sourceDir := writeTestPluginSource(t, "rollback-plugin")

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/plugins", `{"source":"`+sourceDir+`"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST /operator/plugins status = %d body=%s", rec.Code, rec.Body.String())
	}

	enabled, disabled := inst.ListInstalled()
	if len(enabled) != 0 || len(disabled) != 0 {
		t.Fatalf("plugin install was not rolled back: enabled=%d disabled=%d", len(enabled), len(disabled))
	}
}

func TestHandlePluginsDisableRollsBackWhenRuntimeRefreshFails(t *testing.T) {
	t.Parallel()

	gw, _, refresh := newPluginTestGateway(t)
	sourceDir := writeTestPluginSource(t, "rollback-disable")

	install := doRequest(t, gw.Handler(), http.MethodPost, "/operator/plugins", `{"source":"`+sourceDir+`"}`)
	if install.Code != http.StatusCreated {
		t.Fatalf("POST /operator/plugins status = %d body=%s", install.Code, install.Body.String())
	}

	refresh.fail = true
	disable := doRequest(t, gw.Handler(), http.MethodPost, "/operator/plugins/rollback-disable/disable", "")
	if disable.Code != http.StatusInternalServerError {
		t.Fatalf("POST /operator/plugins/{name}/disable status = %d body=%s", disable.Code, disable.Body.String())
	}

	list := doRequest(t, gw.Handler(), http.MethodGet, "/operator/plugins", "")
	var payload pluginsListResponse
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if payload.Count != 1 || !payload.Items[0].Enabled {
		t.Fatalf("expected plugin to remain enabled after rollback: %#v", payload)
	}
}

func TestHandlePluginsInstallEmitsTelemetry(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)

	var mu sync.Mutex
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch struct {
			Events []struct {
				Event      string         `json:"event"`
				Properties map[string]any `json:"properties"`
			} `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Errorf("Decode(batch) error = %v", err)
			http.Error(w, "decode error", http.StatusBadRequest)
			return
		}
		if len(batch.Events) != 1 {
			t.Errorf("events len = %d, want 1", len(batch.Events))
			http.Error(w, "wrong event count", http.StatusBadRequest)
			return
		}
		mu.Lock()
		received = map[string]any{
			"event":      batch.Events[0].Event,
			"properties": batch.Events[0].Properties,
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "accepted": 1})
	}))
	defer server.Close()

	gw, _, _ := newPluginTestGateway(t)
	enabled := true
	gw.config.Diagnostics = config.DiagnosticsConfig{
		TelemetryEnabled:  &enabled,
		TelemetryEndpoint: server.URL,
	}

	sourceDir := writeTestPluginSource(t, "telemetry-plugin")
	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/plugins", `{"source":"`+sourceDir+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /operator/plugins status = %d body=%s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := received
		mu.Unlock()
		if got != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if received["event"] != "plugin.installed" {
		t.Fatalf("received event = %#v", received)
	}
	props, _ := received["properties"].(map[string]any)
	if props["plugin_name"] != "telemetry-plugin" {
		t.Fatalf("plugin_name = %#v, want telemetry-plugin", props["plugin_name"])
	}
}

func newPluginTestGateway(t *testing.T) (*Gateway, *plugin.Installer, *pluginRefreshRecorder) {
	t.Helper()

	manager := plugin.NewManager()
	moduleCatalog := modules.NewStore(modules.Catalog{})
	inst := &plugin.Installer{
		PluginDir: filepath.Join(t.TempDir(), "plugins"),
		Manager:   manager,
	}
	refresh := &pluginRefreshRecorder{
		manager:       manager,
		moduleCatalog: moduleCatalog,
	}

	gw := newTestGatewayFull(t)
	gw.SetModuleCatalog(moduleCatalog)
	gw.SetPluginInstaller(inst)
	gw.SetPluginRuntime(refresh)
	return gw, inst, refresh
}

func writeTestPluginSource(t *testing.T, name string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `name: ` + name + `
version: "1.2.3"
description: "demo plugin"
author: "test"
channels:
  echo:
    type: stdio
    command: "./echo"
tools:
  - name: echo.send
    description: "send echo"
    endpoint: "https://example.com/echo"
agents:
  reviewer:
    model: "gpt-4o-mini"
`
	if err := os.WriteFile(filepath.Join(dir, "hopclaw.plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeMinimalPluginSource(t *testing.T, name string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `name: ` + name + `
version: "1.0.0"
description: "minimal tool plugin"
tools:
  - name: ` + name + `.echo
    description: "echo text"
    endpoint: "inline://` + name + `.echo"
`
	if err := os.WriteFile(filepath.Join(dir, "hopclaw.plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

type pluginRefreshRecorder struct {
	calls         int
	fail          bool
	manager       *plugin.Manager
	moduleCatalog *modules.Store
}

func (p *pluginRefreshRecorder) RefreshPlugins(context.Context) error {
	p.calls++
	if p.fail {
		return errors.New("refresh failed")
	}
	if p.moduleCatalog != nil && p.manager != nil {
		p.moduleCatalog.Swap(modules.BuildCatalog(p.manager.Modules()))
	}
	return nil
}
