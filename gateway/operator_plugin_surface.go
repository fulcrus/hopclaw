package gateway

import (
	"net/http"

	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/plugin"
)

type pluginRuntimeModuleSummary struct {
	ID                string                `json:"id,omitempty"`
	Kind              string                `json:"kind,omitempty"`
	Source            modules.Source        `json:"source,omitempty"`
	Delivery          modules.Delivery      `json:"delivery,omitempty"`
	Level             modules.ModuleLevel   `json:"level,omitempty"`
	Health            *modules.HealthReport `json:"health,omitempty"`
	ContributionCount int                   `json:"contribution_count,omitempty"`
	ProjectionVersion string                `json:"projection_version,omitempty"`
}

type pluginSummary struct {
	Name            string                      `json:"name"`
	Version         string                      `json:"version"`
	Description     string                      `json:"description,omitempty"`
	Author          string                      `json:"author,omitempty"`
	Enabled         bool                        `json:"enabled"`
	Source          string                      `json:"source,omitempty"`
	Dir             string                      `json:"dir,omitempty"`
	ComponentCounts map[string]int              `json:"component_counts,omitempty"`
	RuntimeModule   *pluginRuntimeModuleSummary `json:"runtime_module,omitempty"`
}

type pluginsListResponse struct {
	Items []pluginSummary `json:"items"`
	Count int             `json:"count"`
}

type pluginGetResponse struct {
	Plugin          pluginSummary                `json:"plugin"`
	Channels        map[string]any               `json:"channels,omitempty"`
	Tools           int                          `json:"tools"`
	Components      []plugin.ComponentDescriptor `json:"components,omitempty"`
	ComponentCounts map[string]int               `json:"component_counts,omitempty"`
}

type pluginInstallRequest struct {
	Source string `json:"source"`
}

type pluginOKResponse struct {
	OK   bool   `json:"ok"`
	Name string `json:"name,omitempty"`
}

type operatorPluginSurface struct {
	deps pluginOperatorDeps
}

func newOperatorPluginSurface(deps pluginOperatorDeps) *operatorPluginSurface {
	return &operatorPluginSurface{deps: deps}
}

func (s *operatorPluginSurface) RegisterRoutes(mux *http.ServeMux, mountAuthed func(*http.ServeMux, string, func(http.ResponseWriter, *http.Request))) {
	if mux == nil || mountAuthed == nil {
		return
	}
	mountAuthed(mux, "GET /operator/plugins", s.handlePluginsList)
	mountAuthed(mux, "GET /operator/plugins/{name}", s.handlePluginsGet)
	mountAuthed(mux, "POST /operator/plugins", s.handlePluginsInstall)
	mountAuthed(mux, "DELETE /operator/plugins/{name}", s.handlePluginsUninstall)
	mountAuthed(mux, "POST /operator/plugins/{name}/enable", s.handlePluginsEnable)
	mountAuthed(mux, "POST /operator/plugins/{name}/disable", s.handlePluginsDisable)
}

func (s *operatorPluginSurface) handlePluginsList(w http.ResponseWriter, _ *http.Request) {
	items := s.deps.listPluginSummaries()
	gwJSON(w, http.StatusOK, pluginsListResponse{Items: items, Count: len(items)})
}

func (s *operatorPluginSurface) handlePluginsGet(w http.ResponseWriter, r *http.Request) {
	name, ok := requiredPluginNameFromPath(w, r)
	if !ok {
		return
	}
	entry, ok := s.deps.lookupPlugin(name)
	if !ok {
		gwError(w, http.StatusNotFound, "plugin not found")
		return
	}
	gwJSON(w, http.StatusOK, s.deps.pluginDetailResponseFromEntry(entry))
}

func (s *operatorPluginSurface) handlePluginsInstall(w http.ResponseWriter, r *http.Request) {
	if !requirePluginInstaller(w, s.deps.pluginInstaller) {
		return
	}

	var req pluginInstallRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}

	result, status, err := s.deps.installPlugin(r.Context(), req)
	if err != nil {
		gwError(w, status, err.Error())
		return
	}

	gwJSON(w, http.StatusCreated, pluginOKResponse{OK: true, Name: result.Name})
}

func (s *operatorPluginSurface) handlePluginsUninstall(w http.ResponseWriter, r *http.Request) {
	name, ok := requiredPluginNameFromPath(w, r)
	if !ok {
		return
	}
	if !requirePluginInstaller(w, s.deps.pluginInstaller) {
		return
	}

	status, err := s.deps.uninstallPlugin(r.Context(), name)
	if err != nil {
		gwError(w, status, err.Error())
		return
	}

	gwJSON(w, http.StatusOK, pluginOKResponse{OK: true, Name: name})
}

func (s *operatorPluginSurface) handlePluginsEnable(w http.ResponseWriter, r *http.Request) {
	name, ok := requiredPluginNameFromPath(w, r)
	if !ok {
		return
	}
	if !requirePluginInstaller(w, s.deps.pluginInstaller) {
		return
	}
	status, err := s.deps.setPluginEnabled(r.Context(), name, true)
	if err != nil {
		gwError(w, status, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, pluginOKResponse{OK: true, Name: name})
}

func (s *operatorPluginSurface) handlePluginsDisable(w http.ResponseWriter, r *http.Request) {
	name, ok := requiredPluginNameFromPath(w, r)
	if !ok {
		return
	}
	if !requirePluginInstaller(w, s.deps.pluginInstaller) {
		return
	}
	status, err := s.deps.setPluginEnabled(r.Context(), name, false)
	if err != nil {
		gwError(w, status, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, pluginOKResponse{OK: true, Name: name})
}
