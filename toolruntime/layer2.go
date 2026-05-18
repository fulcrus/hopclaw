package toolruntime

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
)

// Layer2Registry manages tools that depend on external binaries.
// Tools start dormant and become active when their required binaries are found.
type Layer2Registry struct {
	ws
	config         BuiltinsConfig
	disabledGroups map[string]bool // group name → disabled via config

	mu     sync.RWMutex
	groups []*toolGroup
	active map[string]*toolGroupEntry // tool name → entry (active only)
	all    map[string]*toolGroupEntry // tool name → entry (all)
}

type toolGroup struct {
	name         string
	requiredBins []string
	isActive     bool
	isDisabled   bool              // disabled by config — never activates
	binPaths     map[string]string // bin name → resolved path
	entries      []*toolGroupEntry
}

type toolGroupEntry struct {
	group    *toolGroup
	Manifest skill.ToolManifest
	bound    skill.BoundTool
	defn     agent.ToolDefinition
	execFn   layer2ExecFunc
}

type layer2ExecFunc func(ctx context.Context, w *ws, config BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error)

// Layer2Config configures the Layer 2 registry.
type Layer2Config struct {
	Root                string
	AllowedPaths        []string
	DefaultExecTimeout  time.Duration
	MaxReadBytes        int
	DisabledGroups      map[string]bool // group name → true means disabled via config
	IncludeServiceTools *bool
	Services            ServicesConfig
	FSConstraints       config.FSConstraints
	NetConstraints      config.NetConstraints
}

// NewLayer2Registry creates a Layer 2 registry. Tools start dormant.
// Call Probe() to activate eligible tools.
func NewLayer2Registry(cfg Layer2Config) *Layer2Registry {
	builtinsCfg := BuiltinsConfig{
		Root:               cfg.Root,
		AllowedPaths:       cfg.AllowedPaths,
		DefaultExecTimeout: cfg.DefaultExecTimeout,
		MaxReadBytes:       cfg.MaxReadBytes,
		Services:           cfg.Services,
		FSConstraints:      cfg.FSConstraints,
		NetConstraints:     cfg.NetConstraints,
	}
	if builtinsCfg.DefaultExecTimeout <= 0 {
		builtinsCfg.DefaultExecTimeout = 30 * time.Second
	}
	includeServiceTools := true
	if cfg.IncludeServiceTools != nil {
		includeServiceTools = *cfg.IncludeServiceTools
	}
	disabled := cfg.DisabledGroups
	if disabled == nil {
		disabled = make(map[string]bool)
	}
	r := &Layer2Registry{
		ws:             newWorkspace(cfg.Root),
		config:         builtinsCfg,
		disabledGroups: disabled,
		active:         make(map[string]*toolGroupEntry),
		all:            make(map[string]*toolGroupEntry),
	}
	allowedPaths := make([]string, 0, len(cfg.AllowedPaths)+len(cfg.FSConstraints.AllowedRoots))
	allowedPaths = append(allowedPaths, cfg.AllowedPaths...)
	allowedPaths = append(allowedPaths, cfg.FSConstraints.AllowedRoots...)
	if len(allowedPaths) > 0 {
		r.ws.setAllowedPaths(allowedPaths)
	}
	r.ws.setDenyPatterns(cfg.FSConstraints.DenyPatterns)
	r.registerGitGroup()
	r.registerGitWriteGroup()
	r.registerPkgGroup()
	r.registerContainerGroup()
	// Browser automation remains a roadmap item until the CDP client lands.
	r.registerMediaGroup()
	r.registerMediaGoGroup()
	if includeServiceTools {
		r.registerSearchGroup()
		r.registerSpeechGroup()
		r.registerEmailGroup()
		r.registerCalendarGroup()
	}
	// session/memory tools are now L1 builtins (builtin_env.go), not L2.
	r.Probe()
	return r
}

// registerGroup adds a tool group to the registry.
func (r *Layer2Registry) registerGroup(name string, requiredBins []string, tools []layer2ToolDef) {
	pkg := &skill.SkillPackage{
		ID:     "layer2-" + name,
		Kind:   skill.SkillKindExecutable,
		Status: skill.StatusReady,
		Prompt: skill.PromptSkill{
			Name:        "layer2-" + name,
			Description: fmt.Sprintf("Layer 2 %s tools (requires: %v)", name, requiredBins),
			Location:    "builtin:layer2:" + name,
		},
		Source: skill.SkillSource{
			Kind: skill.SourceBundled,
			Dir:  r.rootAbs,
			Root: r.rootAbs,
		},
		Trust: skill.TrustBundled,
	}

	g := &toolGroup{
		name:         name,
		requiredBins: requiredBins,
		isDisabled:   r.disabledGroups[name],
		binPaths:     make(map[string]string),
	}

	for _, t := range tools {
		bound := skill.BoundTool{
			Package: pkg,
			Manifest: skill.ToolManifest{
				Name:             t.manifest.Name,
				Description:      t.manifest.Description,
				InputSchema:      cloneSchema(t.manifest.InputSchema),
				OutputSchema:     cloneSchema(t.manifest.OutputSchema),
				SideEffectClass:  t.manifest.SideEffectClass,
				Idempotent:       t.manifest.Idempotent,
				RequiresApproval: t.manifest.RequiresApproval,
				ExecutionKey:     t.manifest.ExecutionKey,
				Timeout:          t.manifest.Timeout,
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		}
		defn := agent.ToolDefinition{
			Name:             t.manifest.Name,
			Description:      t.manifest.Description,
			InputSchema:      cloneSchema(t.manifest.InputSchema),
			OutputSchema:     cloneSchema(t.manifest.OutputSchema),
			SideEffectClass:  t.manifest.SideEffectClass,
			Idempotent:       t.manifest.Idempotent,
			RequiresApproval: t.manifest.RequiresApproval,
			ExecutionKey:     t.manifest.ExecutionKey,
			Source:           "layer2",
			SourceRef:        "layer2:" + name,
			Trust:            string(skill.TrustBundled),
			Eligible:         true,
			Availability: agent.ToolAvailability{
				Status: agent.AvailabilityReady,
			},
		}

		entry := &toolGroupEntry{
			group:    g,
			Manifest: t.manifest,
			bound:    bound,
			defn:     defn,
			execFn:   t.execFn,
		}
		g.entries = append(g.entries, entry)
		r.all[t.manifest.Name] = entry
	}

	r.groups = append(r.groups, g)
}

type layer2ToolDef struct {
	manifest skill.ToolManifest
	execFn   layer2ExecFunc
}

// Probe checks which groups have their required binaries available
// and activates/deactivates them accordingly. Returns groups that changed state.
func (r *Layer2Registry) Probe() (activated, deactivated []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, g := range r.groups {
		if g.isDisabled {
			if g.isActive {
				// Was active but now disabled by config.
				g.isActive = false
				deactivated = append(deactivated, g.name)
				for _, e := range g.entries {
					delete(r.active, e.Manifest.Name)
				}
			}
			continue
		}
		wasActive := g.isActive
		allFound := true
		for _, bin := range g.requiredBins {
			path, err := exec.LookPath(bin)
			if err != nil {
				allFound = false
				g.binPaths[bin] = ""
			} else {
				g.binPaths[bin] = path
			}
		}
		g.isActive = allFound

		if g.isActive && !wasActive {
			activated = append(activated, g.name)
			for _, e := range g.entries {
				r.active[e.Manifest.Name] = e
			}
		} else if !g.isActive && wasActive {
			deactivated = append(deactivated, g.name)
			for _, e := range g.entries {
				delete(r.active, e.Manifest.Name)
			}
		}
	}
	return
}

// GroupStatus returns the status of all registered groups.
type GroupStatus struct {
	Name      string            `json:"name"`
	Active    bool              `json:"active"`
	Tools     int               `json:"tools"`
	Required  []string          `json:"required_bins"`
	BinPaths  map[string]string `json:"bin_paths,omitempty"`
	ToolNames []string          `json:"tool_names"`
}

func (r *Layer2Registry) GroupStatuses() []GroupStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]GroupStatus, 0, len(r.groups))
	for _, g := range sortedToolGroups(r.groups) {
		names := make([]string, 0, len(g.entries))
		for _, e := range sortedToolGroupEntries(g.entries) {
			names = append(names, e.Manifest.Name)
		}
		required := append([]string(nil), g.requiredBins...)
		sort.Strings(required)
		binPaths := make(map[string]string, len(g.binPaths))
		for key, value := range g.binPaths {
			binPaths[key] = value
		}
		out = append(out, GroupStatus{
			Name:      g.name,
			Active:    g.isActive,
			Tools:     len(g.entries),
			Required:  required,
			BinPaths:  binPaths,
			ToolNames: names,
		})
	}
	return out
}

// --- ToolExecutor / ToolDefinitionProvider / ToolResolver ---

func (r *Layer2Registry) ToolDefinitions(_ *agent.Session) []agent.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []agent.ToolDefinition
	for _, g := range sortedToolGroups(r.groups) {
		for _, e := range sortedToolGroupEntries(g.entries) {
			definition := copyToolDefinition(e.defn)
			if !g.isActive {
				definition.Eligible = false
				definition.EligibilityReasons = missingGroupBins(g)
				definition.Availability = agent.ToolAvailability{
					Status:       agent.AvailabilityBlocked,
					Reasons:      append([]string(nil), definition.EligibilityReasons...),
					InstallHints: buildLayer2InstallHints(g),
				}
			}
			out = append(out, definition)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		if out[i].SourceRef != out[j].SourceRef {
			return out[i].SourceRef < out[j].SourceRef
		}
		return out[i].ExecutionKey < out[j].ExecutionKey
	})
	return out
}

func sortedToolGroups(groups []*toolGroup) []*toolGroup {
	if len(groups) == 0 {
		return nil
	}
	out := append([]*toolGroup(nil), groups...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].name < out[j].name
	})
	return out
}

func sortedToolGroupEntries(entries []*toolGroupEntry) []*toolGroupEntry {
	if len(entries) == 0 {
		return nil
	}
	out := append([]*toolGroupEntry(nil), entries...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Manifest.Name < out[j].Manifest.Name
	})
	return out
}

func (r *Layer2Registry) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	trimmed := strings.TrimSpace(name)
	if e, ok := r.active[trimmed]; ok {
		copied := e.bound
		copied.Manifest.InputSchema = cloneSchema(e.bound.Manifest.InputSchema)
		copied.Manifest.OutputSchema = cloneSchema(e.bound.Manifest.OutputSchema)
		return resolvedToolFromBinding(&copied, e.defn, "layer2:"+e.group.name), true
	}
	if e, ok := r.all[trimmed]; ok {
		copied := e.bound
		copied.Manifest.InputSchema = cloneSchema(e.bound.Manifest.InputSchema)
		copied.Manifest.OutputSchema = cloneSchema(e.bound.Manifest.OutputSchema)
		copied.Eligibility = skill.EligibilityResult{
			Eligible: false,
			Reasons:  missingGroupBins(e.group),
		}
		definition := copyToolDefinition(e.defn)
		definition.Eligible = false
		definition.EligibilityReasons = append([]string(nil), copied.Eligibility.Reasons...)
		definition.Availability = agent.ToolAvailability{
			Status:       agent.AvailabilityBlocked,
			Reasons:      append([]string(nil), copied.Eligibility.Reasons...),
			InstallHints: buildLayer2InstallHints(e.group),
		}
		return resolvedToolFromBinding(&copied, definition, "layer2:"+e.group.name), true
	}
	return nil, false
}

func (r *Layer2Registry) ExecuteBatch(ctx context.Context, _ *agent.Run, _ *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	results := make([]contextengine.ToolResult, 0, len(calls))
	for _, call := range calls {
		// Intercept malformed arguments the same way Composite does.
		if parseErr, ok := call.Input["_parse_error"].(string); ok {
			results = append(results, contextengine.ToolResult{
				ToolCallID: call.ID,
				ToolName:   call.Name,
				Status:     "error",
				Content:    "error: " + parseErr + ". Please retry with valid JSON arguments.",
			})
			continue
		}
		result, err := r.executeOne(ctx, call)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (r *Layer2Registry) executeOne(ctx context.Context, call agent.ToolCall) (contextengine.ToolResult, error) {
	r.mu.RLock()
	entry, ok := r.active[call.Name]
	r.mu.RUnlock()

	if !ok {
		// Check if it's a known dormant tool.
		if e, exists := r.all[call.Name]; exists {
			return resultmodel.UnavailableToolResult(call.Name, call.ID, missingGroupBins(e.group), buildLayer2InstallHints(e.group)), nil
		}
		return contextengine.ToolResult{}, fmt.Errorf("layer2 tool %q is not registered", call.Name)
	}
	return entry.execFn(ctx, &r.ws, r.config, call)
}

func missingGroupBins(group *toolGroup) []string {
	if group == nil {
		return nil
	}
	reasons := make([]string, 0, len(group.requiredBins))
	for _, bin := range group.requiredBins {
		if strings.TrimSpace(group.binPaths[bin]) != "" {
			continue
		}
		reasons = append(reasons, "missing binary: "+bin)
	}
	if group.isDisabled {
		reasons = append(reasons, "tool group disabled by config")
	}
	return reasons
}

func buildLayer2InstallHints(group *toolGroup) []string {
	if group == nil || len(group.requiredBins) == 0 {
		return nil
	}
	hints := make([]string, 0, len(group.requiredBins)+1)
	for _, bin := range group.requiredBins {
		hints = append(hints, "install "+bin)
	}
	hints = append(hints, "run env.refresh after installing dependencies")
	return hints
}
