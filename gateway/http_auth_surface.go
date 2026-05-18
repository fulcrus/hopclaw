package gateway

import (
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/deviceauth"
)

func (g *Gateway) withAuth(next http.Handler) http.Handler {
	return g.authenticatedHandler(next, true)
}

func (g *Gateway) withWSAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("X-HopClaw-Device-Auth")) != "" && g.deviceStore != nil && g.devicePairing != nil {
			mw := deviceauth.NewMiddleware(g.deviceStore, g.devicePairing)
			dc, err := mw.AuthenticateRequest(r)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="hopclaw-device"`)
				gwError(w, http.StatusUnauthorized, err.Error())
				return
			}
			next.ServeHTTP(w, r.WithContext(deviceauth.ContextWithDevice(r.Context(), dc)))
			return
		}
		identity, err := g.authenticateRequest(r)
		if err != nil {
			if g.authInitErr != nil {
				gwError(w, http.StatusServiceUnavailable, g.authInitErr.Error())
				return
			}
			writeAuthError(r.Context(), w, err.Error())
			return
		}
		if identity != nil {
			reqWithIdentity := authScopeFromIdentity(identity).applyHeaders(r.WithContext(contextWithAuthIdentity(r.Context(), identity)))
			if err := g.authorizeRequest(reqWithIdentity, identity); err != nil {
				writeAuthorizationError(w, err.Error())
				return
			}
			next.ServeHTTP(w, reqWithIdentity)
			return
		}
		if !g.authConfigured() {
			next.ServeHTTP(w, r)
			return
		}
		writeAuthError(r.Context(), w, "missing or invalid auth credentials")
	})
}
