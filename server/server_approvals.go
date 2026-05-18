package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/controlplane"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

const (
	approvalListDefaultLimit = 100
	approvalListMaxLimit     = 1000
)

func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	filter, err := parseApprovalListFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	scope, scopeErr := requestScopeFilter(r)
	if scopeErr != nil {
		writeError(w, http.StatusForbidden, scopeErr)
		return
	}
	tickets, err := s.runtime.ListApprovalViewsFiltered(r.Context(), filter, scope)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Items: tickets, Count: len(tickets)})
}

func (s *Server) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	ticket, err := s.runtime.GetApprovalViewScoped(r.Context(), r.PathValue("id"), scope)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ticket)
}

func (s *Server) handleResolveApproval(w http.ResponseWriter, r *http.Request) {
	var resolution approval.Resolution
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&resolution); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	ticket, err := s.runtime.ResolveApprovalViewScoped(r.Context(), r.PathValue("id"), scope, resolution)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ticket)
}

func (s *Server) handleResolveApprovalCallback(w http.ResponseWriter, r *http.Request) {
	if s.approvalCallbackRateLimiter != nil && !s.approvalCallbackRateLimiter.allow(approvalCallbackClientIP(r)) {
		writeMappedError(w, runtimesvc.ErrRateLimited)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var req controlplane.ApprovalResolveCallbackRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !s.authorizeApprovalCallback(w, r, req.Provider, body) {
		return
	}
	ticket, err := s.runtime.ResolveApprovalCallback(r.Context(), req)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ticket)
}

func (s *Server) handleSyncApprovals(w http.ResponseWriter, r *http.Request) {
	if err := s.runtime.SyncPendingApprovals(r.Context()); err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func firstQueryValue(q map[string][]string, key string) string {
	if q == nil {
		return ""
	}
	values := q[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func parseApprovalListFilter(q map[string][]string) (approval.ListFilter, error) {
	filter := approval.ListFilter{
		Status: approval.Status(strings.TrimSpace(firstQueryValue(q, "status"))),
		Limit:  approvalListDefaultLimit,
	}
	if raw := strings.TrimSpace(firstQueryValue(q, "limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return approval.ListFilter{}, fmt.Errorf("invalid limit %q", raw)
		}
		filter.Limit = n
	}
	if filter.Limit > approvalListMaxLimit {
		filter.Limit = approvalListMaxLimit
	}
	if raw := strings.TrimSpace(firstQueryValue(q, "offset")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return approval.ListFilter{}, fmt.Errorf("invalid offset %q", raw)
		}
		filter.Offset = n
	}
	return filter, nil
}
