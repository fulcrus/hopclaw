package toolruntime

func browserScrollInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"direction": map[string]any{
				"type":        "string",
				"description": "Scroll direction. Defaults to down.",
				"enum":        []string{"up", "down", "left", "right"},
			},
			"amount": map[string]any{
				"type":        "integer",
				"description": "Number of pixels to scroll. Defaults to 300.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "Optional CSS selector of an element to scroll within.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserDragInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"source_selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to drag from.",
			},
			"target_selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to drop onto.",
			},
		},
		"required":             []string{"session_id", "source_selector", "target_selector"},
		"additionalProperties": false,
	}
}

func browserUploadInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the file input element.",
			},
			"file_path": map[string]any{
				"type":        "string",
				"description": "Local file path to upload.",
			},
		},
		"required":             []string{"session_id", "selector", "file_path"},
		"additionalProperties": false,
	}
}

func browserDownloadInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"url": map[string]any{
				"type":        "string",
				"description": "Optional URL to navigate to in order to trigger a download.",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "Maximum wait time in milliseconds for the download to complete. Defaults to 30000.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserTabNewInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"url": map[string]any{
				"type":        "string",
				"description": "Optional URL to navigate to in the new tab.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserTabSwitchInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"target_id": map[string]any{
				"type":        "string",
				"description": "Target ID of the tab to switch to (from browser.tabs result).",
			},
		},
		"required":             []string{"session_id", "target_id"},
		"additionalProperties": false,
	}
}

func browserTabCloseInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"target_id": map[string]any{
				"type":        "string",
				"description": "Target ID of the tab to close.",
			},
		},
		"required":             []string{"session_id", "target_id"},
		"additionalProperties": false,
	}
}

func browserElementTextInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element.",
			},
		},
		"required":             []string{"session_id", "selector"},
		"additionalProperties": false,
	}
}

func browserElementAttrInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element.",
			},
			"attribute": map[string]any{
				"type":        "string",
				"description": "Name of the attribute to retrieve.",
			},
		},
		"required":             []string{"session_id", "selector", "attribute"},
		"additionalProperties": false,
	}
}

func browserElementVisibleInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to check.",
			},
		},
		"required":             []string{"session_id", "selector"},
		"additionalProperties": false,
	}
}

func browserKeyboardInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"keys": map[string]any{
				"type":        "string",
				"description": "Key or key combination to send (e.g. Enter, Escape, Control+a, Shift+Tab).",
			},
		},
		"required":             []string{"session_id", "keys"},
		"additionalProperties": false,
	}
}

func browserIframeInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the iframe to switch into. Omit to switch back to the main frame.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserTraceStartInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"categories": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Trace categories to capture (e.g. devtools.timeline, blink.user_timing). Defaults to devtools.timeline,blink.user_timing.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserTraceStopInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserHARStartInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserHARStopInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"format": map[string]any{
				"type":        "string",
				"description": "Output format: full (all fields) or summary (key fields only). Defaults to summary.",
				"enum":        []string{"full", "summary"},
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserConsoleStartInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserConsoleMessagesInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"clear": map[string]any{
				"type":        "boolean",
				"description": "Whether to clear the captured messages after reading. Defaults to false.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserPerformanceInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserSnapshotAriaInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserClickAriaInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"ref": map[string]any{
				"type":        "string",
				"description": "Element ref ID from a previous snapshot_aria call (e.g. 'e1', 'e2').",
			},
		},
		"required":             []string{"session_id", "ref"},
		"additionalProperties": false,
	}
}

func browserTypeAriaInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"ref": map[string]any{
				"type":        "string",
				"description": "Element ref ID from a previous snapshot_aria call (e.g. 'e1', 'e2').",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Text to type into the element.",
			},
			"clear": map[string]any{
				"type":        "boolean",
				"description": "Whether to clear the field before typing. Defaults to false.",
			},
		},
		"required":             []string{"session_id", "ref", "text"},
		"additionalProperties": false,
	}
}

func browserEmulateDeviceInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"device": map[string]any{
				"type":        "string",
				"description": "Device preset name: iphone_14, iphone_14_pro_max, ipad_pro_11, pixel_7, galaxy_s23, desktop_1080p, desktop_1440p, desktop_4k. If set, width/height/scale/mobile are auto-filled.",
			},
			"width": map[string]any{
				"type":        "integer",
				"description": "Viewport width in pixels. Overrides device preset.",
			},
			"height": map[string]any{
				"type":        "integer",
				"description": "Viewport height in pixels. Overrides device preset.",
			},
			"scale": map[string]any{
				"type":        "number",
				"description": "Device scale factor (e.g. 2.0 for Retina). Default 1.0.",
			},
			"mobile": map[string]any{
				"type":        "boolean",
				"description": "Whether to emulate a mobile device. Default false.",
			},
			"user_agent": map[string]any{
				"type":        "string",
				"description": "Custom User-Agent string. Overrides device preset.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserEmulateVisionInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "Vision deficiency type: none, achromatopsia, deuteranopia, protanopia, tritanopia, blurredVision.",
			},
		},
		"required":             []string{"session_id", "type"},
		"additionalProperties": false,
	}
}

func browserSetGeolocationInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{"type": "string", "description": "Browser session ID."},
			"latitude":   map[string]any{"type": "number", "description": "Latitude in degrees."},
			"longitude":  map[string]any{"type": "number", "description": "Longitude in degrees."},
			"accuracy":   map[string]any{"type": "number", "description": "Accuracy in meters. Default 10."},
			"clear":      map[string]any{"type": "boolean", "description": "Set true to clear geolocation override."},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserSetTimezoneInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id":  map[string]any{"type": "string", "description": "Browser session ID."},
			"timezone_id": map[string]any{"type": "string", "description": "IANA timezone ID (e.g. 'America/New_York', 'Asia/Tokyo')."},
		},
		"required":             []string{"session_id", "timezone_id"},
		"additionalProperties": false,
	}
}

func browserSetLocaleInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{"type": "string", "description": "Browser session ID."},
			"locale":     map[string]any{"type": "string", "description": "BCP 47 locale (e.g. 'en-US', 'zh-CN', 'ja-JP')."},
		},
		"required":             []string{"session_id", "locale"},
		"additionalProperties": false,
	}
}

func browserSetColorSchemeInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{"type": "string", "description": "Browser session ID."},
			"scheme":     map[string]any{"type": "string", "description": "Color scheme: 'dark', 'light', or 'no-preference'."},
		},
		"required":             []string{"session_id", "scheme"},
		"additionalProperties": false,
	}
}

func browserSetOfflineInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{"type": "string", "description": "Browser session ID."},
			"offline":    map[string]any{"type": "boolean", "description": "Whether to go offline. Default true."},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserSetHeadersInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{"type": "string", "description": "Browser session ID."},
			"headers":    map[string]any{"type": "object", "description": "Map of header names to values.", "additionalProperties": map[string]any{"type": "string"}},
		},
		"required":             []string{"session_id", "headers"},
		"additionalProperties": false,
	}
}

func browserSetCredentialsInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{"type": "string", "description": "Browser session ID."},
			"username":   map[string]any{"type": "string", "description": "HTTP Basic Auth username."},
			"password":   map[string]any{"type": "string", "description": "HTTP Basic Auth password."},
			"clear":      map[string]any{"type": "boolean", "description": "Set true to clear credentials and disable auth interception."},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}
