package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/durablefact"
)

type memorySetRequest struct {
	Value string `json:"value"`
}

type memoryRecordRequest struct {
	Key             string                `json:"key,omitempty"`
	FactClass       durablefact.FactClass `json:"fact_class,omitempty"`
	Namespace       string                `json:"namespace,omitempty"`
	ScopeKey        string                `json:"scope_key,omitempty"`
	Field           string                `json:"field,omitempty"`
	Label           string                `json:"label,omitempty"`
	Value           string                `json:"value"`
	Source          string                `json:"source,omitempty"`
	Score           float64               `json:"score,omitempty"`
	State           agent.MemoryState     `json:"state,omitempty"`
	SupersededBy    string                `json:"superseded_by,omitempty"`
	SessionKey      string                `json:"session_key,omitempty"`
	ProjectID       string                `json:"project_id,omitempty"`
	MediaRefs       []string              `json:"media_refs,omitempty"`
	UsedCount       int                   `json:"used_count,omitempty"`
	LastUsedAt      time.Time             `json:"last_used_at,omitempty"`
	CorrectionCount int                   `json:"correction_count,omitempty"`
	Tags            []string              `json:"tags,omitempty"`
}

type memoryIndexRequest struct {
	Force bool `json:"force"`
}

type memoryStatusResponse struct {
	StoreType  string `json:"store_type"`
	EntryCount int    `json:"entry_count"`
	IndexReady bool   `json:"index_ready"`
}

type memoryIndexResponse struct {
	Status  string `json:"status"`
	Indexed int    `json:"indexed"`
}

func (s *Server) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	entry, err := s.runtime.GetMemory(r.Context(), key)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("memory key %q not found", key))
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleSetMemory(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var req memorySetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.runtime.SetMemory(r.Context(), key, req.Value); err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{OK: true})
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if err := s.runtime.DeleteMemory(r.Context(), key); err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{OK: true})
}

func (s *Server) handleUpsertMemoryRecord(w http.ResponseWriter, r *http.Request) {
	var req memoryRecordRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	entry, err := s.runtime.UpsertMemoryRecord(r.Context(), agent.MemoryRecord{
		Key:             req.Key,
		FactClass:       req.FactClass,
		Namespace:       req.Namespace,
		ScopeKey:        req.ScopeKey,
		Field:           req.Field,
		Label:           req.Label,
		Value:           req.Value,
		Source:          req.Source,
		Score:           req.Score,
		State:           req.State,
		SupersededBy:    req.SupersededBy,
		SessionKey:      req.SessionKey,
		ProjectID:       req.ProjectID,
		MediaRefs:       req.MediaRefs,
		UsedCount:       req.UsedCount,
		LastUsedAt:      req.LastUsedAt,
		CorrectionCount: req.CorrectionCount,
		Tags:            req.Tags,
	})
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleGetMemoryNotebook(w http.ResponseWriter, r *http.Request) {
	notebook, err := s.runtime.GetMemoryNotebook(r.Context())
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, notebook)
}

func (s *Server) handleSearchMemory(w http.ResponseWriter, r *http.Request) {
	filter := agent.MemoryFilter{
		Query:     strings.TrimSpace(r.URL.Query().Get("q")),
		Namespace: strings.TrimSpace(r.URL.Query().Get("namespace")),
		ScopeKey:  strings.TrimSpace(r.URL.Query().Get("scope_key")),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("managed_only")); raw != "" {
		managedOnly, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid managed_only %q", raw))
			return
		}
		filter.ManagedOnly = managedOnly
	}
	var (
		results []agent.MemoryEntry
		err     error
	)
	if filter.Query != "" && filter.Namespace == "" && filter.ScopeKey == "" && !filter.ManagedOnly {
		results, err = s.runtime.SearchMemory(r.Context(), filter.Query)
	} else {
		results, err = s.runtime.ListMemoryFiltered(r.Context(), filter)
	}
	if err != nil {
		writeMappedError(w, err)
		return
	}
	if results == nil {
		results = make([]agent.MemoryEntry, 0)
	}
	writeJSON(w, http.StatusOK, countedListResponse{Items: results, Count: len(results)})
}

func (s *Server) handleGetMemoryStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.runtime.GetMemoryStatus(r.Context())
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, memoryStatusResponse{
		StoreType:  status.StoreType,
		EntryCount: status.EntryCount,
		IndexReady: status.IndexReady,
	})
}

func (s *Server) handleReindexMemory(w http.ResponseWriter, r *http.Request) {
	var req memoryIndexRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.runtime.ReindexMemory(r.Context(), req.Force)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, memoryIndexResponse{
		Status:  result.Status,
		Indexed: result.Indexed,
	})
}
