package deviceauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Pairing crypto constants
// ---------------------------------------------------------------------------

const (
	pairingTTL   = 5 * time.Minute
	nonceLength  = 32
	pemBlockType = "PUBLIC KEY"
)

// ---------------------------------------------------------------------------
// PairingChallenge
// ---------------------------------------------------------------------------

// PairingChallenge represents a cryptographic pairing challenge using ECDSA
// P-256. The device generates a key pair, signs the server-provided nonce, and
// the server verifies the signature to prove possession of the private key.
type PairingChallenge struct {
	RequestID    string    `json:"request_id"`
	DeviceID     string    `json:"device_id"`
	PublicKeyPEM string    `json:"public_key"` // PEM-encoded ECDSA public key
	DisplayName  string    `json:"display_name"`
	Platform     string    `json:"platform"`
	Nonce        string    `json:"nonce"`     // base64-encoded random nonce
	Signature    string    `json:"signature"` // base64 signature of nonce by device
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// IsExpired reports whether the challenge has expired.
func (c *PairingChallenge) IsExpired() bool {
	return time.Now().UTC().After(c.ExpiresAt)
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	// ErrChallengeExpired indicates the pairing challenge has expired.
	ErrChallengeExpired = fmt.Errorf("pairing challenge expired")

	// ErrInvalidSignature indicates the ECDSA signature verification failed.
	ErrInvalidSignature = fmt.Errorf("invalid signature")

	// ErrInvalidPublicKey indicates the PEM-encoded public key could not be
	// parsed.
	ErrInvalidPublicKey = fmt.Errorf("invalid public key")
)

// ---------------------------------------------------------------------------
// Key generation
// ---------------------------------------------------------------------------

// GenerateKeyPair generates a new ECDSA P-256 key pair for device pairing.
// It returns the private key, the PEM-encoded public key, and any error.
func GenerateKeyPair() (*ecdsa.PrivateKey, string, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("generate ecdsa key: %w", err)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, "", fmt.Errorf("marshal public key: %w", err)
	}

	pemBlock := &pem.Block{
		Type:  pemBlockType,
		Bytes: pubBytes,
	}
	publicKeyPEM := string(pem.EncodeToMemory(pemBlock))

	return privateKey, publicKeyPEM, nil
}

// ---------------------------------------------------------------------------
// Challenge creation
// ---------------------------------------------------------------------------

// CreateChallenge creates a new pairing challenge with a cryptographically
// random nonce and a 5-minute TTL. The caller is expected to send the nonce to
// the device which signs it with its private key and returns the signature.
func CreateChallenge(deviceID, publicKeyPEM, displayName, platform string) (*PairingChallenge, error) {
	nonce, err := generateNonce()
	if err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	requestID, err := generateRequestID()
	if err != nil {
		return nil, fmt.Errorf("generate request id: %w", err)
	}

	now := time.Now().UTC()
	return &PairingChallenge{
		RequestID:    requestID,
		DeviceID:     deviceID,
		PublicKeyPEM: publicKeyPEM,
		DisplayName:  displayName,
		Platform:     platform,
		Nonce:        nonce,
		CreatedAt:    now,
		ExpiresAt:    now.Add(pairingTTL),
	}, nil
}

// ---------------------------------------------------------------------------
// Challenge verification
// ---------------------------------------------------------------------------

// VerifyChallenge verifies the device's ECDSA signature of the nonce in the
// pairing challenge. It decodes the PEM public key, decodes the base64
// signature, hashes the nonce with SHA-256, and verifies the ECDSA signature.
func VerifyChallenge(challenge *PairingChallenge) error {
	if challenge.IsExpired() {
		return ErrChallengeExpired
	}

	// Decode PEM public key.
	pubKey, err := decodePEMPublicKey(challenge.PublicKeyPEM)
	if err != nil {
		return err
	}

	// Decode base64 signature.
	sigBytes, err := base64.StdEncoding.DecodeString(challenge.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	// Hash nonce with SHA-256.
	hash := sha256.Sum256([]byte(challenge.Nonce))

	// Verify ECDSA signature.
	if !ecdsa.VerifyASN1(pubKey, hash[:], sigBytes) {
		return ErrInvalidSignature
	}

	return nil
}

// ---------------------------------------------------------------------------
// Signing (device side)
// ---------------------------------------------------------------------------

// SignNonce signs a nonce with the given ECDSA private key and returns the
// base64-encoded ASN.1 DER signature. This is intended for the device side of
// the pairing flow.
func SignNonce(privateKey *ecdsa.PrivateKey, nonce string) (string, error) {
	hash := sha256.Sum256([]byte(nonce))

	sig, err := ecdsa.SignASN1(rand.Reader, privateKey, hash[:])
	if err != nil {
		return "", fmt.Errorf("sign nonce: %w", err)
	}

	return base64.StdEncoding.EncodeToString(sig), nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// decodePEMPublicKey decodes a PEM-encoded ECDSA public key.
func decodePEMPublicKey(pemStr string) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("%w: no PEM block found", ErrInvalidPublicKey)
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPublicKey, err)
	}

	ecdsaPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: not an ECDSA key", ErrInvalidPublicKey)
	}

	return ecdsaPub, nil
}

// generateNonce produces a cryptographically random base64-encoded nonce.
func generateNonce() (string, error) {
	b := make([]byte, nonceLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// generateRequestID produces a short random identifier for pairing requests.
func generateRequestID() (string, error) {
	const requestIDBytes = 8
	b := make([]byte, requestIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return fmt.Sprintf("pair_%x", b), nil
}
