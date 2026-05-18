package toolruntime

func envProbeInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"bins": stringArraySchema("Additional binary names to check beyond the common set."),
	})
}

func envInfoInputSchema() map[string]any {
	return objectSchema(map[string]any{})
}

func envGetInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"name":  stringSchema("Environment variable name to read."),
		"names": stringArraySchema("Multiple environment variable names to read in bulk."),
	})
}

func envSetInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"name":  stringSchema("Environment variable name."),
		"value": stringSchema("Value to assign."),
	}, "name", "value")
}

func envRefreshInputSchema() map[string]any {
	return objectSchema(map[string]any{})
}

func skillListInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"query": stringSchema("Optional search query to filter catalog results."),
	})
}

func skillInspectInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"name":        stringSchema("Installed or loaded skill name, config key, or source hint."),
		"source":      stringSchema("Optional local skill directory to inspect directly."),
		"source_kind": stringSchema("Optional source kind for local inspection (workspace, user, bundled, clawhub, plugin)."),
	})
}

func skillInstallInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"name":    stringSchema("Skill name/ID to install."),
		"source":  stringSchema("Optional local skill directory to install instead of searching the hub."),
		"version": stringSchema("Specific version to install (default: latest)."),
	}, "name")
}

func skillEnsureInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"goal":           stringSchema("Short description of the missing capability or task to unlock."),
		"query":          stringSchema("Optional explicit catalog search query."),
		"required_tools": stringArraySchema("Tool names or capability names that are missing."),
		"limit":          integerSchema("Maximum number of candidate skills to inspect."),
	})
}

func skillRemoveInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"name": stringSchema("Skill name to remove."),
	}, "name")
}

func skillPublishInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill_dir": map[string]any{
				"type":        "string",
				"description": "Path to skill directory containing SKILL.md.",
			},
			"slug": map[string]any{
				"type":        "string",
				"description": "Registry slug for the skill (e.g. 'my-skill').",
			},
			"version": map[string]any{
				"type":        "string",
				"description": "Semantic version to publish (e.g. '1.0.0').",
			},
			"changelog": map[string]any{
				"type":        "string",
				"description": "Optional changelog for this version.",
			},
		},
		"required":             []string{"skill_dir", "slug", "version"},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func sessionListInputSchema() map[string]any {
	return objectSchema(map[string]any{})
}

func sessionHistoryInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"session_id": stringSchema("Session identifier."),
		"limit":      integerSchema("Maximum number of messages to return (default: 50)."),
	}, "session_id")
}

func memoryGetInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"key": stringSchema("Memory key."),
	}, "key")
}

func memorySetInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"key":   stringSchema("Memory key."),
		"value": stringSchema("Value to store."),
	}, "key", "value")
}

func memoryDeleteInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"key": stringSchema("Memory key."),
	}, "key")
}

func memoryListInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"prefix": stringSchema("Optional key prefix filter."),
		"limit":  integerSchema("Maximum number of entries to return (default 10)."),
	})
}

func memorySearchInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"query": stringSchema("Search query (matches key or value content)."),
		"mode": map[string]any{
			"type":        "string",
			"description": "Search mode: keyword (default, case-insensitive substring match over key/value), semantic (vector similarity), hybrid (keyword + semantic when embeddings are available, otherwise keyword), or mmr (semantic search reranked for diversity).",
			"enum":        []string{memorySearchModeKeyword, memorySearchModeSemantic, memorySearchModeHybrid, memorySearchModeMMR},
		},
		"limit":  integerSchema("Optional maximum number of results. semantic/mmr default to 10 when omitted."),
		"lambda": numberSchema("MMR lambda parameter (0.0-1.0, default 0.5). Higher values favor relevance, lower values favor diversity. Only used with mmr mode."),
	}, "query")
}
