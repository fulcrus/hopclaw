package toolruntime

func envProbeOutputSchema() map[string]any {
	dormantGroupEntry := objectSchema(map[string]any{
		"group":        stringSchema("Layer 2 group name."),
		"tool_count":   integerSchema("Number of tools in the group."),
		"missing":      stringArraySchema("Binaries not found."),
		"install_hint": stringSchema("How to install missing dependencies."),
	}, "group", "tool_count", "missing", "install_hint")
	return objectSchema(map[string]any{
		"os":                  stringSchema("Operating system."),
		"arch":                stringSchema("CPU architecture."),
		"hostname":            stringSchema("Machine hostname."),
		"shell":               stringSchema("Default shell path."),
		"in_container":        booleanSchema("Whether running inside a container."),
		"package_managers":    stringArraySchema("Detected package managers."),
		"available_bins":      objectSchema(map[string]any{}),
		"checked_bins":        objectSchema(map[string]any{}),
		"dormant_groups":      arraySchema(dormantGroupEntry, "Layer 2 groups that are dormant (missing dependencies)."),
		"layer2_active_tools": integerSchema("Number of active Layer 2 tools."),
	}, "os", "arch", "hostname", "shell", "in_container", "package_managers", "available_bins", "checked_bins")
}

func envInfoOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"os":         stringSchema("Operating system."),
		"arch":       stringSchema("CPU architecture."),
		"hostname":   stringSchema("Machine hostname."),
		"cpus":       integerSchema("Number of logical CPUs."),
		"go_version": stringSchema("Go runtime version."),
	}, "os", "arch", "hostname", "cpus", "go_version")
}

func envGetOutputSchema() map[string]any {
	// Single mode fields + bulk mode field; caller sees one or the other.
	varEntry := objectSchema(map[string]any{
		"name":     stringSchema("Variable name."),
		"exists":   booleanSchema("Whether the variable is available."),
		"source":   stringSchema("Where the variable comes from."),
		"managed":  booleanSchema("Whether the variable is injected by managed config."),
		"redacted": booleanSchema("Whether the value is intentionally hidden."),
	}, "name", "exists", "redacted")
	return objectSchema(map[string]any{
		"name":     stringSchema("Variable name (single mode)."),
		"exists":   booleanSchema("Whether the variable is available (single mode)."),
		"source":   stringSchema("Where the variable comes from (single mode)."),
		"managed":  booleanSchema("Whether the variable is injected by managed config (single mode)."),
		"redacted": booleanSchema("Whether the value is intentionally hidden (single mode)."),
		"vars":     arraySchema(varEntry, "Variables (bulk mode)."),
	})
}

func envSetOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"name":    stringSchema("Variable name that was set."),
		"scope":   stringSchema("Where the overlay applies."),
		"message": stringSchema("Human-readable confirmation."),
	}, "name", "scope", "message")
}

func envRefreshOutputSchema() map[string]any {
	groupEntry := objectSchema(map[string]any{
		"group":  stringSchema("Group name."),
		"active": booleanSchema("Whether the group is active."),
		"tools":  integerSchema("Number of tools in the group."),
	}, "group", "active", "tools")
	return objectSchema(map[string]any{
		"newly_active":  stringArraySchema("Tool groups that became active after re-probe."),
		"newly_dormant": stringArraySchema("Tool groups that became dormant after re-probe."),
		"groups":        arraySchema(groupEntry, "All Layer 2 groups with current status."),
		"summary": objectSchema(map[string]any{
			"os":              stringSchema("Operating system."),
			"arch":            stringSchema("CPU architecture."),
			"hostname":        stringSchema("Machine hostname."),
			"common_bins":     integerSchema("Number of common binaries checked."),
			"available_count": integerSchema("Number of available binaries found."),
		}, "os", "arch", "hostname", "common_bins", "available_count"),
	}, "newly_active", "newly_dormant", "groups", "summary")
}

func skillListOutputSchema() map[string]any {
	installedEntry := objectSchema(map[string]any{
		"id":           stringSchema("Skill ID."),
		"version":      stringSchema("Installed version."),
		"install_dir":  stringSchema("Installation directory."),
		"pinned":       booleanSchema("Whether version is pinned."),
		"installed_at": stringSchema("Installation timestamp."),
	}, "id", "version")
	loadedEntry := objectSchema(map[string]any{
		"name":        stringSchema("Skill name."),
		"description": stringSchema("Skill description."),
		"kind":        stringSchema("Skill kind (prompt/executable)."),
		"status":      stringSchema("Package status."),
		"source":      stringSchema("Source kind."),
		"tools":       integerSchema("Number of tool manifests."),
	}, "name")
	catalogEntry := objectSchema(map[string]any{
		"id":      stringSchema("Skill ID."),
		"name":    stringSchema("Skill name."),
		"version": stringSchema("Available version."),
		"summary": stringSchema("Short description."),
	}, "id", "name")
	return objectSchema(map[string]any{
		"installed": arraySchema(installedEntry, "Skills installed via ClawHub."),
		"loaded":    arraySchema(loadedEntry, "Skills loaded into the registry."),
		"catalog":   arraySchema(catalogEntry, "Skills available in the ClawHub catalog."),
	}, "installed", "loaded", "catalog")
}

func skillInspectOutputSchema() map[string]any {
	checkEntry := objectSchema(map[string]any{
		"kind":       stringSchema("Dependency check kind."),
		"name":       stringSchema("Dependency name."),
		"candidates": stringArraySchema("Alternative candidates for any-binary checks."),
		"status":     stringSchema("Check status."),
		"present":    booleanSchema("Whether the dependency is satisfied."),
		"source":     stringSchema("How the dependency is satisfied."),
		"path":       stringSchema("Resolved binary path when available."),
		"message":    stringSchema("Human-readable status message."),
		"hint":       stringSchema("Suggested next action."),
	}, "kind", "status", "present")
	toolEntry := objectSchema(map[string]any{
		"name":              stringSchema("Tool name."),
		"aliases":           stringArraySchema("Tool aliases."),
		"description":       stringSchema("Tool description."),
		"side_effect_class": stringSchema("Tool side-effect class."),
		"idempotent":        booleanSchema("Whether the tool is idempotent."),
		"requires_approval": booleanSchema("Whether the tool requires approval."),
		"execution_key":     stringSchema("Execution deduplication key."),
		"runtime_entry":     stringSchema("Runtime entrypoint."),
		"runtime_shell":     stringSchema("Runtime shell."),
		"timeout":           stringSchema("Declared timeout."),
	}, "name", "idempotent", "requires_approval")
	issueEntry := objectSchema(map[string]any{
		"severity": stringSchema("Issue severity."),
		"code":     stringSchema("Issue code."),
		"message":  stringSchema("Issue message."),
	}, "severity", "message")
	installHintEntry := objectSchema(map[string]any{
		"id":               stringSchema("Installer step ID."),
		"kind":             stringSchema("Installer kind."),
		"label":            stringSchema("Installer label."),
		"os":               stringArraySchema("Supported operating systems."),
		"bins":             stringArraySchema("Binaries this installer can satisfy."),
		"formula":          stringSchema("Homebrew formula."),
		"package":          stringSchema("Package manager package."),
		"module":           stringSchema("Go module."),
		"url":              stringSchema("Download URL."),
		"archive":          stringSchema("Archive kind."),
		"target_dir":       stringSchema("Target directory."),
		"strip_components": integerSchema("Archive strip-components value."),
	})
	return objectSchema(map[string]any{
		"found":             booleanSchema("Whether the skill was found."),
		"loaded":            booleanSchema("Whether the skill is present in the runtime registry."),
		"blocked":           booleanSchema("Whether the skill is blocked during load/compile."),
		"installed":         booleanSchema("Whether the skill is installed via ClawHub."),
		"name":              stringSchema("Skill display name."),
		"skill_id":          stringSchema("Skill identifier."),
		"config_key":        stringSchema("Skill config key."),
		"description":       stringSchema("Skill description."),
		"homepage":          stringSchema("Skill homepage."),
		"location":          stringSchema("Skill prompt catalog location."),
		"kind":              stringSchema("Skill kind."),
		"status":            stringSchema("Package status."),
		"trust":             stringSchema("Trust class."),
		"source_kind":       stringSchema("Source kind."),
		"source_root":       stringSchema("Source root."),
		"source_dir":        stringSchema("Source directory."),
		"source_name_hint":  stringSchema("Source name hint."),
		"source_priority":   integerSchema("Source priority."),
		"installed_version": stringSchema("Installed version."),
		"install_dir":       stringSchema("Install directory."),
		"bundle_dir":        stringSchema("Bundle directory."),
		"pinned":            booleanSchema("Whether the skill is pinned."),
		"eligible":          booleanSchema("Whether the current runtime can execute the skill."),
		"ready":             booleanSchema("Whether the skill is ready for use."),
		"always":            booleanSchema("Whether the skill is always included."),
		"reasons":           stringArraySchema("Human-readable eligibility reasons."),
		"checks":            arraySchema(checkEntry, "Structured dependency checks."),
		"injected_env":      stringArraySchema("Environment keys injected by managed config."),
		"tools":             arraySchema(toolEntry, "Tools exported by the skill."),
		"install_hints":     arraySchema(installHintEntry, "Install hints declared by the skill."),
		"issues":            arraySchema(issueEntry, "Compile or validation issues."),
		"next_actions":      stringArraySchema("Suggested next steps."),
		"message":           stringSchema("Status message."),
	}, "found", "loaded", "eligible", "ready")
}

func skillInstallOutputSchema() map[string]any {
	stepEntry := objectSchema(map[string]any{
		"id":      stringSchema("Installer step ID."),
		"kind":    stringSchema("Installer kind."),
		"label":   stringSchema("Human-readable installer label."),
		"status":  stringSchema("Installer status (ran or skipped)."),
		"reason":  stringSchema("Why the step was skipped, if applicable."),
		"command": stringArraySchema("Command that was executed, if applicable."),
		"path":    stringSchema("Target path produced by the installer, if applicable."),
	}, "id", "kind", "status")
	return objectSchema(map[string]any{
		"name":            stringSchema("Skill ID."),
		"version":         stringSchema("Installed version."),
		"install_dir":     stringSchema("Installation directory path."),
		"installer_steps": arraySchema(stepEntry, "Installer steps executed as part of skill setup."),
		"validation":      objectSchema(map[string]any{}),
		"success":         booleanSchema("Whether installation succeeded."),
		"message":         stringSchema("Status message."),
	}, "name", "success", "message")
}

func skillEnsureOutputSchema() map[string]any {
	candidateEntry := objectSchema(map[string]any{
		"id":        stringSchema("Skill ID."),
		"name":      stringSchema("Skill name."),
		"version":   stringSchema("Skill version."),
		"summary":   stringSchema("Short description."),
		"installed": booleanSchema("Whether the skill is already installed."),
		"loaded":    booleanSchema("Whether the skill is already loaded in the registry."),
		"tools":     stringArraySchema("Known tool names exposed by the skill, if loaded."),
		"score":     integerSchema("Internal ranking score."),
	}, "id", "name", "installed", "loaded", "score")
	stepEntry := objectSchema(map[string]any{
		"id":      stringSchema("Installer step ID."),
		"kind":    stringSchema("Installer kind."),
		"label":   stringSchema("Human-readable installer label."),
		"status":  stringSchema("Installer status (ran or skipped)."),
		"reason":  stringSchema("Why the step was skipped, if applicable."),
		"command": stringArraySchema("Command that was executed, if applicable."),
		"path":    stringSchema("Target path produced by the installer, if applicable."),
	}, "id", "kind", "status")
	return objectSchema(map[string]any{
		"success":         booleanSchema("Whether the ensure operation completed without an internal error."),
		"resolved":        booleanSchema("Whether the missing capability is now available."),
		"installed":       booleanSchema("Whether a skill was installed during this call."),
		"query":           stringSchema("Effective search query used for catalog lookup."),
		"required_tools":  stringArraySchema("Tool names requested by the caller."),
		"selected":        candidateEntry,
		"candidates":      arraySchema(candidateEntry, "Candidate skills considered during resolution."),
		"name":            stringSchema("Installed skill ID."),
		"version":         stringSchema("Installed skill version."),
		"install_dir":     stringSchema("Installed skill directory."),
		"installer_steps": arraySchema(stepEntry, "Installer steps executed as part of skill setup."),
		"validation":      objectSchema(map[string]any{}),
		"fallback_hint":   stringSchema("Actionable guidance when no skill is found."),
		"message":         stringSchema("Status message."),
	}, "success", "resolved", "installed", "message")
}

func skillRemoveOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"name":    stringSchema("Skill ID."),
		"success": booleanSchema("Whether removal succeeded."),
		"message": stringSchema("Status message."),
	}, "name", "success", "message")
}

func skillPublishOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":      booleanSchema("Whether the publish succeeded."),
		"slug":    stringSchema("Published skill slug."),
		"version": stringSchema("Published version."),
		"url":     stringSchema("URL to view the published skill."),
	}, "ok")
}

// ---------------------------------------------------------------------------
// session.list
// ---------------------------------------------------------------------------

func sessionListOutputSchema() map[string]any {
	sessionEntry := objectSchema(map[string]any{
		"id":         stringSchema("Session ID."),
		"key":        stringSchema("Session key."),
		"model":      stringSchema("Model used."),
		"messages":   integerSchema("Number of messages."),
		"created_at": stringSchema("Creation timestamp."),
		"updated_at": stringSchema("Last update timestamp."),
	}, "id", "key")
	return objectSchema(map[string]any{
		"sessions": arraySchema(sessionEntry, "Active sessions."),
		"count":    integerSchema("Total session count."),
	}, "sessions", "count")
}

func sessionHistoryOutputSchema() map[string]any {
	msgEntry := objectSchema(map[string]any{
		"role":       stringSchema("Message role (user/assistant/system/tool)."),
		"content":    stringSchema("Message content."),
		"created_at": stringSchema("Message timestamp."),
	}, "role", "content")
	return objectSchema(map[string]any{
		"session_id":     stringSchema("Session ID."),
		"messages":       arraySchema(msgEntry, "Conversation messages."),
		"total_messages": integerSchema("Total message count in session."),
	}, "session_id", "messages")
}

func memoryGetOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"key":        stringSchema("Memory key."),
		"value":      stringSchema("Stored value."),
		"found":      booleanSchema("Whether the key exists."),
		"created_at": stringSchema("Creation timestamp."),
		"updated_at": stringSchema("Last update timestamp."),
	}, "key", "found")
}

func memorySetOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"key":     stringSchema("Memory key."),
		"success": booleanSchema("Whether the operation succeeded."),
		"blocked": booleanSchema("Whether the write was blocked by memory governance."),
		"reason":  stringSchema("Why the write was blocked."),
		"hint":    stringSchema("Guidance for the agent when a write is blocked."),
		"message": stringSchema("Status message."),
	}, "key", "success")
}

func memoryDeleteOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"key":     stringSchema("Memory key."),
		"success": booleanSchema("Whether the operation succeeded."),
		"message": stringSchema("Status message."),
	}, "key", "success")
}

func memoryListOutputSchema() map[string]any {
	entrySchema := objectSchema(map[string]any{
		"key":        stringSchema("Memory key."),
		"value":      stringSchema("Stored value."),
		"created_at": stringSchema("Creation timestamp."),
		"updated_at": stringSchema("Last update timestamp."),
	}, "key", "value")
	return objectSchema(map[string]any{
		"results": arraySchema(entrySchema, "Listed memory entries."),
		"count":   integerSchema("Number of entries returned."),
		"prefix":  stringSchema("Applied key prefix filter."),
	}, "results", "count")
}

func memorySearchOutputSchema() map[string]any {
	entrySchema := objectSchema(map[string]any{
		"key":        stringSchema("Memory key."),
		"value":      stringSchema("Stored value."),
		"score":      map[string]any{"type": "number", "description": "Similarity score when the selected retrieval mode returns one (for example semantic, hybrid, or mmr)."},
		"created_at": stringSchema("Creation timestamp."),
		"updated_at": stringSchema("Last update timestamp."),
	}, "key", "value")
	return objectSchema(map[string]any{
		"results": arraySchema(entrySchema, "Matching memory entries."),
		"count":   integerSchema("Number of results."),
		"mode":    stringSchema("Search mode used."),
	}, "results", "count", "mode")
}
