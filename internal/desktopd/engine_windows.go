//go:build windows

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

// ---------------------------------------------------------------------------
// WindowsEngine
// ---------------------------------------------------------------------------

type WindowsEngine struct{}

type windowsSession struct {
	id        string
	workspace string
	mu        sync.Mutex
	closed    bool
}

func NewDefaultEngine() (Engine, error) {
	return &WindowsEngine{}, nil
}

func (e *WindowsEngine) OpenSession(_ context.Context, spec OpenSessionSpec) (Session, error) {
	return &windowsSession{
		id:        spec.ID,
		workspace: stringParam(spec.Params, "workspace"),
	}, nil
}

func (s *windowsSession) ID() string { return s.id }

func (s *windowsSession) Handle(ctx context.Context, req desktoptypes.Request) (*desktoptypes.Response, error) {
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

func (s *windowsSession) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *windowsSession) handleListApps(ctx context.Context) (*desktoptypes.Response, error) {
	script := `Get-Process | Where-Object {$_.MainWindowTitle} | Select-Object ProcessName,MainWindowTitle,Id | ConvertTo-Json`
	cmd := exec.CommandContext(ctx, "powershell", "-Command", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list_apps: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"apps_json": strings.TrimSpace(string(output)),
		},
	}, nil
}

func (s *windowsSession) handleFocusWindow(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	title := stringParam(params, "title")
	pidStr := stringParam(params, "pid")
	if title == "" && pidStr == "" {
		return nil, errors.New("focus_window requires params.title or params.pid")
	}

	var filter string
	if pidStr != "" {
		filter = fmt.Sprintf(`$proc = Get-Process -Id %s -ErrorAction Stop`, pidStr)
	} else {
		filter = fmt.Sprintf(`$proc = Get-Process | Where-Object {$_.MainWindowTitle -like '*%s*'} | Select-Object -First 1`, escapePowerShell(title))
	}

	script := fmt.Sprintf(`
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class WinAPI {
    [DllImport("user32.dll")]
    public static extern bool SetForegroundWindow(IntPtr hWnd);
}
"@
%s
if ($proc -ne $null) {
    [WinAPI]::SetForegroundWindow($proc.MainWindowHandle) | Out-Null
    Write-Output "focused"
} else {
    Write-Error "window not found"
}
`, filter)

	cmd := exec.CommandContext(ctx, "powershell", "-Command", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("focus_window: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"title":   title,
			"pid":     pidStr,
			"focused": true,
		},
	}, nil
}

func (s *windowsSession) handleTypeText(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("type_text requires params.text")
	}

	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.SendKeys]::SendWait('%s')
`, escapeSendKeys(text))

	cmd := exec.CommandContext(ctx, "powershell", "-Command", script)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("type_text: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"text":   text,
			"typed":  true,
			"driver": "windows",
		},
	}, nil
}

func (s *windowsSession) handleHotkey(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	combo := stringParam(params, "combo")
	if combo == "" {
		return nil, errors.New("hotkey requires params.combo")
	}

	sendKeysCombo, err := comboToSendKeys(combo)
	if err != nil {
		return nil, err
	}

	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.SendKeys]::SendWait('%s')
`, sendKeysCombo)

	cmd := exec.CommandContext(ctx, "powershell", "-Command", script)
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

func (s *windowsSession) handleScreenshot(ctx context.Context) (*desktoptypes.Response, error) {
	dir, err := os.MkdirTemp("", "hopclaw-desktopd-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "desktop.png")

	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Windows.Forms
$bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$bitmap = New-Object System.Drawing.Bitmap($bounds.Width, $bounds.Height)
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$graphics.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size)
$bitmap.Save('%s', [System.Drawing.Imaging.ImageFormat]::Png)
$graphics.Dispose()
$bitmap.Dispose()
`, escapePowerShell(path))

	cmd := exec.CommandContext(ctx, "powershell", "-Command", script)
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

func (s *windowsSession) handleClipboardRead(ctx context.Context) (*desktoptypes.Response, error) {
	cmd := exec.CommandContext(ctx, "powershell", "-Command", "Get-Clipboard")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("clipboard_read: %w", err)
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"text": strings.TrimSpace(string(output)),
		},
	}, nil
}

func (s *windowsSession) handleClipboardWrite(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("clipboard_write requires params.text")
	}
	cmd := exec.CommandContext(ctx, "cmd", "/c", "clip")
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

func (s *windowsSession) handleMouseMove(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	x := intParam(params, "x")
	y := intParam(params, "y")

	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point(%d, %d)
`, x, y)

	cmd := exec.CommandContext(ctx, "powershell", "-Command", script)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("mouse_move: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return &desktoptypes.Response{
		OK:   true,
		Data: map[string]any{"x": x, "y": y, "moved": true},
	}, nil
}

func (s *windowsSession) handleMouseClick(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	x := intParam(params, "x")
	y := intParam(params, "y")
	button := stringParam(params, "button")
	clickCount := intParam(params, "click_count")
	if clickCount <= 0 {
		clickCount = 1
	}

	var downFlag, upFlag string
	switch button {
	case "right":
		downFlag = "0x0008"
		upFlag = "0x0010"
	default: // "left" or empty
		button = "left"
		downFlag = "0x0002"
		upFlag = "0x0004"
	}

	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class MouseAPI {
    [DllImport("user32.dll")]
    public static extern void SetCursorPos(int x, int y);
    [DllImport("user32.dll")]
    public static extern void mouse_event(uint dwFlags, int dx, int dy, uint dwData, int dwExtraInfo);
}
"@
[MouseAPI]::SetCursorPos(%d, %d)
for ($i = 0; $i -lt %d; $i++) {
    [MouseAPI]::mouse_event(%s, 0, 0, 0, 0)
    [MouseAPI]::mouse_event(%s, 0, 0, 0, 0)
}
`, x, y, clickCount, downFlag, upFlag)

	cmd := exec.CommandContext(ctx, "powershell", "-Command", script)
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

func (s *windowsSession) handleOpenApp(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	app := stringParam(params, "app")
	if app == "" {
		return nil, errors.New("open_app requires params.app")
	}
	cmd := exec.CommandContext(ctx, "cmd", "/c", "start", "", app)
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

// escapePowerShell escapes single quotes for PowerShell string literals.
func escapePowerShell(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// escapeSendKeys escapes special SendKeys characters.
func escapeSendKeys(s string) string {
	replacer := strings.NewReplacer(
		"+", "{+}",
		"^", "{^}",
		"%", "{%}",
		"~", "{~}",
		"(", "{(}",
		")", "{)}",
		"{", "{{}",
		"}", "{}}",
		"[", "{[}",
		"]", "{]}",
	)
	return replacer.Replace(s)
}

// comboToSendKeys converts a human-readable combo like "ctrl+shift+s" into
// SendKeys syntax like "^+s".
func comboToSendKeys(combo string) (string, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(combo)), "+")
	if len(parts) == 0 {
		return "", errors.New("hotkey requires a non-empty combo")
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	if key == "" {
		return "", errors.New("hotkey requires a key")
	}

	var prefix string
	for _, raw := range parts[:len(parts)-1] {
		switch strings.TrimSpace(raw) {
		case "ctrl", "control":
			prefix += "^"
		case "shift":
			prefix += "+"
		case "alt":
			prefix += "%"
		case "":
		default:
			return "", fmt.Errorf("unsupported hotkey modifier %q", raw)
		}
	}

	sendKey, ok := specialSendKey(key)
	if ok {
		return prefix + sendKey, nil
	}
	return prefix + key, nil
}

func specialSendKey(key string) (string, bool) {
	switch key {
	case "enter", "return":
		return "{ENTER}", true
	case "tab":
		return "{TAB}", true
	case "space":
		return " ", true
	case "backspace", "delete":
		return "{BACKSPACE}", true
	case "escape", "esc":
		return "{ESC}", true
	case "left":
		return "{LEFT}", true
	case "right":
		return "{RIGHT}", true
	case "up":
		return "{UP}", true
	case "down":
		return "{DOWN}", true
	case "home":
		return "{HOME}", true
	case "end":
		return "{END}", true
	case "pageup":
		return "{PGUP}", true
	case "pagedown":
		return "{PGDN}", true
	case "f1":
		return "{F1}", true
	case "f2":
		return "{F2}", true
	case "f3":
		return "{F3}", true
	case "f4":
		return "{F4}", true
	case "f5":
		return "{F5}", true
	case "f6":
		return "{F6}", true
	case "f7":
		return "{F7}", true
	case "f8":
		return "{F8}", true
	case "f9":
		return "{F9}", true
	case "f10":
		return "{F10}", true
	case "f11":
		return "{F11}", true
	case "f12":
		return "{F12}", true
	default:
		return "", false
	}
}
