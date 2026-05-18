package toolruntime

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

func fsExtraToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:             "fs.chmod",
				Description:      "Modify file permissions within the workspace root.",
				InputSchema:      fsChmodInputSchema(),
				OutputSchema:     fsChmodOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "fs:{path}",
			},
			Handler: handleFSChmod,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "fs.link",
				Description:      "Create a symbolic link within the workspace root.",
				InputSchema:      fsLinkInputSchema(),
				OutputSchema:     fsLinkOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "fs:{link_path}",
			},
			Handler: handleFSLink,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "fs.tmp",
				Description:     "Create a temporary file or directory within the workspace root.",
				InputSchema:     fsTmpInputSchema(),
				OutputSchema:    fsTmpOutputSchema(),
				SideEffectClass: "local_write",
			},
			Handler: handleFSTmp,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "fs.disk",
				Description:     "Report disk usage information for a path within the workspace root.",
				InputSchema:     fsDiskInputSchema(),
				OutputSchema:    fsDiskOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "fs:disk",
			},
			Handler: handleFSDisk,
		},
	}
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func fsChmodInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path relative to the workspace root.",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "File mode as an octal string, e.g. \"0644\" or \"0755\".",
			},
		},
		"required":             []string{"path", "mode"},
		"additionalProperties": false,
	}
}

func fsLinkInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "The path that the symbolic link will point to.",
			},
			"link_path": map[string]any{
				"type":        "string",
				"description": "The path of the symbolic link to create.",
			},
		},
		"required":             []string{"target", "link_path"},
		"additionalProperties": false,
	}
}

func fsTmpInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir": map[string]any{
				"type":        "string",
				"description": "Parent directory for the temporary entry. Defaults to the workspace root.",
			},
			"prefix": map[string]any{
				"type":        "string",
				"description": "Optional prefix for the generated name.",
			},
			"is_dir": map[string]any{
				"type":        "boolean",
				"description": "When true, create a temporary directory instead of a file.",
			},
		},
		"additionalProperties": false,
	}
}

func fsDiskInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to query. Defaults to the workspace root.",
			},
		},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func fsChmodOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":    stringSchema("Path relative to the workspace root."),
		"mode":    stringSchema("Applied file mode."),
		"message": stringSchema("Human-readable summary."),
	}, "path", "mode", "message")
}

func fsLinkOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"target":    stringSchema("Target the symlink points to."),
		"link_path": stringSchema("Path of the created symlink."),
		"message":   stringSchema("Human-readable summary."),
	}, "target", "link_path", "message")
}

func fsTmpOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":   stringSchema("Path of the created temporary entry."),
		"is_dir": booleanSchema("Whether a directory was created."),
	}, "path", "is_dir")
}

func fsDiskOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":            stringSchema("Path that was queried."),
		"total_bytes":     integerSchema("Total disk space in bytes."),
		"free_bytes":      integerSchema("Free disk space in bytes."),
		"available_bytes": integerSchema("Available disk space in bytes (may differ from free)."),
		"used_bytes":      integerSchema("Used disk space in bytes."),
		"use_percent":     integerSchema("Disk usage percentage (0-100)."),
	}, "path", "total_bytes", "free_bytes", "available_bytes", "used_bytes", "use_percent")
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleFSChmod(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.chmod: %w", err)
	}
	modeStr, err := requiredString(call.Input, "mode")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.chmod: %w", err)
	}
	resolved, err := b.resolvePath(pathValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	parsed, err := strconv.ParseUint(modeStr, 8, 32)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.chmod: invalid mode %q: %w", modeStr, err)
	}
	if err := os.Chmod(resolved, os.FileMode(parsed)); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.chmod: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"path":    b.displayPath(resolved),
		"mode":    modeStr,
		"message": fmt.Sprintf("changed mode of %s to %s", b.displayPath(resolved), modeStr),
	})
}

func handleFSLink(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	targetValue, err := requiredString(call.Input, "target")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.link: %w", err)
	}
	linkPathValue, err := requiredString(call.Input, "link_path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.link: %w", err)
	}

	resolvedTarget, err := b.resolvePath(targetValue)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.link: target: %w", err)
	}
	resolvedLink, err := b.resolvePath(linkPathValue)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.link: link_path: %w", err)
	}

	if err := os.Symlink(resolvedTarget, resolvedLink); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.link: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"target":    b.displayPath(resolvedTarget),
		"link_path": b.displayPath(resolvedLink),
		"message":   fmt.Sprintf("created symlink %s -> %s", b.displayPath(resolvedLink), b.displayPath(resolvedTarget)),
	})
}

func handleFSTmp(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	prefix, _ := stringFrom(call.Input["prefix"])
	isDir, err := boolFrom(call.Input["is_dir"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.tmp: invalid is_dir: %w", err)
	}

	parentDir := b.rootAbs
	if strings.TrimSpace(dirValue) != "" {
		parentDir, err = b.resolvePath(dirValue)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("fs.tmp: %w", err)
		}
	}

	var createdPath string
	if isDir {
		createdPath, err = os.MkdirTemp(parentDir, prefix)
	} else {
		var f *os.File
		f, err = os.CreateTemp(parentDir, prefix)
		if err == nil {
			createdPath = f.Name()
			f.Close()
		}
	}
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.tmp: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"path":   b.displayPath(createdPath),
		"is_dir": isDir,
	})
}

func handleFSDisk(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, _ := stringFrom(call.Input["path"])

	resolved := b.rootAbs
	if strings.TrimSpace(pathValue) != "" {
		var err error
		resolved, err = b.resolvePath(pathValue)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("fs.disk: %w", err)
		}
	}

	total, free, available, err := diskUsage(resolved)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.disk: %w", err)
	}
	used := total - free

	var usePercent uint64
	if total > 0 {
		usePercent = (used * 100) / total
	}

	return b.jsonResult(call, map[string]any{
		"path":            b.displayPath(resolved),
		"total_bytes":     total,
		"free_bytes":      free,
		"available_bytes": available,
		"used_bytes":      used,
		"use_percent":     usePercent,
	})
}
