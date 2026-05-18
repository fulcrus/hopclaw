package ssrf

import (
	"fmt"
	"net"
	"strings"
)

var defaultPrivateNetworks = MustParseNetworks([]string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"0.0.0.0/8",
	"100.64.0.0/10",
	"198.18.0.0/15",
	"::1/128",
	"fc00::/7",
	"fe80::/10",
})

var benchmarkTestingNetwork = MustParseCIDR("198.18.0.0/15")

func ParseNetworks(cidrs []string) ([]*net.IPNet, error) {
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("parse cidr %q: %w", cidr, err)
		}
		networks = append(networks, network)
	}
	return networks, nil
}

func MustParseNetworks(cidrs []string) []*net.IPNet {
	networks, err := ParseNetworks(cidrs)
	if err != nil {
		panic(err)
	}
	return networks
}

func MustParseCIDR(cidr string) *net.IPNet {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(fmt.Sprintf("invalid cidr %q: %v", cidr, err))
	}
	return network
}

func DefaultPrivateNetworks() []*net.IPNet {
	out := make([]*net.IPNet, len(defaultPrivateNetworks))
	copy(out, defaultPrivateNetworks)
	return out
}

func IsIPInNetworks(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func IsPrivateIP(ip net.IP) bool {
	return IsIPInNetworks(ip, defaultPrivateNetworks)
}

func IsBenchmarkTestingIP(ip net.IP) bool {
	return benchmarkTestingNetwork.Contains(ip)
}

func HostMatchesList(host string, list []string) bool {
	lower := strings.ToLower(host)
	for _, entry := range list {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if lower == entry {
			return true
		}
		if strings.HasPrefix(entry, ".") && strings.HasSuffix(lower, entry) {
			return true
		}
		if !strings.HasPrefix(entry, ".") && strings.HasSuffix(lower, "."+entry) {
			return true
		}
	}
	return false
}

func IsLoopbackHost(host string) bool {
	lower := strings.ToLower(strings.TrimSpace(host))
	lower = strings.TrimSuffix(lower, ".")
	switch lower {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	default:
		return false
	}
}
