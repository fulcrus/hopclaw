package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/server"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolruntime"
)

func TestHandleToolsTestExecutesReadOnlyTool(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	gw := newGatewayWithBuiltins(t, root)
	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/tools/test", `{"tool":"fs.list","input":{"path":"."}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /operator/tools/test status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp toolTestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !resp.OK {
		t.Fatalf("ok = false, body=%s", rec.Body.String())
	}
	if resp.Tool != "fs.list" {
		t.Fatalf("tool = %q, want fs.list", resp.Tool)
	}
	if resp.SideEffectClass != "read" {
		t.Fatalf("side_effect_class = %q, want read", resp.SideEffectClass)
	}
	if !strings.Contains(resp.Output, "hello.txt") {
		t.Fatalf("output = %q, want to contain hello.txt", resp.Output)
	}
}

func TestHandleToolsTestRejectsWriteTool(t *testing.T) {
	gw := newGatewayWithBuiltins(t, t.TempDir())
	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/tools/test", `{"tool":"fs.write","input":{"path":"demo.txt","content":"x"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/tools/test status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not testable") {
		t.Fatalf("body = %s, want read-only rejection", rec.Body.String())
	}
}

func TestHandleToolsTestRejectsTrailingJSON(t *testing.T) {
	gw := newGatewayWithBuiltins(t, t.TempDir())
	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/tools/test", `{"tool":"fs.list","input":{"path":"."}} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/tools/test trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleSkillsUpdateConfigStoresConfigUnderSkillKey(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	initialCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	skillRoot := filepath.Join(t.TempDir(), "skills")
	skillDir := filepath.Join(skillRoot, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillBody := `---
name: demo
description: demo skill
metadata: {"openclaw":{"skillKey":"demo.key"}}
---
# demo
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillBody), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := skill.NewService(skill.ServiceConfig{
		Roots: []skill.DiscoveryRoot{{Kind: skill.SourceWorkspace, Path: skillRoot}},
	})
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("skill refresh error = %v", err)
	}

	gw := newTestGatewayFull(t)
	gw.SetSkillService(svc)
	gw.SetConfigWatcher(config.NewWatcher(cfgPath, initialCfg, time.Hour), cfgPath)

	rec := doRequest(t, gw.Handler(), http.MethodPut, "/operator/skills/demo/config", `{"config":{"enabled":true,"project":{"id":"abc"}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /operator/skills/demo/config status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK         bool              `json:"ok"`
		ConfigKey  string            `json:"config_key"`
		Config     map[string]any    `json:"config"`
		ReloadPlan config.ReloadPlan `json:"reload_plan"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !resp.OK {
		t.Fatalf("ok = false, body=%s", rec.Body.String())
	}
	if resp.ConfigKey != "demo.key" {
		t.Fatalf("config_key = %q, want demo.key", resp.ConfigKey)
	}
	if resp.ReloadPlan.Action != config.ReloadActionHot {
		t.Fatalf("reload action = %q, want hot", resp.ReloadPlan.Action)
	}

	current := gw.configWatcher.Current()
	if current.Skills.Config["demo.key"]["enabled"] != true {
		t.Fatalf("skills.config = %#v", current.Skills.Config)
	}
	project, _ := current.Skills.Config["demo.key"]["project"].(map[string]any)
	if project["id"] != "abc" {
		t.Fatalf("project config = %#v", project)
	}
}

func newGatewayWithBuiltins(t *testing.T, root string) *Gateway {
	t.Helper()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	builtins := toolruntime.NewBuiltins(toolruntime.BuiltinsConfig{Root: root})
	engine := contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "test",
		IncludeSkillCatalog:  false,
		DefaultContextWindow: 512,
		DefaultOutputTokens:  64,
	}, nil)
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), engine, nil, builtins, agent.StaticRuntimeContextProvider{})
	runtimeSvc := runtimepkg.NewService(component, sessions, runs, nil, bus, nil)
	srv := server.New(runtimeSvc, server.Config{AuthToken: "test-token"})
	return gatewayFromServer(srv, Config{
		AuthToken: "test-token",
		Runtime:   runtimeSvc,
	})
}
