//go:build darwin

package desktopd

import "testing"

func TestClassifyCommandPolicySystemCommandIsBlockedByDefault(t *testing.T) {
	t.Parallel()

	driver := resolveAppDriverForTarget("QQMusic", "com.tencent.QQMusicMac")
	command := applyCommandPolicy(driver, desktopCommandSnapshot{
		CommandID:      "menu:Apple > 锁定屏幕",
		Title:          "锁定屏幕",
		MenuPath:       []string{"Apple", "锁定屏幕"},
		MenuPathString: "Apple > 锁定屏幕",
		Enabled:        true,
	})

	if command.Source != "system" {
		t.Fatalf("Source = %q", command.Source)
	}
	if command.Scope != "global" {
		t.Fatalf("Scope = %q", command.Scope)
	}
	if command.RiskLevel != commandRiskCritical {
		t.Fatalf("RiskLevel = %q", command.RiskLevel)
	}
	if command.AvailableByDefault {
		t.Fatal("AvailableByDefault = true, want false")
	}
}

func TestFilterCommandSnapshotsHidesSystemAndUnsafeByDefault(t *testing.T) {
	t.Parallel()

	commands := []desktopCommandSnapshot{
		{
			CommandID:          "menu:Apple > 锁定屏幕",
			MenuPathString:     "Apple > 锁定屏幕",
			Source:             "system",
			AvailableByDefault: false,
		},
		{
			CommandID:          "menu:QQ音乐 > 退出QQ音乐",
			MenuPathString:     "QQ音乐 > 退出QQ音乐",
			Source:             "menu",
			AvailableByDefault: false,
		},
		{
			CommandID:          "menu:QQ音乐 > 设置…",
			MenuPathString:     "QQ音乐 > 设置…",
			Source:             "menu",
			AvailableByDefault: true,
		},
	}

	filtered := filterCommandSnapshots(commands, desktopCommandListOptions{})
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d", len(filtered))
	}
	if filtered[0].CommandID != "menu:QQ音乐 > 设置…" {
		t.Fatalf("filtered[0].CommandID = %q", filtered[0].CommandID)
	}
}

func TestCommandBlockedByPolicyRequiresUnsafeOverride(t *testing.T) {
	t.Parallel()

	command := desktopCommandSnapshot{
		CommandID:          "menu:QQ音乐 > 退出QQ音乐",
		MenuPathString:     "QQ音乐 > 退出QQ音乐",
		AvailableByDefault: false,
	}
	if err := commandBlockedByPolicy(command, false); err == nil {
		t.Fatal("commandBlockedByPolicy() error = nil")
	}
	if err := commandBlockedByPolicy(command, true); err != nil {
		t.Fatalf("commandBlockedByPolicy(allow) error = %v", err)
	}
}

func TestResolveAppDriverForTarget(t *testing.T) {
	t.Parallel()

	if driver := resolveAppDriverForTarget("QQMusic", ""); driver.ID != "qqmusic" {
		t.Fatalf("driver.ID = %q", driver.ID)
	}
	if driver := resolveAppDriverForTarget("", "com.adobe.PremierePro.15"); driver.ID != "premiere_pro" {
		t.Fatalf("driver.ID = %q", driver.ID)
	}
	if driver := resolveAppDriverForTarget("Unknown App", ""); driver.ID != "generic" {
		t.Fatalf("driver.ID = %q", driver.ID)
	}
}

func TestDriverCommandAliasesQQMusicPlayToggleOnlyMatchesPlayPause(t *testing.T) {
	t.Parallel()

	driver := resolveAppDriverForTarget("QQMusic", "")
	play := applyCommandPolicy(driver, desktopCommandSnapshot{
		Title:          "播放",
		MenuPath:       []string{"播放控制", "播放"},
		MenuPathString: "播放控制 > 播放",
		Enabled:        true,
	})
	if !commandHasAlias(play, "media.play_toggle") {
		t.Fatalf("play aliases = %#v", play.Aliases)
	}

	next := applyCommandPolicy(driver, desktopCommandSnapshot{
		Title:          "下一首",
		MenuPath:       []string{"播放控制", "下一首"},
		MenuPathString: "播放控制 > 下一首",
		Enabled:        true,
	})
	if commandHasAlias(next, "media.play_toggle") {
		t.Fatalf("next aliases = %#v", next.Aliases)
	}
}
