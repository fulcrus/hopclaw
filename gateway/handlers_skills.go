package gateway

import (
	"net/http"
	"strings"
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type skillSummary struct {
	ID              string               `json:"id,omitempty"`
	Name            string               `json:"name"`
	Kind            string               `json:"kind,omitempty"`
	Status          string               `json:"status,omitempty"`
	Trust           string               `json:"trust,omitempty"`
	Version         string               `json:"version,omitempty"`
	InstallDir      string               `json:"install_dir,omitempty"`
	BundleDir       string               `json:"bundle_dir,omitempty"`
	SourceKind      string               `json:"source_kind,omitempty"`
	Summary         string               `json:"summary,omitempty"`
	Description     string               `json:"description,omitempty"`
	Pinned          bool                 `json:"pinned,omitempty"`
	Installed       bool                 `json:"installed,omitempty"`
	InstalledAt     string               `json:"installed_at,omitempty"`
	UserInvocable   *bool                `json:"user_invocable,omitempty"`
	Ready           bool                 `json:"ready"`
	Eligible        bool                 `json:"eligible"`
	Tools           []string             `json:"tools,omitempty"`
	ToolCount       int                  `json:"tool_count,omitempty"`
	DetailAvailable bool                 `json:"detail_available,omitempty"`
	Installability  *skillInstallability `json:"installability,omitempty"`
	Risk            *skillRiskProjection `json:"risk,omitempty"`
}

type skillsListResponse struct {
	Items []skillSummary `json:"items"`
	Count int            `json:"count"`
}

type skillsCatalogResponse struct {
	Items []skillCatalogItem `json:"items"`
	Count int                `json:"count"`
}

type skillCatalogItem struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Version         string               `json:"version,omitempty"`
	Summary         string               `json:"summary,omitempty"`
	Description     string               `json:"description,omitempty"`
	Installed       bool                 `json:"installed"`
	Ready           bool                 `json:"ready"`
	Eligible        bool                 `json:"eligible"`
	Tools           []string             `json:"tools,omitempty"`
	ToolCount       int                  `json:"tool_count,omitempty"`
	SourceKind      string               `json:"source_kind,omitempty"`
	DetailAvailable bool                 `json:"detail_available,omitempty"`
	Installability  *skillInstallability `json:"installability,omitempty"`
	Risk            *skillRiskProjection `json:"risk,omitempty"`
}

type skillInstallability struct {
	Score    int    `json:"score"`
	Label    string `json:"label"`
	Checks   int    `json:"checks,omitempty"`
	Missing  int    `json:"missing,omitempty"`
	Warnings int    `json:"warnings,omitempty"`
}

type skillRiskProjection struct {
	Level string   `json:"level"`
	Tags  []string `json:"tags,omitempty"`
}

type skillInstallRequest struct {
	Source  string `json:"source"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type skillConfigRequest struct {
	Config map[string]any `json:"config"`
}

type preflightRequest struct {
	Name     string   `json:"name,omitempty"`
	Source   string   `json:"source,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Binaries []string `json:"binaries"`
	EnvVars  []string `json:"env_vars"`
}

type preflightCheck struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // "binary" or "env_var"
	Present bool   `json:"present"`
}

type preflightResponse struct {
	Checks []preflightCheck `json:"checks"`
	Ready  bool             `json:"ready"`
	Skill  map[string]any   `json:"skill,omitempty"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleSkillsList returns all installed skills from the skill service.
//
//	GET /operator/skills
func (g *Gateway) handleSkillsList(w http.ResponseWriter, r *http.Request) {
	items := g.listInstalledSkills(r.Context())

	gwJSON(w, http.StatusOK, skillsListResponse{
		Items: items,
		Count: len(items),
	})
}

// handleSkillsGet returns a deep runtime report for one skill.
//
//	GET /operator/skills/{name}
func (g *Gateway) handleSkillsGet(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		gwError(w, http.StatusBadRequest, "missing skill name")
		return
	}
	if g.skillService == nil {
		gwError(w, http.StatusServiceUnavailable, "skill service not available")
		return
	}
	report, ok := g.skillService.Inspect(name, g.skillRuntimeContext())
	if !ok {
		gwError(w, http.StatusNotFound, "skill not found")
		return
	}
	g.attachInstalledSkill(&report)
	payload := gatewaySkillReportPayload(report)
	payload["message"] = "skill inspected successfully"
	gwJSON(w, http.StatusOK, payload)
}

// handleSkillsCatalogGet returns an inspect-like detail payload for one catalog skill.
//
//	GET /operator/skills/catalog/{id}
func (g *Gateway) handleSkillsCatalogGet(w http.ResponseWriter, r *http.Request) {
	if g.skillHub == nil {
		gwError(w, http.StatusServiceUnavailable, "skill hub not available")
		return
	}
	ref := strings.TrimSpace(r.PathValue("id"))
	if ref == "" {
		gwError(w, http.StatusBadRequest, "missing skill id")
		return
	}
	entry, ok, err := g.findCatalogEntry(r.Context(), ref)
	if err != nil {
		gwError(w, http.StatusBadGateway, err.Error())
		return
	}
	if !ok {
		gwError(w, http.StatusNotFound, "skill not found in catalog")
		return
	}
	payload := g.buildCatalogSkillDetailPayload(r.Context(), entry)
	payload["message"] = "catalog skill inspected successfully"
	gwJSON(w, http.StatusOK, payload)
}

// handleSkillsCatalog returns the available skill catalog.
//
//	GET /operator/skills/catalog
func (g *Gateway) handleSkillsCatalog(w http.ResponseWriter, r *http.Request) {
	if g.skillHub == nil {
		gwError(w, http.StatusServiceUnavailable, "skill hub not available")
		return
	}
	results, err := g.skillHub.Search(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		gwError(w, http.StatusBadGateway, err.Error())
		return
	}
	installed := g.installedSkillSet()
	items := make([]skillCatalogItem, 0, len(results))
	for _, entry := range results {
		items = append(items, g.buildCatalogSkillItem(r.Context(), entry, installed))
	}
	gwJSON(w, http.StatusOK, skillsCatalogResponse{
		Items: items,
		Count: len(items),
	})
}

// handleSkillsInstall installs a skill from the configured skill hub.
//
//	POST /operator/skills/install
func (g *Gateway) handleSkillsInstall(w http.ResponseWriter, r *http.Request) {
	if g.skillHub == nil {
		gwError(w, http.StatusServiceUnavailable, "skill hub not available")
		return
	}

	var req skillInstallRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	result, status, err := g.installSkill(r.Context(), req)
	if err != nil {
		gwError(w, status, err.Error())
		return
	}
	g.refreshSkillRegistry(r.Context())
	payload := skillInstallPayload(result)
	if report, ok := g.inspectInstalledSkill(result.SkillID); ok {
		payload["validation"] = gatewaySkillReportPayload(report)
	}
	gwJSON(w, http.StatusCreated, payload)
}

// handleSkillsDelete removes an installed skill.
//
//	DELETE /operator/skills/{name}
func (g *Gateway) handleSkillsDelete(w http.ResponseWriter, r *http.Request) {
	if g.skillHub == nil {
		gwError(w, http.StatusServiceUnavailable, "skill hub not available")
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		gwError(w, http.StatusBadRequest, "missing skill name")
		return
	}
	if err := g.skillHub.Remove(name); err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	g.refreshSkillRegistry(r.Context())
	gwJSON(w, http.StatusOK, namedOKResponse{OK: true, Name: name})
}

// handleSkillsConfig updates a skill's configuration (placeholder).
//
//	PUT /operator/skills/{name}/config
func (g *Gateway) handleSkillsUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if g.configWatcher == nil || g.configPath == "" {
		gwError(w, http.StatusServiceUnavailable, "config watcher not available")
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		gwError(w, http.StatusBadRequest, "missing skill name")
		return
	}

	var req skillConfigRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	resp, err := g.updateSkillConfig(name, req.Config)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, resp)
}

// handleSkillsPreflight checks whether required binaries and env vars exist.
//
//	POST /operator/skills/preflight
func (g *Gateway) handleSkillsPreflight(w http.ResponseWriter, r *http.Request) {
	var req preflightRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	resp, status, err := g.buildSkillPreflight(r.Context(), req)
	if err != nil {
		gwError(w, status, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, resp)
}
