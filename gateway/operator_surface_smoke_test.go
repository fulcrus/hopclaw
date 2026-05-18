package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/deviceauth"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	"github.com/fulcrus/hopclaw/plugin"
)

func TestOperatorSurfaceSmokeProductFlow(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
		Locale: "en",
	}
	cfg.ApplyDefaults()

	gw, configPath := newLiveFileBackedOperatorGateway(t, cfg)

	skillHub, _ := newTestSkillHubWithCatalog(t, "review-skill", "Review Skill")
	gw.SetSkillHub(skillHub)

	manager := plugin.NewManager()
	refresh := &pluginRefreshRecorder{}
	gw.SetPluginInstaller(&plugin.Installer{
		PluginDir: filepath.Join(t.TempDir(), "plugins"),
		Manager:   manager,
	})
	gw.SetPluginRuntime(refresh)

	catalogRec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/setup/catalog", "")
	if catalogRec.Code != http.StatusOK {
		t.Fatalf("GET /operator/setup/catalog status = %d body=%s", catalogRec.Code, catalogRec.Body.String())
	}
	var catalog config.OperatorSetupCatalog
	if err := json.Unmarshal(catalogRec.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode setup catalog: %v", err)
	}
	if len(catalog.Providers) == 0 || len(catalog.Channels) == 0 || len(catalog.ProviderAPIs) == 0 {
		t.Fatalf("unexpected setup catalog payload: %#v", catalog)
	}
	if !setupCatalogProviderHasCapabilities(catalog.Providers, "openai") {
		t.Fatalf("expected setup catalog provider capability matrix: %#v", catalog.Providers)
	}
	if !setupCatalogAPIHasCapabilities(catalog.ProviderAPIs, "bedrock-converse") {
		t.Fatalf("expected setup catalog provider api capability matrix: %#v", catalog.ProviderAPIs)
	}

	putLocale := doRequest(t, gw.Handler(), http.MethodPut, "/operator/config/locale", `"zh-CN"`)
	if putLocale.Code != http.StatusOK {
		t.Fatalf("PUT /operator/config/locale status = %d body=%s", putLocale.Code, putLocale.Body.String())
	}

	getLocale := doRequest(t, gw.Handler(), http.MethodGet, "/operator/config/locale", "")
	if getLocale.Code != http.StatusOK {
		t.Fatalf("GET /operator/config/locale status = %d body=%s", getLocale.Code, getLocale.Body.String())
	}
	var locale string
	if err := json.Unmarshal(getLocale.Body.Bytes(), &locale); err != nil {
		t.Fatalf("decode locale: %v", err)
	}
	if locale != "zh-CN" {
		t.Fatalf("locale = %q, want zh-CN", locale)
	}

	createModel := doRequest(t, gw.Handler(), http.MethodPost, "/operator/models", `{
		"name": "anthropic",
		"api_key": "sk-anthropic",
		"default_model": "claude-3-7-sonnet"
	}`)
	if createModel.Code != http.StatusCreated {
		t.Fatalf("POST /operator/models status = %d body=%s", createModel.Code, createModel.Body.String())
	}

	listModels := doRequest(t, gw.Handler(), http.MethodGet, "/operator/models", "")
	if listModels.Code != http.StatusOK {
		t.Fatalf("GET /operator/models status = %d body=%s", listModels.Code, listModels.Body.String())
	}
	var modelsPayload modelsListResponse
	if err := json.Unmarshal(listModels.Body.Bytes(), &modelsPayload); err != nil {
		t.Fatalf("decode models list: %v", err)
	}
	if modelsPayload.Count != 1 || modelsPayload.Providers[0].Name != "anthropic" {
		t.Fatalf("unexpected models payload: %#v", modelsPayload)
	}
	if matrix := modelsPayload.Providers[0].CapabilityMatrix; matrix.ProviderName != "anthropic" || matrix.ProviderAPI == "" || !matrix.SupportsTools {
		t.Fatalf("unexpected capability matrix payload: %+v", matrix)
	}

	routerModels := doRequest(t, gw.Handler(), http.MethodGet, "/operator/models/router", "")
	if routerModels.Code != http.StatusOK {
		t.Fatalf("GET /operator/models/router status = %d body=%s", routerModels.Code, routerModels.Body.String())
	}
	var routerPayload modelsRouterResponse
	if err := json.Unmarshal(routerModels.Body.Bytes(), &routerPayload); err != nil {
		t.Fatalf("decode models router: %v", err)
	}
	if routerPayload.Count == 0 {
		t.Fatalf("unexpected empty models router payload: %#v", routerPayload)
	}
	if routerPayload.DefaultProvider != "anthropic" {
		t.Fatalf("router default provider = %q, want anthropic", routerPayload.DefaultProvider)
	}

	setupStatusRec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/setup/status", "")
	if setupStatusRec.Code != http.StatusOK {
		t.Fatalf("GET /operator/setup/status status = %d body=%s", setupStatusRec.Code, setupStatusRec.Body.String())
	}
	var setupStatus setupStatusResponse
	if err := json.Unmarshal(setupStatusRec.Body.Bytes(), &setupStatus); err != nil {
		t.Fatalf("decode setup status: %v", err)
	}
	if !setupStatus.Configured || !containsString(setupStatus.Providers, "anthropic") {
		t.Fatalf("unexpected setup status payload: %#v", setupStatus)
	}

	createChannel := doRequest(t, gw.Handler(), http.MethodPost, "/operator/channels", `{
		"name":"slack",
		"config":{
			"type":"slack",
			"bot_token":"xoxb-smoke",
			"app_token":"xapp-smoke",
			"dm_policy":"allowlist"
		},
		"enabled":true
	}`)
	if createChannel.Code != http.StatusCreated {
		t.Fatalf("POST /operator/channels status = %d body=%s", createChannel.Code, createChannel.Body.String())
	}

	listChannels := doRequest(t, gw.Handler(), http.MethodGet, "/operator/channels", "")
	if listChannels.Code != http.StatusOK {
		t.Fatalf("GET /operator/channels status = %d body=%s", listChannels.Code, listChannels.Body.String())
	}
	var channelsPayload channelsListResponse
	if err := json.Unmarshal(listChannels.Body.Bytes(), &channelsPayload); err != nil {
		t.Fatalf("decode channels list: %v", err)
	}
	if channelsPayload.Count != 1 || channelsPayload.Items[0].Name != "slack" {
		t.Fatalf("unexpected channels payload: %#v", channelsPayload)
	}

	catalogSkills := doRequest(t, gw.Handler(), http.MethodGet, "/operator/skills/catalog", "")
	if catalogSkills.Code != http.StatusOK {
		t.Fatalf("GET /operator/skills/catalog status = %d body=%s", catalogSkills.Code, catalogSkills.Body.String())
	}
	var catalogSkillsPayload skillsCatalogResponse
	if err := json.Unmarshal(catalogSkills.Body.Bytes(), &catalogSkillsPayload); err != nil {
		t.Fatalf("decode skills catalog: %v", err)
	}
	if catalogSkillsPayload.Count != 1 || catalogSkillsPayload.Items[0].ID != "review-skill" {
		t.Fatalf("unexpected skills catalog payload: %#v", catalogSkillsPayload)
	}

	installSkill := doRequest(t, gw.Handler(), http.MethodPost, "/operator/skills/install", `{"source":"review-skill"}`)
	if installSkill.Code != http.StatusCreated {
		t.Fatalf("POST /operator/skills/install status = %d body=%s", installSkill.Code, installSkill.Body.String())
	}

	listSkills := doRequest(t, gw.Handler(), http.MethodGet, "/operator/skills", "")
	if listSkills.Code != http.StatusOK {
		t.Fatalf("GET /operator/skills status = %d body=%s", listSkills.Code, listSkills.Body.String())
	}
	var skillsPayload skillsListResponse
	if err := json.Unmarshal(listSkills.Body.Bytes(), &skillsPayload); err != nil {
		t.Fatalf("decode skills list: %v", err)
	}
	if skillsPayload.Count != 1 || skillsPayload.Items[0].ID != "review-skill" {
		t.Fatalf("unexpected skills list payload: %#v", skillsPayload)
	}

	deleteSkill := doRequest(t, gw.Handler(), http.MethodDelete, "/operator/skills/review-skill", "")
	if deleteSkill.Code != http.StatusOK {
		t.Fatalf("DELETE /operator/skills/{name} status = %d body=%s", deleteSkill.Code, deleteSkill.Body.String())
	}

	sourceDir := writeTestPluginSource(t, "smoke-plugin")
	installPlugin := doRequest(t, gw.Handler(), http.MethodPost, "/operator/plugins", `{"source":"`+sourceDir+`"}`)
	if installPlugin.Code != http.StatusCreated {
		t.Fatalf("POST /operator/plugins status = %d body=%s", installPlugin.Code, installPlugin.Body.String())
	}

	listPlugins := doRequest(t, gw.Handler(), http.MethodGet, "/operator/plugins", "")
	if listPlugins.Code != http.StatusOK {
		t.Fatalf("GET /operator/plugins status = %d body=%s", listPlugins.Code, listPlugins.Body.String())
	}
	var pluginsPayload pluginsListResponse
	if err := json.Unmarshal(listPlugins.Body.Bytes(), &pluginsPayload); err != nil {
		t.Fatalf("decode plugins list: %v", err)
	}
	if pluginsPayload.Count != 1 || pluginsPayload.Items[0].Name != "smoke-plugin" || !pluginsPayload.Items[0].Enabled {
		t.Fatalf("unexpected plugins list payload: %#v", pluginsPayload)
	}

	disablePlugin := doRequest(t, gw.Handler(), http.MethodPost, "/operator/plugins/smoke-plugin/disable", "")
	if disablePlugin.Code != http.StatusOK {
		t.Fatalf("POST /operator/plugins/{name}/disable status = %d body=%s", disablePlugin.Code, disablePlugin.Body.String())
	}

	enablePlugin := doRequest(t, gw.Handler(), http.MethodPost, "/operator/plugins/smoke-plugin/enable", "")
	if enablePlugin.Code != http.StatusOK {
		t.Fatalf("POST /operator/plugins/{name}/enable status = %d body=%s", enablePlugin.Code, enablePlugin.Body.String())
	}

	deletePlugin := doRequest(t, gw.Handler(), http.MethodDelete, "/operator/plugins/smoke-plugin", "")
	if deletePlugin.Code != http.StatusOK {
		t.Fatalf("DELETE /operator/plugins/{name} status = %d body=%s", deletePlugin.Code, deletePlugin.Body.String())
	}

	finalPlugins := doRequest(t, gw.Handler(), http.MethodGet, "/operator/plugins", "")
	if finalPlugins.Code != http.StatusOK {
		t.Fatalf("GET /operator/plugins final status = %d body=%s", finalPlugins.Code, finalPlugins.Body.String())
	}
	var finalPluginsPayload pluginsListResponse
	if err := json.Unmarshal(finalPlugins.Body.Bytes(), &finalPluginsPayload); err != nil {
		t.Fatalf("decode final plugins list: %v", err)
	}
	if finalPluginsPayload.Count != 0 {
		t.Fatalf("unexpected final plugins payload: %#v", finalPluginsPayload)
	}

	if refresh.calls != 4 {
		t.Fatalf("plugin refresh calls = %d, want 4", refresh.calls)
	}

	updated := loadConfigFileForTest(t, configPath)
	if updated.Locale != "zh-CN" {
		t.Fatalf("config locale = %q, want zh-CN", updated.Locale)
	}
	if provider, ok := updated.Models.Providers["anthropic"]; !ok || provider.DefaultModel != "claude-3-7-sonnet" {
		t.Fatalf("config models = %#v", updated.Models.Providers)
	}
	if updated.Channels.Slack.BotToken != "xoxb-smoke" || updated.Channels.Slack.AppToken != "xapp-smoke" {
		t.Fatalf("config channels = %#v", updated.Channels.Slack)
	}
}

func setupCatalogProviderHasCapabilities(items []config.SetupProviderProfile, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return item.CapabilityMatrix.ProviderAPI != "" && item.CapabilityMatrix.SupportsStreaming
		}
	}
	return false
}

func setupCatalogAPIHasCapabilities(items []config.ProviderAPIProfile, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return item.CapabilityMatrix.ProviderAPI != "" && item.CapabilityMatrix.SupportsTools
		}
	}
	return false
}

func TestOperatorSurfaceSmokeDeviceAuthFlow(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
		Locale: "en",
	}
	cfg.ApplyDefaults()

	gw, _ := newLiveFileBackedOperatorGateway(t, cfg)
	store := deviceauth.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("store.Load() error = %v", err)
	}
	pairing := deviceauth.NewPairingManager(store)
	gw.SetDeviceAuth(store, pairing)

	nodeCreate := doRequest(t, gw.Handler(), http.MethodPost, "/operator/devices/pair", `{
		"device_id":"node-smoke",
		"name":"Worker Node",
		"platform":"linux",
		"device_family":"desktop",
		"channel":"desktop"
	}`)
	if nodeCreate.Code != http.StatusCreated {
		t.Fatalf("POST /operator/devices/pair node status = %d body=%s", nodeCreate.Code, nodeCreate.Body.String())
	}
	var nodePair devicePairCreateResponse
	if err := json.Unmarshal(nodeCreate.Body.Bytes(), &nodePair); err != nil {
		t.Fatalf("decode node pair response: %v", err)
	}

	claimReq := makeUnauthRequest(t, http.MethodPost, "/device/pair/claim", fmt.Sprintf(`{
		"code":%q,
		"device_id":"node-smoke",
		"name":"Worker Node",
		"platform":"linux",
		"device_family":"desktop",
		"scopes":["nodes.read"]
	}`, nodePair.Code))
	claimReq.Host = "devices.example.com"
	claimReq.Header.Set("Content-Type", "application/json")
	claimReq.Header.Set("X-Forwarded-Proto", "https")

	claimRec := captureResponse(t, gw.Handler(), claimReq)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("POST /device/pair/claim status = %d body=%s", claimRec.Code, claimRec.Body.String())
	}
	var claimPayload devicePairClaimResponse
	if err := json.Unmarshal(claimRec.Body.Bytes(), &claimPayload); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if !claimPayload.OK || claimPayload.Role != string(deviceauth.RoleNode) || claimPayload.WSURL != "wss://devices.example.com/operator/ws" {
		t.Fatalf("unexpected claim payload: %#v", claimPayload)
	}
	if _, ok := store.GetToken("node-smoke", deviceauth.RoleNode); !ok {
		t.Fatal("expected node token after claim")
	}

	mobileCreate := doRequest(t, gw.Handler(), http.MethodPost, "/operator/devices/pair", `{
		"device_id":"ios-smoke",
		"name":"Operator Phone",
		"platform":"ios",
		"device_family":"mobile",
		"channel":"ios"
	}`)
	if mobileCreate.Code != http.StatusCreated {
		t.Fatalf("POST /operator/devices/pair mobile status = %d body=%s", mobileCreate.Code, mobileCreate.Body.String())
	}
	var mobilePair devicePairCreateResponse
	if err := json.Unmarshal(mobileCreate.Body.Bytes(), &mobilePair); err != nil {
		t.Fatalf("decode mobile pair response: %v", err)
	}

	approve := doRequest(t, gw.Handler(), http.MethodPost, "/operator/devices/pair/approve", fmt.Sprintf(`{
		"code":%q,
		"role":"viewer",
		"scopes":["devices.read"]
	}`, mobilePair.Code))
	if approve.Code != http.StatusOK {
		t.Fatalf("POST /operator/devices/pair/approve status = %d body=%s", approve.Code, approve.Body.String())
	}
	var approvePayload devicePairApproveResponse
	if err := json.Unmarshal(approve.Body.Bytes(), &approvePayload); err != nil {
		t.Fatalf("decode approve response: %v", err)
	}
	if !approvePayload.OK || approvePayload.Token == "" || approvePayload.Role != string(deviceauth.RoleViewer) {
		t.Fatalf("unexpected approve payload: %#v", approvePayload)
	}
	firstViewerToken := approvePayload.Token

	list := doRequest(t, gw.Handler(), http.MethodGet, "/operator/devices", "")
	if list.Code != http.StatusOK {
		t.Fatalf("GET /operator/devices status = %d body=%s", list.Code, list.Body.String())
	}
	var devicesPayload devicesListResponse
	if err := json.Unmarshal(list.Body.Bytes(), &devicesPayload); err != nil {
		t.Fatalf("decode devices payload: %v", err)
	}
	if devicesPayload.Count != 2 {
		t.Fatalf("devices count = %d, want 2", devicesPayload.Count)
	}

	rotate := doRequest(t, gw.Handler(), http.MethodPost, "/operator/devices/ios-smoke/tokens/rotate", `{
		"role":"viewer",
		"scopes":["devices.read","nodes.read"]
	}`)
	if rotate.Code != http.StatusOK {
		t.Fatalf("POST /operator/devices/{id}/tokens/rotate status = %d body=%s", rotate.Code, rotate.Body.String())
	}
	var rotatePayload deviceTokenIssueResponse
	if err := json.Unmarshal(rotate.Body.Bytes(), &rotatePayload); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	if !rotatePayload.OK || rotatePayload.Token == "" || rotatePayload.Token == firstViewerToken {
		t.Fatalf("unexpected rotate payload: %#v", rotatePayload)
	}

	revoke := doRequest(t, gw.Handler(), http.MethodPost, "/operator/devices/ios-smoke/tokens/revoke", `{"role":"viewer"}`)
	if revoke.Code != http.StatusOK {
		t.Fatalf("POST /operator/devices/{id}/tokens/revoke status = %d body=%s", revoke.Code, revoke.Body.String())
	}
	if _, ok := store.GetToken("ios-smoke", deviceauth.RoleViewer); ok {
		t.Fatal("expected viewer token to be revoked")
	}
}

func newLiveFileBackedOperatorGateway(t *testing.T, cfg config.Config) (*Gateway, string) {
	t.Helper()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	watcher := config.NewWatcher(configPath, cfg, time.Hour)
	resolver, err := controloverlay.NewResolver(context.Background(), cfg, nil, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	watcher.OnReload(func(_ config.Config, next config.Config) error {
		return resolver.SetBase(context.Background(), next)
	})

	gw.SetConfigWatcher(watcher, configPath)
	gw.SetEffectiveConfigResolver(resolver)
	return gw, configPath
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestOperatorSurfaceSmokeSessionAuthFlow(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	authSessionCfg := defaultAuthSessionConfig(AuthSessionConfig{})
	authSessions := NewMemoryAuthSessionStore(authSessionCfg)
	t.Cleanup(authSessions.Close)

	gw.authChain = NewAuthChain(NewAuthSessionProvider(authSessions, authSessionCfg))
	gw.authSessionStore = authSessions
	gw.authSessionConfig = authSessionCfg

	session := authSessions.Create(&AuthIdentity{
		Subject:  "operator-user",
		Provider: sessionProviderName,
	})

	req := makeUnauthRequest(t, http.MethodGet, "/operator/setup/catalog", "")
	rec := captureResponse(t, gw.Handler(), req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /operator/setup/catalog without session status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = makeUnauthRequest(t, http.MethodGet, "/operator/setup/catalog", "")
	req.AddCookie(&http.Cookie{Name: authSessionCookieName(authSessionCfg.CookieName), Value: session.ID})
	rec = captureResponse(t, gw.Handler(), req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/setup/catalog with session status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = makeUnauthRequest(t, http.MethodPost, "/auth/logout", "")
	req.AddCookie(&http.Cookie{Name: authSessionCookieName(authSessionCfg.CookieName), Value: session.ID})
	rec = captureResponse(t, gw.Handler(), req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /auth/logout without csrf status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := authSessions.Get(session.ID); !ok {
		t.Fatal("expected auth session to remain after csrf failure")
	}

	req = makeUnauthRequest(t, http.MethodPost, "/auth/logout", "")
	req.AddCookie(&http.Cookie{Name: authSessionCookieName(authSessionCfg.CookieName), Value: session.ID})
	req.Header.Set(csrfHeaderName, session.CSRFToken)
	rec = captureResponse(t, gw.Handler(), req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth/logout with csrf status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := authSessions.Get(session.ID); ok {
		t.Fatal("expected auth session to be deleted after logout")
	}

	resultCookies := rec.Result().Cookies()
	if !hasCookieWithMaxAge(resultCookies, authSessionCookieName(authSessionCfg.CookieName), -1) {
		t.Fatalf("expected cleared auth session cookie, got %#v", resultCookies)
	}
	if !hasCookieWithMaxAge(resultCookies, csrfCookieName, -1) {
		t.Fatalf("expected cleared csrf cookie, got %#v", resultCookies)
	}

	req = makeUnauthRequest(t, http.MethodGet, "/operator/setup/catalog", "")
	req.AddCookie(&http.Cookie{Name: authSessionCookieName(authSessionCfg.CookieName), Value: session.ID})
	rec = captureResponse(t, gw.Handler(), req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /operator/setup/catalog after logout status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func hasCookieWithMaxAge(cookies []*http.Cookie, name string, wantMaxAge int) bool {
	for _, cookie := range cookies {
		if cookie != nil && cookie.Name == name && cookie.MaxAge == wantMaxAge {
			return true
		}
	}
	return false
}
