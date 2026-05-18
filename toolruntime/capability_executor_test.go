package toolruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	registry "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
)

type stubBrowserCapability struct {
	manifest        captypes.Manifest
	health          captypes.Health
	capabilityName  string
	openSessionID   string
	lastOpenParams  map[string]any
	lastCloseID     string
	lastInvoke      captypes.InvokeRequest
	invokeResult    *captypes.InvokeResult
	invokeErr       error
	openSessionErr  error
	closeSessionErr error
}

func (s *stubBrowserCapability) Manifest() captypes.Manifest { return s.manifest }

func (s *stubBrowserCapability) Health(context.Context) captypes.Health {
	if s.health.Status == "" {
		return captypes.Health{Status: captypes.StatusReady}
	}
	return s.health
}

func (s *stubBrowserCapability) Invoke(_ context.Context, req captypes.InvokeRequest) (*captypes.InvokeResult, error) {
	s.lastInvoke = req
	if s.invokeErr != nil {
		return nil, s.invokeErr
	}
	if s.invokeResult != nil {
		return s.invokeResult, nil
	}
	return &captypes.InvokeResult{OK: true}, nil
}

func (s *stubBrowserCapability) OpenSession(_ context.Context, params map[string]any) (*captypes.SessionHandle, error) {
	s.lastOpenParams = supportmaps.Clone(params)
	if s.openSessionErr != nil {
		return nil, s.openSessionErr
	}
	return &captypes.SessionHandle{
		ID:         s.openSessionID,
		Capability: capabilityNameForTest(s.capabilityName),
		CreatedAt:  time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC),
	}, nil
}

func (s *stubBrowserCapability) CloseSession(_ context.Context, sessionID string) error {
	s.lastCloseID = sessionID
	return s.closeSessionErr
}

func TestCapabilityExecutorExposesBrowserTools(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	capability := &stubBrowserCapability{
		openSessionID:  "browser-session-1",
		manifest:       browserManifestForTest(),
		capabilityName: "browser",
	}
	if err := reg.Register(capability); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	exec := NewCapabilityExecutor(reg, artifact.NewInMemoryStore())
	if exec == nil {
		t.Fatal("expected capability executor")
	}

	defs := exec.ToolDefinitions(nil)
	if len(defs) != 9 {
		t.Fatalf("len(ToolDefinitions) = %d", len(defs))
	}
	if defs[0].Name != "browser.click" {
		t.Fatalf("defs[0].Name = %q", defs[0].Name)
	}
	for _, legacy := range []string{"browser.create_session", "browser.close_session", "browser.wait_for", "browser.list_tabs"} {
		for _, def := range defs {
			if def.Name == legacy {
				t.Fatalf("legacy tool name %q should not be listed in canonical tool definitions", legacy)
			}
		}
	}
	if _, ok := exec.ResolveTool(nil, "browser.navigate"); !ok {
		t.Fatal("ResolveTool(browser.navigate) = false")
	}
	if _, ok := exec.ResolveTool(nil, "browser.create_session"); !ok {
		t.Fatal("ResolveTool(browser.create_session) = false")
	}
}

func TestCapabilityExecutorCreateSession(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	capability := &stubBrowserCapability{
		openSessionID:  "browser-session-1",
		manifest:       browserManifestForTest(),
		capabilityName: "browser",
	}
	if err := reg.Register(capability); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	exec := NewCapabilityExecutor(reg, artifact.NewInMemoryStore())
	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-1",
		Name: "browser.open",
		Input: map[string]any{
			"url": "https://example.com",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	if capability.lastOpenParams["url"] != "https://example.com" {
		t.Fatalf("lastOpenParams = %#v", capability.lastOpenParams)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, results[0].Content)
	}
	if payload["session_id"] != "browser-session-1" {
		t.Fatalf("session_id = %#v", payload["session_id"])
	}
}

func TestCapabilityExecutorPreservesBatchSlotsOnPerCallFailure(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	capability := &stubBrowserCapability{
		openSessionID:  "browser-session-1",
		manifest:       browserManifestForTest(),
		capabilityName: "browser",
	}
	if err := reg.Register(capability); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	exec := NewCapabilityExecutor(reg, artifact.NewInMemoryStore())
	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{
		{ID: "missing", Name: "browser.missing"},
		{ID: "open", Name: "browser.open"},
	})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Error == nil || results[0].Error.Message == "" {
		t.Fatalf("expected error result in slot 0, got %#v", results[0])
	}
	if results[1].Error != nil {
		t.Fatalf("expected success result in slot 1, got %#v", results[1])
	}
}

func TestCapabilityExecutorSupportsLegacyBrowserSessionAlias(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	capability := &stubBrowserCapability{
		openSessionID:  "browser-session-1",
		manifest:       browserManifestForTest(),
		capabilityName: "browser",
	}
	if err := reg.Register(capability); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	exec := NewCapabilityExecutor(reg, artifact.NewInMemoryStore())
	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{ID: "run-legacy"}, &agent.Session{ID: "sess-legacy"}, []agent.ToolCall{{
		ID:   "call-legacy-open",
		Name: "browser.create_session",
		Input: map[string]any{
			"url": "https://example.com/legacy",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	if capability.lastOpenParams["url"] != "https://example.com/legacy" {
		t.Fatalf("lastOpenParams = %#v", capability.lastOpenParams)
	}
}

func TestCapabilityExecutorStoresScreenshotArtifact(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	capability := &stubBrowserCapability{
		openSessionID:  "browser-session-1",
		manifest:       browserManifestForTest(),
		capabilityName: "browser",
		invokeResult: &captypes.InvokeResult{
			OK: true,
			Data: map[string]any{
				"mime_type":      "image/png",
				"full_page":      true,
				"content_base64": base64.StdEncoding.EncodeToString([]byte("png-bytes")),
			},
		},
	}
	if err := reg.Register(capability); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	store := artifact.NewInMemoryStore()
	exec := NewCapabilityExecutor(reg, store)
	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-2",
		Name: "browser.screenshot",
		Input: map[string]any{
			"session_id": "browser-session-1",
			"full_page":  true,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	if results[0].ArtifactURI == "" {
		t.Fatal("expected artifact uri")
	}
	if capability.lastInvoke.Operation != "screenshot" || capability.lastInvoke.SessionID != "browser-session-1" {
		t.Fatalf("lastInvoke = %#v", capability.lastInvoke)
	}
	body, contentType, err := store.Read(context.Background(), results[0].ArtifactURI)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(body) != "png-bytes" {
		t.Fatalf("artifact body = %q", string(body))
	}
	if contentType != "image/png" {
		t.Fatalf("contentType = %q", contentType)
	}
}

func TestCapabilityExecutorPreservesCapabilityMetadataOnSpecialRenderPaths(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	capability := &stubBrowserCapability{
		openSessionID:  "browser-session-1",
		manifest:       browserManifestForTest(),
		capabilityName: "browser",
		invokeResult: &captypes.InvokeResult{
			OK: true,
			Data: map[string]any{
				"mime_type":      "image/png",
				"full_page":      true,
				"content_base64": base64.StdEncoding.EncodeToString([]byte("png-bytes")),
				"transport_telemetry": map[string]any{
					"profile_id":          "douyin.site",
					"chosen_transport":    capprofile.TransportBrowserNavigation,
					"preferred_transport": capprofile.TransportBrowserNavigation,
				},
			},
			Metadata: map[string]any{
				capprofile.MetadataKeyExecutionTrace: capprofile.ExecutionTrace{
					Surface:            "browser",
					Capability:         "browser",
					Operation:          "screenshot",
					ProfileID:          "douyin.site",
					ChosenTransport:    capprofile.TransportBrowserSnapshot,
					ExecutionMode:      capprofile.ModeDeterministic,
					PreferredTransport: capprofile.TransportBrowserSnapshot,
				}.MetadataMap(),
			},
		},
	}
	if err := reg.Register(capability); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	exec := NewCapabilityExecutor(reg, artifact.NewInMemoryStore())
	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-special-1",
		Name: "browser.screenshot",
		Input: map[string]any{
			"session_id": "browser-session-1",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	trace, ok := capprofile.DecodeExecutionTrace(results[0].Metadata)
	if !ok {
		t.Fatalf("results[0].Metadata = %#v", results[0].Metadata)
	}
	if trace.ProfileID != "douyin.site" {
		t.Fatalf("trace.ProfileID = %q", trace.ProfileID)
	}
	telemetry, _ := results[0].Structured["transport_telemetry"].(map[string]any)
	if telemetry == nil || telemetry["profile_id"] != "douyin.site" {
		t.Fatalf("structured transport_telemetry = %#v", results[0].Structured["transport_telemetry"])
	}
}

func TestCapabilityExecutorHidesUnavailableBrowserTools(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	capability := &stubBrowserCapability{
		manifest:       browserManifestForTest(),
		capabilityName: "browser",
		health: captypes.Health{
			Status:  captypes.StatusUnavailable,
			Message: "browser host is down",
		},
	}
	if err := reg.Register(capability); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	exec := NewCapabilityExecutor(reg, artifact.NewInMemoryStore())
	if exec == nil {
		t.Fatal("expected capability executor")
	}
	defs := exec.ToolDefinitions(nil)
	if len(defs) == 0 {
		t.Fatal("expected blocked capability tools to remain visible in the catalog")
	}
	if defs[0].Availability.Status != agent.AvailabilityBlocked {
		t.Fatalf("availability = %q", defs[0].Availability.Status)
	}
	resolved, ok := exec.ResolveTool(nil, "browser.navigate")
	if !ok {
		t.Fatal("ResolveTool(browser.navigate) = false")
	}
	if resolved.Descriptor.Availability.Status != agent.AvailabilityBlocked {
		t.Fatalf("resolved availability = %q", resolved.Descriptor.Availability.Status)
	}

	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-1",
		Name: "browser.navigate",
		Input: map[string]any{
			"session_id": "browser-session-1",
			"url":        "https://example.com",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 || results[0].Error == nil || results[0].Error.Message != "browser.navigate is unavailable: browser host is down" {
		t.Fatalf("results = %#v", results)
	}
}

func TestCapabilityExecutorExposesDesktopToolsAndStoresDesktopArtifacts(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	capability := &stubBrowserCapability{
		openSessionID:  "desktop-session-1",
		manifest:       desktopManifestForTest(),
		capabilityName: "desktop",
	}
	if err := reg.Register(capability); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	store := artifact.NewInMemoryStore()
	exec := NewCapabilityExecutor(reg, store)
	if exec == nil {
		t.Fatal("expected capability executor")
	}
	if _, ok := exec.ResolveTool(nil, "desktop.open_app"); !ok {
		t.Fatal("ResolveTool(desktop.open_app) = false")
	}

	capability.invokeResult = &captypes.InvokeResult{
		OK: true,
		Data: map[string]any{
			"mime_type":      "image/png",
			"content_base64": base64.StdEncoding.EncodeToString([]byte("desktop-png")),
		},
	}
	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-3",
		Name: "desktop.screenshot",
		Input: map[string]any{
			"session_id": "desktop-session-1",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	if results[0].ArtifactURI == "" {
		t.Fatal("expected artifact uri")
	}
	body, contentType, err := store.Read(context.Background(), results[0].ArtifactURI)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(body) != "desktop-png" {
		t.Fatalf("artifact body = %q", string(body))
	}
	if contentType != "image/png" {
		t.Fatalf("contentType = %q", contentType)
	}

	capability.invokeResult = &captypes.InvokeResult{
		OK: true,
		Data: map[string]any{
			"frontmost_app": map[string]any{
				"name": "Safari",
			},
			"apps": []any{
				map[string]any{"name": "Safari"},
				map[string]any{"name": "Finder"},
			},
		},
	}
	treeResults, err := exec.ExecuteBatch(context.Background(), &agent.Run{ID: "run-2"}, &agent.Session{ID: "sess-2"}, []agent.ToolCall{{
		ID:   "call-4",
		Name: "desktop.capture_tree",
		Input: map[string]any{
			"session_id": "desktop-session-1",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(capture_tree) error = %v", err)
	}
	if len(treeResults) != 1 {
		t.Fatalf("len(treeResults) = %d", len(treeResults))
	}
	if treeResults[0].ArtifactURI == "" {
		t.Fatal("expected capture_tree artifact uri")
	}
	treeBody, treeContentType, err := store.Read(context.Background(), treeResults[0].ArtifactURI)
	if err != nil {
		t.Fatalf("Read(capture_tree) error = %v", err)
	}
	if treeContentType != "application/json" {
		t.Fatalf("treeContentType = %q", treeContentType)
	}
	if !json.Valid(treeBody) {
		t.Fatalf("capture_tree artifact is not valid json: %s", string(treeBody))
	}
}

func TestCapabilityExecutorAllowsSessionlessDesktopListApps(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	capability := &stubBrowserCapability{
		manifest:       desktopManifestForTest(),
		capabilityName: "desktop",
		invokeResult: &captypes.InvokeResult{
			OK: true,
			Data: map[string]any{
				"apps": []any{
					map[string]any{"name": "Safari"},
				},
			},
		},
	}
	if err := reg.Register(capability); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	exec := NewCapabilityExecutor(reg, artifact.NewInMemoryStore())
	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{ID: "run-3"}, &agent.Session{ID: "sess-3"}, []agent.ToolCall{{
		ID:   "call-5",
		Name: "desktop.list_apps",
		Input: map[string]any{
			"include_windows": true,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(list_apps) error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	if capability.lastInvoke.SessionID != "" {
		t.Fatalf("lastInvoke.SessionID = %q, want empty", capability.lastInvoke.SessionID)
	}
}

func browserManifestForTest() captypes.Manifest {
	return captypes.Manifest{
		Name:          "browser",
		Kind:          captypes.KindSession,
		SessionScoped: true,
		Operations: []captypes.OperationSpec{
			{Name: "create_session", Description: "Create session", SideEffectClass: "external_write"},
			{Name: "close_session", Description: "Close session", SideEffectClass: "external_write"},
			{Name: "navigate", Description: "Navigate", SideEffectClass: "external_write"},
			{Name: "click", Description: "Click", SideEffectClass: "external_write"},
			{Name: "type", Description: "Type", SideEffectClass: "external_write"},
			{Name: "wait_for", Description: "Wait", SideEffectClass: "read"},
			{Name: "snapshot", Description: "Snapshot", SideEffectClass: "read"},
			{Name: "screenshot", Description: "Screenshot", SideEffectClass: "read"},
			{Name: "list_tabs", Description: "List tabs", SideEffectClass: "read"},
		},
		ApprovalPolicy: "policy",
	}
}

func desktopManifestForTest() captypes.Manifest {
	return captypes.Manifest{
		Name:          "desktop",
		Kind:          captypes.KindSession,
		SessionScoped: true,
		Operations: []captypes.OperationSpec{
			{Name: "create_session", Description: "Create session", SideEffectClass: "external_write"},
			{Name: "close_session", Description: "Close session", SideEffectClass: "external_write"},
			{Name: "open_app", Description: "Open app", SideEffectClass: "external_write"},
			{Name: "focus_app", Description: "Focus app", SideEffectClass: "external_write"},
			{Name: "focus_window", Description: "Focus window", SideEffectClass: "external_write"},
			{Name: "list_apps", Description: "List apps", SideEffectClass: "read", SessionOptional: true},
			{Name: "list_windows", Description: "List windows", SideEffectClass: "read"},
			{Name: "type_text", Description: "Type text", SideEffectClass: "external_write"},
			{Name: "hotkey", Description: "Hotkey", SideEffectClass: "external_write"},
			{Name: "screenshot", Description: "Screenshot", SideEffectClass: "read"},
			{Name: "clipboard_read", Description: "Clipboard read", SideEffectClass: "read"},
			{Name: "capture_tree", Description: "Capture tree", SideEffectClass: "read"},
			{Name: "clipboard_write", Description: "Clipboard write", SideEffectClass: "external_write"},
		},
		ApprovalPolicy: "policy",
	}
}

func capabilityNameForTest(name string) string {
	if name == "" {
		return "browser"
	}
	return name
}
