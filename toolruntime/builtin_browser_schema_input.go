package toolruntime

func browserOpenInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "Optional initial URL to navigate to after opening the session.",
			},
		},
		"additionalProperties": false,
	}
}

func browserCloseInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID to close.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserNavigateInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"url": map[string]any{
				"type":        "string",
				"description": "URL to navigate to.",
			},
		},
		"required":             []string{"session_id", "url"},
		"additionalProperties": false,
	}
}

func browserClickInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to click.",
			},
		},
		"required":             []string{"session_id", "selector"},
		"additionalProperties": false,
	}
}

func browserTypeInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to type into.",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Text to type into the element.",
			},
		},
		"required":             []string{"session_id", "selector", "text"},
		"additionalProperties": false,
	}
}

func browserScreenshotInputSchema() map[string]any {
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

func browserScreenshotLabeledInputSchema() map[string]any {
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

func browserSnapshotInputSchema() map[string]any {
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

func browserEvalInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"expression": map[string]any{
				"type":        "string",
				"description": "JavaScript expression to evaluate.",
			},
		},
		"required":             []string{"session_id", "expression"},
		"additionalProperties": false,
	}
}

func browserWaitInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector to wait for.",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "Maximum wait time in milliseconds. Defaults to 10000.",
			},
		},
		"required":             []string{"session_id", "selector"},
		"additionalProperties": false,
	}
}

func browserTabsInputSchema() map[string]any {
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

func browserCookiesInputSchema() map[string]any {
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

func browserSetCookieInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Cookie name.",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Cookie value.",
			},
			"domain": map[string]any{
				"type":        "string",
				"description": "Cookie domain.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Optional cookie path. Defaults to /.",
			},
		},
		"required":             []string{"session_id", "name", "value", "domain"},
		"additionalProperties": false,
	}
}

func browserReloadInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"wait_until": map[string]any{
				"type":        "string",
				"description": "When to consider navigation complete: load or domcontentloaded. Defaults to load.",
				"enum":        []string{"load", "domcontentloaded"},
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserBackInputSchema() map[string]any {
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

func browserForwardInputSchema() map[string]any {
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

func browserHoverInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to hover over.",
			},
		},
		"required":             []string{"session_id", "selector"},
		"additionalProperties": false,
	}
}

func browserSelectInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the <select> element.",
			},
			"values": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Option value(s) to select.",
			},
		},
		"required":             []string{"session_id", "selector", "values"},
		"additionalProperties": false,
	}
}

func browserFillInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the form field to fill.",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Value to fill into the field.",
			},
		},
		"required":             []string{"session_id", "selector", "value"},
		"additionalProperties": false,
	}
}

func browserStorageGetInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"key": map[string]any{
				"type":        "string",
				"description": "Storage key to retrieve.",
			},
			"storage_type": map[string]any{
				"type":        "string",
				"description": "Storage type: local or session. Defaults to local.",
				"enum":        []string{"local", "session"},
			},
		},
		"required":             []string{"session_id", "key"},
		"additionalProperties": false,
	}
}

func browserStorageSetInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"key": map[string]any{
				"type":        "string",
				"description": "Storage key to set.",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Value to store.",
			},
			"storage_type": map[string]any{
				"type":        "string",
				"description": "Storage type: local or session. Defaults to local.",
				"enum":        []string{"local", "session"},
			},
		},
		"required":             []string{"session_id", "key", "value"},
		"additionalProperties": false,
	}
}

func browserDialogHandleInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"action": map[string]any{
				"type":        "string",
				"description": "How to handle the dialog: accept or dismiss.",
				"enum":        []string{"accept", "dismiss"},
			},
			"prompt_text": map[string]any{
				"type":        "string",
				"description": "Text to enter in a prompt dialog before accepting.",
			},
		},
		"required":             []string{"session_id", "action"},
		"additionalProperties": false,
	}
}

func browserPDFInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"format": map[string]any{
				"type":        "string",
				"description": "Paper format: A4 or letter. Defaults to A4.",
				"enum":        []string{"A4", "letter"},
			},
			"landscape": map[string]any{
				"type":        "boolean",
				"description": "Whether to use landscape orientation. Defaults to false.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func browserNetworkEnableInputSchema() map[string]any {
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

func browserNetworkRequestsInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Browser session ID.",
			},
			"url_pattern": map[string]any{
				"type":        "string",
				"description": "Optional substring to filter captured requests by URL.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

// Additional browser input schemas are grouped in
// builtin_browser_schema_input_advanced.go.
