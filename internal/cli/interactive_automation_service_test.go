package cli

import (
	"testing"
	"time"

	autopkg "github.com/fulcrus/hopclaw/automation"
	"github.com/fulcrus/hopclaw/watch"
)

func TestAutomationRunNowPath(t *testing.T) {
	if got := automationRunNowPath(string(autopkg.KindCron), "job 1"); got != cronJobsPath+"/job%201/run" {
		t.Fatalf("automationRunNowPath(cron) = %q", got)
	}
	if got := automationRunNowPath(string(autopkg.KindWatch), "watch 1"); got != "/operator/watch/items/watch%201/run" {
		t.Fatalf("automationRunNowPath(watch) = %q", got)
	}
	if got := automationRunNowPath(string(autopkg.KindWakeup), "wake-1"); got != "" {
		t.Fatalf("automationRunNowPath(wakeup) = %q, want empty", got)
	}
}

func TestPromotedCronJobIDNormalizesName(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	if got := promotedCronJobID("  Nightly Review!  ", now); got != "nightly-review-1700000000" {
		t.Fatalf("promotedCronJobID() = %q", got)
	}
	if got := promotedCronJobID("   ", now); got != "promoted-1700000000" {
		t.Fatalf("promotedCronJobID(blank) = %q", got)
	}
}

func TestMapAutomationItemsNeedsInputSetupContract(t *testing.T) {
	items := mapAutomationItems([]autopkg.Item{{
		ID:       "cron-1",
		Kind:     autopkg.KindCron,
		Name:     "Nightly",
		Enabled:  true,
		Schedule: "0 0 * * *",
		Delivery: &autopkg.DeliveryTarget{
			Channel: "slack",
			Label:   "Slack",
		},
	}})
	if len(items) != 1 {
		t.Fatalf("len(mapAutomationItems()) = %d, want 1", len(items))
	}
	if items[0].Status != "needs_input" {
		t.Fatalf("status = %q, want needs_input", items[0].Status)
	}
	if items[0].SetupContract == nil {
		t.Fatal("setup contract = nil")
	}
	if items[0].SetupContract.Summary != "Delivery target is missing for Slack." {
		t.Fatalf("setup summary = %q", items[0].SetupContract.Summary)
	}
	if len(items[0].SetupContract.Slots) != 1 || items[0].SetupContract.Slots[0].Example != "#ops-alerts" {
		t.Fatalf("setup slots = %#v", items[0].SetupContract.Slots)
	}
}

func TestWatchAutomationProjectionUsesFirstNonZeroTime(t *testing.T) {
	checkedAt := time.Date(2026, time.April, 9, 10, 0, 0, 0, time.UTC)
	item := watchAutomationProjection(watch.Watch{
		ID:            "watch-1",
		Name:          "Docs",
		Enabled:       true,
		Interval:      "5m",
		LastCheckedAt: checkedAt,
	})
	if item.LastExecution == nil {
		t.Fatal("LastExecution = nil")
	}
	if !item.LastExecution.OccurredAt.Equal(checkedAt) {
		t.Fatalf("OccurredAt = %s, want %s", item.LastExecution.OccurredAt, checkedAt)
	}
}
