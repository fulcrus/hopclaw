package repl

import (
	"context"
	"strings"
	"testing"
)

func TestDoctorCommandRendersStructuredReadinessAndRecovery(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		readiness: &ReadinessSnapshot{
			OverallStatus: "blocked",
			Categories: []ReadinessCategory{
				{ID: "model_provider", Label: "AI Setup", Status: "blocked", Summary: "no configured provider"},
				{ID: "remote_target", Label: "Remote prod-eu", Status: "degraded", Summary: "heartbeat stale"},
				{ID: "memory_index", Label: "Memory", Status: "ready", Summary: "ready"},
			},
			RecoveryCandidates: []RecoveryCandidate{
				{Type: "paused_run", ID: "run-128", Action: "continue"},
				{Type: "draft", ID: "ops-incident", Action: "restore"},
			},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer:   NewRenderer(&output, false),
		service:    service,
		sessionID:  "sess-1",
		commands:   registry,
		targetName: "prod-eu",
	}

	if _, err := registry.Execute(context.Background(), repl, "/doctor"); err != nil {
		t.Fatalf("Execute(/doctor) error = %v", err)
	}

	text := output.String()
	for _, want := range []string{
		"[panel] System Readiness",
		"Summary        [BLOCKED] 1 blocker · 1 warning · 2 recoverable",
		"Categories",
		"AI Setup        blocked · no configured provider",
		"Remote prod-eu  degraded · heartbeat stale",
		"Recovery Center  2 recoverable item(s)",
		"1. paused run-128 · continue",
		"2. draft ops-incident · restore",
		"Actions: /doctor  /runs  /continue  Esc back",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor panel missing %q: %q", want, text)
		}
	}
}

func TestDeriveReadinessCategoriesProjectNamedLocalRuntimeAsLocal(t *testing.T) {
	categories := deriveReadinessCategories([]DoctorCheck{{
		Category: "gateway",
		Name:     "heartbeat",
		Status:   "warn",
		Detail:   "stale",
	}}, "local-dev", "local")

	for _, item := range categories {
		if item.ID != "remote_target" {
			continue
		}
		if item.Label != "Local runtime local-dev" {
			t.Fatalf("remote_target label = %q, want %q", item.Label, "Local runtime local-dev")
		}
		return
	}
	t.Fatal("remote_target category not found")
}

func TestSnapshotContextIncludesMemoryStrip(t *testing.T) {
	repl := &REPL{
		targetName: "prod-eu",
		sessionKey: "ops",
		viewState: REPLViewState{
			MemoryStrip: "using: pinned 1 · project 2 · recalled 1",
		},
	}

	got := repl.snapshotContext("")
	if !strings.Contains(got, "using: pinned 1 · project 2 · recalled 1") {
		t.Fatalf("snapshotContext() = %q", got)
	}
}
