package badge

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerLoadSave(t *testing.T) {
	manager := newTestManager(t)
	if got := manager.Config(); got != DefaultConfig() {
		t.Fatalf("default config = %#v, want %#v", got, DefaultConfig())
	}

	if err := manager.SetCurrent("b"); err != nil {
		t.Fatalf("SetCurrent(letter) error = %v", err)
	}
	if err := manager.SetColor("#0f0"); err != nil {
		t.Fatalf("SetColor() error = %v", err)
	}
	if err := manager.SetSize(6); err != nil {
		t.Fatalf("SetSize() error = %v", err)
	}
	if err := manager.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reloaded, err := NewManager()
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := reloaded.Config()
	if got.Current != "B" {
		t.Fatalf("Current = %q, want %q", got.Current, "B")
	}
	if got.Color != "#00ff00" {
		t.Fatalf("Color = %q, want %q", got.Color, "#00ff00")
	}
	if got.Size != 6 {
		t.Fatalf("Size = %d, want 6", got.Size)
	}
}

func TestManagerImportRemove(t *testing.T) {
	manager := newTestManager(t)
	sourcePath := filepath.Join(t.TempDir(), "source.png")
	writePNGBadge(t, sourcePath)

	if err := manager.ImportImage(0, sourcePath); err != nil {
		t.Fatalf("ImportImage() error = %v", err)
	}

	slotPath := filepath.Join(os.Getenv("HOME"), ".hopclaw", "avatars", "custom-0.png")
	stored := decodePNGFile(t, slotPath)
	if stored.Bounds().Dx() != internalImageSize || stored.Bounds().Dy() != internalImageSize {
		t.Fatalf("stored image size = %dx%d, want %dx%d", stored.Bounds().Dx(), stored.Bounds().Dy(), internalImageSize, internalImageSize)
	}
	center := stored.RGBAAt(internalImageSize/2, internalImageSize/2)
	if center.A == 0 || center.R != 0xff || center.G != 0xff || center.B != 0xff {
		t.Fatalf("stored center pixel = %#v, want opaque white", center)
	}

	if err := manager.SetCurrent("custom-0"); err != nil {
		t.Fatalf("SetCurrent(custom) error = %v", err)
	}
	if err := manager.SetColor("#ff6600"); err != nil {
		t.Fatalf("SetColor() error = %v", err)
	}
	current, err := manager.GetCurrentImage()
	if err != nil {
		t.Fatalf("GetCurrentImage() error = %v", err)
	}
	tinted := current.RGBAAt(internalImageSize/2, internalImageSize/2)
	if tinted.R != 0xff || tinted.G != 0x66 || tinted.B != 0x00 || tinted.A != 0xff {
		t.Fatalf("tinted center pixel = %#v, want orange", tinted)
	}

	if err := manager.RemoveImage(0); err != nil {
		t.Fatalf("RemoveImage() error = %v", err)
	}
	if _, err := os.Stat(slotPath); !os.IsNotExist(err) {
		t.Fatalf("slot file still exists after RemoveImage: %v", err)
	}
	if got := manager.Config().Current; got != "A" {
		t.Fatalf("Current after RemoveImage = %q, want %q", got, "A")
	}
}

func TestGetCurrentImage_FallbackOnDeletedCustom(t *testing.T) {
	manager := newTestManager(t)
	sourcePath := filepath.Join(t.TempDir(), "source.png")
	writePNGBadge(t, sourcePath)

	if err := manager.ImportImage(0, sourcePath); err != nil {
		t.Fatalf("ImportImage() error = %v", err)
	}
	if err := manager.SetCurrent("custom-0"); err != nil {
		t.Fatalf("SetCurrent(custom-0) error = %v", err)
	}

	beforeDelete, err := manager.GetCurrentImage()
	if err != nil {
		t.Fatalf("GetCurrentImage(before delete) error = %v", err)
	}
	if !hasOpaquePixel(beforeDelete) {
		t.Fatal("GetCurrentImage(before delete) returned an empty image")
	}

	if err := os.Remove(manager.customSlotPath(0)); err != nil {
		t.Fatalf("Remove(%q) error = %v", manager.customSlotPath(0), err)
	}

	afterDelete, err := manager.GetCurrentImage()
	if err != nil {
		t.Fatalf("GetCurrentImage(after delete) error = %v", err)
	}
	if got := manager.Current(); got != "A" {
		t.Fatalf("Current() after delete = %q, want %q", got, "A")
	}

	want := RenderLetter('A', manager.Config().Color)
	if !equalRGBA(afterDelete, want) {
		t.Fatal("GetCurrentImage(after delete) did not fall back to the letter A image")
	}
}

func TestManagerFixedSlots(t *testing.T) {
	manager := newTestManager(t)
	tmp := t.TempDir()
	writePNGBadge(t, filepath.Join(tmp, "slot0.png"))
	writeJPEGBadge(t, filepath.Join(tmp, "slot2.jpg"))

	if err := manager.ImportImage(0, filepath.Join(tmp, "slot0.png")); err != nil {
		t.Fatalf("ImportImage(slot0) error = %v", err)
	}
	if err := manager.ImportImage(2, filepath.Join(tmp, "slot2.jpg")); err != nil {
		t.Fatalf("ImportImage(slot2) error = %v", err)
	}
	if err := manager.RemoveImage(0); err != nil {
		t.Fatalf("RemoveImage(slot0) error = %v", err)
	}

	slots := manager.ListSlots()
	if len(slots) != letterSlotCount+customSlotCount {
		t.Fatalf("len(ListSlots()) = %d, want %d", len(slots), letterSlotCount+customSlotCount)
	}
	if slots[26].Occupied {
		t.Fatalf("custom slot 0 should be empty after removal")
	}
	if slots[27].Occupied {
		t.Fatalf("custom slot 1 should remain empty")
	}
	if !slots[28].Occupied {
		t.Fatalf("custom slot 2 should remain occupied")
	}
}

func TestManagerSlotLimit(t *testing.T) {
	manager := newTestManager(t)
	sourcePath := filepath.Join(t.TempDir(), "source.png")
	writePNGBadge(t, sourcePath)

	if err := manager.ImportImage(23, sourcePath); err != nil {
		t.Fatalf("ImportImage(last slot) error = %v", err)
	}
	if err := manager.ImportImage(24, sourcePath); err == nil || !strings.Contains(err.Error(), "between 0 and 23") {
		t.Fatalf("ImportImage(out of range) error = %v, want slot bounds error", err)
	}
}

func TestManagerSetCurrent(t *testing.T) {
	manager := newTestManager(t)
	if err := manager.SetCurrent("c"); err != nil {
		t.Fatalf("SetCurrent(letter) error = %v", err)
	}
	if got := manager.Config().Current; got != "C" {
		t.Fatalf("Current = %q, want %q", got, "C")
	}
	if err := manager.SetCurrent("custom-1"); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("SetCurrent(empty custom) error = %v, want empty-slot error", err)
	}

	sourcePath := filepath.Join(t.TempDir(), "source.png")
	writePNGBadge(t, sourcePath)
	if err := manager.ImportImage(1, sourcePath); err != nil {
		t.Fatalf("ImportImage() error = %v", err)
	}
	if err := manager.SetCurrent("custom-1"); err != nil {
		t.Fatalf("SetCurrent(populated custom) error = %v", err)
	}
	if got := manager.Config().Current; got != "custom-1" {
		t.Fatalf("Current = %q, want %q", got, "custom-1")
	}
}

func TestManagerSetSizeBounds(t *testing.T) {
	manager := newTestManager(t)

	if err := manager.SetSize(6); err != nil {
		t.Fatalf("SetSize(6) error = %v", err)
	}
	if err := manager.SetSize(7); err == nil || !strings.Contains(err.Error(), "between 2 and 6") {
		t.Fatalf("SetSize(7) error = %v, want bounds error", err)
	}
}

func TestRenderLetterAllLetters(t *testing.T) {
	for letter := 'A'; letter <= 'Z'; letter++ {
		img := RenderLetter(letter, "#00ff88")
		if img.Bounds().Dx() != 16 || img.Bounds().Dy() != 16 {
			t.Fatalf("%c bounds = %v, want 16x16", letter, img.Bounds())
		}
		if !hasOpaquePixel(img) {
			t.Fatalf("%c rendered with no visible pixels", letter)
		}
	}
}

func TestRenderLetterColor(t *testing.T) {
	img := RenderLetter('A', "#f60")
	var found color.RGBA
	var ok bool
	for y := 0; y < img.Bounds().Dy() && !ok; y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			pixel := img.RGBAAt(x, y)
			if pixel.A == 0 {
				continue
			}
			found = pixel
			ok = true
			break
		}
	}
	if !ok {
		t.Fatal("RenderLetter() produced no opaque pixels")
	}
	if found.R != 0xff || found.G != 0x66 || found.B != 0x00 || found.A != 0xff {
		t.Fatalf("letter pixel = %#v, want opaque #ff6600", found)
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	manager, err := NewManager()
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if err := manager.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return manager
}

func writePNGBadge(t *testing.T, path string) {
	t.Helper()
	img := testBadgeSourceImage()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%q) error = %v", path, err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("png.Encode(%q) error = %v", path, err)
	}
}

func writeJPEGBadge(t *testing.T, path string) {
	t.Helper()
	img := testBadgeSourceImage()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%q) error = %v", path, err)
	}
	defer file.Close()
	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatalf("jpeg.Encode(%q) error = %v", path, err)
	}
}

func testBadgeSourceImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 12, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 12; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		}
	}
	for y := 3; y < 9; y++ {
		for x := 3; x < 9; x++ {
			img.SetRGBA(x, y, color.RGBA{A: 0xff})
		}
	}
	return img
}

func decodePNGFile(t *testing.T, path string) *image.RGBA {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", path, err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatalf("png.Decode(%q) error = %v", path, err)
	}
	rgba, ok := img.(*image.RGBA)
	if ok {
		return rgba
	}
	converted := image.NewRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			converted.Set(x, y, img.At(x, y))
		}
	}
	return converted
}

func hasOpaquePixel(img *image.RGBA) bool {
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if img.RGBAAt(x, y).A != 0 {
				return true
			}
		}
	}
	return false
}

func equalRGBA(a, b *image.RGBA) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Bounds() != b.Bounds() {
		return false
	}
	for y := 0; y < a.Bounds().Dy(); y++ {
		for x := 0; x < a.Bounds().Dx(); x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				return false
			}
		}
	}
	return true
}
