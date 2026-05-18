package agent

import (
	"testing"

	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
)

func TestScopeFilterNormalizeAndMatchAutomationIDs(t *testing.T) {
	t.Parallel()

	filter := ScopeFilter{
		AutomationIDs: []string{" auto-a ", "auto-b", "auto-a", ""},
	}.Normalize()

	if filter.Deny {
		t.Fatal("Normalize() should not set deny")
	}
	if len(filter.AutomationIDs) != 2 {
		t.Fatalf("AutomationIDs = %#v", filter.AutomationIDs)
	}
	if !filter.Matches(domainscope.Ref{AutomationID: "auto-a"}) {
		t.Fatal("expected scope filter to match auto-a")
	}
	if filter.Matches(domainscope.Ref{AutomationID: "auto-c"}) {
		t.Fatal("expected scope filter to reject auto-c")
	}
}

func TestScopeFilterDenyMatchesNothing(t *testing.T) {
	t.Parallel()

	filter := ScopeFilter{Deny: true}.Normalize()
	if filter.IsZero() {
		t.Fatal("deny filter should not be zero")
	}
	if filter.Matches(domainscope.Ref{}) {
		t.Fatal("deny filter should reject zero scope")
	}
	if filter.Matches(domainscope.Ref{AutomationID: "auto-a"}) {
		t.Fatal("deny filter should reject automation scope")
	}
}
