package browserd

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	ssrfutil "github.com/fulcrus/hopclaw/internal/ssrf"
)

// ---------------------------------------------------------------------------
// SSRF Protection — Navigation Guard
// ---------------------------------------------------------------------------

// SSRFPolicy defines security policy for browser navigation.
type SSRFPolicy struct {
	AllowPrivateNetwork bool     `json:"allow_private_network" yaml:"allow_private_network"`
	AllowedHostnames    []string `json:"allowed_hostnames" yaml:"allowed_hostnames"`
}

// checkNavigationSSRF validates that a URL does not target a private network
// address, unless explicitly allowed by the policy.
func checkNavigationSSRF(rawURL string, policy SSRFPolicy) error {
	if policy.AllowPrivateNetwork {
		return nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("ssrf: invalid url: %w", err)
	}

	// Only check http/https schemes.
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil
	}

	host := u.Hostname()

	// Check allowlist.
	for _, allowed := range policy.AllowedHostnames {
		if strings.EqualFold(host, allowed) {
			return nil
		}
	}

	// Resolve hostname to IP addresses.
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS resolution failure is not blocking — may be proxy env.
		return nil
	}

	for _, ip := range ips {
		if ssrfutil.IsPrivateIP(ip) {
			return fmt.Errorf("ssrf: navigation to private network address %s (%s) is blocked", host, ip)
		}
	}

	return nil
}
