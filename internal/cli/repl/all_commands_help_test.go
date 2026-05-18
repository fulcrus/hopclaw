package repl

import (
	"strings"
	"testing"
)

func TestAllSlashCommandsHaveHelpTopics(t *testing.T) {
	registry := NewCommandRegistry()
	repl := &REPL{commands: registry}

	commands := registry.SystemCommands()
	if len(commands) == 0 {
		t.Fatal("no REPL slash commands discovered")
	}

	for _, command := range commands {
		command := command
		t.Run(command.Name, func(t *testing.T) {
			title, lines, actions := repl.helpTopic([]string{command.Name})
			if !strings.Contains(title, "/"+command.Name) {
				t.Fatalf("title = %q, want command-specific help title", title)
			}
			if len(lines) == 0 {
				t.Fatalf("lines = %#v, want non-empty help lines", lines)
			}
			if strings.TrimSpace(actions) == "" {
				t.Fatal("expected help actions")
			}
			described, usage, ok := registry.Describe(command.Name)
			if !ok {
				t.Fatalf("Describe(%q) = !ok", command.Name)
			}
			if described.Name != command.Name {
				t.Fatalf("Describe(%q).Name = %q", command.Name, described.Name)
			}
			if strings.TrimSpace(usage) == "" {
				t.Fatalf("Describe(%q) returned empty usage", command.Name)
			}
		})
	}
}
