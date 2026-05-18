package gateway

import (
	"net/http"
	"strings"
	"time"
)

func defaultAuthSessionConfig(cfg AuthSessionConfig) AuthSessionConfig {
	if strings.TrimSpace(cfg.CookieName) == "" {
		cfg.CookieName = sessionDefaultCookie
	}
	return cfg
}

func authSessionCookieName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return sessionDefaultCookie
	}
	return name
}

func requestWantsSecureCookies(r *http.Request, cfg AuthSessionConfig) bool {
	if cfg.Secure {
		return true
	}
	if r != nil && r.TLS != nil {
		return true
	}
	if r != nil && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		return true
	}
	return false
}

func setManagedCookie(w http.ResponseWriter, r *http.Request, cfg AuthSessionConfig, name, value string, maxAge time.Duration, httpOnly bool) {
	cfg = defaultAuthSessionConfig(cfg)
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Domain:   strings.TrimSpace(cfg.CookieDomain),
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: httpOnly,
		Secure:   requestWantsSecureCookies(r, cfg),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearManagedCookie(w http.ResponseWriter, r *http.Request, cfg AuthSessionConfig, name string, httpOnly bool) {
	cfg = defaultAuthSessionConfig(cfg)
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Domain:   strings.TrimSpace(cfg.CookieDomain),
		MaxAge:   -1,
		HttpOnly: httpOnly,
		Secure:   requestWantsSecureCookies(r, cfg),
		SameSite: http.SameSiteLaxMode,
	})
}

func setCSRFCookie(w http.ResponseWriter, r *http.Request, cfg AuthSessionConfig, value string, maxAge time.Duration) {
	setManagedCookie(w, r, cfg, csrfCookieName, value, maxAge, false)
}

func clearCSRFCookie(w http.ResponseWriter, r *http.Request, cfg AuthSessionConfig) {
	clearManagedCookie(w, r, cfg, csrfCookieName, false)
}

func setAuthSessionCookies(w http.ResponseWriter, r *http.Request, cfg AuthSessionConfig, session *AuthSession) {
	if session == nil {
		return
	}
	ttl := time.Until(session.ExpiresAt)
	setManagedCookie(w, r, cfg, authSessionCookieName(cfg.CookieName), session.ID, ttl, true)
	setCSRFCookie(w, r, cfg, session.CSRFToken, ttl)
}

func clearAuthSessionCookies(w http.ResponseWriter, r *http.Request, cfg AuthSessionConfig) {
	clearManagedCookie(w, r, cfg, authSessionCookieName(cfg.CookieName), true)
	clearCSRFCookie(w, r, cfg)
}
