package cli

import (
	"strings"
	"testing"
)

func TestAllowInteractiveRootArgsWithTTYRejectsUnknownSingleWord(t *testing.T) {
	restore := snapshotInteractiveFlags()
	defer restore()

	if allowInteractiveRootArgsWithTTY([]string{"doesnotexist"}, true, true) {
		t.Fatal("expected single-word TTY input to be treated as a command typo")
	}
}

func TestAllowInteractiveRootArgsWithTTYAllowsMultiWordPrompt(t *testing.T) {
	restore := snapshotInteractiveFlags()
	defer restore()

	if !allowInteractiveRootArgsWithTTY([]string{"hello", "world"}, true, true) {
		t.Fatal("expected multi-word TTY input to stay on interactive prompt path")
	}
}

func TestAllowInteractiveRootArgsWithTTYAllowsSingleWordWhenInteractiveFlagsPresent(t *testing.T) {
	restore := snapshotInteractiveFlags()
	defer restore()
	flagInteractiveModel = "gpt-4.1"

	if !allowInteractiveRootArgsWithTTY([]string{"hello"}, true, true) {
		t.Fatal("expected explicit interactive flags to keep root prompt mode enabled")
	}
}

func TestRootRemoteFlagIsAvailableToSubcommands(t *testing.T) {
	restore := snapshotInteractiveFlags()
	defer restore()

	root := newRootCmd()
	buf := new(strings.Builder)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"--remote", "prod", "version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(--remote version) error = %v", err)
	}
}

func TestRootHelpHighlightsCoreEntryFlows(t *testing.T) {
	restore := snapshotInteractiveFlags()
	defer restore()

	root := newRootCmd()
	buf := new(strings.Builder)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(--help) error = %v", err)
	}

	text := buf.String()
	for _, want := range []string{
		"Use `hopclaw` to open the interactive terminal.",
		"Use `hopclaw \"...\"` or pipe stdin for one task and exit.",
		"Use `hopclaw <command>` for runtime, session, tool, and automation operations.",
		"Inside the terminal, exit with Ctrl+C, /quit, or /exit.",
		"hopclaw \"summarize the current repo status\"",
		"git diff --stat | hopclaw",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("root help missing %q: %q", want, text)
		}
	}
}

func snapshotInteractiveFlags() func() {
	oldConfig := flagConfig
	oldVerbose := flagVerbose
	oldJSON := flagJSON
	oldModel := flagInteractiveModel
	oldThink := flagInteractiveThink
	oldSession := flagInteractiveSession
	oldRemote := flagRemote
	oldLocal := flagLocal
	return func() {
		flagConfig = oldConfig
		flagVerbose = oldVerbose
		flagJSON = oldJSON
		flagInteractiveModel = oldModel
		flagInteractiveThink = oldThink
		flagInteractiveSession = oldSession
		flagRemote = oldRemote
		flagLocal = oldLocal
	}
}
