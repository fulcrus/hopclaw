package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/store"
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type channelInfo struct {
	Name    string          `json:"name"`
	Config  json.RawMessage `json:"config"`
	Enabled *bool           `json:"enabled,omitempty"`
	Source  string          `json:"source,omitempty"`
}

type channelsListResponse struct {
	Items []channelInfo `json:"items"`
	Count int           `json:"count"`
}

type addChannelRequest struct {
	Name    string          `json:"name"`
	Config  json.RawMessage `json:"config"`
	Enabled *bool           `json:"enabled,omitempty"`
}

type updateChannelRequest struct {
	Config  json.RawMessage `json:"config,omitempty"`
	Enabled *bool           `json:"enabled,omitempty"`
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleChannelsCRUDList lists configured channels from the config store.
//
//	GET /operator/channels
func (g *Gateway) handleChannelsCRUDList(w http.ResponseWriter, r *http.Request) {
	projections, ok := g.currentChannelProjections()
	if !ok {
		gwError(w, http.StatusServiceUnavailable, "effective config not available")
		return
	}
	currentCfg, ok := g.currentOperatorConfig()
	if !ok {
		gwError(w, http.StatusServiceUnavailable, "effective config not available")
		return
	}
	items := make([]channelInfo, 0, len(projections))
	for _, row := range projections {
		safeConfig, err := config.SanitizeChannelConfigForOperator(row.Name, currentCfg, row.Config, row.Recognized)
		if err != nil {
			gwError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, channelInfo{
			Name:    row.Name,
			Config:  append(json.RawMessage(nil), safeConfig...),
			Enabled: row.Enabled,
			Source:  row.Source,
		})
	}
	sortChannelInfoItems(items)
	gwJSON(w, http.StatusOK, channelsListResponse{
		Items: items,
		Count: len(items),
	})
}

// handleChannelsCRUDCreate adds a new channel to the config store.
//
//	POST /operator/channels
func (g *Gateway) handleChannelsCRUDCreate(w http.ResponseWriter, r *http.Request) {
	if !g.fileBackedConfig() && g.configMutator == nil {
		gwError(w, http.StatusServiceUnavailable, controlplane.ErrMutationUnavailable.Error())
		return
	}

	var req addChannelRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		gwError(w, http.StatusBadRequest, "name is required")
		return
	}
	if g.fileBackedConfig() {
		canonicalName, ok := canonicalFileBackedChannelName(name)
		if !ok {
			gwError(w, http.StatusBadRequest, invalidFileBackedChannelName(name).Error())
			return
		}
		name = canonicalName
		if err := validateFileBackedChannelInputType(name, req.Config); err != nil {
			gwError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if _, exists := g.channelProjection(name); exists {
		gwErrorf(w, http.StatusConflict, "channel %q already exists", name)
		return
	}

	configJSON := json.RawMessage(`{}`)
	if len(req.Config) > 0 {
		merged, _, err := config.MergeChannelConfig(name, nil, req.Config)
		if err != nil {
			gwError(w, http.StatusBadRequest, err.Error())
			return
		}
		configJSON = merged
	}

	row := store.ChannelConfigRow{
		Name:    name,
		Config:  string(configJSON),
		Enabled: req.Enabled,
	}
	if g.fileBackedConfig() {
		if err := g.upsertChannelInFile(name, configJSON, req.Enabled); err != nil {
			gwError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := g.triggerConfigReload(); err != nil {
			gwError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else if err := g.configMutator.UpsertChannel(r.Context(), row); err != nil {
		gwError(w, httpStatusForConfigMutation(err), err.Error())
		return
	}

	gwJSON(w, http.StatusCreated, namedOKResponse{OK: true, Name: name})
}

// handleChannelsCRUDUpdate updates an existing channel in the config store.
//
//	PUT /operator/channels/{name}
func (g *Gateway) handleChannelsCRUDUpdate(w http.ResponseWriter, r *http.Request) {
	if !g.fileBackedConfig() && g.configMutator == nil {
		gwError(w, http.StatusServiceUnavailable, controlplane.ErrMutationUnavailable.Error())
		return
	}

	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		gwError(w, http.StatusBadRequest, "missing channel name")
		return
	}
	if g.fileBackedConfig() {
		canonicalName, ok := canonicalFileBackedChannelName(name)
		if !ok {
			gwError(w, http.StatusBadRequest, invalidFileBackedChannelName(name).Error())
			return
		}
		name = canonicalName
	}

	projection, ok := g.channelProjection(name)
	if !ok {
		gwErrorf(w, http.StatusNotFound, "channel %q not found", name)
		return
	}

	var req updateChannelRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	if g.fileBackedConfig() {
		if err := validateFileBackedChannelInputType(name, req.Config); err != nil {
			gwError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	configJSON := append(json.RawMessage(nil), projection.Config...)
	if len(req.Config) > 0 {
		merged, _, err := config.MergeChannelConfig(name, projection.Config, req.Config)
		if err != nil {
			gwError(w, http.StatusBadRequest, err.Error())
			return
		}
		configJSON = merged
	}
	enabled := projection.Enabled
	if req.Enabled != nil {
		enabled = req.Enabled
	}
	if g.fileBackedConfig() {
		if err := g.upsertChannelInFile(name, configJSON, cloneBoolPtrGateway(enabled)); err != nil {
			gwError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := g.triggerConfigReload(); err != nil {
			gwError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else if err := g.configMutator.UpsertChannel(r.Context(), store.ChannelConfigRow{
		Name:    name,
		Config:  string(configJSON),
		Enabled: cloneBoolPtrGateway(enabled),
	}); err != nil {
		gwError(w, httpStatusForConfigMutation(err), err.Error())
		return
	}

	gwJSON(w, http.StatusOK, namedOKResponse{OK: true, Name: name})
}

// handleChannelsCRUDDelete removes a channel from the config store.
//
//	DELETE /operator/channels/{name}
func (g *Gateway) handleChannelsCRUDDelete(w http.ResponseWriter, r *http.Request) {
	if !g.fileBackedConfig() && g.configMutator == nil {
		gwError(w, http.StatusServiceUnavailable, controlplane.ErrMutationUnavailable.Error())
		return
	}

	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		gwError(w, http.StatusBadRequest, "missing channel name")
		return
	}
	if g.fileBackedConfig() {
		canonicalName, ok := canonicalFileBackedChannelName(name)
		if !ok {
			gwError(w, http.StatusBadRequest, invalidFileBackedChannelName(name).Error())
			return
		}
		name = canonicalName
	}

	projection, ok := g.channelProjection(name)
	if !ok {
		gwErrorf(w, http.StatusNotFound, "channel %q not found", name)
		return
	}
	if g.fileBackedConfig() {
		if err := g.deleteChannelInFile(name); err != nil {
			gwError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := g.triggerConfigReload(); err != nil {
			gwError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else if err := g.configMutator.DeleteChannel(r.Context(), name, projection.BasePresent); err != nil {
		gwError(w, httpStatusForConfigMutation(err), err.Error())
		return
	}

	gwJSON(w, http.StatusOK, namedOKResponse{OK: true, Name: name})
}

func (g *Gateway) currentChannelProjections() ([]controlplane.ChannelProjection, bool) {
	if g == nil || g.effectiveCfg == nil {
		return nil, false
	}
	return g.effectiveCfg.Channels(), true
}

func (g *Gateway) channelProjection(name string) (controlplane.ChannelProjection, bool) {
	projections, ok := g.currentChannelProjections()
	if !ok {
		return controlplane.ChannelProjection{}, false
	}
	name = strings.TrimSpace(name)
	for _, item := range projections {
		if item.Name == name {
			return item, true
		}
	}
	return controlplane.ChannelProjection{}, false
}
