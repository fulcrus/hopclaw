package gateway

import (
	"net/http"
	"strings"

	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
)

type browserProfileCreateRequest struct {
	Name   string `json:"name"`
	Color  string `json:"color,omitempty"`
	CDPURL string `json:"cdp_url,omitempty"`
}

func (g *Gateway) handleBrowserProfilesList(w http.ResponseWriter, r *http.Request) {
	if g.browserClient == nil {
		gwError(w, http.StatusServiceUnavailable, "browser host not configured")
		return
	}
	profiles, err := g.browserClient.ListProfiles(r.Context())
	if err != nil {
		gwError(w, http.StatusBadGateway, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: profiles, Count: len(profiles)})
}

func (g *Gateway) handleBrowserProfilesCreate(w http.ResponseWriter, r *http.Request) {
	if g.browserClient == nil {
		gwError(w, http.StatusServiceUnavailable, "browser host not configured")
		return
	}

	var req browserProfileCreateRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		gwError(w, http.StatusBadRequest, "profile name is required")
		return
	}

	profile, err := g.browserClient.CreateProfile(r.Context(), browserclient.CreateProfileRequest{
		Name:   req.Name,
		Color:  strings.TrimSpace(req.Color),
		CDPURL: strings.TrimSpace(req.CDPURL),
	})
	if err != nil {
		gwError(w, http.StatusBadGateway, err.Error())
		return
	}
	gwJSON(w, http.StatusCreated, profile)
}

func (g *Gateway) handleBrowserProfilesDelete(w http.ResponseWriter, r *http.Request) {
	if g.browserClient == nil {
		gwError(w, http.StatusServiceUnavailable, "browser host not configured")
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		gwError(w, http.StatusBadRequest, "profile name is required")
		return
	}
	if err := g.browserClient.DeleteProfile(r.Context(), name); err != nil {
		gwError(w, http.StatusBadGateway, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, deletedOKResponse{OK: true, Deleted: name})
}
