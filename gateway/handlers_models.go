package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/model"
	"github.com/fulcrus/hopclaw/modelrouter"
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type providerInfo struct {
	Name             string                 `json:"name"`
	API              string                 `json:"api"`
	BaseURL          string                 `json:"base_url"`
	Region           string                 `json:"region,omitempty"`
	AccessKeyID      string                 `json:"access_key_id,omitempty"`
	DefaultModel     string                 `json:"default_model"`
	Models           []modelMetaJSON        `json:"models"`
	HasKey           bool                   `json:"has_key"`
	APIKeysCount     int                    `json:"api_keys_count,omitempty"`
	Enabled          *bool                  `json:"enabled,omitempty"`
	Timeout          string                 `json:"timeout,omitempty"`
	HeaderCount      int                    `json:"header_count,omitempty"`
	Source           string                 `json:"source,omitempty"` // yaml or api
	Mutable          bool                   `json:"mutable"`
	ConfigScope      string                 `json:"config_scope,omitempty"`
	CapabilityMatrix model.CapabilityMatrix `json:"capability_matrix,omitempty"`
}

type modelsListResponse struct {
	Providers         []providerInfo `json:"providers"`
	Count             int            `json:"count"`
	DefaultProvider   string         `json:"default_provider,omitempty"`
	AgentDefaultModel string         `json:"agent_default_model,omitempty"`
}

type modelsRouterResponse struct {
	Profiles        []modelrouter.ProfileView `json:"profiles"`
	Count           int                       `json:"count"`
	DefaultProvider string                    `json:"default_provider,omitempty"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleModelsList lists configured providers and their known models.
//
//	GET /operator/models
func (g *Gateway) handleModelsList(w http.ResponseWriter, r *http.Request) {
	state, ok := g.currentProviderOperatorState()
	if !ok {
		gwError(w, http.StatusServiceUnavailable, "effective config not available")
		return
	}
	gwJSON(w, http.StatusOK, modelsListResponse{
		Providers:         state.Infos,
		Count:             len(state.Infos),
		DefaultProvider:   state.DefaultProvider,
		AgentDefaultModel: state.AgentDefaultModel,
	})
}

// handleModelsRouter returns the effective router profiles derived from the
// operator provider surface.
//
//	GET /operator/models/router
func (g *Gateway) handleModelsRouter(w http.ResponseWriter, _ *http.Request) {
	state, ok := g.currentProviderOperatorState()
	if !ok {
		gwError(w, http.StatusServiceUnavailable, "effective config not available")
		return
	}
	profiles := model.BuildRouterProfilesWithProviderCapabilities(state.Entries, state.CapabilityMatrices, state.DefaultProvider)
	gwJSON(w, http.StatusOK, modelsRouterResponse{
		Profiles:        modelrouter.ProfileViewsFromProfiles(profiles),
		Count:           len(profiles),
		DefaultProvider: state.DefaultProvider,
	})
}

// handleModelsAdd adds a new provider to the config store.
//
//	POST /operator/models
func (g *Gateway) handleModelsCreate(w http.ResponseWriter, r *http.Request) {
	if err := g.ensureProviderMutationAvailable(); err != nil {
		gwError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	var req providerMutationRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	name, err := req.resolveName("")
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, exists := g.providerProjection(name); exists {
		gwErrorf(w, http.StatusConflict, "provider %q already exists", name)
		return
	}

	cfg, err := req.providerConfig(name)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := g.upsertProviderConfig(r.Context(), name, cfg, nil); err != nil {
		gwError(w, httpStatusForConfigMutation(err), err.Error())
		return
	}
	if err := g.ensureAgentDefaultModelConfigured(r.Context(), name, cfg.DefaultModel); err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}

	gwJSON(w, http.StatusCreated, namedOKResponse{OK: true, Name: name})
}

// handleModelsUpdate updates an existing provider in the config store.
//
//	PUT /operator/models/{name}
func (g *Gateway) handleModelsUpdate(w http.ResponseWriter, r *http.Request) {
	if err := g.ensureProviderMutationAvailable(); err != nil {
		gwError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		gwError(w, http.StatusBadRequest, "missing provider name")
		return
	}

	projection, ok := g.providerProjection(name)
	if !ok {
		gwErrorf(w, http.StatusNotFound, "provider %q not found", name)
		return
	}

	var req providerMutationRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	name, err := req.resolveName(name)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}

	next, err := req.mergeProviderConfig(name, projection.Config)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := g.upsertProviderConfig(r.Context(), name, next, projection.Enabled); err != nil {
		gwError(w, httpStatusForConfigMutation(err), err.Error())
		return
	}

	gwJSON(w, http.StatusOK, namedOKResponse{OK: true, Name: name})
}

func (g *Gateway) ensureAgentDefaultModelConfigured(ctx context.Context, providerName, defaultModel string) error {
	if g == nil {
		return nil
	}
	defaultModel = strings.TrimSpace(defaultModel)
	providerName = strings.TrimSpace(providerName)
	if providerName == "" || defaultModel == "" {
		return nil
	}
	currentCfg, ok := g.currentOperatorConfig()
	if !ok {
		return nil
	}
	current := strings.TrimSpace(currentCfg.Agent.DefaultModel)
	if current != "" && current != "unconfigured-model" {
		return nil
	}
	qualifiedModel := providerName + "/" + defaultModel
	if _, _, err := g.putConfigSection(ctx, "agent", map[string]any{"default_model": qualifiedModel}); err != nil {
		return fmt.Errorf("update agent default model: %w", err)
	}
	return nil
}

// handleModelsDelete removes a provider from the config store.
//
//	DELETE /operator/models/{name}
func (g *Gateway) handleModelsDelete(w http.ResponseWriter, r *http.Request) {
	if err := g.ensureProviderMutationAvailable(); err != nil {
		gwError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		gwError(w, http.StatusBadRequest, "missing provider name")
		return
	}

	projection, ok := g.providerProjection(name)
	if !ok {
		gwErrorf(w, http.StatusNotFound, "provider %q not found", name)
		return
	}
	if err := g.deleteProviderConfig(r.Context(), name, projection.BasePresent); err != nil {
		gwError(w, httpStatusForConfigMutation(err), err.Error())
		return
	}

	gwJSON(w, http.StatusOK, namedOKResponse{OK: true, Name: name})
}
