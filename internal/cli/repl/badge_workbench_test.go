package repl

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/agent"
	badgepkg "github.com/fulcrus/hopclaw/internal/cli/badge"
	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

func TestBadgeCommandSubCommands(t *testing.T) {
	repl, registry, output := newBadgeCommandTestREPL(t)
	badgePath := filepath.Join(t.TempDir(), "badge.png")
	writeREPLBadgeImage(t, badgePath)

	if _, err := registry.Execute(context.Background(), repl, "/badge color #f60"); err != nil {
		t.Fatalf("Execute(/badge color) error = %v", err)
	}
	if got := repl.badgeMgr.Config().Color; got != "#ff6600" {
		t.Fatalf("badge color = %q, want %q", got, "#ff6600")
	}

	if _, err := registry.Execute(context.Background(), repl, "/badge size 5"); err != nil {
		t.Fatalf("Execute(/badge size) error = %v", err)
	}
	if got := repl.badgeMgr.Config().Size; got != 5 {
		t.Fatalf("badge size = %d, want 5", got)
	}

	if _, err := registry.Execute(context.Background(), repl, "/badge import "+badgePath); err != nil {
		t.Fatalf("Execute(/badge import) error = %v", err)
	}
	if _, err := registry.Execute(context.Background(), repl, "/badge set custom-0"); err != nil {
		t.Fatalf("Execute(/badge set) error = %v", err)
	}
	if got := repl.badgeMgr.Config().Current; got != "custom-0" {
		t.Fatalf("badge current = %q, want %q", got, "custom-0")
	}

	if _, err := registry.Execute(context.Background(), repl, "/badge hide"); err != nil {
		t.Fatalf("Execute(/badge hide) error = %v", err)
	}
	if !repl.badgeHidden {
		t.Fatal("badgeHidden = false, want true after /badge hide")
	}

	if _, err := registry.Execute(context.Background(), repl, "/badge show"); err != nil {
		t.Fatalf("Execute(/badge show) error = %v", err)
	}
	if repl.badgeHidden {
		t.Fatal("badgeHidden = true, want false after /badge show")
	}

	text := output.String()
	for _, want := range []string{
		"Badge color set to #ff6600.",
		"Badge size set to 5 cells.",
		"Imported badge into custom-0.",
		"Active: no",
		"Badge set to custom-0.",
		"Badge hidden for this session.",
		"Badge shown for this session.",
		"Badge is off globally. Run /badge on to enable it by default.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q: %q", want, text)
		}
	}
}

func TestBadgeCommandPersistentToggle(t *testing.T) {
	repl, registry, output := newBadgeCommandTestREPL(t)

	if _, err := registry.Execute(context.Background(), repl, "/badge on"); err != nil {
		t.Fatalf("Execute(/badge on) error = %v", err)
	}
	if !repl.badgeMgr.Config().Enabled || repl.badgeHidden {
		t.Fatalf("badge state after /badge on = enabled:%v hidden:%v", repl.badgeMgr.Config().Enabled, repl.badgeHidden)
	}
	if _, err := registry.Execute(context.Background(), repl, "/badge off"); err != nil {
		t.Fatalf("Execute(/badge off) error = %v", err)
	}
	if repl.badgeMgr.Config().Enabled || !repl.badgeHidden {
		t.Fatalf("badge state after /badge off = enabled:%v hidden:%v", repl.badgeMgr.Config().Enabled, repl.badgeHidden)
	}
	text := output.String()
	for _, want := range []string{
		"Badge enabled. It will be shown by default in future terminal sessions.",
		"Badge disabled. It will stay hidden by default in future terminal sessions.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q: %q", want, text)
		}
	}
}

func TestBadgeCommandColorValidation(t *testing.T) {
	repl, registry, _ := newBadgeCommandTestREPL(t)

	_, err := registry.Execute(context.Background(), repl, "/badge color orange")
	if err == nil || !strings.Contains(err.Error(), "#rgb or #rrggbb") {
		t.Fatalf("Execute(/badge color orange) error = %v, want color format error", err)
	}
}

func TestBadgeCommandSlotBounds(t *testing.T) {
	repl, registry, _ := newBadgeCommandTestREPL(t)

	_, err := registry.Execute(context.Background(), repl, "/badge remove 24")
	if err == nil || !strings.Contains(err.Error(), "between 0 and 23") {
		t.Fatalf("Execute(/badge remove 24) error = %v, want slot bounds error", err)
	}
}

func TestBadgeCommandRemoveConfirmsBeforeDeleting(t *testing.T) {
	repl, registry, output := newBadgeCommandTestREPL(t)
	repl.prompter = &scriptedPrompter{lines: []string{"n", "y"}}
	badgePath := filepath.Join(t.TempDir(), "badge.png")
	writeREPLBadgeImage(t, badgePath)

	if _, err := registry.Execute(context.Background(), repl, "/badge import "+badgePath+" 0"); err != nil {
		t.Fatalf("Execute(/badge import) error = %v", err)
	}
	if _, err := registry.Execute(context.Background(), repl, "/badge remove 0"); err != nil {
		t.Fatalf("Execute(/badge remove cancel) error = %v", err)
	}
	if !repl.badgeMgr.ListSlots()[26].Occupied {
		t.Fatal("custom-0 should still exist after cancel")
	}
	if _, err := registry.Execute(context.Background(), repl, "/badge remove 0"); err != nil {
		t.Fatalf("Execute(/badge remove confirm) error = %v", err)
	}
	if repl.badgeMgr.ListSlots()[26].Occupied {
		t.Fatal("custom-0 should be removed after confirmation")
	}
	if !strings.Contains(output.String(), "Removed custom-0.") {
		t.Fatalf("output = %q, want removal confirmation", output.String())
	}
}

func TestBadgeCommandRendersPanel(t *testing.T) {
	repl, registry, output := newBadgeCommandTestREPL(t)

	if _, err := registry.Execute(context.Background(), repl, "/badge"); err != nil {
		t.Fatalf("Execute(/badge) error = %v", err)
	}
	text := output.String()
	for _, want := range []string{"[panel] Badge", "Current", "Custom images", "Tip"} {
		if !strings.Contains(text, want) {
			t.Fatalf("badge panel missing %q: %q", want, text)
		}
	}
}

func TestREPLDoesNotAutoRenderBadgeOnStartup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TERM_PROGRAM", "")

	origTermGetSize := termGetSize
	termGetSize = func(int) (int, int, error) {
		return 80, 24, nil
	}
	defer func() {
		termGetSize = origTermGetSize
	}()

	server := acp.NewServer(fakeGateway{}, acp.ServerConfig{
		DefaultSessionKey: "default",
	})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	service := &fakeService{
		models: []ModelInfo{{ID: "gpt-4o", ContextWindow: 128000}},
		detail: &SessionDetail{Summary: SessionSummary{ID: "default", Key: "default", Model: "gpt-4o"}},
	}
	var output strings.Builder
	repl, err := New(Config{
		Client:     client,
		Service:    service,
		Prompter:   &scriptedPrompter{lines: []string{"/exit"}},
		Renderer:   NewRenderer(&output, false),
		History:    NewHistory("", 10),
		SessionKey: "default",
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := output.String()
	if strings.Contains(got, "\033_Gf=100,a=T,i=1,c=3,r=3,m=") {
		t.Fatalf("startup output unexpectedly rendered badge: %q", got)
	}
}

func TestREPLRenderBadgeAnchorsToFullPromptWorkbench(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	origTermGetSize := termGetSize
	termGetSize = func(int) (int, int, error) {
		return 80, 24, nil
	}
	defer func() {
		termGetSize = origTermGetSize
	}()

	var output strings.Builder
	badgeMgr := badgepkg.NewManagerWithPaths(filepath.Join(t.TempDir(), "avatar.json"), filepath.Join(t.TempDir(), "avatars"))
	badgeMgr.SetEnabled(true)
	if err := badgeMgr.SetCurrent("B"); err != nil {
		t.Fatalf("SetCurrent() error = %v", err)
	}
	if err := badgeMgr.SetSize(4); err != nil {
		t.Fatalf("SetSize() error = %v", err)
	}
	badgeRdr, err := badgepkg.NewRenderer(&output, richedit.ProtocolKitty, badgeMgr.Config().Color, badgeMgr.Config().Size)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}

	repl := &REPL{
		renderer:     NewRenderer(&output, true),
		prompter:     &TerminalPrompter{tty: true},
		badgeMgr:     badgeMgr,
		badgeRdr:     badgeRdr,
		layoutMode:   LayoutFull,
		sessionKey:   "default",
		targetName:   "local",
		sessionModel: "gpt-5.4",
	}

	repl.renderBadge()

	got := output.String()
	for _, want := range []string{"\033[21;77H", "\033_Gf=100,a=T,i=1,c=4,r=4,m="} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderBadge() missing %q: %q", want, got)
		}
	}
}

func TestREPLRenderBadgeClearsOutsideFullPromptWorkbench(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	origTermGetSize := termGetSize
	termGetSize = func(int) (int, int, error) {
		return 80, 24, nil
	}
	defer func() {
		termGetSize = origTermGetSize
	}()

	var output strings.Builder
	badgeMgr := badgepkg.NewManagerWithPaths(filepath.Join(t.TempDir(), "avatar.json"), filepath.Join(t.TempDir(), "avatars"))
	badgeMgr.SetEnabled(true)
	if err := badgeMgr.SetCurrent("B"); err != nil {
		t.Fatalf("SetCurrent() error = %v", err)
	}
	if err := badgeMgr.SetSize(4); err != nil {
		t.Fatalf("SetSize() error = %v", err)
	}
	badgeRdr, err := badgepkg.NewRenderer(&output, richedit.ProtocolKitty, badgeMgr.Config().Color, badgeMgr.Config().Size)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}

	repl := &REPL{
		renderer:     NewRenderer(&output, true),
		prompter:     &TerminalPrompter{tty: true},
		badgeMgr:     badgeMgr,
		badgeRdr:     badgeRdr,
		layoutMode:   LayoutFull,
		sessionKey:   "default",
		targetName:   "local",
		sessionModel: "gpt-5.4",
	}

	repl.renderBadge()
	output.Reset()
	repl.layoutMode = LayoutCompact
	repl.renderBadge()
	got := output.String()
	if !strings.Contains(got, "\033_Ga=d,d=i,i=1\033\\") {
		t.Fatalf("compact renderBadge() should clear existing badge: %q", got)
	}

	output.Reset()
	repl.layoutMode = LayoutPlain
	repl.renderBadge()
	if output.Len() != 0 {
		t.Fatalf("plain renderBadge() should remain hidden after clear: %q", output.String())
	}
}

func TestREPLClearBadgeWithoutRendererDoesNotPanic(t *testing.T) {
	badgeRdr, err := badgepkg.NewRenderer(&strings.Builder{}, richedit.ProtocolKitty, "#00ff88", 4)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}

	repl := &REPL{badgeRdr: badgeRdr}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("clearBadge() panicked: %v", recovered)
		}
	}()
	repl.clearBadge()
}

func newBadgeCommandTestREPL(t *testing.T) (*REPL, *CommandRegistry, *strings.Builder) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TERM", "")
	t.Setenv("TERM_PROGRAM", "")

	client := newTestACPClient(t)
	t.Cleanup(func() {
		client.Close()
	})

	service := &fakeService{
		detail: &SessionDetail{Summary: SessionSummary{ID: "default", Key: "default", Model: "gpt-4o"}},
	}
	var output strings.Builder
	repl, err := New(Config{
		Client:     client,
		Service:    service,
		Prompter:   &scriptedPrompter{},
		Renderer:   NewRenderer(&output, false),
		History:    NewHistory("", 10),
		SessionKey: "default",
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return repl, NewCommandRegistry(), &output
}

func writeREPLBadgeImage(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 12, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 12; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		}
	}
	for y := 3; y < 9; y++ {
		for x := 3; x < 9; x++ {
			img.SetRGBA(x, y, color.RGBA{A: 0xff})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%q) error = %v", path, err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("png.Encode(%q) error = %v", path, err)
	}
}

func TestHandlePermissionRendersApprovalSnapshotInNonTTY(t *testing.T) {
	var output strings.Builder
	service := &fakeService{}
	repl := &REPL{
		renderer:     NewRenderer(&output, false),
		service:      service,
		sessionID:    "sess-1",
		sessionKey:   "ops",
		currentRunID: "run-1",
		targetName:   "prod-eu",
		prompt:       &DynamicPrompt{},
	}

	err := repl.handlePermission(context.Background(), acp.PermissionRequest{
		RequestID:   "approval-1",
		SessionID:   "sess-1",
		ToolName:    "exec.shell",
		Description: "remove retry cache",
		Input:       `{"cmd":"rm -rf /tmp/retries"}`,
	})
	if err != nil {
		t.Fatalf("handlePermission() error = %v", err)
	}
	if len(service.resolvedApprovals) != 1 || service.resolvedApprovals[0].Approved {
		t.Fatalf("resolvedApprovals = %#v, want one denied decision", service.resolvedApprovals)
	}
	if got := output.String(); !strings.Contains(got, "[task] Approval Pending · run-1") {
		t.Fatalf("approval snapshot missing from output: %q", got)
	}
}

func TestREPLRenderDockReflectsUpdatedViewState(t *testing.T) {
	var output strings.Builder
	repl := &REPL{
		targetName:     "prod-eu",
		sessionKey:     "ops-incident",
		sessionModel:   "gpt-5.4",
		thinking:       true,
		running:        true,
		layoutMode:     LayoutCompact,
		renderer:       NewRenderer(&output, false),
		currentProject: &agent.Project{Name: "hopclaw"},
	}

	repl.renderDock()
	got := output.String()
	if !strings.Contains(got, "[state] runtime=remote:prod-eu model=gpt-5.4 status=running ctx=0%") {
		t.Fatalf("dock missing updated state: %q", got)
	}
	if strings.Contains(got, "\n>\n") || strings.HasSuffix(got, ">\n") {
		t.Fatalf("dock should not render a duplicate prompt row: %q", got)
	}
	if repl.viewState.Profile != ProfileOps {
		t.Fatalf("viewState.Profile = %q, want %q", repl.viewState.Profile, ProfileOps)
	}

	output.Reset()
	repl.pendingApproval = true
	repl.renderDock()
	if got := output.String(); !strings.Contains(got, "status=waiting approval") {
		t.Fatalf("dock did not update after state change: %q", got)
	} else if strings.Contains(got, "approval>") {
		t.Fatalf("dock should not render an approval prompt row: %q", got)
	}
}

func TestREPLRenderDockDefersToPromptChromeWhileRunningWithPromptWorkbench(t *testing.T) {
	var output strings.Builder
	repl := &REPL{
		targetName:   "local",
		sessionKey:   "default",
		sessionModel: "gpt-5.4",
		running:      true,
		phase:        PhaseThinking,
		renderer:     NewRenderer(&output, true),
		prompter:     &TerminalPrompter{tty: true},
	}

	repl.renderDock()

	if got := output.String(); got != "" {
		t.Fatalf("prompt-workbench runtime should not append dock output, got %q", got)
	}

	chrome := repl.promptChrome(120)
	for _, want := range []string{"--------", "working", "Esc pause"} {
		if !strings.Contains(chrome.Top+"\n"+chrome.Bottom, want) {
			t.Fatalf("prompt chrome missing %q: top=%q bottom=%q", want, chrome.Top, chrome.Bottom)
		}
	}
	for _, unwanted := range []string{"MODEL", "gpt-5.4", "conversation default"} {
		if strings.Contains(chrome.Top+"\n"+chrome.Bottom, unwanted) {
			t.Fatalf("prompt chrome should stay lightweight, found %q: top=%q bottom=%q", unwanted, chrome.Top, chrome.Bottom)
		}
	}
}

func TestREPLRenderDockSkipsIdlePromptWorkbench(t *testing.T) {
	var output strings.Builder
	repl := &REPL{
		targetName:   "local",
		sessionKey:   "default",
		sessionModel: "gpt-5.4",
		renderer:     NewRenderer(&output, true),
		prompter:     &TerminalPrompter{tty: true},
	}

	repl.renderDock()

	if output.Len() != 0 {
		t.Fatalf("idle prompt workbench should stay editor-owned, got %q", output.String())
	}
}

func TestREPLPromptChromeStaysEmptyWhileIdleInPromptWorkbench(t *testing.T) {
	repl := &REPL{
		targetName:   "local",
		sessionKey:   "default",
		sessionModel: "deepseek-chat",
		phase:        PhaseIdle,
		renderer:     NewRenderer(&strings.Builder{}, true),
		prompter:     &TerminalPrompter{tty: true},
	}

	chrome := repl.promptChrome(80)
	for _, want := range []string{"--------", "@ attach", "/help", "/quit"} {
		if !strings.Contains(chrome.Top+"\n"+chrome.Bottom, want) {
			t.Fatalf("idle prompt-workbench chrome missing %q: top=%q bottom=%q", want, chrome.Top, chrome.Bottom)
		}
	}
	if strings.Contains(chrome.Top, "deepseek-chat") || strings.Contains(chrome.Top, "session") {
		t.Fatalf("idle prompt-workbench top rail should stay empty/lightweight: top=%q bottom=%q", chrome.Top, chrome.Bottom)
	}
}

func TestREPLPromptChromeLeavesRightMarginToAvoidTerminalWrap(t *testing.T) {
	repl := &REPL{
		targetName:   "local",
		sessionKey:   "default",
		sessionModel: "deepseek-chat",
		phase:        PhaseIdle,
		renderer:     NewRenderer(&strings.Builder{}, true),
		prompter:     &TerminalPrompter{tty: true},
	}

	chrome := repl.promptChrome(80)
	for _, line := range []string{chrome.Top, chrome.Bottom} {
		if got := visibleLen(line); got >= 80 {
			t.Fatalf("prompt-workbench chrome width = %d, want < 80 to avoid auto-wrap: %q", got, line)
		}
	}
}

func TestHandleUpdateSuppressesPromptWorkbenchRuntimeNoiseInTTY(t *testing.T) {
	var output strings.Builder
	repl := &REPL{
		renderer:     NewRenderer(&output, true),
		prompter:     &TerminalPrompter{tty: true},
		prompt:       &DynamicPrompt{},
		service:      &fakeService{},
		sessionID:    "sess-1",
		sessionKey:   "ops",
		currentRunID: "run-1",
		targetName:   "local",
		runStartedAt: time.Now().Add(-20 * time.Second),
		running:      true,
	}

	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status:     acp.SessionToolUse,
		ToolName:   "net.fetch",
		ToolOutput: `ssrf: host "wttr.in" resolves to private ip 198.18.0.11`,
		ModelFailover: &acp.ModelFailoverInfo{
			OriginalModel: "deepseek-chat",
			FallbackModel: "deepseek-reasoner",
		},
	}); err != nil {
		t.Fatalf("handleUpdate(tool_use) error = %v", err)
	}
	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status: acp.SessionCompleted,
		Usage:  &acp.UsageInfo{PromptTokens: 16, CompletionTokens: 2},
	}); err != nil {
		t.Fatalf("handleUpdate(completed) error = %v", err)
	}

	text := output.String()
	for _, unwanted := range []string{"[tool]", "[system]", "wttr.in", "198.18.0.11", "任务完成", "* Completed", "* Running tools:", "* Thinking", "* Delivering response", "* Idle"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("prompt-workbench runtime leaked %q: %q", unwanted, text)
		}
	}
	for _, want := range []string{"Task Completed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt-workbench runtime missing %q: %q", want, text)
		}
	}
}

func TestHandleUpdatePromptWorkbenchTTYShowsSingleCompletionSurface(t *testing.T) {
	var output strings.Builder
	repl := &REPL{
		renderer:     NewRenderer(&output, true),
		prompter:     &TerminalPrompter{tty: true},
		prompt:       &DynamicPrompt{},
		service:      &fakeService{},
		sessionID:    "sess-1",
		sessionKey:   "ops",
		currentRunID: "run-1",
		targetName:   "local",
		runStartedAt: time.Now().Add(-41 * time.Second),
		running:      true,
	}

	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status: acp.SessionCompleted,
		Usage:  &acp.UsageInfo{PromptTokens: 16, CompletionTokens: 2},
	}); err != nil {
		t.Fatalf("handleUpdate(completed) error = %v", err)
	}

	text := output.String()
	if strings.Count(text, "Task Completed") != 1 {
		t.Fatalf("prompt-workbench completion should render once, got %d in %q", strings.Count(text, "Task Completed"), text)
	}
}
