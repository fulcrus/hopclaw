package repl

import (
	"fmt"
	"strings"
)

// shellHandoffCommand extracts a controlled shell handoff command from user
// input. The command syntax is ASCII-only `!cmd`.
func shellHandoffCommand(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "!") {
		return "", false
	}
	command := strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
	if command == "" {
		return "", false
	}
	return command, true
}

// shellHandoffMessage rewrites `!cmd` into an explicit tool-use request while
// preserving the existing approval and sandbox flow.
func shellHandoffMessage(input string) (string, bool) {
	command, ok := shellHandoffCommand(input)
	if !ok {
		return input, false
	}
	return fmt.Sprintf(
		"Use the `exec.shell` tool to run the exact shell command below in the current workspace. "+
			"Keep the normal approval and sandbox flow. Do not rewrite the command.\n\nCommand:\n%s",
		command,
	), true
}
