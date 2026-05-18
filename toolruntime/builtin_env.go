package toolruntime

import (
	"github.com/fulcrus/hopclaw/skill"
)

func envToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "env.probe",
				Description:     "Detect environment: OS, arch, shell, available binaries.",
				InputSchema:     envProbeInputSchema(),
				OutputSchema:    envProbeOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "env:probe",
			},
			Handler: handleEnvProbe,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "env.info",
				Description:     "Basic system information: OS, arch, hostname, CPU count, Go version.",
				InputSchema:     envInfoInputSchema(),
				OutputSchema:    envInfoOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "env:info",
			},
			Handler: handleEnvInfo,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "env.get",
				Description:     "Inspect one or more environment variables without revealing their values.",
				InputSchema:     envGetInputSchema(),
				OutputSchema:    envGetOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "env:get",
			},
			Handler: handleEnvGet,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "env.set",
				Description:      "Set an environment overlay for the current run/session without mutating the host process.",
				InputSchema:      envSetInputSchema(),
				OutputSchema:     envSetOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "env:set:{name}",
			},
			Handler: handleEnvSet,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "env.refresh",
				Description:     "Re-detect environment and update tool availability using the current run/session overlay.",
				InputSchema:     envRefreshInputSchema(),
				OutputSchema:    envRefreshOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "env:refresh",
			},
			Handler: handleEnvRefresh,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "skill.list",
				Description:     "List installed and available skills.",
				InputSchema:     skillListInputSchema(),
				OutputSchema:    skillListOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "skill:list",
			},
			Handler: handleSkillList,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "skill.inspect",
				Description:     "Inspect one skill deeply: dependencies, readiness, tools, install hints, and next actions.",
				InputSchema:     skillInspectInputSchema(),
				OutputSchema:    skillInspectOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "skill:inspect:{name}",
			},
			Handler: handleSkillInspect,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "skill.ensure",
				Description:      "Search for and install a missing skill needed to complete the current task.",
				InputSchema:      skillEnsureInputSchema(),
				OutputSchema:     skillEnsureOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "skill:ensure:{goal}",
			},
			Handler: handleSkillEnsure,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "skill.install",
				Description:      "Install a skill by name.",
				InputSchema:      skillInstallInputSchema(),
				OutputSchema:     skillInstallOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "skill:install:{name}",
			},
			Handler: handleSkillInstall,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "skill.remove",
				Description:      "Remove an installed skill by name.",
				InputSchema:      skillRemoveInputSchema(),
				OutputSchema:     skillRemoveOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "skill:remove:{name}",
			},
			Handler: handleSkillRemove,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "skill.publish",
				Description:      "Publish a skill directory to the remote ClawHub registry.",
				InputSchema:      skillPublishInputSchema(),
				OutputSchema:     skillPublishOutputSchema(),
				SideEffectClass:  "external_write",
				RequiresApproval: true,
				ExecutionKey:     "skill:publish:{slug}",
			},
			Handler: handleSkillPublish,
		},
		// Session tools (read-only).
		{
			Manifest: skill.ToolManifest{
				Name:            "session.list",
				Description:     "List active agent sessions.",
				InputSchema:     sessionListInputSchema(),
				OutputSchema:    sessionListOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "session:list",
			},
			Handler: handleSessionList,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "session.history",
				Description:     "Retrieve conversation history for a session.",
				InputSchema:     sessionHistoryInputSchema(),
				OutputSchema:    sessionHistoryOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "session:history:{session_id}",
			},
			Handler: handleSessionHistory,
		},
		// Memory tools.
		{
			Manifest: skill.ToolManifest{
				Name:            "memory.get",
				Description:     "Retrieve a value from the persistent memory store.",
				InputSchema:     memoryGetInputSchema(),
				OutputSchema:    memoryGetOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "memory:get:{key}",
			},
			Handler: handleMemoryGet,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "memory.set",
				Description:     "Store a value in the persistent memory store.",
				InputSchema:     memorySetInputSchema(),
				OutputSchema:    memorySetOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "memory:set:{key}",
			},
			Handler: handleMemorySet,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "memory.delete",
				Description:     "Delete a value from the persistent memory store.",
				InputSchema:     memoryDeleteInputSchema(),
				OutputSchema:    memoryDeleteOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "memory:delete:{key}",
			},
			Handler: handleMemoryDelete,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "memory.list",
				Description:     "List stored memory entries, optionally filtered by key prefix.",
				InputSchema:     memoryListInputSchema(),
				OutputSchema:    memoryListOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "memory:list",
			},
			Handler: handleMemoryList,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "memory.search",
				Description:     "Search the persistent memory store. keyword does substring matching; semantic uses vector similarity; hybrid combines keyword and semantic retrieval when embeddings are available; mmr uses maximal marginal relevance for diversity-aware semantic retrieval.",
				InputSchema:     memorySearchInputSchema(),
				OutputSchema:    memorySearchOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "memory:search",
			},
			Handler: handleMemorySearch,
		},
	}
}

// ---------------------------------------------------------------------------
// env.probe
// ---------------------------------------------------------------------------

var commonBins = []string{
	"git", "python3", "python", "node", "npm", "docker",
	"curl", "wget", "ffmpeg", "gcc", "make", "java",
	"ruby", "go", "cargo", "pip", "pip3",
	"brew", "apt-get", "apk", "yum", "dnf",
}
