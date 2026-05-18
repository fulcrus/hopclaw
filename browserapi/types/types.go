// Package types defines the browser.v1 protocol types shared between
// the browserd process and the HopClaw runtime client adapter.
package types

// Action names for browser.v1 RPC.
const (
	ActionCreateSession     = "create_session"
	ActionCloseSession      = "close_session"
	ActionNavigate          = "navigate"
	ActionClick             = "click"
	ActionType              = "type"
	ActionEval              = "eval"
	ActionSnapshot          = "snapshot"
	ActionScreenshot        = "screenshot"
	ActionListTabs          = "list_tabs"
	ActionWaitFor           = "wait_for"
	ActionDownload          = "download"
	ActionGetCookies        = "get_cookies"
	ActionSetCookie         = "set_cookie"
	ActionReload            = "reload"
	ActionBack              = "back"
	ActionForward           = "forward"
	ActionHover             = "hover"
	ActionSelectOption      = "select_option"
	ActionFill              = "fill"
	ActionHandleDialog      = "handle_dialog"
	ActionPDF               = "pdf"
	ActionNetworkEnable     = "network_enable"
	ActionNetworkReqs       = "network_requests"
	ActionScroll            = "scroll"
	ActionDrag              = "drag"
	ActionUpload            = "upload"
	ActionNewTab            = "new_tab"
	ActionSwitchTab         = "switch_tab"
	ActionCloseTab          = "close_tab"
	ActionElementText       = "element_text"
	ActionElementAttr       = "element_attr"
	ActionElementVisible    = "element_visible"
	ActionKeyboard          = "keyboard"
	ActionIframe            = "iframe"
	ActionScreenshotLabeled = "screenshot_labeled"
	ActionSnapshotAria      = "snapshot_aria"
	ActionClickAria         = "click_aria"
	ActionTypeAria          = "type_aria"

	// Debugging / profiling actions.
	ActionTraceStart         = "trace_start"
	ActionTraceStop          = "trace_stop"
	ActionHARStart           = "har_start"
	ActionHARStop            = "har_stop"
	ActionConsoleStart       = "console_start"
	ActionConsoleMessages    = "console_messages"
	ActionPerformanceMetrics = "performance_metrics"

	// Device and environment emulation actions.
	ActionEmulateDevice  = "emulate_device"
	ActionEmulateVision  = "emulate_vision"
	ActionSetCredentials = "set_credentials"
	ActionSetGeolocation = "set_geolocation"
	ActionSetTimezone    = "set_timezone"
	ActionSetLocale      = "set_locale"
	ActionSetColorScheme = "set_color_scheme"
	ActionSetOffline     = "set_offline"
	ActionSetHeaders     = "set_headers"
)

// ---------------------------------------------------------------------------
// Browser Type
// ---------------------------------------------------------------------------

// BrowserType identifies a browser engine for multi-browser support.
type BrowserType string

const (
	BrowserChrome BrowserType = "chrome"
)

// Request is the generic envelope sent to browserd.
type Request struct {
	Action      string         `json:"action"`
	SessionID   string         `json:"session_id,omitempty"`
	BrowserType BrowserType    `json:"browser_type,omitempty"`
	Params      map[string]any `json:"params,omitempty"`
}

// Response is the generic envelope returned by browserd.
type Response struct {
	OK          bool           `json:"ok"`
	SessionID   string         `json:"session_id,omitempty"`
	ArtifactRef string         `json:"artifact_ref,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// Tab represents a single browser tab.
type Tab struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
}
