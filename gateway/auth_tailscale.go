package gateway

import (
	"net/http"
	"strings"
)

// ---------------------------------------------------------------------------
// Tailscale auth provider
// ---------------------------------------------------------------------------

const (
	headerTailscaleUserLogin      = "Tailscale-User-Login"
	headerTailscaleUserName       = "Tailscale-User-Name"
	headerTailscaleUserProfilePic = "Tailscale-User-Profile-Pic"
)

// TailscaleAuthProvider extracts identity from Tailscale Serve headers.
// When a request passes through Tailscale Serve the following headers are
// injected automatically:
//   - Tailscale-User-Login:       user's login name (e.g. "user@example.com")
//   - Tailscale-User-Name:        display name
//   - Tailscale-User-Profile-Pic: avatar URL
type TailscaleAuthProvider struct{}

// NewTailscaleAuthProvider returns a new TailscaleAuthProvider.
func NewTailscaleAuthProvider() *TailscaleAuthProvider {
	return &TailscaleAuthProvider{}
}

// Name returns "tailscale".
func (p *TailscaleAuthProvider) Name() string { return "tailscale" }

// Authenticate checks for Tailscale identity headers.
// Returns (nil, nil) if the Tailscale-User-Login header is not present (skip
// to next provider in the chain).
// Returns (*AuthIdentity, nil) if the header is present (authenticated).
func (p *TailscaleAuthProvider) Authenticate(r *http.Request) (*AuthIdentity, error) {
	login := strings.TrimSpace(r.Header.Get(headerTailscaleUserLogin))
	if login == "" {
		return nil, nil // not a Tailscale request
	}

	metadata := make(map[string]string)
	if name := strings.TrimSpace(r.Header.Get(headerTailscaleUserName)); name != "" {
		metadata["name"] = name
	}
	if pic := strings.TrimSpace(r.Header.Get(headerTailscaleUserProfilePic)); pic != "" {
		metadata["profile_pic"] = pic
	}

	identity := &AuthIdentity{
		Subject:  login,
		Provider: p.Name(),
	}
	if len(metadata) > 0 {
		identity.Metadata = metadata
	}

	return identity, nil
}
