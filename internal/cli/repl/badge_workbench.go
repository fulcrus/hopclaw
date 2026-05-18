package repl

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

func (r *REPL) renderBadge() {
	show, row := r.badgeDisplayAnchor()
	if !show {
		r.clearBadge()
		return
	}
	if err := r.syncBadgeRenderer(); err != nil {
		return
	}
	img, err := r.badgeMgr.GetCurrentImage()
	if err != nil {
		return
	}
	r.badgeRdr.SetRow(row)
	r.renderer.mu.Lock()
	defer r.renderer.mu.Unlock()
	_ = r.badgeRdr.Show(img, r.terminalWidth())
}

func (r *REPL) clearBadge() {
	if r == nil || r.badgeRdr == nil || !r.badgeRdr.Supported() || r.renderer == nil {
		return
	}
	r.renderer.mu.Lock()
	defer r.renderer.mu.Unlock()
	_ = r.badgeRdr.Clear(r.terminalWidth())
}

func (r *REPL) syncBadgeRenderer() error {
	if r.badgeMgr == nil || r.badgeRdr == nil {
		return nil
	}
	cfg := r.badgeMgr.Config()
	return r.badgeRdr.SetAppearance(cfg.Color, cfg.Size)
}

func (r *REPL) startBadgeResizeListener() func() {
	if r.badgeRdr == nil || !r.badgeRdr.Supported() {
		return func() {}
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				return
			case <-signals:
				r.renderBadge()
			}
		}
	}()

	return func() {
		close(done)
		signal.Stop(signals)
	}
}

func (r *REPL) terminalWidth() int {
	width, _, err := termGetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 80
	}
	return width
}

func (r *REPL) badgeDisplayAnchor() (bool, int) {
	if r == nil || r.badgeMgr == nil || r.badgeRdr == nil || r.renderer == nil {
		return false, 0
	}
	if r.badgeHidden || !r.badgeRdr.Supported() || !r.usesPromptWorkbench() {
		return false, 0
	}
	r.refreshViewState()
	state := r.viewState
	mode := resolveDockLayout(state, true)
	if mode != LayoutFull || !badgeVisible(state) {
		return false, 0
	}
	_, height, err := termGetSize(int(os.Stdout.Fd()))
	if err != nil || height <= 0 {
		height = 24
	}
	size := resolveBadgeSize(r.badgeMgr.Config().Size)
	return true, max(1, height-size+1)
}

func (r *REPL) currentBadgeLabel() string {
	if r.badgeMgr == nil {
		return ""
	}
	if r.badgeHidden {
		return "off"
	}
	return r.badgeMgr.Current()
}

func (r *REPL) renderDock() {
	if r.oneShot {
		return
	}
	if r.usesPromptWorkbench() {
		return
	}
	// Never redraw workbench while streaming in TTY mode — it pollutes scroll
	// history with repeated status blocks and corrupts cursor position.
	if r.running && r.renderer != nil && r.renderer.tty {
		return
	}
	r.refreshSupervisorProjection(context.Background(), false)
	r.refreshTransparencyProjection(context.Background(), false)
	r.refreshViewState()
	if r.renderer != nil {
		r.renderer.RenderDock(r.viewState)
	}
}

func (r *REPL) usesPromptWorkbench() bool {
	if r == nil || r.renderer == nil || !r.renderer.tty {
		return false
	}
	terminalPrompter, ok := r.prompter.(*TerminalPrompter)
	return ok && terminalPrompter != nil && terminalPrompter.tty
}

func (r *REPL) suppressPromptWorkbenchRuntimeNoise() bool {
	return r != nil && r.running && r.usesPromptWorkbench()
}

func (r *REPL) shouldRenderPassiveWorkbench() bool {
	if r == nil || !r.usesPromptWorkbench() {
		return false
	}
	if r.pendingApproval {
		return true
	}
	switch r.phase {
	case PhaseThinking, PhasePlanning, PhaseExecutingTools, PhaseProcessingResults, PhaseDelivering:
		return true
	default:
		return false
	}
}

func promptWorkbenchDockState(state REPLViewState) REPLViewState {
	switch normalizedExecutionState(state.ExecutionState) {
	case "running":
		// Keep the passive workbench stable while the editor is detached.
		state.Elapsed = ""
		state.Duration = ""
		state.LastTool = ""
		state.Phase = ""
		state.Quality = ""
	}
	return state
}

func (r *REPL) promptChrome(termWidth int) richedit.Chrome {
	if r == nil || !r.usesPromptWorkbench() {
		return richedit.Chrome{}
	}
	r.refreshViewState()
	state := promptWorkbenchDockState(r.viewState)
	if termWidth > 0 {
		state.TerminalWidth = termWidth
	}
	return promptWorkbenchChrome(state, max(termWidth, 80), true)
}

func promptWorkbenchChrome(state REPLViewState, termWidth int, tty bool) richedit.Chrome {
	if termWidth <= 0 {
		termWidth = 80
	}
	top, bottom := promptWorkbenchChromeLines(state, termWidth, tty)

	return richedit.Chrome{
		Top:    top,
		Bottom: bottom,
	}
}
