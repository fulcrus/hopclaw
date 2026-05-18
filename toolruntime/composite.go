package toolruntime

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
)

type CompositeConfig struct {
	MaxParallel int
}

type Composite struct {
	config    CompositeConfig
	executors []agent.ToolExecutor
}

type earlyResult struct {
	index  int
	result contextengine.ToolResult
}

type plannedCall struct {
	index        int
	call         agent.ToolCall
	executor     agent.ToolExecutor
	resolved     *agent.ResolvedTool
	readOnly     bool
	executionKey string
}

type executionGroup struct {
	parallel bool
	items    []plannedCall
}

func NewComposite(cfg CompositeConfig, executors ...agent.ToolExecutor) *Composite {
	filtered := make([]agent.ToolExecutor, 0, len(executors))
	for _, executor := range executors {
		if executor != nil {
			filtered = append(filtered, executor)
		}
	}
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = 4
	}
	return &Composite{
		config:    cfg,
		executors: filtered,
	}
}

func (c *Composite) ToolDefinitions(session *agent.Session) []agent.ToolDefinition {
	if len(c.executors) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var out []agent.ToolDefinition
	for _, executor := range c.executors {
		provider, ok := executor.(agent.ToolDefinitionProvider)
		if !ok {
			continue
		}
		for _, definition := range provider.ToolDefinitions(session) {
			key := strings.TrimSpace(definition.Name)
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, copyToolDefinition(definition))
		}
	}
	return out
}

func (c *Composite) ResolveTool(session *agent.Session, name string) (*agent.ResolvedTool, bool) {
	for _, executor := range c.executors {
		resolver, ok := executor.(agent.ToolResolver)
		if !ok {
			continue
		}
		if resolved, ok := resolver.ResolveTool(session, name); ok {
			return resolved, true
		}
	}
	return nil, false
}

func (c *Composite) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	// Intercept tool calls with malformed arguments before planning/execution.
	// Return an error result to the model so it can retry with valid JSON.
	var cleanCalls []agent.ToolCall
	var earlyResults []earlyResult
	for i, call := range calls {
		if parseErr, ok := call.Input["_parse_error"].(string); ok {
			earlyResults = append(earlyResults, earlyResult{
				index: i,
				result: contextengine.ToolResult{
					ToolCallID: call.ID,
					ToolName:   call.Name,
					Status:     resultmodel.ToolResultError,
					Content:    "error: " + parseErr + ". Please retry with valid JSON arguments.",
				},
			})
		} else {
			cleanCalls = append(cleanCalls, call)
		}
	}
	if len(cleanCalls) == 0 && len(earlyResults) > 0 {
		results := make([]contextengine.ToolResult, len(calls))
		for _, er := range earlyResults {
			results[er.index] = er.result
		}
		return results, nil
	}
	if len(earlyResults) > 0 {
		// Mix early error results with normal execution results.
		allResults := make([]contextengine.ToolResult, len(calls))
		for _, er := range earlyResults {
			allResults[er.index] = er.result
		}
		planned, err := c.plan(run, session, cleanCalls)
		if err != nil {
			return nil, err
		}
		groups := groupPlannedCalls(planned)
		cleanResults := make([]contextengine.ToolResult, len(cleanCalls))
		for _, group := range groups {
			if group.parallel && len(group.items) > 1 {
				if err := c.executeParallel(ctx, run, session, cleanResults, group.items); err != nil {
					return nil, err
				}
				continue
			}
			if err := c.executeSerial(ctx, run, session, cleanResults, group.items); err != nil {
				return nil, err
			}
		}
		ci := 0
		for i := range allResults {
			if allResults[i].ToolCallID == "" {
				allResults[i] = cleanResults[ci]
				ci++
			}
		}
		return allResults, nil
	}
	planned, err := c.plan(run, session, calls)
	if err != nil {
		return nil, err
	}
	groups := groupPlannedCalls(planned)
	results := make([]contextengine.ToolResult, len(calls))
	for _, group := range groups {
		if group.parallel && len(group.items) > 1 {
			if err := c.executeParallel(ctx, run, session, results, group.items); err != nil {
				return nil, err
			}
			continue
		}
		if err := c.executeSerial(ctx, run, session, results, group.items); err != nil {
			return nil, err
		}
	}
	return results, nil
}

func (c *Composite) plan(run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]plannedCall, error) {
	out := make([]plannedCall, 0, len(calls))
	for idx, call := range calls {
		executor, resolved, err := c.resolveExecution(session, call.Name)
		if err != nil {
			return nil, err
		}
		manifest := skill.ToolManifest{}
		if resolved != nil {
			manifest = resolved.Manifest
		}
		sideEffect := skill.NormalizeSideEffectClass(manifest.SideEffectClass)
		key := renderExecutionKey(manifest.ExecutionKey, run, session, call)
		if key == "" {
			key = "session:" + session.ID
		}
		out = append(out, plannedCall{
			index:        idx,
			call:         call,
			executor:     executor,
			resolved:     resolved,
			readOnly:     sideEffect == "read",
			executionKey: key,
		})
	}
	return out, nil
}

func (c *Composite) resolveExecution(session *agent.Session, name string) (agent.ToolExecutor, *agent.ResolvedTool, error) {
	for _, executor := range c.executors {
		resolver, ok := executor.(agent.ToolResolver)
		if !ok {
			continue
		}
		if resolved, ok := resolver.ResolveTool(session, name); ok {
			return executor, resolved, nil
		}
	}
	return nil, nil, fmt.Errorf("tool %q is not registered", name)
}

func groupPlannedCalls(planned []plannedCall) []executionGroup {
	if len(planned) == 0 {
		return nil
	}
	var groups []executionGroup
	var current executionGroup
	seenKeys := make(map[string]struct{})
	flush := func() {
		if len(current.items) == 0 {
			return
		}
		groups = append(groups, current)
		current = executionGroup{}
		seenKeys = make(map[string]struct{})
	}

	for _, item := range planned {
		if item.readOnly {
			if !current.parallel {
				flush()
				current = executionGroup{parallel: true}
			}
			if _, exists := seenKeys[item.executionKey]; exists {
				flush()
				current = executionGroup{parallel: true}
			}
			current.items = append(current.items, item)
			seenKeys[item.executionKey] = struct{}{}
			continue
		}
		flush()
		groups = append(groups, executionGroup{
			parallel: false,
			items:    []plannedCall{item},
		})
	}
	flush()
	return groups
}

func (c *Composite) executeSerial(ctx context.Context, run *agent.Run, session *agent.Session, results []contextengine.ToolResult, items []plannedCall) error {
	for _, item := range items {
		batch, err := item.executor.ExecuteBatch(ctx, run, session, []agent.ToolCall{item.call})
		if err != nil {
			return err
		}
		if len(batch) != 1 {
			return fmt.Errorf("tool %q returned %d results, want 1", item.call.Name, len(batch))
		}
		results[item.index] = batch[0]
	}
	return nil
}

func (c *Composite) executeParallel(ctx context.Context, run *agent.Run, session *agent.Session, results []contextengine.ToolResult, items []plannedCall) error {
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, c.config.MaxParallel)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	for _, item := range items {
		wg.Add(1)
		go func(item plannedCall) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if execCtx.Err() != nil {
				return
			}
			batch, err := item.executor.ExecuteBatch(execCtx, run, session, []agent.ToolCall{item.call})
			if err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			if len(batch) != 1 {
				once.Do(func() {
					firstErr = fmt.Errorf("tool %q returned %d results, want 1", item.call.Name, len(batch))
					cancel()
				})
				return
			}
			results[item.index] = batch[0]
		}(item)
	}
	wg.Wait()
	return firstErr
}

var executionTokenPattern = regexp.MustCompile(`\{([^{}]+)\}`)

func renderExecutionKey(template string, run *agent.Run, session *agent.Session, call agent.ToolCall) string {
	template = strings.TrimSpace(template)
	if template == "" {
		return ""
	}
	return executionTokenPattern.ReplaceAllStringFunc(template, func(match string) string {
		token := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{"), "}"))
		if token == "" {
			return ""
		}
		return executionTokenValue(token, run, session, call)
	})
}

func executionTokenValue(token string, run *agent.Run, session *agent.Session, call agent.ToolCall) string {
	switch strings.ToLower(token) {
	case "id", "session.id":
		if session != nil {
			return session.ID
		}
	case "run.id":
		if run != nil {
			return run.ID
		}
	case "tool.name":
		return call.Name
	}
	key := token
	if strings.HasPrefix(strings.ToLower(token), "input.") {
		key = token[len("input."):]
	}
	if call.Input == nil {
		return ""
	}
	value, ok := call.Input[key]
	if !ok {
		return ""
	}
	text := fmt.Sprintf("%v", value)
	return strings.TrimSpace(text)
}
