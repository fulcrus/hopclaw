package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/skill"
)

func TestHandleDevicesLifecycle(t *testing.T) {
	gw := newTestGatewayFull(t)

	store := deviceauth.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("store.Load() error = %v", err)
	}
	pairing := deviceauth.NewPairingManager(store)
	gw.SetDeviceAuth(store, pairing)

	handler := gw.Handler()

	create := doRequest(t, handler, http.MethodPost, "/operator/devices/pair", `{"device_id":"ios-1","name":"Alice Phone","platform":"ios","device_family":"mobile","channel":"ios"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("POST /operator/devices/pair status = %d body=%s", create.Code, create.Body.String())
	}
	var createPayload map[string]any
	if err := json.Unmarshal(create.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	code, _ := createPayload["code"].(string)
	if code == "" {
		t.Fatalf("pairing code missing in %#v", createPayload)
	}

	approve := doRequest(t, handler, http.MethodPost, "/operator/devices/pair/approve", fmt.Sprintf(`{"code":%q,"role":"viewer"}`, code))
	if approve.Code != http.StatusOK {
		t.Fatalf("POST /operator/devices/pair/approve status = %d body=%s", approve.Code, approve.Body.String())
	}
	var approvePayload map[string]any
	if err := json.Unmarshal(approve.Body.Bytes(), &approvePayload); err != nil {
		t.Fatalf("decode approve response: %v", err)
	}
	firstToken, _ := approvePayload["token"].(string)
	if firstToken == "" {
		t.Fatalf("issued token missing in %#v", approvePayload)
	}

	list := doRequest(t, handler, http.MethodGet, "/operator/devices", "")
	if list.Code != http.StatusOK {
		t.Fatalf("GET /operator/devices status = %d body=%s", list.Code, list.Body.String())
	}
	var devices devicesListResponse
	if err := json.Unmarshal(list.Body.Bytes(), &devices); err != nil {
		t.Fatalf("decode devices response: %v", err)
	}
	if devices.Count != 1 {
		t.Fatalf("devices count = %d, want 1", devices.Count)
	}
	if got := devices.Items[0].DeviceID; got != "ios-1" {
		t.Fatalf("device id = %q, want ios-1", got)
	}
	if !devices.Items[0].Trusted {
		t.Fatal("expected device to be trusted after approval")
	}
	if len(devices.Items[0].Tokens) != 1 {
		t.Fatalf("tokens len = %d, want 1", len(devices.Items[0].Tokens))
	}

	rotate := doRequest(t, handler, http.MethodPost, "/operator/devices/ios-1/tokens/rotate", `{"role":"viewer"}`)
	if rotate.Code != http.StatusOK {
		t.Fatalf("POST /operator/devices/{id}/tokens/rotate status = %d body=%s", rotate.Code, rotate.Body.String())
	}
	var rotatePayload map[string]any
	if err := json.Unmarshal(rotate.Body.Bytes(), &rotatePayload); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	rotatedToken, _ := rotatePayload["token"].(string)
	if rotatedToken == "" || rotatedToken == firstToken {
		t.Fatalf("rotated token = %q, first token = %q", rotatedToken, firstToken)
	}

	revoke := doRequest(t, handler, http.MethodPost, "/operator/devices/ios-1/tokens/revoke", `{"role":"viewer"}`)
	if revoke.Code != http.StatusOK {
		t.Fatalf("POST /operator/devices/{id}/tokens/revoke status = %d body=%s", revoke.Code, revoke.Body.String())
	}
	if _, ok := store.GetToken("ios-1", deviceauth.RoleViewer); ok {
		t.Fatal("expected token to be revoked from store")
	}
}

func TestHandleBrowserProfilesProxy(t *testing.T) {
	var deleted string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/browser/v1/profiles":
			_ = json.NewEncoder(w).Encode([]browserclient.Profile{{
				Name:   "default",
				Driver: "local",
				Color:  "#4A90D9",
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/browser/v1/profiles":
			var req browserclient.CreateProfileRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(browserclient.Profile{
				Name:   req.Name,
				Driver: "local",
				Color:  req.Color,
				CDPURL: req.CDPURL,
			})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/browser/v1/profiles/"):
			deleted = filepath.Base(r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer mock.Close()

	gw := newTestGatewayFull(t)
	gw.SetBrowserClient(browserclient.New(mock.URL))
	handler := gw.Handler()

	list := doRequest(t, handler, http.MethodGet, "/operator/browser/profiles", "")
	if list.Code != http.StatusOK {
		t.Fatalf("GET /operator/browser/profiles status = %d body=%s", list.Code, list.Body.String())
	}

	create := doRequest(t, handler, http.MethodPost, "/operator/browser/profiles", `{"name":"work","color":"#123456"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("POST /operator/browser/profiles status = %d body=%s", create.Code, create.Body.String())
	}

	del := doRequest(t, handler, http.MethodDelete, "/operator/browser/profiles/work", "")
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE /operator/browser/profiles/{name} status = %d body=%s", del.Code, del.Body.String())
	}
	if deleted != "work" {
		t.Fatalf("deleted = %q, want work", deleted)
	}
}

func TestHandleBrowserProfilesCreateRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	var createCalls int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/browser/v1/profiles" {
			createCalls++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(browserclient.Profile{Name: "work"})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer mock.Close()

	gw := newTestGatewayFull(t)
	gw.SetBrowserClient(browserclient.New(mock.URL))

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/browser/profiles", `{"name":"work"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/browser/profiles trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}
	if createCalls != 0 {
		t.Fatalf("browser create calls = %d, want 0", createCalls)
	}
}

func TestHandleSkillsInstallCatalogDelete(t *testing.T) {
	root := t.TempDir()
	client := skill.NewFileClawHubClient(root)

	bundleDir := filepath.Join(t.TempDir(), "review-bundle")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "SKILL.md"), []byte("# Review Skill\nChecks code."), 0o644); err != nil {
		t.Fatal(err)
	}

	indexDir := client.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry, err := json.MarshalIndent(map[string]any{
		"id":         "review-skill",
		"name":       "Review Skill",
		"version":    "1.0.0",
		"summary":    "Code review helper",
		"bundle_dir": bundleDir,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "review-skill.json"), append(entry, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	gw := newTestGatewayFull(t)
	gw.SetSkillHub(client)
	handler := gw.Handler()

	catalog := doRequest(t, handler, http.MethodGet, "/operator/skills/catalog", "")
	if catalog.Code != http.StatusOK {
		t.Fatalf("GET /operator/skills/catalog status = %d body=%s", catalog.Code, catalog.Body.String())
	}
	var catalogPayload skillsCatalogResponse
	if err := json.Unmarshal(catalog.Body.Bytes(), &catalogPayload); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}
	if catalogPayload.Count != 1 || catalogPayload.Items[0].ID != "review-skill" {
		t.Fatalf("unexpected catalog payload: %#v", catalogPayload)
	}
	if catalogPayload.Items[0].Installability == nil {
		t.Fatalf("expected catalog installability projection: %#v", catalogPayload.Items[0])
	}

	catalogDetail := doRequest(t, handler, http.MethodGet, "/operator/skills/catalog/review-skill", "")
	if catalogDetail.Code != http.StatusOK {
		t.Fatalf("GET /operator/skills/catalog/{id} status = %d body=%s", catalogDetail.Code, catalogDetail.Body.String())
	}
	var catalogDetailPayload map[string]any
	if err := json.Unmarshal(catalogDetail.Body.Bytes(), &catalogDetailPayload); err != nil {
		t.Fatalf("decode catalog detail response: %v", err)
	}
	if strings.TrimSpace(fmt.Sprint(catalogDetailPayload["skill_id"])) != "review-skill" {
		t.Fatalf("unexpected catalog detail payload: %#v", catalogDetailPayload)
	}

	install := doRequest(t, handler, http.MethodPost, "/operator/skills/install", `{"source":"review-skill"}`)
	if install.Code != http.StatusCreated {
		t.Fatalf("POST /operator/skills/install status = %d body=%s", install.Code, install.Body.String())
	}

	list := doRequest(t, handler, http.MethodGet, "/operator/skills", "")
	if list.Code != http.StatusOK {
		t.Fatalf("GET /operator/skills status = %d body=%s", list.Code, list.Body.String())
	}
	var listPayload skillsListResponse
	if err := json.Unmarshal(list.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode skills list response: %v", err)
	}
	if listPayload.Count != 1 || listPayload.Items[0].ID != "review-skill" {
		t.Fatalf("unexpected skills list payload: %#v", listPayload)
	}
	if listPayload.Items[0].Installability == nil {
		t.Fatalf("expected installed skill installability projection: %#v", listPayload.Items[0])
	}

	del := doRequest(t, handler, http.MethodDelete, "/operator/skills/review-skill", "")
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE /operator/skills/{name} status = %d body=%s", del.Code, del.Body.String())
	}

	listAfter := doRequest(t, handler, http.MethodGet, "/operator/skills", "")
	if listAfter.Code != http.StatusOK {
		t.Fatalf("GET /operator/skills after delete status = %d body=%s", listAfter.Code, listAfter.Body.String())
	}
	var listAfterPayload skillsListResponse
	if err := json.Unmarshal(listAfter.Body.Bytes(), &listAfterPayload); err != nil {
		t.Fatalf("decode skills list-after-delete response: %v", err)
	}
	if listAfterPayload.Count != 0 {
		t.Fatalf("skills count after delete = %d, want 0", listAfterPayload.Count)
	}
}

func TestHandleSkillsGetReturnsDeepReport(t *testing.T) {
	root := t.TempDir()
	skillRoot := filepath.Join(root, "skills")
	skillDir := filepath.Join(skillRoot, "github-pr")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `---
name: github-pr
description: github review helper
metadata: {"openclaw":{"primaryEnv":"GITHUB_TOKEN","requires":{"env":["GITHUB_TOKEN"]}}}
---
# github-pr
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
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
	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/skills/github-pr", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/skills/{name} status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Found  bool `json:"found"`
		Ready  bool `json:"ready"`
		Checks []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
			Name   string `json:"name"`
		} `json:"checks"`
		Actions []string `json:"next_actions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Found || payload.Ready {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	foundMissingEnv := false
	for _, check := range payload.Checks {
		if check.Kind == "env" && check.Name == "GITHUB_TOKEN" && check.Status == "missing" {
			foundMissingEnv = true
		}
	}
	if !foundMissingEnv {
		t.Fatalf("expected missing env check: %#v", payload.Checks)
	}
	if len(payload.Actions) == 0 {
		t.Fatalf("expected next actions in payload: %#v", payload)
	}
}

func TestHandleSkillsListUsesModuleCatalogSkillProjections(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	gw.SetModuleCatalog(modules.NewStore(modules.BuildCatalog(modules.SkillModules(skill.RegistrySnapshot{
		Ordered: []*skill.SkillPackage{{
			ID:     "pkg-writer",
			Kind:   skill.SkillKindExecutable,
			Status: skill.StatusReady,
			Trust:  skill.TrustInternal,
			Prompt: skill.PromptSkill{
				Name:          "writer",
				Description:   "Write files",
				UserInvocable: true,
			},
			Source: skill.SkillSource{
				Kind: skill.SourceWorkspace,
				Dir:  "/workspace/skills/writer",
			},
			OpenClaw: skill.OpenClawMetadata{
				SkillKey: "dev.writer",
			},
			ToolManifests: []skill.ToolManifest{{
				Name: "writer.run",
			}},
		}},
	}))))

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/skills", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/skills status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload skillsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode skills list response: %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("payload.Count = %d, want 1", payload.Count)
	}
	if payload.Items[0].Name != "writer" || payload.Items[0].Kind != string(skill.SkillKindExecutable) {
		t.Fatalf("unexpected skill item = %#v", payload.Items[0])
	}
	if payload.Items[0].ToolCount != 1 || len(payload.Items[0].Tools) != 1 || payload.Items[0].Tools[0] != "writer.run" {
		t.Fatalf("unexpected tool projection in item = %#v", payload.Items[0])
	}
	if payload.Items[0].SourceKind != string(skill.SourceWorkspace) {
		t.Fatalf("source kind = %q, want %q", payload.Items[0].SourceKind, skill.SourceWorkspace)
	}
}

func TestResolveSkillConfigKeyPrefersModuleCatalogProjection(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	gw.SetModuleCatalog(modules.NewStore(modules.BuildCatalog(modules.SkillModules(skill.RegistrySnapshot{
		Ordered: []*skill.SkillPackage{{
			ID:     "pkg-writer",
			Status: skill.StatusReady,
			Prompt: skill.PromptSkill{Name: "writer"},
			Source: skill.SkillSource{
				Kind: skill.SourceWorkspace,
				Dir:  "/workspace/skills/writer",
			},
			OpenClaw: skill.OpenClawMetadata{
				SkillKey: "dev.writer",
			},
		}},
	}))))

	if got := gw.resolveSkillConfigKey("writer"); got != "dev.writer" {
		t.Fatalf("resolveSkillConfigKey(writer) = %q, want dev.writer", got)
	}
	if got := gw.resolveSkillConfigKey("pkg-writer"); got != "dev.writer" {
		t.Fatalf("resolveSkillConfigKey(pkg-writer) = %q, want dev.writer", got)
	}
}
