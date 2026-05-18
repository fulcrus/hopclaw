package config

import (
	"fmt"
	"strings"
)

func ProviderEnvVarExamples(limit int) []string {
	profiles := SetupProviderProfiles()
	out := make([]string, 0, len(profiles))
	seen := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		if !providerSupportsAPIKeyEnv(profile.ID) {
			continue
		}
		for _, envVar := range profile.EnvVars {
			envVar = strings.TrimSpace(envVar)
			if envVar == "" {
				continue
			}
			if _, ok := seen[envVar]; ok {
				continue
			}
			seen[envVar] = struct{}{}
			out = append(out, envVar)
			break
		}
		if limit > 0 && len(out) >= limit {
			return append([]string(nil), out[:limit]...)
		}
	}
	return out
}

func ProviderEnvExportHints(limit int) []string {
	examples := ProviderEnvVarExamples(limit)
	out := make([]string, 0, len(examples))
	for _, envVar := range examples {
		out = append(out, fmt.Sprintf("export %s=...", envVar))
	}
	return out
}

func MissingAPIKeyMessage() string {
	examples := ProviderEnvVarExamples(3)
	if len(examples) == 0 {
		return "no API key found; set a supported provider env var"
	}
	return fmt.Sprintf("no API key found; set a supported provider env var such as %s", strings.Join(examples, ", "))
}

func MissingAPIKeyDoctorDetail() string {
	examples := ProviderEnvVarExamples(6)
	if len(examples) == 0 {
		return "no provider API key env vars detected"
	}
	return fmt.Sprintf("no provider API key env vars detected (for example %s)", strings.Join(examples, ", "))
}

func MissingAPIKeyDoctorFix() string {
	examples := ProviderEnvVarExamples(2)
	if len(examples) == 0 {
		return "set a supported provider API key env var"
	}
	return fmt.Sprintf("set a supported provider API key env var such as %s", strings.Join(examples, " or "))
}
