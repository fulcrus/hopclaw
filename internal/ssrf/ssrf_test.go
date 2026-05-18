package ssrf

import (
	"net"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ip      string
		private bool
	}{
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"127.0.0.1", true},
		{"8.8.8.8", false},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if got := IsPrivateIP(ip); got != tt.private {
			t.Fatalf("IsPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

func TestHostMatchesList(t *testing.T) {
	t.Parallel()

	if !HostMatchesList("sub.example.com", []string{"example.com"}) {
		t.Fatal("expected suffix match")
	}
	if HostMatchesList("other.com", []string{"example.com"}) {
		t.Fatal("unexpected match")
	}
}

func TestIsLoopbackHost(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"localhost", "127.0.0.1", "::1", "[::1]"} {
		if !IsLoopbackHost(host) {
			t.Fatalf("expected %q to be loopback", host)
		}
	}
}
