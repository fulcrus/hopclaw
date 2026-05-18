package acp

// defaultCommands returns the slash commands available in ACP sessions.
func defaultCommands() []Command {
	return []Command{
		{Name: "help", Description: "Show available commands"},
		{Name: "status", Description: "Show current session status"},
		{Name: "context", Description: "Show context window info"},
		{Name: "usage", Description: "Show token usage"},
		{Name: "cancel", Description: "Cancel current run", Shortcut: "/cancel"},
		{Name: "compact", Description: "Compact conversation history"},
		{Name: "think", Description: "Toggle extended thinking", Shortcut: "/think"},
		{Name: "verbose", Description: "Toggle verbose output", Shortcut: "/verbose"},
		{Name: "model", Description: "Change model", Shortcut: "/model"},
		{Name: "queue", Description: "Show run queue"},
		{Name: "debug", Description: "Toggle debug mode"},
		{Name: "config", Description: "Show/set config options"},
	}
}

// DefaultCommands returns a copy of the default ACP session command inventory.
func DefaultCommands() []Command {
	return append([]Command(nil), defaultCommands()...)
}
