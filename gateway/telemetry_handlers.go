package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/internal/telemetry"
)

func (g *Gateway) registerTelemetryRoutes(mux *http.ServeMux) {
	if mux == nil || !g.telemetryCollectorReady() {
		return
	}
	mux.HandleFunc("POST /telemetry/events", g.handleTelemetryEventUpload)
}

func (g *Gateway) telemetryCollectorReady() bool {
	if g == nil {
		return false
	}
	return telemetry.CollectorEnabled(g.config.Diagnostics) &&
		strings.TrimSpace(g.config.Diagnostics.TelemetryCollectorAuthToken) != ""
}

func (g *Gateway) handleTelemetryEventUpload(w http.ResponseWriter, r *http.Request) {
	if !g.telemetryCollectorReady() {
		http.NotFound(w, r)
		return
	}

	auth := NewBearerTokenProvider(strings.TrimSpace(g.config.Diagnostics.TelemetryCollectorAuthToken))
	identity, err := auth.Authenticate(r)
	if err != nil {
		gwError(w, http.StatusUnauthorized, "invalid telemetry token")
		return
	}
	if identity == nil {
		gwError(w, http.StatusUnauthorized, "missing telemetry token")
		return
	}

	limit := telemetry.CollectorMaxUploadBytes(g.config.Diagnostics)
	r.Body = http.MaxBytesReader(w, r.Body, limit)

	var batch telemetry.Batch
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		if isRequestBodyTooLarge(err) {
			gwError(w, http.StatusRequestEntityTooLarge, "telemetry batch exceeds maximum size")
			return
		}
		gwError(w, http.StatusBadRequest, "invalid telemetry batch")
		return
	}

	requestID := strings.TrimSpace(w.Header().Get(headerRequestID))
	stored, err := telemetry.StoreBatch(
		g.config.Diagnostics,
		batch,
		r.RemoteAddr,
		r.UserAgent(),
		requestID,
	)
	if err != nil {
		log.Warn("telemetry: failed to store batch", "error", err, "remote_addr", r.RemoteAddr)
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}

	gwJSON(w, http.StatusAccepted, telemetry.SubmitResult{
		OK:        true,
		Accepted:  stored.Accepted,
		BatchID:   stored.BatchID,
		RequestID: requestID,
	})
}
