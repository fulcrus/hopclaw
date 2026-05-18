// Package crypto implements cryptographic tool handlers (crypto.hash, crypto.hmac,
// crypto.random, crypto.aes) for the toolruntime registry.
package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"math/big"
	"os"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// Runtime is the narrow interface that crypto handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	ResolvePath(input string) (string, error)
}

// Handler is the tool handler signature, parameterized on our narrow Runtime interface.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with a crypto handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// ToolDefs returns all crypto domain tool definitions.
func ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "crypto.hash",
				Aliases:         []string{"text.hash"},
				Description:     "Compute a cryptographic hash of a string or file. Supports MD5, SHA1, SHA256, SHA512.",
				InputSchema:     hashInputSchema(),
				OutputSchema:    hashOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "crypto:hash",
			},
			Handler: handleHash,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "crypto.hmac",
				Description:     "Compute or verify an HMAC signature.",
				InputSchema:     hmacInputSchema(),
				OutputSchema:    hmacOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "crypto:hmac",
			},
			Handler: handleHMAC,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "crypto.random",
				Description:     "Generate cryptographically secure random data.",
				InputSchema:     randomInputSchema(),
				OutputSchema:    randomOutputSchema(),
				SideEffectClass: "read",
				ExecutionKey:    "crypto:random",
			},
			Handler: handleRandom,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "crypto.aes",
				Description:     "Encrypt or decrypt data using AES-GCM.",
				InputSchema:     aesInputSchema(),
				OutputSchema:    aesOutputSchema(),
				SideEffectClass: "read",
				ExecutionKey:    "crypto:aes",
			},
			Handler: handleAES,
		},
	}
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func hashInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "String to hash. Provide either input or file.",
			},
			"file": map[string]any{
				"type":        "string",
				"description": "Path to a file to hash. Provide either input or file.",
			},
			"algorithm": map[string]any{
				"type":        "string",
				"description": "Hash algorithm: md5, sha1, sha256, sha512. Defaults to sha256.",
			},
		},
		"additionalProperties": false,
	}
}

func hmacInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "The message to sign.",
			},
			"key": map[string]any{
				"type":        "string",
				"description": "The HMAC secret key.",
			},
			"algorithm": map[string]any{
				"type":        "string",
				"description": "Hash algorithm: md5, sha1, sha256, sha512. Defaults to sha256.",
			},
			"verify": map[string]any{
				"type":        "string",
				"description": "Expected HMAC hex digest to verify against.",
			},
		},
		"required":             []string{"input", "key"},
		"additionalProperties": false,
	}
}

func randomInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"length": map[string]any{
				"type":        "integer",
				"description": "Number of random bytes to generate. Defaults to 32.",
			},
			"encoding": map[string]any{
				"type":        "string",
				"description": "Output encoding: hex, base64, or alphanumeric. Defaults to hex.",
			},
		},
		"additionalProperties": false,
	}
}

func aesInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "Data to encrypt (plaintext string) or decrypt (base64 ciphertext).",
			},
			"key": map[string]any{
				"type":        "string",
				"description": "Hex-encoded AES key (16, 24, or 32 bytes).",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "Operation mode: encrypt or decrypt. Defaults to encrypt.",
			},
			"encoding": map[string]any{
				"type":        "string",
				"description": "Output encoding for ciphertext. Defaults to base64.",
			},
		},
		"required":             []string{"input", "key"},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas — use plain map literals to avoid importing toolruntime.
// ---------------------------------------------------------------------------

func stringSchema(description string) map[string]any {
	schema := map[string]any{"type": "string"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func integerSchema(description string) map[string]any {
	schema := map[string]any{"type": "integer"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func booleanSchema(description string) map[string]any {
	schema := map[string]any{"type": "boolean"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func hashOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"hash":       stringSchema("Hex-encoded hash digest."),
		"algorithm":  stringSchema("Hash algorithm used."),
		"input_size": integerSchema("Size of the input in bytes."),
	}, "hash", "algorithm", "input_size")
}

func hmacOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"hmac":      stringSchema("Hex-encoded HMAC digest."),
		"algorithm": stringSchema("Hash algorithm used."),
		"valid":     booleanSchema("Whether the HMAC matches the expected value. Only present when verify is provided."),
	}, "hmac", "algorithm")
}

func randomOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"value":    stringSchema("Generated random value."),
		"length":   integerSchema("Number of random bytes generated."),
		"encoding": stringSchema("Output encoding used."),
	}, "value", "length", "encoding")
}

func aesOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"result": stringSchema("Encrypted (base64) or decrypted (plaintext) result."),
		"mode":   stringSchema("Operation performed: encrypt or decrypt."),
	}, "result", "mode")
}

// ---------------------------------------------------------------------------
// Param helpers — duplicated locally to avoid importing toolruntime.
// ---------------------------------------------------------------------------

func stringFrom(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	default:
		return "", fmt.Errorf("expected string, got %T", value)
	}
}

func requiredString(input map[string]any, key string) (string, error) {
	value, err := stringFrom(input[key])
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func intFrom(value any, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback, nil
		}
		var v int64
		_, err := fmt.Sscanf(typed, "%d", &v)
		if err != nil {
			return 0, err
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func newHash(algorithm string) (hash.Hash, error) {
	switch strings.ToLower(algorithm) {
	case "md5":
		return md5.New(), nil
	case "sha1":
		return sha1.New(), nil
	case "sha256", "":
		return sha256.New(), nil
	case "sha512":
		return sha512.New(), nil
	default:
		return nil, fmt.Errorf("unsupported algorithm %q (supported: md5, sha1, sha256, sha512)", algorithm)
	}
}

func normalizeAlgorithm(algorithm string) string {
	if strings.TrimSpace(algorithm) == "" {
		return "sha256"
	}
	return strings.ToLower(algorithm)
}

func handleHash(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	inputValue, _ := stringFrom(call.Input["input"])
	fileValue, _ := stringFrom(call.Input["file"])
	algorithm, _ := stringFrom(call.Input["algorithm"])
	algorithm = normalizeAlgorithm(algorithm)

	if strings.TrimSpace(inputValue) == "" && strings.TrimSpace(fileValue) == "" {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.hash: either input or file is required")
	}

	h, err := newHash(algorithm)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.hash: %w", err)
	}

	var inputSize int64
	if strings.TrimSpace(fileValue) != "" {
		resolvedFile, err := rt.ResolvePath(fileValue)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("crypto.hash: %w", err)
		}
		f, err := os.Open(resolvedFile)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("crypto.hash: %w", err)
		}
		defer f.Close()
		n, err := io.Copy(h, f)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("crypto.hash: %w", err)
		}
		inputSize = n
	} else {
		data := []byte(inputValue)
		h.Write(data)
		inputSize = int64(len(data))
	}

	return rt.JSONResult(call, map[string]any{
		"hash":       hex.EncodeToString(h.Sum(nil)),
		"algorithm":  algorithm,
		"input_size": inputSize,
	})
}

func handleHMAC(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	inputValue, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.hmac: %w", err)
	}
	key, err := requiredString(call.Input, "key")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.hmac: %w", err)
	}
	algorithm, _ := stringFrom(call.Input["algorithm"])
	algorithm = normalizeAlgorithm(algorithm)
	verifyValue, _ := stringFrom(call.Input["verify"])

	hashFunc, err := newHash(algorithm)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.hmac: %w", err)
	}
	_ = hashFunc // we need the factory function, not the instance

	var newFunc func() hash.Hash
	switch algorithm {
	case "md5":
		newFunc = md5.New
	case "sha1":
		newFunc = sha1.New
	case "sha256":
		newFunc = sha256.New
	case "sha512":
		newFunc = sha512.New
	default:
		return contextengine.ToolResult{}, fmt.Errorf("crypto.hmac: unsupported algorithm %q", algorithm)
	}

	mac := hmac.New(newFunc, []byte(key))
	mac.Write([]byte(inputValue))
	digest := hex.EncodeToString(mac.Sum(nil))

	result := map[string]any{
		"hmac":      digest,
		"algorithm": algorithm,
	}

	if strings.TrimSpace(verifyValue) != "" {
		result["valid"] = hmac.Equal([]byte(digest), []byte(strings.ToLower(verifyValue)))
	}

	return rt.JSONResult(call, result)
}

func handleRandom(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	length, err := intFrom(call.Input["length"], 32)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.random: %w", err)
	}
	if length <= 0 {
		length = 32
	}
	if length > 1024 {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.random: length must be <= 1024")
	}

	encoding, _ := stringFrom(call.Input["encoding"])
	if strings.TrimSpace(encoding) == "" {
		encoding = "hex"
	}
	encoding = strings.ToLower(encoding)

	var value string
	switch encoding {
	case "hex":
		buf := make([]byte, length)
		if _, err := rand.Read(buf); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("crypto.random: %w", err)
		}
		value = hex.EncodeToString(buf)
	case "base64":
		buf := make([]byte, length)
		if _, err := rand.Read(buf); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("crypto.random: %w", err)
		}
		value = base64.StdEncoding.EncodeToString(buf)
	case "alphanumeric":
		const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		charsetLen := big.NewInt(int64(len(charset)))
		buf := make([]byte, length)
		for i := 0; i < length; i++ {
			idx, err := rand.Int(rand.Reader, charsetLen)
			if err != nil {
				return contextengine.ToolResult{}, fmt.Errorf("crypto.random: %w", err)
			}
			buf[i] = charset[idx.Int64()]
		}
		value = string(buf)
	default:
		return contextengine.ToolResult{}, fmt.Errorf("crypto.random: unsupported encoding %q (supported: hex, base64, alphanumeric)", encoding)
	}

	return rt.JSONResult(call, map[string]any{
		"value":    value,
		"length":   length,
		"encoding": encoding,
	})
}

func handleAES(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	inputValue, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.aes: %w", err)
	}
	keyHex, err := requiredString(call.Input, "key")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.aes: %w", err)
	}
	mode, _ := stringFrom(call.Input["mode"])
	if strings.TrimSpace(mode) == "" {
		mode = "encrypt"
	}
	mode = strings.ToLower(mode)

	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.aes: invalid hex key: %w", err)
	}
	keyLen := len(keyBytes)
	if keyLen != 16 && keyLen != 24 && keyLen != 32 {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.aes: key must be 16, 24, or 32 bytes (got %d)", keyLen)
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("crypto.aes: %w", err)
	}

	switch mode {
	case "encrypt":
		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("crypto.aes: %w", err)
		}
		ciphertext := gcm.Seal(nonce, nonce, []byte(inputValue), nil)
		encoded := base64.StdEncoding.EncodeToString(ciphertext)
		return rt.JSONResult(call, map[string]any{
			"result": encoded,
			"mode":   "encrypt",
		})

	case "decrypt":
		ciphertext, err := base64.StdEncoding.DecodeString(inputValue)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("crypto.aes: invalid base64 input: %w", err)
		}
		nonceSize := gcm.NonceSize()
		if len(ciphertext) < nonceSize {
			return contextengine.ToolResult{}, fmt.Errorf("crypto.aes: ciphertext too short")
		}
		nonce := ciphertext[:nonceSize]
		ciphertext = ciphertext[nonceSize:]
		plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("crypto.aes: decryption failed: %w", err)
		}
		return rt.JSONResult(call, map[string]any{
			"result": string(plaintext),
			"mode":   "decrypt",
		})

	default:
		return contextengine.ToolResult{}, fmt.Errorf("crypto.aes: unsupported mode %q (supported: encrypt, decrypt)", mode)
	}
}
