package repl

import (
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/acp"
)

func TestRendererWritesStreamingOutputWithoutTTYRedrawSequences(t *testing.T) {
	var output strings.Builder
	renderer := NewRenderer(&output, true)

	renderer.WriteDelta("hello")
	renderer.WriteDelta(" world")
	renderer.ToolStatus("search", "done")
	renderer.WriteDelta("final line")
	renderer.Finish()

	got := output.String()
	if strings.Contains(got, "\r\033[2K") {
		t.Fatalf("output contained TTY redraw escape sequence: %q", got)
	}
	want := "hello world\n\033[90m[tool]\033[0m search — done\nfinal line\n"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRendererHistoryLinesRespectTerminalWidthForWideCharacters(t *testing.T) {
	var output strings.Builder
	renderer := NewRenderer(&output, true)

	renderer.RenderSystemEvent("切换到远端 prod-eu 并保持多语言终端布局稳定")
	renderer.RenderPhase(PhaseExecutingTools, "fs.read docs/HopClaw 实施版总方案.md 并检查多语言宽度")
	renderer.RenderToolEvent("audit.deliveries", "读取 docs/HopClaw 实施版总方案.md 并检查多语言布局溢出")

	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		if got := displayWidth(line); got > 80 {
			t.Fatalf("history line width = %d, want <= 80: %q", got, line)
		}
	}
}

func TestRendererBannerUsesTwoLineSessionSummary(t *testing.T) {
	var output strings.Builder
	renderer := NewRenderer(&output, false)

	renderer.Banner(BannerInfo{
		Version:       "test",
		Target:        "local",
		Model:         "gpt-4o",
		Session:       "default",
		ContextWindow: 128000,
	})

	got := output.String()
	if got != "HopClaw test\nlocal · gpt-4o · conversation default\n" {
		t.Fatalf("banner missing two-line summary: %q", got)
	}
}

func TestRendererBannerTruncatesMachineSessionKeys(t *testing.T) {
	var output strings.Builder
	renderer := NewRenderer(&output, false)

	renderer.Banner(BannerInfo{
		Version: "test",
		Target:  "local",
		Model:   "gpt-5.4",
		Session: "cli-20260405-053002-8e657a",
	})

	got := output.String()
	if strings.Contains(got, "cli-20260405-053002-8e657a") {
		t.Fatalf("banner leaked full session key: %q", got)
	}
	if !strings.Contains(got, "conversation cli-2026…") {
		t.Fatalf("banner missing truncated session key: %q", got)
	}
}

func TestRendererBannerTTYShowsHelpHintAndAutoModel(t *testing.T) {
	var output strings.Builder
	renderer := NewRenderer(&output, true)

	renderer.Banner(BannerInfo{
		Version: "test",
		Target:  "local",
		Session: "default",
	})

	got := output.String()
	if got != "HopClaw test · auto\n" {
		t.Fatalf("banner tty output = %q", got)
	}
}

func TestWorkbenchBottomRailTruncatesMachineSessionKeys(t *testing.T) {
	left, _ := workbenchBottomRail(REPLViewState{
		CWD:        "/tmp/project",
		GitBranch:  "main",
		SessionKey: "cli-20260405-053002-8e657a",
	}, LayoutFull, 72)

	if strings.Contains(left, "cli-20260405-053002-8e657a") {
		t.Fatalf("bottom rail leaked full session key: %q", left)
	}
	if !strings.Contains(left, "conv:cli-2026…") {
		t.Fatalf("bottom rail missing truncated session key: %q", left)
	}
}

func TestWorkbenchLinesRespectDisplayWidthForMultilingualState(t *testing.T) {
	const width = 88

	lines := workbenchLines(REPLViewState{
		Target:         "prod-eu",
		Model:          "gpt-5.4",
		ExecutionState: "streaming",
		CWD:            "/home/user/全球化/终端设计/多语言工作区",
		SessionKey:     "会话-默认-国际化-2026-长标识",
		LastTool:       "fs.read docs/多语言指南.md",
		LayoutMode:     LayoutCompact,
		TerminalWidth:  width,
	}, LayoutCompact, true)

	if len(lines) == 0 {
		t.Fatal("workbenchLines() returned no lines")
	}
	for _, line := range lines {
		if got := displayWidth(line); got > width {
			t.Fatalf("workbench line width = %d, want <= %d: %q", got, width, line)
		}
	}
	if text := strings.Join(lines, "\n"); !strings.Contains(text, "多语言指南") {
		t.Fatalf("workbench lost multilingual tool context: %q", text)
	}
}

func TestWorkbenchLinesProjectNamedLocalRuntimeAsLocal(t *testing.T) {
	lines := workbenchLines(REPLViewState{
		Target:         "local-dev",
		TargetKind:     "local",
		Model:          "gpt-5.4",
		ExecutionState: "ready",
		LayoutMode:     LayoutPlain,
		TerminalWidth:  80,
	}, LayoutPlain, true)

	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "[state] runtime=local:local-dev model=gpt-5.4 status=ready ctx=0%") {
		t.Fatalf("plain workbench projection = %q", text)
	}

	full := strings.Join(workbenchLines(REPLViewState{
		Target:         "local-dev",
		TargetKind:     "local",
		Model:          "gpt-5.4",
		ExecutionState: "ready",
		LayoutMode:     LayoutFull,
		TerminalWidth:  120,
	}, LayoutFull, true), "\n")
	if !strings.Contains(full, "LOCAL local-dev") {
		t.Fatalf("full workbench projection = %q, want LOCAL local-dev", full)
	}
}

func TestDockLinesProjectNamedLocalRuntimeAsLocal(t *testing.T) {
	lines := dockLines(REPLViewState{
		Target:         "local-dev",
		TargetKind:     "local",
		Model:          "gpt-5.4",
		ExecutionState: "ready",
		LayoutMode:     LayoutFull,
		TerminalWidth:  132,
	}, LayoutFull, false)

	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "LOCAL local-dev") {
		t.Fatalf("dock projection = %q, want LOCAL local-dev", text)
	}
}

func TestRendererTimelineFormatting(t *testing.T) {
	renderer := NewRenderer(&strings.Builder{}, false)
	entries := []ToolTimelineEntry{
		{Name: "fs.read", Status: "ok", Summary: "read plan", Duration: 1500 * time.Millisecond},
		{Name: "exec.shell", Status: "error", Summary: "permission denied", Duration: 250 * time.Millisecond},
	}

	summary := renderer.TimelineSummary(entries)
	if !strings.Contains(summary, "timeline: 2") || !strings.Contains(summary, "fs.read ok") {
		t.Fatalf("TimelineSummary() = %q", summary)
	}

	lines := renderer.TimelineLines(entries)
	if len(lines) < 3 {
		t.Fatalf("TimelineLines() = %#v", lines)
	}
	for _, want := range []string{"1. fs.read · ok · 1.5s", "read plan", "2. exec.shell · error · 250ms"} {
		found := false
		for _, line := range lines {
			if strings.Contains(line, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("TimelineLines() missing %q in %#v", want, lines)
		}
	}
}

func TestRendererRenderDockSupportsAllLayouts(t *testing.T) {
	tests := []struct {
		name   string
		tty    bool
		state  REPLViewState
		wants  []string
		noANSI bool
	}{
		{
			name: "auto_full",
			tty:  true,
			state: REPLViewState{
				Target:         "local",
				Model:          "gpt-5.4",
				ExecutionState: "ready",
				CWD:            "/tmp/project",
				GitBranch:      "main",
				SessionKey:     "default",
				LayoutMode:     LayoutAuto,
				TerminalWidth:  120,
			},
			wants: []string{"LOCAL", "gpt-5.4", "READY", "conv:default", "/help"},
		},
		{
			name: "auto_compact_running_shows_hint",
			tty:  true,
			state: REPLViewState{
				Target:         "local",
				Model:          "gpt-5.4",
				ExecutionState: "streaming",
				CWD:            "/tmp/project",
				LastTool:       "search.logs",
				LayoutMode:     LayoutAuto,
				TerminalWidth:  96,
			},
			wants: []string{"RUNNING", "search.logs", "Esc pause"},
		},
		{
			name: "full",
			tty:  true,
			state: REPLViewState{
				Target:         "local",
				Profile:        "default",
				Model:          "gpt-5.4",
				Think:          "off",
				ExecutionState: "ready",
				ApprovalMode:   "on-request",
				Health:         "ok",
				CWD:            "/tmp/project",
				GitBranch:      "main",
				SessionKey:     "default",
				Channel:        "cli",
				Runtime:        "local",
				Sandbox:        "local",
				Phase:          "idle",
				LayoutMode:     LayoutFull,
				TerminalWidth:  140,
			},
			wants: []string{"LOCAL", "gpt-5.4", "READY", "conv:default", "/help"},
		},
		{
			name: "compact",
			tty:  true,
			state: REPLViewState{
				Target:         "remote prod",
				Model:          "gpt-5.4-mini",
				Think:          "on",
				ExecutionState: "streaming",
				ApprovalMode:   "on-request",
				Health:         "warn",
				CWD:            "/tmp/project",
				SessionKey:     "ops",
				Phase:          "executing_tools",
				LastTool:       "search.logs",
				LayoutMode:     LayoutCompact,
				TerminalWidth:  90,
			},
			wants: []string{"gpt-5.4-mini", "RUNNING", "phase:executing_tools", "search.logs", "Esc pause"},
		},
		{
			name: "plain",
			tty:  true,
			state: REPLViewState{
				Target:        "local",
				Model:         "gpt-5.4",
				SessionKey:    "default",
				LayoutMode:    LayoutPlain,
				TerminalWidth: 60,
			},
			wants:  []string{"[state] runtime=local model=gpt-5.4 status=ready ctx=0%"},
			noANSI: true,
		},
		{
			name: "explicit_minimal",
			tty:  true,
			state: REPLViewState{
				Target:         "local",
				Model:          "gpt-5.4",
				ExecutionState: "ready",
				LayoutMode:     LayoutMinimal,
				TerminalWidth:  140,
			},
			wants: []string{"→", "gpt-5.4", "ready"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			renderer := NewRenderer(&output, tt.tty)
			renderer.RenderDock(tt.state)
			got := output.String()
			for _, want := range tt.wants {
				if !strings.Contains(got, want) {
					t.Fatalf("dock missing %q: %q", want, got)
				}
			}
			if tt.noANSI && strings.Contains(got, "\033[") {
				t.Fatalf("plain dock contained ANSI escapes: %q", got)
			}
		})
	}
}

func TestResolveDockLayoutAutoThresholds(t *testing.T) {
	tests := []struct {
		name  string
		tty   bool
		width int
		want  LayoutMode
	}{
		{name: "tty full", tty: true, width: 120, want: LayoutFull},
		{name: "tty compact", tty: true, width: 90, want: LayoutCompact},
		{name: "tty plain", tty: true, width: 72, want: LayoutPlain},
		{name: "non tty always plain", tty: false, width: 160, want: LayoutPlain},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDockLayout(REPLViewState{
				LayoutMode:    LayoutAuto,
				TerminalWidth: tt.width,
			}, tt.tty)
			if got != tt.want {
				t.Fatalf("resolveDockLayout() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDockProfileInference(t *testing.T) {
	tests := []struct {
		name  string
		state REPLViewState
		want  string
	}{
		{
			name:  "coding by default",
			state: REPLViewState{Target: "local", Runtime: "local", Channel: "cli"},
			want:  ProfileCoding,
		},
		{
			name:  "ops from gateway runtime",
			state: REPLViewState{Target: "prod-eu", Runtime: "gateway", Channel: "cli"},
			want:  ProfileOps,
		},
		{
			name:  "channel from projected channel",
			state: REPLViewState{Target: "local", Runtime: "local", Channel: "telegram"},
			want:  ProfileChannel,
		},
		{
			name:  "automation from active panel",
			state: REPLViewState{Target: "local", Runtime: "local", ActivePanel: "automation"},
			want:  ProfileAutomation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inferProfile(tt.state); got != tt.want {
				t.Fatalf("inferProfile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDockFullLinesStayFocusedOnCoreState(t *testing.T) {
	tests := []struct {
		name    string
		state   REPLViewState
		wants   []string
		unwants []string
	}{
		{
			name: "running keeps core state and active supervisor summary",
			state: REPLViewState{
				Target:             "prod-eu",
				Model:              "gpt-5.4",
				ExecutionState:     "streaming",
				Phase:              "executing_tools",
				CWD:                "/tmp/project",
				GitBranch:          "main",
				SessionKey:         "ops-incident",
				LastTool:           "go test ./internal/cli/repl",
				ContextPercent:     41,
				Profile:            ProfileOps,
				QueueDepth:         2,
				Sandbox:            "remote",
				Quality:            "ok",
				LastFailure:        "exec.shell permission denied",
				BackgroundRunCount: 2,
				PausedRunCount:     1,
			},
			wants: []string{
				"REMOTE prod-eu",
				"MODEL gpt-5.4",
				"STREAMING",
				"tool: go test ./internal/cli/repl",
				"ctx: 41%",
				"bg:2 paused:1",
				"hint: Esc pause task · Ctrl+C quit terminal",
			},
			unwants: []string{"queue:", "sandbox:", "quality:", "last failure:"},
		},
		{
			name: "approval shows approval guidance without profile rails",
			state: REPLViewState{
				Target:         "prod-eu",
				Model:          "gpt-5.4",
				ExecutionState: "waiting approval",
				Phase:          "waiting_approval",
				CWD:            "/tmp/project",
				SessionKey:     "ops-incident",
				LastTool:       "exec.shell",
				ApprovalRisk:   "destructive",
				ScopeSummary:   "remote_write",
				Profile:        ProfileAutomation,
				QueueDepth:     4,
				LastFailure:    "dead_letter dlq-12",
			},
			wants: []string{
				"WAITING APPROVAL",
				"tool: exec.shell",
				"hint: y approve once · a always · n deny · v details",
			},
			unwants: []string{"queue:", "last failure:", "cron:", "dead letters:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := dockLines(tt.state, LayoutFull, false)
			text := strings.Join(lines, "\n")
			for _, want := range tt.wants {
				if !strings.Contains(text, want) {
					t.Fatalf("dock missing %q: %q", want, text)
				}
			}
			for _, unwanted := range tt.unwants {
				if strings.Contains(text, unwanted) {
					t.Fatalf("dock unexpectedly included %q: %q", unwanted, text)
				}
			}
		})
	}
}

func TestWorkbenchLinesByState(t *testing.T) {
	tests := []struct {
		name      string
		state     REPLViewState
		layout    LayoutMode
		wants     []string
		wantLines int
	}{
		{
			name: "running full",
			state: REPLViewState{
				ExecutionState: "streaming",
				Phase:          "executing_tools",
				LastTool:       "search.logs",
				SessionKey:     "ops",
			},
			layout:    LayoutFull,
			wants:     []string{"RUNNING", "search.logs", "Esc pause · /runs manage"},
			wantLines: 2,
		},
		{
			name: "paused compact",
			state: REPLViewState{
				ExecutionState: "paused",
				Phase:          "paused",
				LastTool:       "audit.deliveries",
				Resumable:      true,
			},
			layout:    LayoutCompact,
			wants:     []string{"PAUSED", "Enter continue"},
			wantLines: 2,
		},
		{
			name: "running compact shows phase and tool",
			state: REPLViewState{
				ExecutionState: "running",
				Phase:          "executing_tools",
				LastTool:       "search.logs",
				SessionKey:     "ops",
			},
			layout:    LayoutCompact,
			wants:     []string{"RUNNING", "phase:executing_tools", "search.logs", "Esc pause"},
			wantLines: 2,
		},
		{
			name: "approval full",
			state: REPLViewState{
				ExecutionState: "waiting approval",
				Phase:          "waiting_approval",
				LastTool:       "exec.shell",
				ScopeSummary:   "scope remote_write",
				ApprovalRisk:   "destructive",
			},
			layout:    LayoutFull,
			wants:     []string{"WAITING APPROVAL", "DESTRUCT", "y approve"},
			wantLines: 2,
		},
		{
			name: "idle no hint",
			state: REPLViewState{
				ExecutionState: "ready",
				Phase:          "idle",
			},
			layout:    LayoutFull,
			wants:     []string{"/help"},
			wantLines: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := workbenchLines(tt.state, tt.layout, true)
			if len(lines) != tt.wantLines {
				t.Fatalf("len(lines) = %d, want %d (%#v)", len(lines), tt.wantLines, lines)
			}
			text := strings.Join(lines, "\n")
			for _, want := range tt.wants {
				if !strings.Contains(text, want) {
					t.Fatalf("workbench missing %q: %q", want, text)
				}
			}
		})
	}
}

func TestWorkbenchLayoutMatrixCoversAllTerminalStates(t *testing.T) {
	tests := []struct {
		name   string
		state  REPLViewState
		layout LayoutMode
		wants  []string
		lines  int
	}{
		{
			name: "idle full",
			state: REPLViewState{
				Target:         "local",
				Model:          "gpt-5.4",
				ExecutionState: "ready",
				ContextPercent: 8,
			},
			layout: LayoutFull,
			wants:  []string{"READY", "/help"},
			lines:  2,
		},
		{
			name: "idle compact",
			state: REPLViewState{
				Target:         "local",
				Model:          "gpt-5.4",
				ExecutionState: "ready",
				ContextPercent: 8,
			},
			layout: LayoutCompact,
			wants:  []string{"READY", "/help"},
			lines:  2,
		},
		{
			name: "idle plain",
			state: REPLViewState{
				Target:         "local",
				Model:          "gpt-5.4",
				ExecutionState: "ready",
				ContextPercent: 8,
			},
			layout: LayoutPlain,
			wants:  []string{"[state] runtime=local model=gpt-5.4 status=ready ctx=8%"},
			lines:  1,
		},
		{
			name: "running full",
			state: REPLViewState{
				Target:         "prod-eu",
				Model:          "gpt-5.4",
				ExecutionState: "streaming",
				Phase:          "executing_tools",
				LastTool:       "search.logs",
				Elapsed:        "00:18",
			},
			layout: LayoutFull,
			wants:  []string{"RUNNING", "search.logs", "Esc pause · /runs manage"},
			lines:  2,
		},
		{
			name: "running compact",
			state: REPLViewState{
				Target:         "prod-eu",
				Model:          "gpt-5.4",
				ExecutionState: "streaming",
				Phase:          "executing_tools",
				LastTool:       "search.logs",
				Elapsed:        "00:18",
			},
			layout: LayoutCompact,
			wants:  []string{"RUNNING", "phase:executing_tools", "search.logs", "Esc pause"},
			lines:  2,
		},
		{
			name: "running plain",
			state: REPLViewState{
				Target:         "prod-eu",
				Model:          "gpt-5.4",
				ExecutionState: "streaming",
			},
			layout: LayoutPlain,
			wants:  []string{"[state] runtime=remote:prod-eu model=gpt-5.4 status=running ctx=0%"},
			lines:  1,
		},
		{
			name: "approval full",
			state: REPLViewState{
				Target:         "prod-eu",
				Model:          "gpt-5.4",
				ExecutionState: "waiting approval",
				ApprovalID:     "approval-7",
				ApprovalRisk:   "destructive",
				LastTool:       "exec.shell",
				ScopeSummary:   "remote_write",
			},
			layout: LayoutFull,
			wants:  []string{"WAITING APPROVAL", "approval approval-7", "y approve"},
			lines:  2,
		},
		{
			name: "approval compact",
			state: REPLViewState{
				Target:         "prod-eu",
				Model:          "gpt-5.4",
				ExecutionState: "waiting approval",
				ApprovalID:     "approval-7",
				ApprovalRisk:   "destructive",
				LastTool:       "exec.shell",
				ScopeSummary:   "remote_write",
			},
			layout: LayoutCompact,
			wants:  []string{"WAITING APPROVAL", "scope:remote_write", "y approve"},
			lines:  2,
		},
		{
			name: "approval plain",
			state: REPLViewState{
				Target:         "prod-eu",
				Model:          "gpt-5.4",
				ExecutionState: "waiting approval",
			},
			layout: LayoutPlain,
			wants:  []string{"[state] runtime=remote:prod-eu model=gpt-5.4 status=waiting approval ctx=0%"},
			lines:  1,
		},
		{
			name: "paused full",
			state: REPLViewState{
				Target:         "local",
				Model:          "gpt-5.4",
				ExecutionState: "paused",
				Resumable:      true,
				RunID:          "run-128",
				LastTool:       "audit.deliveries",
			},
			layout: LayoutFull,
			wants:  []string{"PAUSED", "run-128", "Enter continue · x discard · /retry"},
			lines:  2,
		},
		{
			name: "paused compact",
			state: REPLViewState{
				Target:         "local",
				Model:          "gpt-5.4",
				ExecutionState: "paused",
				Resumable:      true,
				RunID:          "run-128",
				LastTool:       "audit.deliveries",
			},
			layout: LayoutCompact,
			wants:  []string{"PAUSED", "run-128", "Enter continue"},
			lines:  2,
		},
		{
			name: "paused plain",
			state: REPLViewState{
				Target:         "local",
				Model:          "gpt-5.4",
				ExecutionState: "paused",
			},
			layout: LayoutPlain,
			wants:  []string{"[state] runtime=local model=gpt-5.4 status=paused ctx=0%"},
			lines:  1,
		},
		{
			name: "completed full",
			state: REPLViewState{
				Target:           "local",
				Model:            "gpt-5.4",
				ExecutionState:   "completed",
				Duration:         "00:41",
				PromptTokens:     16,
				CompletionTokens: 2,
			},
			layout: LayoutFull,
			wants:  []string{"COMPLETED", "tokens 16 / 2", "/last receipts"},
			lines:  2,
		},
		{
			name: "completed compact",
			state: REPLViewState{
				Target:           "local",
				Model:            "gpt-5.4",
				ExecutionState:   "completed",
				Duration:         "00:41",
				PromptTokens:     16,
				CompletionTokens: 2,
			},
			layout: LayoutCompact,
			wants:  []string{"COMPLETED", "tokens 16 / 2", "/last"},
			lines:  2,
		},
		{
			name: "completed plain",
			state: REPLViewState{
				Target:         "local",
				Model:          "gpt-5.4",
				ExecutionState: "completed",
			},
			layout: LayoutPlain,
			wants:  []string{"[state] runtime=local model=gpt-5.4 status=completed ctx=0%"},
			lines:  1,
		},
		{
			name: "error full",
			state: REPLViewState{
				Target:         "prod-eu",
				Model:          "gpt-5.4",
				ExecutionState: "error",
				LastFailure:    "gateway unreachable",
			},
			layout: LayoutFull,
			wants:  []string{"ERROR", "gateway unreachable", "/doctor"},
			lines:  2,
		},
		{
			name: "error compact",
			state: REPLViewState{
				Target:         "prod-eu",
				Model:          "gpt-5.4",
				ExecutionState: "error",
				LastFailure:    "gateway unreachable",
			},
			layout: LayoutCompact,
			wants:  []string{"ERROR", "gateway unreachable", "/doctor"},
			lines:  2,
		},
		{
			name: "error plain",
			state: REPLViewState{
				Target:         "prod-eu",
				Model:          "gpt-5.4",
				ExecutionState: "error",
			},
			layout: LayoutPlain,
			wants:  []string{"[state] runtime=remote:prod-eu model=gpt-5.4 status=error ctx=0%"},
			lines:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := tt.state
			if state.TerminalWidth == 0 {
				switch tt.layout {
				case LayoutFull:
					state.TerminalWidth = 132
				case LayoutCompact:
					state.TerminalWidth = 96
				default:
					state.TerminalWidth = 72
				}
			}
			lines := workbenchLines(state, tt.layout, true)
			if len(lines) != tt.lines {
				t.Fatalf("len(lines) = %d, want %d (%#v)", len(lines), tt.lines, lines)
			}
			text := strings.Join(lines, "\n")
			for _, want := range tt.wants {
				if !strings.Contains(text, want) {
					t.Fatalf("layout output missing %q: %q", want, text)
				}
			}
		})
	}
}

func TestWorkbenchLinesIdleKeepsQuietSignals(t *testing.T) {
	lines := workbenchLines(REPLViewState{
		Target:         "local",
		Model:          "gpt-5.4",
		ExecutionState: "ready",
		Think:          "off",
		ApprovalMode:   "on-request",
		Health:         "ok",
		CWD:            "/tmp/project",
		GitBranch:      "main",
		SessionKey:     "default",
		ContextPercent: 12,
		LayoutMode:     LayoutFull,
		TerminalWidth:  132,
	}, LayoutFull, true)

	text := strings.Join(lines, "\n")
	for _, want := range []string{"LOCAL", "gpt-5.4", "READY", "ctx 12%", "/help"} {
		if !strings.Contains(text, want) {
			t.Fatalf("idle workbench missing %q: %q", want, text)
		}
	}
	for _, unwanted := range []string{"THINK", "APPROVAL", "[OK]", "[BADGE", "queue", "sandbox"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("idle workbench should stay quiet, found %q in %q", unwanted, text)
		}
	}
}

func TestRendererRenderPhaseFormatsLines(t *testing.T) {
	var output strings.Builder
	renderer := NewRenderer(&output, false)

	renderer.RenderPhase(PhaseExecutingTools, "search.logs")

	if got := output.String(); got != "* Running tools: search.logs\n" {
		t.Fatalf("RenderPhase() = %q, want %q", got, "* Running tools: search.logs\n")
	}
}

func TestRendererUsesTypedPrefixesForSystemAndError(t *testing.T) {
	var output strings.Builder
	renderer := NewRenderer(&output, false)

	renderer.RenderSystemEvent("switched to remote prod")
	renderer.RenderError(assertErr("gateway unreachable"), "Try /remote list.")

	got := output.String()
	for _, want := range []string{
		"[system] switched to remote prod",
		"[error] gateway unreachable Try /remote list.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("typed renderer output missing %q: %q", want, got)
		}
	}
}

func TestRendererRenderErrorDefaultsToDoctorAndLast(t *testing.T) {
	var output strings.Builder
	renderer := NewRenderer(&output, true)

	renderer.RenderError(assertErr("gateway unreachable"), "")

	got := output.String()
	for _, want := range []string{"Task Failed", "/doctor  /last  Esc back"} {
		if !strings.Contains(got, want) {
			t.Fatalf("tty error card missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "/retry") {
		t.Fatalf("tty error card should not suggest /retry by default: %q", got)
	}
}

func TestRendererCompressesRepeatedToolEvents(t *testing.T) {
	var output strings.Builder
	renderer := NewRenderer(&output, false)

	renderer.RenderToolEvent("search.logs", "page 1")
	renderer.RenderToolEvent("search.logs", "page 2")
	renderer.RenderToolEvent("search.logs", "page 3")
	renderer.RenderToolEvent("search.logs", "page 4")
	renderer.RenderSystemEvent("switched to remote prod")

	got := output.String()
	for _, want := range []string{
		"[tool] search.logs — page 1",
		"[tool] search.logs — page 2",
		"[tool] search.logs — 4 calls, latest: page 4",
		"[system] switched to remote prod",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compressed tool output missing %q: %q", want, got)
		}
	}
}

func TestWorkbenchVisibleBadgeStaysOutOfRails(t *testing.T) {
	lines := workbenchLines(REPLViewState{
		Target:         "local",
		Model:          "gpt-5.4",
		ExecutionState: "ready",
		Badge:          "A",
		BadgeSize:      5,
		LayoutMode:     LayoutFull,
		TerminalWidth:  132,
	}, LayoutFull, true)

	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2 (top rail + bottom rail): %#v", len(lines), lines)
	}
	text := strings.Join(lines, "\n")
	for _, unwanted := range []string{"ID A", "BADGE"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("badge should not render as a rail chip: %#v", lines)
		}
	}
}

func TestWorkbenchAvatarGutterStaysHiddenWhenBadgeDisabled(t *testing.T) {
	lines := workbenchLines(REPLViewState{
		Target:         "local",
		Model:          "gpt-5.4",
		ExecutionState: "ready",
		Badge:          "off",
		BadgeSize:      5,
		LayoutMode:     LayoutFull,
		TerminalWidth:  132,
	}, LayoutFull, true)

	for _, line := range lines {
		if strings.Contains(line, "ID ") {
			t.Fatalf("disabled badge should not render identity reflection: %#v", lines)
		}
	}
}

func TestWorkbenchLinesShowSupervisorSummaryChips(t *testing.T) {
	lines := workbenchLines(REPLViewState{
		Target:             "prod-eu",
		Model:              "gpt-5.4",
		ExecutionState:     "running",
		LastTool:           "audit.deliveries",
		SessionKey:         "ops",
		ForegroundRunCount: 1,
		BackgroundRunCount: 2,
		PausedRunCount:     1,
		AttentionCount:     1,
		LayoutMode:         LayoutFull,
		TerminalWidth:      132,
	}, LayoutFull, true)

	text := strings.Join(lines, "\n")
	for _, want := range []string{"FG 1", "BG 2", "PAUSED 1", "ATTN 1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("multi-run workbench missing %q: %q", want, text)
		}
	}
}

func TestWorkbenchLinesShowZeroedSupervisorSummaryChipsWhenBackgrounded(t *testing.T) {
	lines := workbenchLines(REPLViewState{
		Target:             "local",
		Model:              "gpt-5.4",
		ExecutionState:     "ready",
		RunID:              "run-bg",
		ForegroundRunCount: 0,
		BackgroundRunCount: 1,
		PausedRunCount:     0,
		AttentionCount:     0,
		LayoutMode:         LayoutFull,
		TerminalWidth:      132,
	}, LayoutFull, true)

	text := strings.Join(lines, "\n")
	for _, want := range []string{"FG 0", "BG 1", "PAUSED 0", "ATTN 0"} {
		if !strings.Contains(text, want) {
			t.Fatalf("background summary missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "FG 1") {
		t.Fatalf("background summary inferred foreground from last run id: %q", text)
	}
}

func TestRendererApprovalCardFormatting(t *testing.T) {
	var output strings.Builder
	renderer := NewRenderer(&output, false)

	renderer.RenderApprovalCard(ApprovalCard{
		RequestID:    "apr-1",
		Action:       "shell.exec",
		Reason:       "run go test",
		Impact:       "local process execution",
		Input:        "go test ./internal/cli/...",
		Scope:        "once | conversation",
		AllowSession: true,
	}, true)

	got := output.String()
	for _, want := range []string{
		"[card] Approval Required",
		"Tool     shell.exec",
		"apr-1",
		"[y] approve once  [a] allow for conversation  [n] deny  [v] details",
		"Details  go test ./internal/cli/...",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("approval card missing %q: %q", want, got)
		}
	}
}

func TestRendererApprovalCardHidesSessionActionWhenPolicyDisallowsIt(t *testing.T) {
	var output strings.Builder
	renderer := NewRenderer(&output, false)

	renderer.RenderApprovalCard(ApprovalCard{
		RequestID: "apr-2",
		Action:    "fs.write",
		Reason:    "approval required",
		Impact:    "review required",
		Input:     `{"path":"README.md"}`,
		Scope:     "once",
	}, false)

	got := output.String()
	if !strings.Contains(got, "[y] approve once  [n] deny  [v] details") {
		t.Fatalf("approval card missing once-only actions: %q", got)
	}
	if strings.Contains(got, "[a] allow for conversation") {
		t.Fatalf("approval card should hide session grant when unavailable: %q", got)
	}
}

func TestRendererSplitOutputKeepsAssistantBodyOffStatusStream(t *testing.T) {
	var status strings.Builder
	var assistant strings.Builder
	renderer := NewSplitRenderer(&status, &assistant, false)

	renderer.RenderPhase(PhaseThinking, "")
	renderer.RenderAssistantDelta("assistant body")
	renderer.Finish()
	renderer.UsageLine(9, 3, 12)

	if got := assistant.String(); got != "assistant body\n" {
		t.Fatalf("assistant stream = %q, want %q", got, "assistant body\n")
	}
	statusText := status.String()
	for _, want := range []string{"* Thinking", "tokens: 9 in · 3 out · 12 total"} {
		if !strings.Contains(statusText, want) {
			t.Fatalf("status stream missing %q: %q", want, statusText)
		}
	}
	if strings.Contains(statusText, "assistant body") {
		t.Fatalf("status stream leaked assistant body: %q", statusText)
	}
}

func TestRendererPausedAndCompletionCards(t *testing.T) {
	var output strings.Builder
	renderer := NewRenderer(&output, false)

	renderer.RenderPausedCard("run-128", "audit.deliveries")
	renderer.RenderCancelledCard("run-129")
	renderer.RenderCompletionCard(41*time.Second, &acp.UsageInfo{PromptTokens: 16, CompletionTokens: 2})

	got := output.String()
	for _, want := range []string{"Task Paused · run-128", "Enter continue  x discard  /retry", "Task Cancelled · run-129", "Task Completed", "Duration  00:41"} {
		if !strings.Contains(got, want) {
			t.Fatalf("card output missing %q: %q", want, got)
		}
	}
}

func assertErr(text string) error {
	return testError(text)
}

type testError string

func (e testError) Error() string { return string(e) }
