package gateway

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	ssrfutil "github.com/fulcrus/hopclaw/internal/ssrf"
)

// ---------------------------------------------------------------------------
// Trusted proxy auth provider
// ---------------------------------------------------------------------------

const (
	defaultProxyUserHeader  = "X-Forwarded-User"
	defaultProxyEmailHeader = "X-Forwarded-Email"
)

// TrustedProxyProvider authenticates requests by extracting identity from
// headers set by a trusted reverse proxy (e.g., Pomerium, OAuth2 Proxy,
// Authelia). The upstream proxy is responsible for actual authentication;
// this provider merely reads the forwarded identity.
type TrustedProxyProvider struct {
	headerName      string // e.g., "X-Forwarded-User"
	headerEmail     string // e.g., "X-Forwarded-Email" (optional)
	trustedNetworks []*net.IPNet
}

// NewTrustedProxyProvider returns a provider that extracts identity from the
// given proxy headers. If headerName is empty, it defaults to
// "X-Forwarded-User". headerEmail is optional; pass "" to skip email
// extraction.
func NewTrustedProxyProvider(headerName, headerEmail string) *TrustedProxyProvider {
	if headerName == "" {
		headerName = defaultProxyUserHeader
	}
	return &TrustedProxyProvider{
		headerName:      headerName,
		headerEmail:     headerEmail,
		trustedNetworks: ssrfutil.DefaultPrivateNetworks(),
	}
}

func NewTrustedProxyProviderWithTrustedCIDRs(headerName, headerEmail string, trustedCIDRs []string) (*TrustedProxyProvider, error) {
	provider := NewTrustedProxyProvider(headerName, headerEmail)
	if len(trustedCIDRs) == 0 {
		return provider, nil
	}
	networks, err := ssrfutil.ParseNetworks(trustedCIDRs)
	if err != nil {
		return nil, err
	}
	provider.trustedNetworks = networks
	return provider, nil
}

// Name returns "trusted-proxy".
func (p *TrustedProxyProvider) Name() string { return "trusted-proxy" }

// Authenticate extracts identity from proxy headers.
// Returns (nil, nil) if the configured user header is not present (skip to
// next provider in the chain).
// Returns (*AuthIdentity, nil) if the header is present (authenticated).
func (p *TrustedProxyProvider) Authenticate(r *http.Request) (*AuthIdentity, error) {
	subject := strings.TrimSpace(r.Header.Get(p.headerName))
	if subject == "" {
		return nil, nil
	}
	if !p.isTrustedProxySource(r) {
		return nil, fmt.Errorf("trusted proxy auth requires requests from a trusted proxy source")
	}

	identity := &AuthIdentity{
		Subject:  subject,
		Provider: p.Name(),
	}

	if p.headerEmail != "" {
		email := strings.TrimSpace(r.Header.Get(p.headerEmail))
		if email != "" {
			identity.Metadata = map[string]string{
				"email": email,
			}
		}
	}

	return identity, nil
}

func (p *TrustedProxyProvider) isTrustedProxySource(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ssrfutil.IsIPInNetworks(ip, p.trustedNetworks)
}
