package wire

import (
	"io"
	"strings"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	redactedValue = "[REDACTED]"
)

// defaultRedactHeaders lists header names that are always redacted,
// regardless of the Config.RedactHeaders setting.
var defaultRedactHeaders = []string{
	"authorization",
	"x-api-key",
	"api-key",
	"x-hopclaw-token",
}

// ---------------------------------------------------------------------------
// SanitizeHeaders
// ---------------------------------------------------------------------------

// SanitizeHeaders returns a copy of headers with sensitive values replaced by
// "[REDACTED]". The default redact list is always applied; additional header
// names can be supplied via the redact parameter.
func SanitizeHeaders(headers map[string]string, redact []string) map[string]string {
	if headers == nil {
		return nil
	}

	merged := make(map[string]struct{}, len(defaultRedactHeaders)+len(redact))
	for _, h := range defaultRedactHeaders {
		merged[strings.ToLower(h)] = struct{}{}
	}
	for _, h := range redact {
		merged[strings.ToLower(h)] = struct{}{}
	}

	out := make(map[string]string, len(headers))
	for k, v := range headers {
		if _, redacted := merged[strings.ToLower(k)]; redacted {
			out[k] = redactedValue
		} else {
			out[k] = v
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// TruncateBody
// ---------------------------------------------------------------------------

// TruncateBody reads up to maxBytes from r and returns the result as a string.
// If r is nil or maxBytes is zero, an empty string is returned.
func TruncateBody(r io.Reader, maxBytes int) string {
	if r == nil || maxBytes <= 0 {
		return ""
	}
	buf := make([]byte, maxBytes)
	n, _ := io.ReadFull(r, buf)
	return string(buf[:n])
}
