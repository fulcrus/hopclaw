package repl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	badgepkg "github.com/fulcrus/hopclaw/internal/cli/badge"
	"golang.org/x/term"
)

const spinnerInterval = 120 * time.Millisecond
const escKey = rune(0x1b)
const projectionRefreshTTL = 2 * time.Second
const readinessRefreshTTL = 10 * time.Second
const transparencyRefreshTTL = 4 * time.Second

var timeAfter = time.After
var termGetSize = term.GetSize

const commandSyncTimeout = 150 * time.Millisecond

var errREPLExitRequested = errors.New("repl exit requested")
var errREPLBackgrounded = errors.New("repl run backgrounded")

type runKeyListenerFactory func() (<-chan rune, func(), error)

type Config struct {
	Client         *acp.InProcessClient
	Service        Service
	Target         string
	TargetManager  TargetManager
	Prompter       Prompter
	Renderer       *Renderer
	History        *History
	Prompt         *DynamicPrompt
	Version        string
	SessionKey     string
	InitialMessage string
	OneShot        bool
	Model          string
	Thinking       bool
}

type REPL struct {
	client                   *acp.InProcessClient
	service                  Service
	prompter                 Prompter
	renderer                 *Renderer
	history                  *History
	prompt                   *DynamicPrompt
	targetName               string
	targetKind               string
	targetManager            TargetManager
	commands                 *CommandRegistry
	streamer                 *Streamer
	version                  string
	sessionID                string
	serviceSessionID         string
	sessionKey               string
	sessionModel             string
	selectedModel            string
	thinking                 bool
	initialMessage           string
	oneShot                  bool
	running                  bool
	pendingApproval          bool
	lastUsage                *acp.UsageInfo
	modelCache               []ModelInfo
	lastImages               []string
	lastContentBlocks        []contextengine.ContentBlock
	phase                    Phase
	lastPhaseLine            string
	approvalReturn           Phase
	seenReplyText            bool
	pauseRequested           bool
	cancelRequested          bool
	quitConfirmPending       bool
	lastSubmitText           string
	lastSubmitImgs           []string
	lastSubmitContentBlocks  []contextengine.ContentBlock
	runStartedAt             time.Time
	pausedRun                *pausedRunState
	badgeMgr                 *badgepkg.Manager
	badgeRdr                 *badgepkg.Renderer
	badgeHidden              bool
	layoutMode               LayoutMode
	activePanel              string
	panelController          promptPanel
	viewState                REPLViewState
	currentProject           *agent.Project
	textFilter               streamTextFilter
	lastToolStatus           string
	lastRunErr               error
	lastFailure              string
	activeTimeline           *activeToolTimeline
	lastTimeline             []ToolTimelineEntry
	runTimelines             map[string][]ToolTimelineEntry
	lastRunID                string
	currentRunID             string
	foregroundRunID          string
	backgroundRuns           []string
	backgroundRunSessions    map[string]string
	approvalState            approvalDockState
	deliveryState            deliveryDockState
	supervisorSnapshot       *SupervisorSnapshot
	supervisorFetchedAt      time.Time
	readinessSnapshot        *ReadinessSnapshot
	readinessFetchedAt       time.Time
	memoryUsage              []MemoryUsageItem
	memoryUsageFetchedAt     time.Time
	contextPressure          *ContextPressureInfo
	contextPressureFetchedAt time.Time
	lastRunDuration          time.Duration
	snapshotTracker          snapshotTracker
	escCh                    <-chan rune
	escStop                  func()
	escFactory               runKeyListenerFactory
	exitFn                   func(int)
}

type pausedRunState struct {
	Message       string
	Images        []string
	ContentBlocks []contextengine.ContentBlock
	LastStep      string
	RunID         string
}

type approvalDockState struct {
	ID    string
	Tool  string
	Scope string
	Risk  string
}

type deliveryDockState struct {
	State   string
	Summary string
	Next    string
}

type ToolTimelineEntry struct {
	Name     string
	Status   string
	Summary  string
	Duration time.Duration
}

type activeToolTimeline struct {
	Name      string
	Summary   string
	StartedAt time.Time
}

func New(config Config) (*REPL, error) {
	if config.Client == nil {
		return nil, fmt.Errorf("repl client is required")
	}
	if config.Service == nil {
		return nil, fmt.Errorf("repl service is required")
	}
	if config.Renderer == nil {
		config.Renderer = NewRenderer(os.Stdout, isTerminalWriter(os.Stdout))
	}
	if config.History == nil {
		config.History = NewHistory("", 500)
	}
	if config.Prompt == nil {
		config.Prompt = &DynamicPrompt{}
	}
	if config.Prompter == nil {
		config.Prompter = NewTerminalPrompter(os.Stdin, os.Stdout, config.History)
	}
	badgeMgr, err := badgepkg.NewManager()
	if err != nil {
		return nil, err
	}
	if err := badgeMgr.Load(); err != nil {
		return nil, err
	}
	badgeCfg := badgeMgr.Config()
	badgeRdr, err := badgepkg.NewRenderer(config.Renderer.out, badgeMgr.Protocol(), badgeCfg.Color, badgeCfg.Size)
	if err != nil {
		return nil, err
	}
	repl := &REPL{
		client:         config.Client,
		service:        config.Service,
		prompter:       config.Prompter,
		renderer:       config.Renderer,
		history:        config.History,
		prompt:         config.Prompt,
		targetName:     normalizeTargetName(config.Target),
		targetManager:  config.TargetManager,
		commands:       NewCommandRegistry(),
		streamer:       NewStreamer(config.Client.Notifications()),
		version:        config.Version,
		sessionKey:     strings.TrimSpace(config.SessionKey),
		initialMessage: strings.TrimSpace(config.InitialMessage),
		oneShot:        config.OneShot,
		selectedModel:  strings.TrimSpace(config.Model),
		thinking:       config.Thinking,
		badgeMgr:       badgeMgr,
		badgeRdr:       badgeRdr,
		badgeHidden:    !badgeCfg.Enabled,
		layoutMode:     LayoutAuto,
		phase:          PhaseIdle,
		exitFn:         os.Exit,
	}
	if repl.sessionKey == "" {
		repl.sessionKey = "default"
	}
	if repl.targetName == "" {
		repl.targetName = "local"
	}
	if repl.targetManager != nil {
		current := repl.targetManager.CurrentTarget()
		if strings.TrimSpace(current.Name) != "" && strings.EqualFold(repl.targetName, "local") {
			repl.targetName = normalizeTargetName(current.Name)
		}
		repl.targetKind = normalizeTargetKind(current.Kind, repl.targetName)
	}
	if repl.targetKind == "" {
		repl.targetKind = normalizeTargetKind("", repl.targetName)
	}
	repl.prompt.SetTarget(repl.targetName)
	repl.prompt.SetStateProvider(func() REPLViewState {
		repl.refreshViewState()
		return repl.viewState
	})
	if terminalPrompter, ok := repl.prompter.(*TerminalPrompter); ok {
		completer := newComposerCompleter(repl.commands)
		completer.modelNamesFn = repl.completionModelChoices
		completer.remoteNamesFn = repl.completionRemoteChoices
		terminalPrompter.SetCompletionProvider(completer)
		terminalPrompter.SetChromeProvider(repl.promptChrome)
	}
	repl.refreshViewState()
	return repl, nil
}

func (r *REPL) Run(ctx context.Context) error {
	clientVersion := defaultString(strings.TrimSpace(r.version), "dev")
	if _, err := r.client.Initialize(ctx, acp.InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo: acp.Implementation{
			Name:    "hopclaw-repl",
			Version: clientVersion,
		},
	}); err != nil {
		return err
	}
	if err := r.refreshCommands(ctx); err != nil {
		return err
	}
	if err := r.syncCommandsUpdate(ctx); err != nil {
		return err
	}
	if err := r.switchSession(ctx, r.sessionKey, false); err != nil {
		return err
	}
	if err := r.syncCommandsUpdate(ctx); err != nil {
		return err
	}
	_, _ = r.loadModels(ctx)
	stopBadgeResize := func() {}
	if !r.oneShot {
		r.renderBanner()
		r.renderBriefing(ctx)
		if !r.usesPromptWorkbench() {
			r.renderDock()
		}
		stopBadgeResize = r.startBadgeResizeListener()
	}
	defer stopBadgeResize()

	if r.initialMessage != "" {
		cmdResult, err := r.commands.Execute(ctx, r, r.initialMessage)
		if err != nil {
			r.renderer.RenderError(err, "")
			if r.oneShot {
				return nil
			}
		} else if cmdResult.Exit {
			return nil
		} else if strings.TrimSpace(cmdResult.Submit) != "" {
			if err := r.submit(ctx, cmdResult.Submit); err != nil {
				return err
			}
		}
		if r.oneShot {
			return nil
		}
	}

	if r.oneShot {
		return nil
	}

	for {
		if r.phase == PhaseCompleted || r.phase == PhaseCancelled || r.phase == PhaseError {
			r.transitionPhase(PhaseIdle, "")
		}
		if !r.usesPromptWorkbench() {
			r.renderDock()
		} else {
			r.renderBadge()
		}
		if setter, ok := r.prompter.(panelPrompterSetter); ok {
			if r.supportsInteractivePanels() {
				setter.SetOverlayController(r.panelController)
			} else {
				setter.SetOverlayController(nil)
			}
		}
		readResult, err := r.prompter.ReadRichLine(r.prompt.Input(), r.commands)
		if err != nil {
			switch {
			case errors.Is(err, io.EOF):
				return nil
			case err == ErrPromptQuit:
				return nil
			case err == ErrPromptInterrupted:
				continue
			default:
				return err
			}
		}
		hasText := strings.TrimSpace(readResult.Text) != ""
		hasImages := len(readResult.Images) > 0
		hasBlocks := len(readResult.ContentBlocks) > 0
		if !hasText && !hasImages && !hasBlocks {
			if strings.TrimSpace(r.activePanel) != "" {
				r.clearPanel()
				continue
			}
			if r.pausedRun != nil {
				if err := r.resumePaused(ctx, false); err != nil {
					return err
				}
			}
			continue
		}
		if !hasText && (hasImages || hasBlocks) {
			if r.panelController != nil {
				r.clearPanel()
			}
			r.lastImages = append(r.lastImages[:0], readResult.Images...)
			r.lastContentBlocks = append(r.lastContentBlocks[:0], readResult.ContentBlocks...)
			if err := r.submit(ctx, ""); err != nil {
				return err
			}
			continue
		}
		if err := r.history.Add(readResult.Text); err != nil {
			return err
		}
		if r.panelController != nil {
			r.clearPanel()
		}
		cmdResult, err := r.commands.Execute(ctx, r, readResult.Text)
		if err != nil {
			r.renderer.RenderError(err, "")
			continue
		}
		if r.panelController != nil && r.renderer.tty {
			fmt.Fprint(r.renderer.out, "\033[1A\033[2K\r")
		}
		if cmdResult.Exit {
			return nil
		}
		if strings.TrimSpace(cmdResult.Submit) == "" {
			continue
		}
		r.lastImages = append(r.lastImages[:0], readResult.Images...)
		r.lastContentBlocks = append(r.lastContentBlocks[:0], readResult.ContentBlocks...)
		if err := r.submit(ctx, cmdResult.Submit); err != nil {
			return err
		}
	}
}
