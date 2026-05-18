package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/logging"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func (g *Gateway) publicSurfaceHandler() http.Handler {
	if g == nil || g.publicServerHandler == nil {
		return http.NotFoundHandler()
	}
	return g.publicServerHandler
}

func (g *Gateway) runtimeSurfaceHandler() http.Handler {
	if g == nil || g.runtimeServerHandler == nil {
		return http.NotFoundHandler()
	}
	return g.runtimeServerHandler
}

type runtimeToolInventory struct {
	runtime *runtimesvc.Service
}

const runtimeToolInventoryTimeout = 3 * time.Second

// ToolDefinitions returns the runtime-visible tool inventory for the supplied
// session, logging and suppressing lookup failures.
func (r runtimeToolInventory) ToolDefinitions(session *agent.Session) []agent.ToolDefinition {
	if r.runtime == nil {
		return nil
	}
	sessionKey := ""
	if session != nil {
		sessionKey = strings.TrimSpace(session.Key)
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeToolInventoryTimeout)
	defer cancel()
	items, err := r.runtime.ListTools(ctx, sessionKey)
	if err != nil {
		log.Warn("list runtime tools failed", "session_key", sessionKey, "error", err)
		return nil
	}
	return items
}

func (g *Gateway) handleOperatorArtifacts(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	query := r.URL.Query()
	filter := artifact.ListFilter{
		Kind:      strings.TrimSpace(query.Get("kind")),
		RunID:     strings.TrimSpace(query.Get("run_id")),
		SessionID: strings.TrimSpace(query.Get("session_id")),
		Limit:     50,
	}
	items, err := g.runtime.ListArtifacts(r.Context(), filter)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: items, Count: len(items)})
}

func (g *Gateway) handleArtifactPreview(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}
	body, contentType, err := g.runtime.ReadArtifact(r.Context(), r.PathValue("id"))
	if err != nil {
		gwError(w, gatewayHTTPStatusForError(err, http.StatusInternalServerError), err.Error())
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
