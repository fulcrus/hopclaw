package bootstrap

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/watch"
)

func TestBootstrapWatchWorkflowSeedsNextIDFromStore(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "watch.json")
	store, err := watch.Load(storePath)
	if err != nil {
		t.Fatalf("watch.Load() error = %v", err)
	}
	now := time.Now().UTC()
	for _, item := range []watch.Watch{
		{
			ID:        "watch-000001",
			Name:      "first",
			Enabled:   true,
			Interval:  "1h",
			Source:    watch.Source{Kind: watch.SourceKindHTTP, HTTP: &watch.HTTPSource{URL: "https://example.com/1"}},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        "watch-000007",
			Name:      "latest",
			Enabled:   true,
			Interval:  "1h",
			Source:    watch.Source{Kind: watch.SourceKindHTTP, HTTP: &watch.HTTPSource{URL: "https://example.com/7"}},
			CreatedAt: now,
			UpdatedAt: now,
		},
	} {
		if err := store.Add(item); err != nil {
			t.Fatalf("store.Add(%q) error = %v", item.ID, err)
		}
	}
	if err := store.Save(); err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}

	service := watch.NewService(store, nil)
	workflow := newBootstrapWatchWorkflow(service)
	if workflow == nil {
		t.Fatal("expected workflow")
	}

	result, err := workflow.Create(context.Background(), agent.WatchWorkflowRequest{
		SessionKey: "watch:seed",
		Name:       "new watch",
		SourceURL:  "https://example.com/new",
		Interval:   "1h",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.WatchID != "watch-000008" {
		t.Fatalf("WatchID = %q, want %q", result.WatchID, "watch-000008")
	}
	got, err := store.Get("watch-000008")
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if got.Name != "new watch" {
		t.Fatalf("stored watch name = %q, want %q", got.Name, "new watch")
	}
}

func TestBootstrapWatchWorkflowCancelRemovesLatestWatchForSession(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "watch.json")
	store, err := watch.Load(storePath)
	if err != nil {
		t.Fatalf("watch.Load() error = %v", err)
	}
	now := time.Now().UTC()
	for _, item := range []watch.Watch{
		{
			ID:         "watch-000001",
			Name:       "older",
			Enabled:    true,
			Interval:   "1h",
			SessionKey: "watch:test",
			Source:     watch.Source{Kind: watch.SourceKindHTTP, HTTP: &watch.HTTPSource{URL: "https://example.com/old"}},
			CreatedAt:  now.Add(-2 * time.Hour),
			UpdatedAt:  now.Add(-2 * time.Hour),
		},
		{
			ID:         "watch-000002",
			Name:       "latest",
			Enabled:    true,
			Interval:   "1h",
			SessionKey: "watch:test",
			Source:     watch.Source{Kind: watch.SourceKindHTTP, HTTP: &watch.HTTPSource{URL: "https://example.com/latest"}},
			CreatedAt:  now.Add(-time.Hour),
			UpdatedAt:  now.Add(-time.Minute),
		},
	} {
		if err := store.Add(item); err != nil {
			t.Fatalf("store.Add(%q) error = %v", item.ID, err)
		}
	}
	if err := store.Save(); err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}

	service := watch.NewService(store, nil)
	workflow := newBootstrapWatchWorkflow(service)
	result, err := workflow.Cancel(context.Background(), agent.WatchWorkflowCancelRequest{
		SessionKey: "watch:test",
		Query:      "取消刚才那个监控提醒",
	})
	if err != nil {
		t.Fatalf("workflow.Cancel() error = %v", err)
	}
	if len(result.RemovedWatchIDs) != 1 || result.RemovedWatchIDs[0] != "watch-000002" {
		t.Fatalf("RemovedWatchIDs = %#v", result.RemovedWatchIDs)
	}
	if _, err := store.Get("watch-000002"); err == nil {
		t.Fatal("expected latest watch to be removed")
	}
	if _, err := store.Get("watch-000001"); err != nil {
		t.Fatalf("expected older watch to remain, got %v", err)
	}
}

func TestBootstrapWatchWorkflowCancelRemovesAllMatchingDomainWatches(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "watch.json")
	store, err := watch.Load(storePath)
	if err != nil {
		t.Fatalf("watch.Load() error = %v", err)
	}
	now := time.Now().UTC()
	for _, item := range []watch.Watch{
		{
			ID:        "watch-000101",
			Name:      "example one",
			Enabled:   true,
			Interval:  "1h",
			Source:    watch.Source{Kind: watch.SourceKindHTTP, HTTP: &watch.HTTPSource{URL: "https://example.com/a"}},
			CreatedAt: now.Add(-3 * time.Hour),
			UpdatedAt: now.Add(-3 * time.Hour),
		},
		{
			ID:        "watch-000102",
			Name:      "example two",
			Enabled:   true,
			Interval:  "1h",
			Source:    watch.Source{Kind: watch.SourceKindHTTP, HTTP: &watch.HTTPSource{URL: "https://example.com/b"}},
			CreatedAt: now.Add(-2 * time.Hour),
			UpdatedAt: now.Add(-2 * time.Hour),
		},
		{
			ID:        "watch-000103",
			Name:      "other domain",
			Enabled:   true,
			Interval:  "1h",
			Source:    watch.Source{Kind: watch.SourceKindHTTP, HTTP: &watch.HTTPSource{URL: "https://other.test/keep"}},
			CreatedAt: now.Add(-time.Hour),
			UpdatedAt: now.Add(-time.Hour),
		},
	} {
		if err := store.Add(item); err != nil {
			t.Fatalf("store.Add(%q) error = %v", item.ID, err)
		}
	}
	if err := store.Save(); err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}

	service := watch.NewService(store, nil)
	workflow := newBootstrapWatchWorkflow(service)
	result, err := workflow.Cancel(context.Background(), agent.WatchWorkflowCancelRequest{
		Query:     "停掉所有和 example.com 相关的监控",
		TargetRef: "example.com",
		RemoveAll: true,
	})
	if err != nil {
		t.Fatalf("workflow.Cancel() error = %v", err)
	}
	if got := len(result.RemovedWatchIDs); got != 2 {
		t.Fatalf("len(RemovedWatchIDs) = %d, want 2", got)
	}
	if _, err := store.Get("watch-000103"); err != nil {
		t.Fatalf("expected non-matching watch to remain, got %v", err)
	}
}

func TestBootstrapWatchWorkflowCancelPrefersCurrentSessionMatches(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "watch.json")
	store, err := watch.Load(storePath)
	if err != nil {
		t.Fatalf("watch.Load() error = %v", err)
	}
	now := time.Now().UTC()
	for _, item := range []watch.Watch{
		{
			ID:         "watch-000201",
			Name:       "current session example",
			Enabled:    true,
			Interval:   "1h",
			SessionKey: "watch:current",
			Source:     watch.Source{Kind: watch.SourceKindHTTP, HTTP: &watch.HTTPSource{URL: "https://example.com/current"}},
			CreatedAt:  now.Add(-2 * time.Hour),
			UpdatedAt:  now.Add(-90 * time.Minute),
		},
		{
			ID:         "watch-000202",
			Name:       "other session example",
			Enabled:    true,
			Interval:   "1h",
			SessionKey: "watch:other",
			Source:     watch.Source{Kind: watch.SourceKindHTTP, HTTP: &watch.HTTPSource{URL: "https://example.com/other"}},
			CreatedAt:  now.Add(-time.Hour),
			UpdatedAt:  now.Add(-time.Minute),
		},
	} {
		if err := store.Add(item); err != nil {
			t.Fatalf("store.Add(%q) error = %v", item.ID, err)
		}
	}
	if err := store.Save(); err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}

	service := watch.NewService(store, nil)
	workflow := newBootstrapWatchWorkflow(service)
	result, err := workflow.Cancel(context.Background(), agent.WatchWorkflowCancelRequest{
		SessionKey: "watch:current",
		Query:      "停掉 example.com 的监控",
		TargetRef:  "example.com",
	})
	if err != nil {
		t.Fatalf("workflow.Cancel() error = %v", err)
	}
	if len(result.RemovedWatchIDs) != 1 || result.RemovedWatchIDs[0] != "watch-000201" {
		t.Fatalf("RemovedWatchIDs = %#v", result.RemovedWatchIDs)
	}
	if _, err := store.Get("watch-000201"); err == nil {
		t.Fatal("expected current-session watch to be removed")
	}
	if _, err := store.Get("watch-000202"); err != nil {
		t.Fatalf("expected other-session watch to remain, got %v", err)
	}
}
