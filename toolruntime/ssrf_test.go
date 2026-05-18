package toolruntime

import (
	"fmt"
	"net"
	"testing"

	"github.com/fulcrus/hopclaw/config"
)

func TestCheckURLSSRF_BlocksPrivateIPs(t *testing.T) {
	nc := config.NetConstraints{}

	tests := []struct {
		url     string
		blocked bool
	}{
		// Private IPs — blocked by default.
		{"http://192.168.1.1/admin", true},
		{"http://10.0.0.1:8080/api", true},
		{"http://172.16.0.1/secret", true},
		{"http://127.0.0.1:3000", false},
		{"http://[::1]/api", false},
		{"http://169.254.169.254/latest/meta-data/", true}, // AWS metadata

		// Public IPs — allowed.
		{"https://1.1.1.1/dns-query", false},
		{"https://8.8.8.8", false},

		// Invalid schemes — blocked.
		{"ftp://example.com/file", true},
		{"file:///etc/passwd", true},
		{"gopher://evil.com", true},
	}

	for _, tt := range tests {
		err := checkURLSSRF(tt.url, nc)
		if tt.blocked && err == nil {
			t.Errorf("expected %q to be blocked", tt.url)
		}
		if !tt.blocked && err != nil {
			t.Errorf("expected %q to be allowed, got: %v", tt.url, err)
		}
	}
}

func TestCheckURLSSRF_AllowPrivate(t *testing.T) {
	nc := config.NetConstraints{AllowPrivate: true}

	err := checkURLSSRF("http://192.168.1.1/admin", nc)
	if err != nil {
		t.Errorf("expected private IP to be allowed with AllowPrivate=true, got: %v", err)
	}
}

func TestCheckURLSSRF_AllowLocal(t *testing.T) {
	b := false
	nc := config.NetConstraints{AllowLocal: &b}

	err := checkHostSSRF("localhost", nc)
	if err == nil {
		t.Error("expected localhost to be blocked with AllowLocal=false")
	}

	err = checkHostSSRF("127.0.0.1", nc)
	if err == nil {
		t.Error("expected 127.0.0.1 to be blocked with AllowLocal=false")
	}

	err = checkHostSSRF("::1", nc)
	if err == nil {
		t.Error("expected ::1 to be blocked with AllowLocal=false")
	}
}

func TestCheckHostSSRF_DefaultAllowsLoopback(t *testing.T) {
	nc := config.NetConstraints{}

	for _, host := range []string{"localhost", "127.0.0.1", "::1", "[::1]"} {
		if err := checkHostSSRF(host, nc); err != nil {
			t.Fatalf("expected %q to be allowed by default, got %v", host, err)
		}
	}
}

func TestCheckHostSSRF_DenyHosts(t *testing.T) {
	nc := config.NetConstraints{
		AllowPrivate: true, // skip IP checks to isolate deny list test
		DenyHosts:    []string{"evil.com", ".internal.corp"},
	}

	tests := []struct {
		host    string
		blocked bool
	}{
		{"evil.com", true},
		{"sub.evil.com", true},
		{"api.internal.corp", true},
		{"internal.corp", false}, // ".internal.corp" requires subdomain
		{"example.com", false},
	}

	for _, tt := range tests {
		err := checkHostSSRF(tt.host, nc)
		if tt.blocked && err == nil {
			t.Errorf("expected %q to be blocked by deny list", tt.host)
		}
		if !tt.blocked && err != nil {
			t.Errorf("expected %q to be allowed, got: %v", tt.host, err)
		}
	}
}

func TestCheckHostSSRF_AllowHosts(t *testing.T) {
	nc := config.NetConstraints{
		AllowHosts: []string{"api.example.com", ".trusted.org"},
	}

	tests := []struct {
		host    string
		allowed bool
	}{
		{"api.example.com", true},
		{"sub.trusted.org", true},
		{"evil.com", false},
		{"example.com", false},
	}

	for _, tt := range tests {
		err := checkHostSSRF(tt.host, nc)
		if tt.allowed && err != nil {
			t.Errorf("expected %q to be allowed, got: %v", tt.host, err)
		}
		if !tt.allowed && err == nil {
			t.Errorf("expected %q to be blocked by allowlist", tt.host)
		}
	}
}

func TestCheckHostSSRF_AllowsBenchmarkProxyResolutionWhenLocalAccessEnabled(t *testing.T) {
	originalLookup := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		if host != "wttr.in" {
			return nil, fmt.Errorf("unexpected host %q", host)
		}
		return []net.IP{net.ParseIP("198.18.0.11")}, nil
	}
	defer func() { lookupIP = originalLookup }()

	nc := config.NetConstraints{}
	if err := checkHostSSRF("wttr.in", nc); err != nil {
		t.Fatalf("expected benchmark proxy resolution to be allowed in local mode, got %v", err)
	}
}

func TestCheckHostSSRF_BlocksBenchmarkProxyResolutionWhenLocalAccessDisabled(t *testing.T) {
	originalLookup := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		if host != "wttr.in" {
			return nil, fmt.Errorf("unexpected host %q", host)
		}
		return []net.IP{net.ParseIP("198.18.0.11")}, nil
	}
	defer func() { lookupIP = originalLookup }()

	allowLocal := false
	nc := config.NetConstraints{AllowLocal: &allowLocal}
	if err := checkHostSSRF("wttr.in", nc); err == nil {
		t.Fatal("expected benchmark proxy resolution to be blocked when local access is disabled")
	}
}

func TestHostMatchesList(t *testing.T) {
	list := []string{"example.com", ".internal.corp", "EXACT.IO"}

	tests := []struct {
		host  string
		match bool
	}{
		{"example.com", true},       // exact
		{"sub.example.com", true},   // plain suffix
		{"notexample.com", false},   // no match
		{"api.internal.corp", true}, // dot prefix
		{"internal.corp", false},    // dot prefix requires subdomain
		{"exact.io", true},          // case-insensitive
		{"sub.exact.io", true},      // suffix of exact
		{"other.com", false},
	}

	for _, tt := range tests {
		got := hostMatchesList(tt.host, list)
		if got != tt.match {
			t.Errorf("hostMatchesList(%q) = %v, want %v", tt.host, got, tt.match)
		}
	}
}

func TestIsPrivateNetworkIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"127.0.0.1", true},
		{"169.254.169.254", true},
		{"100.64.0.1", true}, // Carrier-grade NAT
		{"0.0.0.1", true},    // "This" network
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"203.0.113.1", false},
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"2001:db8::1", false},
	}

	for _, tt := range tests {
		ip := parseTestIP(t, tt.ip)
		got := isPrivateNetworkIP(ip)
		if got != tt.private {
			t.Errorf("isPrivateNetworkIP(%s) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

func parseTestIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("invalid test IP: %s", s)
	}
	return ip
}
