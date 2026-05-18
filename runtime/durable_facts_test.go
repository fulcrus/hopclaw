package runtime

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/durablefact"
)

func TestServiceListMemoryDurableViewsReportsUnsupportedWithoutStore(t *testing.T) {
	t.Parallel()

	svc := NewService(nil, nil, nil, nil, nil, nil)

	contextViews, supported, err := svc.ListMemoryContextViews(context.Background(), durablefact.Filter{})
	if err != nil {
		t.Fatalf("ListMemoryContextViews() error = %v", err)
	}
	if supported {
		t.Fatal("expected unsupported durable context views without memory store")
	}
	if len(contextViews) != 0 {
		t.Fatalf("unexpected context views: %#v", contextViews)
	}

	operatorViews, supported, err := svc.ListMemoryOperatorViews(context.Background(), durablefact.Filter{})
	if err != nil {
		t.Fatalf("ListMemoryOperatorViews() error = %v", err)
	}
	if supported {
		t.Fatal("expected unsupported durable operator views without memory store")
	}
	if len(operatorViews) != 0 {
		t.Fatalf("unexpected operator views: %#v", operatorViews)
	}
}

func TestServiceListMemoryDurableViewsUsesConfiguredStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := agent.NewSQLiteKVStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteKVStore() error = %v", err)
	}
	defer store.Close()

	svc := NewService(nil, nil, nil, nil, nil, nil).WithMemoryStore(agent.NewGovernedMemoryStore(store))
	if _, err := svc.UpsertMemoryRecord(ctx, agent.MemoryRecord{
		Namespace: "profile",
		ScopeKey:  "user",
		Field:     "reply_language",
		Value:     "zh-CN",
	}); err != nil {
		t.Fatalf("UpsertMemoryRecord() error = %v", err)
	}

	contextViews, supported, err := svc.ListMemoryContextViews(ctx, durablefact.Filter{Namespace: "profile"})
	if err != nil {
		t.Fatalf("ListMemoryContextViews() error = %v", err)
	}
	if !supported {
		t.Fatal("expected durable context views support")
	}
	if len(contextViews) != 1 || contextViews[0].Field != "reply_language" {
		t.Fatalf("unexpected context views: %#v", contextViews)
	}

	operatorViews, supported, err := svc.ListMemoryOperatorViews(ctx, durablefact.Filter{Namespace: "profile"})
	if err != nil {
		t.Fatalf("ListMemoryOperatorViews() error = %v", err)
	}
	if !supported {
		t.Fatal("expected durable operator views support")
	}
	if len(operatorViews) != 1 || operatorViews[0].ViewType != durablefact.ViewTypeContext {
		t.Fatalf("unexpected operator views: %#v", operatorViews)
	}
}
