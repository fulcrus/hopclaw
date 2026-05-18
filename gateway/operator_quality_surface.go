package gateway

import (
	"net/http"

	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
	"github.com/fulcrus/hopclaw/internal/qualityhttp"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
)

type operatorQualitySurface struct {
	runtime *runtimepkg.Service
}

func newOperatorQualitySurface(runtime *runtimepkg.Service) *operatorQualitySurface {
	return &operatorQualitySurface{runtime: runtime}
}

func (s *operatorQualitySurface) RegisterRoutes(mux *http.ServeMux, mountAuthed func(*http.ServeMux, string, func(http.ResponseWriter, *http.Request))) {
	if mux == nil || mountAuthed == nil {
		return
	}
	mountAuthed(mux, "GET /operator/quality/summary", s.handleQualitySummary)
	mountAuthed(mux, "GET /operator/quality/release-readiness", s.handleReleaseReadiness)
	mountAuthed(mux, "GET /operator/evals/suites", s.handleEvalSuites)
	mountAuthed(mux, "POST /operator/evals/run", s.handleEvalRun)
}

func (s *operatorQualitySurface) handleQualitySummary(w http.ResponseWriter, r *http.Request) {
	if s.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	req, err := operatorQualitySummaryRequest(r)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	summary, err := s.runtime.GetQualitySummary(r.Context(), req)
	if err != nil {
		writeOperatorQualityError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, summary)
}

func (s *operatorQualitySurface) handleReleaseReadiness(w http.ResponseWriter, r *http.Request) {
	if s.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	req, err := operatorReleaseReadinessRequest(r)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	report, err := s.runtime.GetReleaseReadiness(r.Context(), req)
	if err != nil {
		writeOperatorQualityError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, report)
}

func (s *operatorQualitySurface) handleEvalSuites(w http.ResponseWriter, _ *http.Request) {
	if s.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	items := s.runtime.ListEvalSuites()
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: items, Count: len(items)})
}

func (s *operatorQualitySurface) handleEvalRun(w http.ResponseWriter, r *http.Request) {
	if s.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	var req runtimepkg.EvalRunRequest
	if err := qualityhttp.DecodeEvalRunRequest(http.MaxBytesReader(w, r.Body, configMaxBodySize), &req); err != nil {
		if isRequestBodyTooLarge(err) {
			gwErrorCode(w, http.StatusRequestEntityTooLarge, apiresponse.ErrorCodeRequestBodyTooLarge, "request body too large")
			return
		}
		gwErrorCode(w, http.StatusBadRequest, apiresponse.ErrorCodeInvalidJSON, "invalid json: "+err.Error())
		return
	}
	report, err := s.runtime.RunEvalSuite(r.Context(), req)
	if err != nil {
		writeOperatorQualityError(w, err)
		return
	}
	gwJSON(w, http.StatusOK, report)
}

func operatorQualitySummaryRequest(r *http.Request) (runtimepkg.QualitySummaryRequest, error) {
	authScope := authScopeFromIdentity(AuthIdentityFromContext(r.Context()))
	scopeFilter, err := requestScopeFilterWithAuthScope(authScope)
	if err != nil {
		return runtimepkg.QualitySummaryRequest{}, err
	}
	return qualityhttp.ParseQualitySummaryRequest(r.URL.Query(), scopeFilter)
}

func operatorReleaseReadinessRequest(r *http.Request) (runtimepkg.ReleaseReadinessRequest, error) {
	authScope := authScopeFromIdentity(AuthIdentityFromContext(r.Context()))
	scopeFilter, err := requestScopeFilterWithAuthScope(authScope)
	if err != nil {
		return runtimepkg.ReleaseReadinessRequest{}, err
	}
	return qualityhttp.ParseReleaseReadinessRequest(r.URL.Query(), scopeFilter)
}

func writeOperatorQualityError(w http.ResponseWriter, err error) {
	gwError(w, gatewayHTTPStatusForError(err, http.StatusInternalServerError), err.Error())
}
