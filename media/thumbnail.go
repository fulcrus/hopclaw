package media

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"

	"golang.org/x/image/draw"
)

// ---------------------------------------------------------------------------
// Thumbnail constants
// ---------------------------------------------------------------------------

const (
	// ThumbnailSmall is the small thumbnail size in pixels.
	ThumbnailSmall = 64
	// ThumbnailMedium is the medium thumbnail size in pixels.
	ThumbnailMedium = 128
	// ThumbnailLarge is the large thumbnail size in pixels.
	ThumbnailLarge = 256

	// thumbnailJPEGQuality is the JPEG quality used for thumbnails.
	thumbnailJPEGQuality = 80

	// thumbnailMinSize is the minimum valid thumbnail size.
	thumbnailMinSize = 8
	// thumbnailMaxSize is the maximum valid thumbnail size.
	thumbnailMaxSize = 1024
)

// ---------------------------------------------------------------------------
// GenerateThumbnail
// ---------------------------------------------------------------------------

// GenerateThumbnail creates a square JPEG thumbnail of the given size from
// image data. It center-crops the source image to a square, then scales to
// the target size. Output is deterministic for the same input.
func GenerateThumbnail(data []byte, mimeType string, size int) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("media/thumbnail: image data is required")
	}
	if size < thumbnailMinSize || size > thumbnailMaxSize {
		return nil, fmt.Errorf("media/thumbnail: size must be between %d and %d", thumbnailMinSize, thumbnailMaxSize)
	}

	kind := DetectKind(mimeType)
	switch kind {
	case KindImage:
		return generateImageThumbnail(data, size)
	case KindAudio:
		return generatePlaceholderThumbnail(size, audioPlaceholderColor, "audio")
	case KindVideo:
		return generatePlaceholderThumbnail(size, videoPlaceholderColor, "video")
	default:
		return generatePlaceholderThumbnail(size, unknownPlaceholderColor, "file")
	}
}

// ---------------------------------------------------------------------------
// Image thumbnail generation
// ---------------------------------------------------------------------------

// generateImageThumbnail decodes image data, center-crops to square,
// and scales to the target size.
func generateImageThumbnail(data []byte, size int) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("media/thumbnail: decoding image: %w", err)
	}

	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	// Center-crop to square.
	cropSize := srcW
	if srcH < cropSize {
		cropSize = srcH
	}
	offsetX := (srcW - cropSize) / 2
	offsetY := (srcH - cropSize) / 2

	cropRect := image.Rect(
		bounds.Min.X+offsetX,
		bounds.Min.Y+offsetY,
		bounds.Min.X+offsetX+cropSize,
		bounds.Min.Y+offsetY+cropSize,
	)

	// Scale to target size.
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, cropRect, draw.Over, nil)

	// Encode as JPEG.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: thumbnailJPEGQuality}); err != nil {
		return nil, fmt.Errorf("media/thumbnail: encoding thumbnail: %w", err)
	}

	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// Placeholder thumbnail generation
// ---------------------------------------------------------------------------

// Placeholder colors for non-image media types.
var (
	audioPlaceholderColor   = color.RGBA{R: 76, G: 175, B: 80, A: 255}   // green
	videoPlaceholderColor   = color.RGBA{R: 33, G: 150, B: 243, A: 255}  // blue
	unknownPlaceholderColor = color.RGBA{R: 158, G: 158, B: 158, A: 255} // gray
)

// iconPatterns defines simple pixel icon patterns for each media type.
// Each pattern is a 7x7 boolean grid where true means draw a white pixel.
var iconPatterns = map[string][]string{
	"audio": {
		"  ###  ",
		" #   # ",
		" #   # ",
		" #   # ",
		"##  ## ",
		"##  ## ",
		"##  ## ",
	},
	"video": {
		"#######",
		"#     #",
		"# ##  #",
		"# ### #",
		"# ##  #",
		"#     #",
		"#######",
	},
	"file": {
		" ####  ",
		" #  ## ",
		" #   # ",
		" #   # ",
		" #   # ",
		" #   # ",
		" ##### ",
	},
}

// iconPatternSize is the size of each icon pattern grid.
const iconPatternSize = 7

// generatePlaceholderThumbnail creates a colored rectangle with a simple
// pixel-art icon rendered at center for the given media type.
func generatePlaceholderThumbnail(size int, bg color.RGBA, mediaType string) ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))

	// Fill background.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dst.SetRGBA(x, y, bg)
		}
	}

	// Draw icon pattern if available.
	pattern, ok := iconPatterns[mediaType]
	if ok {
		drawIconPattern(dst, size, pattern)
	}

	// Encode as JPEG.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: thumbnailJPEGQuality}); err != nil {
		return nil, fmt.Errorf("media/thumbnail: encoding placeholder: %w", err)
	}

	return buf.Bytes(), nil
}

// drawIconPattern renders a simple pixel icon pattern centered on the image.
func drawIconPattern(dst *image.RGBA, size int, pattern []string) {
	// Scale the icon to about 40% of the thumbnail size.
	const iconScalePercent = 40
	pixelSize := (size * iconScalePercent) / (iconPatternSize * 100)
	if pixelSize < 1 {
		pixelSize = 1
	}

	iconSize := iconPatternSize * pixelSize
	offsetX := (size - iconSize) / 2
	offsetY := (size - iconSize) / 2

	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	for row := 0; row < iconPatternSize && row < len(pattern); row++ {
		for col := 0; col < iconPatternSize && col < len(pattern[row]); col++ {
			if pattern[row][col] != ' ' {
				// Fill the scaled pixel block.
				for py := 0; py < pixelSize; py++ {
					for px := 0; px < pixelSize; px++ {
						x := offsetX + col*pixelSize + px
						y := offsetY + row*pixelSize + py
						if x < size && y < size {
							dst.SetRGBA(x, y, white)
						}
					}
				}
			}
		}
	}
}
