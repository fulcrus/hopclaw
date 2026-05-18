package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/config"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	configKeyMaskPlaceholder = "***"
	configMaxBodySize        = 1 << 20 // 1 MiB
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type configUpdateResponse struct {
	OK         bool              `json:"ok"`
	ReloadPlan config.ReloadPlan `json:"reload_plan"`
}

type configValidateResponse struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

type configPreviewRequest struct {
	ChangedPaths []string `json:"changed_paths"`
}

type configPreviewResponse struct {
	Plan config.ReloadPlan `json:"plan"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleConfigGet returns the full current config with API keys masked.
//
//	GET /operator/config
func (g *Gateway) handleConfigGet(w http.ResponseWriter, _ *http.Request) {
	cfg, ok := g.currentOperatorConfig()
	if !ok {
		gwError(w, http.StatusServiceUnavailable, "effective config not available")
		return
	}
	gwJSON(w, http.StatusOK, cfg.SanitizeForOperator())
}

// handleConfigSection returns a single config section by name.
//
//	GET /operator/config/{section}
func (g *Gateway) handleConfigGetSection(w http.ResponseWriter, r *http.Request) {
	section := strings.TrimSpace(r.PathValue("section"))
	if section == "" {
		gwError(w, http.StatusBadRequest, "missing section name")
		return
	}

	cfg, ok := g.currentOperatorConfig()
	if !ok {
		gwError(w, http.StatusServiceUnavailable, "effective config not available")
		return
	}
	value, err := extractConfigSection(cfg.SanitizeForOperator(), section)
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, value)
}

// handleConfigUpdate replaces a config section and writes it to the config file.
//
//	PUT /operator/config/{section}
func (g *Gateway) handleConfigPutSection(w http.ResponseWriter, r *http.Request) {
	section := strings.TrimSpace(r.PathValue("section"))
	if section == "" {
		gwError(w, http.StatusBadRequest, "missing section name")
		return
	}
	var sectionValue any
	if !decodeOperatorJSONBody(w, r, &sectionValue) {
		return
	}
	resp, status, err := g.putConfigSection(r.Context(), section, sectionValue)
	if err != nil {
		gwError(w, status, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, resp)
}

// handleConfigValidate validates a full or partial config JSON body.
//
//	POST /operator/config/validate
func (g *Gateway) handleConfigValidate(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if !decodeOperatorJSONBody(w, r, &raw) {
		return
	}

	currentCfg, hasCurrent := g.currentOperatorConfig()
	errors := validateConfigPayload(raw, currentCfg, hasCurrent)
	gwJSON(w, http.StatusOK, configValidateResponse{
		Valid:  len(errors) == 0,
		Errors: errors,
	})
}

// handleConfigPreview previews the reload plan for a set of changed paths.
//
//	POST /operator/config/preview
func (g *Gateway) handleConfigPreview(w http.ResponseWriter, r *http.Request) {
	var req configPreviewRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}

	plan := config.AnalyzeReloadPlan(req.ChangedPaths)
	gwJSON(w, http.StatusOK, configPreviewResponse{Plan: plan})
}
