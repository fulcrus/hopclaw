package policy

import (
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/audit"
)

type GrantStoreSetter interface {
	SetGrantStore(gs *approval.GrantStore)
}

type SecurityAuditorSetter interface {
	SetSecurityAuditor(a *audit.SecurityAuditor)
}

func WireGrantStore(engine Engine, gs *approval.GrantStore) bool {
	if engine == nil || gs == nil {
		return false
	}
	setter, ok := engine.(GrantStoreSetter)
	if !ok {
		return false
	}
	setter.SetGrantStore(gs)
	return true
}

func WireSecurityAuditor(engine Engine, auditor *audit.SecurityAuditor) bool {
	if engine == nil || auditor == nil {
		return false
	}
	setter, ok := engine.(SecurityAuditorSetter)
	if !ok {
		return false
	}
	setter.SetSecurityAuditor(auditor)
	return true
}
