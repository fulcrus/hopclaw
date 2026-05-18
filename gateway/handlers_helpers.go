package gateway

import (
	"net/http"
	"strings"
)

const helperReclaimNameError = `name must be "browser" or "desktop"`

// handleHelpersStatus returns the status of managed Browser and Desktop helpers.
// GET /operator/helpers/status
func (g *Gateway) handleHelpersStatus(w http.ResponseWriter, r *http.Request) {
	browser := HelperState{Status: "unavailable"}
	desktop := HelperState{Status: "unavailable"}
	if g.managedHelpers == nil {
		gwJSON(w, http.StatusOK, buildHelperStatusResponse(browser, desktop))
		return
	}
	var err error
	browser, desktop, err = g.managedHelpers.Status(r.Context())
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, buildHelperStatusResponse(browser, desktop))
}

// handleHelpersReclaim stops a managed helper so it restarts on next use.
// POST /operator/helpers/reclaim
// Body: {"name": "browser"|"desktop"}
func (g *Gateway) handleHelpersReclaim(w http.ResponseWriter, r *http.Request) {
	if g.managedHelpers == nil {
		gwError(w, http.StatusServiceUnavailable, "managed helpers not available")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeOperatorJSONBody(w, r, &body) {
		return
	}
	name := normalizeManagedHelperName(body.Name)
	if !isManagedHelperNameSupported(name) {
		gwError(w, http.StatusBadRequest, helperReclaimNameError)
		return
	}
	if err := g.managedHelpers.Reclaim(r.Context(), name); err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, namedOKResponse{OK: true, Name: name})
}

func buildHelperStatusResponse(browser, desktop HelperState) helperStatusResponse {
	return helperStatusResponse{
		Browser: browser,
		Desktop: desktop,
		Helpers: []helperStatusItem{
			{Name: "browser", HelperState: browser},
			{Name: "desktop", HelperState: desktop},
		},
	}
}

func normalizeManagedHelperName(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

func isManagedHelperNameSupported(name string) bool {
	switch name {
	case "browser", "desktop":
		return true
	default:
		return false
	}
}
