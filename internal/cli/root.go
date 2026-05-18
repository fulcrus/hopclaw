package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/fulcrus/hopclaw/internal/edition"
	"github.com/fulcrus/hopclaw/internal/version"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/spf13/cobra"
)

var log = logging.WithSubsystem("cli")

// ---------------------------------------------------------------------------
// Global flags
// ---------------------------------------------------------------------------

var (
	flagConfig             string
	flagVerbose            bool
	flagJSON               bool
	flagInteractiveModel   string
	flagInteractiveThink   bool
	flagInteractiveSession string
	flagRemote             string
	flagLocal              bool
)

// ---------------------------------------------------------------------------
// Root command
// ---------------------------------------------------------------------------

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "hopclaw",
		Short: "HopClaw - AI terminal for interactive work, one-shot asks, and runtime operations",
		Long: fmt.Sprintf(
			"HopClaw %s (%s edition)\n"+
				"Use `hopclaw` to open the interactive terminal.\n"+
				"Use `hopclaw \"...\"` or pipe stdin for one task and exit.\n"+
				"Use `hopclaw <command>` for runtime, session, tool, and automation operations.\n"+
				"Inside the terminal, exit with Ctrl+C, /quit, or /exit.",
			version.Version, edition.Edition,
		),
		Example: strings.Join([]string{
			"  hopclaw",
			"    Start the interactive terminal",
			"",
			"  hopclaw \"summarize the current repo status\"",
			"    Run one task and exit",
			"",
			"  git diff --stat | hopclaw",
			"    Send stdin as a one-shot task",
			"",
			"  hopclaw sessions list",
			"    Inspect saved sessions and runtime state",
		}, "\n"),
		Args:          validateRootArgs,
		RunE:          runInteractive,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Global persistent flags.
	pf := root.PersistentFlags()
	pf.StringVar(&flagConfig, "config", "", "path to YAML config file")
	pf.BoolVar(&flagVerbose, "verbose", false, "enable verbose output")
	pf.BoolVar(&flagJSON, "json", false, "output in JSON format")
	pf.StringVar(&flagRemote, "remote", "", "runtime connection for gateway-backed commands (saved remote, local runtime, or URL)")
	pf.BoolVar(&flagLocal, "local", false, "force gateway-backed commands to use the configured local runtime")
	root.Flags().StringVarP(&flagInteractiveSession, "session", "s", "", "session key for interactive REPL")
	root.Flags().StringVar(&flagInteractiveModel, "model", "", "model for interactive REPL")
	root.Flags().BoolVar(&flagInteractiveThink, "think", false, "enable thinking mode in interactive REPL")

	root.AddGroup(
		&cobra.Group{ID: "start", Title: "Getting Started"},
		&cobra.Group{ID: "runtime", Title: "Runtime & Monitoring"},
		&cobra.Group{ID: "automation", Title: "Automation & Integrations"},
		&cobra.Group{ID: "ops", Title: "Config, Ops & Maintenance"},
	)

	serveCmd := newServeCmd()
	serveCmd.GroupID = "start"
	setupCmd := newSetupCmd()
	setupCmd.GroupID = "start"
	onboardCmd := newOnboardCmd()
	onboardCmd.GroupID = "start"
	dashboardCmd := newDashboardCmd()
	dashboardCmd.GroupID = "start"

	versionCmd := newVersionCmd()
	versionCmd.GroupID = "ops"
	statusCmd := newStatusCmd()
	statusCmd.GroupID = "runtime"
	healthCmd := newHealthCmd()
	healthCmd.GroupID = "runtime"
	configCmd := newConfigCmd()
	configCmd.GroupID = "ops"
	secretsCmd := newSecretsCmd()
	secretsCmd.GroupID = "ops"
	daemonCmd := newDaemonCmd()
	daemonCmd.GroupID = "ops"
	dnsCmd := newDNSCmd()
	dnsCmd.GroupID = "ops"
	updateCmd := newUpdateCmd()
	updateCmd.GroupID = "ops"
	bugReportCmd := newBugReportCmd()
	bugReportCmd.GroupID = "ops"
	doctorCmd := newDoctorCmd()
	doctorCmd.GroupID = "ops"
	completionCommand := completionCmd()
	completionCommand.GroupID = "ops"
	messageCmd := newMessageCmd()
	messageCmd.GroupID = "runtime"
	sessionsCmd := newSessionsCmd()
	sessionsCmd.GroupID = "runtime"
	memoryCmd := newMemoryCmd()
	memoryCmd.GroupID = "runtime"
	projectCmd := newProjectCmd()
	projectCmd.GroupID = "runtime"
	qualityCmd := newQualityCmd()
	qualityCmd.GroupID = "runtime"
	evalsCmd := newEvalsCmd()
	evalsCmd.GroupID = "runtime"
	nodesCmd := newNodesCmd()
	nodesCmd.GroupID = "runtime"
	deviceCmd := newDeviceCmd()
	deviceCmd.GroupID = "runtime"
	toolsCmd := newToolsCmd()
	toolsCmd.GroupID = "automation"
	skillsCmd := newSkillsCmd()
	skillsCmd.GroupID = "automation"
	automationCmd := newAutomationCmd()
	automationCmd.GroupID = "automation"
	hooksCmd := newHooksCmd()
	hooksCmd.GroupID = "automation"
	qrCmd := newQRCmd()
	qrCmd.GroupID = "runtime"
	sandboxCmd := newSandboxCmd()
	sandboxCmd.GroupID = "ops"
	tuiCmd := newTUICmd()
	tuiCmd.GroupID = "runtime"
	approvalsCmd := newApprovalsCmd()
	approvalsCmd.GroupID = "runtime"
	modelsCmd := newModelsCmd()
	modelsCmd.GroupID = "runtime"
	logsCmd := newLogsCmd()
	logsCmd.GroupID = "ops"
	channelsCmd := newChannelsCmd()
	channelsCmd.GroupID = "automation"
	browserCmd := newBrowserCmd()
	browserCmd.GroupID = "runtime"
	agentsCmd := newAgentsCmd()
	agentsCmd.GroupID = "runtime"
	pairingCmd := newPairingCmd()
	pairingCmd.GroupID = "runtime"
	targetCmd := newTargetCmd()
	targetCmd.GroupID = "runtime"
	backupCmd := newBackupCmd()
	backupCmd.GroupID = "ops"
	webhooksCmd := newWebhooksCmd()
	webhooksCmd.GroupID = "automation"
	pluginsCmd := newPluginsCmd()
	pluginsCmd.GroupID = "automation"
	securityCmd := newSecurityCmd()
	securityCmd.GroupID = "ops"
	uninstallCmd := newUninstallCmd()
	uninstallCmd.GroupID = "ops"

	root.AddCommand(
		serveCmd,
		setupCmd,
		onboardCmd,
		dashboardCmd,
		versionCmd,
		statusCmd,
		healthCmd,
		configCmd,
		secretsCmd,
		daemonCmd,
		dnsCmd,
		updateCmd,
		bugReportCmd,
		doctorCmd,
		completionCommand,
		messageCmd,
		sessionsCmd,
		memoryCmd,
		projectCmd,
		qualityCmd,
		evalsCmd,
		nodesCmd,
		deviceCmd,
		toolsCmd,
		skillsCmd,
		automationCmd,
		hooksCmd,
		qrCmd,
		sandboxCmd,
		tuiCmd,
		approvalsCmd,
		modelsCmd,
		logsCmd,
		channelsCmd,
		browserCmd,
		agentsCmd,
		pairingCmd,
		targetCmd,
		backupCmd,
		webhooksCmd,
		pluginsCmd,
		securityCmd,
		uninstallCmd,
	)

	return root
}

type cliExitError struct {
	code  int
	err   error
	quiet bool
}

func (e *cliExitError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *cliExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *cliExitError) ExitCode() int {
	if e == nil || e.code == 0 {
		return 1
	}
	return e.code
}

func (e *cliExitError) Quiet() bool {
	return e != nil && e.quiet
}

func silentExitError(code int) error {
	return &cliExitError{code: code, quiet: true}
}

func validateRootArgs(cmd *cobra.Command, args []string) error {
	// When the first token looks like a typoed subcommand (e.g. `hopclaw
	// runs list` where the registered name is `sessions list`), refuse to
	// hand it off to the LLM. Silently treating typos as natural-language
	// prompts costs the user model tokens and hides discoverable mistakes.
	if len(args) > 0 {
		if suggestion := suggestSubcommand(cmd, args[0]); suggestion != "" {
			return fmt.Errorf("unknown command %q. Did you mean %q?", args[0], suggestion)
		}
	}
	if allowInteractiveRootArgs(cmd, args) {
		return nil
	}
	return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
}

// suggestSubcommand returns the closest registered subcommand name when the
// caller's first token is within a short Levenshtein distance, or "" when no
// close match exists. Distance threshold scales with token length so short
// words still tolerate one typo.
func suggestSubcommand(cmd *cobra.Command, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	// Whitespace / punctuation almost certainly means natural language.
	if strings.ContainsAny(token, " \t\n\"'.,!?") {
		return ""
	}
	// Tokens that obviously aren't subcommand names (uppercase letters,
	// non-ASCII) are most likely prompt fragments — leave them to the LLM.
	for _, r := range token {
		if r > 127 {
			return ""
		}
	}
	// Only suggest for tokens that look like a deliberate single-word
	// command, and require a one-edit match. A higher tolerance was matching
	// natural-language inputs like "hello" against unrelated subcommands
	// such as "help".
	if len(token) < 4 {
		return ""
	}
	const maxDist = 1
	best := ""
	bestDist := maxDist + 1
	for _, sub := range cmd.Commands() {
		names := append([]string{sub.Name()}, sub.Aliases...)
		for _, name := range names {
			if name == token {
				return ""
			}
			// Skip suggestions that share no prefix with the input — those
			// almost always indicate a casual prompt, not a typo.
			if !strings.EqualFold(string(name[0]), string(token[0])) {
				continue
			}
			d := levenshtein(strings.ToLower(token), strings.ToLower(name))
			if d < bestDist {
				bestDist = d
				best = name
			}
		}
	}
	if best == "" || bestDist > maxDist {
		return ""
	}
	return best
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			min := prev[j] + 1
			if curr[j-1]+1 < min {
				min = curr[j-1] + 1
			}
			if prev[j-1]+cost < min {
				min = prev[j-1] + cost
			}
			curr[j] = min
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func allowInteractiveRootArgs(cmd *cobra.Command, args []string) bool {
	_, stdinTTY := readerFile(cmd.InOrStdin())
	_, stdoutTTY := writerFile(cmd.OutOrStdout())
	return allowInteractiveRootArgsWithTTY(args, stdinTTY, stdoutTTY)
}

func allowInteractiveRootArgsWithTTY(args []string, stdinTTY, stdoutTTY bool) bool {
	if len(args) == 0 {
		return true
	}
	if strings.TrimSpace(flagInteractiveSession) != "" || strings.TrimSpace(flagInteractiveModel) != "" || flagInteractiveThink || strings.TrimSpace(flagRemote) != "" || flagLocal {
		return true
	}
	if len(args) > 1 {
		return true
	}
	return !stdinTTY || !stdoutTTY
}

// Main is the entry point called from cmd/hopclaw/main.go.
func Main() {
	root := newRootCmd()
	defer handleRootPanic()
	if err := root.Execute(); err != nil {
		exitCode := 1
		quiet := false
		var cliErr *cliExitError
		if errors.As(err, &cliErr) {
			exitCode = cliErr.ExitCode()
			quiet = cliErr.Quiet()
		}
		if !quiet && strings.TrimSpace(err.Error()) != "" {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(exitCode)
	}
}
