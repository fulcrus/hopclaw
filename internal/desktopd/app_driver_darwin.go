//go:build darwin

package desktopd

import "strings"

type desktopAppDriver struct {
	ID               string
	Names            []string
	BundleIDs        []string
	AppFamily        string
	SupportTier      string
	SemanticRichness string
	ViewModel        string
}

var desktopAppDrivers = []desktopAppDriver{
	{
		ID:               "douyin",
		Names:            []string{"抖音"},
		BundleIDs:        []string{"com.bytedance.douyin.desktop"},
		AppFamily:        "media_feed",
		SupportTier:      "b",
		SemanticRichness: "mixed_semantic",
		ViewModel:        "viewful",
	},
	{
		ID:               "qqmusic",
		Names:            []string{"QQMusic", "QQ音乐"},
		BundleIDs:        []string{"com.tencent.QQMusicMac"},
		AppFamily:        "media_player",
		SupportTier:      "a",
		SemanticRichness: "mixed_semantic",
		ViewModel:        "hybrid",
	},
	{
		ID:               "premiere_pro",
		Names:            []string{"Adobe Premiere Pro 2021", "Adobe Premiere Pro"},
		BundleIDs:        []string{"com.adobe.PremierePro.15"},
		AppFamily:        "creative_editor",
		SupportTier:      "b",
		SemanticRichness: "weak_semantic",
		ViewModel:        "viewful",
	},
}

var genericDesktopAppDriver = desktopAppDriver{
	ID:               "generic",
	AppFamily:        "desktop_app",
	SupportTier:      "c",
	SemanticRichness: "mixed_semantic",
	ViewModel:        "hybrid",
}

func resolveAppDriver(app desktopAppSnapshot) desktopAppDriver {
	return resolveAppDriverForTarget(app.Name, app.BundleID)
}

func resolveAppDriverForTarget(appName, bundleID string) desktopAppDriver {
	appName = strings.TrimSpace(appName)
	bundleID = strings.TrimSpace(bundleID)
	for _, driver := range desktopAppDrivers {
		for _, candidate := range driver.BundleIDs {
			if bundleID != "" && strings.EqualFold(strings.TrimSpace(candidate), bundleID) {
				return driver
			}
		}
		for _, candidate := range driver.Names {
			if appName != "" && strings.EqualFold(strings.TrimSpace(candidate), appName) {
				return driver
			}
		}
	}
	return genericDesktopAppDriver
}

func appDriverMetadata(driver desktopAppDriver) map[string]any {
	return map[string]any{
		"driver_id":         driver.ID,
		"app_family":        driver.AppFamily,
		"support_tier":      strings.ToUpper(driver.SupportTier),
		"semantic_richness": driver.SemanticRichness,
		"view_model":        driver.ViewModel,
	}
}
