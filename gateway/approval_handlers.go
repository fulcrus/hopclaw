package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/controlplane"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
)

const (
	gatewayApprovalListDefaultLimit = 100
	gatewayApprovalListMaxLimit     = 1000
)

func (g *Gateway) handleApprovalsList(w http.ResponseWriter, r *http.Request) {
	if g.approvals == nil {
		gwError(w, http.StatusServiceUnavailable, "approvals not available")
		return
	}
	authScope := authScopeFromIdentity(AuthIdentityFromContext(r.Context()))
	filter, err := parseGatewayApprovalListFilter(r)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	scopeFilter, err := requestScopeFilterWithAuthScope(authScope)
	if err != nil {
		gwError(w, http.StatusForbidden, err.Error())
		return
	}
	storeFilter := filter
	if hasGatewayApprovalPostFilters(r) || !authScope.isZero() {
		storeFilter.Limit = 0
		storeFilter.Offset = 0
	}
	views, err := g.listApprovalViews(r, storeFilter, scopeFilter)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	views = g.filterApprovalViews(r, authScope, views)
	if hasGatewayApprovalPostFilters(r) || !authScope.isZero() {
		views = paginateApprovalViews(views, filter.Offset, filter.Limit)
	}
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: views, Count: len(views)})
}

type approvalProvidersResponse struct {
	Items []controlplane.ApprovalProviderSummary `json:"items"`
	Count int                                    `json:"count"`
}

func (g *Gateway) handleApprovalProvidersList(w http.ResponseWriter, _ *http.Request) {
	if g.approvalProviders == nil {
		gwJSON(w, http.StatusOK, approvalProvidersResponse{Items: []controlplane.ApprovalProviderSummary{}, Count: 0})
		return
	}
	items := g.approvalProviders.Describe()
	if items == nil {
		items = []controlplane.ApprovalProviderSummary{}
	}
	gwJSON(w, http.StatusOK, approvalProvidersResponse{Items: items, Count: len(items)})
}

func (g *Gateway) listApprovalViews(r *http.Request, filter approval.ListFilter, scopeFilter agent.ScopeFilter) ([]*runtimepkg.ApprovalView, error) {
	if g.runtime != nil {
		views, err := g.runtime.ListApprovalViewsFiltered(r.Context(), filter, scopeFilter)
		if err == nil {
			return views, nil
		}
		if !errors.Is(err, agent.ErrApprovalStoreNil) {
			return nil, err
		}
	}
	tickets, err := g.approvals.List(r.Context(), filter)
	if err != nil {
		return nil, err
	}
	views := make([]*runtimepkg.ApprovalView, 0, len(tickets))
	for _, ticket := range tickets {
		view, err := g.approvalView(r.Context(), ticket)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (g *Gateway) filterApprovalViews(r *http.Request, authScope authScope, views []*runtimepkg.ApprovalView) []*runtimepkg.ApprovalView {
	if len(views) == 0 {
		return views
	}
	query := r.URL.Query()
	runID := strings.TrimSpace(query.Get("run_id"))
	sessionID := strings.TrimSpace(query.Get("session_id"))
	scopeFilter := authScope.scopeFilter()
	if runID == "" && sessionID == "" && authScope.isZero() {
		return views
	}
	out := make([]*runtimepkg.ApprovalView, 0, len(views))
	for _, view := range views {
		if view == nil {
			continue
		}
		ticket := &view.Ticket
		if runID != "" && strings.TrimSpace(ticket.RunID) != runID {
			continue
		}
		if sessionID != "" && strings.TrimSpace(ticket.SessionID) != sessionID {
			continue
		}
		if !authScope.isZero() {
			if !scopeFilter.Matches(g.approvalScope(r, ticket, scopeFilter)) {
				continue
			}
		}
		out = append(out, view)
	}
	return out
}

func (g *Gateway) approvalScope(r *http.Request, ticket *approval.Ticket, scopeFilter agent.ScopeFilter) domainscope.Ref {
	if ticket == nil {
		return domainscope.Ref{}
	}
	if ticket.Metadata != nil {
		if scope := domainscope.FromValue(ticket.Metadata["scope"]); !scope.IsZero() {
			return scope
		}
	}
	if g.runtime == nil || strings.TrimSpace(ticket.RunID) == "" {
		return domainscope.Ref{}
	}
	var (
		run *agent.Run
		err error
	)
	if scopeFilter.IsZero() {
		run, err = g.runtime.GetRun(r.Context(), ticket.RunID)
	} else {
		run, err = g.runtime.GetRunScoped(r.Context(), ticket.RunID, scopeFilter)
	}
	if err != nil || run == nil {
		return domainscope.Ref{}
	}
	return run.Scope.Normalize()
}

func parseGatewayApprovalListFilter(r *http.Request) (approval.ListFilter, error) {
	filter := approval.ListFilter{
		Status: approval.Status(strings.TrimSpace(r.URL.Query().Get("status"))),
		Limit:  gatewayApprovalListDefaultLimit,
	}
	if filter.Status == "" {
		filter.Status = approval.StatusPending
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return approval.ListFilter{}, fmt.Errorf("invalid limit %q", raw)
		}
		filter.Limit = n
	}
	if filter.Limit > gatewayApprovalListMaxLimit {
		filter.Limit = gatewayApprovalListMaxLimit
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return approval.ListFilter{}, fmt.Errorf("invalid offset %q", raw)
		}
		filter.Offset = n
	}
	return filter, nil
}

func hasGatewayApprovalPostFilters(r *http.Request) bool {
	q := r.URL.Query()
	return strings.TrimSpace(q.Get("run_id")) != "" ||
		strings.TrimSpace(q.Get("session_id")) != ""
}

func paginateApprovalViews(views []*runtimepkg.ApprovalView, offset, limit int) []*runtimepkg.ApprovalView {
	if offset > 0 {
		if offset >= len(views) {
			return nil
		}
		views = views[offset:]
	}
	if limit > 0 && len(views) > limit {
		views = views[:limit]
	}
	return views
}

type resolveApprovalRequest struct {
	Status   string `json:"status"`             // "approved" or "denied"
	Decision string `json:"decision,omitempty"` // legacy alias: "approve" or "deny"
	Scope    string `json:"scope,omitempty"`
	Note     string `json:"note,omitempty"`
	By       string `json:"by,omitempty"`
}

func (g *Gateway) handleApprovalsResolve(w http.ResponseWriter, r *http.Request) {
	if g.approvals == nil {
		gwError(w, http.StatusServiceUnavailable, "approvals not available")
		return
	}
	ticketID := r.PathValue("id")
	if strings.TrimSpace(ticketID) == "" {
		gwError(w, http.StatusBadRequest, "missing ticket id")
		return
	}

	var req resolveApprovalRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}

	resolveStatus := approval.Status(strings.TrimSpace(req.Status))
	if resolveStatus == "" {
		switch strings.ToLower(strings.TrimSpace(req.Decision)) {
		case "approve", "approved":
			resolveStatus = approval.StatusApproved
		case "deny", "denied":
			resolveStatus = approval.StatusDenied
		}
	}
	switch resolveStatus {
	case approval.StatusApproved, approval.StatusDenied:
	default:
		gwError(w, http.StatusBadRequest, "status must be approved or denied")
		return
	}

	resolution := approval.Resolution{
		Status:     resolveStatus,
		ResolvedBy: strings.TrimSpace(req.By),
		Note:       strings.TrimSpace(req.Note),
		Scope:      approval.Scope(strings.TrimSpace(req.Scope)),
	}

	authScope := authScopeFromIdentity(AuthIdentityFromContext(r.Context()))
	scopeFilter := authScope.scopeFilter()
	if _, err := g.getApprovalScoped(r, ticketID, authScope); err != nil {
		g.writeApprovalError(w, err)
		return
	}

	var view *runtimepkg.ApprovalView
	var err error
	if g.runtime != nil {
		view, err = g.runtime.ResolveApprovalViewScoped(r.Context(), ticketID, scopeFilter, resolution)
		if err == nil && view != nil {
		} else if !errors.Is(err, agent.ErrApprovalStoreNil) {
			g.writeApprovalError(w, err)
			return
		} else {
			view, err = g.resolveApprovalViewFallback(r.Context(), ticketID, resolution)
		}
	} else {
		view, err = g.resolveApprovalViewFallback(r.Context(), ticketID, resolution)
	}
	if err != nil {
		g.writeApprovalError(w, err)
		return
	}
	if view == nil {
		g.writeApprovalError(w, approval.ErrNotFound)
		return
	}

	gwJSON(w, http.StatusOK, ticketOKResponse{OK: true, Ticket: view})
}

func (g *Gateway) resolveApprovalViewFallback(ctx context.Context, ticketID string, resolution approval.Resolution) (*runtimepkg.ApprovalView, error) {
	ticket, err := g.approvals.Resolve(ctx, ticketID, resolution)
	if err != nil {
		return nil, err
	}
	return g.approvalView(ctx, ticket)
}

func (g *Gateway) approvalView(ctx context.Context, ticket *approval.Ticket) (*runtimepkg.ApprovalView, error) {
	if ticket == nil {
		return nil, nil
	}
	if g.runtime != nil {
		view, err := g.runtime.BuildApprovalView(ctx, ticket)
		if err == nil && view != nil {
			return view, nil
		}
		if err != nil && !errors.Is(err, agent.ErrApprovalStoreNil) {
			return nil, err
		}
	}
	return &runtimepkg.ApprovalView{Ticket: *ticket}, nil
}

func (g *Gateway) handleApprovalsCancel(w http.ResponseWriter, r *http.Request) {
	if g.approvals == nil {
		gwError(w, http.StatusServiceUnavailable, "approvals not available")
		return
	}
	ticketID := r.PathValue("id")
	if strings.TrimSpace(ticketID) == "" {
		gwError(w, http.StatusBadRequest, "missing ticket id")
		return
	}

	resolution := approval.Resolution{
		Status:     approval.StatusCancelled,
		ResolvedBy: "operator",
		Note:       "cancelled via API",
	}
	authScope := authScopeFromIdentity(AuthIdentityFromContext(r.Context()))
	scopeFilter := authScope.scopeFilter()
	if _, err := g.getApprovalScoped(r, ticketID, authScope); err != nil {
		g.writeApprovalError(w, err)
		return
	}
	var view *runtimepkg.ApprovalView
	var err error
	if g.runtime != nil {
		view, err = g.runtime.ResolveApprovalViewScoped(r.Context(), ticketID, scopeFilter, resolution)
		if err == nil && view != nil {
		} else if !errors.Is(err, agent.ErrApprovalStoreNil) {
			g.writeApprovalError(w, err)
			return
		} else {
			view, err = g.resolveApprovalViewFallback(r.Context(), ticketID, resolution)
		}
	} else {
		view, err = g.resolveApprovalViewFallback(r.Context(), ticketID, resolution)
	}
	if err != nil {
		g.writeApprovalError(w, err)
		return
	}
	if view == nil {
		g.writeApprovalError(w, approval.ErrNotFound)
		return
	}
	gwJSON(w, http.StatusOK, ticketOKResponse{OK: true, Ticket: view})
}

func (g *Gateway) getApprovalScoped(r *http.Request, ticketID string, scope authScope) (*approval.Ticket, error) {
	if g.approvals == nil {
		return nil, agent.ErrApprovalStoreNil
	}
	if g.runtime != nil {
		ticket, err := g.runtime.GetApprovalScoped(r.Context(), ticketID, scope.scopeFilter())
		if err == nil {
			if ticket == nil {
				return nil, approval.ErrNotFound
			}
			return ticket, nil
		}
		if !errors.Is(err, agent.ErrApprovalStoreNil) {
			return nil, approval.ErrNotFound
		}
	}
	ticket, err := g.approvals.Get(r.Context(), ticketID)
	if err != nil {
		return nil, err
	}
	if !scope.scopeFilter().Matches(g.approvalScope(r, ticket, scope.scopeFilter())) {
		return nil, approval.ErrNotFound
	}
	return ticket, nil
}

func (g *Gateway) writeApprovalError(w http.ResponseWriter, err error) {
	gwError(w, gatewayHTTPStatusForError(err, http.StatusInternalServerError), err.Error())
}
