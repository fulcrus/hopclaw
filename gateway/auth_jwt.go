package gateway

import (
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// JWT auth provider (HS256 + RS256, stdlib only)
// ---------------------------------------------------------------------------

const (
	jwtDefaultClockSkew = 60 * time.Second
	jwtDefaultAlgorithm = "HS256"
	jwtAlgorithmRS256   = "RS256"
	jwtPartsCount       = 3
)

// JWTConfig holds the configuration for JWT validation.
type JWTConfig struct {
	Secret    string        `yaml:"secret" json:"secret,omitempty"`
	PublicKey string        `yaml:"public_key" json:"public_key,omitempty"` // reserved for RSA/ECDSA
	Issuer    string        `yaml:"issuer" json:"issuer,omitempty"`
	Audience  string        `yaml:"audience" json:"audience,omitempty"`
	Algorithm string        `yaml:"algorithm" json:"algorithm,omitempty"`
	ClockSkew time.Duration `yaml:"clock_skew" json:"clock_skew,omitempty"`
}

// JWTProvider validates JWTs using HMAC-SHA256 (HS256) or RSA-SHA256 (RS256).
// External libraries are intentionally avoided — only crypto/hmac, crypto/rsa,
// crypto/x509, and encoding/base64 from the standard library are used.
type JWTProvider struct {
	secret    []byte         // HMAC key (HS256)
	publicKey *rsa.PublicKey // RSA public key (RS256)
	issuer    string         // expected "iss" claim
	audience  string         // expected "aud" claim
	algorithm string         // "HS256" or "RS256"
	clockSkew time.Duration  // allowed clock skew for exp/nbf checks
}

// NewJWTProvider creates a JWTProvider from the given config.
// For HS256, cfg.Secret must be set. For RS256, cfg.PublicKey must contain
// a PEM-encoded RSA public key (PKCS1 or PKIX).
func NewJWTProvider(cfg JWTConfig) (*JWTProvider, error) {
	alg := strings.TrimSpace(cfg.Algorithm)
	if alg == "" {
		// Auto-detect algorithm from provided credentials.
		if strings.TrimSpace(cfg.PublicKey) != "" {
			alg = jwtAlgorithmRS256
		} else {
			alg = jwtDefaultAlgorithm
		}
	}

	clockSkew := cfg.ClockSkew
	if clockSkew <= 0 {
		clockSkew = jwtDefaultClockSkew
	}

	provider := &JWTProvider{
		issuer:    strings.TrimSpace(cfg.Issuer),
		audience:  strings.TrimSpace(cfg.Audience),
		algorithm: alg,
		clockSkew: clockSkew,
	}

	switch alg {
	case "HS256":
		secret := strings.TrimSpace(cfg.Secret)
		if secret == "" {
			return nil, fmt.Errorf("jwt secret is required for HS256")
		}
		provider.secret = []byte(secret)

	case jwtAlgorithmRS256:
		pubKeyPEM := strings.TrimSpace(cfg.PublicKey)
		if pubKeyPEM == "" {
			return nil, fmt.Errorf("jwt public_key is required for RS256")
		}
		pubKey, err := parseRSAPublicKey(pubKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("jwt public_key: %w", err)
		}
		provider.publicKey = pubKey

	default:
		return nil, fmt.Errorf("unsupported jwt algorithm %q, supported: HS256, RS256", alg)
	}

	return provider, nil
}

// parseRSAPublicKey decodes a PEM-encoded RSA public key.
// It supports both PKCS1 ("RSA PUBLIC KEY") and PKIX ("PUBLIC KEY") formats.
func parseRSAPublicKey(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	switch block.Type {
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	case "PUBLIC KEY":
		pub, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaPub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("not an RSA public key")
		}
		return rsaPub, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q", block.Type)
	}
}

// Name returns "jwt".
func (p *JWTProvider) Name() string { return "jwt" }

// Authenticate extracts and validates a JWT from the Authorization: Bearer
// header. Returns (nil, nil) when no bearer token is present or the token
// does not look like a JWT (no dots). Returns (nil, error) when the token
// is a JWT but validation fails.
func (p *JWTProvider) Authenticate(r *http.Request) (*AuthIdentity, error) {
	raw := strings.TrimSpace(r.Header.Get(headerAuthorization))
	if raw == "" {
		return nil, nil
	}
	if !strings.HasPrefix(strings.ToLower(raw), bearerPrefix) {
		return nil, nil
	}
	token := strings.TrimSpace(raw[len(bearerPrefix):])
	if token == "" {
		return nil, nil
	}

	// Only attempt JWT validation if the token has the three-part structure.
	if strings.Count(token, ".") != jwtPartsCount-1 {
		return nil, nil
	}

	claims, err := p.validate(token)
	if err != nil {
		return nil, fmt.Errorf("jwt: %w", err)
	}

	return &AuthIdentity{
		Subject:  claims.Subject,
		Provider: p.Name(),
		Scopes:   claims.scopeSet(),
		Metadata: claims.metadata(),
	}, nil
}

// ---------------------------------------------------------------------------
// Internal JWT parsing / validation
// ---------------------------------------------------------------------------

type jwtHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

type jwtClaims struct {
	Subject   string        `json:"sub"`
	Issuer    string        `json:"iss"`
	Audience  jwtAud        `json:"aud"`
	Scopes    jwtStringList `json:"scopes"`
	Scope     jwtStringList `json:"scope"`
	Role      string        `json:"role"`
	Groups    jwtStringList `json:"groups"`
	IssuedAt  float64       `json:"iat"`
	Expire    float64       `json:"exp"`
	NotBefore float64       `json:"nbf"`
}

// jwtAud handles the "aud" claim being either a string or an array of strings.
type jwtAud []string

type jwtStringList []string

func (a *jwtAud) UnmarshalJSON(data []byte) error {
	// Try single string first.
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = []string{single}
		return nil
	}
	// Try array of strings.
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("aud must be a string or array of strings")
	}
	*a = list
	return nil
}

func (l *jwtStringList) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		single = strings.TrimSpace(single)
		if single == "" {
			*l = nil
			return nil
		}
		if strings.Contains(single, " ") {
			*l = strings.Fields(single)
			return nil
		}
		*l = []string{single}
		return nil
	}

	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("claim must be a string or array of strings")
	}
	*l = list
	return nil
}

func (c *jwtClaims) scopeSet() []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(c.Scopes)+len(c.Scope))
	for _, raw := range append(append([]string{}, c.Scopes...), c.Scope...) {
		scope := strings.TrimSpace(raw)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func (c *jwtClaims) metadata() map[string]string {
	var metadata map[string]string
	if role := strings.TrimSpace(c.Role); role != "" {
		if metadata == nil {
			metadata = make(map[string]string)
		}
		metadata[authMetadataKeyRole] = role
	}

	groups := make([]string, 0, len(c.Groups))
	for _, raw := range c.Groups {
		group := strings.TrimSpace(raw)
		if group == "" {
			continue
		}
		groups = append(groups, group)
	}
	if len(groups) > 0 {
		if metadata == nil {
			metadata = make(map[string]string)
		}
		metadata[authMetadataKeyGroups] = strings.Join(groups, ",")
	}
	return metadata
}

func (p *JWTProvider) validate(token string) (*jwtClaims, error) {
	parts := strings.SplitN(token, ".", jwtPartsCount)
	if len(parts) != jwtPartsCount {
		return nil, fmt.Errorf("malformed token")
	}

	headerBytes, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	var hdr jwtHeader
	if err := json.Unmarshal(headerBytes, &hdr); err != nil {
		return nil, fmt.Errorf("parse header: %w", err)
	}
	if hdr.Algorithm != p.algorithm {
		return nil, fmt.Errorf("unexpected algorithm %q", hdr.Algorithm)
	}

	// Verify signature.
	signingInput := parts[0] + "." + parts[1]
	signature, err := base64URLDecode(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	switch p.algorithm {
	case "HS256":
		if !verifyHS256(p.secret, signingInput, signature) {
			return nil, fmt.Errorf("invalid signature")
		}
	case jwtAlgorithmRS256:
		if err := verifyRS256(p.publicKey, signingInput, signature); err != nil {
			return nil, fmt.Errorf("invalid signature: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported algorithm %q", p.algorithm)
	}

	// Parse claims.
	claimsBytes, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}
	var claims jwtClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}

	// Validate time-based claims.
	now := float64(time.Now().Unix())
	skew := p.clockSkew.Seconds()

	if claims.Expire > 0 && now > claims.Expire+skew {
		return nil, fmt.Errorf("token expired")
	}
	if claims.NotBefore > 0 && now < claims.NotBefore-skew {
		return nil, fmt.Errorf("token not yet valid")
	}

	// Validate issuer.
	if p.issuer != "" && claims.Issuer != p.issuer {
		return nil, fmt.Errorf("unexpected issuer %q", claims.Issuer)
	}

	// Validate audience.
	if p.audience != "" {
		if !audContains(claims.Audience, p.audience) {
			return nil, fmt.Errorf("audience mismatch")
		}
	}

	return &claims, nil
}

func audContains(aud jwtAud, want string) bool {
	for _, a := range aud {
		if a == want {
			return true
		}
	}
	return false
}

func verifyHS256(secret []byte, signingInput string, signature []byte) bool {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	expected := mac.Sum(nil)
	return hmac.Equal(expected, signature)
}

// verifyRS256 verifies an RSA-SHA256 signature over the signing input.
func verifyRS256(pubKey *rsa.PublicKey, signingInput string, signature []byte) error {
	hash := sha256.Sum256([]byte(signingInput))
	return rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], signature)
}

// base64URLDecode decodes base64url-encoded data (no padding).
func base64URLDecode(s string) ([]byte, error) {
	// Add padding if necessary.
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
