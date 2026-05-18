package gateway

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
)

func (g *Gateway) handleGovernanceDeliveriesList(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	filter, err := gatewayGovernanceDeliveryFilter(r)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := g.runtime.ListGovernanceDeliveries(r.Context(), filter)
	if err != nil {
		gwGovernanceError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: items, Count: len(items)})
}

func (g *Gateway) handleGovernanceDeliveryGet(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	item, err := g.runtime.GetGovernanceDelivery(r.Context(), r.PathValue("id"))
	if err != nil {
		gwGovernanceError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, item)
}

func (g *Gateway) handleGovernanceDeliveriesStats(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	filter, err := gatewayGovernanceDeliveryFilter(r)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	stats, err := g.runtime.GetGovernanceDeliveryStats(r.Context(), filter)
	if err != nil {
		gwGovernanceError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, stats)
}

func (g *Gateway) handleGovernanceHealth(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	filter, err := gatewayGovernanceDeliveryFilter(r)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	health, err := g.runtime.GetGovernanceDeliveryHealth(r.Context(), filter)
	if err != nil {
		gwGovernanceError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, health)
}

func (g *Gateway) handleGovernanceDeliveryRedrive(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	req, ok := gatewayGovernanceRedriveRequest(w, r)
	if !ok {
		return
	}
	req.IDs = append(req.IDs, r.PathValue("id"))
	result, err := g.runtime.RedriveGovernanceDeliveries(r.Context(), req)
	if err != nil {
		gwGovernanceError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, result)
}

func (g *Gateway) handleGovernanceDeliveriesRedrive(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	req, ok := gatewayGovernanceRedriveRequest(w, r)
	if !ok {
		return
	}
	result, err := g.runtime.RedriveGovernanceDeliveries(r.Context(), req)
	if err != nil {
		gwGovernanceError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, result)
}

func (g *Gateway) handleGovernanceEvents(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	filter, err := gatewayGovernanceEventFilter(r)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	items := g.runtime.ListGovernanceEventViews(filter)
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: items, Count: len(items)})
}

func gatewayGovernanceDeliveryFilter(r *http.Request) (runtimepkg.GovernanceDeliveryFilter, error) {
	query := r.URL.Query()
	filter := runtimepkg.GovernanceDeliveryFilter{
		Status:      controlplane.GovernanceDeliveryStatus(strings.TrimSpace(query.Get("status"))),
		AdapterName: strings.TrimSpace(query.Get("adapter_name")),
		RunID:       strings.TrimSpace(query.Get("run_id")),
		SessionID:   strings.TrimSpace(query.Get("session_id")),
		EventType:   eventbus.EventType(strings.TrimSpace(query.Get("event_type"))),
		Kind:        controlplane.GovernanceKind(strings.TrimSpace(query.Get("kind"))),
		Query:       strings.TrimSpace(query.Get("q")),
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return runtimepkg.GovernanceDeliveryFilter{}, fmt.Errorf("invalid limit")
		}
		filter.Limit = limit
	}
	return filter, nil
}

func gatewayGovernanceEventFilter(r *http.Request) (runtimepkg.GovernanceEventFilter, error) {
	query := r.URL.Query()
	filter := runtimepkg.GovernanceEventFilter{
		Type:           eventbus.EventType(strings.TrimSpace(query.Get("type"))),
		RunID:          strings.TrimSpace(query.Get("run_id")),
		SessionID:      strings.TrimSpace(query.Get("session_id")),
		AdapterName:    strings.TrimSpace(query.Get("adapter_name")),
		DeliveryStatus: strings.TrimSpace(query.Get("delivery_status")),
		Severity:       strings.TrimSpace(query.Get("severity")),
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return runtimepkg.GovernanceEventFilter{}, fmt.Errorf("invalid limit")
		}
		filter.Limit = limit
	}
	return filter, nil
}

func gatewayGovernanceRedriveRequest(w http.ResponseWriter, r *http.Request) (runtimepkg.GovernanceRedriveRequest, bool) {
	var req runtimepkg.GovernanceRedriveRequest
	if _, ok := decodeOptionalGatewayJSONBody(w, r, &req); !ok {
		return runtimepkg.GovernanceRedriveRequest{}, false
	}
	return req, true
}

func gwGovernanceError(w http.ResponseWriter, err error) {
	gwError(w, gatewayHTTPStatusForError(err, http.StatusInternalServerError), err.Error())
}
