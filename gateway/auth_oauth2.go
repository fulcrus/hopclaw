package gateway

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// OAuth2/OIDC auth provider
// ---------------------------------------------------------------------------

const (
	oauth2ProviderName        = "oauth2"
	oauth2StateCookieName     = "_hopclaw_oauth2_state"
	oauth2NonceCookieName     = "_hopclaw_oauth2_nonce"
	oauth2PKCECookieName      = "_hopclaw_oauth2_pkce"
	oauth2StateHMACKeyBytes   = 32
	oauth2PKCEVerifierBytes   = 32
	oauth2StateTTL            = 10 * time.Minute
	oauth2DiscoveryCacheTTL   = 1 * time.Hour
	oauth2JWKSCacheTTL        = 1 * time.Hour
	oauth2HTTPClientTimeout   = 10 * time.Second
	oauth2NonceBytes          = 16
	oauth2CodeExchangeTimeout = 30 * time.Second

	// Well-known OIDC discovery path.
	oidcDiscoveryPath = "/.well-known/openid-configuration"
)

// OAuth2Config holds configuration for the OAuth2/OIDC provider.
type OAuth2Config struct {
	Issuer       string   `json:"issuer" yaml:"issuer"`
	ClientID     string   `json:"client_id" yaml:"client_id"`
	ClientSecret string   `json:"client_secret" yaml:"client_secret"`
	RedirectURI  string   `json:"redirect_uri" yaml:"redirect_uri"`
	Scopes       []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	DiscoveryURL string   `json:"discovery_url,omitempty" yaml:"discovery_url,omitempty"`
}

// OAuth2Provider implements AuthProvider using OAuth2 Authorization Code flow
// with OIDC token validation. It supports:
//   - OIDC Discovery (/.well-known/openid-configuration) with cached endpoints
//   - JWKS endpoint fetching and caching with rotation support
//   - RS256/ES256 signature verification
//   - PKCE (S256) for public clients
//   - State parameter with HMAC anti-forgery
//   - Nonce validation for ID token replay protection
type OAuth2Provider struct {
	config            OAuth2Config
	stateKey          []byte // HMAC key for state anti-forgery
	sessions          AuthSessionStore
	authSessionConfig AuthSessionConfig

	mu        sync.RWMutex // guards discovery and jwks caches
	discovery *oidcDiscoveryDoc
	discAt    time.Time
	jwks      *jwksCache
	jwksAt    time.Time

	httpClient *http.Client
}

// oidcDiscoveryDoc holds the fields we need from the OIDC discovery response.
type oidcDiscoveryDoc struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// jwksCache caches the JSON Web Key Set from the OIDC provider.
type jwksCache struct {
	Keys []jwkKey `json:"keys"`
}

// jwkKey represents a single key from the JWKS endpoint.
type jwkKey struct {
	KID string `json:"kid"`
	KTY string `json:"kty"`
	ALG string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`   // RSA modulus
	E   string `json:"e"`   // RSA exponent
	X   string `json:"x"`   // EC x coordinate
	Y   string `json:"y"`   // EC y coordinate
	CRV string `json:"crv"` // EC curve name
}

// oauth2TokenResponse is the token endpoint response.
type oauth2TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// oauth2IDTokenClaims holds the claims we extract from the ID token.
type oauth2IDTokenClaims struct {
	Issuer   string   `json:"iss"`
	Subject  string   `json:"sub"`
	Audience jwtAud   `json:"aud"`
	Expire   float64  `json:"exp"`
	IssuedAt float64  `json:"iat"`
	Nonce    string   `json:"nonce"`
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Groups   []string `json:"groups"`
}

// NewOAuth2Provider creates an OAuth2/OIDC auth provider from the given config.
func NewOAuth2Provider(cfg OAuth2Config, sessions AuthSessionStore, sessionCfg AuthSessionConfig) (*OAuth2Provider, error) {
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, fmt.Errorf("oauth2: client_id is required")
	}
	if strings.TrimSpace(cfg.RedirectURI) == "" {
		return nil, fmt.Errorf("oauth2: redirect_uri is required")
	}
	if strings.TrimSpace(cfg.Issuer) == "" && strings.TrimSpace(cfg.DiscoveryURL) == "" {
		return nil, fmt.Errorf("oauth2: issuer or discovery_url is required")
	}

	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}

	stateKey := make([]byte, oauth2StateHMACKeyBytes)
	if _, err := rand.Read(stateKey); err != nil {
		return nil, fmt.Errorf("oauth2: generate state key: %w", err)
	}

	return &OAuth2Provider{
		config:            cfg,
		stateKey:          stateKey,
		sessions:          sessions,
		authSessionConfig: defaultAuthSessionConfig(sessionCfg),
		httpClient: &http.Client{
			Timeout: oauth2HTTPClientTimeout,
		},
	}, nil
}

// Name returns "oauth2".
func (p *OAuth2Provider) Name() string { return oauth2ProviderName }

// Authenticate checks for a valid session cookie set after OAuth2 login.
// Returns (nil, nil) if no session cookie is present (allowing the auth chain
// to try other providers).
func (p *OAuth2Provider) Authenticate(r *http.Request) (*AuthIdentity, error) {
	cookie, err := r.Cookie(authSessionCookieName(p.authSessionConfig.CookieName))
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return nil, nil
	}

	session, ok := p.sessions.Get(cookie.Value)
	if !ok {
		return nil, nil
	}

	if time.Now().After(session.ExpiresAt) {
		p.sessions.Delete(cookie.Value)
		return nil, nil
	}

	// Update last seen.
	p.sessions.Touch(cookie.Value)

	return session.Identity, nil
}

// ---------------------------------------------------------------------------
// HTTP handlers for OAuth2 login flow
// ---------------------------------------------------------------------------

// HandleLogin redirects the user to the OIDC authorization endpoint with
// state, nonce, and PKCE parameters.
func (p *OAuth2Provider) HandleLogin(w http.ResponseWriter, r *http.Request) {
	disc, err := p.getDiscovery(r.Context())
	if err != nil {
		gwError(w, http.StatusInternalServerError, fmt.Sprintf("oauth2: discovery failed: %s", err))
		return
	}

	// Generate state with HMAC.
	state, err := p.generateState()
	if err != nil {
		gwError(w, http.StatusInternalServerError, "oauth2: failed to generate state")
		return
	}

	// Generate nonce.
	nonce, err := generateRandomString(oauth2NonceBytes)
	if err != nil {
		gwError(w, http.StatusInternalServerError, "oauth2: failed to generate nonce")
		return
	}

	// Generate PKCE code verifier and challenge.
	verifier, challenge, err := generatePKCE()
	if err != nil {
		gwError(w, http.StatusInternalServerError, "oauth2: failed to generate PKCE")
		return
	}

	// Set cookies for state, nonce, and PKCE verifier.
	setManagedCookie(w, r, p.authSessionConfig, oauth2StateCookieName, state, oauth2StateTTL, true)
	setManagedCookie(w, r, p.authSessionConfig, oauth2NonceCookieName, nonce, oauth2StateTTL, true)
	setManagedCookie(w, r, p.authSessionConfig, oauth2PKCECookieName, verifier, oauth2StateTTL, true)

	// Build authorization URL.
	authURL, err := url.Parse(disc.AuthorizationEndpoint)
	if err != nil {
		gwError(w, http.StatusInternalServerError, "oauth2: invalid authorization endpoint")
		return
	}

	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.config.ClientID)
	q.Set("redirect_uri", p.config.RedirectURI)
	q.Set("scope", strings.Join(p.config.Scopes, " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	authURL.RawQuery = q.Encode()

	http.Redirect(w, r, authURL.String(), http.StatusFound)
}

// HandleCallback processes the OAuth2 callback, exchanges the code for tokens,
// validates the ID token, creates a session, and sets a session cookie.
func (p *OAuth2Provider) HandleCallback(w http.ResponseWriter, r *http.Request) {
	// Verify state.
	stateCookie, err := r.Cookie(oauth2StateCookieName)
	if err != nil || strings.TrimSpace(stateCookie.Value) == "" {
		gwError(w, http.StatusBadRequest, "oauth2: missing state cookie")
		return
	}
	stateParam := r.URL.Query().Get("state")
	if !p.verifyState(stateCookie.Value, stateParam) {
		gwError(w, http.StatusBadRequest, "oauth2: state mismatch")
		return
	}

	// Check for error from provider.
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		gwError(w, http.StatusBadRequest, fmt.Sprintf("oauth2: provider error: %s: %s", errParam, desc))
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		gwError(w, http.StatusBadRequest, "oauth2: missing authorization code")
		return
	}

	// Get PKCE verifier.
	pkceCookie, err := r.Cookie(oauth2PKCECookieName)
	if err != nil || strings.TrimSpace(pkceCookie.Value) == "" {
		gwError(w, http.StatusBadRequest, "oauth2: missing PKCE verifier")
		return
	}

	// Get nonce for validation.
	nonceCookie, err := r.Cookie(oauth2NonceCookieName)
	if err != nil || strings.TrimSpace(nonceCookie.Value) == "" {
		gwError(w, http.StatusBadRequest, "oauth2: missing nonce cookie")
		return
	}

	// Clear one-time cookies.
	clearManagedCookie(w, r, p.authSessionConfig, oauth2StateCookieName, true)
	clearManagedCookie(w, r, p.authSessionConfig, oauth2NonceCookieName, true)
	clearManagedCookie(w, r, p.authSessionConfig, oauth2PKCECookieName, true)

	// Exchange code for tokens.
	ctx, cancel := context.WithTimeout(r.Context(), oauth2CodeExchangeTimeout)
	defer cancel()

	tokenResp, err := p.exchangeCode(ctx, code, pkceCookie.Value)
	if err != nil {
		gwError(w, http.StatusInternalServerError, fmt.Sprintf("oauth2: code exchange failed: %s", err))
		return
	}

	// Validate ID token.
	claims, err := p.validateIDToken(r.Context(), tokenResp.IDToken, nonceCookie.Value)
	if err != nil {
		gwError(w, http.StatusUnauthorized, fmt.Sprintf("oauth2: id token validation failed: %s", err))
		return
	}

	// Build identity.
	identity := &AuthIdentity{
		Subject:  claims.Subject,
		Provider: oauth2ProviderName,
		Metadata: make(map[string]string),
	}
	if claims.Email != "" {
		identity.Metadata["email"] = claims.Email
	}
	if claims.Name != "" {
		identity.Metadata["name"] = claims.Name
	}
	if len(claims.Groups) > 0 {
		identity.Metadata["groups"] = strings.Join(claims.Groups, ",")
		identity.Scopes = claims.Groups
	}

	// Create session.
	session := p.sessions.Create(identity)

	// Set auth-session cookies.
	setAuthSessionCookies(w, r, p.authSessionConfig, session)

	// Redirect to the canonical console entry.
	http.Redirect(w, r, "/dashboard/", http.StatusFound)
}

// ---------------------------------------------------------------------------
// OIDC Discovery
// ---------------------------------------------------------------------------

func (p *OAuth2Provider) getDiscovery(ctx context.Context) (*oidcDiscoveryDoc, error) {
	p.mu.RLock()
	if p.discovery != nil && time.Since(p.discAt) < oauth2DiscoveryCacheTTL {
		disc := p.discovery
		p.mu.RUnlock()
		return disc, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock.
	if p.discovery != nil && time.Since(p.discAt) < oauth2DiscoveryCacheTTL {
		return p.discovery, nil
	}

	discoveryURL := strings.TrimSpace(p.config.DiscoveryURL)
	if discoveryURL == "" {
		issuer := strings.TrimRight(p.config.Issuer, "/")
		discoveryURL = issuer + oidcDiscoveryPath
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build discovery request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch discovery document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery endpoint returned %d", resp.StatusCode)
	}

	var doc oidcDiscoveryDoc
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode discovery document: %w", err)
	}

	p.discovery = &doc
	p.discAt = time.Now()
	return &doc, nil
}

// ---------------------------------------------------------------------------
// JWKS fetching and caching
// ---------------------------------------------------------------------------

func (p *OAuth2Provider) getJWKS(ctx context.Context) (*jwksCache, error) {
	p.mu.RLock()
	if p.jwks != nil && time.Since(p.jwksAt) < oauth2JWKSCacheTTL {
		jwks := p.jwks
		p.mu.RUnlock()
		return jwks, nil
	}
	p.mu.RUnlock()

	return p.refreshJWKS(ctx)
}

func (p *OAuth2Provider) refreshJWKS(ctx context.Context) (*jwksCache, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check.
	if p.jwks != nil && time.Since(p.jwksAt) < oauth2JWKSCacheTTL {
		return p.jwks, nil
	}

	disc, err := p.getDiscoveryLocked(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, disc.JWKSURI, nil)
	if err != nil {
		return nil, fmt.Errorf("build jwks request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks endpoint returned %d", resp.StatusCode)
	}

	var cache jwksCache
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&cache); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}

	p.jwks = &cache
	p.jwksAt = time.Now()
	return &cache, nil
}

// getDiscoveryLocked returns discovery doc; caller must hold p.mu write lock.
func (p *OAuth2Provider) getDiscoveryLocked(ctx context.Context) (*oidcDiscoveryDoc, error) {
	if p.discovery != nil {
		return p.discovery, nil
	}

	discoveryURL := strings.TrimSpace(p.config.DiscoveryURL)
	if discoveryURL == "" {
		issuer := strings.TrimRight(p.config.Issuer, "/")
		discoveryURL = issuer + oidcDiscoveryPath
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build discovery request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch discovery document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery endpoint returned %d", resp.StatusCode)
	}

	var doc oidcDiscoveryDoc
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode discovery document: %w", err)
	}

	p.discovery = &doc
	p.discAt = time.Now()
	return &doc, nil
}

// ---------------------------------------------------------------------------
// Token exchange
// ---------------------------------------------------------------------------

func (p *OAuth2Provider) exchangeCode(ctx context.Context, code, pkceVerifier string) (*oauth2TokenResponse, error) {
	disc, err := p.getDiscovery(ctx)
	if err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.config.RedirectURI},
		"client_id":     {p.config.ClientID},
		"code_verifier": {pkceVerifier},
	}
	if p.config.ClientSecret != "" {
		data.Set("client_secret", p.config.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp oauth2TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	if tokenResp.IDToken == "" {
		return nil, fmt.Errorf("token response missing id_token")
	}

	return &tokenResp, nil
}

// ---------------------------------------------------------------------------
// ID token validation
// ---------------------------------------------------------------------------

func (p *OAuth2Provider) validateIDToken(ctx context.Context, rawToken, expectedNonce string) (*oauth2IDTokenClaims, error) {
	parts := strings.SplitN(rawToken, ".", jwtPartsCount)
	if len(parts) != jwtPartsCount {
		return nil, fmt.Errorf("malformed id token")
	}

	// Parse header.
	headerBytes, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	var hdr jwtHeader
	if err := json.Unmarshal(headerBytes, &hdr); err != nil {
		return nil, fmt.Errorf("parse header: %w", err)
	}

	// Verify signature using JWKS.
	signingInput := parts[0] + "." + parts[1]
	signature, err := base64URLDecode(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	if err := p.verifyJWKSSignature(ctx, hdr, signingInput, signature); err != nil {
		return nil, fmt.Errorf("signature verification: %w", err)
	}

	// Parse claims.
	claimsBytes, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}
	var claims oauth2IDTokenClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}

	// Validate issuer.
	expectedIssuer := strings.TrimRight(p.config.Issuer, "/")
	actualIssuer := strings.TrimRight(claims.Issuer, "/")
	if expectedIssuer != "" && actualIssuer != expectedIssuer {
		return nil, fmt.Errorf("issuer mismatch: got %q", claims.Issuer)
	}

	// Validate audience.
	if !audContains(claims.Audience, p.config.ClientID) {
		return nil, fmt.Errorf("audience mismatch: expected %q", p.config.ClientID)
	}

	// Validate expiry.
	now := float64(time.Now().Unix())
	if claims.Expire > 0 && now > claims.Expire+jwtDefaultClockSkew.Seconds() {
		return nil, fmt.Errorf("id token expired")
	}

	// Validate nonce.
	if expectedNonce != "" && claims.Nonce != expectedNonce {
		return nil, fmt.Errorf("nonce mismatch")
	}

	return &claims, nil
}

func (p *OAuth2Provider) verifyJWKSSignature(ctx context.Context, hdr jwtHeader, signingInput string, signature []byte) error {
	jwks, err := p.getJWKS(ctx)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}

	// Find matching key.
	key, err := findJWKKey(jwks, hdr.Algorithm, "")
	if err != nil {
		// Key not found; try refreshing JWKS (key rotation).
		jwks, err = p.refreshJWKS(ctx)
		if err != nil {
			return fmt.Errorf("refresh jwks: %w", err)
		}
		key, err = findJWKKey(jwks, hdr.Algorithm, "")
		if err != nil {
			return err
		}
	}

	switch hdr.Algorithm {
	case "RS256":
		pubKey, err := jwkToRSAPublicKey(key)
		if err != nil {
			return fmt.Errorf("parse RSA key: %w", err)
		}
		hash := sha256.Sum256([]byte(signingInput))
		return rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], signature)

	case "ES256":
		pubKey, err := jwkToECDSAPublicKey(key)
		if err != nil {
			return fmt.Errorf("parse ECDSA key: %w", err)
		}
		hash := sha256.Sum256([]byte(signingInput))
		if !ecdsa.VerifyASN1(pubKey, hash[:], signature) {
			return fmt.Errorf("invalid ECDSA signature")
		}
		return nil

	default:
		return fmt.Errorf("unsupported id token algorithm %q", hdr.Algorithm)
	}
}

func findJWKKey(jwks *jwksCache, alg, kid string) (jwkKey, error) {
	for _, k := range jwks.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		if kid != "" && k.KID != kid {
			continue
		}
		if k.ALG != "" && k.ALG != alg {
			continue
		}
		// Match key type to algorithm.
		switch alg {
		case "RS256":
			if k.KTY == "RSA" {
				return k, nil
			}
		case "ES256":
			if k.KTY == "EC" {
				return k, nil
			}
		}
	}
	return jwkKey{}, fmt.Errorf("no matching key found for algorithm %q", alg)
}

func jwkToRSAPublicKey(k jwkKey) (*rsa.PublicKey, error) {
	nBytes, err := base64URLDecode(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}
	eBytes, err := base64URLDecode(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}

func jwkToECDSAPublicKey(k jwkKey) (*ecdsa.PublicKey, error) {
	xBytes, err := base64URLDecode(k.X)
	if err != nil {
		return nil, fmt.Errorf("decode x coordinate: %w", err)
	}
	yBytes, err := base64URLDecode(k.Y)
	if err != nil {
		return nil, fmt.Errorf("decode y coordinate: %w", err)
	}

	var curve elliptic.Curve
	switch k.CRV {
	case "P-256", "":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported curve %q", k.CRV)
	}

	return &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}, nil
}

// ---------------------------------------------------------------------------
// State, nonce, and PKCE helpers
// ---------------------------------------------------------------------------

func (p *OAuth2Provider) generateState() (string, error) {
	raw, err := generateRandomString(oauth2StateHMACKeyBytes)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, p.stateKey)
	mac.Write([]byte(raw))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return raw + "." + sig, nil
}

func (p *OAuth2Provider) verifyState(cookie, param string) bool {
	if cookie != param {
		return false
	}
	parts := strings.SplitN(param, ".", 2)
	if len(parts) != 2 {
		return false
	}
	mac := hmac.New(sha256.New, p.stateKey)
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

func generatePKCE() (verifier, challenge string, err error) {
	raw := make([]byte, oauth2PKCEVerifierBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])
	return verifier, challenge, nil
}

func generateRandomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ---------------------------------------------------------------------------
// Cookie helpers
// ---------------------------------------------------------------------------
