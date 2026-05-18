package agent

import (
	"context"
	"testing"
)

func mustReloadRun(t *testing.T, runs RunStore, run *Run) *Run {
	t.Helper()
	if run == nil {
		t.Fatal("run is required")
	}
	latest, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	return latest
}
