package repl

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	badgepkg "github.com/fulcrus/hopclaw/internal/cli/badge"
	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

type Service interface {
	Commands(context.Context) ([]acp.Command, error)
	Models(context.Context) ([]ModelInfo, error)
	ListSessions(context.Context) ([]SessionSummary, error)
	GetSession(context.Context, string) (*SessionDetail, error)
	ListApprovals(context.Context, string, int) ([]ApprovalSummary, error)
	ResolveApproval(context.Context, string, bool) (*ApprovalSummary, error)
	QualitySnapshot(context.Context) (*QualitySnapshot, error)
	ListEvalSuites(context.Context) ([]EvalSuiteSummary, error)
	RunEvalSuite(context.Context, string) (*EvalRunSummary, error)
	ListRuns(context.Context, string, int) ([]RunSummary, error)
	GetRunDetail(context.Context, string) (*RunDetail, error)
	DoctorChecks(context.Context) ([]DoctorCheck, error)
	ListTools(context.Context, string) ([]ToolSummary, error)
	ListSkills(context.Context) ([]SkillSummary, error)
	SearchSkillCatalog(context.Context, string) ([]SkillCatalogSummary, error)
	GetSkill(context.Context, string) (*SkillDetail, error)
	InstallSkill(context.Context, string, string) (*SkillInstallResult, error)
	RemoveSkill(context.Context, string) error
	SupervisorSnapshot(context.Context) (*SupervisorSnapshot, error)
	GetRunDelivery(context.Context, string) (*RunDeliveryDetail, error)
	ListGovernanceDeliveries(context.Context, string, int) ([]DeliveryListItem, error)
	RedriveDelivery(context.Context, string) (*RedriveResult, error)
	ReadinessSnapshot(context.Context) (*ReadinessSnapshot, error)
	RecoveryCandidates(context.Context) ([]RecoveryCandidate, error)
	ListAutomations(context.Context, int) ([]AutomationItem, error)
	CreateAutomation(context.Context, AutomationCreateRequest) (*AutomationItem, error)
	PauseAutomation(context.Context, string, string) error
	ResumeAutomation(context.Context, string, string) error
	RunAutomationNow(context.Context, string, string) error
	GetAutomationDetail(context.Context, string, string) (*AutomationItem, error)
	ListMemory(context.Context, string, int) ([]agent.MemoryEntry, error)
	GetMemory(context.Context, string) (*agent.MemoryEntry, error)
	SaveMemory(context.Context, string, string, string, string, string) (*agent.MemoryEntry, error)
	DeleteMemory(context.Context, string) error
	ListMemoryConflicts(context.Context) ([]agent.MemoryEntry, error)
	ListPendingMemoryWrites(context.Context) ([]agent.MemoryEntry, error)
	ResolveMemoryConflict(context.Context, string, string) error
	RecallMemories(ctx context.Context, sessionKey, projectID string) ([]agent.MemoryEntry, error)
	MemoryUsedInContext(context.Context, string) ([]MemoryUsageItem, error)
	ContextPressure(context.Context, string) (*ContextPressureInfo, error)
	FindOrCreateProject(ctx context.Context, directory string) (*agent.Project, error)
	StartNewEpisode(context.Context, string) error
	ResetSession(context.Context, string) error
	CompactSession(context.Context, string) error
	ResolvePermission(context.Context, acp.PermissionRequest, PermissionDecision) error
	Close(context.Context) error
}

type TargetInfo struct {
	Name        string
	Kind        string
	Description string
}

type TargetBinding struct {
	Client       *acp.InProcessClient
	Service      Service
	Target       TargetInfo
	SessionID    string
	SessionKey   string
	SessionModel string
	Models       []ModelInfo
	Commands     []acp.Command
}

type TargetManager interface {
	CurrentTarget() TargetInfo
	ListTargets(context.Context) ([]TargetInfo, error)
	SwitchTarget(context.Context, string) (*TargetBinding, error)
	LoginTarget(context.Context, string, string) error
	LogoutTarget(context.Context, string) error
}

type ModelInfo struct {
	ID               string
	ContextWindow    int
	SupportsThinking bool
}

type SessionSummary struct {
	ID           string
	Key          string
	Model        string
	MessageCount int
}

type SessionMessage struct {
	Role      string
	Content   string
	CreatedAt string
}

type SessionDetail struct {
	Summary  SessionSummary
	Messages []SessionMessage
}

type PermissionDecision struct {
	Approved bool
	Scope    string
}

type CommandResult struct {
	Exit   bool
	Submit string
}

type CommandHandler func(context.Context, *REPL, []string) (CommandResult, error)

type commandEntry struct {
	Command  acp.Command
	Usage    string
	Group    string
	Intent   string
	Keywords []string
	Handler  CommandHandler
}

type CommandRegistry struct {
	system  map[string]commandEntry
	dynamic map[string]acp.Command
}

var commandAliases = map[string]string{}

type badgeSelector interface {
	Run() (bool, error)
}

const (
	commandGroupTaskControl = "Task Control"
	commandGroupSession     = "Conversation"
	commandGroupRuntime     = "Runtime"
	commandGroupGovernance  = "Governance"
	commandGroupInfo        = "Info"
	commandGroupIdentity    = "Identity"

	internalConfirmedArg   = "__confirmed__"
	internalInspectArg     = "__inspect__"
	internalModelSelectArg = "__select__"
	internalModelThinkArg  = "__selectthink__"
)

var deprecatedCommandReplacements = map[string]string{
	"stop":   "pause",
	"resume": "continue",
}

var defaultACPInventoryCommandNames = func() map[string]struct{} {
	names := make(map[string]struct{}, len(acp.DefaultCommands()))
	for _, command := range acp.DefaultCommands() {
		name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(command.Name)), "/")
		if name == "" {
			continue
		}
		names[name] = struct{}{}
	}
	return names
}()

var remoteCommandErrorReplacer = strings.NewReplacer(
	"standalone", "local",
	"already connected to target", "already connected to remote",
	"already using connection", "already connected to remote",
	"already connected to runtime", "already connected to remote",
	"target switching", "remote switching",
	"connection switching", "remote switching",
	"runtime switching", "remote switching",
	"target name", "remote name",
	"connection name", "remote name",
	"runtime name", "remote name",
	"target ", "remote ",
	"Target ", "Remote ",
)

var newBadgeSelector = func(mgr *badgepkg.Manager, rdr *badgepkg.Renderer, out io.Writer, in *os.File) badgeSelector {
	return badgepkg.NewSelector(mgr, rdr, out, in)
}

func NewCommandRegistry() *CommandRegistry {
	registry := &CommandRegistry{
		system:  make(map[string]commandEntry),
		dynamic: make(map[string]acp.Command),
	}
	registry.registerSystemCommands()
	return registry
}

func (r *CommandRegistry) registerSystemCommands() {
	r.registerDetailed("status", "Show current conversation status", "/status", commandGroupInfo, "show the current REPL state", []string{"state", "summary", "session"}, func(ctx context.Context, repl *REPL, _ []string) (CommandResult, error) {
		repl.renderer.SystemLine(repl.statusLine())
		return CommandResult{}, nil
	})
	r.registerDetailed("view", "Switch dock layout", "/view [full|compact|plain|auto]", commandGroupInfo, "change the dock layout mode", []string{"layout", "dock", "compact", "plain"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) == 0 {
			repl.renderer.SystemLine("Current view: " + string(repl.layoutMode))
			return CommandResult{}, nil
		}
		mode, ok := parseLayoutMode(args[0])
		if !ok || mode == LayoutAuto {
			if strings.EqualFold(strings.TrimSpace(args[0]), "auto") {
				repl.layoutMode = LayoutAuto
				repl.renderDock()
				repl.renderBadge()
				repl.renderer.SystemLine("View set to auto.")
				return CommandResult{}, nil
			}
			return CommandResult{}, fmt.Errorf("usage: /view [full|compact|plain|auto]")
		}
		repl.layoutMode = mode
		repl.renderDock()
		repl.renderBadge()
		repl.renderer.SystemLine("View set to " + string(mode) + ".")
		return CommandResult{}, nil
	})
	r.registerDetailed("pause", "Pause current run", "/pause", commandGroupTaskControl, "pause the active run", []string{"interrupt", "hold"}, func(ctx context.Context, repl *REPL, _ []string) (CommandResult, error) {
		if !repl.running {
			repl.renderer.RenderSystemEvent("No active task.")
			return CommandResult{}, nil
		}
		if err := repl.requestPause(ctx); err != nil {
			return CommandResult{}, err
		}
		return CommandResult{}, nil
	})
	r.registerDetailed("cancel", "Cancel current run", "/cancel", commandGroupTaskControl, "cancel the active run without preserving a paused handle", []string{"abort", "terminate"}, func(ctx context.Context, repl *REPL, _ []string) (CommandResult, error) {
		if !repl.running {
			repl.renderer.RenderSystemEvent("No active task.")
			return CommandResult{}, nil
		}
		if err := repl.requestCancel(ctx); err != nil {
			return CommandResult{}, err
		}
		return CommandResult{}, nil
	})
	r.registerDetailed("continue", "Continue a paused task", "/continue [run_id|conversation_key]", commandGroupTaskControl, "continue a paused task or restore past work", []string{"restart", "run", "session"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) > 1 {
			return CommandResult{}, fmt.Errorf("usage: /continue [run_id|conversation_key]")
		}
		if len(args) == 1 {
			return CommandResult{}, repl.resumeReference(ctx, args[0])
		}
		return CommandResult{}, repl.resumePaused(ctx, false)
	})
	r.registerDetailed("retry", "Restart the last paused task", "/retry", commandGroupTaskControl, "restart the last paused task", []string{"rerun", "again"}, func(ctx context.Context, repl *REPL, _ []string) (CommandResult, error) {
		return CommandResult{}, repl.resumePaused(ctx, true)
	})
	r.registerDetailed("discard", "Discard the paused task", "/discard", commandGroupTaskControl, "discard the current paused task", []string{"drop", "clear"}, func(ctx context.Context, repl *REPL, _ []string) (CommandResult, error) {
		if repl.pausedRun == nil {
			repl.renderer.RenderSystemEvent("No paused task.")
			return CommandResult{}, nil
		}
		repl.discardPaused()
		return CommandResult{}, nil
	})
	r.registerDetailed("background", "Detach the current run into background", "/background", commandGroupTaskControl, "move the current run into background supervision", []string{"bg", "detach", "supervisor"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) != 0 {
			return CommandResult{}, fmt.Errorf("usage: /background")
		}
		if !repl.running {
			return CommandResult{}, fmt.Errorf("no active run to background")
		}
		repl.ensureCurrentRunID()
		runID := strings.TrimSpace(repl.currentRunID)
		if runID == "" {
			return CommandResult{}, fmt.Errorf("no active run to background")
		}
		repl.renderBackgroundedSnapshot()
		if err := repl.backgroundRun(ctx, runID); err != nil {
			return CommandResult{}, err
		}
		repl.running = false
		repl.transitionPhase(PhaseIdle, "")
		repl.renderer.StopSpinner()
		repl.renderer.RenderSystemEvent("Backgrounded run " + runID + ".")
		return CommandResult{}, nil
	})
	r.registerDetailed("bg", "Alias of /background", "/bg", commandGroupTaskControl, "alias for backgrounding the current run", []string{"background"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		return r.system["background"].Handler(ctx, repl, args)
	})
	r.registerDetailed("foreground", "Bring a run back to foreground focus", "/foreground <run_id>", commandGroupTaskControl, "move a background run back into foreground focus", []string{"fg", "focus", "supervisor"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) != 1 {
			return CommandResult{}, fmt.Errorf("usage: /foreground <run_id>")
		}
		runID := strings.TrimSpace(args[0])
		if err := repl.foregroundRun(ctx, runID); err != nil {
			return CommandResult{}, err
		}
		repl.renderer.RenderSystemEvent("Foreground focus moved to run " + runID + ".")
		return CommandResult{}, nil
	})
	r.registerDetailed("fg", "Alias of /foreground", "/fg <run_id>", commandGroupTaskControl, "alias for foregrounding a run", []string{"foreground"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		return r.system["foreground"].Handler(ctx, repl, args)
	})
	r.registerDetailed("model", "Show or change model", "/model [name]", commandGroupRuntime, "inspect or change the active model", []string{"llm", "reasoning", "gpt"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) == 0 {
			return CommandResult{}, repl.renderModelPicker(ctx)
		}
		if len(args) == 2 && args[0] == internalModelSelectArg {
			repl.selectedModel = strings.TrimSpace(args[1])
			repl.renderer.SystemLine("Model set to " + repl.selectedModel + ".")
			return CommandResult{}, nil
		}
		if len(args) == 3 && args[0] == internalModelThinkArg {
			repl.selectedModel = strings.TrimSpace(args[1])
			switch strings.ToLower(strings.TrimSpace(args[2])) {
			case "on":
				repl.thinking = true
			case "off":
				repl.thinking = false
			default:
				return CommandResult{}, fmt.Errorf("usage: /model [name]")
			}
			repl.renderer.SystemLine("Model set to " + repl.selectedModel + ".")
			if repl.thinking {
				repl.renderer.SystemLine("Thinking mode enabled.")
			} else {
				repl.renderer.SystemLine("Thinking mode disabled.")
			}
			return CommandResult{}, nil
		}
		name := strings.TrimSpace(args[0])
		if name == "" {
			return CommandResult{}, nil
		}
		models, err := repl.loadModels(ctx)
		if err == nil && len(models) > 0 {
			valid := false
			for _, item := range models {
				if item.ID == name {
					valid = true
					break
				}
			}
			if !valid {
				names := make([]string, 0, len(models))
				for _, item := range models {
					names = append(names, item.ID)
				}
				return CommandResult{}, fmt.Errorf("model %q is unavailable. available models: %s", name, strings.Join(names, ", "))
			}
		}
		repl.selectedModel = name
		repl.renderer.SystemLine("Model set to " + name)
		return CommandResult{}, nil
	})
	r.registerDetailed("think", "Toggle thinking mode", "/think [on|off]", commandGroupRuntime, "toggle higher-effort reasoning", []string{"reasoning", "thinking"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) == 0 {
			if repl.thinking {
				repl.renderer.SystemLine("Thinking mode: on")
			} else {
				repl.renderer.SystemLine("Thinking mode: off")
			}
			return CommandResult{}, nil
		}
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "on":
			repl.thinking = true
			supported := true
			model := repl.effectiveModel()
			for _, m := range repl.modelCache {
				if m.ID == model {
					supported = m.SupportsThinking
					break
				}
			}
			if !supported && len(repl.modelCache) > 0 {
				repl.renderer.SystemLine("Thinking mode enabled, but the current model may not support it.")
				repl.renderer.SystemLine("Use /model to choose a model that supports thinking.")
			} else {
				repl.renderer.SystemLine("Thinking mode enabled.")
			}
		case "off":
			repl.thinking = false
			repl.renderer.SystemLine("Thinking mode disabled.")
		default:
			return CommandResult{}, fmt.Errorf("usage: /think [on|off]")
		}
		return CommandResult{}, nil
	})
	r.registerDetailed("help", "Show help", "/help [command|keyword]", commandGroupInfo, "show grouped help or command details", []string{"docs", "commands", "usage"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		repl.renderHelp(args)
		return CommandResult{}, nil
	})
	r.register("clear", "Clear screen", "/clear", func(ctx context.Context, repl *REPL, _ []string) (CommandResult, error) {
		repl.renderer.ClearScreen()
		return CommandResult{}, nil
	})
	r.registerDetailed("attach", "Insert an attachment into the composer", "/attach <image|file|dir|video> <path>", commandGroupInfo, "stage an attachment for the next message", []string{"image", "file", "dir", "video", "attachment"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) < 2 {
			return CommandResult{}, fmt.Errorf("usage: /attach <image|file|dir|video> <path>")
		}
		kind, ok := richedit.ParseAttachKind(args[0])
		if !ok {
			return CommandResult{}, fmt.Errorf("attach type must be image, file, dir, or video")
		}
		block, err := richedit.BuildAttachmentContentBlock(kind, strings.Join(args[1:], " "))
		if err != nil {
			return CommandResult{}, err
		}
		if repl.panelController != nil {
			repl.clearPanel()
		}
		if repl.oneShot {
			blocks := append([]contextengine.ContentBlock(nil), repl.lastContentBlocks...)
			blocks = append(blocks, block)
			repl.lastContentBlocks = nil
			return CommandResult{}, repl.submitPrepared(ctx, "", nil, blocks, repl.effectiveModel())
		}
		repl.lastImages = nil
		repl.lastContentBlocks = append(repl.lastContentBlocks, block)
		count := attachmentBlockCount(repl.lastContentBlocks)
		label := strings.ToUpper(strings.TrimSpace(string(block.Type)))
		if label == "" {
			label = "ATTACHMENT"
		}
		repl.renderer.SystemLine(label + " staged for the next message.")
		if count <= 1 {
			repl.renderer.SystemLine("Send your next message to include it.")
			return CommandResult{}, nil
		}
		repl.renderer.SystemLine(fmt.Sprintf("%d attachments staged. Send your next message to include them.", count))
		return CommandResult{}, nil
	})
	r.registerDetailed("badge", "Manage badge display", "/badge [subcommand]", commandGroupIdentity, "manage the terminal badge", []string{"avatar", "profile", "identity"}, handleBadgeCommand)
	r.registerDetailed("compact", "Compact conversation history", "/compact", commandGroupSession, "compact older conversation context", []string{"summarize", "trim", "context"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if repl.sessionID == "" && repl.sessionKey == "" {
			return CommandResult{}, fmt.Errorf("no active conversation")
		}
		if len(args) == 0 && !repl.confirmCompactSession(ctx) {
			if repl.panelController == nil {
				repl.renderer.SystemLine("Conversation compaction cancelled.")
			}
			return CommandResult{}, nil
		}
		if len(args) > 0 && strings.TrimSpace(args[0]) != internalConfirmedArg {
			return CommandResult{}, fmt.Errorf("usage: /compact")
		}
		detail, sessionID, err := repl.currentServiceSession(ctx)
		if err != nil {
			return CommandResult{}, err
		}
		if sessionID == "" || detail == nil || len(detail.Messages) == 0 {
			repl.renderer.SystemLine("No history yet.")
			return CommandResult{}, nil
		}
		before, _ := repl.describeContext(ctx)
		if err := repl.service.CompactSession(ctx, sessionID); err != nil {
			return CommandResult{}, err
		}
		after, _ := repl.describeContext(ctx)
		lines := []string{"Conversation compacted."}
		if strings.TrimSpace(before) != "" {
			lines = append(lines, "Before: "+before)
		}
		if strings.TrimSpace(after) != "" {
			lines = append(lines, "After: "+after)
		}
		lines = append(lines, "Older turns were summarized into the current conversation memory.")
		repl.openInfoPanel("Conversation Compacted", lines, "Esc back")
		return CommandResult{}, nil
	})
	r.registerDetailed("episode", "Start a new conversation checkpoint", "/episode", commandGroupSession, "start a fresh checkpoint inside the current conversation", []string{"boundary", "new topic"}, func(ctx context.Context, repl *REPL, _ []string) (CommandResult, error) {
		if repl.sessionID == "" && repl.sessionKey == "" {
			return CommandResult{}, fmt.Errorf("no active conversation")
		}
		sessionID, err := repl.currentServiceSessionID(ctx)
		if err != nil {
			return CommandResult{}, err
		}
		if sessionID == "" {
			repl.renderer.SystemLine("Conversation is empty. The next message will start fresh.")
			return CommandResult{}, nil
		}
		if err := repl.service.StartNewEpisode(ctx, sessionID); err != nil {
			return CommandResult{}, err
		}
		repl.renderer.SystemLine("Started a new conversation checkpoint.")
		return CommandResult{}, nil
	})
	r.registerDetailed("context", "Show context window usage", "/context", commandGroupSession, "show context usage and compaction guidance", []string{"tokens", "window", "budget"}, func(ctx context.Context, repl *REPL, _ []string) (CommandResult, error) {
		return CommandResult{}, repl.renderContextPanel(ctx)
	})
	r.registerDetailed("memory", "Inspect and manage remembered facts", "/memory [search <query>|inspect <key>|pin <text>|delete <key>]", commandGroupSession, "inspect, pin, or delete remembered facts", []string{"facts", "remember", "pin", "delete"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) == 0 {
			return CommandResult{}, repl.renderMemoryPanel(ctx, "", 12)
		}
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "search":
			if len(args) < 2 {
				return CommandResult{}, fmt.Errorf("usage: /memory search <query>")
			}
			return CommandResult{}, repl.renderMemoryPanel(ctx, strings.Join(args[1:], " "), 12)
		case "inspect":
			if len(args) == 3 && args[1] == internalInspectArg {
				return CommandResult{}, repl.inspectMemory(ctx, args[2])
			}
			if len(args) != 2 {
				return CommandResult{}, fmt.Errorf("usage: /memory inspect <key>")
			}
			return CommandResult{}, repl.inspectMemory(ctx, args[1])
		case "pin":
			if len(args) < 2 {
				return CommandResult{}, fmt.Errorf("usage: /memory pin <text>")
			}
			return CommandResult{}, repl.pinMemory(ctx, strings.Join(args[1:], " "))
		case "delete":
			confirmed := false
			if len(args) == 3 && args[1] == internalConfirmedArg {
				confirmed = true
				args = []string{args[0], args[2]}
			}
			if len(args) != 2 {
				return CommandResult{}, fmt.Errorf("usage: /memory delete <key>")
			}
			return CommandResult{}, repl.deleteMemory(ctx, args[1], confirmed)
		case "conflicts":
			return CommandResult{}, repl.renderMemoryConflicts(ctx)
		case "pending":
			return CommandResult{}, repl.renderMemoryPending(ctx)
		case "resolve":
			if len(args) < 3 {
				return CommandResult{}, fmt.Errorf("usage: /memory resolve <key> keep|accept")
			}
			key := strings.TrimSpace(args[1])
			action := strings.ToLower(strings.TrimSpace(args[2]))
			if action != "keep" && action != "accept" {
				return CommandResult{}, fmt.Errorf("usage: /memory resolve <key> keep|accept")
			}
			if err := repl.service.ResolveMemoryConflict(ctx, key, action); err != nil {
				return CommandResult{}, err
			}
			repl.renderer.SystemLine(fmt.Sprintf("Memory conflict on %q resolved (%s).", key, action))
			return CommandResult{}, nil
		default:
			return CommandResult{}, fmt.Errorf("usage: /memory [search|inspect|pin|delete|conflicts|pending|resolve]")
		}
	})
	r.registerDetailed("tools", "Inspect runtime tools visible to the current target", "/tools [list|search <query>|info <name>|check [name]]", commandGroupRuntime, "inspect available tools and their side-effect profile", []string{"tooling", "capabilities", "inventory"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) == 0 {
			return CommandResult{}, repl.renderToolsPanel(ctx, "")
		}
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "list":
			if len(args) != 1 {
				return CommandResult{}, fmt.Errorf("usage: /tools list")
			}
			return CommandResult{}, repl.renderToolsPanel(ctx, "")
		case "search":
			if len(args) < 2 {
				return CommandResult{}, fmt.Errorf("usage: /tools search <query>")
			}
			return CommandResult{}, repl.renderToolsPanel(ctx, strings.Join(args[1:], " "))
		case "info", "inspect":
			if len(args) == 3 && args[1] == internalInspectArg {
				return CommandResult{}, repl.renderToolDetail(ctx, args[2])
			}
			if len(args) != 2 {
				return CommandResult{}, fmt.Errorf("usage: /tools info <name>")
			}
			return CommandResult{}, repl.renderToolDetail(ctx, args[1])
		case "check":
			if len(args) > 2 {
				return CommandResult{}, fmt.Errorf("usage: /tools check [name]")
			}
			name := ""
			if len(args) == 2 {
				name = args[1]
			}
			return CommandResult{}, repl.renderToolCheck(ctx, name)
		default:
			return CommandResult{}, fmt.Errorf("usage: /tools [list|search <query>|info <name>|check [name]]")
		}
	})
	r.registerDetailed("skills", "Inspect and manage installed skills", "/skills [list|search <query>|info <name>|install <name-or-path>|remove <name>]", commandGroupRuntime, "inspect installed skills and catalog entries", []string{"skill", "catalog", "install"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) == 0 {
			return CommandResult{}, repl.renderSkillsPanel(ctx)
		}
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "list":
			if len(args) != 1 {
				return CommandResult{}, fmt.Errorf("usage: /skills list")
			}
			return CommandResult{}, repl.renderSkillsPanel(ctx)
		case "search":
			if len(args) < 2 {
				return CommandResult{}, fmt.Errorf("usage: /skills search <query>")
			}
			return CommandResult{}, repl.renderSkillCatalogPanel(ctx, strings.Join(args[1:], " "))
		case "info", "inspect":
			if len(args) == 3 && args[1] == internalInspectArg {
				return CommandResult{}, repl.renderSkillDetail(ctx, args[2])
			}
			if len(args) != 2 {
				return CommandResult{}, fmt.Errorf("usage: /skills info <name>")
			}
			return CommandResult{}, repl.renderSkillDetail(ctx, args[1])
		case "install":
			if len(args) < 2 {
				return CommandResult{}, fmt.Errorf("usage: /skills install <name-or-path>")
			}
			return CommandResult{}, repl.installSkill(ctx, strings.Join(args[1:], " "), "")
		case "remove", "delete", "uninstall":
			confirmed := false
			if len(args) == 3 && args[1] == internalConfirmedArg {
				confirmed = true
				args = []string{args[0], args[2]}
			}
			if len(args) != 2 {
				return CommandResult{}, fmt.Errorf("usage: /skills remove <name>")
			}
			return CommandResult{}, repl.removeSkill(ctx, args[1], confirmed)
		default:
			return CommandResult{}, fmt.Errorf("usage: /skills [list|search <query>|info <name>|install <name-or-path>|remove <name>]")
		}
	})
	r.registerDetailed("session", "List or switch conversations", "/session [key] | /session new [key]", commandGroupSession, "list conversations or switch to another conversation", []string{"chat", "conversation", "history"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) == 0 {
			return CommandResult{}, repl.renderSessionPicker(ctx)
		}
		if strings.EqualFold(args[0], "new") {
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
				return CommandResult{}, fmt.Errorf("usage: /session new [key]")
			}
			if err := repl.switchSession(ctx, strings.TrimSpace(args[1]), true); err != nil {
				return CommandResult{}, err
			}
			repl.renderer.SystemLine("Started conversation " + repl.sessionKey + ".")
			return CommandResult{}, nil
		}
		if err := repl.switchSession(ctx, strings.TrimSpace(args[0]), false); err != nil {
			return CommandResult{}, err
		}
		repl.renderer.SystemLine("Switched to conversation " + repl.sessionKey + ".")
		return CommandResult{}, nil
	})
	r.registerDetailed("remote", "List or switch runtimes", "/remote [name|local|list|login|logout]", commandGroupRuntime, "inspect or switch the active remote/local runtime", []string{"remote", "server", "gateway", "local"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if repl.targetManager == nil {
			repl.renderer.SystemLine("Current runtime: " + targetDescriptor(repl.targetName, repl.targetKind))
			return CommandResult{}, nil
		}
		if len(args) == 0 || strings.EqualFold(strings.TrimSpace(args[0]), "list") {
			return CommandResult{}, repl.renderRemotePicker(ctx)
		}
		if strings.EqualFold(strings.TrimSpace(args[0]), "login") {
			if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
				return CommandResult{}, fmt.Errorf("usage: /remote login <name>")
			}
			name := strings.TrimSpace(args[1])
			token, err := repl.prompter.ReadSecret("Bearer token for " + name + ": ")
			if err != nil {
				return CommandResult{}, err
			}
			if err := repl.targetManager.LoginTarget(ctx, name, token); err != nil {
				return CommandResult{}, remoteCommandError(err)
			}
			repl.renderer.SystemLine("Saved credentials for remote " + name + ".")
			if strings.EqualFold(repl.targetName, name) {
				repl.renderer.SystemLine("Reconnect the current remote to use the new credentials.")
			}
			return CommandResult{}, nil
		}
		if strings.EqualFold(strings.TrimSpace(args[0]), "logout") {
			if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
				return CommandResult{}, fmt.Errorf("usage: /remote logout <name>")
			}
			name := strings.TrimSpace(args[1])
			if err := repl.targetManager.LogoutTarget(ctx, name); err != nil {
				return CommandResult{}, remoteCommandError(err)
			}
			repl.renderer.SystemLine("Cleared credentials for remote " + name + ".")
			if strings.EqualFold(repl.targetName, name) {
				repl.renderer.SystemLine("Reconnect the current remote to apply the updated auth state.")
			}
			return CommandResult{}, nil
		}
		name := strings.TrimSpace(args[0])
		if name == "" {
			return CommandResult{}, fmt.Errorf("usage: /remote [name|local|list|login|logout]")
		}
		if name == internalConfirmedArg {
			if len(args) != 2 {
				return CommandResult{}, fmt.Errorf("usage: /remote [name|local|list|login|logout]")
			}
			name = strings.TrimSpace(args[1])
		} else if !repl.confirmRemoteSwitch(ctx, name) {
			if repl.panelController == nil {
				repl.renderer.SystemLine("Remote switch cancelled.")
			}
			return CommandResult{}, nil
		}
		if err := repl.switchTarget(ctx, name); err != nil {
			return CommandResult{}, remoteCommandError(err)
		}
		repl.renderer.SystemLine(fmt.Sprintf("Switched to %s.", targetDescriptor(repl.targetName, repl.targetKind)))
		repl.renderer.SystemLine("Conversation binding moved to the selected runtime target.")
		return CommandResult{}, nil
	})
	r.registerDetailed("history", "Show recent conversation history", "/history", commandGroupSession, "show the recent conversation history", []string{"messages", "turns", "transcript"}, func(ctx context.Context, repl *REPL, _ []string) (CommandResult, error) {
		if err := repl.renderHistory(ctx); err != nil {
			return CommandResult{}, err
		}
		return CommandResult{}, nil
	})
	r.registerDetailed("reset", "Reset current conversation", "/reset", commandGroupSession, "reset the current conversation", []string{"clear", "restart", "session"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if repl.sessionKey == "" {
			return CommandResult{}, fmt.Errorf("no active conversation")
		}
		if len(args) == 0 && !repl.confirmResetSession(ctx) {
			if repl.panelController == nil {
				repl.renderer.SystemLine("Conversation reset cancelled.")
			}
			return CommandResult{}, nil
		}
		if len(args) > 0 && strings.TrimSpace(args[0]) != internalConfirmedArg {
			return CommandResult{}, fmt.Errorf("usage: /reset")
		}
		if err := repl.service.ResetSession(ctx, repl.sessionKey); err != nil {
			return CommandResult{}, err
		}
		if err := repl.switchSession(ctx, repl.sessionKey, true); err != nil {
			return CommandResult{}, err
		}
		repl.renderer.SystemLine("Conversation reset.")
		return CommandResult{}, nil
	})
	r.registerDetailed("cd", "Change working directory and project context", "/cd <path>", commandGroupRuntime, "change the local working directory", []string{"directory", "project", "cwd"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) == 0 {
			dir, err := os.Getwd()
			if err != nil {
				return CommandResult{}, err
			}
			if repl.currentProject != nil {
				repl.renderer.SystemLine(fmt.Sprintf("Current directory: %s (%s)", dir, repl.currentProject.Name))
			} else {
				repl.renderer.SystemLine("Current directory: " + dir)
			}
			return CommandResult{}, nil
		}

		target := strings.Join(args, " ")
		if strings.HasPrefix(target, "~/") {
			home, err := os.UserHomeDir()
			if err == nil {
				target = filepath.Join(home, target[2:])
			}
		}
		if err := os.Chdir(target); err != nil {
			return CommandResult{}, fmt.Errorf("cd: %w", err)
		}

		absDir, err := os.Getwd()
		if err != nil {
			return CommandResult{}, err
		}

		project, err := repl.service.FindOrCreateProject(ctx, absDir)
		if err != nil || project == nil {
			repl.currentProject = nil
			lines := []string{
				"Directory changed.",
				"Project: unavailable",
				"Path: " + absDir,
				"Loaded project memories: 0",
			}
			if normalizeTargetKind(repl.targetKind, repl.targetName) == "remote" {
				lines = append(lines, "Local working directory changed. Execution stays on "+targetDescriptor(repl.targetName, repl.targetKind)+".")
			}
			lines = append(lines, "Suggested actions: /history  /context  /runs recent")
			repl.openInfoPanel("Directory Changed", lines, "/history  /context  /runs recent  Esc back")
			return CommandResult{}, nil
		}
		repl.currentProject = project

		memories, _ := repl.service.RecallMemories(ctx, repl.sessionKey, project.ID)
		lines := []string{
			"Directory changed.",
			"Project: " + project.Name,
			"Path: " + absDir,
			fmt.Sprintf("Loaded project memories: %d", len(memories)),
			"Last conversation: " + defaultString(repl.sessionKey, "default"),
		}
		if normalizeTargetKind(repl.targetKind, repl.targetName) == "remote" {
			lines = append(lines, "Local working directory changed. Execution stays on "+targetDescriptor(repl.targetName, repl.targetKind)+".")
		}
		lines = append(lines, "Suggested actions: /history  /context  /runs recent")
		repl.openInfoPanel("Directory Changed", lines, "/history  /context  /runs recent  Esc back")
		return CommandResult{}, nil
	})
	r.registerDetailed("approvals", "List pending approvals and resolve them", "/approvals [approve|deny <id>]", commandGroupGovernance, "inspect pending approvals and approve or deny them", []string{"permission", "policy", "safety"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		switch len(args) {
		case 0:
			return CommandResult{}, repl.renderApprovals(ctx, "pending", 10)
		default:
			if len(args) != 2 {
				return CommandResult{}, fmt.Errorf("usage: /approvals [approve|deny <id>]")
			}
			switch strings.ToLower(strings.TrimSpace(args[0])) {
			case "approve":
				return CommandResult{}, repl.resolveApproval(ctx, args[1], true)
			case "deny":
				return CommandResult{}, repl.resolveApproval(ctx, args[1], false)
			default:
				return CommandResult{}, fmt.Errorf("usage: /approvals [approve|deny <id>]")
			}
		}
	})
	r.registerDetailed("quality", "Show quality summary and release readiness", "/quality", commandGroupGovernance, "show quality metrics and readiness", []string{"release", "readiness", "metrics"}, func(ctx context.Context, repl *REPL, _ []string) (CommandResult, error) {
		return CommandResult{}, repl.renderQualitySnapshot(ctx)
	})
	r.registerDetailed("evals", "List evaluation suites or run one", "/evals [run <suite_id>]", commandGroupGovernance, "inspect eval suites and run an eval", []string{"evaluation", "tests", "benchmarks"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) == 0 {
			return CommandResult{}, repl.renderEvalSuites(ctx)
		}
		if len(args) == 2 && strings.EqualFold(strings.TrimSpace(args[0]), "run") {
			return CommandResult{}, repl.runEvalSuite(ctx, args[1])
		}
		return CommandResult{}, fmt.Errorf("usage: /evals [run <suite_id>]")
	})
	r.registerDetailed("runs", "List recent runs", "/runs [recent]", commandGroupInfo, "list recent runs", []string{"jobs", "executions", "recent"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) > 0 && !strings.EqualFold(strings.TrimSpace(args[0]), "recent") {
			return CommandResult{}, fmt.Errorf("usage: /runs [recent]")
		}
		sessionID := ""
		if len(args) == 0 {
			resolved, err := repl.currentServiceSessionID(ctx)
			if err != nil {
				return CommandResult{}, err
			}
			if resolved == "" {
				repl.renderer.SystemLine("No runs found.")
				return CommandResult{}, nil
			}
			sessionID = resolved
		}
		return CommandResult{}, repl.renderRuns(ctx, sessionID, 10)
	})
	r.registerDetailed("run", "Show details for a specific run", "/run <id>", commandGroupInfo, "show output, delivery, and scope for a specific run", []string{"jobs", "executions", "detail", "delivery"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) != 1 {
			return CommandResult{}, fmt.Errorf("usage: /run <id>")
		}
		return CommandResult{}, repl.renderRunDetail(ctx, args[0])
	})
	r.registerDetailed("last", "Show the latest run details", "/last", commandGroupInfo, "show the last run in the current conversation", []string{"recent", "latest", "completion"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) == 2 && args[0] == internalInspectArg {
			return CommandResult{}, repl.renderRunDetail(ctx, args[1])
		}
		if len(args) != 0 {
			return CommandResult{}, fmt.Errorf("usage: /last")
		}
		return CommandResult{}, repl.renderLastRun(ctx)
	})
	r.registerDetailed("automation", "Inspect automation items", "/automation [pause|resume|run <kind> <id>]", commandGroupInfo, "inspect automation schedules and operator actions", []string{"cron", "watch", "wakeup", "operator"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) == 0 {
			return CommandResult{}, repl.renderAutomationPanel(ctx)
		}
		sub := strings.ToLower(strings.TrimSpace(args[0]))
		if len(args) < 3 {
			return CommandResult{}, fmt.Errorf("usage: /automation %s <kind> <id>", sub)
		}
		kind, id := strings.TrimSpace(args[1]), strings.TrimSpace(args[2])
		switch sub {
		case "pause":
			if err := repl.service.PauseAutomation(ctx, kind, id); err != nil {
				return CommandResult{}, err
			}
			repl.renderer.SystemLine("Paused " + kind + " " + id + ".")
		case "resume":
			if err := repl.service.ResumeAutomation(ctx, kind, id); err != nil {
				return CommandResult{}, err
			}
			repl.renderer.SystemLine("Resumed " + kind + " " + id + ".")
		case "run", "trigger":
			if err := repl.service.RunAutomationNow(ctx, kind, id); err != nil {
				return CommandResult{}, err
			}
			repl.renderer.SystemLine("Triggered " + kind + " " + id + ".")
		default:
			return CommandResult{}, fmt.Errorf("usage: /automation [pause|resume|run <kind> <id>]")
		}
		return CommandResult{}, nil
	})
	r.registerDetailed("promote", "Promote a recent run into automation", "/promote [schedule]", commandGroupTaskControl, "turn a recent successful run into a cron automation", []string{"automation", "cron", "schedule"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		return CommandResult{}, repl.renderPromotePanel(ctx, strings.TrimSpace(strings.Join(args, " ")))
	})
	r.registerDetailed("delivery", "Inspect delivery status and receipts", "/delivery [status|list|redrive <id>]", commandGroupGovernance, "inspect delivery targets, receipts, and redrive dead-letter items", []string{"receipt", "redrive", "dead-letter"}, func(ctx context.Context, repl *REPL, args []string) (CommandResult, error) {
		if len(args) == 0 {
			return CommandResult{}, repl.renderDeliveryPanel(ctx)
		}
		sub := strings.ToLower(strings.TrimSpace(args[0]))
		switch sub {
		case "status", "list":
			if len(args) != 1 {
				return CommandResult{}, fmt.Errorf("usage: /delivery %s", sub)
			}
			return CommandResult{}, repl.renderDeliveryPanel(ctx)
		case "redrive":
			if len(args) < 2 {
				return CommandResult{}, fmt.Errorf("usage: /delivery redrive <id>")
			}
			id := strings.TrimSpace(args[1])
			result, err := repl.service.RedriveDelivery(ctx, id)
			if err != nil {
				return CommandResult{}, err
			}
			if result == nil {
				return CommandResult{}, fmt.Errorf("delivery %q is unavailable or cannot be redriven", id)
			}
			repl.renderer.SystemLine(fmt.Sprintf("Redriven %d, failed %d.", result.Redriven, result.Failed))
			return CommandResult{}, nil
		default:
			return CommandResult{}, fmt.Errorf("usage: /delivery [status|list|redrive <id>]")
		}
	})
	r.registerDetailed("doctor", "Run REPL health checks", "/doctor", commandGroupInfo, "show health diagnostics", []string{"health", "diagnose", "checks"}, func(ctx context.Context, repl *REPL, _ []string) (CommandResult, error) {
		return CommandResult{}, repl.renderDoctorChecks(ctx)
	})
	r.register("exit", "Exit the REPL", "/exit", func(context.Context, *REPL, []string) (CommandResult, error) {
		return CommandResult{Exit: true}, nil
	})
	r.register("quit", "Alias of /exit", "/quit", func(context.Context, *REPL, []string) (CommandResult, error) {
		return CommandResult{Exit: true}, nil
	})
}

func handleBadgeCommand(_ context.Context, repl *REPL, args []string) (CommandResult, error) {
	if repl.badgeMgr == nil || repl.badgeRdr == nil {
		return CommandResult{}, fmt.Errorf("badge system is unavailable")
	}
	if len(args) == 0 {
		return CommandResult{}, repl.renderBadgePanel()
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "show":
		if len(args) != 1 {
			return CommandResult{}, fmt.Errorf("usage: /badge show")
		}
		repl.badgeHidden = false
		repl.renderer.SystemLine("Badge shown for this session.")
		if !repl.badgeMgr.Enabled() {
			repl.renderer.SystemLine("Badge is off globally. Run /badge on to enable it by default.")
		}
		if repl.badgeRdr.Supported() {
			repl.renderBadge()
		}
		return CommandResult{}, nil
	case "hide":
		if len(args) != 1 {
			return CommandResult{}, fmt.Errorf("usage: /badge hide")
		}
		repl.badgeHidden = true
		repl.clearBadge()
		repl.renderer.SystemLine("Badge hidden for this session.")
		return CommandResult{}, nil
	case "on", "enable":
		if len(args) != 1 {
			return CommandResult{}, fmt.Errorf("usage: /badge on")
		}
		repl.badgeMgr.SetEnabled(true)
		if err := repl.badgeMgr.Save(); err != nil {
			return CommandResult{}, err
		}
		repl.badgeHidden = false
		repl.renderer.SystemLine("Badge enabled. It will be shown by default in future terminal sessions.")
		if repl.badgeRdr.Supported() {
			repl.renderBadge()
		}
		return CommandResult{}, nil
	case "off", "disable":
		if len(args) != 1 {
			return CommandResult{}, fmt.Errorf("usage: /badge off")
		}
		repl.badgeMgr.SetEnabled(false)
		if err := repl.badgeMgr.Save(); err != nil {
			return CommandResult{}, err
		}
		repl.badgeHidden = true
		repl.clearBadge()
		repl.renderer.SystemLine("Badge disabled. It will stay hidden by default in future terminal sessions.")
		return CommandResult{}, nil
	case "color":
		if len(args) == 1 {
			repl.renderer.SystemLine("Badge color: " + repl.badgeMgr.Config().Color)
			return CommandResult{}, nil
		}
		if len(args) != 2 {
			return CommandResult{}, fmt.Errorf("usage: /badge color <hex>")
		}
		if err := repl.badgeMgr.SetColor(args[1]); err != nil {
			return CommandResult{}, err
		}
		if err := repl.badgeMgr.Save(); err != nil {
			return CommandResult{}, err
		}
		if err := repl.syncBadgeRenderer(); err != nil {
			return CommandResult{}, err
		}
		repl.badgeHidden = false
		repl.renderer.SystemLine("Badge color set to " + repl.badgeMgr.Config().Color + ".")
		if repl.badgeRdr.Supported() {
			repl.renderBadge()
		}
		return CommandResult{}, nil
	case "size":
		if len(args) == 1 {
			repl.renderer.SystemLine(fmt.Sprintf("Badge size: %d cells", repl.badgeMgr.Config().Size))
			return CommandResult{}, nil
		}
		if len(args) != 2 {
			return CommandResult{}, fmt.Errorf("usage: /badge size <n>")
		}
		size, err := strconv.Atoi(strings.TrimSpace(args[1]))
		if err != nil {
			return CommandResult{}, fmt.Errorf("badge size must be an integer")
		}
		if err := repl.badgeMgr.SetSize(size); err != nil {
			return CommandResult{}, err
		}
		if err := repl.badgeMgr.Save(); err != nil {
			return CommandResult{}, err
		}
		if err := repl.syncBadgeRenderer(); err != nil {
			return CommandResult{}, err
		}
		repl.badgeHidden = false
		repl.renderer.SystemLine(fmt.Sprintf("Badge size set to %d cells.", repl.badgeMgr.Config().Size))
		if repl.badgeRdr.Supported() {
			repl.renderBadge()
		}
		return CommandResult{}, nil
	case "import":
		if len(args) < 2 || len(args) > 3 {
			return CommandResult{}, fmt.Errorf("usage: /badge import <path> [slot]")
		}
		slot := 0
		var err error
		if len(args) == 3 {
			slot, err = parseBadgeSlotArg(args[2])
			if err != nil {
				return CommandResult{}, err
			}
			if err := validateBadgeSlotIndex(repl.badgeMgr, slot); err != nil {
				return CommandResult{}, err
			}
		} else {
			var ok bool
			slot, ok = firstEmptyBadgeSlot(repl.badgeMgr.ListSlots())
			if !ok {
				return CommandResult{}, fmt.Errorf("no empty custom badge slots available")
			}
		}
		if err := repl.badgeMgr.ImportImage(slot, args[1]); err != nil {
			return CommandResult{}, err
		}
		repl.renderer.SystemLine(fmt.Sprintf("Imported badge into custom-%d.", slot))
		repl.renderer.SystemLine("Source: " + args[1])
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			repl.renderer.SystemLine(fmt.Sprintf("Stored: %s", filepath.Join(home, ".hopclaw", "avatars", fmt.Sprintf("custom-%d.png", slot))))
		}
		active := "no"
		if repl.badgeMgr.Current() == fmt.Sprintf("custom-%d", slot) && !repl.badgeHidden {
			active = "yes"
		}
		repl.renderer.SystemLine("Active: " + active)
		return CommandResult{}, nil
	case "remove":
		confirmed := false
		if len(args) == 3 && args[1] == internalConfirmedArg {
			confirmed = true
			args = []string{args[0], args[2]}
		}
		if len(args) != 2 {
			return CommandResult{}, fmt.Errorf("usage: /badge remove <slot>")
		}
		slot, err := parseBadgeSlotArg(args[1])
		if err != nil {
			return CommandResult{}, err
		}
		if err := validateBadgeSlotIndex(repl.badgeMgr, slot); err != nil {
			return CommandResult{}, err
		}
		if !confirmed {
			ok, err := repl.confirmBadgeRemoval(slot)
			if err != nil {
				return CommandResult{}, err
			}
			if !ok {
				return CommandResult{}, nil
			}
		}
		if err := repl.badgeMgr.RemoveImage(slot); err != nil {
			return CommandResult{}, err
		}
		if err := repl.badgeMgr.Save(); err != nil {
			return CommandResult{}, err
		}
		if err := repl.syncBadgeRenderer(); err != nil {
			return CommandResult{}, err
		}
		repl.renderer.SystemLine(fmt.Sprintf("Removed custom-%d.", slot))
		if repl.badgeRdr.Supported() && !repl.badgeHidden {
			repl.renderBadge()
		}
		return CommandResult{}, nil
	case "set":
		if len(args) != 2 {
			return CommandResult{}, fmt.Errorf("usage: /badge set <id>")
		}
		if err := repl.badgeMgr.SetCurrent(args[1]); err != nil {
			return CommandResult{}, err
		}
		repl.badgeHidden = false
		repl.renderer.SystemLine("Badge set to " + repl.badgeMgr.Current() + ".")
		if repl.badgeRdr.Supported() {
			repl.renderBadge()
		}
		return CommandResult{}, nil
	case "reset":
		if len(args) != 1 {
			return CommandResult{}, fmt.Errorf("usage: /badge reset")
		}
		defaults := badgepkg.DefaultConfig()
		if err := repl.badgeMgr.SetCurrent(defaultCurrent); err != nil {
			return CommandResult{}, err
		}
		if err := repl.badgeMgr.SetColor(defaults.Color); err != nil {
			return CommandResult{}, err
		}
		if err := repl.badgeMgr.SetSize(defaults.Size); err != nil {
			return CommandResult{}, err
		}
		repl.badgeMgr.SetEnabled(defaults.Enabled)
		if err := repl.badgeMgr.Save(); err != nil {
			return CommandResult{}, err
		}
		if err := repl.syncBadgeRenderer(); err != nil {
			return CommandResult{}, err
		}
		repl.badgeHidden = !defaults.Enabled
		repl.renderer.SystemLine("Badge reset to defaults.")
		if repl.badgeRdr.Supported() && !repl.badgeHidden {
			repl.renderBadge()
		}
		return CommandResult{}, nil
	case "help":
		if len(args) != 1 {
			return CommandResult{}, fmt.Errorf("usage: /badge help")
		}
		renderBadgeHelp(repl)
		return CommandResult{}, nil
	default:
		return CommandResult{}, fmt.Errorf("usage: /badge [on|off|show|hide|color|size|import|remove|set|reset|help]")
	}
}

const defaultCurrent = "A"

func renderBadgeHelp(repl *REPL) {
	cfg := repl.badgeMgr.Config()
	repl.renderer.SystemLine(fmt.Sprintf("Badge: %s · color %s · size %d · enabled %t", repl.badgeMgr.Current(), cfg.Color, cfg.Size, cfg.Enabled))
	repl.renderer.SystemLine("/badge on")
	repl.renderer.SystemLine("/badge off")
	repl.renderer.SystemLine("/badge show")
	repl.renderer.SystemLine("/badge hide")
	repl.renderer.SystemLine("/badge color [#RRGGBB]")
	repl.renderer.SystemLine("/badge size [2-6]")
	repl.renderer.SystemLine("/badge import <path> [slot]")
	repl.renderer.SystemLine("/badge remove <slot>")
	repl.renderer.SystemLine("/badge set <A-Z|custom-N>")
	repl.renderer.SystemLine("/badge reset")
	repl.renderer.SystemLine("/badge help")
}

func parseBadgeSlotArg(value string) (int, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "custom-")
	slot, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("badge slot must be an integer or custom-N")
	}
	return slot, nil
}

func validateBadgeSlotIndex(mgr *badgepkg.Manager, slot int) error {
	if mgr == nil {
		return fmt.Errorf("badge system is unavailable")
	}
	maxSlot := -1
	for _, item := range mgr.ListSlots() {
		if item.Kind != badgepkg.SlotCustom {
			continue
		}
		if item.Index > maxSlot {
			maxSlot = item.Index
		}
	}
	if slot < 0 || slot > maxSlot {
		return fmt.Errorf("badge slot must be between 0 and %d", maxSlot)
	}
	return nil
}

func firstEmptyBadgeSlot(slots []badgepkg.Slot) (int, bool) {
	for _, slot := range slots {
		if slot.Kind == badgepkg.SlotCustom && !slot.Occupied {
			return slot.Index, true
		}
	}
	return 0, false
}

func (r *CommandRegistry) register(name, description, usage string, handler CommandHandler) {
	r.registerDetailed(name, description, usage, commandGroupInfo, description, nil, handler)
}

func (r *CommandRegistry) registerDetailed(name, description, usage, group, intent string, keywords []string, handler CommandHandler) {
	r.system[name] = commandEntry{
		Command: acp.Command{
			Name:        name,
			Description: description,
			Shortcut:    "/" + name,
		},
		Usage:    usage,
		Group:    strings.TrimSpace(group),
		Intent:   strings.TrimSpace(intent),
		Keywords: append([]string(nil), keywords...),
		Handler:  handler,
	}
}

func remoteCommandError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", remoteCommandErrorReplacer.Replace(err.Error()))
}

func (r *CommandRegistry) SetDynamic(commands []acp.Command) {
	r.setDynamic(commands, false)
}

func (r *CommandRegistry) SetDynamicFromRuntimeInventory(commands []acp.Command) {
	r.setDynamic(commands, true)
}

func (r *CommandRegistry) setDynamic(commands []acp.Command, filterRuntimeDefaults bool) {
	r.dynamic = make(map[string]acp.Command, len(commands))
	for _, command := range commands {
		name := strings.TrimPrefix(strings.TrimSpace(command.Name), "/")
		if name == "" {
			continue
		}
		name = strings.ToLower(name)
		if _, exists := r.system[name]; exists {
			continue
		}
		if filterRuntimeDefaults {
			if _, reserved := defaultACPInventoryCommandNames[name]; reserved {
				continue
			}
		}
		command.Name = name
		if command.Shortcut == "" {
			command.Shortcut = "/" + name
		}
		r.dynamic[name] = command
	}
}

func (r *CommandRegistry) Execute(ctx context.Context, repl *REPL, input string) (CommandResult, error) {
	if repl != nil && repl.commands == nil {
		repl.commands = r
	}
	if !strings.HasPrefix(strings.TrimSpace(input), "/") {
		return CommandResult{Submit: input}, nil
	}
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(input, "/")))
	if len(fields) == 0 {
		return CommandResult{}, nil
	}

	name := strings.ToLower(fields[0])
	name = canonicalCommandName(name)
	args := fields[1:]

	if entry, ok := r.system[name]; ok {
		return entry.Handler(ctx, repl, args)
	}
	if command, ok := r.dynamic[name]; ok {
		return CommandResult{}, r.dynamicCommandError(command)
	}
	return CommandResult{}, r.unknownCommandError(name)
}

func (r *CommandRegistry) Suggestions(input string) []acp.Command {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return nil
	}
	prefix := strings.ToLower(strings.TrimPrefix(input, "/"))
	commands := r.All()
	out := make([]acp.Command, 0, len(commands))
	for _, command := range commands {
		name := strings.ToLower(command.Name)
		if prefix == "" || strings.HasPrefix(name, prefix) {
			out = append(out, command)
		}
	}
	slices.SortFunc(out, func(a, b acp.Command) int {
		return strings.Compare(a.Name, b.Name)
	})
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func (r *CommandRegistry) Complete(input string) string {
	suggestions := r.Suggestions(input)
	if len(suggestions) == 0 {
		return input
	}
	if len(suggestions) == 1 {
		return "/" + suggestions[0].Name + " "
	}
	common := suggestions[0].Name
	for _, item := range suggestions[1:] {
		common = commonPrefix(common, item.Name)
		if common == "" {
			break
		}
	}
	if common == "" {
		return input
	}
	return "/" + common
}

func (r *CommandRegistry) All() []acp.Command {
	return r.SystemCommands()
}

func (r *CommandRegistry) SystemCommands() []acp.Command {
	out := make([]acp.Command, 0, len(r.system))
	for _, entry := range r.system {
		out = append(out, entry.Command)
	}
	slices.SortFunc(out, func(a, b acp.Command) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func (r *CommandRegistry) DynamicCommands() []acp.Command {
	out := make([]acp.Command, 0, len(r.dynamic))
	for _, command := range r.dynamic {
		out = append(out, command)
	}
	slices.SortFunc(out, func(a, b acp.Command) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func (r *CommandRegistry) AdvisoryDynamicCommands() []acp.Command {
	out := make([]acp.Command, 0, len(r.dynamic))
	for _, command := range r.dynamic {
		if _, exists := r.system[command.Name]; exists {
			continue
		}
		out = append(out, command)
	}
	slices.SortFunc(out, func(a, b acp.Command) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func (r *CommandRegistry) Describe(name string) (acp.Command, string, bool) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if entry, ok := r.system[name]; ok {
		return entry.Command, entry.Usage, true
	}
	if aliasTarget, aliased := commandAliases[name]; aliased {
		if entry, ok := r.system[aliasTarget]; ok {
			command := entry.Command
			command.Name = name
			command.Shortcut = "/" + name
			command.Description = "Alias of /" + aliasTarget
			usage := "/" + name
			if strings.Contains(entry.Usage, " ") {
				usage += entry.Usage[strings.Index(entry.Usage, " "):]
			}
			return command, usage, true
		}
	}
	return acp.Command{}, "", false
}

func (r *CommandRegistry) DynamicCommand(name string) (acp.Command, bool) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	command, ok := r.dynamic[name]
	return command, ok
}

func (r *CommandRegistry) GroupFor(name string) string {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	name = canonicalCommandName(name)
	if entry, ok := r.system[name]; ok && strings.TrimSpace(entry.Group) != "" {
		return entry.Group
	}
	switch name {
	case "pause", "cancel", "continue", "retry", "discard":
		return commandGroupTaskControl
	case "session", "history", "context", "compact", "episode", "reset":
		return commandGroupSession
	case "remote", "model", "think", "cd":
		return commandGroupRuntime
	case "approvals", "quality", "evals":
		return commandGroupGovernance
	case "badge":
		return commandGroupIdentity
	default:
		return commandGroupInfo
	}
}

func (r *CommandRegistry) CommandsByGroup(group string) []commandEntry {
	group = strings.TrimSpace(group)
	out := make([]commandEntry, 0, 8)
	for _, entry := range r.system {
		if strings.EqualFold(entry.Group, group) {
			out = append(out, entry)
		}
	}
	slices.SortFunc(out, func(a, b commandEntry) int {
		return strings.Compare(a.Command.Name, b.Command.Name)
	})
	return out
}

func (r *CommandRegistry) HintFor(name string) string {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if aliasTarget, aliased := commandAliases[name]; aliased {
		return "alias of /" + aliasTarget
	}
	if entry, ok := r.system[name]; ok {
		return defaultString(entry.Intent, entry.Command.Description)
	}
	if command, ok := r.dynamic[name]; ok {
		return strings.TrimSpace(command.Description)
	}
	return ""
}

func (r *CommandRegistry) Search(query string, limit int) []acp.Command {
	query = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(query, "/")))
	if query == "" {
		return nil
	}
	type scoredCommand struct {
		command acp.Command
		score   int
	}
	scored := make([]scoredCommand, 0, len(r.system))
	for _, entry := range r.system {
		score, ok := commandSearchScore(query, entry.Command.Name, entry.Command.Description, entry.Intent, entry.Group, entry.Keywords)
		if ok {
			scored = append(scored, scoredCommand{command: entry.Command, score: score})
		}
	}
	slices.SortFunc(scored, func(a, b scoredCommand) int {
		if a.score != b.score {
			return a.score - b.score
		}
		return strings.Compare(a.command.Name, b.command.Name)
	})
	if limit <= 0 || limit > len(scored) {
		limit = len(scored)
	}
	out := make([]acp.Command, 0, limit)
	for _, item := range scored[:limit] {
		out = append(out, item.command)
	}
	return out
}

func canonicalCommandName(name string) string {
	name = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "/")
	if aliasTarget, aliased := commandAliases[name]; aliased {
		return aliasTarget
	}
	return name
}

func (r *CommandRegistry) SearchDynamic(query string, limit int) []acp.Command {
	query = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(query, "/")))
	if query == "" {
		return nil
	}
	type scoredCommand struct {
		command acp.Command
		score   int
	}
	commands := r.AdvisoryDynamicCommands()
	scored := make([]scoredCommand, 0, len(commands))
	for _, command := range commands {
		score, ok := commandSearchScore(query, command.Name, command.Description, "runtime inventory", "Runtime Inventory", nil)
		if ok {
			scored = append(scored, scoredCommand{command: command, score: score})
		}
	}
	slices.SortFunc(scored, func(a, b scoredCommand) int {
		if a.score != b.score {
			return a.score - b.score
		}
		return strings.Compare(a.command.Name, b.command.Name)
	})
	if limit <= 0 || limit > len(scored) {
		limit = len(scored)
	}
	out := make([]acp.Command, 0, limit)
	for _, item := range scored[:limit] {
		out = append(out, item.command)
	}
	return out
}

func (r *CommandRegistry) unknownCommandError(name string) error {
	if replacement, ok := deprecatedCommandReplacements[name]; ok {
		return fmt.Errorf("unknown command %q. Use /%s instead", "/"+name, replacement)
	}
	matches := r.fuzzyMatches(name, 3)
	if len(matches) == 0 {
		return fmt.Errorf("unknown command %q", "/"+name)
	}
	parts := make([]string, 0, len(matches))
	for _, item := range matches {
		parts = append(parts, "/"+item.Name)
	}
	return fmt.Errorf("unknown command %q. Did you mean %s?", "/"+name, strings.Join(parts, ", "))
}

func (r *CommandRegistry) dynamicCommandError(command acp.Command) error {
	entryName := strings.TrimPrefix(strings.TrimSpace(command.Name), "/")
	name := "/" + entryName
	description := strings.TrimSpace(command.Description)
	if description == "" {
		return fmt.Errorf("%q is not a REPL slash command. The connected runtime advertises an inventory entry named %q", name, entryName)
	}
	return fmt.Errorf("%q is not a REPL slash command. The connected runtime advertises an inventory entry named %q (%s)", name, entryName, description)
}

func (r *CommandRegistry) fuzzyMatches(query string, limit int) []acp.Command {
	query = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(query, "/")))
	if query == "" {
		return nil
	}
	type scoredCommand struct {
		command acp.Command
		score   int
	}
	scored := make([]scoredCommand, 0, len(r.system)+len(r.dynamic))
	for _, command := range r.All() {
		name := strings.ToLower(strings.TrimSpace(command.Name))
		if name == "" {
			continue
		}
		score := levenshteinDistance(query, name)
		if strings.HasPrefix(name, query) {
			score = 0
		}
		if score > 4 {
			continue
		}
		scored = append(scored, scoredCommand{command: command, score: score})
	}
	slices.SortFunc(scored, func(a, b scoredCommand) int {
		if a.score != b.score {
			return a.score - b.score
		}
		return strings.Compare(a.command.Name, b.command.Name)
	})
	if limit <= 0 || limit > len(scored) {
		limit = len(scored)
	}
	out := make([]acp.Command, 0, limit)
	for _, item := range scored[:limit] {
		out = append(out, item.command)
	}
	return out
}

func commandSearchScore(query, name, description, intent, group string, keywords []string) (int, bool) {
	fields := []string{
		strings.ToLower(strings.TrimSpace(name)),
		strings.ToLower(strings.TrimSpace(description)),
		strings.ToLower(strings.TrimSpace(intent)),
		strings.ToLower(strings.TrimSpace(group)),
	}
	for _, keyword := range keywords {
		fields = append(fields, strings.ToLower(strings.TrimSpace(keyword)))
	}
	best := 100
	for index, field := range fields {
		switch {
		case field == "":
			continue
		case field == query:
			return index, true
		case strings.HasPrefix(field, query):
			if index < best {
				best = index
			}
		case strings.Contains(field, query):
			if index+10 < best {
				best = index + 10
			}
		}
	}
	return best, best != 100
}

func levenshteinDistance(left, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	if len(leftRunes) == 0 {
		return len(rightRunes)
	}
	if len(rightRunes) == 0 {
		return len(leftRunes)
	}
	column := make([]int, len(rightRunes)+1)
	for j := range column {
		column[j] = j
	}
	for i, leftRune := range leftRunes {
		prevDiagonal := column[0]
		column[0] = i + 1
		for j, rightRune := range rightRunes {
			insertCost := column[j+1] + 1
			deleteCost := column[j] + 1
			replaceCost := prevDiagonal
			if leftRune != rightRune {
				replaceCost++
			}
			prevDiagonal = column[j+1]
			column[j+1] = min(insertCost, min(deleteCost, replaceCost))
		}
	}
	return column[len(rightRunes)]
}

func commonPrefix(left, right string) string {
	runesLeft := []rune(left)
	runesRight := []rune(right)
	limit := min(len(runesLeft), len(runesRight))
	end := 0
	for end < limit && runesLeft[end] == runesRight[end] {
		end++
	}
	return string(runesLeft[:end])
}
