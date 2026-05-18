package deviceauth

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// V2 round-trip
// ---------------------------------------------------------------------------

func TestEncodeDecodeV2(t *testing.T) {
	p := AuthPayload{
		DeviceID:   "d-100",
		ClientID:   "cli-1",
		ClientMode: "interactive",
		Role:       RoleOperator,
		Scopes:     []string{"read", "write"},
		SignedAtMs: time.Now().UnixMilli(),
		Token:      "hc_abc",
		Nonce:      "n-42",
	}

	encoded := EncodePayloadV2(p)
	if !strings.HasPrefix(encoded, "v2|") {
		t.Fatalf("expected v2 prefix, got %q", encoded)
	}

	decoded, err := DecodePayload(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Version != payloadV2 {
		t.Fatalf("version = %d, want %d", decoded.Version, payloadV2)
	}
	if decoded.DeviceID != p.DeviceID {
		t.Fatalf("device_id = %q, want %q", decoded.DeviceID, p.DeviceID)
	}
	if decoded.ClientID != p.ClientID {
		t.Fatalf("client_id = %q, want %q", decoded.ClientID, p.ClientID)
	}
	if decoded.Role != p.Role {
		t.Fatalf("role = %q, want %q", decoded.Role, p.Role)
	}
	if decoded.Token != p.Token {
		t.Fatalf("token = %q, want %q", decoded.Token, p.Token)
	}
	if decoded.Nonce != p.Nonce {
		t.Fatalf("nonce = %q, want %q", decoded.Nonce, p.Nonce)
	}
	// Scopes should be sorted.
	if len(decoded.Scopes) != 2 || decoded.Scopes[0] != "read" || decoded.Scopes[1] != "write" {
		t.Fatalf("scopes = %v, want [read write]", decoded.Scopes)
	}
}

// ---------------------------------------------------------------------------
// V3 round-trip
// ---------------------------------------------------------------------------

func TestEncodeDecodeV3(t *testing.T) {
	p := AuthPayload{
		DeviceID:     "d-200",
		ClientID:     "cli-2",
		ClientMode:   "headless",
		Role:         RoleNode,
		Scopes:       []string{"execute"},
		SignedAtMs:   time.Now().UnixMilli(),
		Token:        "hc_xyz",
		Nonce:        "n-99",
		Platform:     "linux",
		DeviceFamily: "server",
	}

	encoded := EncodePayloadV3(p)
	if !strings.HasPrefix(encoded, "v3|") {
		t.Fatalf("expected v3 prefix, got %q", encoded)
	}

	decoded, err := DecodePayload(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Version != payloadV3 {
		t.Fatalf("version = %d, want %d", decoded.Version, payloadV3)
	}
	if decoded.Platform != "linux" {
		t.Fatalf("platform = %q, want %q", decoded.Platform, "linux")
	}
	if decoded.DeviceFamily != "server" {
		t.Fatalf("device_family = %q, want %q", decoded.DeviceFamily, "server")
	}
}

// ---------------------------------------------------------------------------
// Invalid payloads
// ---------------------------------------------------------------------------

func TestDecodePayloadInvalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"unknown version", "v1|a|b|c"},
		{"v2 too few fields", "v2|a|b|c"},
		{"v2 too many fields", "v2|a|b|c|d|e|123|tok|nonce|extra"},
		{"v3 too few fields", "v3|a|b|c|d|e|123|tok|nonce"},
		{"v3 too many fields", "v3|a|b|c|d|e|123|tok|nonce|p|f|extra"},
		{"v2 bad signed_at_ms", "v2|a|b|c|d|e|notanumber|tok|nonce"},
		{"v3 bad signed_at_ms", "v3|a|b|c|d|e|notanumber|tok|nonce|p|f"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodePayload(tc.input)
			if err == nil {
				t.Fatal("expected error for invalid payload")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Stale payload
// ---------------------------------------------------------------------------

func TestValidatePayloadStale(t *testing.T) {
	p := &AuthPayload{
		DeviceID:   "d-1",
		Role:       RoleOperator,
		Nonce:      "n-1",
		SignedAtMs: time.Now().Add(-10 * time.Minute).UnixMilli(),
	}
	err := ValidatePayload(p)
	if err == nil {
		t.Fatal("expected stale payload error")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale error message, got: %v", err)
	}
}

func TestValidatePayloadFuture(t *testing.T) {
	p := &AuthPayload{
		DeviceID:   "d-1",
		Role:       RoleOperator,
		Nonce:      "n-1",
		SignedAtMs: time.Now().Add(10 * time.Minute).UnixMilli(),
	}
	err := ValidatePayload(p)
	if err == nil {
		t.Fatal("expected future payload error")
	}
	if !strings.Contains(err.Error(), "future") {
		t.Fatalf("expected future error message, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Missing required fields
// ---------------------------------------------------------------------------

func TestValidatePayloadMissingFields(t *testing.T) {
	now := time.Now().UnixMilli()

	cases := []struct {
		name    string
		payload AuthPayload
		errSub  string
	}{
		{
			name:    "missing device_id",
			payload: AuthPayload{Role: RoleOperator, Nonce: "n", SignedAtMs: now},
			errSub:  "device_id",
		},
		{
			name:    "invalid role",
			payload: AuthPayload{DeviceID: "d", Role: "unknown", Nonce: "n", SignedAtMs: now},
			errSub:  "invalid role",
		},
		{
			name:    "missing nonce",
			payload: AuthPayload{DeviceID: "d", Role: RoleOperator, SignedAtMs: now},
			errSub:  "nonce",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePayload(&tc.payload)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.errSub) {
				t.Fatalf("expected error containing %q, got: %v", tc.errSub, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Scope normalization
// ---------------------------------------------------------------------------

func TestScopeNormalization(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"single", "read", []string{"read"}},
		{"sorted", "write,read", []string{"read", "write"}},
		{"deduped", "read,read,write", []string{"read", "write"}},
		{"trimmed", " read , write , execute ", []string{"execute", "read", "write"}},
		{"empty entries", "read,,write,", []string{"read", "write"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeScopes(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("scopes = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("scopes[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Valid payload
// ---------------------------------------------------------------------------

func TestValidatePayloadOK(t *testing.T) {
	p := &AuthPayload{
		DeviceID:   "d-1",
		Role:       RoleViewer,
		Nonce:      "n-1",
		SignedAtMs: time.Now().UnixMilli(),
	}
	if err := ValidatePayload(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
