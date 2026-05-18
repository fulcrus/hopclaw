package gateway

import (
	"net/http"
	"strings"
)

func (g *Gateway) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if g.oauth2Provider == nil {
		gwError(w, http.StatusNotFound, "oauth2 authentication is not configured")
		return
	}
	g.oauth2Provider.HandleLogin(w, r)
}

func (g *Gateway) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if g.oauth2Provider == nil {
		gwError(w, http.StatusNotFound, "oauth2 authentication is not configured")
		return
	}
	g.oauth2Provider.HandleCallback(w, r)
}

func (g *Gateway) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if g.authSessionStore != nil {
		if cookie, err := r.Cookie(authSessionCookieName(g.authSessionConfig.CookieName)); err == nil && strings.TrimSpace(cookie.Value) != "" {
			g.authSessionStore.Delete(cookie.Value)
		}
	}

	clearAuthSessionCookies(w, r, g.authSessionConfig)
	clearManagedCookie(w, r, g.authSessionConfig, oauth2StateCookieName, true)
	clearManagedCookie(w, r, g.authSessionConfig, oauth2NonceCookieName, true)
	clearManagedCookie(w, r, g.authSessionConfig, oauth2PKCECookieName, true)

	gwJSON(w, http.StatusOK, okResponse{OK: true})
}
