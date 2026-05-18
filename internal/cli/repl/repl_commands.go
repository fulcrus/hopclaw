package repl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/cli/imageinput"
	"golang.org/x/term"
)

func (r *REPL) refreshCommands(ctx context.Context) error {
	commands, err := r.service.Commands(ctx)
	if err != nil {
		return err
	}
	r.commands.SetDynamic(commands)
	return nil
}

func (r *REPL) loadModels(ctx context.Context) ([]ModelInfo, error) {
	if len(r.modelCache) > 0 {
		return append([]ModelInfo(nil), r.modelCache...), nil
	}
	models, err := r.service.Models(ctx)
	if err != nil {
		return nil, err
	}
	r.modelCache = append([]ModelInfo(nil), models...)
	return models, nil
}

func (r *REPL) syncCommandsUpdate(ctx context.Context) error {
	timer := time.NewTimer(commandSyncTimeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case commands, ok := <-r.streamer.Commands():
			if !ok {
				return nil
			}
			r.commands.SetDynamicFromRuntimeInventory(commands)
			for {
				select {
				case commands, ok := <-r.streamer.Commands():
					if !ok {
						return nil
					}
					r.commands.SetDynamicFromRuntimeInventory(commands)
				default:
					return nil
				}
			}
		case <-timer.C:
			return nil
		}
	}
}

func (r *REPL) submit(ctx context.Context, input string) error {
	r.textFilter.Reset()
	r.lastToolStatus = ""
	r.seenReplyText = false
	model := r.effectiveModel()
	var images []string
	var contentBlocks []contextengine.ContentBlock
	promptText := input
	_, shellMode := shellHandoffCommand(input)
	if shellMode {
		r.lastImages = nil
		r.lastContentBlocks = nil
	} else if len(r.lastContentBlocks) > 0 {
		contentBlocks = append([]contextengine.ContentBlock(nil), r.lastContentBlocks...)
		r.lastContentBlocks = nil
		r.lastImages = nil
	} else if len(r.lastImages) > 0 {
		images = append([]string(nil), r.lastImages...)
		contentBlocks = BuildLegacyImageContentBlocks("", images)
		r.lastImages = nil
	} else {
		promptText, images = imageinput.ExtractImagePaths(input)
		contentBlocks = BuildLegacyImageContentBlocks(strings.TrimSpace(promptText), images)
	}
	promptText = strings.TrimSpace(promptText)
	if promptText == "" && len(images) == 0 {
		promptText = strings.TrimSpace(input)
	}
	return r.submitPrepared(ctx, promptText, images, contentBlocks, model)
}

func (r *REPL) waitForRun(ctx context.Context) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	if err := r.startEscListener(); err != nil {
		r.renderer.SystemLine("Esc pause is unavailable in this terminal.")
	}
	defer r.stopEscListener()

	for {
		select {
		case <-ctx.Done():
			r.renderer.StopSpinner()
			r.refreshViewState()
			return ctx.Err()
		case commands, ok := <-r.streamer.Commands():
			if ok {
				r.commands.SetDynamicFromRuntimeInventory(commands)
			}
		case req, ok := <-r.streamer.Permissions():
			if !ok {
				return nil
			}
			r.renderer.StopSpinner()
			if req.SessionID != "" && req.SessionID != r.sessionID {
				continue
			}
			if err := r.handlePermission(ctx, req); err != nil {
				return err
			}
			r.refreshViewState()
			r.renderDock()
			r.renderer.StartSpinner("Continuing…")
		case update, ok := <-r.streamer.Updates():
			if !ok {
				r.renderer.StopSpinner()
				return nil
			}
			if update.SessionID != r.sessionID {
				continue
			}
			if err := r.handleUpdate(update); err != nil {
				return err
			}
			r.refreshViewState()
			if update.Status == acp.SessionCompleted || update.Status == acp.SessionError {
				if err := r.drainPendingSideChannels(ctx); err != nil {
					return err
				}
				return nil
			}
		case key, ok := <-r.escCh:
			if !ok {
				r.escCh = nil
				continue
			}
			if err := r.handleRunKey(ctx, key); err != nil {
				return err
			}
		case sig := <-sigCh:
			if !r.running {
				continue
			}
			switch sig {
			case os.Interrupt:
				if err := r.handleRunInterrupt(ctx); err != nil {
					return err
				}
			default:
				return r.quitTerminal(ctx)
			}
		}
	}
}

func (r *REPL) startEscListener() error {
	r.stopEscListener()
	if r == nil {
		return nil
	}
	if r.escFactory != nil {
		ch, stop, err := r.escFactory()
		if err != nil {
			return err
		}
		r.escCh = ch
		r.escStop = stop
		return nil
	}
	if !r.supportsRunKeyListener() {
		return nil
	}
	ch, stop, err := startRunKeyListener(r.runInputFile())
	if err != nil {
		return err
	}
	r.escCh = ch
	r.escStop = stop
	return nil
}

func (r *REPL) stopEscListener() {
	if r == nil {
		return
	}
	if r.escStop != nil {
		r.escStop()
		r.escStop = nil
	}
	r.escCh = nil
}

func (r *REPL) runInputFile() *os.File {
	if prompter, ok := r.prompter.(*TerminalPrompter); ok && prompter != nil && prompter.in != nil {
		return prompter.in
	}
	return os.Stdin
}

func (r *REPL) supportsRunKeyListener() bool {
	if r == nil {
		return false
	}
	if r.escFactory != nil {
		return true
	}
	input := r.runInputFile()
	if input == nil {
		return false
	}
	if prompter, ok := r.prompter.(*TerminalPrompter); ok && prompter != nil && prompter.in == input {
		return prompter.tty
	}
	return term.IsTerminal(int(input.Fd()))
}

func (r *REPL) supportsQuitConfirmation() bool {
	return r != nil && r.renderer != nil && r.renderer.tty && r.supportsRunKeyListener()
}

func (r *REPL) handleRunInterrupt(ctx context.Context) error {
	if r == nil || !r.running {
		return nil
	}
	if !r.supportsQuitConfirmation() {
		return r.quitTerminal(ctx)
	}
	if r.quitConfirmPending {
		return r.quitTerminal(ctx)
	}
	r.renderQuitConfirmation()
	return nil
}

func (r *REPL) handleRunKey(ctx context.Context, key rune) error {
	if r == nil || !r.running {
		return nil
	}
	switch key {
	case 3:
		return r.handleRunInterrupt(ctx)
	case 12:
		r.redrawRunningWorkbench(ctx)
		return nil
	case escKey:
		if r.quitConfirmPending {
			return nil
		}
		return r.requestPause(ctx)
	case 'q', 'Q':
		if r.quitConfirmPending {
			return r.quitTerminal(ctx)
		}
	case 'b', 'B':
		if r.quitConfirmPending {
			r.dismissQuitConfirmation()
			return nil
		}
		return r.backgroundCurrentRun(ctx)
	}
	return nil
}

func (r *REPL) quitTerminal(ctx context.Context) error {
	if r != nil {
		r.quitConfirmPending = false
		if r.running && r.client != nil && strings.TrimSpace(r.sessionID) != "" {
			r.cancelBeforeExit(ctx)
		}
	}
	if r != nil && r.exitFn != nil {
		r.exitFn(0)
		return errREPLExitRequested
	}
	os.Exit(0)
	return nil
}

func (r *REPL) cancelBeforeExit(ctx context.Context) {
	if r == nil || r.client == nil || strings.TrimSpace(r.sessionID) == "" {
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		_ = r.client.Cancel(ctx, acp.CancelParams{SessionID: r.sessionID})
		if attempt == 2 {
			return
		}
		select {
		case <-time.After(15 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
}

func (r *REPL) backgroundCurrentRun(ctx context.Context) error {
	if r == nil || !r.running {
		return nil
	}
	r.ensureCurrentRunID()
	runID := strings.TrimSpace(r.currentRunID)
	if runID == "" {
		return fmt.Errorf("no active run to background")
	}
	r.renderBackgroundedSnapshot()
	if err := r.backgroundRun(ctx, runID); err != nil {
		return err
	}
	r.running = false
	r.transitionPhase(PhaseIdle, "")
	r.renderer.StopSpinner()
	r.renderer.RenderSystemEvent("Backgrounded run " + runID + ".")
	return errREPLBackgrounded
}

func (r *REPL) drainPendingSideChannels(ctx context.Context) error {
	for {
		select {
		case commands, ok := <-r.streamer.Commands():
			if !ok {
				return nil
			}
			r.commands.SetDynamicFromRuntimeInventory(commands)
		case req, ok := <-r.streamer.Permissions():
			if !ok {
				return nil
			}
			if req.SessionID != "" && req.SessionID != r.sessionID {
				continue
			}
			if err := r.handlePermission(ctx, req); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func (r *REPL) handleUpdate(update acp.SessionUpdateNotification) error {
	if runID := strings.TrimSpace(update.RunID); runID != "" {
		r.currentRunID = runID
		r.foregroundRunID = runID
		r.lastRunID = runID
	}
	switch update.Status {
	case acp.SessionStreaming:
		if update.ToolName != "" {
			r.startOrUpdateToolTimeline(update.ToolName, update.ToolOutput)
		}
		if update.TextDelta != "" {
			r.finishActiveToolTimeline("ok", time.Now())
		}
		r.renderer.StopSpinner()
		r.renderModelFailover(update.ModelFailover)
		if update.ToolName != "" {
			r.renderToolStatus(update.ToolName, update.ToolOutput)
		}
		if update.TextDelta != "" {
			if r.phase == PhaseExecutingTools {
				r.transitionPhase(PhaseProcessingResults, "")
			} else if r.phase == PhaseThinking || r.phase == PhasePlanning {
				r.transitionPhase(PhaseDelivering, "")
			} else if r.phase == PhaseProcessingResults && r.seenReplyText {
				r.transitionPhase(PhaseDelivering, "")
			}
			if visible := r.textFilter.Filter(update.TextDelta); visible != "" {
				r.lastToolStatus = ""
				r.seenReplyText = true
				r.renderer.WriteDelta(visible)
			}
		}
	case acp.SessionToolUse:
		r.ensureCurrentRunID()
		r.startOrUpdateToolTimeline(update.ToolName, update.ToolOutput)
		r.transitionPhase(PhaseExecutingTools, update.ToolName)
		r.maybeRenderPlanSnapshot(update.ToolName)
		r.maybeRenderProgressSnapshot(update.ToolName)
		r.renderer.StopSpinner()
		r.renderModelFailover(update.ModelFailover)
		if update.ToolName != "" {
			r.renderToolStatus(update.ToolName, update.ToolOutput)
		}
	case acp.SessionCompleted:
		r.clearQuitConfirmation()
		status := "ok"
		if r.pauseRequested || r.cancelRequested || update.StopReason == acp.StopCancelled {
			status = "cancelled"
		}
		r.finishActiveToolTimeline(status, time.Now())
		r.cacheLatestRunTimeline()
		r.lastUsage = update.Usage
		r.lastRunDuration = time.Since(r.runStartedAt)
		r.renderer.StopSpinner()
		if tail := r.textFilter.Flush(); tail != "" {
			r.renderer.WriteDelta(tail)
		}
		r.lastRunErr = nil
		if !r.pauseRequested && !r.cancelRequested && update.StopReason != acp.StopCancelled {
			r.lastFailure = ""
		}
		r.renderer.Finish()
		if r.pauseRequested {
			r.enterPausedState()
			return nil
		}
		if r.cancelRequested || update.StopReason == acp.StopCancelled {
			r.enterCancelledState()
			return nil
		}
		r.lastToolStatus = ""
		if update.Runless {
			r.deliveryState = deliveryDockState{}
			r.transitionPhase(PhaseIdle, "")
			break
		}
		r.refreshDeliveryStateForLatestRun()
		r.transitionPhase(PhaseCompleted, "")
		r.renderCompletedSnapshot()
		if strings.EqualFold(strings.TrimSpace(r.deliveryState.State), "retrying") {
			r.renderDeliveryAttentionSnapshot()
		}
		if !r.suppressPromptWorkbenchRuntimeNoise() {
			r.renderer.RenderCompletionCard(time.Since(r.runStartedAt), update.Usage)
		}
	case acp.SessionError:
		r.clearQuitConfirmation()
		r.finishActiveToolTimeline("error", time.Now())
		r.cacheLatestRunTimeline()
		errText := defaultString(strings.TrimSpace(update.Error), "run failed")
		r.lastRunErr = fmt.Errorf("%s", errText)
		r.lastFailure = errText
		r.lastRunDuration = time.Since(r.runStartedAt)
		r.transitionPhase(PhaseError, "")
		r.renderer.StopSpinner()
		if tail := r.textFilter.Flush(); tail != "" {
			r.renderer.WriteDelta(tail)
		}
		r.renderer.Finish()
		if r.pauseRequested {
			r.enterPausedState()
			return nil
		}
		if r.cancelRequested {
			r.enterCancelledState()
			return nil
		}
		r.lastToolStatus = ""
		if update.Runless {
			r.deliveryState = deliveryDockState{}
			r.transitionPhase(PhaseIdle, "")
			r.renderer.RenderError(r.lastRunErr, "")
			break
		}
		r.refreshDeliveryStateForLatestRun()
		if safeFallback, _ := recoveryHint(errText, r.targetName, r.targetKind); strings.TrimSpace(safeFallback) != "" {
			r.renderRecoverySnapshot(errText)
		} else {
			r.renderFailedSnapshot(errText)
		}
		r.renderer.RenderError(r.lastRunErr, "")
	}
	r.refreshViewState()
	r.renderDock()
	return nil
}

func (r *REPL) resolveApproval(ctx context.Context, id string, approved bool) error {
	item, err := r.service.ResolveApproval(ctx, id, approved)
	if err != nil {
		return err
	}
	status := "denied"
	if approved {
		status = "approved"
	}
	if item != nil && strings.TrimSpace(item.Status) != "" {
		status = item.Status
	}
	r.clearPanel()
	r.renderer.SystemLine(fmt.Sprintf("Approval %s %s.", strings.TrimSpace(id), status))
	return nil
}

func (r *REPL) runEvalSuite(ctx context.Context, suiteID string) error {
	report, err := r.service.RunEvalSuite(ctx, suiteID)
	if err != nil {
		return err
	}
	if report == nil {
		r.renderer.SystemLine("Eval run completed without a report.")
		return nil
	}
	r.openInfoPanel("Eval Run", []string{
		fmt.Sprintf("Suite    %s", defaultString(report.SuiteID, strings.TrimSpace(suiteID))),
		fmt.Sprintf("Status   %s", defaultString(report.Status, "unknown")),
		fmt.Sprintf("Cases    %d", report.CaseCount),
		fmt.Sprintf("Passed   %d", report.Passed),
		fmt.Sprintf("Failed   %d", report.Failed),
		fmt.Sprintf("Errored  %d", report.Errored),
	}, "Esc back")
	return nil
}

func (r *REPL) lastFailedRun(ctx context.Context) (*RunSummary, error) {
	if r == nil || r.service == nil {
		return nil, nil
	}
	items, err := r.service.ListRuns(ctx, "", 6)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		status := strings.ToLower(strings.TrimSpace(item.Status))
		switch {
		case status == "failed", status == "error", status == "cancelled":
			copyItem := item
			return &copyItem, nil
		case strings.TrimSpace(item.Error) != "":
			copyItem := item
			return &copyItem, nil
		}
	}
	return nil, nil
}

func (r *REPL) lastRecoverableRun(ctx context.Context) (*RunSummary, error) {
	if r == nil || r.service == nil {
		return nil, nil
	}
	items, err := r.service.ListRuns(ctx, "", 6)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		status := normalizedExecutionState(item.Status)
		phase := normalizedExecutionState(item.Phase)
		if item.Resumable || status == "paused" || phase == "paused" {
			copyItem := item
			return &copyItem, nil
		}
	}
	return nil, nil
}

func (r *REPL) submitPrepared(ctx context.Context, promptText string, images []string, contentBlocks []contextengine.ContentBlock, model string) error {
	return r.submitPreparedWithOptions(ctx, promptText, images, contentBlocks, model, nil, nil)
}

func (r *REPL) submitPreparedWithOptions(ctx context.Context, promptText string, images []string, contentBlocks []contextengine.ContentBlock, model string, structuredCommand *acp.StructuredCommand, structuredApproval *acp.StructuredApproval) error {
	serviceSessionID, _ := r.currentServiceSessionID(ctx)
	if existing := r.backgroundRunForSession(serviceSessionID); existing != "" {
		return fmt.Errorf("session %s already has background run %s; use /fg %s or switch sessions", defaultString(r.sessionKey, r.sessionID), existing, existing)
	}
	r.running = true
	r.pauseRequested = false
	r.cancelRequested = false
	r.quitConfirmPending = false
	r.pausedRun = nil
	r.activeTimeline = nil
	r.lastTimeline = nil
	r.lastRunErr = nil
	r.lastRunDuration = 0
	r.currentRunID = ""
	r.deliveryState = deliveryDockState{}
	r.snapshotTracker.reset()
	r.clearPanel()
	r.prompt.SetPaused(false)
	r.renderer.ResetAssistantState()
	r.lastSubmitText = promptText
	r.lastSubmitImgs = append([]string(nil), images...)
	r.lastSubmitContentBlocks = append([]contextengine.ContentBlock(nil), contentBlocks...)
	r.runStartedAt = time.Now()
	r.transitionPhase(PhaseThinking, "")
	r.refreshViewState()
	r.renderDock()
	defer func() {
		r.running = false
		if r.phase != PhasePaused && r.phase != PhaseCompleted && r.phase != PhaseCancelled && r.phase != PhaseError {
			r.transitionPhase(PhaseIdle, "")
		}
		r.refreshViewState()
		r.renderDock()
	}()

	r.renderer.StartSpinner("Waiting for response…")
	message := promptText
	if rewritten, ok := shellHandoffMessage(promptText); ok {
		message = rewritten
	}
	if err := r.client.Prompt(ctx, acp.PromptParams{
		SessionID:          r.sessionID,
		Message:            message,
		ContentBlocks:      append([]contextengine.ContentBlock(nil), contentBlocks...),
		Images:             images,
		Model:              model,
		StructuredCommand:  structuredCommand,
		StructuredApproval: structuredApproval,
	}); err != nil {
		r.renderer.StopSpinner()
		return err
	}
	if err := r.waitForRun(ctx); err != nil {
		if errors.Is(err, errREPLBackgrounded) {
			return nil
		}
		return err
	}
	if r.oneShot && r.lastRunErr != nil {
		return r.lastRunErr
	}
	return nil
}

func (r *REPL) requestPause(ctx context.Context) error {
	if !r.running {
		return nil
	}
	r.clearQuitConfirmation()
	r.pauseRequested = true
	r.cancelRequested = false
	r.renderer.RenderSystemEvent("Pausing current task…")
	return r.client.Cancel(ctx, acp.CancelParams{SessionID: r.sessionID})
}

func (r *REPL) requestCancel(ctx context.Context) error {
	if !r.running {
		return nil
	}
	r.clearQuitConfirmation()
	r.pauseRequested = false
	r.cancelRequested = true
	r.renderer.RenderSystemEvent("Cancelling current task…")
	return r.client.Cancel(ctx, acp.CancelParams{SessionID: r.sessionID})
}

func (r *REPL) enterPausedState() {
	r.ensureCurrentRunID()
	if r.lastRunDuration <= 0 && !r.runStartedAt.IsZero() {
		r.lastRunDuration = time.Since(r.runStartedAt)
	}
	r.pausedRun = &pausedRunState{
		Message:       r.lastSubmitText,
		Images:        append([]string(nil), r.lastSubmitImgs...),
		ContentBlocks: append([]contextengine.ContentBlock(nil), r.lastSubmitContentBlocks...),
		LastStep:      currentToolName(r.lastToolStatus),
		RunID:         defaultString(strings.TrimSpace(r.currentRunID), defaultString(strings.TrimSpace(r.foregroundRunID), strings.TrimSpace(r.lastRunID))),
	}
	r.lastToolStatus = ""
	r.prompt.SetPaused(true)
	r.transitionPhase(PhasePaused, "")
	r.renderPausedSnapshot(r.pausedRun.LastStep)
	r.renderer.RenderPausedCard(defaultString(r.pausedRun.RunID, defaultString(r.sessionKey, r.sessionID)), r.pausedRun.LastStep)
}

func (r *REPL) enterCancelledState() {
	r.ensureCurrentRunID()
	if r.lastRunDuration <= 0 && !r.runStartedAt.IsZero() {
		r.lastRunDuration = time.Since(r.runStartedAt)
	}
	r.pausedRun = nil
	r.pauseRequested = false
	r.cancelRequested = false
	r.lastToolStatus = ""
	r.prompt.SetPaused(false)
	r.transitionPhase(PhaseCancelled, "")
	r.renderCancelledSnapshot()
	r.renderer.RenderCancelledCard(defaultString(r.currentRunID, defaultString(r.sessionKey, r.sessionID)))
}

func (r *REPL) resumePaused(ctx context.Context, retry bool) error {
	if r.pausedRun == nil {
		r.renderer.RenderSystemEvent("No paused task.")
		return nil
	}
	paused := r.pausedRun
	r.pausedRun = nil
	r.pauseRequested = false
	r.cancelRequested = false
	r.prompt.SetPaused(false)
	if retry {
		r.renderer.RenderSystemEvent("Restarting from the last user message…")
	} else {
		r.renderer.RenderSystemEvent("Continuing paused task from the last stable step…")
	}
	if runID := strings.TrimSpace(paused.RunID); runID != "" {
		return r.submitPreparedWithOptions(ctx, paused.Message, nil, nil, r.effectiveModel(), &acp.StructuredCommand{
			Kind:  "retry",
			RunID: runID,
		}, nil)
	}
	return r.submitPrepared(ctx, paused.Message, append([]string(nil), paused.Images...), append([]contextengine.ContentBlock(nil), paused.ContentBlocks...), r.effectiveModel())
}

func (r *REPL) discardPaused() {
	r.pausedRun = nil
	r.pauseRequested = false
	r.cancelRequested = false
	r.prompt.SetPaused(false)
	r.transitionPhase(PhaseIdle, "")
	r.renderer.RenderSystemEvent("Paused task discarded.")
}
