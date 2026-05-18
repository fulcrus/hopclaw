package mcp

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Manager timeouts
// ---------------------------------------------------------------------------

const (
	// initializeTimeout is the maximum time allowed for each MCP server
	// to complete its initialize handshake.
	initializeTimeout = 30 * time.Second

	// callToolTimeout is the default timeout for a single tool invocation.
	callToolTimeout = 120 * time.Second

	// toolNameSeparator separates the server name prefix from the tool name.
	toolNameSeparator = "__"
)

var newManagerClient = NewClient

// Manager manages the lifecycle of multiple MCP server connections and
// provides a unified interface for discovering and calling tools across
// all connected servers.
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*Client // server name -> client
	configs []ServerConfig
	errors  map[string]string // server name -> last error
}

// NewManager creates a Manager for the given server configurations.
func NewManager(configs []ServerConfig) *Manager {
	return &Manager{
		configs: cloneServerConfigs(configs),
		clients: make(map[string]*Client),
		errors:  make(map[string]string),
	}
}

// Start spawns all configured MCP servers, performs the initialize handshake
// on each, and lists their tools. Returns an error if any server fails to
// start.
func (m *Manager) Start(ctx context.Context) error {
	var errs []string
	for _, cfg := range m.ServerConfigs() {
		if err := m.startOne(ctx, cfg); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", cfg.Name, err))
			m.mu.Lock()
			m.errors[cfg.Name] = err.Error()
			m.mu.Unlock()
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to start mcp servers: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) startOne(ctx context.Context, cfg ServerConfig) error {
	cfg = cloneServerConfig(cfg)
	client, err := newManagerClient(cfg)
	if err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("mcp server %q returned nil client", cfg.Name)
	}

	initCtx, cancel := context.WithTimeout(ctx, initializeTimeout)
	defer cancel()

	if _, err := client.Initialize(initCtx); err != nil {
		client.Close()
		return fmt.Errorf("initialize: %w", err)
	}

	if _, err := client.ListTools(initCtx); err != nil {
		client.Close()
		return fmt.Errorf("list tools: %w", err)
	}

	m.mu.Lock()
	delete(m.errors, cfg.Name)
	m.clients[cfg.Name] = client
	m.mu.Unlock()

	return nil
}

func (m *Manager) AddServer(ctx context.Context, cfg ServerConfig) error {
	cfg = cloneServerConfig(cfg)
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		return fmt.Errorf("mcp server name is required")
	}

	m.mu.RLock()
	_, clientExists := m.clients[cfg.Name]
	configIdx := indexServerConfigByName(m.configs, cfg.Name)
	if clientExists || configIdx >= 0 {
		m.mu.RUnlock()
		return fmt.Errorf("mcp server %q already exists", cfg.Name)
	}
	m.mu.RUnlock()

	client, err := newManagerClient(cfg)
	if err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("mcp server %q returned nil client", cfg.Name)
	}

	initCtx, cancel := context.WithTimeout(ctx, initializeTimeout)
	defer cancel()

	if _, err := client.Initialize(initCtx); err != nil {
		_ = client.Close()
		return fmt.Errorf("initialize: %w", err)
	}
	if _, err := client.ListTools(initCtx); err != nil {
		_ = client.Close()
		return fmt.Errorf("list tools: %w", err)
	}

	m.mu.Lock()
	if _, exists := m.clients[cfg.Name]; exists || indexServerConfigByName(m.configs, cfg.Name) >= 0 {
		m.mu.Unlock()
		_ = client.Close()
		return fmt.Errorf("mcp server %q already exists", cfg.Name)
	}
	m.clients[cfg.Name] = client
	m.configs = insertServerConfigSorted(m.configs, cfg)
	delete(m.errors, cfg.Name)
	m.mu.Unlock()
	return nil
}

func (m *Manager) RemoveServer(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	m.mu.Lock()
	client := m.clients[name]
	delete(m.clients, name)
	delete(m.errors, name)
	if idx := indexServerConfigByName(m.configs, name); idx >= 0 {
		m.configs = append(m.configs[:idx], m.configs[idx+1:]...)
	}
	m.mu.Unlock()

	if client != nil {
		if err := client.Close(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func (m *Manager) ServerConfigs() []ServerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneServerConfigs(m.configs)
}

// Stop closes all connected MCP servers gracefully.
func (m *Manager) Stop() error {
	m.mu.Lock()
	clients := make(map[string]*Client, len(m.clients))
	for k, v := range m.clients {
		clients[k] = v
	}
	m.clients = make(map[string]*Client)
	m.mu.Unlock()

	var errs []string
	for name, client := range clients {
		if err := client.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors stopping mcp servers: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Tools returns an aggregated list of tools from all connected servers.
// Each tool name is prefixed with the server name: "servername__toolname".
func (m *Manager) Tools() []Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []Tool
	for serverName, client := range m.clients {
		for _, tool := range client.toolSnapshot() {
			prefixed := Tool{
				Name:        serverName + toolNameSeparator + tool.Name,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
			}
			all = append(all, prefixed)
		}
	}
	return all
}

// CallTool routes a tool call to the correct MCP server based on the tool
// name prefix. The name format is "servername__toolname".
func (m *Manager) CallTool(ctx context.Context, name string, args map[string]any) (*CallToolResult, error) {
	serverName, toolName, err := m.parseToolName(name)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	client, ok := m.clients[serverName]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("mcp server %q not found", serverName)
	}

	callCtx, cancel := context.WithTimeout(ctx, callToolTimeout)
	defer cancel()

	return client.CallTool(callCtx, toolName, args)
}

// Client returns the Client for a specific server by name.
func (m *Manager) Client(name string) (*Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.clients[name]
	return c, ok
}

// Status returns the connection status of each configured server.
func (m *Manager) Status() map[string]ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]ServerStatus, len(m.configs))
	for _, cfg := range m.configs {
		client, ok := m.clients[cfg.Name]
		if !ok {
			status[cfg.Name] = ServerStatus{
				Connected: false,
				Error:     m.errors[cfg.Name],
			}
			continue
		}
		status[cfg.Name] = client.statusSnapshot()
	}
	return status
}

func cloneServerConfig(cfg ServerConfig) ServerConfig {
	return ServerConfig{
		Name:    strings.TrimSpace(cfg.Name),
		Command: strings.TrimSpace(cfg.Command),
		Args:    append([]string(nil), cfg.Args...),
		Env:     cloneStringMap(cfg.Env),
		WorkDir: strings.TrimSpace(cfg.WorkDir),
	}
}

func cloneServerConfigs(configs []ServerConfig) []ServerConfig {
	if len(configs) == 0 {
		return nil
	}
	out := make([]ServerConfig, 0, len(configs))
	for _, cfg := range configs {
		out = append(out, cloneServerConfig(cfg))
	}
	return out
}

func insertServerConfigSorted(configs []ServerConfig, cfg ServerConfig) []ServerConfig {
	idx := 0
	for idx < len(configs) && strings.TrimSpace(configs[idx].Name) < cfg.Name {
		idx++
	}
	configs = append(configs, ServerConfig{})
	copy(configs[idx+1:], configs[idx:])
	configs[idx] = cfg
	return configs
}

func indexServerConfigByName(configs []ServerConfig, name string) int {
	name = strings.TrimSpace(name)
	for i, cfg := range configs {
		if strings.TrimSpace(cfg.Name) == name {
			return i
		}
	}
	return -1
}

func serverConfigMapsEqual(a, b map[string]string) bool {
	return reflect.DeepEqual(a, b)
}

func ServerConfigEqual(a, b ServerConfig) bool {
	a = cloneServerConfig(a)
	b = cloneServerConfig(b)
	return a.Name == b.Name &&
		a.Command == b.Command &&
		reflect.DeepEqual(a.Args, b.Args) &&
		serverConfigMapsEqual(a.Env, b.Env) &&
		a.WorkDir == b.WorkDir
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// parseToolName splits a prefixed tool name into server name and bare tool name.
// It also handles the case where the tool name has no prefix by searching for
// an unambiguous match across all servers.
func (m *Manager) parseToolName(name string) (string, string, error) {
	if idx := strings.Index(name, toolNameSeparator); idx > 0 {
		return name[:idx], name[idx+len(toolNameSeparator):], nil
	}

	// No prefix: search for an unambiguous match.
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matches []string
	for serverName, client := range m.clients {
		for _, tool := range client.toolSnapshot() {
			if tool.Name == name {
				matches = append(matches, serverName)
			}
		}
	}

	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("tool %q not found on any mcp server", name)
	case 1:
		return matches[0], name, nil
	default:
		return "", "", fmt.Errorf("tool %q is ambiguous, found on servers: %s", name, strings.Join(matches, ", "))
	}
}
