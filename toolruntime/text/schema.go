package text

// ---------------------------------------------------------------------------
// Schema helpers — duplicated locally to avoid importing toolruntime.
// ---------------------------------------------------------------------------

func stringSchema(description string) map[string]any {
	schema := map[string]any{"type": "string"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func integerSchema(description string) map[string]any {
	schema := map[string]any{"type": "integer"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringArraySchema(description string) map[string]any {
	schema := map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string"},
	}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func arraySchema(items map[string]any, description string) map[string]any {
	schema := map[string]any{
		"type":  "array",
		"items": items,
	}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

// ===========================================================================
// Input schemas
// ===========================================================================

func textJSONInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{
				"type":        "string",
				"description": "Path to a JSON file relative to the workspace root.",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Inline JSON string to parse.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Dot-path query, e.g. \".foo.bar\", \".items[0].name\".",
			},
		},
		"additionalProperties": false,
	}
}

func textYAMLInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{
				"type":        "string",
				"description": "Path to a YAML file relative to the workspace root.",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Inline YAML string to parse.",
			},
			"output_format": map[string]any{
				"type":        "string",
				"description": "Output format (default \"json\").",
			},
		},
		"additionalProperties": false,
	}
}

func textCSVInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{
				"type":        "string",
				"description": "Path to a CSV file relative to the workspace root.",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Inline CSV string to parse.",
			},
			"delimiter": map[string]any{
				"type":        "string",
				"description": "Field delimiter character (default \",\").",
			},
			"header": map[string]any{
				"type":        "boolean",
				"description": "Whether the first row is a header (default true).",
			},
			"columns": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Column names or indices to select.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of data rows to return.",
			},
		},
		"additionalProperties": false,
	}
}

func textXMLInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{
				"type":        "string",
				"description": "Path to an XML file relative to the workspace root.",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Inline XML string to parse.",
			},
			"tag": map[string]any{
				"type":        "string",
				"description": "Extract content of a specific XML tag.",
			},
		},
		"additionalProperties": false,
	}
}

func textTOMLInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{
				"type":        "string",
				"description": "Path to a TOML file relative to the workspace root.",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Inline TOML string to parse.",
			},
			"output_format": map[string]any{
				"type":        "string",
				"description": "Output format (default \"json\").",
			},
		},
		"additionalProperties": false,
	}
}

func textINIInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{
				"type":        "string",
				"description": "Path to an INI/properties file relative to the workspace root.",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Inline INI/properties string to parse.",
			},
			"output_format": map[string]any{
				"type":        "string",
				"description": "Output format (default \"json\").",
			},
		},
		"additionalProperties": false,
	}
}

func textDotenvInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{
				"type":        "string",
				"description": "Path to a .env file relative to the workspace root.",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Inline .env content to parse.",
			},
		},
		"additionalProperties": false,
	}
}

func textJSONLInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{
				"type":        "string",
				"description": "Path to a JSONL file relative to the workspace root.",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Inline JSONL string to parse.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Dot-path filter applied to each record.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of records to return (default 100).",
			},
		},
		"additionalProperties": false,
	}
}

func textHTMLInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{
				"type":        "string",
				"description": "Path to an HTML file relative to the workspace root.",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Inline HTML string to process.",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "Processing mode: \"text\" (default), \"links\", or \"tags\".",
			},
			"tag": map[string]any{
				"type":        "string",
				"description": "Extract content of a specific HTML tag (used with mode \"tags\" or as a filter for mode \"text\").",
			},
		},
		"additionalProperties": false,
	}
}

func textMarkdownInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{
				"type":        "string",
				"description": "Path to a Markdown file relative to the workspace root.",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Inline Markdown string to process.",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "Processing mode: \"frontmatter\" (default), \"toc\", or \"section\".",
			},
			"section": map[string]any{
				"type":        "string",
				"description": "Heading text to extract (required for mode \"section\").",
			},
		},
		"additionalProperties": false,
	}
}

func textRegexInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Go regular expression pattern.",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Inline text to process.",
			},
			"file": map[string]any{
				"type":        "string",
				"description": "Path to a text file relative to the workspace root.",
			},
			"replace": map[string]any{
				"type":        "string",
				"description": "Replacement string (used with mode \"replace\").",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "Operation mode: \"match\" (default), \"replace\", \"extract\", or \"split\".",
			},
		},
		"required":             []string{"pattern"},
		"additionalProperties": false,
	}
}

func textBase64InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "String to encode or decode.",
			},
			"decode": map[string]any{
				"type":        "boolean",
				"description": "Set to true to decode; default is false (encode).",
			},
		},
		"required":             []string{"input"},
		"additionalProperties": false,
	}
}

func textHexInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "String to encode or hex string to decode.",
			},
			"decode": map[string]any{
				"type":        "boolean",
				"description": "Set to true to decode; default is false (encode).",
			},
		},
		"required":             []string{"input"},
		"additionalProperties": false,
	}
}

func textURLInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "URL string to parse, encode, or decode.",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "Operation mode: \"parse\" (default), \"encode\", or \"decode\".",
			},
		},
		"required":             []string{"input"},
		"additionalProperties": false,
	}
}

func textUUIDInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"count": map[string]any{
				"type":        "integer",
				"description": "Number of UUIDs to generate (default 1, max 1000).",
			},
		},
		"additionalProperties": false,
	}
}

func textTemplateInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"template": map[string]any{
				"type":        "string",
				"description": "Go text/template string to render.",
			},
			"data": map[string]any{
				"type":        "object",
				"description": "Data object passed to the template.",
			},
			"file": map[string]any{
				"type":        "string",
				"description": "Path to a template file relative to the workspace root.",
			},
		},
		"additionalProperties": false,
	}
}

func textCountInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "Inline text to count.",
			},
			"file": map[string]any{
				"type":        "string",
				"description": "Path to a text file relative to the workspace root.",
			},
		},
		"additionalProperties": false,
	}
}

// ===========================================================================
// Output schemas
// ===========================================================================

func textJSONOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"result": map[string]any{
			"description": "Queried JSON value.",
		},
	}, "result")
}

func textYAMLOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"result": map[string]any{
			"description": "Parsed YAML content as JSON.",
		},
	}, "result")
}

func textCSVOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"headers":   stringArraySchema("Column headers (if header row present)."),
		"rows":      arraySchema(map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "Data rows."),
		"row_count": integerSchema("Number of data rows returned."),
	}, "headers", "rows", "row_count")
}

func textXMLOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"result": map[string]any{
			"description": "JSON representation of the XML document.",
		},
	}, "result")
}

func textTOMLOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"result": map[string]any{
			"description": "Parsed TOML content as JSON.",
		},
	}, "result")
}

func textINIOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"result": map[string]any{
			"description": "Parsed INI content as a JSON map of section to key-value pairs.",
		},
	}, "result")
}

func textDotenvOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"vars":  map[string]any{"type": "object", "description": "Parsed key-value pairs."},
		"count": integerSchema("Number of variables parsed."),
	}, "vars", "count")
}

func textJSONLOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"records": arraySchema(map[string]any{}, "Parsed JSONL records."),
		"count":   integerSchema("Number of records returned."),
	}, "records", "count")
}

func textHTMLOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"content": stringSchema("Extracted text content."),
		"links":   stringArraySchema("Extracted links (mode \"links\")."),
		"count":   integerSchema("Count of extracted items."),
	})
}

func textMarkdownOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"frontmatter": stringSchema("Extracted YAML frontmatter."),
		"toc":         stringArraySchema("Table of contents headings."),
		"content":     stringSchema("Extracted section content."),
	})
}

func textRegexOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"matches": stringArraySchema("Matched strings (mode \"match\")."),
		"groups":  arraySchema(stringArraySchema(""), "Submatch groups (mode \"extract\")."),
		"result":  stringSchema("Result string (mode \"replace\")."),
		"parts":   stringArraySchema("Split parts (mode \"split\")."),
		"count":   integerSchema("Number of matches or parts."),
	})
}

func textBase64OutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"result": stringSchema("Encoded or decoded result."),
	}, "result")
}

func textHexOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"result": stringSchema("Encoded or decoded result."),
	}, "result")
}

func textURLOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"scheme":   stringSchema("URL scheme."),
		"host":     stringSchema("URL host."),
		"path":     stringSchema("URL path."),
		"query":    map[string]any{"type": "object", "description": "Parsed query parameters."},
		"fragment": stringSchema("URL fragment."),
		"result":   stringSchema("Encoded or decoded result."),
	})
}

func textUUIDOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"uuid":  stringSchema("Generated UUID (when count=1)."),
		"uuids": stringArraySchema("Generated UUIDs (when count>1)."),
	})
}

func textTemplateOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"result": stringSchema("Rendered template output."),
	}, "result")
}

func textCountOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"lines":      integerSchema("Number of lines."),
		"words":      integerSchema("Number of words."),
		"characters": integerSchema("Number of Unicode characters."),
		"bytes":      integerSchema("Number of bytes."),
	}, "lines", "words", "characters", "bytes")
}
