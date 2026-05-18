package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/mcp"
)

type stubMutablePluginMCPRuntime struct {
	mu          sync.Mutex
	configs     []mcp.ServerConfig
	addCalls    []string
	removeCalls []string
	failAdd     string
	failRemove  string
}

func newStubMutablePluginMCPRuntime(configs []mcp.ServerConfig) *stubMutablePluginMCPRuntime {
	return &stubMutablePluginMCPRuntime{configs: cloneStubMCPServerConfigs(configs)}
}

func (*stubMutablePluginMCPRuntime) Start(context.Context) error { return nil }

func (*stubMutablePluginMCPRuntime) Stop() error { return nil }

func (s *stubMutablePluginMCPRuntime) Tools() []mcp.Tool {
	s.mu.Lock()
	defer s.mu.Unlock()

	tools := make([]mcp.Tool, 0, len(s.configs))
	for _, cfg := range s.configs {
		tools = append(tools, mcp.Tool{
			Name:        strings.TrimSpace(cfg.Command),
			Description: strings.TrimSpace(cfg.Name),
			InputSchema: map[string]any{"type": "object"},
		})
	}
	return tools
}

func (*stubMutablePluginMCPRuntime) CallTool(context.Context, string, map[string]any) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{}, nil
}

func (s *stubMutablePluginMCPRuntime) AddServer(_ context.Context, cfg mcp.ServerConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg = cloneStubMCPServerConfig(cfg)
	if strings.TrimSpace(cfg.Name) == strings.TrimSpace(s.failAdd) {
		return fmt.Errorf("add %s", cfg.Name)
	}
	if indexStubMCPServerConfigByName(s.configs, cfg.Name) >= 0 {
		return fmt.Errorf("mcp server %q already exists", cfg.Name)
	}
	s.configs = insertStubMCPServerConfigSorted(s.configs, cfg)
	s.addCalls = append(s.addCalls, cfg.Name)
	return nil
}

func (s *stubMutablePluginMCPRuntime) RemoveServer(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if name == strings.TrimSpace(s.failRemove) {
		return fmt.Errorf("remove %s", name)
	}
	idx := indexStubMCPServerConfigByName(s.configs, name)
	if idx < 0 {
		return fmt.Errorf("mcp server %q not found", name)
	}
	s.configs = append(s.configs[:idx], s.configs[idx+1:]...)
	s.removeCalls = append(s.removeCalls, name)
	return nil
}

func (s *stubMutablePluginMCPRuntime) ServerConfigs() []mcp.ServerConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneStubMCPServerConfigs(s.configs)
}

func cloneStubMCPServerConfig(cfg mcp.ServerConfig) mcp.ServerConfig {
	return mcp.ServerConfig{
		Name:    strings.TrimSpace(cfg.Name),
		Command: strings.TrimSpace(cfg.Command),
		Args:    append([]string(nil), cfg.Args...),
		Env:     cloneStringMap(cfg.Env),
		WorkDir: strings.TrimSpace(cfg.WorkDir),
	}
}

func cloneStubMCPServerConfigs(configs []mcp.ServerConfig) []mcp.ServerConfig {
	if len(configs) == 0 {
		return nil
	}
	out := make([]mcp.ServerConfig, 0, len(configs))
	for _, cfg := range configs {
		out = append(out, cloneStubMCPServerConfig(cfg))
	}
	return out
}

func insertStubMCPServerConfigSorted(configs []mcp.ServerConfig, cfg mcp.ServerConfig) []mcp.ServerConfig {
	idx := 0
	for idx < len(configs) && strings.TrimSpace(configs[idx].Name) < strings.TrimSpace(cfg.Name) {
		idx++
	}
	configs = append(configs, mcp.ServerConfig{})
	copy(configs[idx+1:], configs[idx:])
	configs[idx] = cfg
	return configs
}

func indexStubMCPServerConfigByName(configs []mcp.ServerConfig, name string) int {
	name = strings.TrimSpace(name)
	for i, cfg := range configs {
		if strings.TrimSpace(cfg.Name) == name {
			return i
		}
	}
	return -1
}
