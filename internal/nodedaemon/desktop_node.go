package nodedaemon

import (
	"context"
	"fmt"
	"strings"

	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/nodeclient"
)

type nodeRegistrar interface {
	Register(command string, handler nodeclient.Handler)
}

type desktopInvoker interface {
	Do(ctx context.Context, req desktoptypes.Request) (*desktoptypes.Response, error)
	Health(ctx context.Context) error
}

type DesktopNodeConfig struct {
	DeviceID     string
	DeviceName   string
	Platform     string
	DeviceFamily string
	Version      string
	ListenAddr   string
}

type desktopNodeInvocation struct {
	request       desktoptypes.Request
	sessionParams map[string]any
}

var desktopCommandActions = map[string]string{
	"desktop.list_apps":       desktoptypes.ActionListApps,
	"desktop.find_element":    desktoptypes.ActionFindElement,
	"desktop.find_text":       desktoptypes.ActionFindText,
	"desktop.click_text":      desktoptypes.ActionClickText,
	"desktop.list_windows":    desktoptypes.ActionListWindows,
	"desktop.open_app":        desktoptypes.ActionOpenApp,
	"desktop.focus_app":       desktoptypes.ActionFocusApp,
	"desktop.focus_window":    desktoptypes.ActionFocusWindow,
	"desktop.type_text":       desktoptypes.ActionTypeText,
	"desktop.hotkey":          desktoptypes.ActionHotkey,
	"desktop.screenshot":      desktoptypes.ActionScreenshot,
	"desktop.screen_record":   desktoptypes.ActionScreenRecord,
	"desktop.capture_tree":    desktoptypes.ActionCaptureTree,
	"desktop.clipboard_read":  desktoptypes.ActionClipboardRead,
	"desktop.clipboard_write": desktoptypes.ActionClipboardWrite,
	"desktop.mouse_move":      desktoptypes.ActionMouseMove,
	"desktop.mouse_click":     desktoptypes.ActionMouseClick,
	"desktop.scroll":          desktoptypes.ActionScroll,
}

func DesktopNodeCommands(platform string) []string {
	commands := []string{
		"device.info",
		"device.status",
		"desktop.proxy",
		"desktop.list_apps",
		"desktop.open_app",
		"desktop.focus_window",
		"desktop.type_text",
		"desktop.hotkey",
		"desktop.screenshot",
		"desktop.clipboard_read",
		"desktop.clipboard_write",
		"desktop.mouse_move",
		"desktop.mouse_click",
		"desktop.find_element",
		"desktop.find_text",
		"desktop.click_text",
	}
	switch normalizeDesktopPlatform(platform) {
	case "macOS":
		commands = append(commands,
			"desktop.focus_app",
			"desktop.list_windows",
			"desktop.capture_tree",
			"desktop.screen_record",
			"desktop.scroll",
		)
	}
	return commands
}

func RegisterDesktopNodeHandlers(registry nodeRegistrar, client desktopInvoker, cfg DesktopNodeConfig) {
	if registry == nil || client == nil {
		return
	}

	platform := defaultPlatform(cfg.Platform)
	deviceFamily := normalize.FirstNonEmpty(cfg.DeviceFamily, "desktop")

	registry.Register("device.info", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{
			"device_id":     cfg.DeviceID,
			"name":          cfg.DeviceName,
			"platform":      platform,
			"device_family": deviceFamily,
			"daemon":        "desktopd",
			"version":       cfg.Version,
			"listen":        cfg.ListenAddr,
			"commands":      DesktopNodeCommands(platform),
		}, nil
	})

	registry.Register("device.status", func(ctx context.Context, _ map[string]any) (map[string]any, error) {
		if err := client.Health(ctx); err != nil {
			return nil, err
		}
		return map[string]any{
			"ok":      true,
			"daemon":  "desktopd",
			"listen":  cfg.ListenAddr,
			"version": cfg.Version,
		}, nil
	})

	registry.Register("desktop.proxy", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		invocation, err := desktopNodeRequest("desktop.proxy", params)
		if err != nil {
			return nil, err
		}
		return invokeDesktopNodeRequest(ctx, client, invocation)
	})

	for _, command := range DesktopNodeCommands(platform) {
		if command == "device.info" || command == "device.status" || command == "desktop.proxy" {
			continue
		}
		commandName := command
		registry.Register(commandName, func(ctx context.Context, params map[string]any) (map[string]any, error) {
			invocation, err := desktopNodeRequest(commandName, params)
			if err != nil {
				return nil, err
			}
			return invokeDesktopNodeRequest(ctx, client, invocation)
		})
	}
}

func desktopNodeRequest(command string, params map[string]any) (desktopNodeInvocation, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return desktopNodeInvocation{}, fmt.Errorf("command is required")
	}
	if params == nil {
		params = map[string]any{}
	}

	action := desktopCommandActions[command]
	if command == "desktop.proxy" {
		action = strings.TrimSpace(normalize.String(params["action"]))
	}
	if action == "" {
		return desktopNodeInvocation{}, fmt.Errorf("action is required")
	}

	return desktopNodeInvocation{
		request: desktoptypes.Request{
			Action:    action,
			SessionID: strings.TrimSpace(normalize.String(params["session_id"])),
			Params:    desktopActionParams(params),
		},
		sessionParams: desktopSessionParams(params),
	}, nil
}

func desktopActionParams(params map[string]any) map[string]any {
	if nested, ok := params["params"].(map[string]any); ok {
		return cloneAnyMap(nested)
	}

	out := make(map[string]any, len(params))
	for key, value := range params {
		switch key {
		case "action", "session_id", "session", "session_params":
			continue
		default:
			out[key] = value
		}
	}
	return out
}

func desktopSessionParams(params map[string]any) map[string]any {
	if nested, ok := params["session"].(map[string]any); ok {
		return cloneAnyMap(nested)
	}
	if nested, ok := params["session_params"].(map[string]any); ok {
		return cloneAnyMap(nested)
	}
	workspace := strings.TrimSpace(normalize.String(params["workspace"]))
	if workspace == "" {
		return nil
	}
	return map[string]any{"workspace": workspace}
}

func invokeDesktopNodeRequest(ctx context.Context, client desktopInvoker, invocation desktopNodeInvocation) (map[string]any, error) {
	req := invocation.request
	if strings.TrimSpace(req.Action) == "" {
		return nil, fmt.Errorf("action is required")
	}
	switch req.Action {
	case desktoptypes.ActionCreateSession, desktoptypes.ActionCloseSession:
		return doDesktopRequest(ctx, client, req)
	}

	ephemeral := strings.TrimSpace(req.SessionID) == ""
	if ephemeral {
		createResp, err := client.Do(ctx, desktoptypes.Request{
			Action: desktoptypes.ActionCreateSession,
			Params: invocation.sessionParams,
		})
		if err != nil {
			return nil, err
		}
		if !createResp.OK {
			return nil, desktopResponseError(desktoptypes.ActionCreateSession, createResp)
		}
		sessionID := resolveDesktopSessionID(createResp)
		if sessionID == "" {
			return nil, fmt.Errorf("create_session returned an empty session_id")
		}
		req.SessionID = sessionID
		defer func() {
			_, _ = client.Do(context.Background(), desktoptypes.Request{
				Action:    desktoptypes.ActionCloseSession,
				SessionID: sessionID,
			})
		}()
	}
	return doDesktopRequest(ctx, client, req)
}

func doDesktopRequest(ctx context.Context, client desktopInvoker, req desktoptypes.Request) (map[string]any, error) {
	resp, err := client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, desktopResponseError(req.Action, resp)
	}

	result := map[string]any{"ok": true}
	for key, value := range resp.Data {
		result[key] = value
	}
	if resp.ArtifactRef != "" {
		result["artifact_ref"] = resp.ArtifactRef
	}
	if resp.SessionID != "" {
		result["session_id"] = resp.SessionID
	} else if req.SessionID != "" && req.Action == desktoptypes.ActionCloseSession {
		result["session_id"] = req.SessionID
	}
	return result, nil
}

func resolveDesktopSessionID(resp *desktoptypes.Response) string {
	if resp == nil {
		return ""
	}
	if strings.TrimSpace(resp.SessionID) != "" {
		return strings.TrimSpace(resp.SessionID)
	}
	return strings.TrimSpace(normalize.String(resp.Data["session_id"]))
}

func desktopResponseError(action string, resp *desktoptypes.Response) error {
	if resp == nil {
		return fmt.Errorf("%s failed", action)
	}
	message := strings.TrimSpace(resp.Error)
	if message == "" {
		message = strings.TrimSpace(normalize.String(resp.Data["error"]))
	}
	if message == "" {
		message = "request failed"
	}
	return fmt.Errorf("%s: %s", action, message)
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func normalizeDesktopPlatform(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "darwin", "macos", "mac":
		return "macOS"
	case "windows", "win":
		return "Windows"
	case "linux":
		return "Linux"
	default:
		return strings.TrimSpace(platform)
	}
}
