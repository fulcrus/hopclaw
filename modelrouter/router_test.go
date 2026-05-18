package modelrouter

import (
	"context"
	"testing"
	"time"
)

func TestSelectRequestedModelWhenCompatible(t *testing.T) {
	t.Parallel()

	router := NewInMemoryRouter([]ModelProfile{
		{
			ID:              "anthropic/sonnet",
			Provider:        "anthropic",
			Priority:        100,
			ContextWindow:   200000,
			MaxOutputTokens: 8000,
			Enabled:         true,
			Supports: map[Capability]bool{
				CapabilityTools: true,
			},
		},
		{
			ID:              "openai/gpt-4.1",
			Provider:        "openai",
			Priority:        90,
			ContextWindow:   128000,
			MaxOutputTokens: 8000,
			Enabled:         true,
			Supports: map[Capability]bool{
				CapabilityTools: true,
			},
		},
	}, time.Minute)

	decision, err := router.Select(context.Background(), RouteRequest{
		RequestedModel:   "anthropic/sonnet",
		Required:         []Capability{CapabilityTools},
		MinContextWindow: 64000,
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Model.ID != "anthropic/sonnet" {
		t.Fatalf("decision.Model.ID = %q", decision.Model.ID)
	}
}

func TestProfileViewRoundTrip(t *testing.T) {
	t.Parallel()

	profile := ModelProfile{
		ID:              "openai/gpt-4o",
		Provider:        "openai",
		Priority:        100,
		ContextWindow:   128000,
		MaxOutputTokens: 8192,
		Enabled:         true,
		Supports: map[Capability]bool{
			CapabilityChat:      true,
			CapabilityTools:     true,
			CapabilityStreaming: true,
		},
		CooldownUntil: time.Now().UTC().Add(time.Minute),
	}

	view := ProfileViewFromProfile(profile)
	roundTrip := view.ModelProfile()

	if roundTrip.ID != profile.ID || roundTrip.Provider != profile.Provider {
		t.Fatalf("roundTrip = %+v, want id/provider from %+v", roundTrip, profile)
	}
	if roundTrip.Priority != profile.Priority || roundTrip.ContextWindow != profile.ContextWindow || roundTrip.MaxOutputTokens != profile.MaxOutputTokens {
		t.Fatalf("roundTrip = %+v, want scalar fields from %+v", roundTrip, profile)
	}
	if !roundTrip.Enabled {
		t.Fatal("expected roundTrip.Enabled = true")
	}
	if !roundTrip.Supports[CapabilityTools] || !roundTrip.Supports[CapabilityStreaming] {
		t.Fatalf("roundTrip.Supports = %#v", roundTrip.Supports)
	}
	if !roundTrip.CooldownUntil.IsZero() {
		t.Fatalf("expected public ProfileView round-trip to omit cooldown state, got %s", roundTrip.CooldownUntil)
	}
}

func TestSelectFallsBackAcrossCompatibleModels(t *testing.T) {
	t.Parallel()

	router := NewInMemoryRouter([]ModelProfile{
		{
			ID:              "anthropic/sonnet",
			Provider:        "anthropic",
			Priority:        100,
			ContextWindow:   200000,
			MaxOutputTokens: 8000,
			Enabled:         true,
			Supports: map[Capability]bool{
				CapabilityTools: true,
			},
			CooldownUntil: time.Now().UTC().Add(time.Minute),
		},
		{
			ID:              "anthropic/haiku",
			Provider:        "anthropic",
			Priority:        95,
			ContextWindow:   200000,
			MaxOutputTokens: 8000,
			Enabled:         true,
			Supports: map[Capability]bool{
				CapabilityTools: true,
			},
		},
		{
			ID:              "openai/gpt-4.1",
			Provider:        "openai",
			Priority:        90,
			ContextWindow:   128000,
			MaxOutputTokens: 8000,
			Enabled:         true,
			Supports: map[Capability]bool{
				CapabilityTools: true,
			},
		},
	}, time.Minute)

	decision, err := router.Select(context.Background(), RouteRequest{
		RequestedModel: "anthropic/sonnet",
		Required:       []Capability{CapabilityTools},
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Model.ID != "anthropic/haiku" {
		t.Fatalf("decision.Model.ID = %q", decision.Model.ID)
	}
	if decision.FailoverFrom != "anthropic/sonnet" {
		t.Fatalf("decision.FailoverFrom = %q", decision.FailoverFrom)
	}
}

func TestSelectSkipsIncompatibleCapabilities(t *testing.T) {
	t.Parallel()

	router := NewInMemoryRouter([]ModelProfile{
		{
			ID:              "model-a",
			Provider:        "provider-a",
			Priority:        100,
			ContextWindow:   128000,
			MaxOutputTokens: 8000,
			Enabled:         true,
			Supports: map[Capability]bool{
				CapabilityTools: false,
			},
		},
		{
			ID:              "model-b",
			Provider:        "provider-b",
			Priority:        90,
			ContextWindow:   128000,
			MaxOutputTokens: 8000,
			Enabled:         true,
			Supports: map[Capability]bool{
				CapabilityTools: true,
			},
		},
	}, time.Minute)

	decision, err := router.Select(context.Background(), RouteRequest{
		Required: []Capability{CapabilityTools},
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Model.ID != "model-b" {
		t.Fatalf("decision.Model.ID = %q", decision.Model.ID)
	}
}

func TestReportFailureSetsCooldown(t *testing.T) {
	t.Parallel()

	router := NewInMemoryRouter([]ModelProfile{
		{
			ID:            "model-a",
			Provider:      "provider-a",
			Priority:      100,
			ContextWindow: 128000,
			Enabled:       true,
		},
	}, time.Minute)
	if err := router.ReportFailure(context.Background(), "model-a", FailureUnavailable); err != nil {
		t.Fatalf("ReportFailure() error = %v", err)
	}
	if _, err := router.Select(context.Background(), RouteRequest{RequestedModel: "model-a"}); err == nil {
		t.Fatal("expected cooled-down model to be skipped")
	}
}

func TestReportFailureWithReasonTransientCooldown(t *testing.T) {
	t.Parallel()

	router := NewInMemoryRouter([]ModelProfile{
		{
			ID:            "model-a",
			Provider:      "provider-a",
			Priority:      100,
			ContextWindow: 128000,
			Enabled:       true,
		},
		{
			ID:            "model-b",
			Provider:      "provider-b",
			Priority:      90,
			ContextWindow: 128000,
			Enabled:       true,
		},
	}, time.Minute)

	// Report a transient failure.
	if err := router.ReportFailureWithReason(context.Background(), "model-a", FailureRateLimit); err != nil {
		t.Fatalf("ReportFailureWithReason() error = %v", err)
	}

	// model-a should be in cooldown, selection should fail over to model-b.
	decision, err := router.Select(context.Background(), RouteRequest{RequestedModel: "model-a"})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Model.ID != "model-b" {
		t.Fatalf("expected failover to model-b, got %q", decision.Model.ID)
	}
	if decision.FailoverFrom != "model-a" {
		t.Fatalf("decision.FailoverFrom = %q", decision.FailoverFrom)
	}

	// Verify failure stats.
	stats := router.GetFailureStats("model-a")
	if stats == nil {
		t.Fatal("expected failure stats for model-a")
	}
	if stats.FailureCounts[FailureRateLimit] != 1 {
		t.Fatalf("FailureCounts[rate_limit] = %d, want 1", stats.FailureCounts[FailureRateLimit])
	}
	if stats.CooldownUntil.IsZero() {
		t.Fatal("expected CooldownUntil to be set")
	}
}

func TestReportFailureWithReasonPermanentDisable(t *testing.T) {
	t.Parallel()

	router := NewInMemoryRouter([]ModelProfile{
		{
			ID:            "model-a",
			Provider:      "provider-a",
			Priority:      100,
			ContextWindow: 128000,
			Enabled:       true,
		},
		{
			ID:            "model-b",
			Provider:      "provider-b",
			Priority:      90,
			ContextWindow: 128000,
			Enabled:       true,
		},
	}, time.Minute)

	// Report a permanent failure.
	if err := router.ReportFailureWithReason(context.Background(), "model-a", FailureBilling); err != nil {
		t.Fatalf("ReportFailureWithReason() error = %v", err)
	}

	// model-a should be disabled.
	decision, err := router.Select(context.Background(), RouteRequest{RequestedModel: "model-a"})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Model.ID != "model-b" {
		t.Fatalf("expected failover to model-b, got %q", decision.Model.ID)
	}

	// Verify failure stats.
	stats := router.GetFailureStats("model-a")
	if stats == nil {
		t.Fatal("expected failure stats for model-a")
	}
	if stats.DisabledReason != FailureBilling {
		t.Fatalf("DisabledReason = %q, want %q", stats.DisabledReason, FailureBilling)
	}
	if stats.DisabledUntil.IsZero() {
		t.Fatal("expected DisabledUntil to be set")
	}
}

func TestSuccessClearsTransientCooldown(t *testing.T) {
	t.Parallel()

	router := NewInMemoryRouter([]ModelProfile{
		{
			ID:            "model-a",
			Provider:      "provider-a",
			Priority:      100,
			ContextWindow: 128000,
			Enabled:       true,
		},
	}, time.Minute)

	ctx := context.Background()
	// Report a transient failure, then a success.
	if err := router.ReportFailureWithReason(ctx, "model-a", FailureRateLimit); err != nil {
		t.Fatalf("ReportFailureWithReason() error = %v", err)
	}
	if err := router.ReportSuccess(ctx, "model-a"); err != nil {
		t.Fatalf("ReportSuccess() error = %v", err)
	}

	// model-a should be available again.
	decision, err := router.Select(ctx, RouteRequest{RequestedModel: "model-a"})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Model.ID != "model-a" {
		t.Fatalf("expected model-a to be available, got %q", decision.Model.ID)
	}

	// Verify transient failure counts are cleared.
	stats := router.GetFailureStats("model-a")
	if stats == nil {
		t.Fatal("expected failure stats for model-a")
	}
	if stats.FailureCounts[FailureRateLimit] != 0 {
		t.Fatalf("FailureCounts[rate_limit] = %d, want 0", stats.FailureCounts[FailureRateLimit])
	}
	if !stats.CooldownUntil.IsZero() {
		t.Fatal("expected CooldownUntil to be cleared")
	}
}

func TestSuccessDoesNotClearPermanentDisable(t *testing.T) {
	t.Parallel()

	router := NewInMemoryRouter([]ModelProfile{
		{
			ID:            "model-a",
			Provider:      "provider-a",
			Priority:      100,
			ContextWindow: 128000,
			Enabled:       true,
		},
		{
			ID:            "model-b",
			Provider:      "provider-b",
			Priority:      90,
			ContextWindow: 128000,
			Enabled:       true,
		},
	}, time.Minute)

	ctx := context.Background()
	// Report a permanent failure, then success.
	if err := router.ReportFailureWithReason(ctx, "model-a", FailureAuth); err != nil {
		t.Fatalf("ReportFailureWithReason() error = %v", err)
	}
	if err := router.ReportSuccess(ctx, "model-a"); err != nil {
		t.Fatalf("ReportSuccess() error = %v", err)
	}

	// model-a should still be disabled (DisabledUntil not cleared by success).
	decision, err := router.Select(ctx, RouteRequest{RequestedModel: "model-a"})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Model.ID != "model-b" {
		t.Fatalf("expected model-a still disabled, got %q", decision.Model.ID)
	}
}

func TestActiveWindowPreservation(t *testing.T) {
	t.Parallel()

	router := NewInMemoryRouter([]ModelProfile{
		{
			ID:            "model-a",
			Provider:      "provider-a",
			Priority:      100,
			ContextWindow: 128000,
			Enabled:       true,
		},
		{
			ID:            "model-b",
			Provider:      "provider-b",
			Priority:      90,
			ContextWindow: 128000,
			Enabled:       true,
		},
	}, time.Minute)

	ctx := context.Background()

	// First transient failure sets cooldown.
	if err := router.ReportFailureWithReason(ctx, "model-a", FailureRateLimit); err != nil {
		t.Fatalf("ReportFailureWithReason(1) error = %v", err)
	}
	stats1 := router.GetFailureStats("model-a")
	if stats1 == nil {
		t.Fatal("expected failure stats for model-a")
	}
	firstCooldown := stats1.CooldownUntil

	// Second transient failure while window is active should NOT extend it.
	if err := router.ReportFailureWithReason(ctx, "model-a", FailureRateLimit); err != nil {
		t.Fatalf("ReportFailureWithReason(2) error = %v", err)
	}
	stats2 := router.GetFailureStats("model-a")
	if !stats2.CooldownUntil.Equal(firstCooldown) {
		t.Fatalf("expected cooldown window to be preserved: first=%v, second=%v", firstCooldown, stats2.CooldownUntil)
	}
	// Error count should still be incremented.
	if stats2.FailureCounts[FailureRateLimit] != 2 {
		t.Fatalf("FailureCounts[rate_limit] = %d, want 2", stats2.FailureCounts[FailureRateLimit])
	}
}

func TestCooldownExpiryMakesModelAvailable(t *testing.T) {
	t.Parallel()

	router := NewInMemoryRouter([]ModelProfile{
		{
			ID:            "model-a",
			Provider:      "provider-a",
			Priority:      100,
			ContextWindow: 128000,
			Enabled:       true,
		},
	}, time.Minute)

	ctx := context.Background()

	// Manually set an already-expired cooldown on the failure stats.
	router.mu.Lock()
	router.failureStats["model-a"].CooldownUntil = time.Now().UTC().Add(-time.Second)
	router.mu.Unlock()

	// Model should be available because cooldown has expired.
	decision, err := router.Select(ctx, RouteRequest{RequestedModel: "model-a"})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Model.ID != "model-a" {
		t.Fatalf("expected model-a to be available after cooldown expiry, got %q", decision.Model.ID)
	}
}

func TestFailureClassToReasonMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		class  FailureClass
		reason FailureReason
	}{
		{FailureRateLimited, FailureRateLimit},
		{FailureQuota, FailureBilling},
		{FailureUnavailable, FailureOverloaded},
		{FailureServer, FailureTimeout},
		{FailureClient, FailureFormat},
	}
	for _, tt := range tests {
		got := FailureClassToReason(tt.class)
		if got != tt.reason {
			t.Fatalf("FailureClassToReason(%q) = %q, want %q", tt.class, got, tt.reason)
		}
	}
}
