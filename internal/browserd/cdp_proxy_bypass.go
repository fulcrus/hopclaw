package browserd

import (
	"os"
	"strings"
)

// loopbackEntries are the addresses that CDP connects to locally and must
// bypass any configured HTTP proxy.
const loopbackEntries = "localhost,127.0.0.1,::1"

// EnsureCDPProxyBypass temporarily adds loopback entries to NO_PROXY
// to prevent CDP connections from being routed through HTTP proxies.
// Returns a cleanup function that restores the original NO_PROXY value.
func EnsureCDPProxyBypass() func() {
	httpProxy := os.Getenv("HTTP_PROXY")
	httpsProxy := os.Getenv("HTTPS_PROXY")

	// Also check lowercase variants.
	if httpProxy == "" {
		httpProxy = os.Getenv("http_proxy")
	}
	if httpsProxy == "" {
		httpsProxy = os.Getenv("https_proxy")
	}

	// No proxy configured — nothing to bypass.
	if httpProxy == "" && httpsProxy == "" {
		return func() {}
	}

	// Read current NO_PROXY (prefer uppercase, fall back to lowercase).
	envKey := "NO_PROXY"
	original := os.Getenv(envKey)
	if original == "" {
		if v := os.Getenv("no_proxy"); v != "" {
			envKey = "no_proxy"
			original = v
		}
	}

	// Check if all loopback entries are already present.
	if allLoopbackPresent(original) {
		return func() {}
	}

	// Append loopback entries.
	updated := original
	if updated != "" {
		updated += ","
	}
	updated += loopbackEntries
	os.Setenv(envKey, updated)

	return func() {
		if original == "" {
			os.Unsetenv(envKey)
		} else {
			os.Setenv(envKey, original)
		}
	}
}

// allLoopbackPresent returns true if NO_PROXY already contains all three
// loopback addresses (localhost, 127.0.0.1, ::1).
func allLoopbackPresent(noProxy string) bool {
	if noProxy == "" {
		return false
	}
	entries := strings.Split(noProxy, ",")
	found := map[string]bool{
		"localhost": false,
		"127.0.0.1": false,
		"::1":       false,
	}
	for _, e := range entries {
		trimmed := strings.TrimSpace(e)
		if _, ok := found[trimmed]; ok {
			found[trimmed] = true
		}
	}
	for _, v := range found {
		if !v {
			return false
		}
	}
	return true
}
