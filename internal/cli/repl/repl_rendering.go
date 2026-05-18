package repl

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fulcrus/hopclaw/acp"
	updatepkg "github.com/fulcrus/hopclaw/internal/update"
)

func (r *REPL) renderBanner() {
	r.refreshViewState()
	info := BannerInfo{
		Version:    r.version,
		Target:     r.targetName,
		TargetKind: r.targetKind,
		Model:      r.effectiveModel(),
		Session:    r.sessionKey,
	}
	for _, item := range r.modelCache {
		if item.ID == info.Model {
			info.ContextWindow = item.ContextWindow
			break
		}
	}
	if result := updatepkg.LastCheckResult(); result != nil && !result.UpToDate && result.LatestVersion != "" {
		info.UpdateAvail = result.LatestVersion
	}
	r.renderer.Banner(info)
}

func (r *REPL) renderBriefing(ctx context.Context) {
	if r.service == nil || r.renderer == nil {
		return
	}

	dir, err := os.Getwd()
	if err == nil {
		project, projectErr := r.service.FindOrCreateProject(ctx, dir)
		if projectErr == nil && project != nil {
			r.currentProject = project
		} else {
			r.currentProject = nil
		}
	}

	if spec, ok := r.startupRecoveryCardSpec(ctx); ok {
		r.renderer.RenderCard(spec)
	}
}

func (r *REPL) redrawRunningWorkbench(ctx context.Context) {
	if r == nil || r.renderer == nil {
		return
	}
	r.renderer.ClearScreen()
	if r.quitConfirmPending {
		r.renderQuitConfirmation()
		return
	}
	r.refreshSupervisorProjection(ctx, false)
	r.refreshTransparencyProjection(ctx, false)
	r.refreshViewState()

	state := r.viewState
	if r.shouldRenderPassiveWorkbench() {
		state = promptWorkbenchDockState(state)
	}
	r.renderer.RenderWorkbench(state)
	if r.usesPromptWorkbench() {
		r.renderBadge()
	}
	if r.running && !r.seenReplyText && !r.quitConfirmPending {
		r.renderer.StartSpinner("Waiting for response…")
	}
}

func (r *REPL) renderToolStatus(name string, output string) {
	name = strings.TrimSpace(name)
	output = strings.TrimSpace(output)
	if name == "" || output == "" {
		return
	}
	key := name + "\n" + output
	if key == r.lastToolStatus {
		return
	}
	r.lastToolStatus = key
	if r.suppressPromptWorkbenchRuntimeNoise() {
		return
	}
	r.renderer.ToolStatus(name, output)
}

func (r *REPL) renderModelFailover(info *acp.ModelFailoverInfo) {
	if info == nil {
		return
	}
	if r.suppressPromptWorkbenchRuntimeNoise() {
		return
	}
	original := strings.TrimSpace(info.OriginalModel)
	fallback := strings.TrimSpace(info.FallbackModel)
	switch {
	case original != "" && fallback != "":
		r.renderer.SystemLine(fmt.Sprintf("[model failover] %s → %s", original, fallback))
	case fallback != "":
		r.renderer.SystemLine(fmt.Sprintf("[model failover] %s", fallback))
	}
}

func (r *REPL) statusLine() string {
	r.refreshViewState()
	return r.viewState.Summary()
}
