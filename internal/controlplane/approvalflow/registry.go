package approvalflow

import (
	"strings"

	"github.com/fulcrus/hopclaw/controlplane"
)

type CallbackAuthPolicy = controlplane.ApprovalCallbackAuthPolicy
type ProviderDescriptor = controlplane.ApprovalProviderDescriptor
type CallbackAuthSummary = controlplane.ApprovalCallbackAuthSummary
type ProviderSummary = controlplane.ApprovalProviderSummary

type ProviderRegistry struct {
	descriptors map[string]ProviderDescriptor
	providers   []Provider
}

func NewProviderRegistry(descriptors []ProviderDescriptor, providers ...Provider) *ProviderRegistry {
	registry := &ProviderRegistry{
		descriptors: make(map[string]ProviderDescriptor),
	}
	hasDescriptors := false
	for _, descriptor := range descriptors {
		name := normalizeProviderName(descriptor.Name)
		if name == "" {
			continue
		}
		hasDescriptors = true
		descriptor.Name = strings.TrimSpace(descriptor.Name)
		descriptor.CallbackAuth.HeaderName = strings.TrimSpace(descriptor.CallbackAuth.HeaderName)
		descriptor.CallbackAuth.Token = strings.TrimSpace(descriptor.CallbackAuth.Token)
		registry.descriptors[name] = descriptor
	}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		name := normalizeProviderName(provider.Name())
		if name == "" {
			continue
		}
		if hasDescriptors {
			descriptor, ok := registry.descriptors[name]
			if !ok || !descriptor.Enabled {
				continue
			}
		}
		registry.providers = append(registry.providers, provider)
	}
	return registry
}

func (r *ProviderRegistry) Providers() []Provider {
	if r == nil || len(r.providers) == 0 {
		return nil
	}
	out := make([]Provider, len(r.providers))
	copy(out, r.providers)
	return out
}

func (r *ProviderRegistry) CallbackPolicies() map[string]CallbackAuthPolicy {
	if r == nil || len(r.descriptors) == 0 {
		return nil
	}
	out := make(map[string]CallbackAuthPolicy)
	for key, descriptor := range r.descriptors {
		if !descriptor.Enabled {
			continue
		}
		mode := strings.ToLower(strings.TrimSpace(descriptor.CallbackAuth.Mode))
		if mode == "" {
			if strings.TrimSpace(descriptor.CallbackAuth.Secret) != "" {
				mode = "hmac"
			} else {
				mode = "token"
			}
		}
		if mode == "token" && strings.TrimSpace(descriptor.CallbackAuth.Token) == "" {
			continue
		}
		if mode == "hmac" && strings.TrimSpace(descriptor.CallbackAuth.Secret) == "" {
			continue
		}
		out[key] = CallbackAuthPolicy{
			Mode:            mode,
			HeaderName:      strings.TrimSpace(descriptor.CallbackAuth.HeaderName),
			Token:           strings.TrimSpace(descriptor.CallbackAuth.Token),
			Secret:          strings.TrimSpace(descriptor.CallbackAuth.Secret),
			SignatureHeader: strings.TrimSpace(descriptor.CallbackAuth.SignatureHeader),
			TimestampHeader: strings.TrimSpace(descriptor.CallbackAuth.TimestampHeader),
			MaxAge:          descriptor.CallbackAuth.MaxAge,
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *ProviderRegistry) EnabledProviderNames() []string {
	if r == nil {
		return nil
	}
	if len(r.descriptors) > 0 {
		names := make([]string, 0, len(r.descriptors))
		for _, descriptor := range r.descriptors {
			if !descriptor.Enabled {
				continue
			}
			names = append(names, strings.TrimSpace(descriptor.Name))
		}
		sortStrings(names)
		return emptyIfZero(names)
	}
	names := make([]string, 0, len(r.providers))
	for _, provider := range r.providers {
		if provider == nil {
			continue
		}
		if name := strings.TrimSpace(provider.Name()); name != "" {
			names = append(names, name)
		}
	}
	sortStrings(names)
	return emptyIfZero(names)
}

func (r *ProviderRegistry) CallbackProtectedProviderNames() []string {
	policies := r.CallbackPolicies()
	if len(policies) == 0 {
		return nil
	}
	names := make([]string, 0, len(policies))
	for _, descriptor := range r.descriptors {
		key := normalizeProviderName(descriptor.Name)
		if _, ok := policies[key]; !ok {
			continue
		}
		names = append(names, strings.TrimSpace(descriptor.Name))
	}
	sortStrings(names)
	return emptyIfZero(names)
}

func (r *ProviderRegistry) Describe() []ProviderSummary {
	if r == nil {
		return nil
	}
	summaries := make([]ProviderSummary, 0, len(r.descriptors)+len(r.providers))
	seen := make(map[string]struct{}, len(r.descriptors)+len(r.providers))

	for key, descriptor := range r.descriptors {
		summary := ProviderSummary{
			Name:          strings.TrimSpace(descriptor.Name),
			Type:          strings.TrimSpace(descriptor.Type),
			Enabled:       descriptor.Enabled,
			Registered:    r.hasProvider(key),
			SubmitEnabled: descriptor.SubmitEnabled,
			UpdateEnabled: descriptor.UpdateEnabled,
			SyncEnabled:   descriptor.SyncEnabled,
			CallbackAuth:  summarizeCallbackAuth(descriptor.CallbackAuth),
			Metadata:      cloneMetadata(descriptor.Metadata),
		}
		if summary.Type == "" {
			summary.Type = "custom"
		}
		summaries = append(summaries, summary)
		seen[key] = struct{}{}
	}

	for _, provider := range r.providers {
		if provider == nil {
			continue
		}
		key := normalizeProviderName(provider.Name())
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		_, syncEnabled := provider.(SyncProvider)
		summaries = append(summaries, ProviderSummary{
			Name:          strings.TrimSpace(provider.Name()),
			Type:          "custom",
			Enabled:       true,
			Registered:    true,
			SubmitEnabled: true,
			UpdateEnabled: true,
			SyncEnabled:   syncEnabled,
			CallbackAuth:  CallbackAuthSummary{},
		})
	}

	sortProviderSummaries(summaries)
	return emptyIfZeroProviderSummaries(summaries)
}

func summarizeCallbackAuth(policy CallbackAuthPolicy) CallbackAuthSummary {
	mode := strings.ToLower(strings.TrimSpace(policy.Mode))
	protected := false
	switch mode {
	case "hmac":
		protected = strings.TrimSpace(policy.Secret) != ""
	case "token":
		protected = strings.TrimSpace(policy.Token) != ""
	default:
		if strings.TrimSpace(policy.Secret) != "" {
			mode = "hmac"
			protected = true
		} else if strings.TrimSpace(policy.Token) != "" {
			mode = "token"
			protected = true
		}
	}
	return CallbackAuthSummary{
		Protected:       protected,
		Mode:            mode,
		HeaderName:      strings.TrimSpace(policy.HeaderName),
		SignatureHeader: strings.TrimSpace(policy.SignatureHeader),
		TimestampHeader: strings.TrimSpace(policy.TimestampHeader),
		MaxAge:          policy.MaxAge,
	}
}

func (r *ProviderRegistry) hasProvider(name string) bool {
	if r == nil {
		return false
	}
	normalized := normalizeProviderName(name)
	if normalized == "" {
		return false
	}
	for _, provider := range r.providers {
		if provider == nil {
			continue
		}
		if normalizeProviderName(provider.Name()) == normalized {
			return true
		}
	}
	return false
}

func cloneMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func sortStrings(items []string) {
	if len(items) < 2 {
		return
	}
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if strings.ToLower(items[j]) < strings.ToLower(items[i]) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func emptyIfZero(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	return items
}

func sortProviderSummaries(items []ProviderSummary) {
	if len(items) < 2 {
		return
	}
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if strings.ToLower(items[j].Name) < strings.ToLower(items[i].Name) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func emptyIfZeroProviderSummaries(items []ProviderSummary) []ProviderSummary {
	if len(items) == 0 {
		return nil
	}
	return items
}
