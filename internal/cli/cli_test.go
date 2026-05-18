package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(--help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"serve", "setup", "onboard", "dashboard", "version", "status", "health", "config", "secrets", "daemon", "devices", "pairing", "update", "bug-report", "doctor", "message", "sessions", "memory", "project", "quality", "evals", "tools", "skills", "automation", "hooks"} {
		if !strings.Contains(output, sub) {
			t.Errorf("help output missing subcommand %q", sub)
		}
	}
	if strings.Contains(output, "\ntui") || strings.Contains(output, "  tui") {
		t.Fatalf("root help should hide legacy tui entry: %q", output)
	}
}

func TestVersionCmd(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(version) error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "hopclaw") {
		t.Errorf("version output missing 'hopclaw': %s", output)
	}
}

func TestVersionCmdJSON(t *testing.T) {
	// Reset the global flag.
	flagJSON = true
	defer func() { flagJSON = false }()

	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"version", "--json"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(version --json) error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"version"`) {
		t.Errorf("JSON version output missing 'version' key: %s", output)
	}
}

func TestDaemonCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"daemon", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(daemon --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"install", "uninstall", "start", "stop", "restart", "status"} {
		if !strings.Contains(output, sub) {
			t.Errorf("daemon help output missing subcommand %q", sub)
		}
	}
}

func TestSecretsCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"secrets", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(secrets --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"set", "get", "delete", "list"} {
		if !strings.Contains(output, sub) {
			t.Errorf("secrets help output missing subcommand %q", sub)
		}
	}
}

func TestMessageCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"message", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(message --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"send", "list"} {
		if !strings.Contains(output, sub) {
			t.Errorf("message help output missing subcommand %q", sub)
		}
	}
	for _, want := range []string{
		"interactive terminal or one-shot asks",
		"explicit session/run messaging operations",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("message help missing %q: %q", want, output)
		}
	}
}

func TestMessageSendHelpDirectsAdHocUsersToRootOneShot(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"message", "send", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(message send --help) error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		"For ad-hoc asks, prefer `hopclaw \"...\"`.",
		"Preferred for ad-hoc one-shot asks",
		"Use when you need an explicit session key in scripts",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("message send help missing %q: %q", want, output)
		}
	}
}

func TestSessionsCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"sessions", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(sessions --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"list", "get"} {
		if !strings.Contains(output, sub) {
			t.Errorf("sessions help output missing subcommand %q", sub)
		}
	}
}

func TestMemoryCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"memory", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(memory --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"get", "set", "delete", "search"} {
		if !strings.Contains(output, sub) {
			t.Errorf("memory help output missing subcommand %q", sub)
		}
	}
}

func TestQualityCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"quality", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(quality --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"summary", "readiness"} {
		if !strings.Contains(output, sub) {
			t.Errorf("quality help output missing subcommand %q", sub)
		}
	}
}

func TestEvalsCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"evals", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(evals --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"list", "run"} {
		if !strings.Contains(output, sub) {
			t.Errorf("evals help output missing subcommand %q", sub)
		}
	}
}

func TestSkillsCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"skills", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(skills --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"list", "search", "install", "remove"} {
		if !strings.Contains(output, sub) {
			t.Errorf("skills help output missing subcommand %q", sub)
		}
	}
}

func TestToolsCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"tools", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(tools --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"list", "search", "info", "check"} {
		if !strings.Contains(output, sub) {
			t.Errorf("tools help output missing subcommand %q", sub)
		}
	}
}

func TestConfigCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"config", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(config --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"get", "set", "unset", "validate", "edit", "path", "show"} {
		if !strings.Contains(output, sub) {
			t.Errorf("config help output missing subcommand %q", sub)
		}
	}
}

func TestDashboardCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"dashboard", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(dashboard --help) error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "--open") {
		t.Fatalf("dashboard help missing --open flag: %s", output)
	}
}

func TestAutomationCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"automation", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(automation --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"list", "inspect", "recent", "templates", "pause", "resume", "create", "delete", "trigger", "status"} {
		if !strings.Contains(output, sub) {
			t.Errorf("automation help output missing subcommand %q", sub)
		}
	}
}

func TestHooksCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"hooks", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(hooks --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"list", "inspect", "recent", "errors", "test-fire", "replay", "delete"} {
		if !strings.Contains(output, sub) {
			t.Errorf("hooks help output missing subcommand %q", sub)
		}
	}
}

func TestChannelsCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"channels", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(channels --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"list", "status", "test", "add", "remove", "logs"} {
		if !strings.Contains(output, sub) {
			t.Errorf("channels help output missing subcommand %q", sub)
		}
	}
}
