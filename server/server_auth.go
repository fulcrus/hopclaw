package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/controlplane"
	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
)

func writeMappedError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	writeError(w, serverHTTPStatusForError(err, http.StatusInternalServerError), err)
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	token := strings.TrimSpace(s.config.AuthToken)
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/healthz" || r.URL.Path == RuntimeWebSocketPath || r.URL.Path == "/runtime/approvals/callbacks/resolve" {
			next.ServeHTTP(w, r)
			return
		}
		if authAuthorized(r, token) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="hopclaw-runtime"`)
		writeError(w, http.StatusUnauthorized, fmt.Errorf("missing or invalid auth token"))
	})
}

func (s *Server) authorizeApprovalCallback(w http.ResponseWriter, r *http.Request, provider string, body []byte) bool {
	if authAuthorized(r, strings.TrimSpace(s.config.AuthToken)) {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(provider))
	if name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("provider is required"))
		return false
	}
	policy, ok := s.config.ApprovalCallbacks[name]
	if !ok {
		writeError(w, http.StatusForbidden, fmt.Errorf("approval callback provider %q is not configured", strings.TrimSpace(provider)))
		return false
	}
	switch strings.ToLower(strings.TrimSpace(policy.Mode)) {
	case "", "token":
		headerName := strings.TrimSpace(policy.HeaderName)
		if headerName == "" {
			headerName = "X-HopClaw-Approval-Token"
		}
		token := strings.TrimSpace(policy.Token)
		if token == "" {
			writeError(w, http.StatusForbidden, fmt.Errorf("approval callback provider %q does not allow callbacks", strings.TrimSpace(provider)))
			return false
		}
		got := strings.TrimSpace(r.Header.Get(headerName))
		if got == "" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`HopClaw-Approval realm="callback", header="%s"`, headerName))
			writeError(w, http.StatusUnauthorized, fmt.Errorf("missing approval callback token"))
			return false
		}
		if !secureTokenEqual(got, token) {
			writeError(w, http.StatusForbidden, fmt.Errorf("invalid approval callback token"))
			return false
		}
	case "hmac":
		if !authorizeApprovalCallbackHMAC(w, r, provider, policy, body) {
			return false
		}
	default:
		writeError(w, http.StatusForbidden, fmt.Errorf("approval callback provider %q uses unsupported auth mode", strings.TrimSpace(provider)))
		return false
	}
	return true
}

func authorizeApprovalCallbackHMAC(w http.ResponseWriter, r *http.Request, provider string, policy controlplane.ApprovalCallbackAuthPolicy, body []byte) bool {
	secret := strings.TrimSpace(policy.Secret)
	if secret == "" {
		writeError(w, http.StatusForbidden, fmt.Errorf("approval callback provider %q does not allow hmac callbacks", strings.TrimSpace(provider)))
		return false
	}
	signatureHeader := strings.TrimSpace(policy.SignatureHeader)
	if signatureHeader == "" {
		signatureHeader = "X-HopClaw-Signature"
	}
	timestampHeader := strings.TrimSpace(policy.TimestampHeader)
	if timestampHeader == "" {
		timestampHeader = "X-HopClaw-Timestamp"
	}
	maxAge := policy.MaxAge
	if maxAge <= 0 {
		maxAge = 5 * time.Minute
	}
	signature := strings.TrimSpace(r.Header.Get(signatureHeader))
	timestamp := strings.TrimSpace(r.Header.Get(timestampHeader))
	if signature == "" || timestamp == "" {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`HopClaw-Approval realm="callback", headers="%s,%s"`, signatureHeader, timestampHeader))
		writeError(w, http.StatusUnauthorized, fmt.Errorf("missing approval callback signature"))
		return false
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid approval callback timestamp"))
		return false
	}
	signedAt := time.Unix(seconds, 0).UTC()
	now := time.Now().UTC()
	if signedAt.After(now.Add(maxAge)) || now.Sub(signedAt) > maxAge {
		writeError(w, http.StatusForbidden, fmt.Errorf("approval callback signature expired"))
		return false
	}
	expected := "sha256=" + computeApprovalCallbackHMAC(secret, timestamp, body)
	if !secureTokenEqual(signature, expected) {
		writeError(w, http.StatusForbidden, fmt.Errorf("invalid approval callback signature"))
		return false
	}
	return true
}

func computeApprovalCallbackHMAC(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func authAuthorized(r *http.Request, token string) bool {
	if secureTokenEqual(strings.TrimSpace(r.Header.Get("X-HopClaw-Token")), token) {
		return true
	}
	if secureTokenEqual(strings.TrimSpace(r.Header.Get("X-OpenClaw-Token")), token) {
		return true
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return secureTokenEqual(strings.TrimSpace(authz[len("Bearer "):]), token)
	}
	return false
}

func secureTokenEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	gotDigest := sha256.Sum256([]byte(got))
	wantDigest := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(gotDigest[:], wantDigest[:]) == 1
}

func writeError(w http.ResponseWriter, status int, err error) {
	apiresponse.WriteError(context.Background(), w, status, err, "write server json response failed")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	apiresponse.WriteJSON(context.Background(), w, status, v, "write server json response failed")
}
