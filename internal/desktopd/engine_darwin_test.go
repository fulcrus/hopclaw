//go:build darwin

package desktopd

import "testing"

func TestDesktopWaitTargetParam(t *testing.T) {
	t.Parallel()

	value, err := desktopWaitTargetParam(nil, desktopWaitRunning, desktopWaitNone, desktopWaitRunning, desktopWaitWindow)
	if err != nil {
		t.Fatalf("desktopWaitTargetParam(nil) error = %v", err)
	}
	if value != desktopWaitRunning {
		t.Fatalf("desktopWaitTargetParam(nil) = %q", value)
	}

	value, err = desktopWaitTargetParam(map[string]any{"wait_until": "WINDOW"}, desktopWaitRunning, desktopWaitNone, desktopWaitRunning, desktopWaitWindow)
	if err != nil {
		t.Fatalf("desktopWaitTargetParam(WINDOW) error = %v", err)
	}
	if value != desktopWaitWindow {
		t.Fatalf("desktopWaitTargetParam(WINDOW) = %q", value)
	}

	if _, err := desktopWaitTargetParam(map[string]any{"wait_until": "interactive"}, desktopWaitRunning, desktopWaitRunning, desktopWaitWindow); err == nil {
		t.Fatal("desktopWaitTargetParam(interactive) error = nil, want unsupported wait target")
	}
}

func TestReadyStateForApp(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		app  desktopAppSnapshot
		want string
	}{
		{
			name: "process only",
			app:  desktopAppSnapshot{WindowCount: 0, Frontmost: false},
			want: desktopReadyProcessSeen,
		},
		{
			name: "window visible",
			app:  desktopAppSnapshot{WindowCount: 2, Frontmost: false},
			want: desktopReadyWindowVisible,
		},
		{
			name: "focused without window",
			app:  desktopAppSnapshot{WindowCount: 0, Frontmost: true},
			want: desktopReadyFocused,
		},
		{
			name: "interactive",
			app:  desktopAppSnapshot{WindowCount: 1, Frontmost: true},
			want: desktopReadyInteractive,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := readyStateForApp(tc.app); got != tc.want {
				t.Fatalf("readyStateForApp(%+v) = %q, want %q", tc.app, got, tc.want)
			}
		})
	}
}

func TestDesktopReadyStateMeets(t *testing.T) {
	t.Parallel()

	state := desktopReadyState{
		Found:       true,
		ReadyState:  desktopReadyInteractive,
		Interactive: true,
		App: desktopAppSnapshot{
			Frontmost:   true,
			WindowCount: 2,
		},
	}
	for _, waitUntil := range []string{
		desktopWaitNone,
		desktopWaitRunning,
		desktopWaitWindow,
		desktopWaitFocused,
		desktopWaitInteractive,
	} {
		if !state.meets(waitUntil) {
			t.Fatalf("state.meets(%q) = false", waitUntil)
		}
	}
}

func TestMatchTargetWindow(t *testing.T) {
	t.Parallel()

	app := desktopAppSnapshot{
		Windows: []desktopWindowSnapshot{
			{Index: 1, Title: "Welcome"},
			{Index: 2, Title: "Export Settings"},
		},
	}

	if got, ok := matchTargetWindow(app, "", 0); !ok || got.Index != 1 {
		t.Fatalf("matchTargetWindow(default) = %+v, %v", got, ok)
	}
	if got, ok := matchTargetWindow(app, "Export", 0); !ok || got.Index != 2 {
		t.Fatalf("matchTargetWindow(title) = %+v, %v", got, ok)
	}
	if got, ok := matchTargetWindow(app, "", 2); !ok || got.Title != "Export Settings" {
		t.Fatalf("matchTargetWindow(index) = %+v, %v", got, ok)
	}
	if _, ok := matchTargetWindow(app, "Missing", 0); ok {
		t.Fatal("matchTargetWindow(Missing) = ok, want false")
	}
}

func TestWantsTargetedScreenshot(t *testing.T) {
	t.Parallel()

	if wantsTargetedScreenshot(nil) {
		t.Fatal("wantsTargetedScreenshot(nil) = true")
	}
	if !wantsTargetedScreenshot(map[string]any{"app": "QQMusic"}) {
		t.Fatal("wantsTargetedScreenshot(app) = false")
	}
	if !wantsTargetedScreenshot(map[string]any{"window_index": 1}) {
		t.Fatal("wantsTargetedScreenshot(window_index) = false")
	}
}

func TestScreenshotRectArg(t *testing.T) {
	t.Parallel()

	rect, bounds, err := screenshotRectArg(desktopWindowSnapshot{
		Position: []int{90, 76},
		Size:     []int{1620, 930},
	})
	if err != nil {
		t.Fatalf("screenshotRectArg() error = %v", err)
	}
	if rect != "90,76,1620,930" {
		t.Fatalf("rect = %q", rect)
	}
	if len(bounds) != 4 || bounds[0] != 90 || bounds[3] != 930 {
		t.Fatalf("bounds = %#v", bounds)
	}
	if _, _, err := screenshotRectArg(desktopWindowSnapshot{}); err == nil {
		t.Fatal("screenshotRectArg(empty) error = nil")
	}
}

func TestShouldAttemptWindowRecovery(t *testing.T) {
	t.Parallel()

	if shouldAttemptWindowRecovery(desktopReadyState{}, desktopWaitRunning) {
		t.Fatal("shouldAttemptWindowRecovery(running) = true")
	}
	if shouldAttemptWindowRecovery(desktopReadyState{Found: false}, desktopWaitInteractive) {
		t.Fatal("shouldAttemptWindowRecovery(not found) = true")
	}
	if !shouldAttemptWindowRecovery(desktopReadyState{
		Found:      true,
		ReadyState: desktopReadyProcessSeen,
		App:        desktopAppSnapshot{WindowCount: 0},
	}, desktopWaitInteractive) {
		t.Fatal("shouldAttemptWindowRecovery(process_seen) = false")
	}
}

func TestAcceleratorFromAX(t *testing.T) {
	t.Parallel()

	if got := acceleratorFromAX("N", 0); got != "command+n" {
		t.Fatalf("acceleratorFromAX(command) = %q", got)
	}
	if got := acceleratorFromAX("P", 3); got != "command+shift+option+p" {
		t.Fatalf("acceleratorFromAX(command+shift+option) = %q", got)
	}
	if got := acceleratorFromAX("K", 8); got != "k" {
		t.Fatalf("acceleratorFromAX(no-command) = %q", got)
	}
	if got := acceleratorFromAX("", 0); got != "" {
		t.Fatalf("acceleratorFromAX(empty) = %q", got)
	}
}

func TestNormalizeCommandIDAndSplitMenuPath(t *testing.T) {
	t.Parallel()

	menuPath := splitMenuPath(" File > Open Recent > Clear Menu ")
	if len(menuPath) != 3 || menuPath[0] != "File" || menuPath[2] != "Clear Menu" {
		t.Fatalf("splitMenuPath() = %#v", menuPath)
	}
	if got := normalizeCommandID(menuPath); got != "menu:File > Open Recent > Clear Menu" {
		t.Fatalf("normalizeCommandID() = %q", got)
	}
}

func TestScoreCGWindowCandidate(t *testing.T) {
	t.Parallel()

	app := desktopAppSnapshot{Name: "QQMusic", PID: 4242}
	window := desktopWindowSnapshot{
		Title:    "发如雪",
		Position: []int{100, 120},
		Size:     []int{800, 600},
	}
	best := cgWindowInfo{
		WindowID:  101,
		OwnerName: "QQMusic",
		OwnerPID:  4242,
		Title:     "发如雪",
		Layer:     0,
		OnScreen:  true,
		Alpha:     1,
		Bounds:    []int{100, 120, 800, 600},
	}
	worse := cgWindowInfo{
		WindowID:  102,
		OwnerName: "QQMusic",
		OwnerPID:  4242,
		Title:     "其他窗口",
		Layer:     0,
		OnScreen:  true,
		Alpha:     1,
		Bounds:    []int{130, 150, 800, 600},
	}

	bestScore := scoreCGWindowCandidate(app, window, best)
	worseScore := scoreCGWindowCandidate(app, window, worse)
	if bestScore <= worseScore {
		t.Fatalf("bestScore=%d worseScore=%d", bestScore, worseScore)
	}
	if got := scoreCGWindowCandidate(app, window, cgWindowInfo{WindowID: 103, OwnerName: "Finder", OwnerPID: 7, OnScreen: true, Alpha: 1, Layer: 0, Bounds: []int{100, 120, 800, 600}}); got >= 0 {
		t.Fatalf("mismatched owner score = %d, want < 0", got)
	}
}

func TestMatchCGWindowAcceptsLayeredPIDOwnedWindow(t *testing.T) {
	t.Parallel()

	app := desktopAppSnapshot{
		Name:     "Adobe Premiere Pro 2021",
		BundleID: "com.adobe.PremierePro.15",
		PID:      97092,
	}
	window := desktopWindowSnapshot{
		Position: []int{0, 58},
		Size:     []int{1512, 869},
	}
	candidates := []cgWindowInfo{
		{
			WindowID:  75537,
			OwnerName: "Adobe Premiere Pro 2021",
			OwnerPID:  97092,
			Layer:     3,
			OnScreen:  true,
			Alpha:     1,
			Bounds:    []int{0, 58, 1512, 869},
		},
		{
			WindowID:  75538,
			OwnerName: "Finder",
			OwnerPID:  111,
			Layer:     0,
			OnScreen:  true,
			Alpha:     1,
			Bounds:    []int{0, 58, 1512, 869},
		},
	}

	got, ok := matchCGWindow(app, window, candidates)
	if !ok {
		t.Fatal("matchCGWindow() = not found, want layered PID-owned window")
	}
	if got.WindowID != 75537 {
		t.Fatalf("matchCGWindow() window_id = %d", got.WindowID)
	}
}

func TestRectIntersectionArea(t *testing.T) {
	t.Parallel()

	if got := rectIntersectionArea([]int{0, 0, 100, 100}, []int{50, 50, 100, 100}); got != 2500 {
		t.Fatalf("rectIntersectionArea(overlap) = %d", got)
	}
	if got := rectIntersectionArea([]int{0, 0, 100, 100}, []int{200, 200, 50, 50}); got != 0 {
		t.Fatalf("rectIntersectionArea(disjoint) = %d", got)
	}
}

func TestDetectCGWindowOcclusion(t *testing.T) {
	t.Parallel()

	target := cgWindowInfo{
		WindowID:  200,
		OwnerName: "抖音",
		Layer:     0,
		OnScreen:  true,
		Alpha:     1,
		Bounds:    []int{0, 0, 1000, 800},
	}
	covering := cgWindowInfo{
		WindowID:  201,
		OwnerName: "Finder",
		Layer:     0,
		OnScreen:  true,
		Alpha:     1,
		Bounds:    []int{100, 100, 500, 500},
	}
	if !detectCGWindowOcclusion(target, []cgWindowInfo{target, covering}) {
		t.Fatal("detectCGWindowOcclusion() = false, want true")
	}
	if detectCGWindowOcclusion(target, []cgWindowInfo{target}) {
		t.Fatal("detectCGWindowOcclusion(target only) = true")
	}
}
