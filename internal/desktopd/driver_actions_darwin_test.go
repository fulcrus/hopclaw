//go:build darwin

package desktopd

import (
	"slices"
	"testing"
)

func TestDriverActionsForKnownDrivers(t *testing.T) {
	t.Parallel()

	driver := resolveAppDriverForTarget("QQMusic", "")
	definitions := driverActionsFor(driver)
	ids := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		ids = append(ids, definition.ID)
	}
	for _, want := range []string{"app.search.focus", "search.submit", "media.play_toggle"} {
		if !slices.Contains(ids, want) {
			t.Fatalf("driver action %q missing from %#v", want, ids)
		}
	}
}

func TestDriverActionArgumentsMergeTopLevelParams(t *testing.T) {
	t.Parallel()

	args := driverActionArguments(map[string]any{
		"app":             "QQMusic",
		"driver_id":       "qqmusic",
		"semantic_action": "search.submit",
		"arguments": map[string]any{
			"query": "existing",
		},
		"text": "override",
	})
	if got := stringParam(args, "query"); got != "existing" {
		t.Fatalf("query = %q", got)
	}
	if got := stringParam(args, "text"); got != "override" {
		t.Fatalf("text = %q", got)
	}
	if got := stringParam(args, "app"); got != "" {
		t.Fatalf("app leaked into args = %q", got)
	}
}

func TestSelectDouyinSearchMatchPrefersTopSearchBar(t *testing.T) {
	t.Parallel()

	match, ok := selectDouyinSearchMatch([]map[string]any{
		{"text": "AI 搜索", "x": 157, "y": 255, "width": 65, "confidence": 0.3},
		{"text": "搜索你感兴趣的内容", "x": 705, "y": 106, "width": 148, "confidence": 1.0},
		{"text": "Q搜索", "x": 1122, "y": 103, "width": 58, "confidence": 0.5},
	})
	if !ok {
		t.Fatal("selectDouyinSearchMatch() = false")
	}
	if got := normalizeString(match["text"]); got != "搜索你感兴趣的内容" {
		t.Fatalf("text = %q", got)
	}
}

func TestEvaluateDouyinNextItemVerification(t *testing.T) {
	t.Parallel()

	before := driverWindowOCRSnapshot{
		Matches: []map[string]any{
			{"text": "刘德华演唱会抖音独家播出", "y": 780, "confidence": 1.0},
			{"text": "9月25日19:30", "y": 805, "confidence": 1.0},
			{"text": "刘德华歌曲", "y": 285, "confidence": 1.0},
		},
	}
	after := driverWindowOCRSnapshot{
		Matches: []map[string]any{
			{"text": "刘德华吃素", "y": 412, "confidence": 1.0},
			{"text": "刘德华模仿", "y": 380, "confidence": 1.0},
			{"text": "刘德华演唱会2026", "y": 347, "confidence": 1.0},
		},
	}

	verified, evidence := evaluateDouyinNextItemVerification(before, after)
	if !verified {
		t.Fatalf("verified = false evidence=%#v", evidence)
	}
}

func TestEvaluateDouyinNextItemVerificationUsesImageDistance(t *testing.T) {
	t.Parallel()

	before := driverWindowOCRSnapshot{Fingerprint: repeatedFingerprint(10)}
	after := driverWindowOCRSnapshot{Fingerprint: repeatedFingerprint(220)}

	verified, evidence := evaluateDouyinNextItemVerification(before, after)
	if !verified {
		t.Fatalf("verified = false evidence=%#v", evidence)
	}
	if evidence["image_verified"] != true {
		t.Fatalf("image_verified = %#v", evidence["image_verified"])
	}
}

func TestExtractPremiereTimecodes(t *testing.T) {
	t.Parallel()

	got := extractPremiereTimecodes([]map[string]any{
		{"text": "00:00:01:12", "confidence": 1.0},
		{"text": "01;23;45;06", "confidence": 1.0},
		{"text": "not-a-timecode", "confidence": 1.0},
	})
	want := []string{"00:00:01:12", "01:23:45:06"}
	if !slices.Equal(got, want) {
		t.Fatalf("timecodes = %#v want %#v", got, want)
	}
}

func TestEvaluatePremierePlaybackToggle(t *testing.T) {
	t.Parallel()

	beforeA := driverWindowOCRSnapshot{Matches: []map[string]any{{"text": "00:00:01:00", "confidence": 1.0}}}
	beforeB := driverWindowOCRSnapshot{Matches: []map[string]any{{"text": "00:00:01:00", "confidence": 1.0}}}
	afterA := driverWindowOCRSnapshot{Matches: []map[string]any{{"text": "00:00:01:05", "confidence": 1.0}}}
	afterB := driverWindowOCRSnapshot{Matches: []map[string]any{{"text": "00:00:01:22", "confidence": 1.0}}}

	verified, evidence := evaluatePremierePlaybackToggle(beforeA, beforeB, afterA, afterB)
	if !verified {
		t.Fatalf("verified = false evidence=%#v", evidence)
	}
	if evidence["toggle_state"] != "started_playback" {
		t.Fatalf("toggle_state = %#v", evidence["toggle_state"])
	}
}

func TestSelectPremiereRecentProjectMenuItemSkipsCurrentAndUntitled(t *testing.T) {
	t.Parallel()

	item, evidence, ok := selectPremiereRecentProjectMenuItem([]premiereRecentProjectMenuItem{
		{TopMenu: "文件", Parent: "打开最近使用的内容", Title: "/用户/tester/文稿/Adobe/Premiere Pro/15.0/未命名.prproj", Enabled: true},
		{TopMenu: "文件", Parent: "打开最近使用的内容", Title: "/用户/tester/文稿/Adobe/Premiere Pro/15.0/aaa.prproj", Enabled: true},
		{TopMenu: "文件", Parent: "打开最近使用的内容", Title: "/用户/tester/文稿/Adobe/Premiere Pro/15.0/0309.prproj", Enabled: true},
	}, "/Users/tester/Documents/Adobe/Premiere Pro/15.0/0309.prproj")
	if !ok {
		t.Fatalf("selectPremiereRecentProjectMenuItem() = false evidence=%#v", evidence)
	}
	if got := item.Title; got != "/用户/tester/文稿/Adobe/Premiere Pro/15.0/aaa.prproj" {
		t.Fatalf("selected title = %q", got)
	}
}

func TestSelectPremiereRecentProjectMenuItemFailsWhenOnlyCurrentOrUntitled(t *testing.T) {
	t.Parallel()

	_, evidence, ok := selectPremiereRecentProjectMenuItem([]premiereRecentProjectMenuItem{
		{TopMenu: "文件", Parent: "打开最近使用的内容", Title: "/用户/tester/文稿/Adobe/Premiere Pro/15.0/未命名.prproj", Enabled: true},
		{TopMenu: "文件", Parent: "打开最近使用的内容", Title: "/用户/tester/文稿/Adobe/Premiere Pro/15.0/0309.prproj", Enabled: true},
	}, "/Users/tester/Documents/Adobe/Premiere Pro/15.0/0309.prproj")
	if ok {
		t.Fatalf("selectPremiereRecentProjectMenuItem() = true evidence=%#v", evidence)
	}
}

func TestEvaluatePremierePlaybackPreconditionDetectsEmptySequence(t *testing.T) {
	t.Parallel()

	blocked, evidence := evaluatePremierePlaybackPrecondition(driverWindowOCRSnapshot{
		Matches: []map[string]any{
			{"text": "在此处放下媒体以创建序列。", "confidence": 1.0, "x": 748, "y": 826},
		},
	})
	if !blocked {
		t.Fatalf("blocked = false evidence=%#v", evidence)
	}
	if evidence["reason"] != "no_playable_sequence" {
		t.Fatalf("reason = %#v", evidence["reason"])
	}
}

func TestEvaluatePremierePlaybackToggleUsesVisualMotion(t *testing.T) {
	t.Parallel()

	beforeA := driverWindowOCRSnapshot{Fingerprint: repeatedFingerprint(90)}
	beforeB := driverWindowOCRSnapshot{Fingerprint: repeatedFingerprint(92)}
	afterA := driverWindowOCRSnapshot{Fingerprint: repeatedFingerprint(10)}
	afterB := driverWindowOCRSnapshot{Fingerprint: repeatedFingerprint(200)}

	verified, evidence := evaluatePremierePlaybackToggle(beforeA, beforeB, afterA, afterB)
	if !verified {
		t.Fatalf("verified = false evidence=%#v", evidence)
	}
	if evidence["toggle_state"] != "started_playback" {
		t.Fatalf("toggle_state = %#v", evidence["toggle_state"])
	}
}

func TestFingerprintDistance(t *testing.T) {
	t.Parallel()

	a := repeatedFingerprint(10)
	b := repeatedFingerprint(10)
	c := repeatedFingerprint(250)
	if got := fingerprintDistance(a, b); got != 0 {
		t.Fatalf("distance same = %v", got)
	}
	if got := fingerprintDistance(a, c); got <= 0 {
		t.Fatalf("distance different = %v", got)
	}
}

func repeatedFingerprint(value uint8) []uint8 {
	out := make([]uint8, 12*12)
	for i := range out {
		out[i] = value
	}
	return out
}
