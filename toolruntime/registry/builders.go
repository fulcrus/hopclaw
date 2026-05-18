package registry

import (
	"context"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/internal/runtimeenv"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/mediagen"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolruntime"
)

const (
	providerOrderDefault           = 100
	configExternalToolModulePrefix = "config:tool:"
)

type BaseRuntimeResult struct {
	BuildResult
	Builtins *toolruntime.Builtins
	Layer2   *toolruntime.Layer2Registry
}

type toolProviderBuildState struct {
	builtins *toolruntime.Builtins
	layer2   *toolruntime.Layer2Registry
}

func BuildBase(ctx context.Context, cfg config.Config, capabilities *capregistry.Registry, artifactStore artifact.Store) (BaseRuntimeResult, error) {
	reg := New()
	state := &toolProviderBuildState{}
	registerBaseProviders(reg, cfg, capabilities, artifactStore, state)

	built, err := reg.Build(ctx)
	if err != nil {
		return BaseRuntimeResult{}, err
	}
	return BaseRuntimeResult{
		BuildResult: built,
		Builtins:    state.builtins,
		Layer2:      state.layer2,
	}, nil
}

func BuildRuntime(ctx context.Context, base agent.ToolExecutor, artifactStore artifact.Store, cfg config.Config, moduleCatalog *modules.Store, pluginMCP agent.ToolExecutor) (BuildResult, error) {
	reg := New()
	if base != nil {
		reg.Register(ProviderDescriptor{
			Name:   "base",
			Source: "runtime",
			Order:  providerOrderDefault,
			Build: func(context.Context) (ProviderInstance, error) {
				return ProviderInstance{
					Name:     "base",
					Source:   "runtime",
					Executor: base,
					Metadata: map[string]any{"kind": "base"},
				}, nil
			},
		})
	}
	if extTools := buildExternalTools(cfg.Tools.External, moduleCatalog); extTools != nil {
		reg.Register(ProviderDescriptor{
			Name:   "external",
			Source: "external",
			Order:  providerOrderDefault,
			After:  []string{"base"},
			Build: func(context.Context) (ProviderInstance, error) {
				return ProviderInstance{
					Name:     "external",
					Source:   "external",
					Executor: extTools,
					Metadata: map[string]any{"kind": "external"},
				}, nil
			},
		})
	}
	if pluginMCP != nil {
		reg.Register(ProviderDescriptor{
			Name:   "mcp",
			Source: "mcp",
			Order:  providerOrderDefault,
			After:  []string{"external"},
			Build: func(context.Context) (ProviderInstance, error) {
				return ProviderInstance{
					Name:     "mcp",
					Source:   "mcp",
					Executor: pluginMCP,
					Metadata: map[string]any{"kind": "mcp"},
				}, nil
			},
		})
	}

	built, err := reg.Build(ctx)
	if err != nil {
		return BuildResult{}, err
	}
	if built.Executor == nil {
		return BuildResult{}, nil
	}

	middlewares := []toolruntime.ToolMiddleware{
		toolruntime.MetricsMiddleware(),
		toolruntime.WithSideEffectBoundary(),
		toolruntime.WithEditShadow(),
	}
	if artifactStore != nil && enabledOrDefault(cfg.Runtime.Artifacts.Enabled, true) {
		middlewares = append(middlewares, func(next agent.ToolExecutor) agent.ToolExecutor {
			return toolruntime.NewArtifactingExecutor(next, artifactStore, toolruntime.ArtifactingConfig{
				InlineMaxBytes: cfg.Runtime.Artifacts.InlineThreshold,
				PreviewChars:   cfg.Runtime.Artifacts.PreviewChars,
			})
		})
	}
	built.Executor = toolruntime.Chain(built.Executor, middlewares...)
	return built, nil
}

func registerBaseProviders(reg *Registry, cfg config.Config, capabilities *capregistry.Registry, artifactStore artifact.Store, state *toolProviderBuildState) {
	if reg == nil {
		return
	}

	services := mapServicesConfig(cfg.Tools.Services)
	serviceTools := toolruntime.NewServicesExecutor(toolruntime.BuiltinsConfig{
		Root:               cfg.Tools.Builtins.Root,
		AllowedPaths:       cfg.Tools.Builtins.AllowedPaths,
		DefaultExecTimeout: cfg.Tools.Builtins.DefaultExecTimeout,
		MaxReadBytes:       cfg.Tools.Builtins.MaxReadBytes,
		Services:           services,
		FSConstraints:      cfg.Tools.Capabilities.FS,
		NetConstraints:     cfg.Tools.Capabilities.Net,
	})
	ensureBuiltinStack := func() (*toolruntime.Builtins, *toolruntime.Layer2Registry) {
		if state.builtins != nil && state.layer2 != nil {
			return state.builtins, state.layer2
		}
		includeServiceTools := false
		builtins := toolruntime.NewBuiltins(toolruntime.BuiltinsConfig{
			Root:               cfg.Tools.Builtins.Root,
			AllowedPaths:       cfg.Tools.Builtins.AllowedPaths,
			DefaultExecTimeout: cfg.Tools.Builtins.DefaultExecTimeout,
			MaxReadBytes:       cfg.Tools.Builtins.MaxReadBytes,
			RuntimeFacts: func(root string) skill.RuntimeContext {
				return runtimeenv.BuildRuntimeFacts(root, cfg)
			},
			Services:                services,
			SkillEnsureLimit:        cfg.Skills.EnsureLimit,
			ExecConstraints:         cfg.Tools.Capabilities.Exec,
			FSConstraints:           cfg.Tools.Capabilities.FS,
			NetConstraints:          cfg.Tools.Capabilities.Net,
			MediaGenerationRegistry: mediagen.BuildBuiltinRegistry(cfg.Models),
		})
		layer2 := toolruntime.NewLayer2Registry(toolruntime.Layer2Config{
			Root:                cfg.Tools.Builtins.Root,
			AllowedPaths:        cfg.Tools.Builtins.AllowedPaths,
			DefaultExecTimeout:  cfg.Tools.Builtins.DefaultExecTimeout,
			MaxReadBytes:        cfg.Tools.Builtins.MaxReadBytes,
			DisabledGroups:      buildDisabledGroups(cfg.Tools.Capabilities.Layer2),
			IncludeServiceTools: &includeServiceTools,
			Services:            services,
			FSConstraints:       cfg.Tools.Capabilities.FS,
			NetConstraints:      cfg.Tools.Capabilities.Net,
		})
		builtins.ApplyBindings(toolruntime.BuiltinsBindings{
			Layer2: layer2,
		})
		state.builtins = builtins
		state.layer2 = layer2
		return builtins, layer2
	}

	if enabledOrDefault(cfg.Tools.Builtins.Enabled, true) {
		reg.Register(ProviderDescriptor{
			Name:   "builtin",
			Source: "builtin",
			Order:  providerOrderDefault,
			After:  []string{"capability", "hostbridge"},
			Build: func(context.Context) (ProviderInstance, error) {
				builtins, _ := ensureBuiltinStack()
				return ProviderInstance{
					Name:     "builtin",
					Source:   "builtin",
					Executor: builtins,
					Metadata: map[string]any{"kind": "builtin"},
				}, nil
			},
		})
		reg.Register(ProviderDescriptor{
			Name:   "services",
			Source: "services",
			Order:  providerOrderDefault,
			After:  []string{"builtin"},
			Build: func(context.Context) (ProviderInstance, error) {
				return ProviderInstance{
					Name:     "services",
					Source:   "services",
					Executor: serviceTools,
					Metadata: map[string]any{"kind": "services"},
				}, nil
			},
		})
		operatorClient := toolruntime.NewOperatorClient()
		reg.Register(ProviderDescriptor{
			Name:   "operator",
			Source: "operator",
			Order:  providerOrderDefault,
			After:  []string{"layer2"},
			Build: func(context.Context) (ProviderInstance, error) {
				return ProviderInstance{
					Name:     "operator",
					Source:   "operator",
					Executor: operatorClient,
					Metadata: map[string]any{"kind": "operator"},
				}, nil
			},
		})
		reg.Register(ProviderDescriptor{
			Name:   "layer2",
			Source: "layer2",
			Order:  providerOrderDefault,
			After:  []string{"services"},
			Build: func(context.Context) (ProviderInstance, error) {
				_, layer2 := ensureBuiltinStack()
				return ProviderInstance{
					Name:     "layer2",
					Source:   "layer2",
					Executor: layer2,
					Metadata: map[string]any{"kind": "layer2"},
				}, nil
			},
		})
	}

	if enabledOrDefault(cfg.Tools.LocalExec.Enabled, true) {
		localExec := toolruntime.NewLocalExec(toolruntime.LocalExecConfig{
			DefaultTimeout: cfg.Tools.LocalExec.DefaultTimeout,
			InjectedEnvResolver: func(pkg *skill.SkillPackage) (map[string]string, error) {
				return runtimeenv.ResolveSkillInjectedEnv(cfg, pkg)
			},
		})
		reg.Register(ProviderDescriptor{
			Name:   "localexec",
			Source: "localexec",
			Order:  providerOrderDefault,
			After:  []string{"operator"},
			Build: func(context.Context) (ProviderInstance, error) {
				return ProviderInstance{
					Name:     "localexec",
					Source:   "localexec",
					Executor: localExec,
					Metadata: map[string]any{"kind": "localexec"},
				}, nil
			},
		})
	}

	if capabilityExecutor := toolruntime.NewCapabilityExecutor(capabilities, artifactStore); capabilityExecutor != nil {
		reg.Register(ProviderDescriptor{
			Name:   "capability",
			Source: "capability",
			Order:  providerOrderDefault,
			Build: func(context.Context) (ProviderInstance, error) {
				return ProviderInstance{
					Name:     "capability",
					Source:   "capability",
					Executor: capabilityExecutor,
					Metadata: map[string]any{"kind": "capability"},
				}, nil
			},
		})
	}
	if hostBridge := toolruntime.NewNodeCapabilityBridge(capabilities); hostBridge != nil {
		reg.Register(ProviderDescriptor{
			Name:   "hostbridge",
			Source: "capability_bridge",
			Order:  providerOrderDefault,
			After:  []string{"capability"},
			Before: []string{"builtin"},
			Build: func(context.Context) (ProviderInstance, error) {
				return ProviderInstance{
					Name:     "hostbridge",
					Source:   "capability_bridge",
					Executor: hostBridge,
					Metadata: map[string]any{"kind": "capability_bridge"},
				}, nil
			},
		})
	}
}

func buildDisabledGroups(l2 config.Layer2Config) map[string]bool {
	return toolruntime.DisabledGroupsFromConfig(l2)
}

func mapServicesConfig(cfg config.ServicesConfig) toolruntime.ServicesConfig {
	return toolruntime.ServicesConfig{
		Search: toolruntime.SearchServiceConfig{
			Provider: cfg.Search.Provider,
			APIKey:   cfg.Search.APIKey,
			BaseURL:  cfg.Search.BaseURL,
		},
		Email: toolruntime.EmailServiceConfig{
			SMTPHost: cfg.Email.SMTPHost,
			SMTPPort: cfg.Email.SMTPPort,
			Username: cfg.Email.Username,
			Password: cfg.Email.Password,
			From:     cfg.Email.From,
			IMAPHost: cfg.Email.IMAPHost,
			IMAPPort: cfg.Email.IMAPPort,
		},
		Speech: toolruntime.SpeechServiceConfig{
			BaseURL: cfg.Speech.BaseURL,
			APIKey:  cfg.Speech.APIKey,
			Model:   cfg.Speech.Model,
		},
		Calendar: toolruntime.CalendarServiceConfig{
			CalDAVURL: cfg.Calendar.CalDAVURL,
			Username:  cfg.Calendar.Username,
			Password:  cfg.Calendar.Password,
		},
	}
}

func buildExternalTools(cfgTools []config.ExternalToolConfig, moduleCatalog *modules.Store) *toolruntime.ExternalToolExecutor {
	var tools []toolruntime.ExternalToolConfig
	seen := make(map[string]struct{})
	useConfigModules := moduleCatalogHasConfigExternalToolModules(moduleCatalog)
	if !useConfigModules {
		for _, t := range cfgTools {
			name := strings.TrimSpace(t.Name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			timeout, _ := time.ParseDuration(t.Timeout)
			tools = append(tools, toolruntime.ExternalToolConfig{
				Name:        name,
				Description: t.Description,
				Endpoint:    t.Endpoint,
				Timeout:     timeout,
				InputSchema: t.InputSchema,
			})
		}
	}
	if moduleCatalog != nil {
		for _, projection := range moduleCatalog.ToolProjections() {
			name := strings.TrimSpace(projection.Name)
			if name == "" || strings.TrimSpace(projection.Endpoint) == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			tools = append(tools, toolruntime.ExternalToolConfig{
				Name:        name,
				Description: projection.Description,
				Endpoint:    projection.Endpoint,
				Timeout:     projection.Timeout,
				InputSchema: projection.InputSchema,
			})
		}
	}
	return toolruntime.NewExternalToolExecutor(tools)
}

func moduleCatalogHasConfigExternalToolModules(moduleCatalog *modules.Store) bool {
	if moduleCatalog == nil {
		return false
	}
	for _, projection := range moduleCatalog.ToolProjections() {
		if strings.HasPrefix(strings.TrimSpace(projection.ModuleID), configExternalToolModulePrefix) {
			return true
		}
	}
	return false
}

func enabledOrDefault(v *bool, fallback bool) bool {
	return normalize.BoolOrDefault(v, fallback)
}

func ProviderKinds(result BuildResult) []string {
	out := make([]string, 0, len(result.Providers))
	for _, provider := range result.Providers {
		kind, _ := provider.Metadata["kind"].(string)
		out = append(out, strings.TrimSpace(kind))
	}
	return out
}
