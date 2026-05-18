package repl

import (
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/acp"
)

func (r *REPL) renderHelp(args []string) {
	title, lines, actions := r.helpTopic(args)
	if r.supportsInteractivePanels() {
		r.openInfoPanel(title, lines, actions)
		return
	}
	if strings.TrimSpace(title) != "" {
		r.renderer.SystemLine(title)
	}
	for _, line := range lines {
		r.renderer.SystemLine(line)
	}
}

func (r *REPL) helpTopic(args []string) (title string, lines []string, actions string) {
	title = "Help"
	actions = "/help <command>  Esc back"
	if len(args) > 0 {
		query := strings.ToLower(strings.TrimSpace(args[0]))
		if query == "attach" {
			return "Help: /attach", attachHelpLines(), actions
		}
		if command, usage, ok := r.commands.Describe(query); ok {
			return "Help: /" + command.Name, r.commandHelpLines(command.Name, command.Description, usage), actions
		}
		if command, ok := r.commands.DynamicCommand(query); ok {
			return "Runtime Inventory: " + strings.TrimPrefix(command.Name, "/"), r.runtimeInventoryHelpLines(command), actions
		}
		if group := r.domainGroupForQuery(query); group != "" {
			return "Help: " + group, r.domainHelpLines(group), actions
		}
		return "Help Search", r.helpSearchLines(query), actions
	}
	return "Quick Start", r.quickStartHelpLines(), actions
}

var domainGroupAliases = map[string]string{
	"task":         commandGroupTaskControl,
	"task control": commandGroupTaskControl,
	"control":      commandGroupTaskControl,
	"session":      commandGroupSession,
	"context":      commandGroupSession,
	"runtime":      commandGroupRuntime,
	"governance":   commandGroupGovernance,
	"approval":     commandGroupGovernance,
	"approvals":    commandGroupGovernance,
	"identity":     commandGroupIdentity,
	"badge":        commandGroupIdentity,
	"info":         commandGroupInfo,
}

func (r *REPL) domainGroupForQuery(query string) string {
	if group, ok := domainGroupAliases[query]; ok {
		return group
	}
	return ""
}

func (r *REPL) domainHelpLines(group string) []string {
	entries := r.commands.CommandsByGroup(group)
	if len(entries) == 0 {
		return []string{"No commands found in group " + group + "."}
	}
	lines := []string{group + " commands", ""}
	for _, entry := range entries {
		hint := defaultString(entry.Intent, entry.Command.Description)
		lines = append(lines, fmt.Sprintf("/%-14s %s", entry.Command.Name, hint))
	}
	if contextual := r.contextualHelpLines(); len(contextual) > 0 {
		lines = append(lines, "")
		lines = append(lines, contextual...)
	}
	lines = append(lines, "", "Use /help <command> for details on a specific command.")
	return lines
}

func (r *REPL) removedCommandHelpLines(name, replacement string) []string {
	lines := []string{
		"/" + name + " is not a current REPL command.",
		"Use /" + replacement + " instead.",
	}
	if replacement == "pause" {
		lines = append(lines,
			"",
			"Task control split:",
			"- /pause keeps a resumable paused handle",
			"- /cancel ends the task without keeping paused work",
		)
	}
	if contextual := r.contextualHelpLines(); len(contextual) > 0 {
		lines = append(lines, "")
		lines = append(lines, contextual...)
	}
	return lines
}

func (r *REPL) quickStartHelpLines() []string {
	lines := []string{
		"Type normally and press Enter to send.",
		"Keys: Enter send · Ctrl+J newline · Ctrl+V clipboard · @ attach · Ctrl+C quit",
		"",
		"Core commands",
		"/help <command>  /status  /last  /runs  /model  /remote  /clear  /quit",
		"",
		"Task control",
		"/pause  /continue  /cancel  /retry",
		"",
		"Attachments",
		"/attach <image|file|dir|video> <path>",
	}
	lines = append(lines, "")
	lines = append(lines, r.contextualHelpLines()...)
	if dynamic := r.commands.AdvisoryDynamicCommands(); len(dynamic) > 0 {
		names := make([]string, 0, min(len(dynamic), 3))
		for _, command := range dynamic[:min(len(dynamic), 3)] {
			name := strings.TrimPrefix(strings.TrimSpace(command.Name), "/")
			if name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			lines = append(lines, "", "Runtime inventory: "+strings.Join(names, ", ")+" (reference only)")
		}
	}
	lines = append(lines, "", "Use /help <command> for details.")
	return lines
}

func (r *REPL) helpSearchLines(query string) []string {
	if strings.TrimSpace(query) == "" {
		return r.quickStartHelpLines()
	}
	if strings.EqualFold(strings.TrimSpace(query), "attach") {
		lines := append([]string{}, attachHelpLines()...)
		lines = append(lines, "")
		lines = append(lines, r.contextualHelpLines()...)
		lines = append(lines, "", "Try /attach image <path> or use @ to insert a token.")
		return lines
	}
	if replacement, ok := deprecatedCommandReplacements[query]; ok {
		lines := r.removedCommandHelpLines(query, replacement)
		lines = append(lines, "", "Try /pause, /continue, /cancel, or /quit.")
		return lines
	}
	matches := r.commands.Search(query, 6)
	dynamic := r.commands.SearchDynamic(query, 6)
	lines := []string{}
	if len(matches) == 0 && len(dynamic) == 0 {
		lines = append(lines, fmt.Sprintf("No matching commands for %q.", query))
	} else if len(matches) > 0 {
		lines = append(lines, fmt.Sprintf("Built-in matches for %q", query))
		for _, command := range matches {
			lines = append(lines, fmt.Sprintf("/%-12s %s", command.Name, defaultString(r.commands.HintFor(command.Name), command.Description)))
		}
	}
	if len(dynamic) > 0 {
		lines = append(lines, "", "Runtime Inventory (reference only)")
		lines = append(lines, "Advertised by the connected runtime. These are inventory entries, not REPL slash commands.")
		for _, command := range dynamic {
			lines = append(lines, runtimeInventoryRow(command))
		}
	}
	lines = append(lines, "")
	lines = append(lines, r.contextualHelpLines()...)
	lines = append(lines, "", "Try /model, /session, /remote, or just ask in natural language.")
	return lines
}

func attachHelpLines() []string {
	return []string{
		"/attach inserts an attachment token into the composer instead of sending a slash command.",
		"Usage: /attach <image|file|dir|video> <path>",
		"",
		"TTY: inserts a token into the composer workbench.",
		"Fallback: stages the attachment for your next message; one-shot /attach submits it immediately.",
		"",
		"Examples",
		"/attach image ./tmp/screenshot.png",
		"/attach file ./docs/spec.md",
		"/attach dir ./internal/cli",
		"",
		"Use @ to browse candidates interactively, or Ctrl+V to paste image data.",
	}
}

func runtimeInventoryRow(command acp.Command) string {
	name := strings.TrimPrefix(strings.TrimSpace(command.Name), "/")
	description := strings.TrimSpace(command.Description)
	if description == "" {
		description = "Advertised by the connected runtime"
	}
	return fmt.Sprintf("%-12s %s · inventory only", name, description)
}

func (r *REPL) runtimeInventoryHelpLines(command acp.Command) []string {
	name := strings.TrimPrefix(strings.TrimSpace(command.Name), "/")
	description := strings.TrimSpace(command.Description)
	if description == "" {
		description = "Advertised by the connected runtime"
	}
	lines := []string{
		"Inventory entry: " + name,
		"Description: " + description,
		"Source: connected runtime inventory",
		"Status: reference only",
		"Execution: this REPL does not execute runtime inventory entries as slash commands",
		"Behavior: typing /" + name + " here returns an explicit unsupported-command error instead of sending it to the model.",
	}
	if contextual := r.contextualHelpLines(); len(contextual) > 0 {
		lines = append(lines, "")
		lines = append(lines, contextual...)
	}
	lines = append(lines, "", "Use built-in commands like /help, /model, /session, or /remote in this REPL.")
	return lines
}

func (r *REPL) commandHelpLines(name, description, usage string) []string {
	group := r.commands.GroupFor(name)
	intent := defaultString(r.commands.HintFor(name), description)
	lines := []string{
		"/" + name + " — " + description,
		"Group: " + group,
		"Purpose: " + intent,
		"Usage: " + usage,
	}
	if flows := commandHelpFlows(name, group); len(flows) > 0 {
		lines = append(lines, "", "Common flows")
		lines = append(lines, flows...)
	}
	if examples := commandHelpExamples(name); len(examples) > 0 {
		lines = append(lines, "", "Examples")
		lines = append(lines, examples...)
	}
	if contextual := r.contextualHelpLines(); len(contextual) > 0 {
		lines = append(lines, "")
		lines = append(lines, contextual...)
	}
	lines = append(lines, "", "Try /model, /session, /remote, or just ask in natural language.")
	return lines
}

func (r *REPL) contextualHelpLines() []string {
	phase := r.phase
	if phase == "" {
		phase = PhaseIdle
	}
	switch {
	case r.pendingApproval || phase == PhaseWaitingApproval:
		return []string{
			"State: approval",
			"y approve once · a always · n deny · v details",
		}
	case r.pausedRun != nil || phase == PhasePaused:
		return []string{
			"State: paused",
			"Enter continue · x discard · /retry · /quit",
		}
	case phase == PhaseCancelled:
		return []string{
			"State: cancelled",
			"/last · /runs recent · /quit",
		}
	case r.running || (phase != PhaseIdle && phase != PhaseCompleted && phase != PhaseCancelled && phase != PhaseError):
		return []string{
			"State: running",
			"Esc pause · /cancel · Ctrl+C quit",
		}
	default:
		return []string{
			"State: idle",
			"Type normally, or run /help <command>.",
		}
	}
}

func commandHelpFlows(name, group string) []string {
	switch name {
	case "pause":
		return []string{
			"Pause the current foreground run and keep a resumable handle in this REPL.",
			"Use when you want to stop execution now but continue from the last stable step later.",
		}
	case "continue":
		return []string{
			"Continue the current paused task, or pass a run/conversation reference to restore older work.",
			"Use after Esc, /pause, or when the startup briefing points at a resumable run.",
		}
	case "cancel":
		return []string{
			"Cancel the current foreground run and do not keep a paused handle.",
			"Use when you want to stop immediately and end the current execution path.",
		}
	case "retry":
		return []string{
			"Restart the last paused task from the last user message instead of the stable step.",
			"Use when the current run drifted, failed, or needs a clean retry.",
		}
	case "model":
		return []string{
			"Run /model with no args to inspect the current model or open the picker.",
			"Pass a model id directly to switch without opening the picker.",
			"Pair with /think on or /think off when the selected model supports reasoning.",
		}
	case "session":
		return []string{
			"Run /session to browse recent conversations and switch context.",
			"Use /session new <key> to start a fresh conversation under a stable name.",
		}
	case "remote":
		return []string{
			"Run /remote to inspect or switch the active remote/local runtime.",
			"Use /remote login <name> when a saved remote needs credentials before switching.",
		}
	case "badge":
		return []string{
			"Open badge management to change the dock identity marker.",
			"Use badge commands when you want a different personal identity marker in the workbench gutter.",
		}
	case "approvals":
		return []string{
			"List pending approvals and resolve them without leaving the REPL.",
			"Use when an approval prompt scrolled away or you need to audit pending requests.",
		}
	case "quality":
		return []string{
			"Inspect release-readiness signals, blockers, and warning counts.",
			"Use before shipping, after risky edits, or when tests/evals look inconsistent.",
		}
	case "evals":
		return []string{
			"List available eval suites, then run one with /evals run <suite_id>.",
			"Use after behavior changes that need repeatable verification beyond a single task run.",
		}
	case "runs":
		return []string{
			"Inspect recent runs, then jump into /last or /continue <run_id> for detail.",
			"Use when you need continuity across pauses, failures, or previous conversations.",
		}
	case "context":
		return []string{
			"Inspect current context usage and compaction guidance before long tasks.",
			"Use with /compact when the window is getting tight.",
		}
	case "compact":
		return []string{
			"Condense older conversation history into a shorter summary while keeping the same conversation.",
			"Use when context usage is high but you want to preserve continuity.",
		}
	case "discard":
		return []string{
			"Discard the current paused task without resuming it.",
			"Use when the paused run is no longer useful and you want a clean slate.",
		}
	case "background", "bg":
		return []string{
			"Detach the current foreground run into background supervision.",
			"Use when the task is long-running and you want to start a new conversation.",
		}
	case "foreground", "fg":
		return []string{
			"Bring a background or paused run back into the foreground.",
			"Pass a run ID to select which run to foreground.",
		}
	case "episode":
		return []string{
			"Insert an episode boundary within the current conversation.",
			"Use to signal a topic change so the model can better segment context.",
		}
	case "reset":
		return []string{
			"Reset the current conversation state (destructive).",
			"Use when the conversation has drifted and you want a clean restart under the same conversation key.",
		}
	case "cd":
		return []string{
			"Change the working directory and rebind the project context.",
			"Memories and project-scoped state will update to match the new directory.",
		}
	case "think":
		return []string{
			"Toggle higher-effort reasoning (thinking mode) for the current model.",
			"Only effective on models that support thinking. Check /model for details.",
		}
	case "memory":
		return []string{
			"Search, inspect, pin, or delete remembered facts.",
			"Use /memory pin <text> to save a fact, /memory search <query> to find, /memory delete <key> to remove.",
		}
	case "attach":
		return []string{
			"Insert or stage an attachment using the shared content-block model.",
			"In fallback mode, the attachment is staged and sent with your next message.",
		}
	case "tools":
		return []string{
			"Inspect runtime tools visible to the current target and conversation context.",
			"Use /tools search <query> to filter, /tools info <name> for detail, and /tools check to verify availability.",
		}
	case "skills":
		return []string{
			"Inspect installed skills and browse the skill catalog from the REPL.",
			"Use /skills search <query> to browse, /skills install <name-or-path> to add, and /skills remove <name> to uninstall.",
		}
	case "last":
		return []string{
			"Show the most recent run's details including output, delivery, and status.",
			"Shortcut for /runs then selecting the latest entry.",
		}
	case "run":
		return []string{
			"Show the full detail view for a specific run ID.",
			"Use after /runs, recovery hints, or supervisor references when you already know the run.",
		}
	case "automation":
		return []string{
			"Inspect automation items (cron, watch, wakeup schedules).",
			"Use /automation pause|resume|run <kind> <id> to control items directly.",
		}
	case "promote":
		return []string{
			"Promote a recent successful run into a recurring automation.",
			"Opens a wizard to configure schedule and delivery from the run's template.",
		}
	case "delivery":
		return []string{
			"Inspect delivery targets, receipts, and redrive dead-letter items.",
			"Use /delivery redrive <id> to manually resend a failed delivery.",
		}
	case "doctor":
		return []string{
			"Run system health checks and show readiness diagnostics.",
			"Use when things feel broken, or after changing targets/remotes.",
		}
	case "help":
		return []string{
			"Show help for a command, domain group, or keyword.",
			"Try /help task, /help session, /help runtime, or /help <command>.",
		}
	case "status":
		return []string{
			"Show a one-line summary of the current conversation, model, target, and execution state.",
		}
	case "view":
		return []string{
			"Switch the dock layout between full, compact, plain, and auto modes.",
			"Use /view compact when terminal space is tight.",
		}
	}
	switch group {
	case commandGroupTaskControl:
		return []string{"This command controls the foreground task lifecycle inside the REPL."}
	case commandGroupSession:
		return []string{"This command manages conversation continuity, history, or context within the current conversation."}
	case commandGroupRuntime:
		return []string{"This command changes where or how the REPL executes work."}
	case commandGroupGovernance:
		return []string{"This command inspects approvals, quality gates, or evaluation signals."}
	case commandGroupIdentity:
		return []string{"This command changes how the terminal presents your identity and badge metadata."}
	default:
		return []string{"This command surfaces REPL state, diagnostics, or navigation help."}
	}
}

func commandHelpExamples(name string) []string {
	switch name {
	case "pause":
		return []string{"/pause"}
	case "continue":
		return []string{"/continue", "/continue run_128", "/continue ops-incident"}
	case "cancel":
		return []string{"/cancel"}
	case "retry":
		return []string{"/retry"}
	case "model":
		return []string{"/model", "/model gpt-5.4", "/think on"}
	case "session":
		return []string{"/session", "/session default", "/session new ops-incident"}
	case "remote":
		return []string{"/remote", "/remote prod-eu", "/remote login prod-eu"}
	case "badge":
		return []string{"/badge", "/badge set A", "/badge color #00ff88"}
	case "approvals":
		return []string{"/approvals", "/approvals approve approval-1", "/approvals deny approval-1"}
	case "quality":
		return []string{"/quality"}
	case "evals":
		return []string{"/evals", "/evals run browser.smoke"}
	case "runs":
		return []string{"/runs recent", "/continue run_128", "/last"}
	case "context":
		return []string{"/context", "/compact"}
	case "compact":
		return []string{"/compact"}
	case "status":
		return []string{"/status"}
	case "help":
		return []string{"/help", "/help model", "/help session", "/help task"}
	case "discard":
		return []string{"/discard"}
	case "background", "bg":
		return []string{"/background", "/bg"}
	case "foreground", "fg":
		return []string{"/foreground run_128", "/fg run_128"}
	case "episode":
		return []string{"/episode"}
	case "reset":
		return []string{"/reset"}
	case "cd":
		return []string{"/cd ~/projects/api", "/cd .."}
	case "think":
		return []string{"/think", "/think on", "/think off"}
	case "memory":
		return []string{"/memory", "/memory search deploy", "/memory pin 'API uses port 8080'", "/memory delete mem_42", "/memory conflicts", "/memory pending"}
	case "delivery":
		return []string{"/delivery", "/delivery redrive gdel_101"}
	case "attach":
		return []string{"/attach image ./tmp/screenshot.png", "/attach file ./docs/spec.md"}
	case "tools":
		return []string{"/tools", "/tools search browser", "/tools info fs.read", "/tools check"}
	case "skills":
		return []string{"/skills", "/skills search review", "/skills install review-skill", "/skills remove review-skill"}
	case "last":
		return []string{"/last"}
	case "run":
		return []string{"/run run_128"}
	case "automation":
		return []string{"/automation", "/automation pause cron cron_1", "/automation run cron cron_1"}
	case "promote":
		return []string{"/promote"}
	case "doctor":
		return []string{"/doctor"}
	case "view":
		return []string{"/view", "/view compact", "/view full"}
	default:
		return []string{"/" + name}
	}
}
