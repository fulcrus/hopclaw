package gateway

import (
	"net/http"
	"time"

	"github.com/fulcrus/hopclaw/heartbeat"
)

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type heartbeatResponse struct {
	OK      bool                     `json:"ok"`
	Running bool                     `json:"running"`
	Stale   bool                     `json:"stale"`
	BeatAt  time.Time                `json:"beat_at"`
	Status  heartbeat.Status         `json:"status"`
	Metrics heartbeatMetricsResponse `json:"metrics"`
}

type heartbeatMetricsResponse struct {
	ActiveSessions int     `json:"active_sessions"`
	TotalRuns      int64   `json:"total_runs"`
	MemoryUsageMB  float64 `json:"memory_usage_mb"`
	GoRoutines     int     `json:"go_routines"`
	Uptime         string  `json:"uptime"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleHeartbeat returns the current system heartbeat status.
//
//	GET /operator/heartbeat
func (g *Gateway) handleHeartbeat(w http.ResponseWriter, _ *http.Request) {
	if g.heartbeat == nil {
		gwError(w, http.StatusServiceUnavailable, "heartbeat not available")
		return
	}

	beat := g.heartbeat.Beat()
	running := g.heartbeat.IsRunning()
	stale := g.heartbeat.IsStale()

	resp := heartbeatResponse{
		OK:      running && !stale,
		Running: running,
		Stale:   stale,
		BeatAt:  beat.BeatAt,
		Status:  beat.Status,
		Metrics: heartbeatMetricsResponse{
			ActiveSessions: beat.Metrics.ActiveSessions,
			TotalRuns:      beat.Metrics.TotalRuns,
			MemoryUsageMB:  beat.Metrics.MemoryUsageMB,
			GoRoutines:     beat.Metrics.GoRoutines,
			Uptime:         beat.Uptime.String(),
		},
	}

	gwJSON(w, http.StatusOK, resp)
}
