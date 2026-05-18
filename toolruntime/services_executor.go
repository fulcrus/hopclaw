package toolruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

type serviceToolDef struct {
	Manifest skill.ToolManifest
	Handler  layer2ExecFunc
}

type ServicesExecutor struct {
	ws
	config       BuiltinsConfig
	tools        map[string]skill.BoundTool
	definitions  []agent.ToolDefinition
	execHandlers map[string]layer2ExecFunc
}

func NewServicesExecutor(cfg BuiltinsConfig) *ServicesExecutor {
	if cfg.DefaultExecTimeout <= 0 {
		cfg.DefaultExecTimeout = 30 * time.Second
	}
	if cfg.MaxReadBytes <= 0 {
		cfg.MaxReadBytes = 256 * 1024
	}
	exec := &ServicesExecutor{
		ws:           newWorkspace(cfg.Root),
		config:       cfg,
		tools:        make(map[string]skill.BoundTool),
		execHandlers: make(map[string]layer2ExecFunc),
	}
	allowedPaths := make([]string, 0, len(cfg.AllowedPaths)+len(cfg.FSConstraints.AllowedRoots))
	allowedPaths = append(allowedPaths, cfg.AllowedPaths...)
	allowedPaths = append(allowedPaths, cfg.FSConstraints.AllowedRoots...)
	if len(allowedPaths) > 0 {
		exec.ws.setAllowedPaths(allowedPaths)
	}
	exec.ws.setDenyPatterns(cfg.FSConstraints.DenyPatterns)

	pkg := &skill.SkillPackage{
		ID:     "services",
		Kind:   skill.SkillKindExecutable,
		Status: skill.StatusReady,
		Prompt: skill.PromptSkill{
			Name:        "services",
			Description: "Remote service tools backed by configured endpoints and credentials",
			Location:    "services:runtime",
		},
		Source: skill.SkillSource{
			Kind: skill.SourceBundled,
			Dir:  exec.rootAbs,
			Root: exec.rootAbs,
		},
		Trust: skill.TrustBundled,
	}
	for _, def := range servicesToolDefs(cfg) {
		exec.addTool(pkg, def)
	}
	return exec
}

func servicesToolDefs(cfg BuiltinsConfig) []serviceToolDef {
	timeout := cfg.DefaultExecTimeout
	return []serviceToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "search.web",
				Description:     "Search the web for information. Use this for general web results, not for digesting today's news into a table.",
				InputSchema:     searchQuerySchema(),
				OutputSchema:    searchOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				Timeout:         timeout,
			},
			Handler: searchExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "search.news",
				Description:     "Search recent news articles. Use this when you need current news results but not a full digest.",
				InputSchema:     searchQuerySchema(),
				OutputSchema:    searchOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				Timeout:         timeout,
			},
			Handler: searchExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "news.digest",
				Description:     "Search today's or recent news, fetch top sources, and return a ready-to-use Markdown/CSV table. Prefer this over hand-rolling search + fetch + regex + fs.write for news summaries and hot-topic tables.",
				InputSchema:     newsDigestSchema(),
				OutputSchema:    newsDigestOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				Timeout:         timeout,
			},
			Handler: newsDigestExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "speech.tts",
				Description:     "Convert text to speech audio.",
				InputSchema:     speechTTSSchema(),
				OutputSchema:    stubOutputSchema(),
				SideEffectClass: "local_write",
				Timeout:         timeout,
			},
			Handler: speechExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "speech.stt",
				Description:     "Convert speech audio to text.",
				InputSchema:     speechSTTSchema(),
				OutputSchema:    stubOutputSchema(),
				SideEffectClass: "read",
				Timeout:         timeout,
			},
			Handler: speechExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "email.send",
				Description:      "Send an email.",
				InputSchema:      emailSendSchema(),
				OutputSchema:     stubOutputSchema(),
				SideEffectClass:  "external_write",
				RequiresApproval: true,
				Timeout:          timeout,
			},
			Handler: emailExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "email.list",
				Description:     "List recent emails from an IMAP folder.",
				InputSchema:     emailListSchema(),
				OutputSchema:    stubOutputSchema(),
				SideEffectClass: "read",
				Timeout:         timeout,
			},
			Handler: emailExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "email.read",
				Description:     "Read one email by IMAP UID.",
				InputSchema:     emailReadSchema(),
				OutputSchema:    stubOutputSchema(),
				SideEffectClass: "read",
				Timeout:         timeout,
			},
			Handler: emailExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "email.search",
				Description:     "Search emails in an IMAP folder.",
				InputSchema:     emailSearchSchema(),
				OutputSchema:    stubOutputSchema(),
				SideEffectClass: "read",
				Timeout:         timeout,
			},
			Handler: emailExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "email.download_attachment",
				Description:      "Download an email attachment to the workspace.",
				InputSchema:      emailDownloadAttachmentSchema(),
				OutputSchema:     stubOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				Timeout:          timeout,
			},
			Handler: emailExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "calendar.list_events",
				Description:     "List events from a CalDAV calendar.",
				InputSchema:     calendarListEventsSchema(),
				OutputSchema:    calendarListEventsOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				Timeout:         timeout,
			},
			Handler: calendarExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "calendar.create_event",
				Description:      "Create a new event on a CalDAV calendar.",
				InputSchema:      calendarCreateEventSchema(),
				OutputSchema:     calendarCreateEventOutputSchema(),
				SideEffectClass:  "external_write",
				RequiresApproval: true,
				Timeout:          timeout,
			},
			Handler: calendarExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "calendar.update_event",
				Description:      "Update an existing event on a CalDAV calendar.",
				InputSchema:      calendarUpdateEventSchema(),
				OutputSchema:     calendarUpdateEventOutputSchema(),
				SideEffectClass:  "external_write",
				RequiresApproval: true,
				Timeout:          timeout,
			},
			Handler: calendarExec,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "calendar.delete_event",
				Description:      "Delete an event from a CalDAV calendar.",
				InputSchema:      calendarDeleteEventSchema(),
				OutputSchema:     calendarDeleteEventOutputSchema(),
				SideEffectClass:  "external_write",
				RequiresApproval: true,
				Timeout:          timeout,
			},
			Handler: calendarExec,
		},
	}
}

func (e *ServicesExecutor) addTool(pkg *skill.SkillPackage, def serviceToolDef) {
	name := strings.TrimSpace(def.Manifest.Name)
	if name == "" {
		return
	}
	eligible, reasons := serviceToolEligibility(e.config, name)
	availability := availabilityFromEligibility(eligible, reasons)
	pkg.ToolManifests = append(pkg.ToolManifests, skill.ToolManifest{
		Name:             name,
		Description:      def.Manifest.Description,
		InputSchema:      cloneSchema(def.Manifest.InputSchema),
		OutputSchema:     cloneSchema(def.Manifest.OutputSchema),
		SideEffectClass:  def.Manifest.SideEffectClass,
		Idempotent:       def.Manifest.Idempotent,
		RequiresApproval: def.Manifest.RequiresApproval,
		ExecutionKey:     def.Manifest.ExecutionKey,
		Timeout:          def.Manifest.Timeout,
	})
	bound := skill.BoundTool{
		Package: pkg,
		Manifest: skill.ToolManifest{
			Name:             name,
			Description:      def.Manifest.Description,
			InputSchema:      cloneSchema(def.Manifest.InputSchema),
			OutputSchema:     cloneSchema(def.Manifest.OutputSchema),
			SideEffectClass:  def.Manifest.SideEffectClass,
			Idempotent:       def.Manifest.Idempotent,
			RequiresApproval: def.Manifest.RequiresApproval,
			ExecutionKey:     def.Manifest.ExecutionKey,
			Timeout:          def.Manifest.Timeout,
		},
		Eligibility: skill.EligibilityResult{Eligible: eligible, Reasons: append([]string(nil), reasons...)},
	}
	e.tools[name] = bound
	e.execHandlers[name] = def.Handler
	e.definitions = append(e.definitions, agent.ToolDefinition{
		Name:               name,
		Description:        def.Manifest.Description,
		InputSchema:        cloneSchema(def.Manifest.InputSchema),
		OutputSchema:       cloneSchema(def.Manifest.OutputSchema),
		SideEffectClass:    def.Manifest.SideEffectClass,
		Idempotent:         def.Manifest.Idempotent,
		RequiresApproval:   def.Manifest.RequiresApproval,
		ExecutionKey:       def.Manifest.ExecutionKey,
		Source:             "services",
		SourceRef:          "services:runtime",
		Trust:              string(skill.TrustBundled),
		Eligible:           eligible,
		EligibilityReasons: append([]string(nil), reasons...),
		Availability:       availability,
	})
}

func (e *ServicesExecutor) ToolDefinitions(*agent.Session) []agent.ToolDefinition {
	if e == nil || len(e.definitions) == 0 {
		return nil
	}
	out := make([]agent.ToolDefinition, 0, len(e.definitions))
	for _, def := range e.definitions {
		out = append(out, copyToolDefinition(def))
	}
	return out
}

func (e *ServicesExecutor) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	if e == nil {
		return nil, false
	}
	bound, ok := e.tools[strings.TrimSpace(name)]
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
		Source:             "services",
		SourceRef:          "services:runtime",
		Trust:              string(copied.Package.Trust),
		Eligible:           copied.Eligibility.Eligible,
		EligibilityReasons: append([]string(nil), copied.Eligibility.Reasons...),
		Availability:       availabilityFromEligibility(copied.Eligibility.Eligible, copied.Eligibility.Reasons),
	}, "services_executor"), true
}

func serviceToolEligibility(cfg BuiltinsConfig, name string) (bool, []string) {
	name = strings.TrimSpace(name)
	switch name {
	case "search.web", "search.news", "news.digest":
		if cfg.Services.Search.IsConfigured() {
			return true, nil
		}
		return false, []string{"configure tools.services.search to enable this tool"}
	case "speech.tts", "speech.stt":
		if cfg.Services.Speech.IsConfigured() {
			return true, nil
		}
		return false, []string{"configure tools.services.speech to enable this tool"}
	case "email.send":
		if cfg.Services.Email.HasSMTP() {
			return true, nil
		}
		return false, []string{"configure tools.services.email SMTP settings to enable this tool"}
	case "email.list", "email.read", "email.search", "email.download_attachment":
		if cfg.Services.Email.HasIMAP() {
			return true, nil
		}
		return false, []string{"configure tools.services.email IMAP settings to enable this tool"}
	case "calendar.list_events", "calendar.create_event", "calendar.update_event", "calendar.delete_event":
		if cfg.Services.Calendar.IsConfigured() {
			return true, nil
		}
		return false, []string{"configure tools.services.calendar to enable this tool"}
	default:
		return true, nil
	}
}

func (e *ServicesExecutor) ExecuteBatch(ctx context.Context, _ *agent.Run, _ *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	results := make([]contextengine.ToolResult, 0, len(calls))
	for _, call := range calls {
		handler, ok := e.execHandlers[strings.TrimSpace(call.Name)]
		if !ok {
			return nil, fmt.Errorf("tool %q is not registered", call.Name)
		}
		result, err := handler(ctx, &e.ws, e.config, call)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}
