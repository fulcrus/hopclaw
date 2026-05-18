package gateway

import "net/http"

const (
	consoleBasePath       = "/dashboard"
	legacyConsoleBasePath = "/webchat"
)

func appendRawQuery(target, rawQuery string) string {
	if rawQuery == "" {
		return target
	}
	return target + "?" + rawQuery
}

func (g *Gateway) handleConsoleRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, appendRawQuery(consoleBasePath+"/", r.URL.RawQuery), http.StatusFound)
}

func (g *Gateway) handleDashboardIndexRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, appendRawQuery(consoleBasePath+"/", r.URL.RawQuery), http.StatusMovedPermanently)
}

func (g *Gateway) handleLegacyConsoleRedirect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Sunset", "2027-01-01")
	w.Header().Set("Link", `</dashboard/>; rel="successor-version"`)
	http.Redirect(w, r, appendRawQuery(consoleBasePath+"/", r.URL.RawQuery), http.StatusMovedPermanently)
}
