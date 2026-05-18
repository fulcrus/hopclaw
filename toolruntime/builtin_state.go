package toolruntime

import (
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/skill"
)

type builtinConfigState struct {
	config BuiltinsConfig
}

type builtinWorkspaceState struct {
	ws
}

type builtinRegistryState struct {
	tools       map[string]skill.BoundTool
	definitions []agent.ToolDefinition
	handlers    map[string]builtinHandler
}

func normalizeBuiltinsConfig(cfg BuiltinsConfig) BuiltinsConfig {
	if cfg.DefaultExecTimeout <= 0 {
		cfg.DefaultExecTimeout = 30 * time.Second
	}
	if cfg.MaxReadBytes <= 0 {
		cfg.MaxReadBytes = 256 * 1024
	}
	if cfg.SkillEnsureLimit <= 0 {
		cfg.SkillEnsureLimit = 5
	}
	return cfg
}

func newBuiltinWorkspaceState(cfg BuiltinsConfig) builtinWorkspaceState {
	workspace := builtinWorkspaceState{ws: newWorkspace(cfg.Root)}
	allowedPaths := make([]string, 0, len(cfg.AllowedPaths)+len(cfg.FSConstraints.AllowedRoots))
	allowedPaths = append(allowedPaths, cfg.AllowedPaths...)
	allowedPaths = append(allowedPaths, cfg.FSConstraints.AllowedRoots...)
	if len(allowedPaths) > 0 {
		workspace.ws.setAllowedPaths(allowedPaths)
	}
	workspace.ws.setDenyPatterns(cfg.FSConstraints.DenyPatterns)
	return workspace
}

func newBuiltinRegistryState() builtinRegistryState {
	return builtinRegistryState{
		tools:    make(map[string]skill.BoundTool),
		handlers: make(map[string]builtinHandler),
	}
}

func newBuiltinRuntimePackage(root string) *skill.SkillPackage {
	return &skill.SkillPackage{
		ID:     "builtin-core-runtime",
		Kind:   skill.SkillKindExecutable,
		Status: skill.StatusReady,
		Prompt: skill.PromptSkill{
			Name:        "builtin-core",
			Description: "Core runtime tools implemented in Go",
			Location:    "builtin:core",
		},
		Source: skill.SkillSource{
			Kind: skill.SourceBundled,
			Dir:  root,
			Root: root,
		},
		Trust: skill.TrustBundled,
	}
}
