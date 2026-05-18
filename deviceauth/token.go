package deviceauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ---------------------------------------------------------------------------
// Token constants
// ---------------------------------------------------------------------------

const (
	tokenLength = 32 // bytes; hex-encoded to 64 chars
	tokenPrefix = "hc_"

	// tokenHexLength is the expected number of hex characters after the prefix.
	tokenHexLength = tokenLength * 2 // 64
)

// ---------------------------------------------------------------------------
// Token generation
// ---------------------------------------------------------------------------

// GenerateToken creates a new cryptographically-secure random token
// prefixed with "hc_".
func GenerateToken() (string, error) {
	b := make([]byte, tokenLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return tokenPrefix + hex.EncodeToString(b), nil
}

// GenerateDeviceID creates a new UUID v4 style device identifier using
// crypto/rand.
func GenerateDeviceID() string {
	var uuid [16]byte
	// Errors from crypto/rand.Read are practically impossible on modern OS.
	_, _ = rand.Read(uuid[:])

	// Set version bits (v4).
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	// Set variant bits (RFC 4122).
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}

// ---------------------------------------------------------------------------
// Token validation
// ---------------------------------------------------------------------------

// ValidateToken checks the format of a device token. It verifies the prefix,
// overall length, and that the suffix contains only hex characters.
func ValidateToken(token string) error {
	if len(token) < len(tokenPrefix) || token[:len(tokenPrefix)] != tokenPrefix {
		return fmt.Errorf("token missing required prefix %q", tokenPrefix)
	}
	hexPart := token[len(tokenPrefix):]
	if len(hexPart) != tokenHexLength {
		return fmt.Errorf("token hex part length %d, want %d", len(hexPart), tokenHexLength)
	}
	for _, c := range hexPart {
		if !isHexChar(c) {
			return fmt.Errorf("token contains non-hex character %q", c)
		}
	}
	return nil
}

// HashToken returns a hex-encoded SHA-256 hash of the token for safe storage
// and comparison.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
