package deviceauth

import "time"

// ---------------------------------------------------------------------------
// Device roles
// ---------------------------------------------------------------------------

// DeviceRole defines the access level for a device.
type DeviceRole string

const (
	RoleOperator   DeviceRole = "operator"
	RoleNode       DeviceRole = "node"
	RoleAutomation DeviceRole = "automation"
	RoleViewer     DeviceRole = "viewer"
)

// validRoles is the set of recognized device roles.
var validRoles = map[DeviceRole]bool{
	RoleOperator:   true,
	RoleNode:       true,
	RoleAutomation: true,
	RoleViewer:     true,
}

// IsValidRole reports whether r is a recognized device role.
func IsValidRole(r DeviceRole) bool {
	return validRoles[r]
}

// ---------------------------------------------------------------------------
// Pairing states
// ---------------------------------------------------------------------------

// PairingStatus represents the lifecycle state of a pairing attempt.
type PairingStatus string

const (
	PairingPending  PairingStatus = "pending"
	PairingVerified PairingStatus = "verified"
	PairingRevoked  PairingStatus = "revoked"
	PairingExpired  PairingStatus = "expired"
)

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// DeviceIdentity represents a registered device.
type DeviceIdentity struct {
	DeviceID     string    `json:"device_id"`
	Name         string    `json:"name,omitempty"`
	Platform     string    `json:"platform,omitempty"`      // "darwin", "linux", "windows", "ios", "android"
	DeviceFamily string    `json:"device_family,omitempty"` // "desktop", "mobile", "server"
	CreatedAt    time.Time `json:"created_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	Trusted      bool      `json:"trusted"`
}

// DeviceToken represents an auth token for a device.
type DeviceToken struct {
	Token     string     `json:"token"`
	DeviceID  string     `json:"device_id"`
	Role      DeviceRole `json:"role"`
	Scopes    []string   `json:"scopes,omitempty"`
	IssuedAt  time.Time  `json:"issued_at"`
	ExpiresAt time.Time  `json:"expires_at,omitempty"` // zero means no expiry
	UpdatedAt time.Time  `json:"updated_at"`
}

// PairingRecord tracks a device pairing attempt.
type PairingRecord struct {
	DeviceID   string        `json:"device_id"`
	Channel    string        `json:"channel"` // e.g., "web", "ios", "android"
	Code       string        `json:"code"`    // 6-digit verification code
	Status     PairingStatus `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
	ExpiresAt  time.Time     `json:"expires_at"`
	VerifiedAt time.Time     `json:"verified_at,omitempty"`
}

// AuthPayload is the decoded auth payload from a device connection.
type AuthPayload struct {
	Version      int        `json:"version"`
	DeviceID     string     `json:"device_id"`
	ClientID     string     `json:"client_id"`
	ClientMode   string     `json:"client_mode"`
	Role         DeviceRole `json:"role"`
	Scopes       []string   `json:"scopes,omitempty"`
	SignedAtMs   int64      `json:"signed_at_ms"`
	Token        string     `json:"token,omitempty"`
	Nonce        string     `json:"nonce"`
	Platform     string     `json:"platform,omitempty"`      // v3 only
	DeviceFamily string     `json:"device_family,omitempty"` // v3 only
}

// StoreData is the persistent on-disk format.
type StoreData struct {
	Version  int                        `json:"version"`
	DeviceID string                     `json:"device_id"`
	Tokens   map[string]*DeviceToken    `json:"tokens"`  // device_id:role -> token
	Devices  map[string]*DeviceIdentity `json:"devices"` // deviceID -> identity
}
