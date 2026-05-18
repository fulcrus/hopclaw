package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/skill"
)

func TestHandleSkillsInstallRejectsTrailingJSON(t *testing.T) {
	client, _ := newTestSkillHubWithCatalog(t, "review-skill", "Review Skill")

	gw := newTestGatewayFull(t)
	gw.SetSkillHub(client)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/skills/install", `{"source":"review-skill"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/skills/install status = %d body=%s", rec.Code, rec.Body.String())
	}

	installed, err := client.Installed()
	if err != nil {
		t.Fatalf("Installed() error = %v", err)
	}
	if len(installed) != 0 {
		t.Fatalf("installed len = %d, want 0", len(installed))
	}
}

func TestHandleSkillsInstallFromLocalSourceUsesDirectoryNameWhenNameMissing(t *testing.T) {
	client := skill.NewFileClawHubClient(t.TempDir())
	bundleDir := writeTestSkillBundle(t, t.TempDir(), "local-review", "# Local Review Skill\nChecks local code.\n")

	gw := newTestGatewayFull(t)
	gw.SetSkillHub(client)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/skills/install", `{"source":"`+bundleDir+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /operator/skills/install status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode install response: %v", err)
	}
	if got := payload["skill_id"]; got != "local-review" {
		t.Fatalf("skill_id = %#v, want local-review", got)
	}

	installed, err := client.Installed()
	if err != nil {
		t.Fatalf("Installed() error = %v", err)
	}
	if len(installed) != 1 {
		t.Fatalf("installed len = %d, want 1", len(installed))
	}
	if installed[0].SkillID != "local-review" {
		t.Fatalf("installed skill_id = %q, want local-review", installed[0].SkillID)
	}
}

func TestHandleSkillsPreflightRejectsTrailingJSON(t *testing.T) {
	gw := newTestGatewayFull(t)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/skills/preflight", `{"binaries":["sh"]} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/skills/preflight status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleSkillsUpdateConfigRejectsTrailingJSON(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	initialCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	gw := newTestGatewayFull(t)
	gw.SetConfigWatcher(config.NewWatcher(cfgPath, initialCfg, time.Hour), cfgPath)

	rec := doRequest(t, gw.Handler(), http.MethodPut, "/operator/skills/demo/config", `{"config":{"enabled":true}} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /operator/skills/demo/config status = %d body=%s", rec.Code, rec.Body.String())
	}

	current := gw.configWatcher.Current()
	if len(current.Skills.Config) != 0 {
		t.Fatalf("skills config mutated on invalid json: %#v", current.Skills.Config)
	}
}

func TestHandleSkillsInstallEmitsTelemetry(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)

	client, _ := newTestSkillHubWithCatalog(t, "review-skill", "Review Skill")

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

	enabled := true
	gw := newTestGatewayFull(t)
	gw.SetSkillHub(client)
	gw.config.Diagnostics = config.DiagnosticsConfig{
		TelemetryEnabled:  &enabled,
		TelemetryEndpoint: server.URL,
	}

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/skills/install", `{"source":"review-skill"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /operator/skills/install status = %d body=%s", rec.Code, rec.Body.String())
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
	if received["event"] != "skill.installed" {
		t.Fatalf("received event = %#v", received)
	}
	props, _ := received["properties"].(map[string]any)
	if props["skill_id"] != "review-skill" {
		t.Fatalf("skill_id = %#v, want review-skill", props["skill_id"])
	}
	if props["source_kind"] != "catalog" {
		t.Fatalf("source_kind = %#v, want catalog", props["source_kind"])
	}
}

func newTestSkillHubWithCatalog(t *testing.T, skillID string, name string) (*skill.FileClawHubClient, string) {
	t.Helper()

	client := skill.NewFileClawHubClient(t.TempDir())
	bundleDir := writeTestSkillBundle(t, t.TempDir(), skillID, "# "+name+"\nChecks code.\n")

	indexDir := client.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry, err := json.MarshalIndent(map[string]any{
		"id":         skillID,
		"name":       name,
		"version":    "1.0.0",
		"summary":    "Code review helper",
		"bundle_dir": bundleDir,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, skillID+".json"), append(entry, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return client, bundleDir
}

func writeTestSkillBundle(t *testing.T, parentDir string, dirName string, markdown string) string {
	t.Helper()

	bundleDir := filepath.Join(parentDir, dirName)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + dirName + "\ndescription: test skill\n---\n" + markdown
	if err := os.WriteFile(filepath.Join(bundleDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return bundleDir
}
