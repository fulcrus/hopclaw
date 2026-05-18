package toolruntime

func browserOpenOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"session_id": stringSchema("Created browser session ID."),
		"url":        stringSchema("Initial page URL when one was opened."),
		"title":      stringSchema("Initial page title when available."),
	}, "session_id")
}

func browserCloseOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the session was closed successfully."),
	}, "ok")
}

func browserNavigateOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":  booleanSchema("Whether navigation succeeded."),
		"url": stringSchema("Final page URL after navigation."),
	}, "ok", "url")
}

func browserClickOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":    booleanSchema("Whether the click succeeded."),
		"url":   stringSchema("Current page URL after the click when available."),
		"title": stringSchema("Current page title after the click when available."),
	}, "ok")
}

func browserTypeOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":       booleanSchema("Whether the type operation succeeded."),
		"selector": stringSchema("Selector that received the typed text."),
		"text":     stringSchema("Text that was entered."),
	}, "ok")
}

func browserScreenshotOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":           booleanSchema("Whether the screenshot was captured."),
		"artifact_ref": stringSchema("Path or reference to the saved screenshot image."),
		"artifact_uri": stringSchema("Normalized artifact URI for the screenshot."),
	}, "ok")
}

func browserScreenshotLabeledOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":            booleanSchema("Whether the labeled screenshot was captured."),
		"artifact_ref":  stringSchema("Path or reference to the saved screenshot image."),
		"artifact_uri":  stringSchema("Normalized artifact URI for the screenshot."),
		"elements":      map[string]any{"type": "object", "description": "Map of element labels (e1, e2, ...) to element metadata (tag, text, rect, etc.)."},
		"element_count": integerSchema("Number of labeled interactive elements found."),
	}, "ok")
}

func browserSnapshotAriaOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":            booleanSchema("Whether the ARIA snapshot was captured."),
		"text":          stringSchema("Human-readable text representation of the ARIA tree with ref labels."),
		"element_count": integerSchema("Number of interactive elements with assigned refs."),
		"refs": map[string]any{
			"type":        "array",
			"description": "Flat ordered summary of interactive ref targets extracted from the ARIA tree.",
			"items": objectSchema(map[string]any{
				"ref":                 stringSchema("Element ref ID such as e1 or e15."),
				"role":                stringSchema("ARIA role for the interactive element."),
				"name":                stringSchema("Accessible name for the element when available."),
				"value":               stringSchema("Current value for the element when available."),
				"action_hint":         stringSchema("Suggested interaction mode such as type, click, or select."),
				"specialized_control": booleanSchema("Whether the control is a specialized widget such as a spinbutton or combo box that may be optional in a generic form task."),
				"submit_candidate":    booleanSchema("Whether the element name suggests a submit/confirm action."),
			}, "ref"),
		},
		"tree": map[string]any{"type": "object", "description": "Structured ARIA tree with role, name, value, and ref fields."},
	}, "ok")
}

func browserClickAriaOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":      booleanSchema("Whether the click succeeded."),
		"ref":     stringSchema("Element ref ID that was clicked."),
		"clicked": booleanSchema("Whether the element click was acknowledged."),
		"url":     stringSchema("Current page URL after the click when available."),
		"title":   stringSchema("Current page title after the click when available."),
	}, "ok")
}

func browserTypeAriaOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":    booleanSchema("Whether the type operation succeeded."),
		"ref":   stringSchema("Element ref ID that received the typed text."),
		"text":  stringSchema("Text that was entered."),
		"typed": booleanSchema("Whether text entry completed."),
	}, "ok")
}

func browserSnapshotOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":           booleanSchema("Whether the snapshot was captured."),
		"content":      stringSchema("Page DOM content (text/html)."),
		"html":         stringSchema("Page DOM content (text/html)."),
		"url":          stringSchema("Current page URL."),
		"title":        stringSchema("Current page title."),
		"content_type": stringSchema("MIME type for the snapshot content."),
	}, "ok", "content")
}

func browserEvalOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":     booleanSchema("Whether the evaluation succeeded."),
		"result": stringSchema("Result of the JavaScript evaluation."),
	}, "ok")
}

func browserWaitOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the selector became visible."),
	}, "ok")
}

func browserTabsOutputSchema() map[string]any {
	tab := objectSchema(map[string]any{
		"id":    stringSchema("Tab identifier."),
		"url":   stringSchema("Tab URL."),
		"title": stringSchema("Tab title."),
	}, "id", "url", "title")
	return objectSchema(map[string]any{
		"tabs": arraySchema(tab, "List of open browser tabs."),
	}, "tabs")
}

func browserCookiesOutputSchema() map[string]any {
	cookie := objectSchema(map[string]any{
		"name":   stringSchema("Cookie name."),
		"value":  stringSchema("Cookie value."),
		"domain": stringSchema("Cookie domain."),
		"path":   stringSchema("Cookie path."),
	}, "name", "value", "domain", "path")
	return objectSchema(map[string]any{
		"cookies": arraySchema(cookie, "List of cookies for the current page."),
	}, "cookies")
}

func browserSetCookieOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the cookie was set successfully."),
	}, "ok")
}

func browserReloadOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the page was reloaded successfully."),
	}, "ok")
}

func browserBackOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":  booleanSchema("Whether back navigation succeeded."),
		"url": stringSchema("Page URL after navigating back."),
	}, "ok")
}

func browserForwardOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":  booleanSchema("Whether forward navigation succeeded."),
		"url": stringSchema("Page URL after navigating forward."),
	}, "ok")
}

func browserHoverOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the hover succeeded."),
	}, "ok")
}

func browserSelectOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the selection succeeded."),
	}, "ok")
}

func browserFillOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the fill operation succeeded."),
	}, "ok")
}

func browserStorageGetOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":    booleanSchema("Whether the storage read succeeded."),
		"value": stringSchema("Retrieved storage value, or null if not found."),
	}, "ok")
}

func browserStorageSetOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the storage write succeeded."),
	}, "ok")
}

func browserDialogHandleOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the dialog handler was set successfully."),
	}, "ok")
}

func browserPDFOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":           booleanSchema("Whether the PDF was generated."),
		"artifact_ref": stringSchema("Path or reference to the saved PDF."),
		"artifact_uri": stringSchema("Normalized artifact URI for the PDF."),
	}, "ok")
}

func browserNetworkEnableOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether network capturing was enabled."),
	}, "ok")
}

func browserNetworkRequestsOutputSchema() map[string]any {
	request := objectSchema(map[string]any{
		"url":           stringSchema("Request URL."),
		"type":          stringSchema("Initiator type."),
		"duration_ms":   integerSchema("Request duration in milliseconds."),
		"start_time_ms": integerSchema("Request start time in milliseconds."),
	}, "url")
	return objectSchema(map[string]any{
		"ok":       booleanSchema("Whether the request list was retrieved."),
		"requests": arraySchema(request, "Captured network requests."),
	}, "ok", "requests")
}

func browserScrollOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the scroll succeeded."),
	}, "ok")
}

func browserDragOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the drag operation succeeded."),
	}, "ok")
}

func browserUploadOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the file upload succeeded."),
	}, "ok")
}

func browserDownloadOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":        booleanSchema("Whether the download completed."),
		"file_path": stringSchema("Path to the downloaded file."),
	}, "ok")
}

func browserTabNewOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":        booleanSchema("Whether the new tab was opened."),
		"target_id": stringSchema("Target ID of the new tab."),
	}, "ok")
}

func browserTabSwitchOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the tab switch succeeded."),
	}, "ok")
}

func browserTabCloseOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the tab was closed."),
	}, "ok")
}

func browserElementTextOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":   booleanSchema("Whether the text was retrieved."),
		"text": stringSchema("Text content of the element."),
	}, "ok", "text")
}

func browserElementAttrOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":    booleanSchema("Whether the attribute was retrieved."),
		"value": stringSchema("Attribute value, or null if not present."),
		"found": booleanSchema("Whether the attribute exists on the element."),
	}, "ok")
}

func browserElementVisibleOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":      booleanSchema("Whether the check succeeded."),
		"visible": booleanSchema("Whether the element is visible."),
	}, "ok", "visible")
}

func browserKeyboardOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the key event was sent."),
	}, "ok")
}

func browserIframeOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":    booleanSchema("Whether the iframe context switch succeeded."),
		"frame": stringSchema("The frame that is now active: selector or main."),
	}, "ok")
}

func browserTraceStartOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":         booleanSchema("Whether tracing was started."),
		"categories": stringSchema("Trace categories being captured."),
	}, "ok")
}

func browserTraceStopOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":             booleanSchema("Whether the trace was stopped."),
		"event_count":    integerSchema("Number of trace events collected."),
		"content_base64": stringSchema("Base64-encoded JSON array of trace events."),
	}, "ok", "event_count")
}

func browserHARStartOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether HAR recording was started."),
	}, "ok")
}

func browserHARStopOutputSchema() map[string]any {
	harEntry := objectSchema(map[string]any{
		"url":           stringSchema("Request URL."),
		"method":        stringSchema("HTTP method."),
		"status":        integerSchema("HTTP response status code."),
		"response_size": integerSchema("Response size in bytes."),
		"duration":      numberSchema("Request duration in seconds."),
	}, "url", "method", "status")
	return objectSchema(map[string]any{
		"ok":          booleanSchema("Whether the HAR data was retrieved."),
		"format":      stringSchema("Output format: full or summary."),
		"entry_count": integerSchema("Number of captured HTTP entries."),
		"entries":     arraySchema(harEntry, "Captured HTTP request/response entries."),
	}, "ok", "entry_count", "entries")
}

func browserConsoleStartOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether console capture was started."),
	}, "ok")
}

func browserConsoleMessagesOutputSchema() map[string]any {
	msg := objectSchema(map[string]any{
		"level":  stringSchema("Console message level (log, warn, error, info, debug, etc.)."),
		"text":   stringSchema("Console message text."),
		"url":    stringSchema("Source URL where the message originated."),
		"line":   integerSchema("Source line number."),
		"column": integerSchema("Source column number."),
	}, "level", "text")
	return objectSchema(map[string]any{
		"ok":            booleanSchema("Whether the messages were retrieved."),
		"message_count": integerSchema("Number of captured console messages."),
		"messages":      arraySchema(msg, "Captured console messages."),
	}, "ok", "message_count", "messages")
}

func browserPerformanceOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the metrics were retrieved."),
		"metrics": map[string]any{
			"type":        "object",
			"description": "Map of metric names to their numeric values.",
		},
	}, "ok", "metrics")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// requireBrowserClient returns an error if the browser client is not wired.

func browserEmulateDeviceOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":         booleanSchema("Whether the emulation was applied."),
		"width":      integerSchema("Applied viewport width."),
		"height":     integerSchema("Applied viewport height."),
		"scale":      numberSchema("Applied device scale factor."),
		"mobile":     booleanSchema("Whether mobile mode is enabled."),
		"user_agent": stringSchema("Applied User-Agent string."),
		"device":     stringSchema("Device preset name if one was used."),
	}, "ok")
}

// ---------------------------------------------------------------------------
// browser.emulate_vision schemas
// ---------------------------------------------------------------------------

func browserEmulateVisionOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":   booleanSchema("Whether the vision emulation was applied."),
		"type": stringSchema("Applied vision deficiency type."),
	}, "ok")
}

// ---------------------------------------------------------------------------
// browser.set_geolocation
// ---------------------------------------------------------------------------

func browserSetGeolocationOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":        booleanSchema("Whether the geolocation was set."),
		"latitude":  numberSchema("Applied latitude."),
		"longitude": numberSchema("Applied longitude."),
		"accuracy":  numberSchema("Applied accuracy in meters."),
	}, "ok")
}

// ---------------------------------------------------------------------------
// browser.set_timezone
// ---------------------------------------------------------------------------

func browserSetTimezoneOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":          booleanSchema("Whether the timezone was set."),
		"timezone_id": stringSchema("Applied timezone ID."),
	}, "ok")
}

// ---------------------------------------------------------------------------
// browser.set_locale
// ---------------------------------------------------------------------------

func browserSetLocaleOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":     booleanSchema("Whether the locale was set."),
		"locale": stringSchema("Applied locale."),
	}, "ok")
}

// ---------------------------------------------------------------------------
// browser.set_color_scheme
// ---------------------------------------------------------------------------

func browserSetColorSchemeOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":           booleanSchema("Whether the color scheme was set."),
		"color_scheme": stringSchema("Applied color scheme."),
	}, "ok")
}

// ---------------------------------------------------------------------------
// browser.set_offline
// ---------------------------------------------------------------------------

func browserSetOfflineOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":      booleanSchema("Whether the offline mode was set."),
		"offline": booleanSchema("Whether offline mode is enabled."),
	}, "ok")
}

// ---------------------------------------------------------------------------
// browser.set_headers
// ---------------------------------------------------------------------------

func browserSetHeadersOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":          booleanSchema("Whether the headers were set."),
		"headers_set": integerSchema("Number of headers applied."),
	}, "ok")
}

// ---------------------------------------------------------------------------
// browser.set_credentials
// ---------------------------------------------------------------------------

func browserSetCredentialsOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the credentials were set or cleared."),
	}, "ok")
}
