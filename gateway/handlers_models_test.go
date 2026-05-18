package gateway

import (
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/config"
)

func TestHandleModelsCreateSetsAgentDefaultModelWhenUnconfigured(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{
			Address: "127.0.0.1:16280",
		},
		Agent: config.AgentConfig{
			DefaultModel: "unconfigured-model",
		},
	}
	gw, configPath := newFileBackedTestGateway(t, cfg)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/models", `{
		"name":"openai",
		"api":"openai-completions",
		"api_key":"sk-test",
		"base_url":"https://api.openai.com/v1",
		"default_model":"gpt-4.1"
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /operator/models status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	if updated.Agent.DefaultModel != "openai/gpt-4.1" {
		t.Fatalf("agent.default_model = %q, want openai/gpt-4.1", updated.Agent.DefaultModel)
	}
	if updated.Models.Providers["openai"].DefaultModel != "gpt-4.1" {
		t.Fatalf("provider default_model = %q, want gpt-4.1", updated.Models.Providers["openai"].DefaultModel)
	}
}

func TestHandleModelsCreatePreservesConfiguredAgentDefaultModel(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{
			Address: "127.0.0.1:16280",
		},
		Agent: config.AgentConfig{
			DefaultModel: "anthropic/claude-sonnet-4-20250514",
		},
	}
	gw, configPath := newFileBackedTestGateway(t, cfg)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/models", `{
		"name":"openai",
		"api":"openai-completions",
		"api_key":"sk-test",
		"base_url":"https://api.openai.com/v1",
		"default_model":"gpt-4.1"
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /operator/models status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	if updated.Agent.DefaultModel != "anthropic/claude-sonnet-4-20250514" {
		t.Fatalf("agent.default_model = %q, want existing configured model", updated.Agent.DefaultModel)
	}
}
