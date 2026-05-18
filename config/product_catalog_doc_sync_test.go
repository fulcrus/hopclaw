package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChannelGuideSupportTableMatchesCatalog(t *testing.T) {
	t.Parallel()

	rows := parseChannelGuideSupportRows(t, filepath.Join("..", "docs", "guides", "channels", "README.md"))
	for _, profile := range ChannelProfiles() {
		row, ok := rows[profile.ID]
		if !ok {
			t.Fatalf("channel guide support table missing row for %q", profile.ID)
		}
		if row.SupportLevel != string(profile.SupportLevel) {
			t.Fatalf("channel %q support level row = %q, want %q", profile.ID, row.SupportLevel, profile.SupportLevel)
		}
		if row.SetupSupported != yesNo(profile.SetupSupported) {
			t.Fatalf("channel %q setup row = %q, want %q", profile.ID, row.SetupSupported, yesNo(profile.SetupSupported))
		}
		if row.OnboardingSupported != yesNo(profile.OnboardingSupported) {
			t.Fatalf("channel %q onboarding row = %q, want %q", profile.ID, row.OnboardingSupported, yesNo(profile.OnboardingSupported))
		}
	}
}

func TestReadmeChannelSupportListsMatchCatalog(t *testing.T) {
	t.Parallel()

	readme := readDocSyncFile(t, filepath.Join("..", "README.md"))
	supportedNames := parseReadmeChannelList(t, readme, "- supported messaging channels:")
	supportedSpecial := parseReadmeChannelList(t, readme, "- supported personal or special channels:")
	experimentalNames := parseReadmeChannelList(t, readme, "- experimental channels:")

	supportedDocSet := make(map[string]struct{}, len(supportedNames)+len(supportedSpecial))
	for _, name := range append(supportedNames, supportedSpecial...) {
		supportedDocSet[strings.ToLower(name)] = struct{}{}
	}
	experimentalDocSet := make(map[string]struct{}, len(experimentalNames))
	for _, name := range experimentalNames {
		experimentalDocSet[strings.ToLower(name)] = struct{}{}
	}

	for _, profile := range ChannelProfiles() {
		label := strings.ToLower(readmeChannelLabel(profile.ID))
		switch profile.SupportLevel {
		case SupportLevelSupported:
			if _, ok := supportedDocSet[label]; !ok {
				t.Fatalf("README supported channel lists missing %q (%s)", label, profile.ID)
			}
			if _, ok := experimentalDocSet[label]; ok {
				t.Fatalf("README experimental channel list must not include supported channel %q (%s)", label, profile.ID)
			}
		case SupportLevelExperimental:
			if _, ok := experimentalDocSet[label]; !ok {
				t.Fatalf("README experimental channel list missing %q (%s)", label, profile.ID)
			}
			if _, ok := supportedDocSet[label]; ok {
				t.Fatalf("README supported channel lists must not include experimental channel %q (%s)", label, profile.ID)
			}
		}
	}
}

type channelGuideSupportRow struct {
	SupportLevel        string
	SetupSupported      string
	OnboardingSupported string
}

func parseChannelGuideSupportRows(t *testing.T, path string) map[string]channelGuideSupportRow {
	t.Helper()

	body := readDocSyncFile(t, path)
	lines := strings.Split(body, "\n")
	rows := make(map[string]channelGuideSupportRow)
	inTable := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "| `feishu` |") {
			inTable = true
		}
		if !inTable {
			continue
		}
		if strings.HasPrefix(line, "Notes:") {
			break
		}
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		parts := splitMarkdownTableRow(line)
		if len(parts) < 5 {
			t.Fatalf("unexpected channel guide row format: %q", line)
		}
		id := strings.Trim(parts[0], "`")
		rows[id] = channelGuideSupportRow{
			SupportLevel:        strings.Trim(parts[2], "`"),
			SetupSupported:      parts[3],
			OnboardingSupported: parts[4],
		}
	}
	if len(rows) == 0 {
		t.Fatalf("no channel rows parsed from %s", path)
	}
	return rows
}

func parseReadmeChannelList(t *testing.T, body string, prefix string) []string {
	t.Helper()

	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		items := strings.Split(strings.TrimSpace(strings.TrimPrefix(line, prefix)), ",")
		out := make([]string, 0, len(items))
		for _, item := range items {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	}
	t.Fatalf("README missing line with prefix %q", prefix)
	return nil
}

func readmeChannelLabel(channelID string) string {
	switch channelID {
	case "feishu":
		return "Feishu"
	case "imessage":
		return "iMessage (legacy bridge)"
	case "nextcloud_talk":
		return "Nextcloud Talk"
	case "msteams":
		return "Microsoft Teams"
	case "synology_chat":
		return "Synology Chat"
	case "googlechat":
		return "Google Chat"
	case "tlon":
		return "Tlon"
	case "zalouser":
		return "Zalo Personal"
	default:
		for _, profile := range ChannelProfiles() {
			if profile.ID == channelID {
				return profile.DisplayName
			}
		}
		return channelID
	}
}

func readDocSyncFile(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(body)
}

func splitMarkdownTableRow(line string) []string {
	raw := strings.Split(line, "|")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func yesNo(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}
