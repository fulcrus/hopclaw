package gateway

import (
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/channels/pairing"
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type pairingVerifyRequest struct {
	Code string `json:"code"`
}

type pairingInitiateRequest struct {
	Channel     string `json:"channel"`
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name,omitempty"`
}

type pairingRecordResponse struct {
	Record pairing.PairingRecord `json:"record"`
}

type pairingListResponse struct {
	Items []pairing.PairingRecord `json:"items"`
	Count int                     `json:"count"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (g *Gateway) handlePairingList(w http.ResponseWriter, _ *http.Request) {
	if g.pairing == nil {
		gwError(w, http.StatusServiceUnavailable, "pairing service not available")
		return
	}
	records, err := g.pairing.List()
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if records == nil {
		records = []pairing.PairingRecord{}
	}
	gwJSON(w, http.StatusOK, pairingListResponse{Items: records, Count: len(records)})
}

func (g *Gateway) handlePairingInitiate(w http.ResponseWriter, r *http.Request) {
	if g.pairing == nil {
		gwError(w, http.StatusServiceUnavailable, "pairing service not available")
		return
	}
	var req pairingInitiateRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Channel) == "" || strings.TrimSpace(req.UserID) == "" {
		gwError(w, http.StatusBadRequest, "channel and user_id are required")
		return
	}
	if _, err := g.pairing.InitiatePairing(req.Channel, req.UserID, req.DisplayName); err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	rec, err := g.pairing.Store().Get(strings.TrimSpace(req.Channel), strings.TrimSpace(req.UserID))
	if err != nil {
		gwError(w, http.StatusInternalServerError, "pairing record not found after initiation")
		return
	}
	gwJSON(w, http.StatusOK, pairingRecordResponse{Record: *rec})
}

func (g *Gateway) handlePairingVerify(w http.ResponseWriter, r *http.Request) {
	if g.pairing == nil {
		gwError(w, http.StatusServiceUnavailable, "pairing service not available")
		return
	}
	var req pairingVerifyRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		gwError(w, http.StatusBadRequest, "code is required")
		return
	}

	rec, err := g.pairing.VerifyCode(req.Code)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, pairingRecordResponse{Record: *rec})
}

func (g *Gateway) handlePairingRevoke(w http.ResponseWriter, r *http.Request) {
	if g.pairing == nil {
		gwError(w, http.StatusServiceUnavailable, "pairing service not available")
		return
	}

	channel := r.PathValue("channel")
	userID := r.PathValue("user_id")
	if strings.TrimSpace(channel) == "" || strings.TrimSpace(userID) == "" {
		gwError(w, http.StatusBadRequest, "channel and user_id are required")
		return
	}

	if err := g.pairing.Revoke(channel, userID); err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, pairingRevokeResponse{
		OK:      true,
		Channel: strings.TrimSpace(channel),
		UserID:  strings.TrimSpace(userID),
	})
}
