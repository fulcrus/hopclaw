package modelrouter

import "strings"

// ClassifyError determines the FailureReason from an HTTP status code and error message.
func ClassifyError(status int, message string) FailureReason {
	lower := strings.ToLower(message)

	// Priority 1: status 402 (Payment Required)
	if status == 402 {
		if strings.Contains(lower, "rate") {
			return FailureRateLimit
		}
		return FailureBilling
	}

	// Priority 2: status 429 (Too Many Requests)
	if status == 429 {
		return FailureRateLimit
	}

	// Priority 3: status 401 (Unauthorized)
	if status == 401 {
		return FailureAuth
	}

	// Priority 4: status 403 (Forbidden)
	if status == 403 {
		return FailureAuthPermanent
	}

	// Priority 5: status 404 + "model" in message
	if status == 404 && strings.Contains(lower, "model") {
		return FailureModelNotFound
	}

	// Priority 6: status 400 (Bad Request)
	if status == 400 {
		return FailureFormat
	}

	// Priority 7: status 408 (Request Timeout)
	if status == 408 {
		return FailureTimeout
	}

	// Priority 8: status 503 + "overload" in message
	if status == 503 && strings.Contains(lower, "overload") {
		return FailureOverloaded
	}

	// Priority 9: status 502, 504, 529
	if status == 502 || status == 504 || status == 529 {
		return FailureTimeout
	}

	// Priority 10: Message pattern matching (for non-specific status codes)
	if strings.Contains(lower, "rate limit") {
		return FailureRateLimit
	}
	if strings.Contains(lower, "overload") {
		return FailureOverloaded
	}
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") {
		return FailureTimeout
	}
	if strings.Contains(lower, "billing") || strings.Contains(lower, "quota") || strings.Contains(lower, "insufficient") {
		return FailureBilling
	}

	// Default
	return FailureUnknown
}
