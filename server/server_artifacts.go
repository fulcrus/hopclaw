package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/logging"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func (s *Server) handleGetArtifact(w http.ResponseWriter, r *http.Request) {
	blob, err := s.runtime.GetArtifact(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, blob)
}

func (s *Server) handleReadArtifact(w http.ResponseWriter, r *http.Request) {
	body, contentType, err := s.runtime.ReadArtifact(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMappedError(w, err)
		return
	}
	contentType = artifact.PreviewMediaType(contentType)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", artifact.PreviewDisposition(contentType))
	w.Header().Set("Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data: blob:; style-src 'unsafe-inline'; sandbox")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		logging.FromContext(r.Context()).Warn("write http response body failed", "error", err)
	}
}

func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	filter, err := parseArtifactFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.runtime.ListArtifacts(r.Context(), filter)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Items: items, Count: len(items)})
}

func (s *Server) handlePruneArtifacts(w http.ResponseWriter, r *http.Request) {
	var payload artifactPrunePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req, err := payload.toRequest()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.runtime.PruneArtifacts(r.Context(), req)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type artifactPrunePayload struct {
	Kind       string `json:"kind"`
	RunID      string `json:"run_id"`
	SessionID  string `json:"session_id"`
	ToolName   string `json:"tool_name"`
	ToolCallID string `json:"tool_call_id"`
	Before     string `json:"before"`
	Retention  string `json:"retention"`
	Limit      int    `json:"limit"`
}

func (p artifactPrunePayload) toRequest() (runtimesvc.ArtifactPruneRequest, error) {
	filter := artifact.ListFilter{
		Kind:       strings.TrimSpace(p.Kind),
		RunID:      strings.TrimSpace(p.RunID),
		SessionID:  strings.TrimSpace(p.SessionID),
		ToolName:   strings.TrimSpace(p.ToolName),
		ToolCallID: strings.TrimSpace(p.ToolCallID),
		Limit:      p.Limit,
	}
	if strings.TrimSpace(p.Before) != "" {
		before, err := time.Parse(time.RFC3339, strings.TrimSpace(p.Before))
		if err != nil {
			return runtimesvc.ArtifactPruneRequest{}, fmt.Errorf("invalid before: %w", err)
		}
		filter.Before = before
	}
	var retention time.Duration
	if strings.TrimSpace(p.Retention) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(p.Retention))
		if err != nil {
			return runtimesvc.ArtifactPruneRequest{}, fmt.Errorf("invalid retention: %w", err)
		}
		retention = parsed
	}
	return runtimesvc.ArtifactPruneRequest{
		Filter:    filter,
		Retention: retention,
	}, nil
}

func parseArtifactFilter(r *http.Request) (artifact.ListFilter, error) {
	query := r.URL.Query()
	filter := artifact.ListFilter{
		Kind:       strings.TrimSpace(query.Get("kind")),
		RunID:      strings.TrimSpace(query.Get("run_id")),
		SessionID:  strings.TrimSpace(query.Get("session_id")),
		ToolName:   strings.TrimSpace(query.Get("tool_name")),
		ToolCallID: strings.TrimSpace(query.Get("tool_call_id")),
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return artifact.ListFilter{}, fmt.Errorf("invalid limit %q", raw)
		}
		filter.Limit = limit
	}
	if raw := strings.TrimSpace(query.Get("before")); raw != "" {
		before, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return artifact.ListFilter{}, fmt.Errorf("invalid before %q", raw)
		}
		filter.Before = before
	}
	return filter, nil
}
