package approval

import (
	"fmt"
	"strings"
)

func NormalizeScope(scope Scope) (Scope, error) {
	switch strings.TrimSpace(strings.ToLower(string(scope))) {
	case "":
		return "", nil
	case string(ScopeOnce):
		return ScopeOnce, nil
	case string(ScopeSession):
		return ScopeSession, nil
	case string(ScopeAlways):
		return ScopeAlways, nil
	case string(ScopeDeny):
		return ScopeDeny, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidScope, scope)
	}
}

func ScopeRank(scope Scope) int {
	switch normalized, _ := NormalizeScope(scope); normalized {
	case ScopeOnce:
		return 1
	case ScopeSession:
		return 2
	case ScopeAlways:
		return 3
	default:
		return 0
	}
}

func IsScopeBroader(scope, maxScope Scope) bool {
	scopeRank := ScopeRank(scope)
	if scopeRank == 0 {
		return false
	}
	maxRank := ScopeRank(maxScope)
	if maxRank == 0 {
		return true
	}
	return scopeRank > maxRank
}

func NarrowerScope(values ...Scope) Scope {
	scope, _ := NarrowerScopeChecked(values...)
	return scope
}

func NarrowerScopeChecked(values ...Scope) (Scope, error) {
	var winner Scope
	for _, candidate := range values {
		normalized, err := NormalizeScope(candidate)
		if err != nil {
			return "", err
		}
		if normalized == "" {
			continue
		}
		if winner == "" || ScopeRank(normalized) < ScopeRank(winner) {
			winner = normalized
		}
	}
	return winner, nil
}

func NormalizeResolution(ticket *Ticket, resolution Resolution) (Resolution, error) {
	scope, err := NormalizeScope(resolution.Scope)
	if err != nil {
		return resolution, err
	}
	if resolution.Status != StatusApproved {
		resolution.Scope = scope
		return resolution, nil
	}

	defaultScope, maxScope := scopePolicyFromTicket(ticket)
	if scope == "" {
		scope = defaultScope
	}
	if scope == "" {
		scope = ScopeOnce
	}
	if maxScope == "" {
		maxScope = defaultScope
	}
	if maxScope == "" {
		maxScope = ScopeOnce
	}
	if IsScopeBroader(scope, maxScope) {
		return resolution, fmt.Errorf("%w: requested=%s max=%s", ErrScopePolicy, scope, maxScope)
	}
	resolution.Scope = scope
	return resolution, nil
}

func scopePolicyFromTicket(ticket *Ticket) (Scope, Scope) {
	if ticket == nil || ticket.Metadata == nil {
		return "", ""
	}
	defaultScope, _ := NormalizeScope(Scope(strings.TrimSpace(fmt.Sprint(ticket.Metadata["policy_approval_default_scope"]))))
	maxScope, _ := NormalizeScope(Scope(strings.TrimSpace(fmt.Sprint(ticket.Metadata["policy_approval_max_scope"]))))
	return defaultScope, maxScope
}
