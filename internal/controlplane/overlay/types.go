package overlay

// EffectiveConfigProvider is the canonical read-only business interface for
// effective configuration access across runtime, gateway, and operator flows.
type EffectiveConfigProvider = Provider

// ConfigMutationService is the canonical overlay-only mutation entry point for
// operator config writes.
type ConfigMutationService = MutationService

// MergePolicyRegistry is the canonical registry for overlay merge policies.
// Wave 1 formalizes settings-domain policy registration first while provider
// and channel object overlays continue to use their specialized merge paths.
type MergePolicyRegistry = SettingPolicyRegistry

func DefaultMergePolicyRegistry() *MergePolicyRegistry {
	return DefaultSettingPolicyRegistry()
}
