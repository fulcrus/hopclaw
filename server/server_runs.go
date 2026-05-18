package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func (s *Server) handleSubmitRun(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, fmt.Errorf("content-type must be application/json"))
		return
	}
	var req runtimesvc.SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Content) == "" && strings.TrimSpace(req.Input) != "" {
		req.Content = req.Input
	}
	if strings.TrimSpace(req.Content) == "" && len(req.ContentBlocks) == 0 && len(req.Images) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("content, content_blocks, or images are required"))
		return
	}
	if err := applySubmitAuthScope(r, &req); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	run, err := s.runtime.Submit(r.Context(), req)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	status := http.StatusAccepted
	if req.Execute != nil && !*req.Execute {
		status = http.StatusCreated
	}
	writeJSON(w, status, run)
}

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	tools, err := s.runtime.ListTools(r.Context(), strings.TrimSpace(r.URL.Query().Get("session_key")))
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Items: tools, Count: len(tools)})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	run, err := s.runtime.GetRunScoped(r.Context(), r.PathValue("id"), scope)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	view := s.runtime.BuildRunViews(r.Context(), []*agent.Run{run}, runtimesvc.RunListViewOptions{
		IncludeExecutionGraph: true,
	})
	if len(view) == 0 || view[0] == nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("run %s view unavailable", r.PathValue("id")))
		return
	}
	writeJSON(w, http.StatusOK, view[0])
}

func (s *Server) handleGetRunResult(w http.ResponseWriter, r *http.Request) {
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if _, err := s.runtime.GetRunScoped(r.Context(), r.PathValue("id"), scope); err != nil {
		writeMappedError(w, err)
		return
	}
	result, err := s.runtime.GetRunResult(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetRunCompletion(w http.ResponseWriter, r *http.Request) {
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if _, err := s.runtime.GetRunScoped(r.Context(), r.PathValue("id"), scope); err != nil {
		writeMappedError(w, err)
		return
	}
	result, err := s.runtime.GetRunCompletion(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetRunVerification(w http.ResponseWriter, r *http.Request) {
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if _, err := s.runtime.GetRunScoped(r.Context(), r.PathValue("id"), scope); err != nil {
		writeMappedError(w, err)
		return
	}
	result, err := s.runtime.GetRunVerification(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleResumeRun(w http.ResponseWriter, r *http.Request) {
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if _, err := s.runtime.GetRunScoped(r.Context(), r.PathValue("id"), scope); err != nil {
		writeMappedError(w, err)
		return
	}
	run, err := s.runtime.ResumeRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	filter := agent.RunListFilter{
		SessionID: strings.TrimSpace(q.Get("session_id")),
		Status:    agent.RunStatus(strings.TrimSpace(q.Get("status"))),
		Scope:     scope,
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid limit %q", raw))
			return
		}
		filter.Limit = n
	}
	items, err := s.runtime.ListRunViews(r.Context(), filter, runtimesvc.RunListViewOptions{
		IncludeVerification:   queryIncludes(q.Get("include"), "verification"),
		IncludeExecutionGraph: queryIncludes(q.Get("include"), "execution_graph"),
	})
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, countedListResponse{Items: items, Count: len(items)})
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if scope, err := requestScopeFilter(r); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	} else if _, err := s.runtime.GetRunScoped(r.Context(), id, scope); err != nil {
		writeMappedError(w, err)
		return
	}
	run, err := s.runtime.CancelRun(r.Context(), id)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func queryIncludes(raw string, want string) bool {
	want = strings.TrimSpace(strings.ToLower(want))
	if want == "" {
		return false
	}
	for _, part := range strings.Split(raw, ",") {
		if strings.TrimSpace(strings.ToLower(part)) == want {
			return true
		}
	}
	return false
}
