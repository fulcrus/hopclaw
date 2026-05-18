package server

import (
	"net/http/httptest"
	"testing"

	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func TestRequestScopeFilterIgnoresQueryInputsWithoutAutomationScope(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/runtime/runs?automation_id=automation-1&limit=10", nil)
	req.Header.Set("X-HopClaw-Auth-Subject", "subject-header")

	filter, err := requestScopeFilter(req)
	if err != nil {
		t.Fatalf("requestScopeFilter() error = %v", err)
	}
	if !filter.IsZero() {
		t.Fatalf("filter = %#v, want zero filter", filter)
	}
}

func TestRequestScopeFilterUsesAutomationHeaders(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/runtime/runs", nil)
	req.Header.Set("X-HopClaw-Auth-Subject", "subject-header")
	req.Header.Set("X-HopClaw-Auth-Automation-ID", "automation-1, automation-2")

	filter, err := requestScopeFilter(req)
	if err != nil {
		t.Fatalf("requestScopeFilter() error = %v", err)
	}
	if filter.IsZero() {
		t.Fatalf("filter = %#v, want scoped filter", filter)
	}
	if !filter.Matches(domainscope.Ref{AutomationID: "automation-1"}) {
		t.Fatalf("filter should match automation-1: %#v", filter)
	}
	if filter.Matches(domainscope.Ref{AutomationID: "automation-3"}) {
		t.Fatalf("filter should reject automation-3: %#v", filter)
	}
}

func TestApplySubmitAuthScopeAutoFillsSingleAutomation(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "/runtime/runs", nil)
	req.Header.Set("X-HopClaw-Auth-Automation-ID", "automation-1")

	submit := &runtimesvc.SubmitRequest{SessionKey: "chat-1", Content: "hello"}
	if err := applySubmitAuthScope(req, submit); err != nil {
		t.Fatalf("applySubmitAuthScope() error = %v", err)
	}
	if submit.AutomationID != "automation-1" {
		t.Fatalf("AutomationID = %q, want automation-1", submit.AutomationID)
	}
}

func TestApplySubmitAuthScopeRejectsAmbiguousAutomation(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "/runtime/runs", nil)
	req.Header.Set("X-HopClaw-Auth-Automation-ID", "automation-1,automation-2")

	submit := &runtimesvc.SubmitRequest{SessionKey: "chat-1", Content: "hello"}
	if err := applySubmitAuthScope(req, submit); err == nil {
		t.Fatal("expected ambiguous automation scope error")
	}
}

func TestApplySubmitAuthScopeRejectsOutOfScopeAutomation(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "/runtime/runs", nil)
	req.Header.Set("X-HopClaw-Auth-Automation-ID", "automation-1")

	submit := &runtimesvc.SubmitRequest{SessionKey: "chat-1", Content: "hello", AutomationID: "automation-2"}
	if err := applySubmitAuthScope(req, submit); err == nil {
		t.Fatal("expected out-of-scope automation error")
	}
}

func TestMergeRequestAuthScopeIntersectsHeaderAndConnectionScopes(t *testing.T) {
	t.Parallel()

	merged := mergeRequestAuthScope(requestAuthScope{
		AutomationIDs: []string{"automation-1", "automation-2"},
		Scoped:        true,
	}, []string{"automation:automation-2", "automation:automation-3"})

	if !merged.Scoped {
		t.Fatalf("merged = %#v, want scoped result", merged)
	}
	if len(merged.AutomationIDs) != 1 || merged.AutomationIDs[0] != "automation-2" {
		t.Fatalf("merged.AutomationIDs = %#v", merged.AutomationIDs)
	}
}

func TestMergeRequestAuthScopeProducesDenyFilterWhenScopesDoNotOverlap(t *testing.T) {
	t.Parallel()

	filter := mergeRequestAuthScope(requestAuthScope{
		AutomationIDs: []string{"automation-1"},
		Scoped:        true,
	}, []string{"automation:automation-2"}).scopeFilter()

	if filter.IsZero() {
		t.Fatalf("filter = %#v, want deny filter", filter)
	}
	if filter.Matches(domainscope.Ref{AutomationID: "automation-1"}) {
		t.Fatalf("filter should reject automation-1: %#v", filter)
	}
}
