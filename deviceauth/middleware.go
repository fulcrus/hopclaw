package deviceauth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/fulcrus/hopclaw/logging"
)

// ---------------------------------------------------------------------------
// Header and context key constants
// ---------------------------------------------------------------------------

const (
	headerDeviceAuth = "X-HopClaw-Device-Auth"
	headerDeviceID   = "X-HopClaw-Device-ID"
	headerDeviceRole = "X-HopClaw-Device-Role"
)

// contextKey is an unexported type for context keys to prevent collisions.
type contextKey struct{ name string }

var (
	contextKeyDevice = &contextKey{"deviceauth.device"}
)

// ---------------------------------------------------------------------------
// DeviceContext
// ---------------------------------------------------------------------------

// DeviceContext holds the authenticated device information extracted from a
// request.
type DeviceContext struct {
	DeviceID     string
	Role         DeviceRole
	Scopes       []string
	Platform     string
	DeviceFamily string
	Trusted      bool
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// Middleware provides HTTP middleware for device authentication.
type Middleware struct {
	store   *Store
	pairing *PairingManager
}

// NewMiddleware creates device-auth middleware backed by the given store and
// pairing manager.
func NewMiddleware(store *Store, pairing *PairingManager) *Middleware {
	return &Middleware{
		store:   store,
		pairing: pairing,
	}
}

// Authenticate returns middleware that requires valid device authentication.
// Unauthenticated or invalid requests receive a 401 response.
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dc, err := m.AuthenticateRequest(r)
		if err != nil {
			writeDeviceAuthError(w, err.Error())
			return
		}
		ctx := contextWithDevice(r.Context(), dc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Optional returns middleware that attaches device context when valid auth is
// present but does not reject unauthenticated requests.
func (m *Middleware) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dc, err := m.AuthenticateRequest(r)
		if err == nil && dc != nil {
			ctx := contextWithDevice(r.Context(), dc)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Core authentication logic
// ---------------------------------------------------------------------------

// AuthenticateRequest validates device credentials on a single request and
// returns the decoded device context.
func (m *Middleware) AuthenticateRequest(r *http.Request) (*DeviceContext, error) {
	raw := r.Header.Get(headerDeviceAuth)
	if raw == "" {
		return nil, errMissingAuthHeader
	}

	payload, err := DecodePayload(raw)
	if err != nil {
		return nil, err
	}
	if err := ValidatePayload(payload); err != nil {
		return nil, err
	}

	// Look up the device in the store.
	device, ok := m.store.GetDevice(payload.DeviceID)
	if !ok {
		return nil, errDeviceNotRegistered
	}
	if !device.Trusted {
		return nil, errDeviceNotTrusted
	}

	// Validate token against stored token for the role.
	if err := m.validateToken(payload); err != nil {
		return nil, err
	}

	// Update last-seen timestamp (best-effort).
	logging.LogIfErr(r.Context(), m.store.UpdateLastSeen(payload.DeviceID), "update device last seen failed")

	return &DeviceContext{
		DeviceID:     payload.DeviceID,
		Role:         payload.Role,
		Scopes:       payload.Scopes,
		Platform:     payload.Platform,
		DeviceFamily: payload.DeviceFamily,
		Trusted:      device.Trusted,
	}, nil
}

func (m *Middleware) validateToken(payload *AuthPayload) error {
	stored, ok := m.store.GetToken(payload.DeviceID, payload.Role)
	if !ok {
		return errTokenNotFound
	}

	storedHash := HashToken(stored.Token)
	payloadHash := HashToken(payload.Token)
	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(payloadHash)) != 1 {
		return errTokenMismatch
	}

	// Check token expiry if set.
	if !stored.ExpiresAt.IsZero() {
		if stored.ExpiresAt.Before(timeNow()) {
			return errTokenExpired
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

// DeviceFromContext extracts the DeviceContext from the request context.
// Returns nil, false when no device auth was established.
func DeviceFromContext(ctx context.Context) (*DeviceContext, bool) {
	dc, ok := ctx.Value(contextKeyDevice).(*DeviceContext)
	return dc, ok
}

// ContextWithDevice attaches a device context to the request context.
func ContextWithDevice(ctx context.Context, dc *DeviceContext) context.Context {
	return context.WithValue(ctx, contextKeyDevice, dc)
}

func contextWithDevice(ctx context.Context, dc *DeviceContext) context.Context {
	return ContextWithDevice(ctx, dc)
}

// ---------------------------------------------------------------------------
// Loopback detection
// ---------------------------------------------------------------------------

// IsLoopback reports whether the request originates from a loopback address
// (127.0.0.1, ::1, or localhost).
func IsLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ---------------------------------------------------------------------------
// Error responses
// ---------------------------------------------------------------------------

type deviceAuthErrorResponse struct {
	Error string `json:"error"`
}

func writeDeviceAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	logging.DebugIfErr(json.NewEncoder(w).Encode(deviceAuthErrorResponse{Error: msg}), "write device auth error response failed")
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	errMissingAuthHeader   = &authError{"missing device auth header"}
	errDeviceNotRegistered = &authError{"device not registered"}
	errDeviceNotTrusted    = &authError{"device not trusted"}
	errTokenNotFound       = &authError{"token not found for device/role"}
	errTokenMismatch       = &authError{"token mismatch"}
	errTokenExpired        = &authError{"token expired"}
)

type authError struct {
	msg string
}

func (e *authError) Error() string { return e.msg }

// timeNow is a package-level function for testing.
var timeNow = func() time.Time { return time.Now().UTC() }
