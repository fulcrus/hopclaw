package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/fulcrus/hopclaw/authz"
	"github.com/fulcrus/hopclaw/eventbus"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /authz/decide", handleAuthZDecision)
	mux.HandleFunc("POST /audit/events", handleAuditEvent)

	addr := "127.0.0.1:18081"
	log.Printf("enterprise bridge listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handleAuthZDecision(w http.ResponseWriter, r *http.Request) {
	if !bridgeAuthorized(r) {
		writeJSON(w, http.StatusForbidden, authz.AuthorizationDecision{
			Allowed: false,
			Reason:  "bridge token mismatch",
			Source:  "enterprise-bridge",
		})
		return
	}
	defer r.Body.Close()

	var req authz.AuthorizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	decision := authz.AuthorizationDecision{
		Allowed: false,
		Reason:  "operator scope required",
		Source:  "enterprise-bridge",
	}
	if req.Resource == authz.ResourceRuns && req.Action == authz.ActionExecute {
		decision.Allowed = true
		decision.Reason = "runtime execution allowed"
	}
	if hasScope(req.Principal, "hopclaw:operator") || hasRole(req.Principal, "operator") {
		decision.Allowed = true
		decision.Reason = "operator policy allow"
	}

	writeJSON(w, http.StatusOK, map[string]any{"decision": decision})
}

func handleAuditEvent(w http.ResponseWriter, r *http.Request) {
	if !bridgeAuthorized(r) {
		http.Error(w, "bridge token mismatch", http.StatusForbidden)
		return
	}
	defer r.Body.Close()

	var event eventbus.Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("audit event received type=%s run_id=%s session_id=%s", event.Type, event.RunID, event.SessionID)
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func bridgeAuthorized(r *http.Request) bool {
	expected := strings.TrimSpace(os.Getenv("BRIDGE_SHARED_TOKEN"))
	if expected == "" {
		return true
	}
	return strings.TrimSpace(r.Header.Get("X-Bridge-Token")) == expected
}

func hasScope(principal *authz.Principal, want string) bool {
	if principal == nil {
		return false
	}
	for _, scope := range principal.Scopes {
		if strings.TrimSpace(scope) == want {
			return true
		}
	}
	return false
}

func hasRole(principal *authz.Principal, want string) bool {
	if principal == nil || principal.Metadata == nil {
		return false
	}
	for key, value := range principal.Metadata {
		key = strings.ToLower(strings.TrimSpace(key))
		if key != "role" && key != "resolved_role" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
