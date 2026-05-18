package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func TestProjectRoutesListRenameGetDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	projects := agent.NewInMemoryProjectStore()
	now := time.Now().UTC()
	if err := projects.Upsert(ctx, agent.Project{
		ID:        "proj-hopclaw",
		Name:      "hopclaw",
		Directory: "/repo/hopclaw",
		GitRepo:   "github.com/fulcrus/hopclaw",
		CreatedAt: now,
		LastUsed:  now,
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	svc := runtimesvc.NewService(nil, nil, nil, nil, nil, nil).WithProjectStore(projects)
	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/runtime/projects", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/projects status = %d body=%s", rec.Code, rec.Body.String())
	}
	var listed []agent.Project
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "hopclaw" {
		t.Fatalf("listed = %#v", listed)
	}

	req = httptest.NewRequest(http.MethodGet, "/runtime/projects/hopclaw", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/projects/hopclaw status = %d body=%s", rec.Code, rec.Body.String())
	}
	var project agent.Project
	if err := json.NewDecoder(rec.Body).Decode(&project); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if project.Directory != "/repo/hopclaw" {
		t.Fatalf("project = %#v", project)
	}

	req = httptest.NewRequest(http.MethodPatch, "/runtime/projects/hopclaw", strings.NewReader(`{"name":"hopclaw-next"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /runtime/projects/hopclaw status = %d body=%s", rec.Code, rec.Body.String())
	}
	var renamed agent.Project
	if err := json.NewDecoder(rec.Body).Decode(&renamed); err != nil {
		t.Fatalf("decode rename: %v", err)
	}
	if renamed.Name != "hopclaw-next" {
		t.Fatalf("renamed project = %#v", renamed)
	}
	found, err := projects.FindByName(ctx, "hopclaw-next")
	if err != nil {
		t.Fatalf("FindByName(renamed) error = %v", err)
	}
	if found == nil || found.ID != "proj-hopclaw" {
		t.Fatalf("renamed project not stored correctly: %#v", found)
	}

	req = httptest.NewRequest(http.MethodDelete, "/runtime/projects/hopclaw-next", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /runtime/projects/hopclaw-next status = %d body=%s", rec.Code, rec.Body.String())
	}
	found, err = projects.FindByName(ctx, "hopclaw-next")
	if err != nil {
		t.Fatalf("FindByName() error = %v", err)
	}
	if found != nil {
		t.Fatalf("project still present after delete: %#v", found)
	}
}

func TestProjectRouteGetMissingReturnsNotFound(t *testing.T) {
	t.Parallel()

	svc := runtimesvc.NewService(nil, nil, nil, nil, nil, nil).WithProjectStore(agent.NewInMemoryProjectStore())
	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/runtime/projects/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /runtime/projects/missing status = %d body=%s", rec.Code, rec.Body.String())
	}
}
