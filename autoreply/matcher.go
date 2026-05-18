package autoreply

import (
	"regexp"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// maxCompiledRegexCache is the upper bound on cached compiled regexps.
	// When exceeded the cache is cleared to prevent unbounded growth.
	maxCompiledRegexCache = 100
)

// ---------------------------------------------------------------------------
// Matcher
// ---------------------------------------------------------------------------

// Matcher evaluates rules against incoming messages. It caches compiled
// regular expressions for repeated use.
type Matcher struct {
	mu         sync.RWMutex // guards regexCache
	regexCache map[string]*regexp.Regexp
}

// NewMatcher returns a ready-to-use Matcher.
func NewMatcher() *Matcher {
	return &Matcher{
		regexCache: make(map[string]*regexp.Regexp),
	}
}

// Match checks whether content satisfies the rule's pattern according to its
// MatchMode. Invalid regex patterns return false rather than an error.
func (m *Matcher) Match(rule Rule, content string) bool {
	switch rule.MatchMode {
	case MatchExact:
		return m.matchExact(rule.Pattern, content)
	case MatchPrefix:
		return m.matchPrefix(rule.Pattern, content)
	case MatchContains:
		return m.matchContains(rule.Pattern, content)
	case MatchRegex:
		return m.matchRegex(rule.Pattern, content)
	case MatchAny:
		return m.matchAny()
	default:
		return false
	}
}

func (m *Matcher) matchExact(pattern, content string) bool {
	return strings.EqualFold(pattern, content)
}

func (m *Matcher) matchPrefix(pattern, content string) bool {
	return strings.HasPrefix(strings.ToLower(content), strings.ToLower(pattern))
}

func (m *Matcher) matchContains(pattern, content string) bool {
	return strings.Contains(
		strings.ToLower(content),
		strings.ToLower(pattern),
	)
}

func (m *Matcher) matchRegex(pattern, content string) bool {
	re, err := m.getOrCompileRegex(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(content)
}

func (m *Matcher) matchAny() bool { return true }

// ---------------------------------------------------------------------------
// Regex cache helpers
// ---------------------------------------------------------------------------

// getOrCompileRegex returns a cached regexp or compiles and caches a new one.
// A single lock is held for the check-and-insert to avoid TOCTOU races.
func (m *Matcher) getOrCompileRegex(pattern string) (*regexp.Regexp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if re, ok := m.regexCache[pattern]; ok {
		return re, nil
	}

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	if len(m.regexCache) >= maxCompiledRegexCache {
		// Evict all entries. A simple clear is acceptable because regex
		// compilation is cheap compared to the cost of tracking LRU order.
		m.regexCache = make(map[string]*regexp.Regexp)
	}
	m.regexCache[pattern] = compiled
	return compiled, nil
}
