package gateway

import (
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/config"
)

func sortChannelInfoItems(items []channelInfo) {
	sort.SliceStable(items, func(i, j int) bool {
		return operatorChannelNameLess(items[i].Name, items[j].Name)
	})
}

func sortDetectedChannelItems(items []detectedChannel) {
	sort.SliceStable(items, func(i, j int) bool {
		return operatorChannelNameLess(items[i].Name, items[j].Name)
	})
}

func operatorChannelNameLess(left, right string) bool {
	leftRank, leftKnown := operatorChannelSortRank(left)
	rightRank, rightKnown := operatorChannelSortRank(right)
	switch {
	case leftKnown && rightKnown && leftRank != rightRank:
		return leftRank < rightRank
	case leftKnown && !rightKnown:
		return true
	case !leftKnown && rightKnown:
		return false
	default:
		return normalizeOperatorChannelName(left) < normalizeOperatorChannelName(right)
	}
}

func operatorChannelSortRank(name string) (int, bool) {
	profile, ok := config.LookupChannelProfile(name)
	if !ok {
		return 0, false
	}
	for index, item := range config.ChannelProfiles() {
		if item.ID == profile.ID {
			return index, true
		}
	}
	return 0, false
}

func normalizeOperatorChannelName(name string) string {
	normalized := strings.TrimSpace(strings.ToLower(name))
	return strings.ReplaceAll(normalized, "-", "_")
}
