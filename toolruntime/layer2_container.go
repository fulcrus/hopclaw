package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

func init() {
	RegisterLayer2GroupToggle("container", "container")
}

func (r *Layer2Registry) registerContainerGroup() {
	timeout := r.config.DefaultExecTimeout
	r.registerGroup("container", []string{"docker"}, []layer2ToolDef{
		{manifest: skill.ToolManifest{
			Name: "container.list", Description: "List Docker containers.",
			InputSchema: containerListSchema(), OutputSchema: containerListOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: containerListExec},
		{manifest: skill.ToolManifest{
			Name: "container.run", Description: "Run a Docker container.",
			InputSchema: containerRunSchema(), OutputSchema: containerRunOutputSchema(),
			SideEffectClass: "destructive", RequiresApproval: true, Idempotent: false, Timeout: timeout,
		}, execFn: containerRunExec},
		{manifest: skill.ToolManifest{
			Name: "container.stop", Description: "Stop a running Docker container.",
			InputSchema: containerStopSchema(), OutputSchema: containerCmdOutputSchema(),
			SideEffectClass: "destructive", RequiresApproval: true, Idempotent: false, Timeout: timeout,
		}, execFn: containerStopExec},
		{manifest: skill.ToolManifest{
			Name: "container.logs", Description: "Retrieve logs from a Docker container.",
			InputSchema: containerLogsSchema(), OutputSchema: containerLogsOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: containerLogsExec},
		{manifest: skill.ToolManifest{
			Name: "container.exec", Description: "Execute a command inside a running Docker container.",
			InputSchema: containerExecSchema(), OutputSchema: containerCmdOutputSchema(),
			SideEffectClass: "destructive", RequiresApproval: true, Idempotent: false, Timeout: timeout,
		}, execFn: containerExecExec},
		{manifest: skill.ToolManifest{
			Name: "container.build", Description: "Build a Docker image from a Dockerfile.",
			InputSchema: containerBuildSchema(), OutputSchema: containerCmdOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Idempotent: false, Timeout: timeout,
		}, execFn: containerBuildExec},
	})
}

// --- Container tool implementations ---

func containerListExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	showAll, _ := boolFrom(call.Input["all"])

	args := []string{"ps", "--format", "json"}
	if showAll {
		args = []string{"ps", "--all", "--format", "json"}
	}

	stdout, stderr, exitCode, err := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout, "docker", args...)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("container.list failed: %w", err)
	}

	// Docker outputs one JSON object per line; collect them.
	var containers []any
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]any
		if jsonErr := json.Unmarshal([]byte(line), &obj); jsonErr == nil {
			containers = append(containers, obj)
		}
	}
	if containers == nil {
		containers = []any{}
	}

	payload := map[string]any{
		"containers": containers,
		"count":      len(containers),
		"all":        showAll,
		"exit_code":  exitCode,
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

func containerRunExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	image, err := requiredString(call.Input, "image")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid container.run timeout_seconds: %w", err)
	}
	command, _ := stringFrom(call.Input["command"])
	cmdArgs, _ := stringSliceFrom(call.Input["args"])
	ports, _ := stringSliceFrom(call.Input["ports"])
	detach, _ := boolFrom(call.Input["detach"])
	containerName, _ := stringFrom(call.Input["name"])

	args := []string{"run"}
	if detach {
		args = append(args, "-d")
	}
	if strings.TrimSpace(containerName) != "" {
		args = append(args, "--name", containerName)
	}
	for _, p := range ports {
		args = append(args, "-p", p)
	}

	// Environment variables
	if envRaw, ok := call.Input["env"]; ok && envRaw != nil {
		if envMap, castOK := envRaw.(map[string]any); castOK {
			for k, v := range envMap {
				sv, _ := stringFrom(v)
				args = append(args, "-e", fmt.Sprintf("%s=%s", k, sv))
			}
		}
	}

	args = append(args, image)
	if strings.TrimSpace(command) != "" {
		args = append(args, command)
		args = append(args, cmdArgs...)
	}

	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, w.rootAbs, timeout, "docker", args...)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("container.run failed: %w", runErr)
	}

	return containerCmdResult(call, "run", stdout, stderr, exitCode)
}

func containerStopExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout, "docker", "stop", id)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("container.stop failed: %w", runErr)
	}

	return containerCmdResult(call, "stop", stdout, stderr, exitCode)
}

func containerLogsExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	tail, err := intFrom(call.Input["tail"], 100)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid container.logs tail: %w", err)
	}

	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout, "docker", "logs", "--tail", fmt.Sprintf("%d", tail), id)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("container.logs failed: %w", runErr)
	}

	payload := map[string]any{
		"id":        id,
		"tail":      tail,
		"stdout":    stdout,
		"stderr":    stderr,
		"exit_code": exitCode,
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

func containerExecExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	command, err := requiredString(call.Input, "command")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	cmdArgs, _ := stringSliceFrom(call.Input["args"])

	args := []string{"exec", id, command}
	args = append(args, cmdArgs...)

	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout, "docker", args...)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("container.exec failed: %w", runErr)
	}

	return containerCmdResult(call, "exec", stdout, stderr, exitCode)
}

func containerBuildExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	tag, err := requiredString(call.Input, "tag")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid container.build timeout_seconds: %w", err)
	}
	pathValue, _ := stringFrom(call.Input["path"])
	if strings.TrimSpace(pathValue) == "" {
		pathValue = "."
	}
	dockerFile, _ := stringFrom(call.Input["file"])

	buildDir := w.rootAbs
	if pathValue != "." {
		resolved, resolveErr := w.resolvePathWithOptions(pathValue, true)
		if resolveErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("container.build path: %w", resolveErr)
		}
		buildDir = resolved
	}

	args := []string{"build"}
	if strings.TrimSpace(dockerFile) != "" {
		args = append(args, "-f", dockerFile)
	}
	args = append(args, "-t", tag, ".")

	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, buildDir, timeout, "docker", args...)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("container.build failed: %w", runErr)
	}

	return containerCmdResult(call, "build", stdout, stderr, exitCode)
}

// --- Container result helper ---

func containerCmdResult(call agent.ToolCall, action, stdout, stderr string, exitCode int) (contextengine.ToolResult, error) {
	payload := map[string]any{
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

// --- Container schemas ---

func containerListSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"all": map[string]any{"type": "boolean", "description": "Include stopped containers when true."},
		},
		"additionalProperties": false,
	}
}

func containerRunSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"image":           map[string]any{"type": "string", "description": "Docker image to run."},
			"command":         map[string]any{"type": "string", "description": "Optional command to execute in the container."},
			"args":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional arguments for the command."},
			"ports":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Port mappings, e.g. [\"8080:80\"]."},
			"env":             map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Environment variables as key-value pairs."},
			"detach":          map[string]any{"type": "boolean", "description": "Run container in detached mode."},
			"name":            map[string]any{"type": "string", "description": "Optional container name."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"required":             []string{"image"},
		"additionalProperties": false,
	}
}

func containerStopSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string", "description": "Container ID or name to stop."},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

func containerLogsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":   map[string]any{"type": "string", "description": "Container ID or name."},
			"tail": map[string]any{"type": "integer", "description": "Number of lines from the end. Defaults to 100."},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

func containerExecSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":      map[string]any{"type": "string", "description": "Container ID or name."},
			"command": map[string]any{"type": "string", "description": "Command to execute."},
			"args":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional command arguments."},
		},
		"required":             []string{"id", "command"},
		"additionalProperties": false,
	}
}

func containerBuildSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":            map[string]any{"type": "string", "description": "Build context path. Defaults to current directory."},
			"tag":             map[string]any{"type": "string", "description": "Tag for the built image."},
			"file":            map[string]any{"type": "string", "description": "Optional Dockerfile path."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"required":             []string{"tag"},
		"additionalProperties": false,
	}
}

// --- Container output schemas ---

func containerListOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"containers": arraySchema(objectSchema(map[string]any{}), "List of container objects."),
		"count":      integerSchema("Number of containers."),
		"all":        booleanSchema("Whether stopped containers were included."),
		"exit_code":  integerSchema("Process exit code."),
	}, "containers", "count", "all", "exit_code")
}

func containerRunOutputSchema() map[string]any {
	return containerCmdOutputSchema()
}

func containerLogsOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":        stringSchema("Container ID or name."),
		"tail":      integerSchema("Number of tail lines requested."),
		"stdout":    stringSchema("Standard output from logs."),
		"stderr":    stringSchema("Standard error from logs."),
		"exit_code": integerSchema("Process exit code."),
	}, "id", "tail", "stdout", "stderr", "exit_code")
}

func containerCmdOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"action":    stringSchema("Docker action performed."),
		"stdout":    stringSchema("Standard output."),
		"stderr":    stringSchema("Standard error."),
		"exit_code": integerSchema("Process exit code."),
	}, "action", "stdout", "stderr", "exit_code")
}
