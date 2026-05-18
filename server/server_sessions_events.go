package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	sessions, err := s.runtime.ListSessionsFiltered(r.Context(), agent.SessionListFilter{
		Scope: scope,
	})
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, countedListResponse{Items: sessions, Count: len(sessions)})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("include")) != "messages" {
		summary, err := s.runtime.GetSessionSummaryScoped(r.Context(), id, scope)
		if err != nil {
			writeMappedError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, summary)
		return
	}
	sess, err := s.runtime.GetSessionScoped(r.Context(), id, scope)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleGetSessionMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	sess, err := s.runtime.GetSessionScoped(r.Context(), id, scope)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	messages := sess.Messages
	if messages == nil {
		messages = []contextengine.Message{}
	}
	writeJSON(w, http.StatusOK, messages)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if scope, err := requestScopeFilter(r); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	} else if _, err := s.runtime.GetSessionSummaryScoped(r.Context(), id, scope); err != nil {
		writeMappedError(w, err)
		return
	}
	if err := s.runtime.DeleteSession(r.Context(), id); err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{OK: true})
}

func (s *Server) handleCompactSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if _, err := s.runtime.GetSessionSummaryScoped(r.Context(), id, scope); err != nil {
		writeMappedError(w, err)
		return
	}
	session, err := s.runtime.CompactSession(r.Context(), id)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleStartSessionEpisode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	scope, err := requestScopeFilter(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	if _, err := s.runtime.GetSessionSummaryScoped(r.Context(), id, scope); err != nil {
		writeMappedError(w, err)
		return
	}
	episodeID, err := s.runtime.StartNewEpisode(r.Context(), id)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"session_id": id,
		"episode_id": episodeID,
	})
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	options, err := parseEventQueryOptions(r, s.config.MaxEventResults)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sinceID := options.Since
	if sinceID != "" {
		result := s.runtime.EventsSinceFiltered(sinceID, options.Filter, options.Limit)
		writeJSON(w, http.StatusOK, cursorListResponse{
			Items:        runtimesvc.ProjectEventViews(result.Events),
			Count:        len(result.Events),
			CursorStatus: result.Status,
			NextCursor:   result.NextCursor,
		})
		return
	}
	items := s.runtime.EventSnapshotFiltered(options.Filter, options.Limit)
	writeJSON(w, http.StatusOK, listResponse{Items: runtimesvc.ProjectEventViews(items), Count: len(items)})
}

func (s *Server) handleEventStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming not supported"))
		return
	}

	sub := s.runtime.SubscribeEvents(128)
	if sub == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("event streaming not available"))
		return
	}
	defer sub.Close()

	options, err := parseEventQueryOptions(r, s.config.MaxEventResults)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	replayed := make(map[string]struct{})
	if sinceID := options.Since; sinceID != "" {
		result := s.runtime.EventsSinceFiltered(sinceID, options.Filter, options.Limit)
		replayed = replayEventIDs(result.Events)
		for _, event := range result.Events {
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
		}
		flusher.Flush()
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.Events():
			if !ok {
				return
			}
			if shouldSkipReplayedEvent(replayed, event.ID) {
				continue
			}
			if !options.Filter.IsZero() && !options.Filter.Matches(event) {
				continue
			}
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, event any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func replayEventIDs(events []eventbus.Event) map[string]struct{} {
	if len(events) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.ID) == "" {
			continue
		}
		out[event.ID] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func shouldSkipReplayedEvent(replayed map[string]struct{}, eventID string) bool {
	if len(replayed) == 0 {
		return false
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false
	}
	if _, ok := replayed[eventID]; !ok {
		return false
	}
	delete(replayed, eventID)
	return true
}

type eventQueryOptions struct {
	Limit  int
	Since  string
	Filter runtimesvc.EventFilter
}

func parseEventQueryOptions(r *http.Request, defaultLimit int) (eventQueryOptions, error) {
	query := r.URL.Query()
	options := eventQueryOptions{
		Limit: defaultLimit,
		Since: strings.TrimSpace(query.Get("since")),
		Filter: runtimesvc.EventFilter{
			Type:      eventbus.EventType(strings.TrimSpace(query.Get("type"))),
			RunID:     strings.TrimSpace(query.Get("run_id")),
			SessionID: strings.TrimSpace(query.Get("session_id")),
		},
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return eventQueryOptions{}, fmt.Errorf("invalid limit %q", raw)
		}
		options.Limit = limit
	}
	return options, nil
}
