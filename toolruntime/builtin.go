package toolruntime

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	"github.com/fulcrus/hopclaw/canvas"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/contextengine"
	cronsvc "github.com/fulcrus/hopclaw/cron"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/internal/modules"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
	"github.com/fulcrus/hopclaw/isolation"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/media"
	"github.com/fulcrus/hopclaw/mediagen"
	"github.com/fulcrus/hopclaw/skill"
	netpkg "github.com/fulcrus/hopclaw/toolruntime/net"
	wakeupsvc "github.com/fulcrus/hopclaw/wakeup"
	watchsvc "github.com/fulcrus/hopclaw/watch"
)

var log = logging.WithSubsystem("toolruntime")

// ---------------------------------------------------------------------------
// Tool execution timeout defaults
// ---------------------------------------------------------------------------

const (
	defaultToolTimeout = 30 * time.Second
	browserToolTimeout = 60 * time.Second
	fsReadToolTimeout  = 10 * time.Second
	fsWriteToolTimeout = 30 * time.Second
	searchToolTimeout  = 30 * time.Second
	netToolTimeout     = 30 * time.Second
	execToolFallback   = 30 * time.Second // used when config.DefaultExecTimeout is zero
)

type BuiltinsConfig struct {
	Root                    string
	AllowedPaths            []string // extra paths allowed outside root (e.g. /tmp, ~/Documents)
	DefaultExecTimeout      time.Duration
	MaxReadBytes            int
	RuntimeFacts            func(root string) skill.RuntimeContext
	Services                ServicesConfig
	BrowserHost             BrowserHostConfig
	SkillEnsureLimit        int
	ExecConstraints         config.ExecConstraints
	FSConstraints           config.FSConstraints
	NetConstraints          config.NetConstraints
	MediaGenerationRegistry *mediagen.Registry
}

type BrowserHostConfig struct {
	BaseURL   string
	AuthToken string
}

type Builtins struct {
	builtinWorkspaceState
	builtinConfigState
	destinations DestinationCatalog
	builtinRegistryState
	layer2           *Layer2Registry       // optional: env.probe/env.refresh coordination
	skillService     *skill.Service        // optional: skill.list reads registry
	moduleCatalog    *modules.Store        // optional: unified skill projections
	clawHub          skill.ClawHubClient   // optional: skill.install/remove/list catalog
	sessions         agent.SessionStore    // optional: session.list/history
	memoryStore      agent.MemoryStore     // optional: memory.get/set/delete/search
	knowledge        *knowledge.Service    // optional: knowledge sources/search
	artifactStore    artifact.Store        // optional: artifact URI resolution in text tools
	channelManager   *channelmgr.Manager   // optional: channel.* tools
	extensions       *extregistry.Registry // optional: unified extension registry
	cronService      *cronsvc.Service      // optional: cron.* tools
	wakeupService    *wakeupsvc.Service    // optional: wakeup.* tools
	watchService     *watchsvc.Service     // optional: watch.* tools
	spawner          *isolation.Spawner    // optional: agent.* tools
	browserClient    *browserclient.Client // optional: browser.* tools
	desktopClient    *desktopclient.Client // optional: desktop.* tools
	canvasHost       *canvas.Host          // optional: a2ui.* tools
	mediaRegistry    *media.Registry       // optional: vision.* tools
	mediaGenRegistry *mediagen.Registry    // optional: image/video/music generation tools
}

func NewBuiltins(cfg BuiltinsConfig) *Builtins {
	cfg = normalizeBuiltinsConfig(cfg)
	workspace := newBuiltinWorkspaceState(cfg)
	runtimePkg := newBuiltinRuntimePackage(workspace.rootAbs)

	b := &Builtins{
		builtinWorkspaceState: workspace,
		builtinConfigState:    builtinConfigState{config: cfg},
		builtinRegistryState:  newBuiltinRegistryState(),
		mediaGenRegistry:      cfg.MediaGenerationRegistry,
	}
	for _, category := range builtinCategoryCatalog() {
		b.addTools(runtimePkg, category.Load(cfg))
	}
	return b
}

// jsonResult builds a ToolResult from a JSON-serializable payload.
func (b *Builtins) jsonResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) ToolDefinitions(*agent.Session) []agent.ToolDefinition {
	if len(b.definitions) == 0 {
		return nil
	}
	out := make([]agent.ToolDefinition, 0, len(b.definitions))
	for _, definition := range b.definitions {
		if hiddenBuiltinToolName(definition.Name) {
			continue
		}
		out = append(out, copyToolDefinition(definition))
	}
	return out
}

func (b *Builtins) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	trimmed := strings.TrimSpace(name)
	if hiddenBuiltinToolName(trimmed) {
		return nil, false
	}
	bound, ok := b.tools[trimmed]
	if !ok {
		return nil, false
	}
	copied := bound
	copied.Manifest.InputSchema = cloneSchema(bound.Manifest.InputSchema)
	copied.Manifest.OutputSchema = cloneSchema(bound.Manifest.OutputSchema)
	copied.Manifest.Aliases = append([]string(nil), bound.Manifest.Aliases...)
	definition, _ := findBuiltinDefinition(b.definitions, copied.Manifest.Name)
	return resolvedToolFromBinding(&copied, definition, "builtin"), true
}

func (b *Builtins) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	if len(calls) == 0 {
		return nil, nil
	}

	// Fast path: single tool call — no goroutine overhead.
	if len(calls) == 1 {
		result, err := b.executeOneWithTimeout(ctx, run, session, calls[0])
		if err != nil {
			return nil, err
		}
		return []contextengine.ToolResult{result}, nil
	}

	// Concurrent path: pre-allocate result slots so each goroutine writes its own index.
	results := make([]contextengine.ToolResult, len(calls))
	errs := make([]error, len(calls))

	var wg sync.WaitGroup
	wg.Add(len(calls))
	for i, call := range calls {
		go func(idx int, c agent.ToolCall) {
			defer wg.Done()
			results[idx], errs[idx] = b.executeOneWithTimeout(ctx, run, session, c)
		}(i, call)
	}
	wg.Wait()

	// Multi-call batches should preserve per-call outcomes instead of discarding
	// completed results because one sibling failed.
	for i, err := range errs {
		if err != nil {
			results[i] = batchErrorResult(calls[i], err)
		}
	}
	return results, nil
}

// executeOneWithTimeout wraps executeOne with a per-tool context timeout.
// On timeout it returns a ToolResult with an error message rather than a Go error,
// so the agent can continue processing remaining tools.
func (b *Builtins) executeOneWithTimeout(ctx context.Context, run *agent.Run, session *agent.Session, call agent.ToolCall) (contextengine.ToolResult, error) {
	timeout := b.toolTimeout(call.Name)
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	result, err := b.executeOne(ctx, run, session, call)
	if err != nil {
		return result, err
	}

	// Check if the context timed out during execution.
	if ctx.Err() == context.DeadlineExceeded {
		return contextengine.ToolResult{
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Content:    fmt.Sprintf(`{"error":"tool %q timed out after %s"}`, call.Name, timeout),
		}, nil
	}
	return result, nil
}

// toolTimeout returns the execution timeout for the given tool name.
// It checks the tool manifest first, then falls back to domain-based defaults.
func (b *Builtins) toolTimeout(name string) time.Duration {
	// Prefer manifest-declared timeout.
	if bound, ok := b.tools[name]; ok && bound.Manifest.Timeout > 0 {
		return bound.Manifest.Timeout
	}

	// Domain-based defaults.
	switch {
	case strings.HasPrefix(name, "browser."):
		return browserToolTimeout
	case strings.HasPrefix(name, "exec."):
		if b.config.DefaultExecTimeout > 0 {
			return b.config.DefaultExecTimeout
		}
		return execToolFallback
	case strings.HasPrefix(name, "net."), strings.HasPrefix(name, "search."):
		return netToolTimeout
	case strings.HasPrefix(name, "fs."):
		switch name {
		case "fs.read", "fs.list", "fs.tree", "fs.find", "fs.grep", "fs.stat", "fs.hash":
			return fsReadToolTimeout
		default:
			return fsWriteToolTimeout
		}
	default:
		return defaultToolTimeout
	}
}

func (b *Builtins) executeOne(ctx context.Context, run *agent.Run, session *agent.Session, call agent.ToolCall) (contextengine.ToolResult, error) {
	ctx = withBuiltinRunContext(ctx, run)
	ctx = withBuiltinSessionContext(ctx, session)
	if handler, ok := b.handlers[call.Name]; ok {
		return handler(ctx, b, call)
	}
	switch call.Name {
	case "fs.list":
		return b.listFiles(call)
	case "fs.tree":
		return b.treeFiles(call)
	case "fs.find":
		return b.findFiles(call)
	case "fs.grep":
		return b.grepFiles(call)
	case "fs.read":
		return b.readFile(call)
	case "fs.stat":
		return b.statFile(call)
	case "fs.hash":
		return b.hashFile(call)
	case "fs.write":
		return b.writeFile(call)
	case "fs.edit":
		return b.editFile(call)
	case "fs.patch":
		return b.patchFiles(ctx, call)
	case "fs.diff", "fs.changes", "fs.revert":
		// These are handled by ShadowExecutor middleware.
		// If reached here, shadow middleware is not in the chain.
		return contextengine.ToolResult{
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Content:    `{"error": "edit shadow middleware is not configured"}`,
		}, nil
	case "fs.delete":
		return b.deleteFile(call)
	case "fs.move":
		return b.moveFile(call)
	case "fs.copy":
		return b.copyFile(call)
	case "fs.mkdir":
		return b.mkdirFile(call)
	case "fs.append":
		return b.appendFile(call)
	default:
		return contextengine.ToolResult{}, fmt.Errorf("builtin tool %q is not supported", call.Name)
	}
}

func (b *Builtins) patchFiles(ctx context.Context, call agent.ToolCall) (contextengine.ToolResult, error) {
	patchText, err := requiredString(call.Input, "patch")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], b.config.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.patch timeout_seconds: %w", err)
	}
	strip, err := intFrom(call.Input["strip"], 1)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.patch strip: %w", err)
	}
	reverse, err := boolFrom(call.Input["reverse"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.patch reverse flag: %w", err)
	}
	patchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	appliedFiles, err := b.applyUnifiedPatch(patchCtx, patchText, strip, reverse)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.patch failed: %w", err)
	}
	body, err := json.MarshalIndent(map[string]any{
		"applied_files": appliedFiles,
		"file_count":    len(appliedFiles),
		"reverse":       reverse,
		"message":       fmt.Sprintf("patch applied to %d file(s)", len(appliedFiles)),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) deleteFile(call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	recursive, err := boolFrom(call.Input["recursive"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.delete recursive flag: %w", err)
	}
	resolvedPath, err := b.resolvePathNoFollow(pathValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	info, err := os.Lstat(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.delete: %w", err)
	}
	if info.IsDir() && recursive {
		if err := os.RemoveAll(resolvedPath); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("fs.delete: %w", err)
		}
	} else {
		if err := os.Remove(resolvedPath); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("fs.delete: %w", err)
		}
	}
	body, err := json.MarshalIndent(map[string]any{
		"path":      b.displayPath(resolvedPath),
		"workspace": filepath.ToSlash(b.rootAbs),
		"message":   fmt.Sprintf("deleted %s", b.displayPath(resolvedPath)),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) moveFile(call agent.ToolCall) (contextengine.ToolResult, error) {
	source, err := requiredString(call.Input, "source")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	destination, err := requiredString(call.Input, "destination")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	srcPath, err := b.resolvePath(source)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	dstPath, err := b.resolvePath(destination)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if _, err := os.Lstat(srcPath); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.move: source does not exist: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.move: %w", err)
	}
	if err := os.Rename(srcPath, dstPath); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.move: %w", err)
	}
	body, err := json.MarshalIndent(map[string]any{
		"source":      b.displayPath(srcPath),
		"destination": b.displayPath(dstPath),
		"workspace":   filepath.ToSlash(b.rootAbs),
		"message":     fmt.Sprintf("moved %s → %s", b.displayPath(srcPath), b.displayPath(dstPath)),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) copyFile(call agent.ToolCall) (contextengine.ToolResult, error) {
	source, err := requiredString(call.Input, "source")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	destination, err := requiredString(call.Input, "destination")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	srcPath, err := b.resolvePath(source)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	dstPath, err := b.resolvePath(destination)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.copy: %w", err)
	}
	if srcInfo.IsDir() {
		return contextengine.ToolResult{}, fmt.Errorf("fs.copy: source is a directory; use fs.move or exec.run for directory copies")
	}
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.copy: %w", err)
	}
	defer srcFile.Close()
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.copy: %w", err)
	}
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.copy: %w", err)
	}
	defer dstFile.Close()
	written, err := io.Copy(dstFile, srcFile)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.copy: %w", err)
	}
	body, err := json.MarshalIndent(map[string]any{
		"source":       b.displayPath(srcPath),
		"destination":  b.displayPath(dstPath),
		"workspace":    filepath.ToSlash(b.rootAbs),
		"bytes_copied": written,
		"message":      fmt.Sprintf("copied %s → %s (%d bytes)", b.displayPath(srcPath), b.displayPath(dstPath), written),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) mkdirFile(call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolvedPath, err := b.resolvePath(pathValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := os.MkdirAll(resolvedPath, 0o755); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.mkdir: %w", err)
	}
	body, err := json.MarshalIndent(map[string]any{
		"path":      b.displayPath(resolvedPath),
		"workspace": filepath.ToSlash(b.rootAbs),
		"message":   fmt.Sprintf("created directory %s", b.displayPath(resolvedPath)),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) appendFile(call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	contentValue, err := requiredString(call.Input, "content")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolvedPath, err := b.resolvePath(pathValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(resolvedPath), 0o755); err != nil {
		return contextengine.ToolResult{}, err
	}
	file, err := os.OpenFile(resolvedPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.append: %w", err)
	}
	defer file.Close()
	n, err := file.WriteString(contentValue)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.append: %w", err)
	}
	body, err := json.MarshalIndent(map[string]any{
		"path":           b.displayPath(resolvedPath),
		"workspace":      filepath.ToSlash(b.rootAbs),
		"bytes_appended": n,
		"message":        fmt.Sprintf("appended %d bytes to %s", n, b.displayPath(resolvedPath)),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func builtinFSReadSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path relative to the configured workspace root.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Optional byte offset to start reading from.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Optional maximum number of bytes to read.",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func builtinFSTreeSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Optional tree root relative to the configured workspace root. Defaults to root.",
			},
			"max_depth": map[string]any{
				"type":        "integer",
				"description": "Maximum directory depth to descend into.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of entries to return.",
			},
		},
		"additionalProperties": false,
	}
}

func builtinFSFindSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Optional search root relative to the configured workspace root. Defaults to root.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Filename or relative-path pattern to match.",
			},
			"glob": map[string]any{
				"type":        "boolean",
				"description": "Treat pattern as a glob instead of substring match.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "Walk directories recursively when true.",
			},
			"kind": map[string]any{
				"type":        "string",
				"description": "Filter by file, directory, or any.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of matches to return.",
			},
		},
		"required":             []string{"pattern"},
		"additionalProperties": false,
	}
}

func builtinFSGrepSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Optional file or directory path relative to the configured workspace root. Defaults to root.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Text or regular expression to search for.",
			},
			"regexp": map[string]any{
				"type":        "boolean",
				"description": "Interpret pattern as a Go regular expression.",
			},
			"ignore_case": map[string]any{
				"type":        "boolean",
				"description": "Perform case-insensitive matching when true.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "Walk directories recursively when true.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of matches to return.",
			},
		},
		"required":             []string{"pattern"},
		"additionalProperties": false,
	}
}

func builtinFSHashSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to the configured workspace root.",
			},
			"algorithm": map[string]any{
				"type":        "string",
				"description": "Hash algorithm: md5, sha1, sha256, sha512.",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func builtinFSListSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Optional directory path relative to the configured workspace root. Defaults to root.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "Walk directories recursively when true.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of entries to return.",
			},
		},
		"additionalProperties": false,
	}
}

func builtinFSStatSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path relative to the configured workspace root.",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func builtinFSWriteSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path relative to the configured workspace root.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Full content to write.",
			},
			"append": map[string]any{
				"type":        "boolean",
				"description": "Append instead of overwriting when true.",
			},
		},
		"required":             []string{"path", "content"},
		"additionalProperties": false,
	}
}

func builtinFSEditSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path relative to the configured workspace root.",
			},
			"old_text": map[string]any{
				"type":        "string",
				"description": "Text to search for. Exact match is tried first; if it fails, whitespace-normalized matching is attempted automatically.",
			},
			"new_text": map[string]any{
				"type":        "string",
				"description": "Replacement text.",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace every match when true, otherwise only the first.",
			},
			"line_start": map[string]any{
				"type":        "integer",
				"description": "Optional 1-based line number. When provided and exact text match fails, replaces lines from line_start to line_end instead.",
			},
			"line_end": map[string]any{
				"type":        "integer",
				"description": "Optional 1-based end line (inclusive). Defaults to line_start + number of lines in old_text - 1.",
			},
		},
		"required":             []string{"path", "old_text", "new_text"},
		"additionalProperties": false,
	}
}

func builtinFSPatchSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"patch": map[string]any{
				"type":        "string",
				"description": "Unified diff patch content.",
			},
			"strip": map[string]any{
				"type":        "integer",
				"description": "Path strip count applied to patch paths. Defaults to 1.",
			},
			"reverse": map[string]any{
				"type":        "boolean",
				"description": "Reverse the patch when true.",
			},
			"timeout_seconds": map[string]any{
				"type":        "number",
				"description": "Optional timeout in seconds.",
			},
		},
		"required":             []string{"patch"},
		"additionalProperties": false,
	}
}

func builtinFSDeleteSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path relative to the configured workspace root.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "Remove directories and their contents recursively when true.",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func builtinFSMoveSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source": map[string]any{
				"type":        "string",
				"description": "Source path relative to the configured workspace root.",
			},
			"destination": map[string]any{
				"type":        "string",
				"description": "Destination path relative to the configured workspace root.",
			},
		},
		"required":             []string{"source", "destination"},
		"additionalProperties": false,
	}
}

func builtinFSCopySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source": map[string]any{
				"type":        "string",
				"description": "Source file path relative to the configured workspace root.",
			},
			"destination": map[string]any{
				"type":        "string",
				"description": "Destination file path relative to the configured workspace root.",
			},
		},
		"required":             []string{"source", "destination"},
		"additionalProperties": false,
	}
}

func builtinFSMkdirSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path relative to the configured workspace root. Parent directories are created automatically.",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func builtinFSAppendSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to the configured workspace root.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Content to append.",
			},
		},
		"required":             []string{"path", "content"},
		"additionalProperties": false,
	}
}

func builtinFSDiffSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path_a": map[string]any{
				"type":        "string",
				"description": "First file path relative to the workspace root.",
			},
			"path_b": map[string]any{
				"type":        "string",
				"description": "Second file path relative to the workspace root.",
			},
		},
		"required":             []string{"path_a", "path_b"},
		"additionalProperties": false,
	}
}

func builtinFSDiffOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path_a": stringSchema("first file path"),
		"path_b": stringSchema("second file path"),
		"diff":   stringSchema("unified diff output"),
	})
}

func builtinFSChangesSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Optional filter: only show changes for paths containing this string.",
			},
			"detail": map[string]any{
				"type":        "boolean",
				"description": "Include unified diffs for each changed file when true.",
			},
		},
		"additionalProperties": false,
	}
}

func builtinFSChangesOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"changes": arraySchema(objectSchema(map[string]any{
			"path":   stringSchema("relative file path"),
			"status": stringSchema("modified, created, or deleted"),
			"diff":   stringSchema("unified diff if detail=true"),
		}), "list of changed files"),
		"count": integerSchema("number of changed files"),
	})
}

func builtinFSRevertSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to the workspace root to revert.",
			},
			"dry_run": map[string]any{
				"type":        "boolean",
				"description": "Show what would be reverted without actually reverting.",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func builtinFSRevertOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":    stringSchema("file path"),
		"message": stringSchema("revert result"),
	})
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".hopclaw-write-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		tempFile.Close()
		if cleanup {
			logging.DebugIfErr(os.Remove(tempPath), "remove temp file failed", slog.String("path", tempPath))
		}
	}()
	if _, err := tempFile.Write(data); err != nil {
		return err
	}
	if err := tempFile.Chmod(mode); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func requiredString(input map[string]any, key string) (string, error) {
	value, err := stringFrom(input[key])
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func stringFrom(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	default:
		return "", fmt.Errorf("expected string, got %T", value)
	}
}

func stringFromDefault(value any, fallback string) string {
	if s, ok := value.(string); ok && s != "" {
		return s
	}
	return fallback
}

func stringSliceFrom(value any) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, err := stringFrom(item)
			if err != nil {
				return nil, err
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected array, got %T", value)
	}
}

func boolFrom(value any) (bool, error) {
	switch typed := value.(type) {
	case nil:
		return false, nil
	case bool:
		return typed, nil
	case string:
		parsed, err := strconv.ParseBool(typed)
		if err != nil {
			return false, err
		}
		return parsed, nil
	default:
		return false, fmt.Errorf("expected boolean, got %T", value)
	}
}

func boolFromDefault(value any, fallback bool) (bool, error) {
	if value == nil {
		return fallback, nil
	}
	return boolFrom(value)
}

func int64From(value any) (int64, error) {
	switch typed := value.(type) {
	case nil:
		return 0, nil
	case int:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		return int64(typed), nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0, nil
		}
		return strconv.ParseInt(typed, 10, 64)
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

func intFrom(value any, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	converted, err := int64From(value)
	if err != nil {
		return 0, err
	}
	return int(converted), nil
}

func mapFrom(value any) (map[string]any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return typed, nil
	default:
		return nil, fmt.Errorf("expected object, got %T", value)
	}
}

func floatFrom(value any, fallback float64) (float64, error) {
	switch typed := value.(type) {
	case nil:
		return fallback, nil
	case float64:
		return typed, nil
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback, nil
		}
		return strconv.ParseFloat(typed, 64)
	default:
		return 0, fmt.Errorf("expected number, got %T", value)
	}
}

func timeoutFrom(value any, fallback time.Duration) (time.Duration, error) {
	switch typed := value.(type) {
	case nil:
		return fallback, nil
	case int:
		return time.Duration(typed) * time.Second, nil
	case int64:
		return time.Duration(typed) * time.Second, nil
	case float64:
		return time.Duration(typed * float64(time.Second)), nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback, nil
		}
		seconds, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(seconds * float64(time.Second)), nil
	default:
		return 0, fmt.Errorf("expected duration seconds, got %T", value)
	}
}

func fileKind(info os.FileInfo) string {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "symlink"
	case info.IsDir():
		return "directory"
	default:
		return "file"
	}
}

func newHasher(algorithm string) (hash.Hash, string, error) {
	switch strings.ToLower(strings.TrimSpace(algorithm)) {
	case "", "sha256":
		return sha256.New(), "sha256", nil
	case "md5":
		return md5.New(), "md5", nil
	case "sha1":
		return sha1.New(), "sha1", nil
	case "sha512":
		return sha512.New(), "sha512", nil
	default:
		return nil, "", fmt.Errorf("unsupported fs.hash algorithm %q", algorithm)
	}
}

func matchesPattern(candidate, pattern string, useGlob bool) bool {
	if useGlob {
		ok, err := filepath.Match(pattern, candidate)
		if err != nil {
			return false
		}
		return ok
	}
	return strings.Contains(candidate, pattern)
}

func isBlameHeader(line string) bool {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return false
	}
	if len(fields[0]) < 8 {
		return false
	}
	for _, field := range fields[1:3] {
		if _, err := strconv.Atoi(field); err != nil {
			return false
		}
	}
	return true
}

type lineMatcher interface {
	Match(string) bool
}

type regexpMatcher struct {
	re *regexp.Regexp
}

func (m regexpMatcher) Match(line string) bool {
	return m.re.MatchString(line)
}

type containsMatcher struct {
	needle     string
	ignoreCase bool
}

func (m containsMatcher) Match(line string) bool {
	if m.ignoreCase {
		line = strings.ToLower(line)
	}
	return strings.Contains(line, m.needle)
}

// stripHTML removes HTML tags and returns plain text.
// This is used by root-level services (services_search.go).
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var whitespaceCollapseRe = regexp.MustCompile(`[ \t]+`)
var blankLineCollapseRe = regexp.MustCompile(`\n{3,}`)

func stripHTML(html string) string {
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	text := scriptRe.ReplaceAllString(html, "")
	text = styleRe.ReplaceAllString(text, "")
	text = htmlTagRe.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", `"`)
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&apos;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = whitespaceCollapseRe.ReplaceAllString(text, " ")
	text = blankLineCollapseRe.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)
	return text
}

func intSliceFrom(value any) ([]int, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case []int:
		return append([]int(nil), typed...), nil
	case []any:
		out := make([]int, 0, len(typed))
		for _, item := range typed {
			n, err := int64From(item)
			if err != nil {
				return nil, err
			}
			out = append(out, int(n))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected array of integers, got %T", value)
	}
}

// handleNetUpload is a forwarding wrapper for use by builtin_semantic.go.
// It delegates to the net sub-package handler via the runtime adapter.
func handleNetUpload(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	adapter := &netRuntimeAdapter{b}
	// Find the net.upload handler from the sub-package defs.
	for _, d := range netpkg.ToolDefs() {
		if d.Manifest.Name == "net.upload" {
			return d.Handler(ctx, adapter, call)
		}
	}
	return contextengine.ToolResult{}, fmt.Errorf("net.upload handler not found in net sub-package")
}

func cloneSchema(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
