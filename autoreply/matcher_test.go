package autoreply

import (
	"fmt"
	"testing"
)

func TestMatchExact(t *testing.T) {
	t.Parallel()
	m := NewMatcher()

	tests := []struct {
		pattern string
		content string
		want    bool
	}{
		{"hello", "hello", true},
		{"Hello", "hello", true},
		{"HELLO", "hello", true},
		{"hello", "Hello World", false},
		{"", "", true},
	}

	for _, tt := range tests {
		rule := Rule{MatchMode: MatchExact, Pattern: tt.pattern}
		got := m.Match(rule, tt.content)
		if got != tt.want {
			t.Errorf("matchExact(%q, %q) = %v, want %v", tt.pattern, tt.content, got, tt.want)
		}
	}
}

func TestMatchPrefix(t *testing.T) {
	t.Parallel()
	m := NewMatcher()

	tests := []struct {
		pattern string
		content string
		want    bool
	}{
		{"hello", "hello world", true},
		{"Hello", "hello world", true},
		{"world", "hello world", false},
		{"hello world!", "hello", false},
		{"", "anything", true},
	}

	for _, tt := range tests {
		rule := Rule{MatchMode: MatchPrefix, Pattern: tt.pattern}
		got := m.Match(rule, tt.content)
		if got != tt.want {
			t.Errorf("matchPrefix(%q, %q) = %v, want %v", tt.pattern, tt.content, got, tt.want)
		}
	}
}

func TestMatchContains(t *testing.T) {
	t.Parallel()
	m := NewMatcher()

	tests := []struct {
		pattern string
		content string
		want    bool
	}{
		{"world", "hello world", true},
		{"World", "hello world", true},
		{"xyz", "hello world", false},
		{"", "anything", true},
	}

	for _, tt := range tests {
		rule := Rule{MatchMode: MatchContains, Pattern: tt.pattern}
		got := m.Match(rule, tt.content)
		if got != tt.want {
			t.Errorf("matchContains(%q, %q) = %v, want %v", tt.pattern, tt.content, got, tt.want)
		}
	}
}

func TestMatchRegex(t *testing.T) {
	t.Parallel()
	m := NewMatcher()

	tests := []struct {
		pattern string
		content string
		want    bool
	}{
		{`^hello`, "hello world", true},
		{`world$`, "hello world", true},
		{`\d+`, "order 42", true},
		{`^exact$`, "exact", true},
		{`^exact$`, "not exact", false},
	}

	for _, tt := range tests {
		rule := Rule{MatchMode: MatchRegex, Pattern: tt.pattern}
		got := m.Match(rule, tt.content)
		if got != tt.want {
			t.Errorf("matchRegex(%q, %q) = %v, want %v", tt.pattern, tt.content, got, tt.want)
		}
	}
}

func TestMatchRegexInvalid(t *testing.T) {
	t.Parallel()
	m := NewMatcher()

	rule := Rule{MatchMode: MatchRegex, Pattern: `[invalid`}
	got := m.Match(rule, "anything")
	if got {
		t.Error("expected false for invalid regex")
	}
}

func TestMatchAny(t *testing.T) {
	t.Parallel()
	m := NewMatcher()

	rule := Rule{MatchMode: MatchAny}
	for _, content := range []string{"", "hello", "anything at all"} {
		got := m.Match(rule, content)
		if !got {
			t.Errorf("matchAny should return true for %q", content)
		}
	}
}

func TestRegexCaching(t *testing.T) {
	t.Parallel()
	m := NewMatcher()

	pattern := `^cached\d+$`
	rule := Rule{MatchMode: MatchRegex, Pattern: pattern}

	// First call compiles and caches.
	m.Match(rule, "cached123")

	// Verify it is in the cache.
	m.mu.Lock()
	_, ok := m.regexCache[pattern]
	m.mu.Unlock()
	if !ok {
		t.Fatal("expected pattern to be cached after first match")
	}

	// Second call should use the cache (we just verify it still works).
	got := m.Match(rule, "cached456")
	if !got {
		t.Error("expected match using cached regex")
	}
}

func TestRegexCacheEviction(t *testing.T) {
	t.Parallel()
	m := NewMatcher()

	// Fill the cache to its limit with unique patterns.
	for i := range maxCompiledRegexCache {
		pattern := fmt.Sprintf(`^pattern_%d$`, i)
		m.Match(Rule{MatchMode: MatchRegex, Pattern: pattern}, "pattern_0")
	}

	m.mu.Lock()
	sizeBefore := len(m.regexCache)
	m.mu.Unlock()
	if sizeBefore != maxCompiledRegexCache {
		t.Fatalf("cache size = %d, want %d", sizeBefore, maxCompiledRegexCache)
	}

	// One more should trigger eviction.
	m.Match(Rule{MatchMode: MatchRegex, Pattern: `^overflow$`}, "overflow")

	m.mu.Lock()
	sizeAfter := len(m.regexCache)
	m.mu.Unlock()
	// After eviction, cache should have only the new entry.
	if sizeAfter != 1 {
		t.Fatalf("cache size after eviction = %d, want 1", sizeAfter)
	}
}

func TestMatchUnknownMode(t *testing.T) {
	t.Parallel()
	m := NewMatcher()

	rule := Rule{MatchMode: "nonexistent", Pattern: "hello"}
	if m.Match(rule, "hello") {
		t.Error("unknown match mode should return false")
	}
}
