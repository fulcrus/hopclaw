// Package types defines the desktop.v1 protocol shared between
// hopclaw-desktopd and the runtime capability client.
package types

const (
	ActionCreateSession      = "create_session"
	ActionCloseSession       = "close_session"
	ActionOpenApp            = "open_app"
	ActionFocusApp           = "focus_app"
	ActionFocusWindow        = "focus_window"
	ActionListApps           = "list_apps"
	ActionListWindows        = "list_windows"
	ActionDescribeHost       = "describe_host"
	ActionListCommands       = "list_commands"
	ActionInvokeCommand      = "invoke_command"
	ActionListDriverActions  = "list_driver_actions"
	ActionInvokeDriverAction = "invoke_driver_action"
	ActionCaptureTree        = "capture_tree"
	ActionTypeText           = "type_text"
	ActionHotkey             = "hotkey"
	ActionScreenshot         = "screenshot"
	ActionScreenRecord       = "screen_record"
	ActionClipboardRead      = "clipboard_read"
	ActionClipboardWrite     = "clipboard_write"
	ActionMouseMove          = "mouse_move"
	ActionMouseClick         = "mouse_click"
	ActionScroll             = "scroll"

	// UILocator actions — element discovery and OCR-based targeting.
	ActionFindElement     = "find_element"
	ActionClickElement    = "click_element"
	ActionSetElementValue = "set_element_value"
	ActionClearElement    = "clear_element"
	ActionGetElementValue = "get_element_value"
	ActionAssertElement   = "assert_element"
	ActionFindText        = "find_text"
	ActionClickText       = "click_text"
)

type Request struct {
	Action    string         `json:"action"`
	SessionID string         `json:"session_id,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
}

type Response struct {
	OK          bool           `json:"ok"`
	SessionID   string         `json:"session_id,omitempty"`
	ArtifactRef string         `json:"artifact_ref,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
	Error       string         `json:"error,omitempty"`
}
