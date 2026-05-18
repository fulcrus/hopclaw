package server

import (
	"fmt"
	"net/http"
)

func (s *Server) handleGetEffectiveConfig(w http.ResponseWriter, r *http.Request) {
	snapshot := s.runtime.EffectiveConfigSnapshot()
	if snapshot == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("effective config snapshot not configured"))
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleGetRunGovernance(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.runtime.GetGovernanceSnapshot(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}
