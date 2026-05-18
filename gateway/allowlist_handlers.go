package gateway

import (
	"errors"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/channels/allowlist"
	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type allowlistSetRequest struct {
	AllowAll    bool     `json:"allow_all"`
	AllowUsers  []string `json:"allow_users,omitempty"`
	DenyUsers   []string `json:"deny_users,omitempty"`
	AllowGroups []string `json:"allow_groups,omitempty"`
	DenyGroups  []string `json:"deny_groups,omitempty"`
}

type allowlistListResponse struct {
	Items []allowlist.ChannelRules `json:"items"`
	Count int                      `json:"count"`
}

type allowlistSetResponse struct {
	OK      bool   `json:"ok"`
	Channel string `json:"channel"`
}

type allowlistDeleteResponse struct {
	OK      bool   `json:"ok"`
	Channel string `json:"channel"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (g *Gateway) handleAllowlistList(w http.ResponseWriter, _ *http.Request) {
	if g.allowlist == nil {
		gwError(w, http.StatusServiceUnavailable, "allowlist service not available")
		return
	}
	items := g.allowlist.ListRules()
	if items == nil {
		items = []allowlist.ChannelRules{}
	}
	gwJSON(w, http.StatusOK, allowlistListResponse{Items: items, Count: len(items)})
}

func (g *Gateway) handleAllowlistGet(w http.ResponseWriter, r *http.Request) {
	if g.allowlist == nil {
		gwError(w, http.StatusServiceUnavailable, "allowlist service not available")
		return
	}
	channel := strings.TrimSpace(r.PathValue("channel"))
	if channel == "" {
		gwError(w, http.StatusBadRequest, "channel is required")
		return
	}
	rules, ok := g.allowlist.GetRules(channel)
	if !ok {
		gwError(w, http.StatusNotFound, "no rules for channel")
		return
	}
	gwJSON(w, http.StatusOK, rules)
}

func (g *Gateway) handleAllowlistSet(w http.ResponseWriter, r *http.Request) {
	if g.allowlist == nil {
		gwError(w, http.StatusServiceUnavailable, "allowlist service not available")
		return
	}
	channel := strings.TrimSpace(r.PathValue("channel"))
	if channel == "" {
		gwError(w, http.StatusBadRequest, "channel is required")
		return
	}

	var req allowlistSetRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		if errors.Is(err, errRequestBodyTooLarge) {
			gwErrorCode(w, http.StatusRequestEntityTooLarge, apiresponse.ErrorCodeRequestBodyTooLarge, err.Error())
			return
		}
		gwErrorCode(w, http.StatusBadRequest, apiresponse.ErrorCodeInvalidJSON, "invalid json")
		return
	}

	g.allowlist.SetRules(channel, allowlist.ChannelRules{
		AllowAll:    req.AllowAll,
		AllowUsers:  req.AllowUsers,
		DenyUsers:   req.DenyUsers,
		AllowGroups: req.AllowGroups,
		DenyGroups:  req.DenyGroups,
	})
	gwJSON(w, http.StatusOK, allowlistSetResponse{OK: true, Channel: channel})
}

func (g *Gateway) handleAllowlistDelete(w http.ResponseWriter, r *http.Request) {
	if g.allowlist == nil {
		gwError(w, http.StatusServiceUnavailable, "allowlist service not available")
		return
	}
	channel := strings.TrimSpace(r.PathValue("channel"))
	if channel == "" {
		gwError(w, http.StatusBadRequest, "channel is required")
		return
	}
	g.allowlist.RemoveRules(channel)
	gwJSON(w, http.StatusOK, allowlistDeleteResponse{OK: true, Channel: channel})
}
