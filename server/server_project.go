package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
)

type projectRenameRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.runtime.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if projects == nil {
		projects = make([]agent.Project, 0)
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	project, err := s.runtime.FindProjectByName(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("project %q not found", name))
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleRenameProject(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req projectRenameRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("project name is required"))
		return
	}
	if err := s.runtime.RenameProject(r.Context(), name, req.Name); err != nil {
		writeMappedError(w, err)
		return
	}
	project, err := s.runtime.FindProjectByName(r.Context(), req.Name)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("project %q not found", name))
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.runtime.DeleteProject(r.Context(), name); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{OK: true})
}
