//go:build darwin

package desktopd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
)

const (
	screenRecordMaxDuration   = 300 // seconds
	screenRecordTimeoutBuffer = 10 * time.Second
	screenRecordDefaultFPS    = 30
	desktopReadyDefaultWait   = 15 * time.Second
	desktopReadyPollInterval  = 250 * time.Millisecond

	desktopWaitNone        = "none"
	desktopWaitRunning     = "running"
	desktopWaitWindow      = "window"
	desktopWaitFocused     = "focused"
	desktopWaitInteractive = "interactive"

	desktopReadyLaunching     = "launching"
	desktopReadyProcessSeen   = "process_seen"
	desktopReadyWindowVisible = "window_visible"
	desktopReadyFocused       = "focused"
	desktopReadyInteractive   = "interactive"
)

type DarwinEngine struct {
	actionMu sync.Mutex
}

type darwinSession struct {
	id              string
	workspace       string
	hostProfile     map[string]any
	focusLease      *desktopFocusLease
	visibilityLease *desktopFocusLease
	engine          *DarwinEngine
	mu              sync.Mutex
	closed          bool
}

type desktopReadyState struct {
	App            desktopAppSnapshot
	Found          bool
	ReadyState     string
	Interactive    bool
	MatchedWindow  desktopWindowSnapshot
	HasMatchWindow bool
}

func NewDefaultEngine() (Engine, error) {
	return &DarwinEngine{}, nil
}

func (e *DarwinEngine) Invoke(ctx context.Context, req desktoptypes.Request) (*desktoptypes.Response, error) {
	switch strings.TrimSpace(req.Action) {
	case desktoptypes.ActionListApps:
		snapshot, err := desktopSnapshot(ctx, boolParam(req.Params, "include_windows"))
		if err != nil {
			return nil, fmt.Errorf("list_apps: %w", err)
		}
		return &desktoptypes.Response{
			OK: true,
			Data: map[string]any{
				"frontmost_app": toMap(snapshot.FrontmostApp, boolParam(req.Params, "include_windows")),
				"apps":          appsToAny(snapshot.Apps, boolParam(req.Params, "include_windows")),
			},
		}, nil
	case desktoptypes.ActionDescribeHost:
		return &desktoptypes.Response{
			OK: true,
			Data: map[string]any{
				"profile": darwinHostProfile(),
			},
		}, nil
	default:
		return nil, ErrSessionIDRequired
	}
}

func (e *DarwinEngine) OpenSession(_ context.Context, spec OpenSessionSpec) (Session, error) {
	return &darwinSession{
		id:          spec.ID,
		workspace:   stringParam(spec.Params, "workspace"),
		hostProfile: darwinHostProfile(),
		engine:      e,
	}, nil
}

func (s *darwinSession) ID() string { return s.id }

func (s *darwinSession) Handle(ctx context.Context, req desktoptypes.Request) (*desktoptypes.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("desktop session is closed")
	}
	if s.engine != nil {
		s.engine.actionMu.Lock()
		defer s.engine.actionMu.Unlock()
	}

	switch strings.TrimSpace(req.Action) {
	case desktoptypes.ActionDescribeHost:
		return s.handleDescribeHost(ctx)
	case desktoptypes.ActionOpenApp:
		return s.handleOpenApp(ctx, req.Params)
	case desktoptypes.ActionFocusApp:
		return s.handleFocusApp(ctx, req.Params)
	case desktoptypes.ActionFocusWindow:
		return s.handleFocusWindow(ctx, req.Params)
	case desktoptypes.ActionListApps:
		return s.handleListApps(ctx, req.Params)
	case desktoptypes.ActionListWindows:
		return s.handleListWindows(ctx, req.Params)
	case desktoptypes.ActionListCommands:
		return s.handleListCommands(ctx, req.Params)
	case desktoptypes.ActionInvokeCommand:
		return s.handleInvokeCommand(ctx, req.Params)
	case desktoptypes.ActionListDriverActions:
		return s.handleListDriverActions(ctx, req.Params)
	case desktoptypes.ActionInvokeDriverAction:
		return s.handleInvokeDriverAction(ctx, req.Params)
	case desktoptypes.ActionTypeText:
		return s.handleTypeText(ctx, req.Params)
	case desktoptypes.ActionHotkey:
		return s.handleHotkey(ctx, req.Params)
	case desktoptypes.ActionScreenshot:
		return s.handleScreenshot(ctx, req.Params)
	case desktoptypes.ActionScreenRecord:
		return s.handleScreenRecord(ctx, req.Params)
	case desktoptypes.ActionClipboardRead:
		return s.handleClipboardRead(ctx)
	case desktoptypes.ActionCaptureTree:
		return s.handleCaptureTree(ctx, req.Params)
	case desktoptypes.ActionClipboardWrite:
		return s.handleClipboardWrite(ctx, req.Params)
	case desktoptypes.ActionMouseMove:
		return s.handleMouseMove(ctx, req.Params)
	case desktoptypes.ActionMouseClick:
		return s.handleMouseClick(ctx, req.Params)
	case desktoptypes.ActionScroll:
		return s.handleScroll(ctx, req.Params)
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

func (s *darwinSession) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *darwinSession) handleDescribeHost(_ context.Context) (*desktoptypes.Response, error) {
	profile := cloneMapAny(s.hostProfile)
	if profile == nil {
		profile = darwinHostProfile()
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"profile": profile,
		},
	}, nil
}

func (s *darwinSession) handleOpenApp(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	app := stringParam(params, "app")
	bundleID := stringParam(params, "bundle_id")
	waitUntil, err := desktopWaitTargetParam(params, desktopWaitRunning, desktopWaitNone, desktopWaitRunning, desktopWaitWindow, desktopWaitFocused, desktopWaitInteractive)
	if err != nil {
		return nil, fmt.Errorf("open_app: %w", err)
	}
	timeout := durationMillisParam(params, "timeout_ms", desktopReadyDefaultWait)
	var cmd *exec.Cmd
	switch {
	case bundleID != "":
		cmd = exec.CommandContext(ctx, "open", "-b", bundleID)
	case app != "":
		cmd = exec.CommandContext(ctx, "open", "-a", app)
	default:
		return nil, errors.New("open_app requires params.app or params.bundle_id")
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("open_app: %w: %s", err, strings.TrimSpace(string(output)))
	}
	state, waited, recovered, err := waitForAppReadyWithWindowRecovery(ctx, app, bundleID, waitUntil, timeout)
	if err != nil {
		return nil, fmt.Errorf("open_app: %w", err)
	}
	if state.App.WindowCount > 0 {
		s.setVisibilityLease(state.App.Name, state.App.BundleID, "", 0)
	}
	if state.App.Frontmost {
		s.setFocusLease(state.App.Name, state.App.BundleID, "", 0)
	}
	data := state.resultData(waitUntil, waited)
	data["opened"] = true
	if recovered {
		data["recovered"] = true
	}
	data = annotateActionResult(data, actionStatusVerified, "open", "launch_wait", 0.98, map[string]any{
		"ready_state": state.ReadyState,
		"waited_ms":   int(waited / time.Millisecond),
		"recovered":   recovered,
	})
	return &desktoptypes.Response{
		OK:   true,
		Data: data,
	}, nil
}

func (s *darwinSession) handleFocusApp(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	app := stringParam(params, "app")
	bundleID := stringParam(params, "bundle_id")
	waitUntil, err := desktopWaitTargetParam(params, desktopWaitFocused, desktopWaitNone, desktopWaitRunning, desktopWaitWindow, desktopWaitFocused, desktopWaitInteractive)
	if err != nil {
		return nil, fmt.Errorf("focus_app: %w", err)
	}
	timeout := durationMillisParam(params, "timeout_ms", desktopReadyDefaultWait)
	switch {
	case bundleID != "":
		if err := runAppleScript(ctx, fmt.Sprintf(`tell application id "%s" to activate`, escapeAppleScript(bundleID))); err != nil {
			return nil, fmt.Errorf("focus_app: %w", err)
		}
	case app != "":
		if err := runAppleScript(ctx, fmt.Sprintf(`tell application "%s" to activate`, escapeAppleScript(app))); err != nil {
			return nil, fmt.Errorf("focus_app: %w", err)
		}
	default:
		return nil, errors.New("focus_app requires params.app or params.bundle_id")
	}
	state, waited, recovered, err := waitForAppReadyWithWindowRecovery(ctx, app, bundleID, waitUntil, timeout)
	if err != nil {
		return nil, fmt.Errorf("focus_app: %w", err)
	}
	s.setVisibilityLease(state.App.Name, state.App.BundleID, "", 0)
	s.setFocusLease(state.App.Name, state.App.BundleID, "", 0)
	data := state.resultData(waitUntil, waited)
	data["focused"] = state.App.Frontmost
	if recovered {
		data["recovered"] = true
	}
	data = annotateActionResult(data, actionStatusVerified, "activate", "focus_wait", 0.99, map[string]any{
		"ready_state": state.ReadyState,
		"waited_ms":   int(waited / time.Millisecond),
		"recovered":   recovered,
	})
	return &desktoptypes.Response{
		OK:   true,
		Data: data,
	}, nil
}

func (s *darwinSession) handleFocusWindow(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	appName := stringParam(params, "app")
	bundleID := stringParam(params, "bundle_id")
	titleContains := stringParam(params, "title_contains")
	windowIndex := intParam(params, "window_index")
	waitUntil, err := desktopWaitTargetParam(params, desktopWaitInteractive, desktopWaitWindow, desktopWaitFocused, desktopWaitInteractive)
	if err != nil {
		return nil, fmt.Errorf("focus_window: %w", err)
	}
	timeout := durationMillisParam(params, "timeout_ms", desktopReadyDefaultWait)
	state, waited, err := focusWindowReady(ctx, appName, bundleID, titleContains, windowIndex, waitUntil, timeout)
	if err != nil {
		return nil, fmt.Errorf("focus_window: %w", err)
	}
	resolvedTitle := firstNonEmpty(state.MatchedWindow.Title, titleContains)
	resolvedIndex := state.MatchedWindow.Index
	s.setVisibilityLease(state.App.Name, state.App.BundleID, resolvedTitle, resolvedIndex)
	s.setFocusLease(state.App.Name, state.App.BundleID, resolvedTitle, resolvedIndex)
	data := state.resultData(waitUntil, waited)
	data["focused"] = state.App.Frontmost
	data = annotateActionResult(data, actionStatusVerified, "ax_raise", "window_focus_wait", 0.98, map[string]any{
		"ready_state":  state.ReadyState,
		"title":        resolvedTitle,
		"window_index": resolvedIndex,
		"waited_ms":    int(waited / time.Millisecond),
	})
	return &desktoptypes.Response{
		OK:   true,
		Data: data,
	}, nil
}

func (s *darwinSession) handleListApps(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	includeWindows := boolParam(params, "include_windows")
	snapshot, err := desktopSnapshot(ctx, includeWindows)
	if err != nil {
		return nil, fmt.Errorf("list_apps: %w", err)
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"frontmost_app": toMap(snapshot.FrontmostApp, includeWindows),
			"apps":          appsToAny(snapshot.Apps, includeWindows),
		},
	}, nil
}

func (s *darwinSession) handleListWindows(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	app, err := resolveTargetApp(ctx, stringParam(params, "app"), stringParam(params, "bundle_id"), true)
	if err != nil {
		return nil, fmt.Errorf("list_windows: %w", err)
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"app":     toMap(&app, true),
			"windows": windowsToAny(app.Windows),
		},
	}, nil
}

func (s *darwinSession) handleTypeText(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("type_text requires params.text")
	}

	mode := strings.ToLower(stringParam(params, "mode"))
	focusLease, err := s.ensureFocusLease(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("type_text: focus: %w", err)
	}

	// "keys" mode sends raw key events via keystroke — the active input
	// method WILL intercept these, so Latin characters may be converted
	// to CJK candidates.  Use this only when you need raw key simulation.
	//
	// Default (empty or "paste") mode uses the clipboard to inject text,
	// completely bypassing the input method.  The original clipboard
	// content is saved and restored after pasting.
	if mode == "keys" {
		script := fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escapeAppleScript(text))
		if err := runAppleScript(ctx, script); err != nil {
			return nil, fmt.Errorf("type_text: %w", err)
		}
		data := map[string]any{
			"text":   text,
			"typed":  true,
			"mode":   "keys",
			"driver": "darwin",
		}
		if focusLease != nil {
			data["focus_lease"] = focusLease
		}
		data = annotateActionResult(data, actionStatusAttempted, "keys", "text_injection", 0.92, map[string]any{
			"text_len": len([]rune(text)),
		})
		return &desktoptypes.Response{
			OK:   true,
			Data: data,
		}, nil
	}

	// Default: clipboard-paste mode — bypasses input method entirely.
	if err := typeViaPaste(ctx, text); err != nil {
		return nil, fmt.Errorf("type_text: %w", err)
	}
	data := map[string]any{
		"text":   text,
		"typed":  true,
		"mode":   "paste",
		"driver": "darwin",
	}
	if focusLease != nil {
		data["focus_lease"] = focusLease
	}
	data = annotateActionResult(data, actionStatusAttempted, "paste", "text_injection", 0.97, map[string]any{
		"text_len": len([]rune(text)),
	})
	return &desktoptypes.Response{
		OK:   true,
		Data: data,
	}, nil
}

// typeViaPaste injects text via the clipboard and Cmd+V, bypassing the
// active input method.  The previous clipboard content is saved and
// restored afterwards so the user's clipboard is not clobbered.
func typeViaPaste(ctx context.Context, text string) error {
	// 1. Save current clipboard.
	savedClip, _ := exec.CommandContext(ctx, "pbpaste").Output()

	// 2. Write target text to clipboard.
	writeCmd := exec.CommandContext(ctx, "pbcopy")
	writeCmd.Stdin = bytes.NewBufferString(text)
	if out, err := writeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("write clipboard: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// 3. Small delay so the pasteboard daemon can sync.
	time.Sleep(50 * time.Millisecond)

	// 4. Cmd+V paste.
	pasteScript := `tell application "System Events" to keystroke "v" using command down`
	if err := runAppleScript(ctx, pasteScript); err != nil {
		return fmt.Errorf("paste: %w", err)
	}

	// 5. Small delay to let the paste complete before restoring clipboard.
	time.Sleep(100 * time.Millisecond)

	// 6. Restore original clipboard (best-effort).
	restoreCmd := exec.CommandContext(ctx, "pbcopy")
	restoreCmd.Stdin = bytes.NewBuffer(savedClip)
	_ = restoreCmd.Run()

	return nil
}

func (s *darwinSession) handleHotkey(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	combo := stringParam(params, "combo")
	if combo == "" {
		return nil, errors.New("hotkey requires params.combo")
	}
	focusLease, err := s.ensureFocusLease(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("hotkey: focus: %w", err)
	}
	script, err := hotkeyAppleScript(combo)
	if err != nil {
		return nil, err
	}
	if err := runAppleScript(ctx, script); err != nil {
		return nil, fmt.Errorf("hotkey: %w", err)
	}
	// Give the target app a brief moment to apply selection/focus changes
	// before the next automation step runs.
	time.Sleep(inputSettleDelay)
	data := map[string]any{
		"combo":   combo,
		"pressed": true,
	}
	if focusLease != nil {
		data["focus_lease"] = focusLease
	}
	data = annotateActionResult(data, actionStatusAttempted, "hotkey", "key_chord", 0.95, map[string]any{
		"combo": combo,
	})
	return &desktoptypes.Response{
		OK:   true,
		Data: data,
	}, nil
}

func (s *darwinSession) handleScreenshot(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	// Pre-check screen recording permission.  On macOS, CLI processes
	// cannot trigger the TCC authorization dialog — calling
	// CGRequestScreenCaptureAccess merely registers the process in the
	// Privacy settings list so the user can toggle it manually.
	if err := ensureScreenCapturePermission(ctx); err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "hopclaw-desktopd-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "desktop.png")
	data := map[string]any{
		"mime_type":    "image/png",
		"scope":        "full_screen",
		"capture_mode": "screen",
	}
	if wantsTargetedScreenshot(params) {
		app, window, err := resolveScreenshotTarget(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("screenshot: %w", err)
		}
		capture, err := captureTargetedPNG(ctx, app, window, path)
		if err != nil {
			return nil, fmt.Errorf("screenshot: %w", err)
		}
		data["scope"] = "app_window"
		data["capture_mode"] = capture.CaptureMode
		data["app"] = app.Name
		data["bundle_id"] = app.BundleID
		data["title"] = window.Title
		data["window_index"] = window.Index
		data["bounds"] = capture.Bounds
		if capture.WindowID > 0 {
			data["window_id"] = capture.WindowID
		}
		if capture.Occluded {
			data["occluded"] = true
		}
		data = annotateActionResult(data, capture.ActionStatus, "", "targeted_capture", confidenceForCaptureMode(capture.CaptureMode), capture.Evidence)
		s.setVisibilityLease(app.Name, app.BundleID, firstNonEmpty(window.Title, stringParam(params, "title_contains")), window.Index)
	} else {
		if err := captureFullScreenPNG(ctx, path); err != nil {
			return nil, fmt.Errorf("screenshot: %w", err)
		}
		data = annotateActionResult(data, actionStatusVerified, "", "screen_capture", 0.99, map[string]any{
			"scope": "full_screen",
		})
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read screenshot: %w", err)
	}
	data["content_base64"] = base64.StdEncoding.EncodeToString(body)
	return &desktoptypes.Response{
		OK:   true,
		Data: data,
	}, nil
}

func (s *darwinSession) handleScreenRecord(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	if err := ensureScreenCapturePermission(ctx); err != nil {
		return nil, err
	}

	outputPath := stringParam(params, "output_path")
	if outputPath == "" {
		return nil, errors.New("screen_record requires params.output_path")
	}

	durationSec := intParam(params, "duration_sec")
	if durationSec <= 0 {
		return nil, errors.New("screen_record requires positive params.duration_sec")
	}
	if durationSec > screenRecordMaxDuration {
		return nil, fmt.Errorf("screen_record: duration_sec exceeds maximum of %d", screenRecordMaxDuration)
	}

	audio := boolParam(params, "audio")
	display := stringParam(params, "display")

	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return nil, fmt.Errorf("screen_record: %w", err)
	}

	recordTimeout := time.Duration(durationSec)*time.Second + screenRecordTimeoutBuffer
	execCtx, cancel := context.WithTimeout(ctx, recordTimeout)
	defer cancel()

	args := []string{"-v", "-V", strconv.Itoa(durationSec)}
	if audio {
		args = append([]string{"-k"}, args...)
	}
	if display != "" {
		args = append(args, "-D", display)
	}
	args = append(args, absPath)

	cmd := exec.CommandContext(execCtx, "screencapture", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("screen_record: %w: %s", err, strings.TrimSpace(string(output)))
	}

	var sizeBytes int64
	if info, statErr := os.Stat(absPath); statErr == nil {
		sizeBytes = info.Size()
	}

	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"ok":           true,
			"path":         absPath,
			"size_bytes":   sizeBytes,
			"duration_sec": durationSec,
		},
	}, nil
}

func (s *darwinSession) handleClipboardRead(ctx context.Context) (*desktoptypes.Response, error) {
	cmd := exec.CommandContext(ctx, "pbpaste")
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

func (s *darwinSession) handleCaptureTree(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	appName := stringParam(params, "app")
	bundleID := stringParam(params, "bundle_id")
	snapshot, err := desktopSnapshot(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("capture_tree: %w", err)
	}
	if appName != "" || bundleID != "" {
		target, err := snapshot.findApp(appName, bundleID)
		if err != nil {
			return nil, fmt.Errorf("capture_tree: %w", err)
		}
		return &desktoptypes.Response{
			OK: true,
			Data: map[string]any{
				"frontmost_app": toMap(snapshot.FrontmostApp, true),
				"apps":          []any{toMap(&target, true)},
			},
		}, nil
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"frontmost_app": toMap(snapshot.FrontmostApp, true),
			"apps":          appsToAny(snapshot.Apps, true),
		},
	}, nil
}

func (s *darwinSession) handleClipboardWrite(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("clipboard_write requires params.text")
	}
	cmd := exec.CommandContext(ctx, "pbcopy")
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

// ---------------------------------------------------------------------------
// Mouse & scroll actions (CoreGraphics via JXA ObjC bridge)
// ---------------------------------------------------------------------------

func (s *darwinSession) handleMouseMove(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	x := intParam(params, "x")
	y := intParam(params, "y")
	script := fmt.Sprintf(`(() => {
  ObjC.import('CoreGraphics');
  var pt = $.CGPointMake(%d, %d);
  var ev = $.CGEventCreateMouseEvent($(), $.kCGEventMouseMoved, pt, $.kCGMouseButtonLeft);
  $.CGEventPost($.kCGHIDEventTap, ev);
  return JSON.stringify({ok:true, x:%d, y:%d});
})()`, x, y, x, y)
	var result map[string]any
	if err := runJXAJSON(ctx, script, &result); err != nil {
		return nil, fmt.Errorf("mouse_move: %w", err)
	}
	return &desktoptypes.Response{
		OK:   true,
		Data: map[string]any{"x": x, "y": y, "moved": true},
	}, nil
}

func (s *darwinSession) handleMouseClick(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	x := intParam(params, "x")
	y := intParam(params, "y")
	button := stringParam(params, "button")
	clickCount := intParam(params, "click_count")
	if clickCount <= 0 {
		clickCount = 1
	}

	var cgButton, cgDown, cgUp string
	switch button {
	case "right":
		cgButton = "$.kCGMouseButtonRight"
		cgDown = "$.kCGEventRightMouseDown"
		cgUp = "$.kCGEventRightMouseUp"
	default: // "left" or empty
		cgButton = "$.kCGMouseButtonLeft"
		cgDown = "$.kCGEventLeftMouseDown"
		cgUp = "$.kCGEventLeftMouseUp"
	}

	script := fmt.Sprintf(`(() => {
  ObjC.import('CoreGraphics');
  var pt = $.CGPointMake(%d, %d);
  for (var i = 0; i < %d; i++) {
    var down = $.CGEventCreateMouseEvent($(), %s, pt, %s);
    $.CGEventSetIntegerValueField(down, $.kCGMouseEventClickState, i + 1);
    $.CGEventPost($.kCGHIDEventTap, down);
    var up = $.CGEventCreateMouseEvent($(), %s, pt, %s);
    $.CGEventSetIntegerValueField(up, $.kCGMouseEventClickState, i + 1);
    $.CGEventPost($.kCGHIDEventTap, up);
  }
  return JSON.stringify({ok:true});
})()`, x, y, clickCount, cgDown, cgButton, cgUp, cgButton)

	var result map[string]any
	if err := runJXAJSON(ctx, script, &result); err != nil {
		return nil, fmt.Errorf("mouse_click: %w", err)
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

func (s *darwinSession) handleScroll(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	dx := intParam(params, "dx")
	dy := intParam(params, "dy")
	script := fmt.Sprintf(`(() => {
  ObjC.import('CoreGraphics');
  var ev = $.CGEventCreateScrollWheelEvent($(), $.kCGScrollEventUnitLine, 2, %d, %d);
  $.CGEventPost($.kCGHIDEventTap, ev);
  return JSON.stringify({ok:true});
})()`, dy, dx)
	var result map[string]any
	if err := runJXAJSON(ctx, script, &result); err != nil {
		return nil, fmt.Errorf("scroll: %w", err)
	}
	return &desktoptypes.Response{
		OK:   true,
		Data: map[string]any{"dx": dx, "dy": dy, "scrolled": true},
	}, nil
}

func runAppleScript(ctx context.Context, script string) error {
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runAppleScriptString(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func runJXAJSON(ctx context.Context, script string, target any) error {
	cmd := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := json.Unmarshal(output, target); err != nil {
		return fmt.Errorf("decode jxa output: %w", err)
	}
	return nil
}

func hotkeyAppleScript(combo string) (string, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(combo)), "+")
	if len(parts) == 0 {
		return "", errors.New("hotkey requires a non-empty combo")
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	if key == "" {
		return "", errors.New("hotkey requires a key")
	}
	modifiers := make([]string, 0, len(parts)-1)
	for _, raw := range parts[:len(parts)-1] {
		switch strings.TrimSpace(raw) {
		case "cmd", "command":
			modifiers = append(modifiers, "command down")
		case "shift":
			modifiers = append(modifiers, "shift down")
		case "ctrl", "control":
			modifiers = append(modifiers, "control down")
		case "alt", "option":
			modifiers = append(modifiers, "option down")
		case "":
		default:
			return "", fmt.Errorf("unsupported hotkey modifier %q", raw)
		}
	}
	using := ""
	if len(modifiers) > 0 {
		using = " using {" + strings.Join(modifiers, ", ") + "}"
	}
	if keyCode, ok := specialKeyCode(key); ok {
		return fmt.Sprintf(`tell application "System Events" to key code %d%s`, keyCode, using), nil
	}
	if len([]rune(key)) != 1 {
		return "", fmt.Errorf("unsupported hotkey key %q", key)
	}
	return fmt.Sprintf(`tell application "System Events" to keystroke "%s"%s`, escapeAppleScript(key), using), nil
}

func specialKeyCode(key string) (int, bool) {
	switch key {
	case "return", "enter":
		return 36, true
	case "tab":
		return 48, true
	case "space":
		return 49, true
	case "delete", "backspace":
		return 51, true
	case "escape", "esc":
		return 53, true
	case "left":
		return 123, true
	case "right":
		return 124, true
	case "down":
		return 125, true
	case "up":
		return 126, true
	default:
		return 0, false
	}
}

func escapeAppleScript(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}

type desktopWindowSnapshot struct {
	Index    int    `json:"index"`
	Title    string `json:"title,omitempty"`
	Role     string `json:"role,omitempty"`
	Subrole  string `json:"subrole,omitempty"`
	Position []int  `json:"position,omitempty"`
	Size     []int  `json:"size,omitempty"`
}

type desktopAppSnapshot struct {
	Name        string                  `json:"name"`
	BundleID    string                  `json:"bundle_id,omitempty"`
	PID         int                     `json:"pid,omitempty"`
	Frontmost   bool                    `json:"frontmost"`
	WindowCount int                     `json:"window_count"`
	Windows     []desktopWindowSnapshot `json:"windows,omitempty"`
}

type desktopSnapshotResult struct {
	FrontmostApp *desktopAppSnapshot  `json:"frontmost_app,omitempty"`
	Apps         []desktopAppSnapshot `json:"apps,omitempty"`
}

func desktopSnapshot(ctx context.Context, includeWindows bool) (*desktopSnapshotResult, error) {
	includeWindowsJSON, _ := json.Marshal(includeWindows)
	script := fmt.Sprintf(`(() => {
  const includeWindows = %s;
  const se = Application("System Events");
  const procs = se.applicationProcesses();
  const apps = [];
  let frontmostApp = null;

  function safeCall(fn, fallback) {
    try { return fn(); } catch (_) { return fallback; }
  }

  for (let i = 0; i < procs.length; i++) {
    const proc = procs[i];
    const name = safeCall(() => proc.name(), "");
    if (!name) continue;
    const backgroundOnly = safeCall(() => proc.backgroundOnly(), false);
    if (backgroundOnly) continue;

    const app = {
      name: name,
      bundle_id: safeCall(() => proc.bundleIdentifier(), ""),
      pid: Number(safeCall(() => proc.unixId(), 0) || 0),
      frontmost: safeCall(() => proc.frontmost(), false),
      window_count: 0,
      windows: []
    };

    if (includeWindows) {
      const procWindows = safeCall(() => proc.windows(), []);
      for (let j = 0; j < procWindows.length; j++) {
        const w = procWindows[j];
        const pos = safeCall(() => w.position(), null);
        const size = safeCall(() => w.size(), null);
        app.windows.push({
          index: j + 1,
          title: safeCall(() => w.name(), ""),
          role: safeCall(() => w.role(), ""),
          subrole: safeCall(() => w.subrole(), ""),
          position: Array.isArray(pos) ? pos.map(v => Number(v)) : null,
          size: Array.isArray(size) ? size.map(v => Number(v)) : null
        });
      }
      app.window_count = app.windows.length;
    } else {
      app.window_count = safeCall(() => proc.windows().length, 0);
      delete app.windows;
    }

    apps.push(app);
    if (app.frontmost) {
      frontmostApp = app;
    }
  }

  return JSON.stringify({
    frontmost_app: frontmostApp,
    apps: apps
  });
})()`, string(includeWindowsJSON))

	var snapshot desktopSnapshotResult
	if err := runJXAJSON(ctx, script, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (s desktopReadyState) meets(waitUntil string) bool {
	switch waitUntil {
	case desktopWaitNone:
		return true
	case desktopWaitRunning:
		return s.Found
	case desktopWaitWindow:
		return s.HasMatchWindow || s.App.WindowCount > 0
	case desktopWaitFocused:
		return s.Found && s.App.Frontmost
	case desktopWaitInteractive:
		return s.Found && s.Interactive
	default:
		return false
	}
}

func (s desktopReadyState) resultData(waitUntil string, waited time.Duration) map[string]any {
	data := map[string]any{
		"app":          s.App.Name,
		"bundle_id":    s.App.BundleID,
		"wait_until":   waitUntil,
		"ready_state":  s.ReadyState,
		"ready":        s.meets(waitUntil),
		"frontmost":    s.App.Frontmost,
		"interactive":  s.Interactive,
		"window_count": s.App.WindowCount,
		"waited_ms":    int(waited / time.Millisecond),
	}
	if s.HasMatchWindow {
		data["title"] = s.MatchedWindow.Title
		data["window_index"] = s.MatchedWindow.Index
	}
	return data
}

func resolveApp(ctx context.Context, appName string, bundleID string) (desktopAppSnapshot, error) {
	return resolveTargetApp(ctx, appName, bundleID, true)
}

func queryTargetApp(ctx context.Context, appName string, bundleID string, includeWindows bool) (desktopAppSnapshot, bool, error) {
	appName = strings.TrimSpace(appName)
	bundleID = strings.TrimSpace(bundleID)
	if appName == "" && bundleID == "" {
		snapshot, err := desktopSnapshot(ctx, includeWindows)
		if err != nil {
			return desktopAppSnapshot{}, false, err
		}
		app, err := snapshot.findApp("", "")
		if err != nil {
			return desktopAppSnapshot{}, false, err
		}
		return app, true, nil
	}

	appNameJSON, _ := json.Marshal(appName)
	bundleIDJSON, _ := json.Marshal(bundleID)
	includeWindowsJSON, _ := json.Marshal(includeWindows)
	script := fmt.Sprintf(`(() => {
  const requestedName = %s;
  const requestedBundleID = %s;
  const includeWindows = %s;
  const se = Application("System Events");
  const procs = se.applicationProcesses();

  function safeCall(fn, fallback) {
    try { return fn(); } catch (_) { return fallback; }
  }

  function procExists(proc) {
    return !!proc && safeCall(() => proc.exists(), true);
  }

  let target = null;
  if (requestedName) {
    const named = se.processes.byName(requestedName);
    if (procExists(named)) {
      target = named;
    }
  }

  if (!target && requestedBundleID) {
    for (let i = 0; i < procs.length; i++) {
      const proc = procs[i];
      if (safeCall(() => proc.bundleIdentifier(), "") === requestedBundleID) {
        target = proc;
        break;
      }
    }
  }

  if (!target) return JSON.stringify({found:false});

  const app = {
    found: true,
    name: safeCall(() => target.name(), requestedName),
    bundle_id: safeCall(() => target.bundleIdentifier(), requestedBundleID),
    pid: Number(safeCall(() => target.unixId(), 0) || 0),
    frontmost: safeCall(() => target.frontmost(), false),
    window_count: 0,
    windows: []
  };

  if (includeWindows) {
    const procWindows = safeCall(() => target.windows(), []);
    for (let j = 0; j < procWindows.length; j++) {
      const w = procWindows[j];
      const pos = safeCall(() => w.position(), null);
      const size = safeCall(() => w.size(), null);
      app.windows.push({
        index: j + 1,
        title: safeCall(() => w.name(), ""),
        role: safeCall(() => w.role(), ""),
        subrole: safeCall(() => w.subrole(), ""),
        position: Array.isArray(pos) ? pos.map(v => Number(v)) : null,
        size: Array.isArray(size) ? size.map(v => Number(v)) : null
      });
    }
    app.window_count = app.windows.length;
  } else {
    app.window_count = safeCall(() => target.windows().length, 0);
    delete app.windows;
  }

  return JSON.stringify(app);
})()`, string(appNameJSON), string(bundleIDJSON), string(includeWindowsJSON))

	var payload struct {
		Found       bool                    `json:"found"`
		Name        string                  `json:"name"`
		BundleID    string                  `json:"bundle_id"`
		PID         int                     `json:"pid"`
		Frontmost   bool                    `json:"frontmost"`
		WindowCount int                     `json:"window_count"`
		Windows     []desktopWindowSnapshot `json:"windows,omitempty"`
	}
	if err := runJXAJSON(ctx, script, &payload); err != nil {
		return desktopAppSnapshot{}, false, err
	}
	if !payload.Found {
		return desktopAppSnapshot{}, false, nil
	}
	return desktopAppSnapshot{
		Name:        payload.Name,
		BundleID:    payload.BundleID,
		PID:         payload.PID,
		Frontmost:   payload.Frontmost,
		WindowCount: payload.WindowCount,
		Windows:     payload.Windows,
	}, true, nil
}

func resolveTargetApp(ctx context.Context, appName string, bundleID string, includeWindows bool) (desktopAppSnapshot, error) {
	app, found, err := queryTargetApp(ctx, appName, bundleID, includeWindows)
	if err != nil {
		return desktopAppSnapshot{}, err
	}
	if found {
		return app, nil
	}
	switch {
	case strings.TrimSpace(bundleID) != "":
		return desktopAppSnapshot{}, fmt.Errorf("application with bundle_id %q not found", bundleID)
	case strings.TrimSpace(appName) != "":
		return desktopAppSnapshot{}, fmt.Errorf("application %q not found", appName)
	default:
		return desktopAppSnapshot{}, errors.New("no matching application found")
	}
}

func (s *desktopSnapshotResult) findApp(appName string, bundleID string) (desktopAppSnapshot, error) {
	if s == nil {
		return desktopAppSnapshot{}, errors.New("desktop snapshot unavailable")
	}
	appName = strings.TrimSpace(appName)
	bundleID = strings.TrimSpace(bundleID)
	for _, app := range s.Apps {
		switch {
		case bundleID != "" && strings.EqualFold(strings.TrimSpace(app.BundleID), bundleID):
			return app, nil
		case appName != "" && strings.EqualFold(strings.TrimSpace(app.Name), appName):
			return app, nil
		}
	}
	if appName == "" && bundleID == "" && s.FrontmostApp != nil {
		return *s.FrontmostApp, nil
	}
	if bundleID != "" {
		return desktopAppSnapshot{}, fmt.Errorf("application with bundle_id %q not found", bundleID)
	}
	if appName != "" {
		return desktopAppSnapshot{}, fmt.Errorf("application %q not found", appName)
	}
	return desktopAppSnapshot{}, errors.New("no matching application found")
}

func raiseWindow(ctx context.Context, appName string, bundleID string, titleContains string, windowIndex int) (string, string, string, int, error) {
	appJSON, _ := json.Marshal(appName)
	bundleIDJSON, _ := json.Marshal(bundleID)
	titleJSON, _ := json.Marshal(titleContains)
	script := fmt.Sprintf(`(() => {
  const requestedName = %s;
  const requestedBundleID = %s;
  const titleContains = %s;
  const requestedIndex = %d;
  const se = Application("System Events");
  const procs = se.applicationProcesses();

  function safeCall(fn, fallback) {
    try { return fn(); } catch (_) { return fallback; }
  }

  let proc = null;
  if (requestedName) {
    const named = se.processes.byName(requestedName);
    if (safeCall(() => named.exists(), true)) {
      proc = named;
    }
  }
  if (!proc && requestedBundleID) {
    for (let i = 0; i < procs.length; i++) {
      if (safeCall(() => procs[i].bundleIdentifier(), "") === requestedBundleID) {
        proc = procs[i];
        break;
      }
    }
  }
  if (!proc) throw new Error("application process not found");

  proc.frontmost = true;
  const windows = proc.windows();
  if (!windows.length) throw new Error("window not found");

  let target = null;
  let resolvedIndex = 1;
  if (titleContains) {
    for (let i = 0; i < windows.length; i++) {
      const title = (() => { try { return windows[i].name(); } catch (_) { return ""; } })();
      if (title && title.indexOf(titleContains) >= 0) {
        target = windows[i];
        resolvedIndex = i + 1;
        break;
      }
    }
    if (!target) throw new Error("window not found matching title");
  } else if (requestedIndex > 0) {
    if (requestedIndex > windows.length) throw new Error("window index out of range");
    target = windows[requestedIndex - 1];
    resolvedIndex = requestedIndex;
  } else {
    target = windows[0];
  }

  try {
    target.actions.byName("AXRaise").perform();
  } catch (_) {}

  return JSON.stringify({
    name: safeCall(() => proc.name(), requestedName),
    bundle_id: safeCall(() => proc.bundleIdentifier(), requestedBundleID),
    title: (() => { try { return target.name(); } catch (_) { return ""; } })(),
    window_index: resolvedIndex
  });
})()`, string(appJSON), string(bundleIDJSON), string(titleJSON), windowIndex)

	var payload struct {
		Name        string `json:"name"`
		BundleID    string `json:"bundle_id"`
		Title       string `json:"title"`
		WindowIndex int    `json:"window_index"`
	}
	if err := runJXAJSON(ctx, script, &payload); err != nil {
		return "", "", "", 0, err
	}
	if payload.WindowIndex <= 0 {
		payload.WindowIndex = 1
	}
	return payload.Name, payload.BundleID, payload.Title, payload.WindowIndex, nil
}

func waitForAppReady(ctx context.Context, appName string, bundleID string, waitUntil string, timeout time.Duration) (desktopReadyState, time.Duration, error) {
	startedAt := time.Now()
	includeWindows := waitTargetNeedsWindows(waitUntil)
	state, err := inspectAppReadyState(ctx, appName, bundleID, includeWindows)
	if err != nil {
		return state, time.Since(startedAt), err
	}
	if state.meets(waitUntil) || waitUntil == desktopWaitNone {
		return state, time.Since(startedAt), nil
	}

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return state, time.Since(startedAt), fmt.Errorf("timed out waiting for %s (last ready_state=%s)", waitUntil, state.ReadyState)
		}
		if err := sleepContext(ctx, desktopReadyPollInterval); err != nil {
			return state, time.Since(startedAt), err
		}
		state, err = inspectAppReadyState(ctx, appName, bundleID, includeWindows)
		if err != nil {
			return state, time.Since(startedAt), err
		}
		if state.meets(waitUntil) {
			return state, time.Since(startedAt), nil
		}
	}
}

func waitForAppReadyWithWindowRecovery(ctx context.Context, appName string, bundleID string, waitUntil string, timeout time.Duration) (desktopReadyState, time.Duration, bool, error) {
	state, waited, err := waitForAppReady(ctx, appName, bundleID, waitUntil, timeout)
	if err == nil {
		return state, waited, false, nil
	}
	if !shouldAttemptWindowRecovery(state, waitUntil) {
		return state, waited, false, annotateDesktopWaitError(ctx, err)
	}
	if recoverErr := reopenApplicationWindow(ctx, appName, bundleID); recoverErr != nil {
		return state, waited, false, annotateDesktopWaitError(ctx, err)
	}
	recoveredState, recoveredWaited, recoveredErr := waitForAppReady(ctx, appName, bundleID, waitUntil, timeout)
	totalWaited := waited + recoveredWaited
	if recoveredErr != nil {
		return recoveredState, totalWaited, true, annotateDesktopWaitError(ctx, recoveredErr)
	}
	return recoveredState, totalWaited, true, nil
}

func shouldAttemptWindowRecovery(state desktopReadyState, waitUntil string) bool {
	if waitUntil == desktopWaitNone || waitUntil == desktopWaitRunning {
		return false
	}
	if !state.Found {
		return false
	}
	return state.ReadyState == desktopReadyProcessSeen || state.App.WindowCount == 0
}

func reopenApplicationWindow(ctx context.Context, appName string, bundleID string) error {
	switch {
	case strings.TrimSpace(bundleID) != "":
		return runAppleScript(ctx, fmt.Sprintf(`tell application id "%s" to reopen`, escapeAppleScript(bundleID)))
	case strings.TrimSpace(appName) != "":
		return runAppleScript(ctx, fmt.Sprintf(`tell application "%s" to reopen`, escapeAppleScript(appName)))
	default:
		return errors.New("reopen requires app or bundle id")
	}
}

func annotateDesktopWaitError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	snapshot, snapErr := desktopSnapshot(ctx, false)
	if snapErr != nil {
		return err
	}
	if snapshot.FrontmostApp == nil {
		return fmt.Errorf("%w; host reports no frontmost application and desktop session may be inactive", err)
	}
	return err
}

func focusWindowReady(ctx context.Context, appName string, bundleID string, titleContains string, windowIndex int, waitUntil string, timeout time.Duration) (desktopReadyState, time.Duration, error) {
	startedAt := time.Now()
	deadline := time.Now().Add(timeout)
	var lastState desktopReadyState

	for {
		state, err := inspectWindowReadyState(ctx, appName, bundleID, titleContains, windowIndex)
		if err != nil {
			return state, time.Since(startedAt), err
		}
		lastState = state
		if state.HasMatchWindow {
			resolvedName := firstNonEmpty(state.App.Name, strings.TrimSpace(appName))
			resolvedBundleID := firstNonEmpty(state.App.BundleID, strings.TrimSpace(bundleID))
			name, resolvedBundleID, title, resolvedIndex, err := raiseWindow(ctx, resolvedName, resolvedBundleID, titleContains, windowIndex)
			if err == nil {
				finalTarget := waitUntil
				if finalTarget == desktopWaitWindow {
					finalTarget = desktopWaitFocused
				}
				remaining := time.Until(deadline)
				if remaining <= 0 {
					lastState.MatchedWindow = desktopWindowSnapshot{Index: resolvedIndex, Title: title}
					lastState.HasMatchWindow = resolvedIndex > 0 || strings.TrimSpace(title) != ""
					return lastState, time.Since(startedAt), fmt.Errorf("timed out waiting for %s (last ready_state=%s)", waitUntil, lastState.ReadyState)
				}
				finalState, _, waitErr := waitForAppReady(ctx, firstNonEmpty(name, resolvedName), firstNonEmpty(resolvedBundleID, bundleID), finalTarget, remaining)
				finalState.MatchedWindow = desktopWindowSnapshot{Index: resolvedIndex, Title: title}
				finalState.HasMatchWindow = true
				if waitErr != nil {
					return finalState, time.Since(startedAt), waitErr
				}
				return finalState, time.Since(startedAt), nil
			}
			if !isRetriableFocusWindowError(err) {
				return state, time.Since(startedAt), err
			}
		}

		if time.Now().After(deadline) {
			return lastState, time.Since(startedAt), fmt.Errorf("timed out waiting for %s (last ready_state=%s)", describeWindowTarget(titleContains, windowIndex), lastState.ReadyState)
		}
		if err := sleepContext(ctx, desktopReadyPollInterval); err != nil {
			return lastState, time.Since(startedAt), err
		}
	}
}

func inspectAppReadyState(ctx context.Context, appName string, bundleID string, includeWindows bool) (desktopReadyState, error) {
	state := desktopReadyState{
		App: desktopAppSnapshot{
			Name:     strings.TrimSpace(appName),
			BundleID: strings.TrimSpace(bundleID),
		},
		ReadyState: desktopReadyLaunching,
	}
	app, found, err := queryTargetApp(ctx, appName, bundleID, includeWindows)
	if err != nil {
		return state, err
	}
	if !found {
		return state, nil
	}
	state.App = app
	state.Found = true
	state.ReadyState = readyStateForApp(app)
	state.Interactive = app.Frontmost && app.WindowCount > 0
	return state, nil
}

func inspectWindowReadyState(ctx context.Context, appName string, bundleID string, titleContains string, windowIndex int) (desktopReadyState, error) {
	state, err := inspectAppReadyState(ctx, appName, bundleID, true)
	if err != nil {
		return state, err
	}
	if !state.Found {
		return state, nil
	}
	if match, ok := matchTargetWindow(state.App, titleContains, windowIndex); ok {
		state.MatchedWindow = match
		state.HasMatchWindow = true
	}
	return state, nil
}

func readyStateForApp(app desktopAppSnapshot) string {
	switch {
	case app.Frontmost && app.WindowCount > 0:
		return desktopReadyInteractive
	case app.Frontmost:
		return desktopReadyFocused
	case app.WindowCount > 0:
		return desktopReadyWindowVisible
	default:
		return desktopReadyProcessSeen
	}
}

func matchTargetWindow(app desktopAppSnapshot, titleContains string, windowIndex int) (desktopWindowSnapshot, bool) {
	switch {
	case windowIndex > 0:
		if windowIndex <= len(app.Windows) {
			return app.Windows[windowIndex-1], true
		}
		return desktopWindowSnapshot{}, false
	case strings.TrimSpace(titleContains) != "":
		for _, window := range app.Windows {
			if strings.Contains(window.Title, titleContains) {
				return window, true
			}
		}
		return desktopWindowSnapshot{}, false
	default:
		if len(app.Windows) == 0 {
			return desktopWindowSnapshot{}, false
		}
		return app.Windows[0], true
	}
}

func waitTargetNeedsWindows(waitUntil string) bool {
	return waitUntil == desktopWaitWindow || waitUntil == desktopWaitInteractive
}

func desktopWaitTargetParam(params map[string]any, fallback string, allowed ...string) (string, error) {
	value := strings.ToLower(stringParam(params, "wait_until"))
	if value == "" {
		return fallback, nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "", fmt.Errorf("unsupported wait_until %q", value)
}

func isRetriableFocusWindowError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, "application process not found"):
		return true
	case strings.Contains(message, "window not found"):
		return true
	case strings.Contains(message, "window index out of range"):
		return true
	default:
		return false
	}
}

func describeWindowTarget(titleContains string, windowIndex int) string {
	switch {
	case strings.TrimSpace(titleContains) != "":
		return fmt.Sprintf("window title containing %q", titleContains)
	case windowIndex > 0:
		return fmt.Sprintf("window index %d", windowIndex)
	default:
		return "window"
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func wantsTargetedScreenshot(params map[string]any) bool {
	return strings.TrimSpace(stringParam(params, "app")) != "" ||
		strings.TrimSpace(stringParam(params, "bundle_id")) != "" ||
		strings.TrimSpace(stringParam(params, "title_contains")) != "" ||
		intParam(params, "window_index") > 0
}

func resolveScreenshotTarget(ctx context.Context, params map[string]any) (desktopAppSnapshot, desktopWindowSnapshot, error) {
	appName := stringParam(params, "app")
	bundleID := stringParam(params, "bundle_id")
	titleContains := stringParam(params, "title_contains")
	windowIndex := intParam(params, "window_index")

	app, err := resolveTargetApp(ctx, appName, bundleID, true)
	if err != nil {
		return desktopAppSnapshot{}, desktopWindowSnapshot{}, err
	}
	window, ok := matchTargetWindow(app, titleContains, windowIndex)
	if !ok {
		return desktopAppSnapshot{}, desktopWindowSnapshot{}, fmt.Errorf("%s not found", describeWindowTarget(titleContains, windowIndex))
	}
	return app, window, nil
}

func screenshotRectArg(window desktopWindowSnapshot) (string, []int, error) {
	if len(window.Position) < 2 || len(window.Size) < 2 {
		return "", nil, errors.New("window bounds are unavailable")
	}
	x := window.Position[0]
	y := window.Position[1]
	width := window.Size[0]
	height := window.Size[1]
	if width <= 0 || height <= 0 {
		return "", nil, errors.New("window bounds are invalid")
	}
	return fmt.Sprintf("%d,%d,%d,%d", x, y, width, height), []int{x, y, width, height}, nil
}

type targetedCaptureResult struct {
	CaptureMode  string
	Bounds       []int
	WindowID     int
	Occluded     bool
	ActionStatus string
	Evidence     map[string]any
}

func captureFullScreenPNG(ctx context.Context, path string) error {
	return capturePNG(ctx, path, "-x", "-tpng")
}

func captureRectPNG(ctx context.Context, rectArg, path string) error {
	return capturePNG(ctx, path, "-x", "-tpng", "-R", rectArg)
}

func capturePNG(ctx context.Context, path string, args ...string) error {
	cmdArgs := append(append([]string(nil), args...), path)
	cmd := exec.CommandContext(ctx, "screencapture", cmdArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func captureTargetedPNG(ctx context.Context, app desktopAppSnapshot, window desktopWindowSnapshot, path string) (targetedCaptureResult, error) {
	result := targetedCaptureResult{
		CaptureMode:  "rect_fallback",
		ActionStatus: actionStatusRecovered,
		Evidence: map[string]any{
			"app":          app.Name,
			"bundle_id":    app.BundleID,
			"title":        window.Title,
			"window_index": window.Index,
		},
	}

	var contentErr error
	if nativeWindow, occluded, err := resolveWindowContentTarget(ctx, app, window); err == nil {
		result.WindowID = nativeWindow.WindowID
		result.Occluded = occluded
		if err := captureWindowContent(ctx, nativeWindow.WindowID, path); err == nil {
			result.CaptureMode = "window_content"
			result.Bounds = windowBoundsFromCGWindow(nativeWindow)
			result.ActionStatus = actionStatusVerified
			result.Evidence["window_id"] = nativeWindow.WindowID
			result.Evidence["occluded"] = occluded
			return result, nil
		} else {
			contentErr = err
			result.Evidence["window_id"] = nativeWindow.WindowID
			result.Evidence["window_content_error"] = err.Error()
		}
	} else {
		contentErr = err
		result.Evidence["window_match_error"] = err.Error()
	}

	rectArg, bounds, err := screenshotRectArg(window)
	if err != nil {
		if contentErr != nil {
			return result, fmt.Errorf("window capture unavailable: %v; rect fallback unavailable: %w", contentErr, err)
		}
		return result, err
	}
	if err := captureRectPNG(ctx, rectArg, path); err != nil {
		return result, err
	}
	result.Bounds = bounds
	return result, nil
}

func confidenceForCaptureMode(mode string) float64 {
	switch mode {
	case "window_content":
		return 0.98
	case "rect_fallback":
		return 0.8
	case "screen":
		return 0.99
	default:
		return 0.9
	}
}

func appsToAny(apps []desktopAppSnapshot, includeWindows bool) []any {
	out := make([]any, 0, len(apps))
	for _, app := range apps {
		out = append(out, toMap(&app, includeWindows))
	}
	return out
}

func windowsToAny(windows []desktopWindowSnapshot) []any {
	out := make([]any, 0, len(windows))
	for _, window := range windows {
		out = append(out, map[string]any{
			"index":    window.Index,
			"title":    window.Title,
			"role":     window.Role,
			"subrole":  window.Subrole,
			"position": window.Position,
			"size":     window.Size,
		})
	}
	return out
}

func toMap(app *desktopAppSnapshot, includeWindows bool) map[string]any {
	if app == nil {
		return nil
	}
	driver := resolveAppDriver(*app)
	out := map[string]any{
		"name":         app.Name,
		"bundle_id":    app.BundleID,
		"frontmost":    app.Frontmost,
		"window_count": app.WindowCount,
	}
	if app.PID > 0 {
		out["pid"] = app.PID
	}
	for key, value := range appDriverMetadata(driver) {
		out[key] = value
	}
	if includeWindows {
		out["windows"] = windowsToAny(app.Windows)
	}
	return out
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
	return intValue(params[key])
}

func intValue(value any) int {
	switch value := value.(type) {
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

func durationMillisParam(params map[string]any, key string, fallback time.Duration) time.Duration {
	if value := intParam(params, key); value > 0 {
		return time.Duration(value) * time.Millisecond
	}
	return fallback
}

func boolParam(params map[string]any, key string) bool {
	if len(params) == 0 {
		return false
	}
	value, _ := params[key].(bool)
	return value
}
