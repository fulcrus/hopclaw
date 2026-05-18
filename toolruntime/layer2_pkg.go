package toolruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

func init() {
	RegisterLayer2GroupToggle("pkg", "packages")
}

// runExternalCmd executes an external command with a timeout and returns
// stdout, stderr, the exit code, and any execution error. It is used by
// Layer 2 tools that shell out to non-git binaries.
func runExternalCmd(ctx context.Context, workingDir string, timeout time.Duration, name string, args ...string) (string, string, int, error) {
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(execCtx, name, args...)
	cmd.Dir = workingDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", "", -1, err
		}
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), exitCode, nil
}

func (r *Layer2Registry) registerPkgGroup() {
	timeout := r.config.DefaultExecTimeout
	r.registerGroup("pkg", []string{}, []layer2ToolDef{
		{manifest: skill.ToolManifest{
			Name: "pkg.install", Description: "Install a package using the system package manager.",
			InputSchema: pkgInstallSchema(), OutputSchema: pkgResultOutputSchema(),
			SideEffectClass: "destructive", RequiresApproval: true, Idempotent: false, Timeout: timeout,
		}, execFn: pkgInstallExec},
		{manifest: skill.ToolManifest{
			Name: "pkg.uninstall", Description: "Uninstall a package using the system package manager.",
			InputSchema: pkgUninstallSchema(), OutputSchema: pkgResultOutputSchema(),
			SideEffectClass: "destructive", RequiresApproval: true, Idempotent: false, Timeout: timeout,
		}, execFn: pkgUninstallExec},
		{manifest: skill.ToolManifest{
			Name: "pkg.list", Description: "List installed packages using the system package manager.",
			InputSchema: pkgListSchema(), OutputSchema: pkgListOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: pkgListExec},
		{manifest: skill.ToolManifest{
			Name: "pkg.search", Description: "Search for packages using the system package manager.",
			InputSchema: pkgSearchSchema(), OutputSchema: pkgResultOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: pkgSearchExec},
		{manifest: skill.ToolManifest{
			Name: "pkg.update", Description: "Update packages using the system package manager.",
			InputSchema: pkgUpdateSchema(), OutputSchema: pkgResultOutputSchema(),
			SideEffectClass: "destructive", RequiresApproval: true, Idempotent: false, Timeout: timeout,
		}, execFn: pkgUpdateExec},
	})
}

// --- Package manager detection ---

type pkgManager struct {
	name    string
	bin     string
	install func(name string) []string
	remove  func(name string) []string
	list    func() []string
	search  func(query string) []string
	update  func(name string) []string
}

var knownManagers = []pkgManager{
	{
		name:    "brew",
		bin:     "brew",
		install: func(n string) []string { return []string{"install", n} },
		remove:  func(n string) []string { return []string{"uninstall", n} },
		list:    func() []string { return []string{"list", "--formula"} },
		search:  func(q string) []string { return []string{"search", q} },
		update:  func(n string) []string { return updateArgs("upgrade", n) },
	},
	{
		name:    "apt-get",
		bin:     "apt-get",
		install: func(n string) []string { return []string{"install", "-y", n} },
		remove:  func(n string) []string { return []string{"remove", "-y", n} },
		list:    func() []string { return []string{"list", "--installed"} },
		search:  func(q string) []string { return []string{"search", q} },
		update:  func(n string) []string { return updateArgs("upgrade", n) },
	},
	{
		name:    "apk",
		bin:     "apk",
		install: func(n string) []string { return []string{"add", n} },
		remove:  func(n string) []string { return []string{"del", n} },
		list:    func() []string { return []string{"list", "--installed"} },
		search:  func(q string) []string { return []string{"search", q} },
		update:  func(n string) []string { return updateArgs("upgrade", n) },
	},
	{
		name:    "yum",
		bin:     "yum",
		install: func(n string) []string { return []string{"install", "-y", n} },
		remove:  func(n string) []string { return []string{"remove", "-y", n} },
		list:    func() []string { return []string{"list", "installed"} },
		search:  func(q string) []string { return []string{"search", q} },
		update:  func(n string) []string { return updateArgs("update", n) },
	},
	{
		name:    "dnf",
		bin:     "dnf",
		install: func(n string) []string { return []string{"install", "-y", n} },
		remove:  func(n string) []string { return []string{"remove", "-y", n} },
		list:    func() []string { return []string{"list", "installed"} },
		search:  func(q string) []string { return []string{"search", q} },
		update:  func(n string) []string { return updateArgs("upgrade", n) },
	},
	{
		name:    "pacman",
		bin:     "pacman",
		install: func(n string) []string { return []string{"-S", "--noconfirm", n} },
		remove:  func(n string) []string { return []string{"-R", "--noconfirm", n} },
		list:    func() []string { return []string{"-Q"} },
		search:  func(q string) []string { return []string{"-Ss", q} },
		update:  func(n string) []string { return updateArgsPacman(n) },
	},
	{
		name:    "pip3",
		bin:     "pip3",
		install: func(n string) []string { return []string{"install", n} },
		remove:  func(n string) []string { return []string{"uninstall", "-y", n} },
		list:    func() []string { return []string{"list"} },
		search:  func(q string) []string { return []string{"index", "versions", q} },
		update:  func(n string) []string { return updateArgs("install", "--upgrade", n) },
	},
	{
		name:    "pip",
		bin:     "pip",
		install: func(n string) []string { return []string{"install", n} },
		remove:  func(n string) []string { return []string{"uninstall", "-y", n} },
		list:    func() []string { return []string{"list"} },
		search:  func(q string) []string { return []string{"index", "versions", q} },
		update:  func(n string) []string { return updateArgs("install", "--upgrade", n) },
	},
	{
		name:    "npm",
		bin:     "npm",
		install: func(n string) []string { return []string{"install", n} },
		remove:  func(n string) []string { return []string{"uninstall", n} },
		list:    func() []string { return []string{"list", "--depth=0"} },
		search:  func(q string) []string { return []string{"search", q} },
		update:  func(n string) []string { return updateArgs("update", n) },
	},
	{
		name:    "cargo",
		bin:     "cargo",
		install: func(n string) []string { return []string{"install", n} },
		remove:  func(n string) []string { return []string{"uninstall", n} },
		list:    func() []string { return []string{"install", "--list"} },
		search:  func(q string) []string { return []string{"search", q} },
		update:  func(n string) []string { return updateArgs("install", n) },
	},
}

func updateArgs(base ...string) []string {
	return base
}

func updateArgsPacman(name string) []string {
	if name == "" {
		return []string{"-Syu", "--noconfirm"}
	}
	return []string{"-S", "--noconfirm", name}
}

func detectPkgManager(preferred string) (*pkgManager, error) {
	if strings.TrimSpace(preferred) != "" {
		for i := range knownManagers {
			if knownManagers[i].name == preferred || knownManagers[i].bin == preferred {
				if _, err := exec.LookPath(knownManagers[i].bin); err == nil {
					return &knownManagers[i], nil
				}
				return nil, fmt.Errorf("package manager %q is not installed", preferred)
			}
		}
		return nil, fmt.Errorf("unknown package manager %q", preferred)
	}
	for i := range knownManagers {
		if _, err := exec.LookPath(knownManagers[i].bin); err == nil {
			return &knownManagers[i], nil
		}
	}
	return nil, fmt.Errorf("no supported package manager found; install one of: brew, apt-get, apk, yum, dnf, pacman, pip3, pip, npm, cargo")
}

// --- Package tool implementations ---

func pkgInstallExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	name, err := requiredString(call.Input, "name")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	managerHint, _ := stringFrom(call.Input["manager"])
	mgr, err := detectPkgManager(managerHint)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pkg.install: %w", err)
	}
	args := mgr.install(name)
	stdout, stderr, exitCode, err := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout, mgr.bin, args...)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pkg.install failed: %w", err)
	}
	return pkgResult(call, mgr.name, "install", stdout, stderr, exitCode)
}

func pkgUninstallExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	name, err := requiredString(call.Input, "name")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	managerHint, _ := stringFrom(call.Input["manager"])
	mgr, err := detectPkgManager(managerHint)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pkg.uninstall: %w", err)
	}
	args := mgr.remove(name)
	stdout, stderr, exitCode, err := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout, mgr.bin, args...)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pkg.uninstall failed: %w", err)
	}
	return pkgResult(call, mgr.name, "uninstall", stdout, stderr, exitCode)
}

func pkgListExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	managerHint, _ := stringFrom(call.Input["manager"])
	mgr, err := detectPkgManager(managerHint)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pkg.list: %w", err)
	}
	args := mgr.list()
	stdout, stderr, exitCode, err := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout, mgr.bin, args...)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pkg.list failed: %w", err)
	}
	packages := compactNonEmptyLines(stdout)
	payload := map[string]any{
		"manager":   mgr.name,
		"action":    "list",
		"packages":  packages,
		"count":     len(packages),
		"exit_code": exitCode,
	}
	if stderr != "" {
		payload["stderr"] = stderr
	}
	body, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return contextengine.ToolResult{}, marshalErr
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func pkgSearchExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	query, err := requiredString(call.Input, "query")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	managerHint, _ := stringFrom(call.Input["manager"])
	mgr, err := detectPkgManager(managerHint)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pkg.search: %w", err)
	}
	args := mgr.search(query)
	stdout, stderr, exitCode, err := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout, mgr.bin, args...)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pkg.search failed: %w", err)
	}
	return pkgResult(call, mgr.name, "search", stdout, stderr, exitCode)
}

func pkgUpdateExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	name, _ := stringFrom(call.Input["name"])
	managerHint, _ := stringFrom(call.Input["manager"])
	mgr, err := detectPkgManager(managerHint)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pkg.update: %w", err)
	}
	args := mgr.update(strings.TrimSpace(name))
	stdout, stderr, exitCode, err := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout, mgr.bin, args...)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pkg.update failed: %w", err)
	}
	return pkgResult(call, mgr.name, "update", stdout, stderr, exitCode)
}

func pkgResult(call agent.ToolCall, manager, action, stdout, stderr string, exitCode int) (contextengine.ToolResult, error) {
	payload := map[string]any{
		"manager":   manager,
		"action":    action,
		"stdout":    stdout,
		"stderr":    stderr,
		"exit_code": exitCode,
	}
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

// --- Package schemas ---

func pkgInstallSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string", "description": "Package name to install."},
			"manager": map[string]any{"type": "string", "description": "Package manager to use. Auto-detected if omitted."},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
}

func pkgUninstallSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string", "description": "Package name to uninstall."},
			"manager": map[string]any{"type": "string", "description": "Package manager to use. Auto-detected if omitted."},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
}

func pkgListSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"manager": map[string]any{"type": "string", "description": "Package manager to use. Auto-detected if omitted."},
		},
		"additionalProperties": false,
	}
}

func pkgSearchSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":   map[string]any{"type": "string", "description": "Search query."},
			"manager": map[string]any{"type": "string", "description": "Package manager to use. Auto-detected if omitted."},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func pkgUpdateSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string", "description": "Specific package to update. Omit to update all."},
			"manager": map[string]any{"type": "string", "description": "Package manager to use. Auto-detected if omitted."},
		},
		"additionalProperties": false,
	}
}

// --- Package output schemas ---

func pkgResultOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"manager":   stringSchema("Package manager that was used."),
		"action":    stringSchema("Action that was performed."),
		"stdout":    stringSchema("Standard output from the command."),
		"stderr":    stringSchema("Standard error from the command."),
		"exit_code": integerSchema("Process exit code."),
	}, "manager", "action", "stdout", "stderr", "exit_code")
}

func pkgListOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"manager":  stringSchema("Package manager that was used."),
		"action":   stringSchema("Action that was performed."),
		"packages": stringArraySchema("Installed package lines."),
		"count":    integerSchema("Number of packages listed."),
	}, "manager", "action", "packages", "count")
}
