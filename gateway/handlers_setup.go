package gateway

import (
	"context"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/model"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	validateChatTimeout = 15 * time.Second
	validateTestMessage = "hello"
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type detectedKey struct {
	Provider  string `json:"provider"`
	MaskedKey string `json:"masked_key"`
}

type detectedProvider struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MaskedKey string `json:"masked_key,omitempty"`
}

type setupStatusResponse struct {
	Configured        bool               `json:"configured"`
	Providers         []string           `json:"providers"`
	DetectedEnvKeys   []detectedKey      `json:"detected_env_keys"`
	DetectedProviders []detectedProvider `json:"detected_providers"`
}

type validateModelsRequest struct {
	providerConnectionInput
}

type validateModelsResponse struct {
	Valid   bool            `json:"valid"`
	Message string          `json:"message"`
	Models  []modelMetaJSON `json:"models"`
}

type testChatRequest struct {
	providerConnectionInput
	Message string `json:"message"`
}

type testChatResponse struct {
	OK        bool   `json:"ok"`
	Reply     string `json:"reply"`
	LatencyMS int64  `json:"latency_ms"`
	Tokens    int    `json:"tokens"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleSetupCatalog returns the registry-backed setup metadata used by CLI and operator surfaces.
//
//	GET /operator/setup/catalog
func (g *Gateway) handleSetupCatalog(w http.ResponseWriter, _ *http.Request) {
	gwJSON(w, http.StatusOK, g.currentSetupCatalog())
}

// handleSetupStatus returns the current setup state including detected env keys
// and configured providers.
//
//	GET /operator/setup/status
func (g *Gateway) handleSetupStatus(w http.ResponseWriter, _ *http.Request) {
	resp := setupStatusResponse{
		Providers:         make([]string, 0),
		DetectedEnvKeys:   make([]detectedKey, 0),
		DetectedProviders: make([]detectedProvider, 0),
	}

	// Detect environment API keys.
	for _, detected := range detectSetupCatalogEnvKeys(g.currentSetupCatalog()) {
		resp.DetectedEnvKeys = append(resp.DetectedEnvKeys, detectedKey{
			Provider:  detected.Provider,
			MaskedKey: maskAPIKey(detected.Key),
		})
		resp.DetectedProviders = append(resp.DetectedProviders, detected.ProviderInfo)
	}

	if state, ok := g.currentProviderOperatorState(); ok {
		for _, info := range state.Infos {
			name := strings.TrimSpace(info.Name)
			if name == "" {
				continue
			}
			if info.Source == "openai_compat" && name == "default" {
				name = "openai"
			}
			resp.Providers = append(resp.Providers, name)
		}
		sort.Strings(resp.Providers)
		resp.Providers = normalize.DedupeStrings(resp.Providers)
		resp.Configured = len(resp.Providers) > 0
	}

	gwJSON(w, http.StatusOK, resp)
}

type setupCatalogDetectedKey struct {
	Provider     string
	Key          string
	ProviderInfo detectedProvider
}

func detectSetupCatalogEnvKeys(catalog config.OperatorSetupCatalog) []setupCatalogDetectedKey {
	if len(catalog.Providers) == 0 {
		return nil
	}

	apiSupportsAPIKey := make(map[string]bool, len(catalog.ProviderAPIs))
	for _, profile := range catalog.ProviderAPIs {
		api := canonicalProviderAPI(profile.ID)
		if api == "" {
			continue
		}
		for _, field := range profile.Fields {
			if strings.TrimSpace(field.ID) == "api_key" {
				apiSupportsAPIKey[api] = true
				break
			}
		}
	}

	out := make([]setupCatalogDetectedKey, 0, len(catalog.Providers))
	for _, profile := range catalog.Providers {
		api := canonicalProviderAPI(profile.API)
		if api == "" || !apiSupportsAPIKey[api] {
			continue
		}
		for _, envVar := range profile.EnvVars {
			envVar = strings.TrimSpace(envVar)
			if envVar == "" {
				continue
			}
			if value := strings.TrimSpace(os.Getenv(envVar)); value != "" {
				displayName := strings.TrimSpace(profile.DisplayName)
				if displayName == "" {
					displayName = strings.TrimSpace(profile.ID)
				}
				out = append(out, setupCatalogDetectedKey{
					Provider: profile.ID,
					Key:      value,
					ProviderInfo: detectedProvider{
						ID:        profile.ID,
						Name:      displayName,
						MaskedKey: maskAPIKey(value),
					},
				})
				break
			}
		}
	}
	return out
}

// handleModelsValidate creates a temporary registry and tests connectivity.
//
//	POST /operator/models/validate
func (g *Gateway) handleModelsValidate(w http.ResponseWriter, r *http.Request) {
	var req validateModelsRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	providerName := strings.TrimSpace(req.Provider)
	if providerName == "" {
		gwError(w, http.StatusBadRequest, "provider is required")
		return
	}
	input := req.providerConnectionInput
	if projection, ok := g.providerProjection(providerName); ok {
		merged, err := input.mergeProviderConfig(providerName, projection.Config)
		if err != nil {
			gwError(w, http.StatusBadRequest, err.Error())
			return
		}
		input = providerConnectionInput{
			Provider:        providerName,
			CatalogProvider: input.catalogProviderName(),
			API:             merged.API,
			BaseURL:         merged.BaseURL,
			Region:          merged.Region,
			APIKey:          merged.APIKey,
			APIKeys:         append([]string(nil), merged.APIKeys...),
			AccessKeyID:     merged.AccessKeyID,
			SecretKey:       merged.SecretKey,
			SessionToken:    merged.SessionToken,
			DefaultModel:    merged.DefaultModel,
			Timeout:         durationString(merged.Timeout),
			Headers:         cloneStringMap(merged.Headers),
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), validateChatTimeout)
	defer cancel()

	_, _, err := executeTempProviderChat(ctx, input, validateTestMessage)
	if err != nil {
		gwJSON(w, http.StatusOK, validateModelsResponse{
			Valid:   false,
			Message: err.Error(),
			Models:  make([]modelMetaJSON, 0),
		})
		return
	}

	modelsProvider := strings.TrimSpace(input.catalogProviderName())
	if modelsProvider == "" {
		modelsProvider = providerName
	}
	models := toModelMetaJSON(model.ModelsForProvider(modelsProvider))
	gwJSON(w, http.StatusOK, validateModelsResponse{
		Valid:   true,
		Message: "connection successful",
		Models:  models,
	})
}

// handleTestChat sends a user message through a temporary provider and returns
// the reply with latency metrics.
//
//	POST /operator/models/test-chat
func (g *Gateway) handleModelsTestChat(w http.ResponseWriter, r *http.Request) {
	var req testChatRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	providerName := strings.TrimSpace(req.Provider)
	if providerName == "" {
		gwError(w, http.StatusBadRequest, "provider is required")
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		gwError(w, http.StatusBadRequest, "message is required")
		return
	}
	input := req.providerConnectionInput
	if projection, ok := g.providerProjection(providerName); ok {
		merged, err := input.mergeProviderConfig(providerName, projection.Config)
		if err != nil {
			gwError(w, http.StatusBadRequest, err.Error())
			return
		}
		input = providerConnectionInput{
			Provider:        providerName,
			CatalogProvider: input.catalogProviderName(),
			API:             merged.API,
			BaseURL:         merged.BaseURL,
			Region:          merged.Region,
			APIKey:          merged.APIKey,
			APIKeys:         append([]string(nil), merged.APIKeys...),
			AccessKeyID:     merged.AccessKeyID,
			SecretKey:       merged.SecretKey,
			SessionToken:    merged.SessionToken,
			DefaultModel:    merged.DefaultModel,
			Timeout:         durationString(merged.Timeout),
			Headers:         cloneStringMap(merged.Headers),
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), validateChatTimeout)
	defer cancel()

	resp, latency, err := executeTempProviderChat(ctx, input, msg)
	if err != nil {
		gwJSON(w, http.StatusOK, testChatResponse{OK: false, Reply: err.Error()})
		return
	}

	tokens := 0
	if resp.Usage != nil {
		tokens = resp.Usage.TotalTokens
	}
	gwJSON(w, http.StatusOK, testChatResponse{
		OK:        true,
		Reply:     resp.Message.Content,
		LatencyMS: latency.Milliseconds(),
		Tokens:    tokens,
	})
}
