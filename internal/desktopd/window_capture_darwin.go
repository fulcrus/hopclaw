//go:build darwin

package desktopd

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const cgWindowMinMatchScore = 80

type cgWindowInfo struct {
	WindowID  int     `json:"window_id"`
	OwnerName string  `json:"owner_name"`
	OwnerPID  int     `json:"owner_pid"`
	Title     string  `json:"title"`
	Layer     int     `json:"layer"`
	OnScreen  bool    `json:"onscreen"`
	Alpha     float64 `json:"alpha"`
	Bounds    []int   `json:"bounds"`
}

type cgWindowListPayload struct {
	Windows []cgWindowInfo `json:"windows"`
}

func cgWindowList(ctx context.Context) ([]cgWindowInfo, error) {
	script := `(() => {
  ObjC.import("CoreGraphics");
  ObjC.import("Foundation");
  const list = ObjC.castRefToObject($.CGWindowListCopyWindowInfo(
    $.kCGWindowListOptionOnScreenOnly | $.kCGWindowListExcludeDesktopElements,
    $.kCGNullWindowID
  ));
  const out = [];
  for (let i = 0; i < list.count; i++) {
    const raw = ObjC.deepUnwrap(list.objectAtIndex(i));
    const bounds = raw.kCGWindowBounds || {};
    out.push({
      window_id: Number(raw.kCGWindowNumber || 0),
      owner_name: String(raw.kCGWindowOwnerName || ""),
      owner_pid: Number(raw.kCGWindowOwnerPID || 0),
      title: String(raw.kCGWindowName || ""),
      layer: Number(raw.kCGWindowLayer || 0),
      onscreen: !!raw.kCGWindowIsOnscreen,
      alpha: Number(raw.kCGWindowAlpha || 0),
      bounds: [
        Number(bounds.X || 0),
        Number(bounds.Y || 0),
        Number(bounds.Width || 0),
        Number(bounds.Height || 0)
      ]
    });
  }
  return JSON.stringify({windows: out});
})()`

	var payload cgWindowListPayload
	if err := runJXAJSON(ctx, script, &payload); err != nil {
		return nil, err
	}
	return payload.Windows, nil
}

func captureWindowContent(ctx context.Context, windowID int, path string) error {
	if windowID <= 0 {
		return fmt.Errorf("invalid window_id %d", windowID)
	}
	args := []string{"-x", "-tpng", "-o", "-l", strconv.Itoa(windowID), path}
	cmd := exec.CommandContext(ctx, "screencapture", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("window capture: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func resolveWindowContentTarget(ctx context.Context, app desktopAppSnapshot, window desktopWindowSnapshot) (cgWindowInfo, bool, error) {
	windows, err := cgWindowList(ctx)
	if err != nil {
		return cgWindowInfo{}, false, err
	}
	target, ok := matchCGWindow(app, window, windows)
	if !ok {
		return cgWindowInfo{}, false, fmt.Errorf("native window content target not found")
	}
	return target, detectCGWindowOcclusion(target, windows), nil
}

func matchCGWindow(app desktopAppSnapshot, window desktopWindowSnapshot, windows []cgWindowInfo) (cgWindowInfo, bool) {
	bestScore := -1
	best := cgWindowInfo{}
	for _, candidate := range windows {
		score := scoreCGWindowCandidate(app, window, candidate)
		if score > bestScore {
			bestScore = score
			best = candidate
		}
	}
	if bestScore < cgWindowMinMatchScore {
		return cgWindowInfo{}, false
	}
	return best, true
}

func scoreCGWindowCandidate(app desktopAppSnapshot, window desktopWindowSnapshot, candidate cgWindowInfo) int {
	if candidate.WindowID <= 0 || !candidate.OnScreen || candidate.Alpha <= 0 || len(candidate.Bounds) < 4 {
		return -1
	}

	ownerScore, ok := ownerIdentityScore(app, candidate)
	if !ok {
		return -1
	}
	if rectArea(candidate.Bounds) <= 0 {
		return -1
	}

	score := ownerScore + layerPreferenceScore(candidate.Layer)
	wantTitle := strings.TrimSpace(window.Title)
	gotTitle := strings.TrimSpace(candidate.Title)
	switch {
	case wantTitle != "" && gotTitle == wantTitle:
		score += 50
	case wantTitle != "" && strings.Contains(gotTitle, wantTitle):
		score += 25
	case wantTitle != "" && gotTitle == "":
		score -= 25
	case wantTitle == "" && gotTitle == "":
		score += 10
	}
	if len(window.Position) >= 2 && len(window.Size) >= 2 {
		wantBounds := []int{window.Position[0], window.Position[1], window.Size[0], window.Size[1]}
		score += boundsSimilarityScore(wantBounds, candidate.Bounds)
	}
	return score
}

func ownerIdentityScore(app desktopAppSnapshot, candidate cgWindowInfo) (int, bool) {
	if app.PID > 0 && candidate.OwnerPID == app.PID {
		return 170, true
	}
	if sameWindowOwner(app.Name, candidate.OwnerName) {
		return 130, true
	}
	if strings.TrimSpace(app.BundleID) != "" && sameWindowOwner(app.BundleID, candidate.OwnerName) {
		return 100, true
	}
	return 0, false
}

func sameWindowOwner(want, got string) bool {
	want = normalizeWindowOwner(want)
	got = normalizeWindowOwner(got)
	if want == "" || got == "" {
		return false
	}
	if want == got {
		return true
	}
	return strings.Contains(got, want) || strings.Contains(want, got)
}

func normalizeWindowOwner(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, ".app", "")
	return value
}

func layerPreferenceScore(layer int) int {
	switch {
	case layer == 0:
		return 20
	case layer > 0 && layer <= 3:
		return 10
	case layer > 3 && layer <= 8:
		return 2
	case layer < 0:
		return -100
	default:
		return -10
	}
}

func boundsSimilarityScore(want, got []int) int {
	if len(want) < 4 || len(got) < 4 {
		return 0
	}
	score := 0
	if want[0] == got[0] && want[1] == got[1] {
		score += 20
	}
	if want[2] == got[2] && want[3] == got[3] {
		score += 20
	}
	intersection := rectIntersectionArea(want, got)
	if intersection > 0 {
		score += 10
	}
	if intersection == rectArea(want) && intersection == rectArea(got) {
		score += 20
	}
	return score
}

func detectCGWindowOcclusion(target cgWindowInfo, windows []cgWindowInfo) bool {
	if len(target.Bounds) < 4 {
		return false
	}
	targetArea := rectArea(target.Bounds)
	if targetArea <= 0 {
		return false
	}
	for _, candidate := range windows {
		if candidate.WindowID == target.WindowID {
			continue
		}
		if candidate.Layer != 0 || !candidate.OnScreen || candidate.Alpha <= 0 || len(candidate.Bounds) < 4 {
			continue
		}
		overlap := rectIntersectionArea(target.Bounds, candidate.Bounds)
		if overlap > targetArea/10 {
			return true
		}
	}
	return false
}

func rectArea(bounds []int) int {
	if len(bounds) < 4 || bounds[2] <= 0 || bounds[3] <= 0 {
		return 0
	}
	return bounds[2] * bounds[3]
}

func rectIntersectionArea(a, b []int) int {
	if len(a) < 4 || len(b) < 4 {
		return 0
	}
	left := maxInt(a[0], b[0])
	top := maxInt(a[1], b[1])
	right := minInt(a[0]+a[2], b[0]+b[2])
	bottom := minInt(a[1]+a[3], b[1]+b[3])
	if right <= left || bottom <= top {
		return 0
	}
	return (right - left) * (bottom - top)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func windowBoundsFromCGWindow(target cgWindowInfo) []int {
	return append([]int(nil), target.Bounds...)
}
