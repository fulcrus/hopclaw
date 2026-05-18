package toolruntime

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

func booleanSchema(description string) map[string]any {
	schema := map[string]any{"type": "boolean"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func numberSchema(description string) map[string]any {
	schema := map[string]any{"type": "number"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func stringArraySchema(description string) map[string]any {
	schema := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "string",
		},
	}
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

func builtinTextResultSchema(description string) map[string]any {
	return objectSchema(map[string]any{
		"message": stringSchema(description),
	}, "message")
}

func builtinFSListOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"path":   stringSchema("Path relative to the workspace root."),
		"name":   stringSchema("Base name."),
		"type":   stringSchema("Entry type."),
		"size":   integerSchema("Size in bytes."),
		"is_dir": booleanSchema("Whether the entry is a directory."),
	}, "path", "name", "type", "is_dir")
	return objectSchema(map[string]any{
		"path":      stringSchema("Root path that was listed."),
		"recursive": booleanSchema("Whether recursive listing was enabled."),
		"count":     integerSchema("Number of entries returned."),
		"entries":   arraySchema(entry, "Listed entries."),
	}, "path", "recursive", "count", "entries")
}

func builtinFSTreeOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"path":  stringSchema("Path relative to the workspace root."),
		"name":  stringSchema("Base name."),
		"type":  stringSchema("Entry type."),
		"depth": integerSchema("Depth relative to the requested root."),
	}, "path", "name", "type", "depth")
	return objectSchema(map[string]any{
		"path":      stringSchema("Root path that was rendered."),
		"max_depth": integerSchema("Maximum depth that was traversed."),
		"count":     integerSchema("Number of entries returned."),
		"entries":   arraySchema(entry, "Tree entries."),
	}, "path", "max_depth", "count", "entries")
}

func builtinFSFindOutputSchema() map[string]any {
	match := objectSchema(map[string]any{
		"path": stringSchema("Path relative to the workspace root."),
		"name": stringSchema("Base name."),
		"type": stringSchema("Entry type."),
	}, "path", "name", "type")
	return objectSchema(map[string]any{
		"path":      stringSchema("Root path that was searched."),
		"pattern":   stringSchema("Pattern used for matching."),
		"glob":      booleanSchema("Whether glob matching was enabled."),
		"recursive": booleanSchema("Whether recursive search was enabled."),
		"count":     integerSchema("Number of matches returned."),
		"matches":   arraySchema(match, "Matching entries."),
	}, "path", "pattern", "glob", "recursive", "count", "matches")
}

func builtinFSGrepOutputSchema() map[string]any {
	match := objectSchema(map[string]any{
		"path":    stringSchema("Path relative to the workspace root."),
		"line":    integerSchema("1-based line number."),
		"content": stringSchema("Matched line content."),
	}, "path", "line", "content")
	return objectSchema(map[string]any{
		"path":        stringSchema("Root path that was searched."),
		"pattern":     stringSchema("Pattern used for matching."),
		"regexp":      booleanSchema("Whether regexp matching was enabled."),
		"ignore_case": booleanSchema("Whether case-insensitive matching was enabled."),
		"recursive":   booleanSchema("Whether recursive search was enabled."),
		"count":       integerSchema("Number of matches returned."),
		"matches":     arraySchema(match, "Matched lines."),
	}, "path", "pattern", "regexp", "ignore_case", "recursive", "count", "matches")
}

func builtinFSReadOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":    stringSchema("Path relative to the workspace root."),
		"content": stringSchema("File content slice that was read."),
	}, "path", "content")
}

func builtinFSStatOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":      stringSchema("Path relative to the workspace root."),
		"name":      stringSchema("Base name."),
		"type":      stringSchema("Entry type."),
		"is_dir":    booleanSchema("Whether the path is a directory."),
		"size":      integerSchema("Size in bytes."),
		"mode":      stringSchema("File mode string."),
		"mod_time":  stringSchema("Modification time in RFC3339Nano."),
		"workspace": stringSchema("Workspace root."),
	}, "path", "name", "type", "is_dir", "size", "mode", "mod_time", "workspace")
}

func builtinFSHashOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":      stringSchema("Path relative to the workspace root."),
		"algorithm": stringSchema("Hash algorithm."),
		"hash":      stringSchema("Computed hash digest."),
		"size":      integerSchema("File size in bytes."),
	}, "path", "algorithm", "hash", "size")
}

func builtinFSWriteOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":          stringSchema("Path relative to the workspace root."),
		"workspace":     stringSchema("Workspace root."),
		"bytes_written": integerSchema("Number of bytes written."),
		"append":        booleanSchema("Whether append mode was used."),
		"message":       stringSchema("Human-readable write summary."),
	}, "path", "workspace", "bytes_written", "append", "message")
}

func builtinFSEditOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":         stringSchema("Path relative to the workspace root."),
		"workspace":    stringSchema("Workspace root."),
		"replacements": integerSchema("Number of replacements performed."),
		"message":      stringSchema("Human-readable edit summary."),
	}, "path", "workspace", "replacements", "message")
}

func builtinFSPatchOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"applied_files": stringArraySchema("Files touched by the patch."),
		"file_count":    integerSchema("Number of files touched."),
		"reverse":       booleanSchema("Whether the patch was reversed."),
		"message":       stringSchema("Human-readable patch summary."),
	}, "applied_files", "file_count", "reverse", "message")
}

func builtinFSDeleteOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":      stringSchema("Path that was deleted."),
		"workspace": stringSchema("Workspace root."),
		"message":   stringSchema("Human-readable delete summary."),
	}, "path", "workspace", "message")
}

func builtinFSMoveOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"source":      stringSchema("Original path."),
		"destination": stringSchema("New path."),
		"workspace":   stringSchema("Workspace root."),
		"message":     stringSchema("Human-readable move summary."),
	}, "source", "destination", "workspace", "message")
}

func builtinFSCopyOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"source":       stringSchema("Source path."),
		"destination":  stringSchema("Destination path."),
		"workspace":    stringSchema("Workspace root."),
		"bytes_copied": integerSchema("Number of bytes copied."),
		"message":      stringSchema("Human-readable copy summary."),
	}, "source", "destination", "workspace", "bytes_copied", "message")
}

func builtinFSMkdirOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":      stringSchema("Created directory path."),
		"workspace": stringSchema("Workspace root."),
		"message":   stringSchema("Human-readable mkdir summary."),
	}, "path", "workspace", "message")
}

func builtinFSAppendOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":           stringSchema("Path relative to the workspace root."),
		"workspace":      stringSchema("Workspace root."),
		"bytes_appended": integerSchema("Number of bytes appended."),
		"message":        stringSchema("Human-readable append summary."),
	}, "path", "workspace", "bytes_appended", "message")
}

func builtinExecRunOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"command": stringSchema("Executed command name."),
		"dir":     stringSchema("Working directory."),
		"stdout":  stringSchema("Captured stdout content."),
		"stderr":  stringSchema("Captured stderr content."),
		"content": stringSchema("Combined human-facing output."),
	}, "command", "dir", "stdout", "stderr", "content")
}
