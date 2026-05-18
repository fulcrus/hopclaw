package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func (s *Server) handleListGovernanceDeliveries(w http.ResponseWriter, r *http.Request) {
	filter, err := parseGovernanceDeliveryFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.runtime.ListGovernanceDeliveries(r.Context(), filter)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, countedListResponse{Items: items, Count: len(items)})
}

func (s *Server) handleGetGovernanceDelivery(w http.ResponseWriter, r *http.Request) {
	item, err := s.runtime.GetGovernanceDelivery(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleGetGovernanceDeliveryStats(w http.ResponseWriter, r *http.Request) {
	filter, err := parseGovernanceDeliveryFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	stats, err := s.runtime.GetGovernanceDeliveryStats(r.Context(), filter)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleGetGovernanceDeliveryHealth(w http.ResponseWriter, r *http.Request) {
	filter, err := parseGovernanceDeliveryFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	health, err := s.runtime.GetGovernanceDeliveryHealth(r.Context(), filter)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, health)
}

func (s *Server) handleRedriveGovernanceDelivery(w http.ResponseWriter, r *http.Request) {
	req, err := parseGovernanceRedriveRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req.IDs = append(req.IDs, r.PathValue("id"))
	result, err := s.runtime.RedriveGovernanceDeliveries(r.Context(), req)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRedriveGovernanceDeliveries(w http.ResponseWriter, r *http.Request) {
	req, err := parseGovernanceRedriveRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.runtime.RedriveGovernanceDeliveries(r.Context(), req)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleListGovernanceEvents(w http.ResponseWriter, r *http.Request) {
	filter, err := parseGovernanceEventFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items := s.runtime.ListGovernanceEventViews(filter)
	writeJSON(w, http.StatusOK, countedListResponse{Items: items, Count: len(items)})
}

func parseGovernanceDeliveryFilter(r *http.Request) (runtimesvc.GovernanceDeliveryFilter, error) {
	query := r.URL.Query()
	filter := runtimesvc.GovernanceDeliveryFilter{
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
			return runtimesvc.GovernanceDeliveryFilter{}, fmt.Errorf("invalid limit")
		}
		filter.Limit = limit
	}
	return filter, nil
}

func parseGovernanceEventFilter(r *http.Request) (runtimesvc.GovernanceEventFilter, error) {
	query := r.URL.Query()
	filter := runtimesvc.GovernanceEventFilter{
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
			return runtimesvc.GovernanceEventFilter{}, fmt.Errorf("invalid limit")
		}
		filter.Limit = limit
	}
	return filter, nil
}

func parseGovernanceRedriveRequest(r *http.Request) (runtimesvc.GovernanceRedriveRequest, error) {
	var req runtimesvc.GovernanceRedriveRequest
	if r.Body == nil || r.ContentLength == 0 {
		return req, nil
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		return runtimesvc.GovernanceRedriveRequest{}, fmt.Errorf("invalid request body")
	}
	return req, nil
}
