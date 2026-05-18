package gateway

import (
	"net/http"
	"strconv"
	"time"

	"github.com/fulcrus/hopclaw/wire"
)

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

const wireDefaultLimit = 50

type wireEntriesResponse struct {
	Items []wire.Entry `json:"items"`
	Count int          `json:"count"`
}

type wireClearResponse struct {
	OK bool `json:"ok"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleWireEntries returns wire log entries matching optional query filters.
//
//	GET /operator/wire/entries?provider=&method=&limit=50&since=
func (g *Gateway) handleWireEntries(w http.ResponseWriter, r *http.Request) {
	if g.wire == nil {
		gwError(w, http.StatusServiceUnavailable, "wire logger not available")
		return
	}

	query := r.URL.Query()

	limit := wireDefaultLimit
	if v := query.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	filter := wire.QueryFilter{
		Provider: query.Get("provider"),
		Limit:    limit,
	}
	if v := query.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.Since = t
		}
	}

	entries := g.wire.Query(filter)
	if entries == nil {
		entries = []wire.Entry{}
	}
	gwJSON(w, http.StatusOK, wireEntriesResponse{Items: entries, Count: len(entries)})
}

// handleWireStats returns summary statistics about captured wire entries.
//
//	GET /operator/wire/stats
func (g *Gateway) handleWireStats(w http.ResponseWriter, _ *http.Request) {
	if g.wire == nil {
		gwError(w, http.StatusServiceUnavailable, "wire logger not available")
		return
	}
	gwJSON(w, http.StatusOK, g.wire.Stats())
}

// handleWireClear removes all captured wire entries.
//
//	DELETE /operator/wire/entries
func (g *Gateway) handleWireClear(w http.ResponseWriter, _ *http.Request) {
	if g.wire == nil {
		gwError(w, http.StatusServiceUnavailable, "wire logger not available")
		return
	}
	g.wire.Clear()
	gwJSON(w, http.StatusOK, wireClearResponse{OK: true})
}
