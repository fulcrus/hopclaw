package toolruntime

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

// createTestPNG creates a 4x4 PNG image where (0,0) is red, (1,0) is green,
// (0,1) is blue, and (1,1) is white. This gives us enough pixels to test
// crop and resize meaningfully.
func createTestPNG(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	// Fill with a pattern: each quadrant a different colour.
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			switch {
			case x < 2 && y < 2:
				img.Set(x, y, color.NRGBA{R: 255, A: 255}) // red
			case x >= 2 && y < 2:
				img.Set(x, y, color.NRGBA{G: 255, A: 255}) // green
			case x < 2 && y >= 2:
				img.Set(x, y, color.NRGBA{B: 255, A: 255}) // blue
			default:
				img.Set(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255}) // white
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test PNG: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return path
}

func execMediaTool(t *testing.T, reg *Layer2Registry, name string, input map[string]any) string {
	t.Helper()
	results, err := reg.ExecuteBatch(
		context.Background(),
		&agent.Run{ID: "run-media"},
		&agent.Session{ID: "sess-media"},
		[]agent.ToolCall{{ID: "call-" + name, Name: name, Input: input}},
	)
	if err != nil {
		t.Fatalf("%s error: %v", name, err)
	}
	if len(results) != 1 {
		t.Fatalf("%s: expected 1 result, got %d", name, len(results))
	}
	return results[0].Content
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestMediaImageInfo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	createTestPNG(t, root, "test.png")

	reg := NewLayer2Registry(Layer2Config{Root: root})
	content := execMediaTool(t, reg, "media.image_info", map[string]any{
		"file": "test.png",
	})

	var payload struct {
		File       string `json:"file"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		Format     string `json:"format"`
		ColorModel string `json:"color_model"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v\nraw: %s", err, content)
	}
	if payload.Width != 4 || payload.Height != 4 {
		t.Fatalf("expected 4x4, got %dx%d", payload.Width, payload.Height)
	}
	if payload.Format != "png" {
		t.Fatalf("expected format png, got %q", payload.Format)
	}
	// PNG DecodeConfig reports color.NRGBAModel for images with alpha.
	// The exact model name depends on the PNG encoder/decoder behaviour;
	// accept any non-empty, non-"unknown" value.
	if payload.ColorModel == "" || payload.ColorModel == "unknown" {
		t.Fatalf("expected a known color_model, got %q", payload.ColorModel)
	}
	if payload.File != "test.png" {
		t.Fatalf("expected file test.png, got %q", payload.File)
	}
}

func TestMediaImageConvertPNGToJPEG(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	createTestPNG(t, root, "source.png")

	reg := NewLayer2Registry(Layer2Config{Root: root})
	content := execMediaTool(t, reg, "media.image_convert", map[string]any{
		"input":  "source.png",
		"output": "converted.jpg",
	})

	var payload struct {
		Input      string `json:"input"`
		Output     string `json:"output"`
		SrcFormat  string `json:"src_format"`
		DestFormat string `json:"dest_format"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v\nraw: %s", err, content)
	}
	if payload.SrcFormat != "png" {
		t.Fatalf("expected src_format png, got %q", payload.SrcFormat)
	}
	if payload.DestFormat != "jpeg" {
		t.Fatalf("expected dest_format jpeg, got %q", payload.DestFormat)
	}

	// Verify the output file exists and is a valid JPEG.
	outPath := filepath.Join(root, "converted.jpg")
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open converted.jpg: %v", err)
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("DecodeConfig(converted.jpg): %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("expected jpeg format, got %q", format)
	}
	if cfg.Width != 4 || cfg.Height != 4 {
		t.Fatalf("expected 4x4, got %dx%d", cfg.Width, cfg.Height)
	}
}

func TestMediaImageCrop(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	createTestPNG(t, root, "source.png")

	reg := NewLayer2Registry(Layer2Config{Root: root})
	content := execMediaTool(t, reg, "media.image_crop", map[string]any{
		"input":  "source.png",
		"output": "cropped.png",
		"x":      0,
		"y":      0,
		"width":  2,
		"height": 2,
	})

	var payload struct {
		Input  string `json:"input"`
		Output string `json:"output"`
		X      int    `json:"x"`
		Y      int    `json:"y"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v\nraw: %s", err, content)
	}
	if payload.Width != 2 || payload.Height != 2 {
		t.Fatalf("expected crop result 2x2, got %dx%d", payload.Width, payload.Height)
	}

	// Verify the output file has the right dimensions and the expected colour.
	outPath := filepath.Join(root, "cropped.png")
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open cropped.png: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("png.Decode(cropped.png): %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 2 || bounds.Dy() != 2 {
		t.Fatalf("expected 2x2 image, got %dx%d", bounds.Dx(), bounds.Dy())
	}
	// The top-left quadrant of the 4x4 source is red.
	r, g, b, a := img.At(bounds.Min.X, bounds.Min.Y).RGBA()
	if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 || a>>8 != 255 {
		t.Fatalf("expected red pixel at (0,0), got RGBA(%d,%d,%d,%d)", r>>8, g>>8, b>>8, a>>8)
	}
}

func TestMediaImageResize(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	createTestPNG(t, root, "source.png")

	reg := NewLayer2Registry(Layer2Config{Root: root})
	content := execMediaTool(t, reg, "media.image_resize", map[string]any{
		"input":  "source.png",
		"output": "resized.png",
		"width":  8,
		"height": 8,
	})

	var payload struct {
		Input      string `json:"input"`
		Output     string `json:"output"`
		SrcWidth   int    `json:"src_width"`
		SrcHeight  int    `json:"src_height"`
		DestWidth  int    `json:"dest_width"`
		DestHeight int    `json:"dest_height"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v\nraw: %s", err, content)
	}
	if payload.SrcWidth != 4 || payload.SrcHeight != 4 {
		t.Fatalf("expected src 4x4, got %dx%d", payload.SrcWidth, payload.SrcHeight)
	}
	if payload.DestWidth != 8 || payload.DestHeight != 8 {
		t.Fatalf("expected dest 8x8, got %dx%d", payload.DestWidth, payload.DestHeight)
	}

	// Verify the output file dimensions.
	outPath := filepath.Join(root, "resized.png")
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open resized.png: %v", err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("DecodeConfig(resized.png): %v", err)
	}
	if cfg.Width != 8 || cfg.Height != 8 {
		t.Fatalf("expected 8x8, got %dx%d", cfg.Width, cfg.Height)
	}
}

func TestMediaImageResizeAspectRatio(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	createTestPNG(t, root, "source.png")

	reg := NewLayer2Registry(Layer2Config{Root: root})

	// Specify only width; height should be computed to maintain aspect ratio.
	content := execMediaTool(t, reg, "media.image_resize", map[string]any{
		"input":  "source.png",
		"output": "resized_w.png",
		"width":  8,
	})

	var payload struct {
		DestWidth  int `json:"dest_width"`
		DestHeight int `json:"dest_height"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v\nraw: %s", err, content)
	}
	// 4x4 source scaled to width=8 -> height should be 8 (square image).
	if payload.DestWidth != 8 || payload.DestHeight != 8 {
		t.Fatalf("expected 8x8, got %dx%d", payload.DestWidth, payload.DestHeight)
	}
}
