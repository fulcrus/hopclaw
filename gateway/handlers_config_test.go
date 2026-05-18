package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/config"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
)

func TestHandleConfigValidateRejectsSemanticError(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/config/validate", `{
		"models": {
			"openai_compat": {
				"base_url": "https://api.example.com/v1"
			}
		}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /operator/config/validate status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload configValidateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Valid {
		t.Fatalf("valid = true, want false; body=%s", rec.Body.String())
	}
	if len(payload.Errors) == 0 || !strings.Contains(payload.Errors[0], "models.openai_compat.model is required") {
		t.Fatalf("errors = %#v", payload.Errors)
	}
}

func TestHandleConfigValidateMergesWithEffectiveConfig(t *testing.T) {
	t.Parallel()

	base := config.Config{
		Server: config.ServerConfig{
			AuthToken: "test-token",
		},
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    ".hopclaw/state",
		},
		Agent: config.AgentConfig{
			DefaultModel: "openai/gpt-4o",
		},
		Models: config.ModelsConfig{
			OpenAICompat: config.OpenAICompatConfig{
				BaseURL: "https://api.example.com/v1",
				Model:   "gpt-4o-mini",
			},
		},
	}
	base.ApplyDefaults()

	resolver, err := controloverlay.NewResolver(context.Background(), base, nil, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	gw := newTestGatewayFull(t)
	gw.SetEffectiveConfigResolver(resolver)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/config/validate", `{
		"models": {
			"openai_compat": {
				"model": ""
			}
		}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /operator/config/validate status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload configValidateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Valid {
		t.Fatalf("valid = true, want false; body=%s", rec.Body.String())
	}
	if len(payload.Errors) == 0 || !strings.Contains(payload.Errors[0], "models.openai_compat.model is required") {
		t.Fatalf("errors = %#v", payload.Errors)
	}
}

func TestHandleConfigValidateRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/config/validate", `{"agent":{"default_model":"gpt-4o"}} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/config/validate status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleConfigPreviewReturnsReloadPlan(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/config/preview", `{"changed_paths":["models.providers.openai","skills.config.demo"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /operator/config/preview status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload configPreviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Plan.Action == "" {
		t.Fatalf("reload plan missing action: %#v", payload)
	}
}

func TestHandleConfigPreviewRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/config/preview", `{"changed_paths":["agent.default_model"]} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/config/preview status = %d body=%s", rec.Code, rec.Body.String())
	}
}
