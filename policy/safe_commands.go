package policy

import (
	"log/slog"
	"regexp"
	"strings"

	"github.com/fulcrus/hopclaw/logging"
)

// SafeCommandMatcher checks whether a shell command matches the safe command whitelist.
type SafeCommandMatcher struct {
	patterns        []*regexp.Regexp
	invalidPatterns []string
}

var log = logging.WithSubsystem("policy")

// DefaultSafePatterns returns the default set of safe command patterns.
// These commands are read-only and pose no risk to system state.
func DefaultSafePatterns() []string {
	return []string{
		`^ls(\s|$)`,
		`^cat\s`,
		`^head\s`,
		`^tail\s`,
		`^wc(\s|$)`,
		`^echo(\s|$)`,
		`^date(\s|$)`,
		`^whoami(\s|$)`,
		`^pwd(\s|$)`,
		`^uname(\s|$)`,
		`^env(\s|$)`,
		`^printenv(\s|$)`,
		`^git\s+(status|log|diff|show|branch|tag|remote|rev-parse)(\s|$)`,
		`^which\s`,
		`^file\s`,
		`^stat\s`,
		`^df(\s|$)`,
		`^du\s`,
		`^hostname(\s|$)`,
		`^uptime(\s|$)`,
		`^id(\s|$)`,
		`^groups(\s|$)`,
	}
}

// NewSafeCommandMatcher creates a matcher from the given patterns.
// Invalid patterns are skipped but recorded and logged.
func NewSafeCommandMatcher(patterns []string) *SafeCommandMatcher {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	invalid := make([]string, 0)
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			invalid = append(invalid, p)
			continue
		}
		compiled = append(compiled, re)
	}
	if len(invalid) > 0 {
		log.Warn("invalid safe command patterns skipped", slog.Int("count", len(invalid)))
	}
	return &SafeCommandMatcher{patterns: compiled, invalidPatterns: invalid}
}

// IsSafe returns true if the command matches any safe pattern.
func (m *SafeCommandMatcher) IsSafe(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	for _, re := range m.patterns {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}

func (m *SafeCommandMatcher) InvalidPatterns() []string {
	if m == nil || len(m.invalidPatterns) == 0 {
		return nil
	}
	return append([]string(nil), m.invalidPatterns...)
}
