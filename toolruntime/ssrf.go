package toolruntime

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	ssrfutil "github.com/fulcrus/hopclaw/internal/ssrf"
)

// ---------------------------------------------------------------------------
// SSRF Guard — Validates URLs and hostnames against private network access
// ---------------------------------------------------------------------------

var privateNetworks = ssrfutil.DefaultPrivateNetworks()
var lookupIP = net.LookupIP

func hostMatchesList(host string, list []string) bool {
	return ssrfutil.HostMatchesList(host, list)
}

func isLoopbackHost(host string) bool {
	return ssrfutil.IsLoopbackHost(host)
}

// isPrivateNetworkIP checks whether an IP falls within a private/reserved range.
func isPrivateNetworkIP(ip net.IP) bool {
	return ssrfutil.IsIPInNetworks(ip, privateNetworks)
}

func isBenchmarkTestingIP(ip net.IP) bool {
	return ssrfutil.IsBenchmarkTestingIP(ip)
}

// ---------------------------------------------------------------------------
// URL-based guard (for net.fetch, net.http, net.download, net.upload)
// ---------------------------------------------------------------------------

// checkURLSSRF validates that a URL does not target a private network address.
// It resolves the hostname via DNS to catch aliases like "localhost",
// "internal.corp", or DNS rebinding attacks.
func checkURLSSRF(rawURL string, constraints config.NetConstraints) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("ssrf: invalid url: %w", err)
	}

	// Only validate http/https — other schemes are blocked.
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("ssrf: scheme %q is not allowed, use http or https", u.Scheme)
	}

	host := u.Hostname()
	return checkHostSSRF(host, constraints)
}

// ---------------------------------------------------------------------------
// Host-based guard (for net.ping, net.port_check, net.cert, net.dns, net.whois)
// ---------------------------------------------------------------------------

// checkHostSSRF validates that a hostname does not resolve to a private network address.
func checkHostSSRF(host string, constraints config.NetConstraints) error {
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("ssrf: empty hostname")
	}

	// Check allowlist first — explicitly allowed hosts skip all checks.
	if len(constraints.AllowHosts) > 0 {
	if ssrfutil.HostMatchesList(host, constraints.AllowHosts) {
			return nil
		}
		// When an explicit allowlist is set, deny everything not on it.
		return fmt.Errorf("ssrf: host %q is not in the allow list", host)
	}

	// Check denylist.
	if ssrfutil.HostMatchesList(host, constraints.DenyHosts) {
		return fmt.Errorf("ssrf: host %q is blocked by deny list", host)
	}

	// If private networks are allowed, skip the IP check.
	if constraints.AllowPrivate {
		return nil
	}

	// Check if AllowLocal permits localhost.
	allowLocal := constraints.AllowLocal == nil || *constraints.AllowLocal
	if ssrfutil.IsLoopbackHost(host) {
		if allowLocal {
			return nil
		}
		return fmt.Errorf("ssrf: localhost access is not allowed")
	}

	// Check literal IP addresses first (no DNS resolution needed).
	literalHost := host
	if strings.HasPrefix(literalHost, "[") && strings.HasSuffix(literalHost, "]") {
		literalHost = strings.TrimPrefix(strings.TrimSuffix(literalHost, "]"), "[")
	}
	if ip := net.ParseIP(literalHost); ip != nil {
		if ip.IsLoopback() {
			if allowLocal {
				return nil
			}
			return fmt.Errorf("ssrf: localhost access is not allowed")
		}
		if isPrivateNetworkIP(ip) {
			return fmt.Errorf("ssrf: target ip %s is in a private network range", host)
		}
		return nil
	}

	// Resolve hostname to IPs and check each resolved address.
	// This catches DNS-based SSRF (e.g., attacker.com → 192.168.1.1).
	ips, err := lookupIP(host)
	if err != nil {
		// DNS resolution failure in production: fail-closed.
		// In desktop mode (AllowLocal=true): fail-open for usability.
		if !allowLocal {
			return fmt.Errorf("ssrf: cannot resolve %q, blocking for safety: %w", host, err)
		}
		return nil
	}

	sawLoopback := false
	for _, ip := range ips {
		if ip.IsLoopback() {
			sawLoopback = true
			continue
		}
		if isPrivateNetworkIP(ip) {
			if allowLocal && isBenchmarkTestingIP(ip) {
				continue
			}
			return fmt.Errorf("ssrf: host %q resolves to private ip %s", host, ip)
		}
	}
	if sawLoopback {
		if allowLocal {
			return nil
		}
		return fmt.Errorf("ssrf: localhost access is not allowed")
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
