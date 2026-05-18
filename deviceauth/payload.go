package deviceauth

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Payload constants
// ---------------------------------------------------------------------------

const (
	payloadV2        = 2
	payloadV3        = 3
	payloadSeparator = "|"

	// payloadMaxAge is the staleness threshold for auth payloads (5 minutes).
	payloadMaxAge = 5 * time.Minute

	// payloadMaxAgeMs is payloadMaxAge expressed in milliseconds for wire comparison.
	payloadMaxAgeMs = int64(payloadMaxAge / time.Millisecond)

	// Field counts for each payload version.
	payloadV2FieldCount = 9  // v2|deviceId|clientId|clientMode|role|scopes|signedAtMs|token|nonce
	payloadV3FieldCount = 11 // v3 extends v2 with |platform|deviceFamily
)

// ---------------------------------------------------------------------------
// Encode
// ---------------------------------------------------------------------------

// EncodePayloadV2 encodes an auth payload in V2 wire format.
// Format: v2|{deviceId}|{clientId}|{clientMode}|{role}|{scopes}|{signedAtMs}|{token}|{nonce}
func EncodePayloadV2(p AuthPayload) string {
	scopes := normalizeScopesString(p.Scopes)
	return strings.Join([]string{
		"v2",
		p.DeviceID,
		p.ClientID,
		p.ClientMode,
		string(p.Role),
		scopes,
		strconv.FormatInt(p.SignedAtMs, 10),
		p.Token,
		p.Nonce,
	}, payloadSeparator)
}

// EncodePayloadV3 encodes an auth payload in V3 wire format, which extends V2
// with device metadata fields.
// Format: v3|{deviceId}|{clientId}|{clientMode}|{role}|{scopes}|{signedAtMs}|{token}|{nonce}|{platform}|{deviceFamily}
func EncodePayloadV3(p AuthPayload) string {
	scopes := normalizeScopesString(p.Scopes)
	return strings.Join([]string{
		"v3",
		p.DeviceID,
		p.ClientID,
		p.ClientMode,
		string(p.Role),
		scopes,
		strconv.FormatInt(p.SignedAtMs, 10),
		p.Token,
		p.Nonce,
		p.Platform,
		p.DeviceFamily,
	}, payloadSeparator)
}

// ---------------------------------------------------------------------------
// Decode
// ---------------------------------------------------------------------------

// DecodePayload decodes a V2 or V3 auth payload string.
func DecodePayload(raw string) (*AuthPayload, error) {
	fields := strings.Split(raw, payloadSeparator)
	if len(fields) < 1 {
		return nil, fmt.Errorf("empty payload")
	}

	version := fields[0]
	switch version {
	case "v2":
		return decodeV2(fields)
	case "v3":
		return decodeV3(fields)
	default:
		return nil, fmt.Errorf("unsupported payload version %q", version)
	}
}

func decodeV2(fields []string) (*AuthPayload, error) {
	if len(fields) != payloadV2FieldCount {
		return nil, fmt.Errorf("v2 payload: expected %d fields, got %d", payloadV2FieldCount, len(fields))
	}
	signedAtMs, err := strconv.ParseInt(fields[6], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("v2 payload: invalid signed_at_ms %q: %w", fields[6], err)
	}
	return &AuthPayload{
		Version:    payloadV2,
		DeviceID:   fields[1],
		ClientID:   fields[2],
		ClientMode: fields[3],
		Role:       DeviceRole(fields[4]),
		Scopes:     normalizeScopes(fields[5]),
		SignedAtMs: signedAtMs,
		Token:      fields[7],
		Nonce:      fields[8],
	}, nil
}

func decodeV3(fields []string) (*AuthPayload, error) {
	if len(fields) != payloadV3FieldCount {
		return nil, fmt.Errorf("v3 payload: expected %d fields, got %d", payloadV3FieldCount, len(fields))
	}
	signedAtMs, err := strconv.ParseInt(fields[6], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("v3 payload: invalid signed_at_ms %q: %w", fields[6], err)
	}
	return &AuthPayload{
		Version:      payloadV3,
		DeviceID:     fields[1],
		ClientID:     fields[2],
		ClientMode:   fields[3],
		Role:         DeviceRole(fields[4]),
		Scopes:       normalizeScopes(fields[5]),
		SignedAtMs:   signedAtMs,
		Token:        fields[7],
		Nonce:        fields[8],
		Platform:     fields[9],
		DeviceFamily: fields[10],
	}, nil
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

// ValidatePayload checks that a decoded auth payload has all required fields
// and is not stale.
func ValidatePayload(p *AuthPayload) error {
	if p.DeviceID == "" {
		return fmt.Errorf("payload missing device_id")
	}
	if !IsValidRole(p.Role) {
		return fmt.Errorf("payload has invalid role %q", p.Role)
	}
	if p.Nonce == "" {
		return fmt.Errorf("payload missing nonce")
	}

	now := time.Now().UnixMilli()
	age := now - p.SignedAtMs
	if age > payloadMaxAgeMs {
		return fmt.Errorf("payload is stale: age %dms exceeds max %dms", age, payloadMaxAgeMs)
	}
	if age < 0 {
		return fmt.Errorf("payload signed_at_ms is in the future")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Scope helpers
// ---------------------------------------------------------------------------

// normalizeScopes parses a comma-separated scope string, trims whitespace,
// removes empty entries, deduplicates, and sorts.
func normalizeScopes(raw string) []string {
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	seen := make(map[string]bool, len(parts))
	var result []string
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		result = append(result, s)
	}
	sort.Strings(result)
	return result
}

// normalizeScopesString returns scopes as a sorted, deduped, comma-separated
// string suitable for wire encoding.
func normalizeScopesString(scopes []string) string {
	if len(scopes) == 0 {
		return ""
	}
	sorted := make([]string, len(scopes))
	copy(sorted, scopes)
	sort.Strings(sorted)

	// Deduplicate.
	j := 0
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			sorted[j] = s
			j++
		}
	}
	return strings.Join(sorted[:j], ",")
}
