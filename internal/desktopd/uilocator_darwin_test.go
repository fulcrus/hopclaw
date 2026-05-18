//go:build darwin

package desktopd

import "testing"

func TestRegionFromWindow(t *testing.T) {
	t.Parallel()

	region, ok := regionFromWindow(desktopWindowSnapshot{
		Position: []int{120, 45},
		Size:     []int{800, 600},
	})
	if !ok {
		t.Fatal("regionFromWindow ok = false, want true")
	}
	if region.X != 120 || region.Y != 45 || region.Width != 800 || region.Height != 600 {
		t.Fatalf("region = %+v", region)
	}
}

func TestRegionFromWindowRejectsInvalidBounds(t *testing.T) {
	t.Parallel()

	if _, ok := regionFromWindow(desktopWindowSnapshot{
		Position: []int{10},
		Size:     []int{500, 300},
	}); ok {
		t.Fatal("regionFromWindow ok = true, want false for invalid position")
	}
	if _, ok := regionFromWindow(desktopWindowSnapshot{
		Position: []int{10, 20},
		Size:     []int{0, 300},
	}); ok {
		t.Fatal("regionFromWindow ok = true, want false for zero width")
	}
}

func TestRegionFromAppUsesFirstUsableWindow(t *testing.T) {
	t.Parallel()

	region, ok := regionFromApp(desktopAppSnapshot{
		Windows: []desktopWindowSnapshot{
			{Position: []int{0}, Size: []int{10, 10}},
			{Position: []int{25, 35}, Size: []int{640, 480}},
		},
	})
	if !ok {
		t.Fatal("regionFromApp ok = false, want true")
	}
	if region.X != 25 || region.Y != 35 || region.Width != 640 || region.Height != 480 {
		t.Fatalf("region = %+v", region)
	}
}

func TestMatchToScreenMapAppliesScaleAndWindowOffset(t *testing.T) {
	t.Parallel()

	match := matchToScreenMap(ocrMatch{
		Text:       "Submit",
		CenterX:    200,
		CenterY:    100,
		PixelW:     80,
		PixelH:     40,
		Confidence: 0.92,
	}, 2.0, ocrCaptureRegion{X: 50, Y: 70}, true)

	if got := match["x"]; got != 150 {
		t.Fatalf("x = %v, want 150", got)
	}
	if got := match["y"]; got != 120 {
		t.Fatalf("y = %v, want 120", got)
	}
	if got := match["width"]; got != 40 {
		t.Fatalf("width = %v, want 40", got)
	}
	if got := match["height"]; got != 20 {
		t.Fatalf("height = %v, want 20", got)
	}
}
