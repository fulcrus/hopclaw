package plugin

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"
)

var (
	ErrNilRuntime      = errors.New("plugin runtime is nil")
	ErrNotImplemented  = errors.New("plugin method is not implemented")
	ErrConfigKeyAbsent = errors.New("plugin config key is not set")
)

type Event struct {
	Name    string
	Payload map[string]any
	Time    time.Time
}

type PluginRuntime interface {
	Manifest() Manifest
	Config() map[string]any
	LookupEnv(key string) (string, bool)
	Emit(ctx context.Context, event Event) error
	Logf(format string, args ...any)
}

func ConfigValue(runtime PluginRuntime, key string) (any, error) {
	if runtime == nil {
		return nil, ErrNilRuntime
	}
	value, ok := runtime.Config()[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrConfigKeyAbsent, key)
	}
	return value, nil
}

func cloneManifest(src Manifest) Manifest {
	dst := src
	dst.ConfigSchema = cloneMapAny(src.ConfigSchema)
	dst.UIHints = maps.Clone(src.UIHints)
	dst.ProviderAuthEnvVars = cloneStringSlices(src.ProviderAuthEnvVars)
	dst.Providers = cloneProviders(src.Providers)
	dst.Channels = cloneChannels(src.Channels)
	dst.Tools = cloneTools(src.Tools)
	dst.SkillsDirs = slices.Clone(src.SkillsDirs)
	dst.MCPServers = cloneMCPServers(src.MCPServers)
	dst.Agents = cloneAgents(src.Agents)
	dst.Commands = slices.Clone(src.Commands)
	dst.UnsupportedProviders = slices.Clone(src.UnsupportedProviders)
	return dst
}

func cloneMapAny(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = cloneValue(value)
	}
	return dst
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMapAny(typed)
	case map[string]string:
		return maps.Clone(typed)
	case []string:
		return slices.Clone(typed)
	case []any:
		out := make([]any, len(typed))
		for idx := range typed {
			out[idx] = cloneValue(typed[idx])
		}
		return out
	default:
		return typed
	}
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	return maps.Clone(src)
}

func cloneStringSlices(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string][]string, len(src))
	for key, values := range src {
		dst[key] = slices.Clone(values)
	}
	return dst
}

func cloneProviders(src map[string]ProviderDecl) map[string]ProviderDecl {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]ProviderDecl, len(src))
	for key, value := range src {
		value.Headers = cloneStringMap(value.Headers)
		value.EnvVars = slices.Clone(value.EnvVars)
		dst[key] = value
	}
	return dst
}

func cloneChannels(src map[string]ChannelDecl) map[string]ChannelDecl {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]ChannelDecl, len(src))
	for key, value := range src {
		value.Args = slices.Clone(value.Args)
		value.Env = cloneStringMap(value.Env)
		value.Capabilities = slices.Clone(value.Capabilities)
		value.Config = cloneMapAny(value.Config)
		dst[key] = value
	}
	return dst
}

func cloneTools(src []ToolDecl) []ToolDecl {
	if len(src) == 0 {
		return nil
	}
	dst := make([]ToolDecl, len(src))
	for idx, value := range src {
		value.InputSchema = cloneMapAny(value.InputSchema)
		dst[idx] = value
	}
	return dst
}

func cloneMCPServers(src map[string]MCPServerDecl) map[string]MCPServerDecl {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]MCPServerDecl, len(src))
	for key, value := range src {
		value.Env = cloneStringMap(value.Env)
		value.Args = slices.Clone(value.Args)
		dst[key] = value
	}
	return dst
}

func cloneAgents(src map[string]AgentDecl) map[string]AgentDecl {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]AgentDecl, len(src))
	for key, value := range src {
		value.Tools = slices.Clone(value.Tools)
		value.Skills = slices.Clone(value.Skills)
		dst[key] = value
	}
	return dst
}
