package config

import "strings"

func ChannelProfiles() []ChannelProfile {
	out := make([]ChannelProfile, len(channelProfiles))
	for i, profile := range channelProfiles {
		out[i] = cloneChannelProfile(profile)
	}
	return out
}

func SetupChannelProfiles() []ChannelProfile {
	return filterChannelProfiles(func(profile ChannelProfile) bool { return profile.SetupSupported })
}

func OnboardingChannelProfiles() []ChannelProfile {
	return filterChannelProfiles(func(profile ChannelProfile) bool { return profile.OnboardingSupported })
}

func LookupChannelProfile(channel string) (ChannelProfile, bool) {
	normalized := normalizeChannelCatalogID(channel)
	for _, profile := range channelProfiles {
		if profile.ID == normalized {
			return cloneChannelProfile(profile), true
		}
	}
	return ChannelProfile{}, false
}

func normalizeChannelCatalogID(channel string) string {
	normalized := strings.TrimSpace(strings.ToLower(channel))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

func cloneChannelProfile(profile ChannelProfile) ChannelProfile {
	out := profile
	out.Fields = cloneSetupChannelFields(profile.Fields)
	operatorFields := profile.OperatorFields
	if len(operatorFields) == 0 {
		operatorFields = channelOperatorFields(profile.ID, profile.Fields)
	}
	out.OperatorFields = cloneSetupChannelFields(operatorFields)
	return out
}

func filterChannelProfiles(keep func(ChannelProfile) bool) []ChannelProfile {
	out := make([]ChannelProfile, 0, len(channelProfiles))
	for _, profile := range channelProfiles {
		if keep(profile) {
			out = append(out, cloneChannelProfile(profile))
		}
	}
	return out
}
