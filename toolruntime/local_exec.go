package toolruntime

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/runtimeenv"
	"github.com/fulcrus/hopclaw/skill"
)

type LocalExecConfig struct {
	DefaultTimeout      time.Duration
	InjectedEnvResolver func(pkg *skill.SkillPackage) (map[string]string, error)
}

type LocalExec struct {
	config LocalExecConfig
}

func NewLocalExec(cfg LocalExecConfig) *LocalExec {
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Second
	}
	return &LocalExec{config: cfg}
}

func (e *LocalExec) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	results := make([]contextengine.ToolResult, 0, len(calls))
	for _, call := range calls {
		result, err := e.executeOne(ctx, run, session, call)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (e *LocalExec) ResolveTool(session *agent.Session, name string) (*agent.ResolvedTool, bool) {
	if session == nil {
		return nil, false
	}
	bound, ok := session.SkillSnapshot.ResolveTool(name)
	if !ok {
		return nil, false
	}
	copied := bound
	return resolvedToolFromBinding(&copied, agent.ToolDefinition{
		Name:               copied.Manifest.Name,
		Description:        copied.Manifest.Description,
		InputSchema:        cloneSchema(copied.Manifest.InputSchema),
		OutputSchema:       cloneSchema(copied.Manifest.OutputSchema),
		SideEffectClass:    copied.Manifest.SideEffectClass,
		Idempotent:         copied.Manifest.Idempotent,
		RequiresApproval:   copied.Manifest.RequiresApproval,
		ExecutionKey:       copied.Manifest.ExecutionKey,
		Source:             "skill",
		SourceRef:          copied.Package.Source.Dir,
		Trust:              string(copied.Package.Trust),
		Eligible:           copied.Eligibility.Eligible,
		EligibilityReasons: append([]string(nil), copied.Eligibility.Reasons...),
		Availability:       availabilityFromEligibility(copied.Eligibility.Eligible, copied.Eligibility.Reasons),
	}, "local_exec"), true
}

func (e *LocalExec) executeOne(ctx context.Context, run *agent.Run, session *agent.Session, call agent.ToolCall) (contextengine.ToolResult, error) {
	resolved, ok := e.ResolveTool(session, call.Name)
	if !ok {
		return contextengine.ToolResult{}, fmt.Errorf("tool %q not found in session skill snapshot", call.Name)
	}
	bound := resolved.SkillBinding
	if bound == nil {
		return contextengine.ToolResult{}, fmt.Errorf("tool %q is missing skill binding", call.Name)
	}
	entry := strings.TrimSpace(bound.Manifest.Runtime.Entry)
	if entry == "" {
		return contextengine.ToolResult{}, fmt.Errorf("tool %q is missing runtime entry", call.Name)
	}
	if !filepath.IsAbs(entry) {
		entry = filepath.Join(bound.Package.Source.Dir, entry)
	}

	timeout := bound.Manifest.Timeout
	if timeout <= 0 {
		timeout = e.config.DefaultTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command, err := buildCommand(execCtx, bound.Manifest.Runtime.Shell, entry)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	command.Dir = bound.Package.Source.Dir
	extraEnv := map[string]string{
		"OPENCLAW_TOOL_NAME":    call.Name,
		"OPENCLAW_TOOL_CALL_ID": call.ID,
	}
	if run != nil {
		extraEnv["OPENCLAW_RUN_ID"] = run.ID
	}
	if session != nil {
		extraEnv["OPENCLAW_SESSION_ID"] = session.ID
	}
	injectedEnv := map[string]string(nil)
	if e.config.InjectedEnvResolver != nil {
		injectedEnv, err = e.config.InjectedEnvResolver(bound.Package)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("resolve injected env for %q: %w", call.Name, err)
		}
	}
	command.Env = runtimeenv.BuildChildEnv(
		runtimeenv.ModuleExecProfile,
		bound.Package.OpenClaw.Requires.Env,
		extraEnv,
		injectedEnv,
		sessionEnvOverlay(session, run),
	)

	inputPayload, err := encodeExecuteRequest(call, run, session)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	command.Stdin = bytes.NewReader(inputPayload)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return contextengine.ToolResult{}, fmt.Errorf("tool %q failed: %s", call.Name, message)
	}

	return decodeToolResultPayload(call, stdout.Bytes()), nil
}

func buildCommand(ctx context.Context, shell, entry string) (*exec.Cmd, error) {
	if strings.TrimSpace(shell) != "" {
		return exec.CommandContext(ctx, shell, entry), nil
	}
	return exec.CommandContext(ctx, entry), nil
}
