package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type searchOnlyManagedMemoryStore struct {
	result []agent.MemoryEntry
}

func (s *searchOnlyManagedMemoryStore) Get(context.Context, string) (*agent.MemoryEntry, error) {
	return nil, nil
}

func (s *searchOnlyManagedMemoryStore) Set(context.Context, string, string) error {
	return nil
}

func (s *searchOnlyManagedMemoryStore) Delete(context.Context, string) error {
	return nil
}

func (s *searchOnlyManagedMemoryStore) Search(context.Context, string) ([]agent.MemoryEntry, error) {
	return append([]agent.MemoryEntry(nil), s.result...), nil
}

func (s *searchOnlyManagedMemoryStore) SemanticSearch(context.Context, string, int) ([]agent.MemoryEntry, error) {
	return nil, nil
}

func (s *searchOnlyManagedMemoryStore) SemanticSearchMMR(context.Context, string, int, float64) ([]agent.MemoryEntry, error) {
	return nil, nil
}

func (s *searchOnlyManagedMemoryStore) List(context.Context) ([]agent.MemoryEntry, error) {
	return nil, nil
}

func (s *searchOnlyManagedMemoryStore) UpsertRecord(context.Context, agent.MemoryRecord) (*agent.MemoryEntry, error) {
	return nil, nil
}

func (s *searchOnlyManagedMemoryStore) ListFiltered(context.Context, agent.MemoryFilter) ([]agent.MemoryEntry, error) {
	return nil, nil
}

func TestHandleSearchMemoryListsAllWhenQueryMissing(t *testing.T) {
	memory := agent.NewInMemoryKVStore()
	if err := memory.Set(context.Background(), "user.name", "Alice"); err != nil {
		t.Fatalf("Set(user.name) error = %v", err)
	}
	if err := memory.Set(context.Background(), "project.name", "HopClaw"); err != nil {
		t.Fatalf("Set(project.name) error = %v", err)
	}
	svc := runtimesvc.NewService(nil, nil, nil, nil, nil, nil).WithMemoryStore(memory)
	server := New(svc, Config{})
	req := httptest.NewRequest(http.MethodGet, "/runtime/memory", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"count":2`) || !strings.Contains(body, `"user.name"`) || !strings.Contains(body, `"project.name"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestHandleSearchMemorySupportsNamespaceFilter(t *testing.T) {
	ctx := context.Background()
	store := agent.NewGovernedMemoryStore(agent.NewInMemoryKVStore())
	if _, err := store.UpsertRecord(ctx, agent.MemoryRecord{
		Namespace: "profile",
		ScopeKey:  "user",
		Field:     "reply_language",
		Value:     "zh-CN",
	}); err != nil {
		t.Fatalf("UpsertRecord(profile) error = %v", err)
	}
	if _, err := store.UpsertRecord(ctx, agent.MemoryRecord{
		Namespace: "project",
		ScopeKey:  "repo",
		Field:     "name",
		Value:     "HopClaw",
	}); err != nil {
		t.Fatalf("UpsertRecord(project) error = %v", err)
	}
	svc := runtimesvc.NewService(nil, nil, nil, nil, nil, nil).WithMemoryStore(store)
	server := New(svc, Config{})
	req := httptest.NewRequest(http.MethodGet, "/runtime/memory?namespace=profile&managed_only=true", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"namespace":"profile"`) || strings.Contains(body, `"namespace":"project"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestHandleSearchMemoryUsesSearchForPureQuery(t *testing.T) {
	store := &searchOnlyManagedMemoryStore{
		result: []agent.MemoryEntry{{
			Key:       "project.default.note",
			Value:     "hello world",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}},
	}
	svc := runtimesvc.NewService(nil, nil, nil, nil, nil, nil).WithMemoryStore(store)
	server := New(svc, Config{})
	req := httptest.NewRequest(http.MethodGet, "/runtime/memory?q=hello", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"count":1`) || !strings.Contains(body, `"hello world"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestHandleGetMemoryStatus(t *testing.T) {
	ctx := context.Background()
	store := agent.NewInMemoryKVStore()
	if err := store.Set(ctx, "project.name", "HopClaw"); err != nil {
		t.Fatalf("Set(project.name) error = %v", err)
	}
	svc := runtimesvc.NewService(nil, nil, nil, nil, nil, nil).WithMemoryStore(store)
	server := New(svc, Config{})
	req := httptest.NewRequest(http.MethodGet, "/runtime/memory/status", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"store_type":"in-memory"`) || !strings.Contains(body, `"entry_count":1`) || !strings.Contains(body, `"index_ready":true`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestHandleReindexMemory(t *testing.T) {
	ctx := context.Background()
	store := agent.NewInMemoryKVStore()
	if err := store.Set(ctx, "project.name", "HopClaw"); err != nil {
		t.Fatalf("Set(project.name) error = %v", err)
	}
	svc := runtimesvc.NewService(nil, nil, nil, nil, nil, nil).WithMemoryStore(store)
	server := New(svc, Config{})
	req := httptest.NewRequest(http.MethodPost, "/runtime/memory/index", strings.NewReader(`{"force":true}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"status":"rebuilt_forced"`) || !strings.Contains(body, `"indexed":1`) {
		t.Fatalf("unexpected body: %s", body)
	}
}
