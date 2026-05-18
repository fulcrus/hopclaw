package registry

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
	"github.com/fulcrus/hopclaw/channels/stdio"
	"github.com/fulcrus/hopclaw/channels/webhook"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/plugin"
)

var runtimeLog = logging.WithSubsystem("channels.registry")

type RuntimeDeps = registration.RuntimeDeps

type RuntimeBuildOptions struct {
	IncludeBuiltin bool
	IncludePlugins bool
}

type RuntimeBuildResult struct {
	Installations   []Installation
	WebhookAdapters map[string]*webhook.Adapter
	ProcessManager  *plugin.ProcessManager
}

type runtimeDescriptorState struct {
	webhookAdapters map[string]*webhook.Adapter
	processManager  *plugin.ProcessManager
}

func (s *runtimeDescriptorState) RememberWebhookAdapter(id string, adapter channels.Adapter) {
	if s == nil {
		return
	}
	webhookAdapter, ok := adapter.(*webhook.Adapter)
	if !ok {
		return
	}
	s.webhookAdapters[id] = webhookAdapter
}

func BuildRuntime(ctx context.Context, deps RuntimeDeps, opts RuntimeBuildOptions) (RuntimeBuildResult, error) {
	state := &runtimeDescriptorState{
		webhookAdapters: make(map[string]*webhook.Adapter),
	}
	reg := New()
	if opts.IncludeBuiltin {
		registerBuiltinChannelDescriptors(reg, deps, state)
	}
	if opts.IncludePlugins {
		registerPluginChannelDescriptors(reg, deps, state)
	}
	installations, err := reg.Build(ctx)
	if err != nil {
		return RuntimeBuildResult{}, err
	}
	return RuntimeBuildResult{
		Installations:   installations,
		WebhookAdapters: state.webhookAdapters,
		ProcessManager:  state.processManager,
	}, nil
}

func BuildAll(ctx context.Context, deps RuntimeDeps) (RuntimeBuildResult, error) {
	return BuildRuntime(ctx, deps, RuntimeBuildOptions{
		IncludeBuiltin: true,
		IncludePlugins: true,
	})
}

func BuildPlugins(ctx context.Context, deps RuntimeDeps) (RuntimeBuildResult, error) {
	return BuildRuntime(ctx, deps, RuntimeBuildOptions{
		IncludePlugins: true,
	})
}

// BuiltinRuntimeChannelConfigs returns the active builtin runtime channel names
// mapped to the config payload that produced each runtime adapter.
func BuiltinRuntimeChannelConfigs(cfg config.Config) map[string]any {
	reg := New()
	registerBuiltinChannelDescriptors(reg, RuntimeDeps{Channels: cfg.Channels, StorePath: cfg.Store.Path}, nil)
	return reg.RuntimeConfigs()
}

func PluginRuntimeChannelConfigs(moduleCatalog *modules.Store) map[string]any {
	specs := pluginChannelRuntimeSpecs(RuntimeDeps{ModuleCatalog: moduleCatalog})
	if len(specs) == 0 {
		return nil
	}
	runtimeConfigs := make(map[string]any, len(specs))
	for _, spec := range specs {
		runtimeConfigs[pluginChannelRuntimeName(spec)] = spec
	}
	return runtimeConfigs
}

func registerBuiltinChannelDescriptors(reg *Registry, deps RuntimeDeps, state *runtimeDescriptorState) {
	if reg == nil {
		return
	}
	for _, descriptor := range registration.BuiltinDescriptors(deps, state) {
		reg.Register(descriptor)
	}
}

func registerPluginChannelDescriptors(reg *Registry, deps RuntimeDeps, state *runtimeDescriptorState) {
	if reg == nil {
		return
	}
	for _, spec := range pluginChannelRuntimeSpecs(deps) {
		spec := spec
		switch spec.Type {
		case "webhook":
			reg.Register(Descriptor{
				Name:  "webhook:" + spec.Key,
				Order: 300,
				Build: func(context.Context) ([]Installation, error) {
					adapter := webhook.New(webhook.Config{ID: spec.Key, CallbackURL: spec.CallbackURL, Secret: spec.Secret})
					if err := deps.ChannelManager.Register("webhook:"+spec.Key, adapter); err != nil {
						runtimeLog.Warn("plugin channel register failed", "key", spec.Key, "error", err)
						return nil, nil
					}
					state.webhookAdapters[spec.Key] = adapter
					bridge := webhook.NewBridge(spec.Key, adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay)
					return []Installation{{
						Name:    "webhook:" + spec.Key,
						Adapter: adapter,
						Bridge:  bridge,
					}}, nil
				},
			})
		case "stdio":
			reg.Register(Descriptor{
				Name:  "plugin:" + spec.Key,
				Order: 300,
				Build: func(context.Context) ([]Installation, error) {
					if strings.TrimSpace(spec.Command) == "" {
						runtimeLog.Warn("plugin stdio channel missing command", "key", spec.Key)
						return nil, nil
					}
					command := resolvePluginChannelCommand(spec.ModuleDir, spec.Command)
					channelName := "plugin:" + spec.Key
					adapter := stdio.New(stdio.Config{
						Name:          channelName,
						Command:       command,
						Args:          append([]string(nil), spec.Args...),
						Env:           cloneStringMap(spec.Env),
						WorkDir:       spec.WorkDir,
						ChannelConfig: cloneAnyMap(spec.Config),
					})
					if err := deps.ChannelManager.Register(channelName, adapter); err != nil {
						runtimeLog.Warn("plugin stdio channel register failed", "key", spec.Key, "error", err)
						return nil, nil
					}
					bridge := channels.NewBridge(
						channels.BridgeConfig{ChannelName: channelName, TargetIDKey: "chat_id", MessageIDKey: "message_id"},
						adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay,
					)

					state.ensureProcessManager()
					processConfig := plugin.ProcessConfig{
						Name:        channelName,
						Command:     command,
						Args:        append([]string(nil), spec.Args...),
						Env:         cloneStringMap(spec.Env),
						WorkDir:     spec.WorkDir,
						MaxRestarts: spec.MaxRestarts,
						OnExit: func(err error) {
							if err != nil {
								runtimeLog.Warn("stdio plugin exited", "channel", channelName, "error", err)
							}
						},
					}
					adapterRef := adapter

					runtimeLog.Info("stdio plugin channel registered", "key", spec.Key, "command", command)
					return []Installation{{
						Name:    channelName,
						Adapter: adapter,
						Bridge:  bridge,
						ManagedProcess: &ManagedProcessPlan{
							Config: processConfig,
							Spawn: func(ctx context.Context) error {
								if err := adapterRef.Connect(ctx); err != nil {
									return err
								}
								<-ctx.Done()
								return adapterRef.Disconnect(context.Background())
							},
						},
					}}, nil
				},
			})
		}
	}
}

type pluginChannelRuntimeSpec struct {
	Key         string
	Type        string
	CallbackURL string
	Secret      string
	Command     string
	Args        []string
	Env         map[string]string
	WorkDir     string
	Config      map[string]any
	MaxRestarts int
	ModuleDir   string
}

func pluginChannelRuntimeSpecs(deps RuntimeDeps) []pluginChannelRuntimeSpec {
	if deps.ModuleCatalog == nil {
		return nil
	}
	projections := deps.ModuleCatalog.ChannelProjections()
	out := make([]pluginChannelRuntimeSpec, 0, len(projections))
	for _, projection := range projections {
		if projection.Source != modules.SourcePlugin {
			continue
		}
		key := strings.TrimSpace(projection.Name)
		if key == "" {
			continue
		}
		out = append(out, pluginChannelRuntimeSpec{
			Key:         key,
			Type:        projection.Type,
			CallbackURL: projection.CallbackURL,
			Secret:      projection.Secret,
			Command:     projection.Command,
			Args:        append([]string(nil), projection.Args...),
			Env:         cloneStringMap(projection.Env),
			WorkDir:     projection.WorkDir,
			Config:      cloneAnyMap(projection.Config),
			MaxRestarts: projection.MaxRestarts,
			ModuleDir:   projection.ModuleDir,
		})
	}
	return out
}

func pluginChannelRuntimeName(spec pluginChannelRuntimeSpec) string {
	key := strings.TrimSpace(spec.Key)
	if key == "" {
		return ""
	}
	switch strings.TrimSpace(spec.Type) {
	case "webhook":
		return "webhook:" + key
	case "stdio":
		return "plugin:" + key
	default:
		return ""
	}
}

func resolvePluginChannelCommand(moduleDir, command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if strings.TrimSpace(moduleDir) == "" {
		return command
	}
	return plugin.ResolveCommand(moduleDir, command)
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

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (s *runtimeDescriptorState) ensureProcessManager() *plugin.ProcessManager {
	if s.processManager == nil {
		s.processManager = plugin.NewProcessManager()
	}
	return s.processManager
}
