package toolruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

// ---------------------------------------------------------------------------
// crypto.hash tests
// ---------------------------------------------------------------------------

func TestCryptoHashStringSHA256(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-hash",
		Name: "crypto.hash",
		Input: map[string]any{
			"input":     "hello world",
			"algorithm": "sha256",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	var payload struct {
		Hash      string `json:"hash"`
		Algorithm string `json:"algorithm"`
		InputSize int64  `json:"input_size"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	want := sha256Hex([]byte("hello world"))
	if payload.Hash != want {
		t.Fatalf("hash = %q, want %q", payload.Hash, want)
	}
	if payload.Algorithm != "sha256" {
		t.Fatalf("algorithm = %q", payload.Algorithm)
	}
	if payload.InputSize != 11 {
		t.Fatalf("input_size = %d, want 11", payload.InputSize)
	}
}

func TestCryptoHashStringMD5(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-hash-md5",
		Name: "crypto.hash",
		Input: map[string]any{
			"input":     "test",
			"algorithm": "md5",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	var payload struct {
		Hash      string `json:"hash"`
		Algorithm string `json:"algorithm"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if payload.Algorithm != "md5" {
		t.Fatalf("algorithm = %q, want md5", payload.Algorithm)
	}
	// MD5 of "test" is known: 098f6bcd4621d373cade4e832627b4f6
	if payload.Hash != "098f6bcd4621d373cade4e832627b4f6" {
		t.Fatalf("hash = %q", payload.Hash)
	}
}

func TestCryptoHashDefaultAlgorithm(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-hash-default",
		Name: "crypto.hash",
		Input: map[string]any{
			"input": "test",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	var payload struct {
		Algorithm string `json:"algorithm"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if payload.Algorithm != "sha256" {
		t.Fatalf("default algorithm = %q, want sha256", payload.Algorithm)
	}
}

func TestCryptoHashFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("file content for hashing")
	if err := os.WriteFile(filepath.Join(root, "hashme.txt"), content, 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-hash-file",
		Name: "crypto.hash",
		Input: map[string]any{
			"file":      "hashme.txt",
			"algorithm": "sha256",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	var payload struct {
		Hash      string `json:"hash"`
		InputSize int64  `json:"input_size"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	h := sha256.Sum256(content)
	want := hex.EncodeToString(h[:])
	if payload.Hash != want {
		t.Fatalf("hash = %q, want %q", payload.Hash, want)
	}
	if payload.InputSize != int64(len(content)) {
		t.Fatalf("input_size = %d, want %d", payload.InputSize, len(content))
	}
}

func TestCryptoHashMissingInput(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:    "call-hash-empty",
		Name:  "crypto.hash",
		Input: map[string]any{},
	}})
	if err == nil {
		t.Fatal("expected error when both input and file are empty")
	}
}

func TestCryptoHashUnsupportedAlgorithm(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-hash-bad-algo",
		Name: "crypto.hash",
		Input: map[string]any{
			"input":     "test",
			"algorithm": "sha3",
		},
	}})
	if err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
}

// ---------------------------------------------------------------------------
// crypto.hmac tests
// ---------------------------------------------------------------------------

func TestCryptoHMAC(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-hmac",
		Name: "crypto.hmac",
		Input: map[string]any{
			"input":     "hello",
			"key":       "secret",
			"algorithm": "sha256",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	var payload struct {
		HMAC      string `json:"hmac"`
		Algorithm string `json:"algorithm"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if payload.Algorithm != "sha256" {
		t.Fatalf("algorithm = %q", payload.Algorithm)
	}
	if len(payload.HMAC) == 0 {
		t.Fatal("hmac should not be empty")
	}
}

func TestCryptoHMACVerifyCorrect(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	// First, compute the HMAC.
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-hmac-compute",
		Name: "crypto.hmac",
		Input: map[string]any{
			"input": "verify-me",
			"key":   "my-key",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	var computePayload struct {
		HMAC string `json:"hmac"`
	}
	json.Unmarshal([]byte(results[0].Content), &computePayload)

	// Verify with the correct HMAC.
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-hmac-verify",
		Name: "crypto.hmac",
		Input: map[string]any{
			"input":  "verify-me",
			"key":    "my-key",
			"verify": computePayload.HMAC,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	var verifyPayload struct {
		Valid bool `json:"valid"`
	}
	json.Unmarshal([]byte(results[0].Content), &verifyPayload)
	if !verifyPayload.Valid {
		t.Fatal("HMAC verification should be valid")
	}
}

func TestCryptoHMACVerifyIncorrect(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-hmac-bad",
		Name: "crypto.hmac",
		Input: map[string]any{
			"input":  "hello",
			"key":    "secret",
			"verify": "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	var payload struct {
		Valid bool `json:"valid"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Valid {
		t.Fatal("HMAC verification should be invalid")
	}
}

// ---------------------------------------------------------------------------
// crypto.random tests
// ---------------------------------------------------------------------------

func TestCryptoRandomHex(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-random-hex",
		Name: "crypto.random",
		Input: map[string]any{
			"length":   16,
			"encoding": "hex",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	var payload struct {
		Value    string `json:"value"`
		Length   int    `json:"length"`
		Encoding string `json:"encoding"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Length != 16 {
		t.Fatalf("length = %d, want 16", payload.Length)
	}
	if payload.Encoding != "hex" {
		t.Fatalf("encoding = %q, want hex", payload.Encoding)
	}
	// Hex encoding of 16 bytes = 32 hex chars.
	if len(payload.Value) != 32 {
		t.Fatalf("hex value length = %d, want 32", len(payload.Value))
	}
}

func TestCryptoRandomBase64(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-random-b64",
		Name: "crypto.random",
		Input: map[string]any{
			"length":   32,
			"encoding": "base64",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	var payload struct {
		Value    string `json:"value"`
		Encoding string `json:"encoding"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Encoding != "base64" {
		t.Fatalf("encoding = %q", payload.Encoding)
	}
	if len(payload.Value) == 0 {
		t.Fatal("base64 value should not be empty")
	}
}

func TestCryptoRandomAlphanumeric(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-random-alnum",
		Name: "crypto.random",
		Input: map[string]any{
			"length":   20,
			"encoding": "alphanumeric",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	var payload struct {
		Value  string `json:"value"`
		Length int    `json:"length"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if len(payload.Value) != 20 {
		t.Fatalf("alphanumeric value length = %d, want 20", len(payload.Value))
	}
}

func TestCryptoRandomTooLarge(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-random-big",
		Name: "crypto.random",
		Input: map[string]any{
			"length": 2000,
		},
	}})
	if err == nil {
		t.Fatal("expected error when length > 1024")
	}
}

// ---------------------------------------------------------------------------
// crypto.aes tests
// ---------------------------------------------------------------------------

func TestCryptoAESEncryptDecrypt(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	// 32-byte key in hex (AES-256).
	keyHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	plaintext := "secret message"

	// Encrypt.
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-aes-enc",
		Name: "crypto.aes",
		Input: map[string]any{
			"input": plaintext,
			"key":   keyHex,
			"mode":  "encrypt",
		},
	}})
	if err != nil {
		t.Fatalf("encrypt error = %v", err)
	}
	var encPayload struct {
		Result string `json:"result"`
		Mode   string `json:"mode"`
	}
	json.Unmarshal([]byte(results[0].Content), &encPayload)
	if encPayload.Mode != "encrypt" {
		t.Fatalf("mode = %q, want encrypt", encPayload.Mode)
	}
	if encPayload.Result == "" {
		t.Fatal("encrypted result should not be empty")
	}

	// Decrypt.
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-aes-dec",
		Name: "crypto.aes",
		Input: map[string]any{
			"input": encPayload.Result,
			"key":   keyHex,
			"mode":  "decrypt",
		},
	}})
	if err != nil {
		t.Fatalf("decrypt error = %v", err)
	}
	var decPayload struct {
		Result string `json:"result"`
		Mode   string `json:"mode"`
	}
	json.Unmarshal([]byte(results[0].Content), &decPayload)
	if decPayload.Mode != "decrypt" {
		t.Fatalf("mode = %q, want decrypt", decPayload.Mode)
	}
	if decPayload.Result != plaintext {
		t.Fatalf("decrypted = %q, want %q", decPayload.Result, plaintext)
	}
}

func TestCryptoAESInvalidKeyLength(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-aes-bad-key",
		Name: "crypto.aes",
		Input: map[string]any{
			"input": "test",
			"key":   "0123456789", // 5 bytes, invalid
		},
	}})
	if err == nil {
		t.Fatal("expected error for invalid key length")
	}
	if !strings.Contains(err.Error(), "must be 16, 24, or 32") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCryptoAESInvalidHexKey(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-aes-bad-hex",
		Name: "crypto.aes",
		Input: map[string]any{
			"input": "test",
			"key":   "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", // not valid hex
		},
	}})
	if err == nil {
		t.Fatal("expected error for invalid hex key")
	}
}

// sha256Hex is a test helper — duplicated from the model package for use in toolruntime tests.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
