package repl

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestAutomationCommandRendersPanel(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		automations: []AutomationItem{{
			ID:       "cron-1",
			Name:     "weekday-briefing",
			Kind:     "cron",
			Status:   "ready",
			Schedule: "weekday",
			Delivery: "ops",
			NextRun:  "tomorrow",
			Health:   "healthy",
		}},
		readiness: &ReadinessSnapshot{
			Categories: []ReadinessCategory{{
				ID:      "automation_runtime",
				Label:   "Automation Runtime",
				Status:  "ready",
				Summary: "1 automation item(s)",
			}},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  service,
		commands: registry,
	}

	if _, err := registry.Execute(context.Background(), repl, "/automation"); err != nil {
		t.Fatalf("Execute(/automation) error = %v", err)
	}

	text := output.String()
	for _, want := range []string{"[panel] Automation", "weekday-briefing", "ops", "healthy"} {
		if !strings.Contains(text, want) {
			t.Fatalf("automation panel missing %q: %q", want, text)
		}
	}
}

func TestAutomationCommandRequiresGatewayWhenUnavailable(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		readiness: &ReadinessSnapshot{
			Categories: []ReadinessCategory{{
				ID:      "automation_runtime",
				Label:   "Automation Runtime",
				Status:  "unknown",
				Summary: "automation status unavailable",
			}},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  service,
		commands: registry,
	}

	if _, err := registry.Execute(context.Background(), repl, "/automation"); err != nil {
		t.Fatalf("Execute(/automation) error = %v", err)
	}
	if got := output.String(); !strings.Contains(got, "Automation requires a gateway connection.") {
		t.Fatalf("automation fallback output = %q", got)
	}
}

func TestAutomationCommandAvailabilityDoesNotDependOnEnglishSummary(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		readiness: &ReadinessSnapshot{
			Categories: []ReadinessCategory{{
				ID:      "automation_runtime",
				Label:   "Automation Runtime",
				Status:  "ready",
				Summary: "automation status unavailable",
			}},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  service,
		commands: registry,
	}

	if _, err := registry.Execute(context.Background(), repl, "/automation"); err != nil {
		t.Fatalf("Execute(/automation) error = %v", err)
	}
	if got := output.String(); !strings.Contains(got, "No automations found.") {
		t.Fatalf("automation output = %q, want empty-state panel instead of gateway fallback", got)
	}
}

func TestPromoteCommandRendersCandidatePanel(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		detail: &SessionDetail{
			Summary: SessionSummary{ID: "sess-1", Key: "ops", Model: "gpt-5.4"},
			Messages: []SessionMessage{
				{Role: "user", Content: "Prepare weekday ops briefing"},
				{Role: "assistant", Content: "done"},
			},
		},
		runs: []RunSummary{{
			ID:         "run-188",
			SessionID:  "sess-1",
			SessionKey: "ops",
			Status:     "completed",
			Phase:      "completed",
		}},
		runDetails: map[string]*RunDetail{
			"run-188": {
				Run: RunSummary{
					ID:         "run-188",
					SessionID:  "sess-1",
					SessionKey: "ops",
					Model:      "gpt-5.4",
					Status:     "completed",
					Phase:      "completed",
				},
				Output: "weekday summary delivered",
			},
		},
		runDelivery: map[string]*RunDeliveryDetail{
			"run-188": {
				Targets: []DeliveryTarget{{Kind: "telegram", Label: "ops-room", Status: "delivered"}},
			},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer:   NewRenderer(&output, false),
		service:    service,
		commands:   registry,
		sessionID:  "sess-1",
		sessionKey: "ops",
	}

	if _, err := registry.Execute(context.Background(), repl, "/promote"); err != nil {
		t.Fatalf("Execute(/promote) error = %v", err)
	}

	text := output.String()
	for _, want := range []string{"[panel] Promote To Automation", "Source run: run-188", "Suggested kind: cron", "Schedule:", "Delivery:", "Prompt: Prepare weekday ops briefing"} {
		if !strings.Contains(text, want) {
			t.Fatalf("promote panel missing %q: %q", want, text)
		}
	}
}

func TestPromoteCommandAcceptsScheduleOverrideAndCreatesAutomation(t *testing.T) {
	registry := NewCommandRegistry()
	prompter := &panelAwarePrompter{}
	service := &fakeService{
		detail: &SessionDetail{
			Summary: SessionSummary{ID: "sess-1", Key: "ops", Model: "gpt-5.4"},
			Messages: []SessionMessage{
				{Role: "user", Content: "Prepare weekday ops briefing"},
				{Role: "assistant", Content: "done"},
			},
		},
		runs: []RunSummary{{
			ID:         "run-188",
			SessionID:  "sess-1",
			SessionKey: "ops",
			Status:     "completed",
			Phase:      "completed",
		}},
		runDetails: map[string]*RunDetail{
			"run-188": {
				Run: RunSummary{
					ID:         "run-188",
					SessionID:  "sess-1",
					SessionKey: "ops",
					Model:      "gpt-5.4",
					Status:     "completed",
					Phase:      "completed",
				},
				Output: "weekday summary delivered",
			},
		},
		runDelivery: map[string]*RunDeliveryDetail{
			"run-188": {
				Targets: []DeliveryTarget{{Kind: "telegram", Label: "ops-room", Status: "delivered"}},
			},
		},
	}
	repl := &REPL{
		renderer:   NewRenderer(io.Discard, true),
		service:    service,
		commands:   registry,
		prompter:   prompter,
		sessionID:  "sess-1",
		sessionKey: "ops",
	}

	if _, err := registry.Execute(context.Background(), repl, "/promote */15 * * * *"); err != nil {
		t.Fatalf("Execute(/promote schedule) error = %v", err)
	}
	panel, ok := repl.panelController.(*selectionPanel)
	if !ok || panel == nil {
		t.Fatalf("panelController = %#v, want *selectionPanel", repl.panelController)
	}
	if len(panel.baseItems) != 1 {
		t.Fatalf("len(panel.baseItems) = %d, want 1", len(panel.baseItems))
	}
	if !strings.Contains(panel.baseItems[0].Text, "Schedule: */15 * * * *") {
		t.Fatalf("panel text = %q, want schedule override", panel.baseItems[0].Text)
	}
	if _, err := panel.onConfirm(panel.baseItems[0]); err != nil {
		t.Fatalf("panel.onConfirm() error = %v", err)
	}
	if len(service.createdAutomations) != 1 {
		t.Fatalf("created automations = %d, want 1", len(service.createdAutomations))
	}
	if got := service.createdAutomations[0].Expression; got != "*/15 * * * *" {
		t.Fatalf("created automation expression = %q, want %q", got, "*/15 * * * *")
	}
}

func TestDeliveryCommandStatusOpensDeliveryPanel(t *testing.T) {
	registry := NewCommandRegistry()
	prompter := &panelAwarePrompter{}
	service := &fakeService{
		governanceItems: []DeliveryListItem{{
			ID:          "gdel-1",
			RunID:       "run-1",
			AdapterName: "audit-hub",
			Status:      "pending",
			Attempts:    1,
			MaxAttempts: 3,
			Summary:     "waiting for retry",
		}},
	}
	repl := &REPL{
		renderer: NewRenderer(io.Discard, true),
		service:  service,
		commands: registry,
		prompter: prompter,
	}

	if _, err := registry.Execute(context.Background(), repl, "/delivery status"); err != nil {
		t.Fatalf("Execute(/delivery status) error = %v", err)
	}
	panel, ok := repl.panelController.(*selectionPanel)
	if !ok || panel == nil {
		t.Fatalf("panelController = %#v, want *selectionPanel", repl.panelController)
	}
	if panel.title != "Deliveries" {
		t.Fatalf("panel.title = %q, want %q", panel.title, "Deliveries")
	}
}

func TestRenderDeliveryDetailHidesRedriveActionWhenNotRedrivable(t *testing.T) {
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
	}

	repl.renderDeliveryDetail(context.Background(), "gdel-2", []DeliveryListItem{{
		ID:          "gdel-2",
		RunID:       "run-2",
		AdapterName: "audit-hub",
		Status:      "delivered",
		Attempts:    1,
		MaxAttempts: 3,
		Summary:     "delivery complete",
		CanRedrive:  false,
	}})

	got := output.String()
	if !strings.Contains(got, "[panel] Delivery gdel-2") {
		t.Fatalf("output missing delivery detail panel: %q", got)
	}
	if strings.Contains(got, "/delivery redrive gdel-2") {
		t.Fatalf("output unexpectedly contains redrive action: %q", got)
	}
}

func TestRenderAutomationDetailUsesDetailedItemAndExactRunMatches(t *testing.T) {
	service := &fakeService{
		runs: []RunSummary{
			{
				ID:        "run-match",
				Status:    "completed",
				CreatedAt: "2026-04-09T10:00:00Z",
				Automation: &AutomationProjection{
					ID:   "cron-1",
					Kind: "cron",
				},
			},
			{
				ID:        "run-other",
				Status:    "completed",
				CreatedAt: "2026-04-09T09:00:00Z",
				Automation: &AutomationProjection{
					ID:   "cron-2",
					Kind: "cron",
				},
			},
		},
		automationDetail: map[string]*AutomationItem{
			"cron:cron-1": {
				ID:       "cron-1",
				Name:     "Daily report",
				Kind:     "cron",
				Status:   "needs_input",
				Schedule: "0 9 * * *",
				Delivery: "slack",
				Health:   "missing destination",
				SetupContract: &AutomationSetupInfo{
					Status:  "needs_input",
					Summary: "Delivery target is missing for slack.",
					Slots: []AutomationSetupSlot{{
						Field:    "delivery_target",
						Question: "Provide the delivery target for slack.",
						Required: true,
					}},
				},
			},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  service,
	}

	if err := repl.renderAutomationDetail(context.Background(), AutomationItem{ID: "cron-1", Name: "Daily report", Kind: "cron"}); err != nil {
		t.Fatalf("renderAutomationDetail() error = %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "Setup required: Delivery target is missing for slack.") {
		t.Fatalf("output missing setup contract: %q", got)
	}
	if !strings.Contains(got, "run-match") {
		t.Fatalf("output missing matching automation run: %q", got)
	}
	if strings.Contains(got, "run-other") {
		t.Fatalf("output included unrelated same-kind automation run: %q", got)
	}
}

func TestRenderMemoryConflictsShowsConflictEntries(t *testing.T) {
	service := &fakeService{
		memoryConflicts: []agent.MemoryEntry{{
			Key:            "service_url",
			Value:          "https://old.example.com",
			Source:         "project",
			ConflictWith:   "https://new.example.com",
			ConflictSource: "agent",
		}},
	}
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  service,
	}

	if err := repl.renderMemoryConflicts(context.Background()); err != nil {
		t.Fatalf("renderMemoryConflicts() error = %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "[panel] Memory Conflicts") || !strings.Contains(got, "service_url") || !strings.Contains(got, "https://new.example.com") {
		t.Fatalf("output missing memory conflict detail: %q", got)
	}
}

func TestRenderMemoryPendingShowsPendingWrites(t *testing.T) {
	service := &fakeService{
		pendingMemory: []agent.MemoryEntry{{
			Key:                "deploy_env",
			PendingWrite:       true,
			PendingWriteSource: "agent",
			PendingWriteValue:  "production",
		}},
	}
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  service,
	}

	if err := repl.renderMemoryPending(context.Background()); err != nil {
		t.Fatalf("renderMemoryPending() error = %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "[panel] Pending Memory Writes") || !strings.Contains(got, "deploy_env") || !strings.Contains(got, "production") {
		t.Fatalf("output missing pending memory detail: %q", got)
	}
}

func TestDeriveDeliveryStatusFromTargetsTreatsPendingAsDelivering(t *testing.T) {
	got := deriveDeliveryStatusFromTargets(&RunDeliveryDetail{
		Targets: []DeliveryTarget{{Status: "pending"}},
	})
	if got != "delivering" {
		t.Fatalf("deriveDeliveryStatusFromTargets(pending) = %q, want %q", got, "delivering")
	}
}
