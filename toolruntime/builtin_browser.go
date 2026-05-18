package toolruntime

import (
	"time"

	"github.com/fulcrus/hopclaw/skill"
)

const (
	// defaultBrowserWaitTimeout is the default wait timeout in milliseconds for browser.wait.
	defaultBrowserWaitTimeout = 10000

	// defaultBrowserScrollAmount is the default scroll amount in pixels for browser.scroll.
	defaultBrowserScrollAmount = 300

	// defaultBrowserDownloadTimeout is the default download timeout in milliseconds for browser.download.
	defaultBrowserDownloadTimeout = 30000

	// defaultStorageType is the default storage type for browser.storage_get and browser.storage_set.
	defaultStorageType = "local"

	// defaultBrowserCaptureRequestTimeout gives heavier capture operations enough
	// room to complete after browserd starts processing them.
	defaultBrowserCaptureRequestTimeout = 105 * time.Second
)

func browserToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	_ = cfg
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.open",
				Description:     "Create a new browser automation session, optionally navigating to a URL.",
				InputSchema:     browserOpenInputSchema(),
				OutputSchema:    browserOpenOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:open",
			},
			Handler: handleBrowserOpen,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.close",
				Description:     "Close an existing browser automation session.",
				InputSchema:     browserCloseInputSchema(),
				OutputSchema:    browserCloseOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:close:{session_id}",
			},
			Handler: handleBrowserClose,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.navigate",
				Description:     "Navigate the active page to a URL.",
				InputSchema:     browserNavigateInputSchema(),
				OutputSchema:    browserNavigateOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:navigate:{session_id}",
			},
			Handler: handleBrowserNavigate,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.click",
				Description:     "Click an element identified by a CSS selector. Use this for buttons, links, checkboxes, radio buttons, and submit controls.",
				InputSchema:     browserClickInputSchema(),
				OutputSchema:    browserClickOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:click:{session_id}",
			},
			Handler: handleBrowserClick,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.type",
				Description:     "Type text into a text input, textarea, or other text-entry element identified by a CSS selector. Do not use this for <select>, checkbox, radio, or submit controls; use browser.select or browser.click instead.",
				InputSchema:     browserTypeInputSchema(),
				OutputSchema:    browserTypeOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:type:{session_id}",
			},
			Handler: handleBrowserType,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.screenshot",
				Description:     "Capture a screenshot of the current page.",
				InputSchema:     browserScreenshotInputSchema(),
				OutputSchema:    browserScreenshotOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "browser:screenshot:{session_id}",
			},
			Handler: handleBrowserScreenshot,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.screenshot_labeled",
				Description:     "Capture a screenshot with interactive elements highlighted and labeled (e1, e2, ...). Returns the image and a mapping of labels to element metadata.",
				InputSchema:     browserScreenshotLabeledInputSchema(),
				OutputSchema:    browserScreenshotLabeledOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "browser:screenshot_labeled:{session_id}",
			},
			Handler: handleBrowserScreenshotLabeled,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.snapshot_aria",
				Description:     "Get an ARIA accessibility tree snapshot of the current page. Returns a structured tree with ref IDs (e1, e2, ...) for interactive elements that can be used with browser.click_aria and browser.type_aria.",
				InputSchema:     browserSnapshotAriaInputSchema(),
				OutputSchema:    browserSnapshotAriaOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "browser:snapshot_aria:{session_id}",
			},
			Handler: handleBrowserSnapshotAria,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.click_aria",
				Description:     "Click an element by its ARIA ref ID from a previous snapshot_aria call. More stable than CSS selectors as it uses ARIA role+name.",
				InputSchema:     browserClickAriaInputSchema(),
				OutputSchema:    browserClickAriaOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:click_aria:{session_id}",
			},
			Handler: handleBrowserClickAria,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.type_aria",
				Description:     "Type text into an element by its ARIA ref ID from a previous snapshot_aria call.",
				InputSchema:     browserTypeAriaInputSchema(),
				OutputSchema:    browserTypeAriaOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:type_aria:{session_id}",
			},
			Handler: handleBrowserTypeAria,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.snapshot",
				Description:     "Get a DOM snapshot (HTML content) of the current page.",
				InputSchema:     browserSnapshotInputSchema(),
				OutputSchema:    browserSnapshotOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "browser:snapshot:{session_id}",
			},
			Handler: handleBrowserSnapshot,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "browser.eval",
				Description:      "Execute a JavaScript expression in the browser page context.",
				InputSchema:      browserEvalInputSchema(),
				OutputSchema:     browserEvalOutputSchema(),
				SideEffectClass:  "remote_write",
				RequiresApproval: true,
				ExecutionKey:     "browser:eval:{session_id}",
			},
			Handler: handleBrowserEval,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.wait",
				Description:     "Wait for a CSS selector to become visible on the page.",
				InputSchema:     browserWaitInputSchema(),
				OutputSchema:    browserWaitOutputSchema(),
				SideEffectClass: "read",
				ExecutionKey:    "browser:wait:{session_id}",
			},
			Handler: handleBrowserWait,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.tabs",
				Description:     "List all open tabs in the browser session.",
				InputSchema:     browserTabsInputSchema(),
				OutputSchema:    browserTabsOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "browser:tabs:{session_id}",
			},
			Handler: handleBrowserTabs,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.cookies",
				Description:     "Get all cookies for the current page.",
				InputSchema:     browserCookiesInputSchema(),
				OutputSchema:    browserCookiesOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "browser:cookies:{session_id}",
			},
			Handler: handleBrowserCookies,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.set_cookie",
				Description:     "Set a cookie in the browser session.",
				InputSchema:     browserSetCookieInputSchema(),
				OutputSchema:    browserSetCookieOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:set_cookie:{session_id}",
			},
			Handler: handleBrowserSetCookie,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.reload",
				Description:     "Reload the current page.",
				InputSchema:     browserReloadInputSchema(),
				OutputSchema:    browserReloadOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:reload:{session_id}",
			},
			Handler: handleBrowserReload,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.back",
				Description:     "Navigate back in browser history.",
				InputSchema:     browserBackInputSchema(),
				OutputSchema:    browserBackOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:back:{session_id}",
			},
			Handler: handleBrowserBack,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.forward",
				Description:     "Navigate forward in browser history.",
				InputSchema:     browserForwardInputSchema(),
				OutputSchema:    browserForwardOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:forward:{session_id}",
			},
			Handler: handleBrowserForward,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.hover",
				Description:     "Hover over an element identified by a CSS selector.",
				InputSchema:     browserHoverInputSchema(),
				OutputSchema:    browserHoverOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:hover:{session_id}",
			},
			Handler: handleBrowserHover,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.select",
				Description:     "Select option(s) in a <select> element. Prefer this over browser.type when the target is a dropdown.",
				InputSchema:     browserSelectInputSchema(),
				OutputSchema:    browserSelectOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:select:{session_id}",
			},
			Handler: handleBrowserSelect,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.fill",
				Description:     "Fill a form field by clearing existing content and typing a new value.",
				InputSchema:     browserFillInputSchema(),
				OutputSchema:    browserFillOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:fill:{session_id}",
			},
			Handler: handleBrowserFill,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.storage_get",
				Description:     "Get a value from localStorage or sessionStorage.",
				InputSchema:     browserStorageGetInputSchema(),
				OutputSchema:    browserStorageGetOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "browser:storage_get:{session_id}",
			},
			Handler: handleBrowserStorageGet,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.storage_set",
				Description:     "Set a value in localStorage or sessionStorage.",
				InputSchema:     browserStorageSetInputSchema(),
				OutputSchema:    browserStorageSetOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:storage_set:{session_id}",
			},
			Handler: handleBrowserStorageSet,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.dialog_handle",
				Description:     "Handle the next browser dialog (alert, confirm, or prompt).",
				InputSchema:     browserDialogHandleInputSchema(),
				OutputSchema:    browserDialogHandleOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:dialog_handle:{session_id}",
			},
			Handler: handleBrowserDialogHandle,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.pdf",
				Description:     "Generate a PDF of the current page.",
				InputSchema:     browserPDFInputSchema(),
				OutputSchema:    browserPDFOutputSchema(),
				SideEffectClass: "read",
				ExecutionKey:    "browser:pdf:{session_id}",
			},
			Handler: handleBrowserPDF,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.network_enable",
				Description:     "Start capturing network requests in the browser session.",
				InputSchema:     browserNetworkEnableInputSchema(),
				OutputSchema:    browserNetworkEnableOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:network_enable:{session_id}",
			},
			Handler: handleBrowserNetworkEnable,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.network_requests",
				Description:     "Get captured network requests from the browser session.",
				InputSchema:     browserNetworkRequestsInputSchema(),
				OutputSchema:    browserNetworkRequestsOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "browser:network_requests:{session_id}",
			},
			Handler: handleBrowserNetworkRequests,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.scroll",
				Description:     "Scroll the page or a specific element in any direction.",
				InputSchema:     browserScrollInputSchema(),
				OutputSchema:    browserScrollOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:scroll:{session_id}",
			},
			Handler: handleBrowserScroll,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.drag",
				Description:     "Drag an element from a source selector to a target selector.",
				InputSchema:     browserDragInputSchema(),
				OutputSchema:    browserDragOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:drag:{session_id}",
			},
			Handler: handleBrowserDrag,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.upload",
				Description:     "Upload a file to a file input element.",
				InputSchema:     browserUploadInputSchema(),
				OutputSchema:    browserUploadOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:upload:{session_id}",
			},
			Handler: handleBrowserUpload,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.download",
				Description:     "Download a file, optionally by navigating to a URL, and wait for the download to complete.",
				InputSchema:     browserDownloadInputSchema(),
				OutputSchema:    browserDownloadOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:download:{session_id}",
			},
			Handler: handleBrowserDownload,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.tab_new",
				Description:     "Open a new browser tab, optionally navigating to a URL.",
				InputSchema:     browserTabNewInputSchema(),
				OutputSchema:    browserTabNewOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:tab_new:{session_id}",
			},
			Handler: handleBrowserTabNew,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.tab_switch",
				Description:     "Switch to a specific browser tab by target ID.",
				InputSchema:     browserTabSwitchInputSchema(),
				OutputSchema:    browserTabSwitchOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:tab_switch:{session_id}",
			},
			Handler: handleBrowserTabSwitch,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.tab_close",
				Description:     "Close a specific browser tab by target ID.",
				InputSchema:     browserTabCloseInputSchema(),
				OutputSchema:    browserTabCloseOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:tab_close:{session_id}",
			},
			Handler: handleBrowserTabClose,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.element_text",
				Description:     "Get the text content of an element identified by a CSS selector.",
				InputSchema:     browserElementTextInputSchema(),
				OutputSchema:    browserElementTextOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "browser:element_text:{session_id}",
			},
			Handler: handleBrowserElementText,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.element_attr",
				Description:     "Get the value of an attribute on an element identified by a CSS selector.",
				InputSchema:     browserElementAttrInputSchema(),
				OutputSchema:    browserElementAttrOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "browser:element_attr:{session_id}",
			},
			Handler: handleBrowserElementAttr,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.element_visible",
				Description:     "Check whether an element identified by a CSS selector is visible on the page.",
				InputSchema:     browserElementVisibleInputSchema(),
				OutputSchema:    browserElementVisibleOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "browser:element_visible:{session_id}",
			},
			Handler: handleBrowserElementVisible,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.keyboard",
				Description:     "Send keyboard keys or shortcuts to the browser (e.g. Enter, Escape, Control+a).",
				InputSchema:     browserKeyboardInputSchema(),
				OutputSchema:    browserKeyboardOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "browser:keyboard:{session_id}",
			},
			Handler: handleBrowserKeyboard,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.iframe",
				Description:     "Switch execution context to an iframe, or back to the main frame if no selector is given.",
				InputSchema:     browserIframeInputSchema(),
				OutputSchema:    browserIframeOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:iframe:{session_id}",
			},
			Handler: handleBrowserIframe,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.trace_start",
				Description:     "Start recording a browser trace/performance timeline for debugging.",
				InputSchema:     browserTraceStartInputSchema(),
				OutputSchema:    browserTraceStartOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:trace_start:{session_id}",
			},
			Handler: handleBrowserTraceStart,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.trace_stop",
				Description:     "Stop recording and return the trace data.",
				InputSchema:     browserTraceStopInputSchema(),
				OutputSchema:    browserTraceStopOutputSchema(),
				SideEffectClass: "read",
				ExecutionKey:    "browser:trace_stop:{session_id}",
			},
			Handler: handleBrowserTraceStop,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.har_start",
				Description:     "Start capturing HTTP traffic as HAR (HTTP Archive) for debugging network requests.",
				InputSchema:     browserHARStartInputSchema(),
				OutputSchema:    browserHARStartOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:har_start:{session_id}",
			},
			Handler: handleBrowserHARStart,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.har_stop",
				Description:     "Stop capturing HTTP traffic and return the HAR data.",
				InputSchema:     browserHARStopInputSchema(),
				OutputSchema:    browserHARStopOutputSchema(),
				SideEffectClass: "read",
				ExecutionKey:    "browser:har_stop:{session_id}",
			},
			Handler: handleBrowserHARStop,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.console_start",
				Description:     "Start capturing browser console messages (log, warn, error, etc.).",
				InputSchema:     browserConsoleStartInputSchema(),
				OutputSchema:    browserConsoleStartOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:console_start:{session_id}",
			},
			Handler: handleBrowserConsoleStart,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.console_messages",
				Description:     "Get captured console messages from the browser session.",
				InputSchema:     browserConsoleMessagesInputSchema(),
				OutputSchema:    browserConsoleMessagesOutputSchema(),
				SideEffectClass: "read",
				ExecutionKey:    "browser:console_messages:{session_id}",
			},
			Handler: handleBrowserConsoleMessages,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.performance",
				Description:     "Get page performance metrics (Core Web Vitals, DOM content loaded, first paint, etc.).",
				InputSchema:     browserPerformanceInputSchema(),
				OutputSchema:    browserPerformanceOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "browser:performance:{session_id}",
			},
			Handler: handleBrowserPerformance,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.emulate_device",
				Description:     "Set device emulation (viewport, scale, mobile mode, user agent). Use built-in presets like 'iphone_14', 'pixel_7', 'ipad_pro_11', or custom dimensions.",
				InputSchema:     browserEmulateDeviceInputSchema(),
				OutputSchema:    browserEmulateDeviceOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:emulate_device:{session_id}",
			},
			Handler: handleBrowserEmulateDevice,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.emulate_vision",
				Description:     "Simulate vision deficiencies for accessibility testing: achromatopsia, deuteranopia, protanopia, tritanopia, or blurredVision. Use 'none' to reset.",
				InputSchema:     browserEmulateVisionInputSchema(),
				OutputSchema:    browserEmulateVisionOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:emulate_vision:{session_id}",
			},
			Handler: handleBrowserEmulateVision,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.set_geolocation",
				Description:     "Set or clear geolocation override. Emulates GPS coordinates for the page.",
				InputSchema:     browserSetGeolocationInputSchema(),
				OutputSchema:    browserSetGeolocationOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:set_geolocation:{session_id}",
			},
			Handler: handleBrowserSetGeolocation,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.set_timezone",
				Description:     "Override the browser timezone (e.g. 'America/New_York', 'Asia/Tokyo').",
				InputSchema:     browserSetTimezoneInputSchema(),
				OutputSchema:    browserSetTimezoneOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:set_timezone:{session_id}",
			},
			Handler: handleBrowserSetTimezone,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.set_locale",
				Description:     "Override the browser locale (e.g. 'en-US', 'zh-CN', 'ja-JP').",
				InputSchema:     browserSetLocaleInputSchema(),
				OutputSchema:    browserSetLocaleOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:set_locale:{session_id}",
			},
			Handler: handleBrowserSetLocale,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.set_color_scheme",
				Description:     "Set the preferred color scheme: 'dark', 'light', or 'no-preference'.",
				InputSchema:     browserSetColorSchemeInputSchema(),
				OutputSchema:    browserSetColorSchemeOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:set_color_scheme:{session_id}",
			},
			Handler: handleBrowserSetColorScheme,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.set_offline",
				Description:     "Enable or disable offline network emulation.",
				InputSchema:     browserSetOfflineInputSchema(),
				OutputSchema:    browserSetOfflineOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:set_offline:{session_id}",
			},
			Handler: handleBrowserSetOffline,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.set_headers",
				Description:     "Set extra HTTP headers that will be sent with every request.",
				InputSchema:     browserSetHeadersInputSchema(),
				OutputSchema:    browserSetHeadersOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:set_headers:{session_id}",
			},
			Handler: handleBrowserSetHeaders,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "browser.set_credentials",
				Description:     "Set HTTP Basic Auth credentials for automatic authentication. Use clear=true to disable.",
				InputSchema:     browserSetCredentialsInputSchema(),
				OutputSchema:    browserSetCredentialsOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "browser:set_credentials:{session_id}",
			},
			Handler: handleBrowserSetCredentials,
		},
	}
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------
