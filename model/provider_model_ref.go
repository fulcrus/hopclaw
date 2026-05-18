package model

import "strings"

// MatchProviderPrefix resolves the longest configured provider prefix for a
// qualified model reference such as "provider/model" or "plugin/provider/model".
// It returns ok=false when the model reference does not begin with any known
// provider name followed by a slash.
func MatchProviderPrefix(modelID string, providerNames []string) (provider string, model string, ok bool) {
	modelID = strings.TrimSpace(modelID)
	bestProvider := ""
	bestLen := 0
	for _, name := range providerNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		prefix := name + "/"
		if !strings.HasPrefix(modelID, prefix) {
			continue
		}
		if len(name) > bestLen {
			bestProvider = name
			bestLen = len(name)
		}
	}
	if bestProvider == "" {
		return "", modelID, false
	}
	return bestProvider, strings.TrimSpace(modelID[bestLen+1:]), true
}
