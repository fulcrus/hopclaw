package server

import (
	"io"
	"net/http"

	"github.com/fulcrus/hopclaw/internal/qualityhttp"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func (s *Server) handleQualitySummary(w http.ResponseWriter, r *http.Request) {
	req, err := parseQualitySummaryRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	summary, err := s.runtime.GetQualitySummary(r.Context(), req)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleReleaseReadiness(w http.ResponseWriter, r *http.Request) {
	req, err := parseReleaseReadinessRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	report, err := s.runtime.GetReleaseReadiness(r.Context(), req)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleEvalSuites(w http.ResponseWriter, _ *http.Request) {
	items := s.runtime.ListEvalSuites()
	writeJSON(w, http.StatusOK, countedListResponse{Items: items, Count: len(items)})
}

func (s *Server) handleEvalRun(w http.ResponseWriter, r *http.Request) {
	var req runtimesvc.EvalRunRequest
	if err := qualityhttp.DecodeEvalRunRequest(io.LimitReader(r.Body, 1<<20), &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := applyEvalRunAuthScope(r, &req); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	report, err := s.runtime.RunEvalSuite(r.Context(), req)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func parseQualitySummaryRequest(r *http.Request) (runtimesvc.QualitySummaryRequest, error) {
	scope, err := requestScopeFilter(r)
	if err != nil {
		return runtimesvc.QualitySummaryRequest{}, err
	}
	return qualityhttp.ParseQualitySummaryRequest(r.URL.Query(), scope)
}

func parseReleaseReadinessRequest(r *http.Request) (runtimesvc.ReleaseReadinessRequest, error) {
	scope, err := requestScopeFilter(r)
	if err != nil {
		return runtimesvc.ReleaseReadinessRequest{}, err
	}
	return qualityhttp.ParseReleaseReadinessRequest(r.URL.Query(), scope)
}

func applyEvalRunAuthScope(r *http.Request, req *runtimesvc.EvalRunRequest) error {
	if req == nil {
		return nil
	}
	return applyAutomationIDAuthScope(r, &req.AutomationID)
}
