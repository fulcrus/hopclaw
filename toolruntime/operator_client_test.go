package toolruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestOperatorClientExposesGatewayTools(t *testing.T) {
	t.Parallel()

	client := NewOperatorClient()
	defs := client.ToolDefinitions(nil)
	if len(defs) != 4 {
		t.Fatalf("len(ToolDefinitions) = %d, want 4", len(defs))
	}
	statusDef, ok := findOperatorTool(defs, "gateway.status")
	if !ok {
		t.Fatalf("gateway.status missing from tool definitions: %#v", defs)
	}
	if statusDef.Source != "operator" {
		t.Fatalf("gateway.status source = %q, want operator", statusDef.Source)
	}
	if _, ok := client.ResolveTool(nil, "gateway.reload"); !ok {
		t.Fatal("ResolveTool(gateway.reload) = false")
	}
}

func TestOperatorClientExecutesGatewayRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version":          "dev",
				"uptime":           "12s",
				"status":           "ok",
				"capability_count": 3,
			})
		case "/operator/reload":
			if r.Method != http.MethodPost {
				t.Fatalf("reload method = %s, want POST", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"message": "reloaded",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv(gatewayAddrEnvVar, server.URL)
	client := NewOperatorClient()

	results, err := client.ExecuteBatch(context.Background(), nil, nil, []agent.ToolCall{
		{ID: "status-1", Name: "gateway.status"},
		{ID: "reload-1", Name: "gateway.reload"},
	})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	var statusPayload map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &statusPayload); err != nil {
		t.Fatalf("status json.Unmarshal() error = %v", err)
	}
	if statusPayload["status"] != "ok" {
		t.Fatalf("status payload = %#v", statusPayload)
	}

	var reloadPayload map[string]any
	if err := json.Unmarshal([]byte(results[1].Content), &reloadPayload); err != nil {
		t.Fatalf("reload json.Unmarshal() error = %v", err)
	}
	if reloadPayload["message"] != "reloaded" {
		t.Fatalf("reload payload = %#v", reloadPayload)
	}
}

func findOperatorTool(defs []agent.ToolDefinition, name string) (agent.ToolDefinition, bool) {
	for _, def := range defs {
		if def.Name == name {
			return def, true
		}
	}
	return agent.ToolDefinition{}, false
}
