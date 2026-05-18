package gateway

import (
	"net/http"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/usage"
)

type usageSummaryResponse struct {
	Summary *usage.Summary `json:"summary"`
}

type sessionCostResponse struct {
	Session *usage.SessionCostSummary `json:"session"`
}

type dailyUsageResponse struct {
	Items []usage.DailyUsage `json:"items"`
	Count int                `json:"count"`
}

type providerUsageResponse struct {
	Providers map[string]*usage.ProviderUsage `json:"providers"`
	Count     int                             `json:"count"`
}

type operatorUsageSurface struct {
	store usage.Store
}

func newOperatorUsageSurface(store usage.Store) *operatorUsageSurface {
	return &operatorUsageSurface{store: store}
}

func (s *operatorUsageSurface) RegisterRoutes(mux *http.ServeMux, mountAuthed func(*http.ServeMux, string, func(http.ResponseWriter, *http.Request))) {
	if mux == nil || mountAuthed == nil {
		return
	}
	mountAuthed(mux, "GET /operator/usage/summary", s.handleUsageSummary)
	mountAuthed(mux, "GET /operator/usage/session/{session_id}", s.handleUsageSessionSummary)
	mountAuthed(mux, "GET /operator/usage/daily", s.handleUsageDailySummary)
	mountAuthed(mux, "GET /operator/usage/providers", s.handleUsageProviderSummary)
}

func (s *operatorUsageSurface) handleUsageSummary(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		gwError(w, http.StatusServiceUnavailable, "usage tracking not available")
		return
	}

	filter := operatorUsageQueryFilter(r)
	summary, err := s.store.Summarize(r.Context(), filter)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, usageSummaryResponse{Summary: summary})
}

func (s *operatorUsageSurface) handleUsageSessionSummary(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		gwError(w, http.StatusServiceUnavailable, "usage tracking not available")
		return
	}

	sessionID := r.PathValue("session_id")
	if strings.TrimSpace(sessionID) == "" {
		gwError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	summary, err := s.store.SessionSummary(r.Context(), sessionID)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, sessionCostResponse{Session: summary})
}

func (s *operatorUsageSurface) handleUsageDailySummary(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		gwError(w, http.StatusServiceUnavailable, "usage tracking not available")
		return
	}

	filter := operatorUsageQueryFilter(r)
	items, err := s.store.DailySummary(r.Context(), filter)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, dailyUsageResponse{Items: items, Count: len(items)})
}

func (s *operatorUsageSurface) handleUsageProviderSummary(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		gwError(w, http.StatusServiceUnavailable, "usage tracking not available")
		return
	}

	filter := operatorUsageQueryFilter(r)
	providers, err := s.store.ProviderSummary(r.Context(), filter)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, providerUsageResponse{Providers: providers, Count: len(providers)})
}

func operatorUsageQueryFilter(r *http.Request) usage.QueryFilter {
	query := r.URL.Query()
	filter := usage.QueryFilter{
		Model: strings.TrimSpace(query.Get("model")),
	}

	if since := strings.TrimSpace(query.Get("since")); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			filter.Since = t
		}
	}
	if until := strings.TrimSpace(query.Get("until")); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			filter.Until = t
		}
	}
	return filter
}
