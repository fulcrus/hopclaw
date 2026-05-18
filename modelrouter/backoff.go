package modelrouter

import (
	"math"
	"time"
)

// IsTransient returns true for failure reasons that should use short cooldowns.
// Transient: rate_limit, overloaded, timeout, unknown.
// Permanent: auth, auth_permanent, billing, format, model_not_found.
func IsTransient(reason FailureReason) bool {
	switch reason {
	case FailureRateLimit, FailureOverloaded, FailureTimeout, FailureUnknown:
		return true
	default:
		return false
	}
}

// TransientCooldown calculates cooldown for transient failures.
// Formula: min(30 min, 20s * 3^(errorCount-1))
// errorCount=1 -> 20s, 2 -> 60s, 3 -> 3min, 4 -> 9min, 5+ -> 27min → cap 30min
// Short base keeps models available; exponential growth still protects against storms.
func TransientCooldown(errorCount int) time.Duration {
	if errorCount <= 0 {
		errorCount = 1
	}
	const (
		baseSeconds          = 20
		multiplier           = 3
		transientCooldownMax = 30 * time.Minute
	)
	seconds := float64(baseSeconds) * math.Pow(float64(multiplier), float64(errorCount-1))
	d := time.Duration(seconds) * time.Second
	if d > transientCooldownMax {
		return transientCooldownMax
	}
	return d
}

// PermanentDisableDuration calculates disable duration for permanent failures.
// Formula: min(12h, 1h * 2^(errorCount-1))
// errorCount=1 -> 1h, 2 -> 2h, 3 -> 4h, 4 -> 8h, 5+ -> 12h
// Shorter base than before so temporary auth/billing issues recover faster.
func PermanentDisableDuration(errorCount int) time.Duration {
	if errorCount <= 0 {
		errorCount = 1
	}
	const (
		baseHours           = 1
		multiplier          = 2
		permanentDisableMax = 12 * time.Hour
	)
	hours := float64(baseHours) * math.Pow(float64(multiplier), float64(errorCount-1))
	d := time.Duration(hours * float64(time.Hour))
	if d > permanentDisableMax {
		return permanentDisableMax
	}
	return d
}
