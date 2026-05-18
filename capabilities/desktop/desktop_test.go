package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
)

func TestCloseSessionKeepsTrackedHandleOnRemoteFailure(t *testing.T) {
	t.Parallel()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/desktop/v1":
			var req desktoptypes.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			switch req.Action {
			case desktoptypes.ActionCreateSession:
				_ = json.NewEncoder(w).Encode(desktoptypes.Response{
					OK:        true,
					SessionID: "desktop-session-1",
					Data: map[string]any{
						"session_id": "desktop-session-1",
					},
				})
			case desktoptypes.ActionCloseSession:
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(desktoptypes.Response{
					OK:    false,
					Error: "remote close failed",
				})
			default:
				t.Fatalf("unexpected action %q", req.Action)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer host.Close()

	capability := New(Config{BaseURL: host.URL})
	handle, err := capability.OpenSession(context.Background(), nil)
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	if handle.ID != "desktop-session-1" {
		t.Fatalf("handle.ID = %q", handle.ID)
	}

	err = capability.CloseSession(context.Background(), handle.ID)
	if err == nil || err.Error() != "desktop host: remote close failed" {
		t.Fatalf("CloseSession() error = %v", err)
	}

	sessions := capability.ListSessions()
	if len(sessions) != 1 || sessions[0].ID != handle.ID {
		t.Fatalf("ListSessions() = %#v", sessions)
	}
}

func TestCloseSessionRemovesTrackedHandleOnSuccess(t *testing.T) {
	t.Parallel()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/desktop/v1":
			var req desktoptypes.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(desktoptypes.Response{
				OK:        true,
				SessionID: "desktop-session-1",
				Data: map[string]any{
					"session_id": "desktop-session-1",
					"closed":     req.Action == desktoptypes.ActionCloseSession,
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer host.Close()

	capability := New(Config{BaseURL: host.URL})
	handle, err := capability.OpenSession(context.Background(), nil)
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	if err := capability.CloseSession(context.Background(), handle.ID); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if sessions := capability.ListSessions(); len(sessions) != 0 {
		t.Fatalf("ListSessions() = %#v", sessions)
	}
}

func TestManifestIncludesExpandedDesktopOperations(t *testing.T) {
	t.Parallel()

	manifest := New(Config{}).Manifest()
	names := make([]string, 0, len(manifest.Operations))
	for _, op := range manifest.Operations {
		names = append(names, op.Name)
	}
	for _, want := range []string{
		"describe_host",
		"focus_window",
		"list_apps",
		"list_windows",
		"list_commands",
		"invoke_command",
		"list_driver_actions",
		"invoke_driver_action",
		"capture_tree",
		"mouse_click",
		"click_element",
		"set_element_value",
		"clear_element",
		"get_element_value",
		"assert_element",
		"find_text",
		"click_text",
	} {
		if !slices.Contains(names, want) {
			t.Fatalf("manifest missing %q in %#v", want, names)
		}
	}
	if !slices.Contains(manifest.ArtifactKinds, "desktop.capture_tree") {
		t.Fatalf("ArtifactKinds = %#v", manifest.ArtifactKinds)
	}

	var describeHost, listCommands, invokeCommand, listDriverActions, invokeDriverAction *captypes.OperationSpec

	var openApp, focusApp, focusWindow *captypes.OperationSpec
	for i := range manifest.Operations {
		switch manifest.Operations[i].Name {
		case "open_app":
			openApp = &manifest.Operations[i]
		case "focus_app":
			focusApp = &manifest.Operations[i]
		case "focus_window":
			focusWindow = &manifest.Operations[i]
		case "describe_host":
			describeHost = &manifest.Operations[i]
		case "list_commands":
			listCommands = &manifest.Operations[i]
		case "invoke_command":
			invokeCommand = &manifest.Operations[i]
		case "list_driver_actions":
			listDriverActions = &manifest.Operations[i]
		case "invoke_driver_action":
			invokeDriverAction = &manifest.Operations[i]
		}
	}
	for name, op := range map[string]*captypes.OperationSpec{
		"open_app":     openApp,
		"focus_app":    focusApp,
		"focus_window": focusWindow,
	} {
		if op == nil {
			t.Fatalf("operation %q missing", name)
		}
		properties, _ := op.InputSchema["properties"].(map[string]any)
		if _, ok := properties["wait_until"]; !ok {
			t.Fatalf("%s input schema missing wait_until: %#v", name, op.InputSchema)
		}
		if _, ok := properties["timeout_ms"]; !ok {
			t.Fatalf("%s input schema missing timeout_ms: %#v", name, op.InputSchema)
		}
		outputProperties, _ := op.OutputSchema["properties"].(map[string]any)
		for _, field := range []string{"wait_until", "ready_state", "ready", "waited_ms"} {
			if _, ok := outputProperties[field]; !ok {
				t.Fatalf("%s output schema missing %q: %#v", name, field, op.OutputSchema)
			}
		}
	}
	if describeHost == nil {
		t.Fatal("operation \"describe_host\" missing")
	}
	if listCommands == nil {
		t.Fatal("operation \"list_commands\" missing")
	}
	if invokeCommand == nil {
		t.Fatal("operation \"invoke_command\" missing")
	}
	if listDriverActions == nil {
		t.Fatal("operation \"list_driver_actions\" missing")
	}
	if invokeDriverAction == nil {
		t.Fatal("operation \"invoke_driver_action\" missing")
	}
	describeHostOutput, _ := describeHost.OutputSchema["properties"].(map[string]any)
	if _, ok := describeHostOutput["profile"]; !ok {
		t.Fatalf("describe_host output schema missing profile: %#v", describeHost.OutputSchema)
	}
	listCommandsOutput, _ := listCommands.OutputSchema["properties"].(map[string]any)
	for _, field := range []string{"app", "commands", "count"} {
		if _, ok := listCommandsOutput[field]; !ok {
			t.Fatalf("list_commands output schema missing %q: %#v", field, listCommands.OutputSchema)
		}
	}
	listCommandsInput, _ := listCommands.InputSchema["properties"].(map[string]any)
	for _, field := range []string{"include_system", "include_unsafe"} {
		if _, ok := listCommandsInput[field]; !ok {
			t.Fatalf("list_commands input schema missing %q: %#v", field, listCommands.InputSchema)
		}
	}
	invokeCommandInput, _ := invokeCommand.InputSchema["properties"].(map[string]any)
	for _, field := range []string{"command_id", "menu_path", "title", "transport", "allow_unsafe"} {
		if _, ok := invokeCommandInput[field]; !ok {
			t.Fatalf("invoke_command input schema missing %q: %#v", field, invokeCommand.InputSchema)
		}
	}
	listDriverActionsInput, _ := listDriverActions.InputSchema["properties"].(map[string]any)
	for _, field := range []string{"app", "bundle_id", "driver_id"} {
		if _, ok := listDriverActionsInput[field]; !ok {
			t.Fatalf("list_driver_actions input schema missing %q: %#v", field, listDriverActions.InputSchema)
		}
	}
	listDriverActionsOutput, _ := listDriverActions.OutputSchema["properties"].(map[string]any)
	for _, field := range []string{"driver_id", "app_family", "support_tier", "actions", "count"} {
		if _, ok := listDriverActionsOutput[field]; !ok {
			t.Fatalf("list_driver_actions output schema missing %q: %#v", field, listDriverActions.OutputSchema)
		}
	}
	invokeDriverActionInput, _ := invokeDriverAction.InputSchema["properties"].(map[string]any)
	for _, field := range []string{"driver_id", "semantic_action", "arguments"} {
		if _, ok := invokeDriverActionInput[field]; !ok {
			t.Fatalf("invoke_driver_action input schema missing %q: %#v", field, invokeDriverAction.InputSchema)
		}
	}
	invokeDriverActionOutput, _ := invokeDriverAction.OutputSchema["properties"].(map[string]any)
	for _, field := range []string{"semantic_action", "driver_id", "invoked", "action_status", "verification_mode"} {
		if _, ok := invokeDriverActionOutput[field]; !ok {
			t.Fatalf("invoke_driver_action output schema missing %q: %#v", field, invokeDriverAction.OutputSchema)
		}
	}

	var screenshot *captypes.OperationSpec
	for i := range manifest.Operations {
		if manifest.Operations[i].Name == "screenshot" {
			screenshot = &manifest.Operations[i]
			break
		}
	}
	if screenshot == nil {
		t.Fatal("operation \"screenshot\" missing")
	}
	screenshotInput, _ := screenshot.InputSchema["properties"].(map[string]any)
	for _, field := range []string{"app", "bundle_id", "title_contains", "window_index"} {
		if _, ok := screenshotInput[field]; !ok {
			t.Fatalf("screenshot input schema missing %q: %#v", field, screenshot.InputSchema)
		}
	}
	screenshotOutput, _ := screenshot.OutputSchema["properties"].(map[string]any)
	for _, field := range []string{"scope", "capture_mode", "app", "bundle_id", "title", "window_index", "window_id"} {
		if _, ok := screenshotOutput[field]; !ok {
			t.Fatalf("screenshot output schema missing %q: %#v", field, screenshot.OutputSchema)
		}
	}

	var findText, clickText *captypes.OperationSpec
	for i := range manifest.Operations {
		switch manifest.Operations[i].Name {
		case "find_text":
			findText = &manifest.Operations[i]
		case "click_text":
			clickText = &manifest.Operations[i]
		}
	}
	if findText == nil || clickText == nil {
		t.Fatalf("find_text=%v click_text=%v", findText != nil, clickText != nil)
	}
	findTextOutput, _ := findText.OutputSchema["properties"].(map[string]any)
	if _, ok := findTextOutput["capture_mode"]; !ok {
		t.Fatalf("find_text output schema missing capture_mode: %#v", findText.OutputSchema)
	}
	clickTextOutput, _ := clickText.OutputSchema["properties"].(map[string]any)
	if _, ok := clickTextOutput["action_status"]; !ok {
		t.Fatalf("click_text output schema missing action_status: %#v", clickText.OutputSchema)
	}
}

func TestInvokeDriverActionAttachesRoutingEvidenceAndTracksSessionState(t *testing.T) {
	t.Parallel()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/desktop/v1":
			var req desktoptypes.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			switch req.Action {
			case desktoptypes.ActionCreateSession:
				_ = json.NewEncoder(w).Encode(desktoptypes.Response{
					OK:        true,
					SessionID: "desktop-session-1",
					Data: map[string]any{
						"session_id": "desktop-session-1",
					},
				})
			case desktoptypes.ActionInvokeDriverAction:
				if req.Params["route_profile_id"] != "douyin.desktop.macos" {
					t.Fatalf("route_profile_id = %#v", req.Params["route_profile_id"])
				}
				if req.Params["preferred_transport"] != capprofile.TransportSemanticUIAction {
					t.Fatalf("preferred_transport = %#v", req.Params["preferred_transport"])
				}
				if req.Params["verification_policy"] != "scene_transition" {
					t.Fatalf("verification_policy = %#v", req.Params["verification_policy"])
				}
				_ = json.NewEncoder(w).Encode(desktoptypes.Response{
					OK: true,
					Data: map[string]any{
						"semantic_action": "search.submit",
						"driver_id":       "douyin.desktop.macos",
						"app":             "抖音",
						"bundle_id":       "com.bytedance.douyin.desktop",
						"invoked":         true,
						"verified":        true,
						"action_status":   "verified",
					},
				})
			default:
				t.Fatalf("unexpected action %q", req.Action)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer host.Close()

	capability := New(Config{BaseURL: host.URL})
	handle, err := capability.OpenSession(context.Background(), nil)
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}

	result, err := capability.Invoke(context.Background(), captypes.InvokeRequest{
		Operation: desktoptypes.ActionInvokeDriverAction,
		SessionID: handle.ID,
		Params: map[string]any{
			"app":             "抖音",
			"bundle_id":       "com.bytedance.douyin.desktop",
			"semantic_action": "search.submit",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	trace, ok := capprofile.DecodeExecutionTrace(result.Metadata)
	if !ok {
		t.Fatalf("result.Metadata = %#v", result.Metadata)
	}
	if trace.ProfileID != "douyin.desktop.macos" {
		t.Fatalf("trace.ProfileID = %q", trace.ProfileID)
	}
	if trace.ChosenTransport != capprofile.TransportSemanticUIAction {
		t.Fatalf("trace.ChosenTransport = %q", trace.ChosenTransport)
	}
	evidence, _ := result.Data["evidence"].(map[string]any)
	if evidence == nil || evidence["routing"] == nil {
		t.Fatalf("evidence = %#v", result.Data["evidence"])
	}
	sessions := capability.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("ListSessions() count = %d", len(sessions))
	}
	if sessions[0].Metadata["profile_id"] != "douyin.desktop.macos" {
		t.Fatalf("session metadata = %#v", sessions[0].Metadata)
	}
	if sessions[0].Metadata["last_transport"] != capprofile.TransportSemanticUIAction {
		t.Fatalf("session last_transport = %#v", sessions[0].Metadata["last_transport"])
	}
}
