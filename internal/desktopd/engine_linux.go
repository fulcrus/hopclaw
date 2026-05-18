//go:build linux

package desktopd

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
)

// displayServer indicates whether the session targets X11 or Wayland.
type displayServer int

const (
	displayX11 displayServer = iota
	displayWayland
)

// ---------------------------------------------------------------------------
// LinuxEngine
// ---------------------------------------------------------------------------

type LinuxEngine struct{}

type linuxSession struct {
	id        string
	workspace string
	display   displayServer
	mu        sync.Mutex
	closed    bool
}

func NewDefaultEngine() (Engine, error) {
	return &LinuxEngine{}, nil
}

func (e *LinuxEngine) OpenSession(_ context.Context, spec OpenSessionSpec) (Session, error) {
	ds := displayX11
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		ds = displayWayland
	}
	return &linuxSession{
		id:        spec.ID,
		workspace: stringParam(spec.Params, "workspace"),
		display:   ds,
	}, nil
}

func (s *linuxSession) ID() string { return s.id }

func (s *linuxSession) Handle(ctx context.Context, req desktoptypes.Request) (*desktoptypes.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("desktop session is closed")
	}

	switch strings.TrimSpace(req.Action) {
	case desktoptypes.ActionListApps:
		return s.handleListApps(ctx)
	case desktoptypes.ActionFocusWindow:
		return s.handleFocusWindow(ctx, req.Params)
	case desktoptypes.ActionTypeText:
		return s.handleTypeText(ctx, req.Params)
	case desktoptypes.ActionHotkey:
		return s.handleHotkey(ctx, req.Params)
	case desktoptypes.ActionScreenshot:
		return s.handleScreenshot(ctx)
	case desktoptypes.ActionClipboardRead:
		return s.handleClipboardRead(ctx)
	case desktoptypes.ActionClipboardWrite:
		return s.handleClipboardWrite(ctx, req.Params)
	case desktoptypes.ActionMouseMove:
		return s.handleMouseMove(ctx, req.Params)
	case desktoptypes.ActionMouseClick:
		return s.handleMouseClick(ctx, req.Params)
	case desktoptypes.ActionOpenApp:
		return s.handleOpenApp(ctx, req.Params)
	case desktoptypes.ActionFindElement:
		return s.handleFindElement(ctx, req.Params)
	case desktoptypes.ActionClickElement:
		return s.handleClickElement(ctx, req.Params)
	case desktoptypes.ActionSetElementValue:
		return s.handleSetElementValue(ctx, req.Params)
	case desktoptypes.ActionClearElement:
		return s.handleClearElement(ctx, req.Params)
	case desktoptypes.ActionGetElementValue:
		return s.handleGetElementValue(ctx, req.Params)
	case desktoptypes.ActionAssertElement:
		return s.handleAssertElement(ctx, req.Params)
	case desktoptypes.ActionFindText:
		return s.handleFindText(ctx, req.Params)
	case desktoptypes.ActionClickText:
		return s.handleClickText(ctx, req.Params)
	default:
		return nil, fmt.Errorf("unsupported desktop action %q", req.Action)
	}
}

func (s *linuxSession) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *linuxSession) handleListApps(ctx context.Context) (*desktoptypes.Response, error) {
	var cmd *exec.Cmd
	switch s.display {
	case displayWayland:
		cmd = exec.CommandContext(ctx, "swaymsg", "-t", "get_tree")
	default:
		cmd = exec.CommandContext(ctx, "wmctrl", "-l")
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list_apps: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"apps_raw": strings.TrimSpace(string(output)),
			"driver":   s.driverName(),
		},
	}, nil
}

func (s *linuxSession) handleFocusWindow(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	windowID := stringParam(params, "window_id")
	if windowID == "" {
		return nil, errors.New("focus_window requires params.window_id")
	}

	if s.display == displayWayland {
		return nil, errors.New("focus_window is not supported on wayland without xdotool compatibility")
	}

	cmd := exec.CommandContext(ctx, "xdotool", "windowactivate", windowID)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("focus_window: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"window_id": windowID,
			"focused":   true,
		},
	}, nil
}

func (s *linuxSession) handleTypeText(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("type_text requires params.text")
	}

	var cmd *exec.Cmd
	switch s.display {
	case displayWayland:
		cmd = exec.CommandContext(ctx, "ydotool", "type", "--", text)
	default:
		cmd = exec.CommandContext(ctx, "xdotool", "type", "--", text)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("type_text: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"text":   text,
			"typed":  true,
			"driver": s.driverName(),
		},
	}, nil
}

func (s *linuxSession) handleHotkey(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	combo := stringParam(params, "combo")
	if combo == "" {
		return nil, errors.New("hotkey requires params.combo")
	}

	var cmd *exec.Cmd
	switch s.display {
	case displayWayland:
		cmd = exec.CommandContext(ctx, "ydotool", "key", combo)
	default:
		cmd = exec.CommandContext(ctx, "xdotool", "key", combo)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("hotkey: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"combo":   combo,
			"pressed": true,
		},
	}, nil
}

func (s *linuxSession) handleScreenshot(ctx context.Context) (*desktoptypes.Response, error) {
	dir, err := os.MkdirTemp("", "hopclaw-desktopd-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "desktop.png")

	var cmd *exec.Cmd
	switch s.display {
	case displayWayland:
		cmd = exec.CommandContext(ctx, "grim", path)
	default:
		cmd = exec.CommandContext(ctx, "scrot", path)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("screenshot: %w: %s", err, strings.TrimSpace(string(output)))
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read screenshot: %w", err)
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"mime_type":      "image/png",
			"content_base64": base64.StdEncoding.EncodeToString(body),
		},
	}, nil
}

func (s *linuxSession) handleClipboardRead(ctx context.Context) (*desktoptypes.Response, error) {
	var cmd *exec.Cmd
	switch s.display {
	case displayWayland:
		cmd = exec.CommandContext(ctx, "wl-paste")
	default:
		cmd = exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-o")
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("clipboard_read: %w", err)
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"text": string(output),
		},
	}, nil
}

func (s *linuxSession) handleClipboardWrite(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("clipboard_write requires params.text")
	}

	var cmd *exec.Cmd
	switch s.display {
	case displayWayland:
		cmd = exec.CommandContext(ctx, "wl-copy")
	default:
		cmd = exec.CommandContext(ctx, "xclip", "-selection", "clipboard")
	}
	cmd.Stdin = bytes.NewBufferString(text)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("clipboard_write: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"written": true,
		},
	}, nil
}

func (s *linuxSession) handleMouseMove(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	x := intParam(params, "x")
	y := intParam(params, "y")

	if s.display == displayWayland {
		return nil, errors.New("mouse_move is not supported on wayland without ydotool mousemove support")
	}

	cmd := exec.CommandContext(ctx, "xdotool", "mousemove", fmt.Sprintf("%d", x), fmt.Sprintf("%d", y))
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("mouse_move: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &desktoptypes.Response{
		OK:   true,
		Data: map[string]any{"x": x, "y": y, "moved": true},
	}, nil
}

func (s *linuxSession) handleMouseClick(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	x := intParam(params, "x")
	y := intParam(params, "y")
	button := stringParam(params, "button")
	clickCount := intParam(params, "click_count")
	if clickCount <= 0 {
		clickCount = 1
	}

	if s.display == displayWayland {
		return nil, errors.New("mouse_click is not supported on wayland without ydotool click support")
	}

	// xdotool button mapping: 1=left, 3=right
	xButton := "1"
	if button == "right" {
		xButton = "3"
	} else {
		button = "left"
	}

	args := []string{"mousemove", fmt.Sprintf("%d", x), fmt.Sprintf("%d", y), "click", "--repeat", fmt.Sprintf("%d", clickCount), xButton}
	cmd := exec.CommandContext(ctx, "xdotool", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("mouse_click: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"x":           x,
			"y":           y,
			"button":      button,
			"click_count": clickCount,
			"clicked":     true,
		},
	}, nil
}

func (s *linuxSession) handleOpenApp(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	app := stringParam(params, "app")
	if app == "" {
		return nil, errors.New("open_app requires params.app")
	}
	cmd := exec.CommandContext(ctx, "xdg-open", app)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("open_app: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"app":    app,
			"opened": true,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *linuxSession) driverName() string {
	if s.display == displayWayland {
		return "linux-wayland"
	}
	return "linux-x11"
}

func stringParam(params map[string]any, key string) string {
	if len(params) == 0 {
		return ""
	}
	value, _ := params[key].(string)
	return strings.TrimSpace(value)
}

func intParam(params map[string]any, key string) int {
	if len(params) == 0 {
		return 0
	}
	switch value := params[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func boolParam(params map[string]any, key string) bool {
	if len(params) == 0 {
		return false
	}
	value, _ := params[key].(bool)
	return value
}
