package config

import (
	"strings"

	"github.com/fulcrus/hopclaw/model"
)

func hydrateSetupProviderProfile(profile SetupProviderProfile) SetupProviderProfile {
	profile.DefaultModels = append([]string(nil), profile.DefaultModels...)
	profile.EnvVars = append([]string(nil), profile.EnvVars...)
	profile.CapabilityMatrix = model.CapabilityMatrixForCatalogEntry(
		profile.ID,
		model.ProviderAPI(profile.API),
		firstNonEmptyString(profile.DefaultModels...),
	)
	return profile
}

func hydrateProviderAPIProfile(profile ProviderAPIProfile) ProviderAPIProfile {
	profile.Fields = append([]SetupProviderField(nil), profile.Fields...)
	profile.CapabilityMatrix = model.CapabilityMatrixForCatalogEntry(
		profile.ID,
		model.ProviderAPI(profile.ID),
		defaultModelHintFromSetupFields(profile.Fields),
	)
	return profile
}

func defaultModelHintFromSetupFields(fields []SetupProviderField) string {
	for _, field := range fields {
		if strings.TrimSpace(field.ID) != "default_model" {
			continue
		}
		if value := strings.TrimSpace(field.DefaultValue); value != "" {
			return value
		}
		if value := strings.TrimSpace(field.Placeholder); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
