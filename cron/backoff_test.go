package cron

import (
	"testing"
	"time"
)

func TestComputeBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		errors int
		min    time.Duration
		max    time.Duration
	}{
		{0, 0, 0},
		{1, 30 * time.Second, 30 * time.Second},
		{2, 60 * time.Second, 60 * time.Second},
		{3, 2 * time.Minute, 2 * time.Minute},
		{4, 4 * time.Minute, 4 * time.Minute},
		{5, 8 * time.Minute, 8 * time.Minute},
		{10, backoffMax, backoffMax},
		{100, backoffMax, backoffMax},
	}

	for _, tt := range tests {
		d := computeBackoff(tt.errors)
		if d < tt.min || d > tt.max {
			t.Errorf("computeBackoff(%d) = %v, want [%v, %v]", tt.errors, d, tt.min, tt.max)
		}
	}
}
