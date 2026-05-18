package modelrouter

import "testing"

func TestClassifyErrorHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		message string
		want    FailureReason
	}{
		{"402 billing", 402, "payment required", FailureBilling},
		{"402 with rate keyword", 402, "rate limit on billing", FailureRateLimit},
		{"429 rate limit", 429, "too many requests", FailureRateLimit},
		{"401 auth", 401, "unauthorized", FailureAuth},
		{"403 auth permanent", 403, "forbidden", FailureAuthPermanent},
		{"404 model not found", 404, "model xyz not found", FailureModelNotFound},
		{"404 without model keyword", 404, "page not found", FailureUnknown},
		{"400 format", 400, "invalid request body", FailureFormat},
		{"408 timeout", 408, "request timeout", FailureTimeout},
		{"503 overloaded", 503, "server overloaded", FailureOverloaded},
		{"503 without overload", 503, "service unavailable", FailureUnknown},
		{"502 bad gateway", 502, "bad gateway", FailureTimeout},
		{"504 gateway timeout", 504, "gateway timeout", FailureTimeout},
		{"529 overloaded", 529, "site is overloaded", FailureTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyError(tt.status, tt.message)
			if got != tt.want {
				t.Fatalf("ClassifyError(%d, %q) = %q, want %q", tt.status, tt.message, got, tt.want)
			}
		})
	}
}

func TestClassifyErrorMessagePatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		want    FailureReason
	}{
		{"rate limit in message", "you have hit the rate limit", FailureRateLimit},
		{"overload in message", "server is overloaded", FailureOverloaded},
		{"timeout in message", "connection timeout", FailureTimeout},
		{"timed out in message", "request timed out", FailureTimeout},
		{"billing in message", "billing account suspended", FailureBilling},
		{"quota in message", "quota exceeded", FailureBilling},
		{"insufficient in message", "insufficient credits", FailureBilling},
		{"unknown message", "something went wrong", FailureUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyError(0, tt.message)
			if got != tt.want {
				t.Fatalf("ClassifyError(0, %q) = %q, want %q", tt.message, got, tt.want)
			}
		})
	}
}

func TestClassifyErrorEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("empty message", func(t *testing.T) {
		t.Parallel()
		got := ClassifyError(0, "")
		if got != FailureUnknown {
			t.Fatalf("ClassifyError(0, \"\") = %q, want %q", got, FailureUnknown)
		}
	})

	t.Run("unknown status no message", func(t *testing.T) {
		t.Parallel()
		got := ClassifyError(999, "")
		if got != FailureUnknown {
			t.Fatalf("ClassifyError(999, \"\") = %q, want %q", got, FailureUnknown)
		}
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		t.Parallel()
		got := ClassifyError(0, "RATE LIMIT exceeded")
		if got != FailureRateLimit {
			t.Fatalf("ClassifyError(0, \"RATE LIMIT exceeded\") = %q, want %q", got, FailureRateLimit)
		}
	})

	t.Run("status takes priority over message", func(t *testing.T) {
		t.Parallel()
		// status 400 (format) should win even if message says "rate limit"
		got := ClassifyError(400, "rate limit")
		if got != FailureFormat {
			t.Fatalf("ClassifyError(400, \"rate limit\") = %q, want %q", got, FailureFormat)
		}
	})
}
