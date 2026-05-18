package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/internal/diagnostics"
)

func (g *Gateway) registerDiagnosticsRoutes(mux *http.ServeMux) {
	if mux == nil || !g.diagnosticsCollectorReady() {
		return
	}
	mux.HandleFunc("POST /diagnostics/reports", g.handleDiagnosticsReportUpload)
}

func (g *Gateway) diagnosticsCollectorReady() bool {
	if g == nil {
		return false
	}
	return diagnostics.CollectorEnabled(g.config.Diagnostics) &&
		strings.TrimSpace(g.config.Diagnostics.CollectorAuthToken) != ""
}

func (g *Gateway) handleDiagnosticsReportUpload(w http.ResponseWriter, r *http.Request) {
	if !g.diagnosticsCollectorReady() {
		http.NotFound(w, r)
		return
	}

	auth := NewBearerTokenProvider(strings.TrimSpace(g.config.Diagnostics.CollectorAuthToken))
	identity, err := auth.Authenticate(r)
	if err != nil {
		gwError(w, http.StatusUnauthorized, "invalid diagnostics token")
		return
	}
	if identity == nil {
		gwError(w, http.StatusUnauthorized, "missing diagnostics token")
		return
	}

	limit := diagnostics.CollectorMaxUploadBytes(g.config.Diagnostics)
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseMultipartForm(limit); err != nil {
		if isRequestBodyTooLarge(err) {
			gwError(w, http.StatusRequestEntityTooLarge, "diagnostics bundle exceeds maximum size")
			return
		}
		gwError(w, http.StatusBadRequest, "invalid diagnostics upload form")
		return
	}

	rawEnvelope := strings.TrimSpace(r.FormValue("envelope"))
	if rawEnvelope == "" {
		gwError(w, http.StatusBadRequest, "missing diagnostics envelope")
		return
	}
	var envelope diagnostics.Envelope
	if err := json.Unmarshal([]byte(rawEnvelope), &envelope); err != nil {
		gwError(w, http.StatusBadRequest, "invalid diagnostics envelope")
		return
	}

	file, header, err := r.FormFile("bundle")
	if err != nil {
		gwError(w, http.StatusBadRequest, "missing diagnostics bundle")
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		if isRequestBodyTooLarge(err) {
			gwError(w, http.StatusRequestEntityTooLarge, "diagnostics bundle exceeds maximum size")
			return
		}
		gwError(w, http.StatusInternalServerError, "failed to read diagnostics bundle")
		return
	}
	if len(content) == 0 {
		gwError(w, http.StatusBadRequest, "diagnostics bundle is empty")
		return
	}

	requestID := strings.TrimSpace(w.Header().Get(headerRequestID))
	stored, err := diagnostics.StoreBundle(
		g.config.Diagnostics,
		envelope,
		header.Filename,
		content,
		r.RemoteAddr,
		r.UserAgent(),
		requestID,
	)
	if err != nil {
		log.Warn("diagnostics: failed to store bundle", "error", err, "remote_addr", r.RemoteAddr)
		gwError(w, http.StatusInternalServerError, "failed to store diagnostics bundle")
		return
	}

	gwJSON(w, http.StatusCreated, diagnostics.SubmitResult{
		OK:        true,
		ReportID:  stored.ReportID,
		RequestID: requestID,
	})
}
