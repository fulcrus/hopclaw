package durablefact

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestStore(t *testing.T) (*SQLiteStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "durable_facts.db"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	return store, db
}

func TestSQLiteStoreUpsertGetAndViewProjection(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	createdAt := time.Now().UTC().Add(-time.Minute)
	got, err := store.Upsert(ctx, Fact{
		Key:            "profile.user.reply_language",
		FactClass:      FactClassPreference,
		ViewType:       ViewTypeContext,
		Namespace:      "profile",
		ScopeKey:       "user",
		Name:           "reply_language",
		Label:          "Reply Language",
		Value:          "zh-CN",
		Source:         "runtime.submit",
		Managed:        true,
		Confidence:     0.88,
		ReviewRequired: false,
		Tags:           []string{"language", "preference"},
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if got == nil || got.Key != "profile.user.reply_language" {
		t.Fatalf("unexpected durable fact: %#v", got)
	}

	loaded, err := store.Get(ctx, "profile.user.reply_language")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if loaded == nil || loaded.FactClass != FactClassPreference || loaded.ViewType != ViewTypeContext {
		t.Fatalf("unexpected loaded fact: %#v", loaded)
	}

	contextView, ok := ToContextView(*loaded)
	if !ok {
		t.Fatal("expected context view")
	}
	if contextView.Field != "reply_language" || contextView.Value != "zh-CN" {
		t.Fatalf("unexpected context view: %#v", contextView)
	}
}

func TestSQLiteStoreListAndSearch(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	for _, fact := range []Fact{
		{
			Key:       "profile.user.reply_language",
			FactClass: FactClassPreference,
			ViewType:  ViewTypeContext,
			Namespace: "profile",
			ScopeKey:  "user",
			Name:      "reply_language",
			Value:     "zh-CN",
		},
		{
			Key:       "config.provider.openai",
			FactClass: FactClassSystemConfig,
			ViewType:  ViewTypeConfigProvider,
			Namespace: "provider",
			Name:      "openai",
			Value:     `{"api":"openai-responses"}`,
			ValueType: ValueTypeJSON,
			Source:    "yaml",
		},
		{
			Key:            "general.default.note",
			FactClass:      FactClassImportedNote,
			ViewType:       ViewTypeContext,
			Namespace:      "general",
			ScopeKey:       "default",
			Name:           "note",
			Value:          "Needs manual review",
			ReviewRequired: true,
		},
	} {
		if _, err := store.Upsert(ctx, fact); err != nil {
			t.Fatalf("Upsert(%s) error = %v", fact.Key, err)
		}
	}

	contextFacts, err := store.List(ctx, Filter{ViewType: ViewTypeContext})
	if err != nil {
		t.Fatalf("List(context) error = %v", err)
	}
	if len(contextFacts) != 2 {
		t.Fatalf("len(contextFacts) = %d, want 2", len(contextFacts))
	}

	reviewRequired := true
	reviewFacts, err := store.List(ctx, Filter{ReviewRequired: &reviewRequired})
	if err != nil {
		t.Fatalf("List(review_required) error = %v", err)
	}
	if len(reviewFacts) != 1 || reviewFacts[0].Key != "general.default.note" {
		t.Fatalf("unexpected review facts: %#v", reviewFacts)
	}

	searchResults, err := store.Search(ctx, Filter{Query: "openai"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(searchResults) != 1 || searchResults[0].Key != "config.provider.openai" {
		t.Fatalf("unexpected search results: %#v", searchResults)
	}

	configView, ok := ToConfigView(searchResults[0])
	if !ok {
		t.Fatal("expected config view")
	}
	if configView.Kind != ConfigViewKindProvider || configView.Name != "openai" {
		t.Fatalf("unexpected config view: %#v", configView)
	}
}

func TestSQLiteStoreListTypedViews(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	for _, fact := range []Fact{
		{
			Key:            "profile.user.reply_language",
			FactClass:      FactClassPreference,
			ViewType:       ViewTypeContext,
			Namespace:      "profile",
			ScopeKey:       "user",
			Name:           "reply_language",
			Value:          "zh-CN",
			ReviewRequired: true,
		},
		{
			Key:       "config.provider.openai",
			FactClass: FactClassSystemConfig,
			ViewType:  ViewTypeConfigProvider,
			Namespace: "provider",
			Name:      "openai",
			Value:     `{"api":"openai-responses"}`,
			ValueType: ValueTypeJSON,
		},
	} {
		if _, err := store.Upsert(ctx, fact); err != nil {
			t.Fatalf("Upsert(%s) error = %v", fact.Key, err)
		}
	}

	contextViews, err := store.ListContextViews(ctx, Filter{})
	if err != nil {
		t.Fatalf("ListContextViews() error = %v", err)
	}
	if len(contextViews) != 1 || contextViews[0].Key != "profile.user.reply_language" || !contextViews[0].ReviewRequired {
		t.Fatalf("unexpected context views: %#v", contextViews)
	}

	configViews, err := store.ListConfigViews(ctx, Filter{})
	if err != nil {
		t.Fatalf("ListConfigViews() error = %v", err)
	}
	if len(configViews) != 1 || configViews[0].Kind != ConfigViewKindProvider || configViews[0].Name != "openai" {
		t.Fatalf("unexpected config views: %#v", configViews)
	}

	operatorViews, err := store.ListOperatorViews(ctx, Filter{})
	if err != nil {
		t.Fatalf("ListOperatorViews() error = %v", err)
	}
	if len(operatorViews) != 2 {
		t.Fatalf("len(operatorViews) = %d, want 2", len(operatorViews))
	}
	if operatorViews[0].Key != "config.provider.openai" || operatorViews[1].Key != "profile.user.reply_language" {
		t.Fatalf("unexpected operator view order: %#v", operatorViews)
	}
}

func TestSQLiteStoreGetRejectsInvalidTimestamp(t *testing.T) {
	store, db := openTestStore(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO durable_facts (
			key, fact_class, view_type, namespace, scope_key, name, label, value, value_type,
			source, managed, confidence, review_required, tags, previous_values, evidence, metadata,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"broken", "imported_note", "context", "general", "default", "note", "Note", "value", "text",
		"manual", 1, 1.0, 0, "[]", "[]", "[]", `{}`, "bad-time", time.Now().UTC().Format(sqliteTimeFormat),
	); err != nil {
		t.Fatalf("insert broken fact: %v", err)
	}

	if _, err := store.Get(ctx, "broken"); err == nil {
		t.Fatal("expected Get() to fail for invalid created_at")
	}
}
