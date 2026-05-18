package bootstrap

import (
	"context"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/mcp"
	"github.com/fulcrus/hopclaw/toolruntime"
)

type mutablePluginMCPRuntime interface {
	pluginMCPRuntime
	AddServer(ctx context.Context, cfg mcp.ServerConfig) error
	RemoveServer(name string) error
	ServerConfigs() []mcp.ServerConfig
}

type preparedPluginMCPRuntime struct {
	runtime  pluginMCPRuntime
	executor agent.ToolExecutor
	release  func(context.Context)
	apply    func(context.Context) error
	commit   func(context.Context)
	rollback func(context.Context)
}

func preparePluginMCPRuntime(ctx context.Context, current pluginMCPRuntime, moduleCatalog *modules.Store) (*preparedPluginMCPRuntime, error) {
	configs := pluginMCPServerConfigs(moduleCatalog)
	if live, ok := current.(mutablePluginMCPRuntime); ok {
		return prepareIncrementalPluginMCPRuntime(live, configs), nil
	}

	runtime, executor, err := startPluginMCPWithConfigs(ctx, configs)
	return &preparedPluginMCPRuntime{
		runtime:  runtime,
		executor: executor,
		release: func(context.Context) {
			if runtime != nil && runtime != current {
				_ = runtime.Stop()
			}
		},
	}, err
}

func prepareIncrementalPluginMCPRuntime(runtime mutablePluginMCPRuntime, desired []mcp.ServerConfig) *preparedPluginMCPRuntime {
	prepared := &preparedPluginMCPRuntime{
		runtime:  runtime,
		executor: toolruntime.NewMCPExecutor(runtime),
	}
	if runtime == nil {
		return prepared
	}

	mutation := newPluginMCPMutation(runtime, runtime.ServerConfigs(), desired)
	prepared.apply = mutation.Apply
	prepared.commit = mutation.Commit
	prepared.rollback = mutation.Rollback
	return prepared
}

func pluginMCPServerConfigs(moduleCatalog *modules.Store) []mcp.ServerConfig {
	if moduleCatalog == nil {
		return nil
	}
	projections := moduleCatalog.MCPServerProjections()
	configs := make([]mcp.ServerConfig, 0, len(projections))
	for _, projection := range projections {
		if projection.Source != modules.SourcePlugin {
			continue
		}
		command := projection.ResolvedCommand()
		if command == "" {
			continue
		}
		configs = append(configs, mcp.ServerConfig{
			Name:    projection.RuntimeName(),
			Command: command,
			Args:    append([]string(nil), projection.Args...),
			Env:     cloneStringMap(projection.Env),
			WorkDir: projection.ResolvedWorkDir(),
		})
	}
	sort.Slice(configs, func(i, j int) bool {
		return strings.TrimSpace(configs[i].Name) < strings.TrimSpace(configs[j].Name)
	})
	return configs
}

func startPluginMCPWithConfigs(ctx context.Context, configs []mcp.ServerConfig) (pluginMCPRuntime, agent.ToolExecutor, error) {
	if len(configs) == 0 {
		return nil, nil, nil
	}
	runtime := newPluginMCPRuntime(configs)
	if runtime == nil {
		return nil, nil, nil
	}
	err := runtime.Start(ctx)
	return runtime, toolruntime.NewMCPExecutor(runtime), err
}

type pluginMCPMutation struct {
	runtime          mutablePluginMCPRuntime
	removals         []mcp.ServerConfig
	additions        []mcp.ServerConfig
	appliedRemovals  []mcp.ServerConfig
	appliedAdditions []mcp.ServerConfig
	applied          bool
}

func newPluginMCPMutation(runtime mutablePluginMCPRuntime, current, desired []mcp.ServerConfig) *pluginMCPMutation {
	currentByName := make(map[string]mcp.ServerConfig, len(current))
	for _, cfg := range current {
		name := strings.TrimSpace(cfg.Name)
		if name == "" {
			continue
		}
		currentByName[name] = cfg
	}

	desiredByName := make(map[string]mcp.ServerConfig, len(desired))
	for _, cfg := range desired {
		name := strings.TrimSpace(cfg.Name)
		if name == "" {
			continue
		}
		desiredByName[name] = cfg
	}

	removals := make([]mcp.ServerConfig, 0)
	for name, currentCfg := range currentByName {
		desiredCfg, ok := desiredByName[name]
		if !ok || !mcp.ServerConfigEqual(currentCfg, desiredCfg) {
			removals = append(removals, currentCfg)
		}
	}

	additions := make([]mcp.ServerConfig, 0)
	for name, desiredCfg := range desiredByName {
		currentCfg, ok := currentByName[name]
		if !ok || !mcp.ServerConfigEqual(currentCfg, desiredCfg) {
			additions = append(additions, desiredCfg)
		}
	}

	sort.Slice(removals, func(i, j int) bool {
		return strings.TrimSpace(removals[i].Name) < strings.TrimSpace(removals[j].Name)
	})
	sort.Slice(additions, func(i, j int) bool {
		return strings.TrimSpace(additions[i].Name) < strings.TrimSpace(additions[j].Name)
	})

	return &pluginMCPMutation{
		runtime:   runtime,
		removals:  removals,
		additions: additions,
	}
}

func (m *pluginMCPMutation) Apply(ctx context.Context) error {
	if m == nil || m.runtime == nil || m.applied {
		return nil
	}
	m.appliedRemovals = nil
	m.appliedAdditions = nil

	for _, cfg := range m.removals {
		if err := m.runtime.RemoveServer(cfg.Name); err != nil {
			m.rollbackApplied(ctx)
			return err
		}
		m.appliedRemovals = append(m.appliedRemovals, cfg)
	}
	for _, cfg := range m.additions {
		if err := m.runtime.AddServer(ctx, cfg); err != nil {
			m.rollbackApplied(ctx)
			return err
		}
		m.appliedAdditions = append(m.appliedAdditions, cfg)
	}

	m.applied = true
	return nil
}

func (m *pluginMCPMutation) Commit(context.Context) {
	if m == nil {
		return
	}
	m.appliedRemovals = nil
	m.appliedAdditions = nil
	m.applied = false
}

func (m *pluginMCPMutation) Rollback(ctx context.Context) {
	if m == nil || !m.applied {
		return
	}
	m.rollbackApplied(ctx)
}

func (m *pluginMCPMutation) rollbackApplied(ctx context.Context) {
	if m == nil || m.runtime == nil {
		return
	}

	for i := len(m.appliedAdditions) - 1; i >= 0; i-- {
		if err := m.runtime.RemoveServer(m.appliedAdditions[i].Name); err != nil {
			log.Warn("rollback added mcp server failed", "server", m.appliedAdditions[i].Name, "error", err)
		}
	}
	for i := len(m.appliedRemovals) - 1; i >= 0; i-- {
		if err := m.runtime.AddServer(ctx, m.appliedRemovals[i]); err != nil {
			log.Warn("restore removed mcp server failed", "server", m.appliedRemovals[i].Name, "error", err)
		}
	}

	m.appliedRemovals = nil
	m.appliedAdditions = nil
	m.applied = false
}
