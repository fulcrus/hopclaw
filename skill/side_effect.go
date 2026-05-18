package skill

import "strings"

type SideEffectClass string

const (
	SideEffectRead          SideEffectClass = "read"
	SideEffectLocalWrite    SideEffectClass = "local_write"
	SideEffectExternalWrite SideEffectClass = "external_write"
	SideEffectDestructive   SideEffectClass = "destructive"
)

type RuntimeBoundaryPolicy struct {
	Class                      SideEffectClass
	AllowsFilesystemWrite      bool
	AllowsProcessSpawn         bool
	AllowsExternalNetworkRead  bool
	AllowsExternalNetworkWrite bool
}

func NormalizeSideEffectClass(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "", "read", "readonly", "read_only":
		return string(SideEffectRead)
	case "local_write", "local-write":
		return string(SideEffectLocalWrite)
	case "external_write", "external-write", "remote_write", "remote-write":
		return string(SideEffectExternalWrite)
	case "destructive":
		return string(SideEffectDestructive)
	default:
		return string(SideEffectDestructive)
	}
}

func NormalizedSideEffectClass(v string) SideEffectClass {
	return SideEffectClass(NormalizeSideEffectClass(v))
}

func RuntimeBoundaryForSideEffect(v string) RuntimeBoundaryPolicy {
	class := NormalizedSideEffectClass(v)
	policy := RuntimeBoundaryPolicy{
		Class:                     class,
		AllowsExternalNetworkRead: true,
	}
	switch class {
	case SideEffectRead:
		return policy
	case SideEffectLocalWrite:
		policy.AllowsFilesystemWrite = true
		return policy
	case SideEffectExternalWrite:
		policy.AllowsExternalNetworkWrite = true
		return policy
	default:
		policy.Class = SideEffectDestructive
		policy.AllowsFilesystemWrite = true
		policy.AllowsProcessSpawn = true
		policy.AllowsExternalNetworkWrite = true
		return policy
	}
}
