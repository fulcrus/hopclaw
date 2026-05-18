package media

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"

	"golang.org/x/image/draw"
)

// ---------------------------------------------------------------------------
// Resize constants
// ---------------------------------------------------------------------------

const (
	// defaultMaxDimension is the maximum width or height for LLM vision input.
	defaultMaxDimension = 2048

	// defaultJPEGQuality is the default JPEG compression quality (1-100).
	defaultJPEGQuality = 85

	// minQuality is the minimum acceptable quality value.
	minQuality = 1

	// maxQuality is the maximum acceptable quality value.
	maxQuality = 100

	// minDimension is the minimum acceptable dimension for resize output.
	minDimension = 1
)

// ---------------------------------------------------------------------------
// Resize strategy
// ---------------------------------------------------------------------------

// ResizeStrategy controls how images are resized to fit target dimensions.
type ResizeStrategy string

const (
	// ResizeFit preserves aspect ratio, fitting within the bounding box.
	ResizeFit ResizeStrategy = "fit"
	// ResizeFill crops to fill the bounding box exactly.
	ResizeFill ResizeStrategy = "fill"
	// ResizeThumbnail creates a small square thumbnail.
	ResizeThumbnail ResizeStrategy = "thumbnail"
)

// ---------------------------------------------------------------------------
// Resize configuration
// ---------------------------------------------------------------------------

// ResizeConfig controls image resize and optimization behaviour.
type ResizeConfig struct {
	MaxWidth  int            `json:"max_width" yaml:"max_width"`
	MaxHeight int            `json:"max_height" yaml:"max_height"`
	Quality   int            `json:"quality" yaml:"quality"`
	Format    string         `json:"format" yaml:"format"`     // "jpeg", "png"
	Strategy  ResizeStrategy `json:"strategy" yaml:"strategy"` // "fit", "fill", "thumbnail"
	StripEXIF bool           `json:"strip_exif" yaml:"strip_exif"`
}

// ---------------------------------------------------------------------------
// ResizeImage
// ---------------------------------------------------------------------------

// ResizeImage decodes, resizes, and re-encodes image data according to cfg.
// It returns the processed bytes, the output MIME type, and any error.
// Supported input formats: JPEG, PNG, GIF, BMP (via stdlib decoders).
func ResizeImage(data []byte, mimeType string, cfg ResizeConfig) ([]byte, string, error) {
	if len(data) == 0 {
		return nil, "", fmt.Errorf("media/resize: image data is required")
	}

	cfg = applyResizeDefaults(cfg)

	if err := validateResizeConfig(cfg); err != nil {
		return nil, "", err
	}

	// Decode the source image.
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("media/resize: decoding image: %w", err)
	}

	// Calculate target dimensions.
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	dstW, dstH := calculateDimensions(srcW, srcH, cfg)

	// Resize the image using high-quality interpolation.
	dst := resizeWithStrategy(src, srcW, srcH, dstW, dstH, cfg.Strategy)

	// Encode to the target format.
	outData, outMIME, err := encodeImage(dst, cfg)
	if err != nil {
		return nil, "", err
	}

	return outData, outMIME, nil
}

// ---------------------------------------------------------------------------
// ResizeForVision
// ---------------------------------------------------------------------------

// ResizeForVision is a convenience function that resizes images to be optimal
// for LLM vision APIs. It caps dimensions at defaultMaxDimension and
// converts PNGs with no transparency to JPEG for size reduction.
func ResizeForVision(data []byte, mimeType string) ([]byte, string, error) {
	cfg := ResizeConfig{
		MaxWidth:  defaultMaxDimension,
		MaxHeight: defaultMaxDimension,
		Quality:   defaultJPEGQuality,
		Strategy:  ResizeFit,
		StripEXIF: true,
	}

	// Convert non-transparent PNGs to JPEG for smaller payload.
	if mimeType == "image/png" {
		src, _, err := image.Decode(bytes.NewReader(data))
		if err == nil && !hasTransparency(src) {
			cfg.Format = "jpeg"
		}
	}

	return ResizeImage(data, mimeType, cfg)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// applyResizeDefaults fills in zero-value fields with sensible defaults.
func applyResizeDefaults(cfg ResizeConfig) ResizeConfig {
	if cfg.MaxWidth <= 0 {
		cfg.MaxWidth = defaultMaxDimension
	}
	if cfg.MaxHeight <= 0 {
		cfg.MaxHeight = defaultMaxDimension
	}
	if cfg.Quality <= 0 {
		cfg.Quality = defaultJPEGQuality
	}
	if cfg.Strategy == "" {
		cfg.Strategy = ResizeFit
	}
	return cfg
}

// validateResizeConfig validates resize configuration values.
func validateResizeConfig(cfg ResizeConfig) error {
	if cfg.Quality < minQuality || cfg.Quality > maxQuality {
		return fmt.Errorf("media/resize: quality must be between %d and %d", minQuality, maxQuality)
	}
	if cfg.MaxWidth < minDimension {
		return fmt.Errorf("media/resize: max_width must be at least %d", minDimension)
	}
	if cfg.MaxHeight < minDimension {
		return fmt.Errorf("media/resize: max_height must be at least %d", minDimension)
	}
	switch cfg.Format {
	case "", "jpeg", "png":
		// valid
	default:
		return fmt.Errorf("media/resize: unsupported output format %q", cfg.Format)
	}
	return nil
}

// calculateDimensions computes the target width and height based on the
// source dimensions, max bounds, and resize strategy.
func calculateDimensions(srcW, srcH int, cfg ResizeConfig) (int, int) {
	maxW := cfg.MaxWidth
	maxH := cfg.MaxHeight

	switch cfg.Strategy {
	case ResizeThumbnail:
		// Thumbnail is always square, using the smaller of maxW/maxH.
		size := maxW
		if maxH < size {
			size = maxH
		}
		return size, size

	case ResizeFill:
		return maxW, maxH

	default: // ResizeFit
		// If image already fits, keep original dimensions.
		if srcW <= maxW && srcH <= maxH {
			return srcW, srcH
		}

		ratioW := float64(maxW) / float64(srcW)
		ratioH := float64(maxH) / float64(srcH)
		ratio := math.Min(ratioW, ratioH)

		dstW := int(math.Round(float64(srcW) * ratio))
		dstH := int(math.Round(float64(srcH) * ratio))

		if dstW < minDimension {
			dstW = minDimension
		}
		if dstH < minDimension {
			dstH = minDimension
		}

		return dstW, dstH
	}
}

// resizeWithStrategy applies the resize strategy and returns the output image.
func resizeWithStrategy(src image.Image, srcW, srcH, dstW, dstH int, strategy ResizeStrategy) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))

	switch strategy {
	case ResizeFill:
		// Scale to cover the target area, then crop center.
		scaleW := float64(dstW) / float64(srcW)
		scaleH := float64(dstH) / float64(srcH)
		scale := math.Max(scaleW, scaleH)

		scaledW := int(math.Round(float64(srcW) * scale))
		scaledH := int(math.Round(float64(srcH) * scale))

		scaled := image.NewRGBA(image.Rect(0, 0, scaledW, scaledH))
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), src, src.Bounds(), draw.Over, nil)

		// Center crop.
		offsetX := (scaledW - dstW) / 2
		offsetY := (scaledH - dstH) / 2
		cropRect := image.Rect(offsetX, offsetY, offsetX+dstW, offsetY+dstH)
		draw.Copy(dst, image.Point{}, scaled, cropRect, draw.Src, nil)

	case ResizeThumbnail:
		// Square crop from center of source, then scale down.
		cropSize := srcW
		if srcH < cropSize {
			cropSize = srcH
		}
		offsetX := (srcW - cropSize) / 2
		offsetY := (srcH - cropSize) / 2

		srcBounds := src.Bounds()
		cropRect := image.Rect(
			srcBounds.Min.X+offsetX,
			srcBounds.Min.Y+offsetY,
			srcBounds.Min.X+offsetX+cropSize,
			srcBounds.Min.Y+offsetY+cropSize,
		)
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, cropRect, draw.Over, nil)

	default: // ResizeFit
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	}

	return dst
}

// encodeImage encodes an image to the specified format.
func encodeImage(img image.Image, cfg ResizeConfig) ([]byte, string, error) {
	var buf bytes.Buffer

	format := cfg.Format
	if format == "" {
		format = "jpeg"
	}

	switch format {
	case "jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: cfg.Quality}); err != nil {
			return nil, "", fmt.Errorf("media/resize: encoding jpeg: %w", err)
		}
		return buf.Bytes(), "image/jpeg", nil

	case "png":
		encoder := &png.Encoder{CompressionLevel: png.DefaultCompression}
		if err := encoder.Encode(&buf, img); err != nil {
			return nil, "", fmt.Errorf("media/resize: encoding png: %w", err)
		}
		return buf.Bytes(), "image/png", nil

	default:
		return nil, "", fmt.Errorf("media/resize: unsupported format %q", format)
	}
}

// hasTransparency checks whether an image contains any transparent pixels.
func hasTransparency(img image.Image) bool {
	bounds := img.Bounds()

	// Sample a grid of pixels rather than checking every pixel for performance.
	const sampleStep = 10
	for y := bounds.Min.Y; y < bounds.Max.Y; y += sampleStep {
		for x := bounds.Min.X; x < bounds.Max.X; x += sampleStep {
			_, _, _, a := img.At(x, y).RGBA()
			if a < uint32(color.Opaque.A) {
				return true
			}
		}
	}
	return false
}
