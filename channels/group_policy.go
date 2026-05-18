package channels

// ---------------------------------------------------------------------------
// Group Policy — Per-channel group permission management
// ---------------------------------------------------------------------------

// GroupPolicy defines permissions for a channel group/room.
type GroupPolicy struct {
	MentionRequired bool     `json:"mention_required" yaml:"mention_required"`
	AllowedTools    []string `json:"allowed_tools,omitempty" yaml:"allowed_tools,omitempty"`
	DeniedTools     []string `json:"denied_tools,omitempty" yaml:"denied_tools,omitempty"`
	MaxTokens       int      `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
}

// IsToolAllowed checks whether a tool is permitted under this policy.
func (gp *GroupPolicy) IsToolAllowed(toolName string) bool {
	if gp == nil {
		return true
	}

	// Check deny list first.
	for _, t := range gp.DeniedTools {
		if t == toolName {
			return false
		}
	}

	// If allow list is set, tool must be in it.
	if len(gp.AllowedTools) > 0 {
		for _, t := range gp.AllowedTools {
			if t == toolName {
				return true
			}
		}
		return false
	}

	return true
}
