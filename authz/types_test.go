package authz

import "testing"

func TestAllResourcesStable(t *testing.T) {
	t.Parallel()

	got := ResourceNames(AllResources())
	want := []string{
		"*",
		"approvals",
		"audit",
		"channels",
		"config",
		"cron",
		"discovery",
		"governance",
		"hooks",
		"knowledge",
		"operator",
		"plugins",
		"runs",
		"sandbox",
		"sessions",
		"skills",
		"tools",
		"usage",
		"wakeup",
		"watch",
		"wire",
	}

	if len(got) != len(want) {
		t.Fatalf("len(resources) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resources[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAllActionsStable(t *testing.T) {
	t.Parallel()

	got := ActionNames(AllActions())
	want := []string{"admin", "approve", "execute", "read", "write"}

	if len(got) != len(want) {
		t.Fatalf("len(actions) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("actions[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseResourceCanonicalizesStableCatalog(t *testing.T) {
	t.Parallel()

	resource, ok := ParseResource("  RUNS ")
	if !ok {
		t.Fatal("ParseResource() = false, want true")
	}
	if resource != ResourceRuns {
		t.Fatalf("resource = %q, want %q", resource, ResourceRuns)
	}

	if _, ok := ParseResource("runtime_runs"); ok {
		t.Fatal("ParseResource() = true, want false for unknown resource")
	}
}

func TestParseActionCanonicalizesStableCatalog(t *testing.T) {
	t.Parallel()

	action, ok := ParseAction(" Execute ")
	if !ok {
		t.Fatal("ParseAction() = false, want true")
	}
	if action != ActionExecute {
		t.Fatalf("action = %q, want %q", action, ActionExecute)
	}

	if _, ok := ParseAction("launch"); ok {
		t.Fatal("ParseAction() = true, want false for unknown action")
	}
}

func TestDescribeFallsBackForCustomDecider(t *testing.T) {
	t.Parallel()

	summary := Describe(nil)
	if summary.Kind != "custom" {
		t.Fatalf("summary.Kind = %q, want custom", summary.Kind)
	}
	if len(summary.Resources) == 0 || len(summary.Actions) == 0 {
		t.Fatalf("summary missing catalogs: %+v", summary)
	}
}
