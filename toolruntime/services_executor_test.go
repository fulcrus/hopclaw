package toolruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestServicesExecutorMarksUnconfiguredServiceToolsBlocked(t *testing.T) {
	t.Parallel()

	exec := NewServicesExecutor(BuiltinsConfig{Root: t.TempDir()})
	defs := exec.ToolDefinitions(nil)
	if len(defs) == 0 {
		t.Fatal("expected service tool definitions")
	}
	searchWeb, ok := findServiceTool(defs, "search.web")
	if !ok {
		t.Fatalf("search.web missing from tool definitions: %#v", defs)
	}
	if searchWeb.Source != "services" {
		t.Fatalf("search.web source = %q, want services", searchWeb.Source)
	}
	if searchWeb.Eligible {
		t.Fatalf("search.web.Eligible = %v, want false when search service is not configured", searchWeb.Eligible)
	}
	if searchWeb.Availability.Status != agent.AvailabilityBlocked {
		t.Fatalf("search.web.Availability.Status = %q, want blocked", searchWeb.Availability.Status)
	}
	if _, ok := exec.ResolveTool(nil, "calendar.list_events"); !ok {
		t.Fatal("ResolveTool(calendar.list_events) = false")
	}
}

func TestServicesExecutorMarksConfiguredServiceToolsReady(t *testing.T) {
	t.Parallel()

	exec := NewServicesExecutor(BuiltinsConfig{
		Root: t.TempDir(),
		Services: ServicesConfig{
			Search: SearchServiceConfig{
				Provider: "generic",
				BaseURL:  "https://example.com/search",
			},
		},
	})
	defs := exec.ToolDefinitions(nil)
	searchWeb, ok := findServiceTool(defs, "search.web")
	if !ok {
		t.Fatalf("search.web missing from tool definitions: %#v", defs)
	}
	if !searchWeb.Eligible {
		t.Fatalf("search.web.Eligible = %v, want true", searchWeb.Eligible)
	}
	if searchWeb.Availability.Status != agent.AvailabilityReady {
		t.Fatalf("search.web.Availability.Status = %q, want ready", searchWeb.Availability.Status)
	}
}

func TestServicesExecutorExecutesConfiguredSearch(t *testing.T) {
	t.Parallel()

	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"title": "Result", "url": "https://example.com"},
			},
		})
	}))
	defer searchSrv.Close()

	exec := NewServicesExecutor(BuiltinsConfig{
		Root: t.TempDir(),
		Services: ServicesConfig{
			Search: SearchServiceConfig{
				Provider: "serpapi",
				BaseURL:  searchSrv.URL,
				APIKey:   "search-key",
			},
		},
	})

	results, err := exec.ExecuteBatch(context.Background(), nil, nil, []agent.ToolCall{{
		ID: "search-1", Name: "search.web", Input: map[string]any{"query": "hopclaw"},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["provider"] != "serpapi" {
		t.Fatalf("provider = %#v, want serpapi", payload["provider"])
	}
}

func findServiceTool(defs []agent.ToolDefinition, name string) (agent.ToolDefinition, bool) {
	for _, def := range defs {
		if def.Name == name {
			return def, true
		}
	}
	return agent.ToolDefinition{}, false
}
