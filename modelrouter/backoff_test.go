package modelrouter

import (
	"testing"
	"time"
)

func TestIsTransient(t *testing.T) {
	t.Parallel()

	transient := []FailureReason{FailureRateLimit, FailureOverloaded, FailureTimeout, FailureUnknown}
	for _, reason := range transient {
		if !IsTransient(reason) {
			t.Fatalf("IsTransient(%q) = false, want true", reason)
		}
	}

	permanent := []FailureReason{FailureAuth, FailureAuthPermanent, FailureBilling, FailureFormat, FailureModelNotFound}
	for _, reason := range permanent {
		if IsTransient(reason) {
			t.Fatalf("IsTransient(%q) = true, want false", reason)
		}
	}
}

func TestTransientCooldown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		errorCount int
		want       time.Duration
	}{
		{0, 20 * time.Second},  // clamped to 1: 20s * 3^0 = 20s
		{1, 20 * time.Second},  // 20s * 3^0 = 20s
		{2, 60 * time.Second},  // 20s * 3^1 = 60s
		{3, 3 * time.Minute},   // 20s * 3^2 = 180s
		{4, 9 * time.Minute},   // 20s * 3^3 = 540s
		{5, 27 * time.Minute},  // 20s * 3^4 = 1620s
		{6, 30 * time.Minute},  // 20s * 3^5 = 4860s > 1800s, capped at 30min
		{10, 30 * time.Minute}, // way beyond cap
	}
	for _, tt := range tests {
		got := TransientCooldown(tt.errorCount)
		if got != tt.want {
			t.Fatalf("TransientCooldown(%d) = %v, want %v", tt.errorCount, got, tt.want)
		}
	}
}

func TestPermanentDisableDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		errorCount int
		want       time.Duration
	}{
		{0, 1 * time.Hour},   // clamped to 1: 1h * 2^0 = 1h
		{1, 1 * time.Hour},   // 1h * 2^0 = 1h
		{2, 2 * time.Hour},   // 1h * 2^1 = 2h
		{3, 4 * time.Hour},   // 1h * 2^2 = 4h
		{4, 8 * time.Hour},   // 1h * 2^3 = 8h
		{5, 12 * time.Hour},  // 1h * 2^4 = 16h > 12h, capped at 12h
		{10, 12 * time.Hour}, // way beyond cap
	}
	for _, tt := range tests {
		got := PermanentDisableDuration(tt.errorCount)
		if got != tt.want {
			t.Fatalf("PermanentDisableDuration(%d) = %v, want %v", tt.errorCount, got, tt.want)
		}
	}
}
