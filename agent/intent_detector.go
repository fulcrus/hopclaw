package agent

import "strings"

// ---------------------------------------------------------------------------
// Intent Detection — Semantic classification of user messages
// ---------------------------------------------------------------------------

// ToolIntent represents detected user preferences for specific tools/commands.
type ToolIntent struct {
	// ExplicitCommands contains shell commands the user explicitly requested.
	// When non-empty, the model should respect these choices.
	ExplicitCommands []string `json:"explicit_commands,omitempty"`

	// MessageType classifies the message for tool selection.
	MessageType MessageType `json:"message_type"`

	// RequiresCurrentInfo is true when the request likely depends on
	// current, recent, or otherwise time-sensitive facts.
	RequiresCurrentInfo bool `json:"requires_current_info,omitempty"`

	// Confidence is the classifier's confidence in the classification (0-1).
	Confidence float64 `json:"confidence,omitempty"`

	// Reason explains the classification decision.
	Reason string `json:"reason,omitempty"`
}

// HasExplicit returns true if user explicitly requested any commands.
func (ti ToolIntent) HasExplicit(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, explicit := range ti.ExplicitCommands {
		if strings.Contains(lower, strings.ToLower(explicit)) {
			return true
		}
	}
	return false
}

// ShouldSuggestAlternatives returns true if we should suggest built-in tools.
func (ti ToolIntent) ShouldSuggestAlternatives() bool {
	return len(ti.ExplicitCommands) == 0
}

// MessageType classifies messages for tool selection.
type MessageType string

const (
	// MessageTypeKnowledge is a pure knowledge question (no tools needed).
	MessageTypeKnowledge MessageType = "knowledge"

	// MessageTypeAction requires executing actions/tools.
	MessageTypeAction MessageType = "action"

	// MessageTypeHybrid may need both knowledge and action.
	MessageTypeHybrid MessageType = "hybrid"
)

// ---------------------------------------------------------------------------
// Fallback: Fast heuristic detection (for when LLM is unavailable)
// ---------------------------------------------------------------------------

// DetectFast provides a fast heuristic-based detection without LLM.
// It detects explicit command requests via backtick/quote/standalone-line patterns.
// Language understanding is delegated entirely to the model.
func DetectFast(message string) ToolIntent {
	trimmed := strings.TrimSpace(message)
	lower := strings.ToLower(trimmed)
	explicitCommands := detectExplicitCommands(lower)
	return ToolIntent{
		ExplicitCommands: explicitCommands,
		MessageType:      MessageTypeAction,
	}
}

func detectExplicitCommands(lower string) []string {
	knownCommands := []string{"openssl", "curl", "wget", "jq", "shasum", "md5", "base64"}
	commands := make([]string, 0, 2)
	for _, cmd := range knownCommands {
		if hasExplicitCommandStructure(lower, cmd) {
			commands = appendExplicitCommand(commands, cmd)
		}
	}
	return commands
}

func appendExplicitCommand(commands []string, command string) []string {
	for _, existing := range commands {
		if existing == command {
			return commands
		}
	}
	return append(commands, command)
}

func hasExplicitCommandStructure(lower string, command string) bool {
	switch {
	case strings.Contains(lower, "`"+command+"`"):
		return true
	case strings.Contains(lower, `"`+command+`"`):
		return true
	case strings.Contains(lower, "'"+command+"'"):
		return true
	}
	for _, line := range strings.Split(lower, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == command:
			return true
		case strings.HasPrefix(trimmed, command+" "):
			return true
		case strings.HasPrefix(trimmed, command+"\t"):
			return true
		}
	}
	return false
}
