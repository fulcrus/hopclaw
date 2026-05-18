package gateway

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/audit"
	"github.com/fulcrus/hopclaw/eventbus"
)

func (g *Gateway) handleAuditDeliveriesList(w http.ResponseWriter, r *http.Request) {
	if g.auditDelivery == nil {
		gwError(w, http.StatusServiceUnavailable, "audit delivery controller not available")
		return
	}
	filter, err := gatewayAuditDeliveryFilter(r)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := g.auditDelivery.ListDeliveries(r.Context(), filter)
	if err != nil {
		gwAuditDeliveryError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: items, Count: len(items)})
}

func (g *Gateway) handleAuditDeliveryGet(w http.ResponseWriter, r *http.Request) {
	if g.auditDelivery == nil {
		gwError(w, http.StatusServiceUnavailable, "audit delivery controller not available")
		return
	}
	item, err := g.auditDelivery.GetDelivery(r.Context(), r.PathValue("id"))
	if err != nil {
		gwAuditDeliveryError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, item)
}

func (g *Gateway) handleAuditDeliveriesStats(w http.ResponseWriter, r *http.Request) {
	if g.auditDelivery == nil {
		gwError(w, http.StatusServiceUnavailable, "audit delivery controller not available")
		return
	}
	filter, err := gatewayAuditDeliveryFilter(r)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	stats, err := g.auditDelivery.GetDeliveryStats(r.Context(), filter)
	if err != nil {
		gwAuditDeliveryError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, stats)
}

func (g *Gateway) handleAuditDeliveryRedrive(w http.ResponseWriter, r *http.Request) {
	if g.auditDelivery == nil {
		gwError(w, http.StatusServiceUnavailable, "audit delivery controller not available")
		return
	}
	req, ok := gatewayAuditDeliveryRedriveRequest(w, r)
	if !ok {
		return
	}
	req.IDs = append(req.IDs, r.PathValue("id"))
	items, err := g.auditDelivery.Redrive(r.Context(), req.IDs, req.Options)
	if err != nil {
		gwAuditDeliveryError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: items, Count: len(items)})
}

func (g *Gateway) handleAuditDeliveriesRedrive(w http.ResponseWriter, r *http.Request) {
	if g.auditDelivery == nil {
		gwError(w, http.StatusServiceUnavailable, "audit delivery controller not available")
		return
	}
	req, ok := gatewayAuditDeliveryRedriveRequest(w, r)
	if !ok {
		return
	}
	items, err := g.auditDelivery.Redrive(r.Context(), req.IDs, req.Options)
	if err != nil {
		gwAuditDeliveryError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: items, Count: len(items)})
}

type gatewayAuditDeliveryRedrivePayload struct {
	IDs     []string `json:"ids,omitempty"`
	Options audit.DeliveryRedriveOptions
}

func gatewayAuditDeliveryFilter(r *http.Request) (audit.DeliveryListFilter, error) {
	query := r.URL.Query()
	filter := audit.DeliveryListFilter{
		Status:    audit.DeliveryStatus(strings.TrimSpace(query.Get("status"))),
		SinkName:  strings.TrimSpace(query.Get("sink_name")),
		RunID:     strings.TrimSpace(query.Get("run_id")),
		SessionID: strings.TrimSpace(query.Get("session_id")),
		EventType: eventbus.EventType(strings.TrimSpace(query.Get("event_type"))),
		Query:     strings.TrimSpace(query.Get("q")),
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return audit.DeliveryListFilter{}, fmt.Errorf("invalid limit")
		}
		filter.Limit = limit
	}
	return filter, nil
}

func gatewayAuditDeliveryRedriveRequest(w http.ResponseWriter, r *http.Request) (gatewayAuditDeliveryRedrivePayload, bool) {
	var req gatewayAuditDeliveryRedrivePayload
	if _, ok := decodeOptionalGatewayJSONBody(w, r, &req); !ok {
		return gatewayAuditDeliveryRedrivePayload{}, false
	}
	return req, true
}

func gwAuditDeliveryError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, audit.ErrDeliveryNotFound) {
		status = http.StatusNotFound
	}
	gwError(w, status, err.Error())
}
