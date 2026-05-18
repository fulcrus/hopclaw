package media

import (
	"context"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// OCR constants
// ---------------------------------------------------------------------------

const (
	// ocrMaxPageCount is the maximum number of pages to process sequentially.
	ocrMaxPageCount = 50
)

// ---------------------------------------------------------------------------
// OCR configuration and result types
// ---------------------------------------------------------------------------

// OCROutputFormat specifies the desired output format for OCR results.
type OCROutputFormat string

const (
	// OCRFormatText produces plain text output.
	OCRFormatText OCROutputFormat = "text"
	// OCRFormatJSON produces structured JSON output.
	OCRFormatJSON OCROutputFormat = "json"
	// OCRFormatMarkdown produces markdown-formatted output.
	OCRFormatMarkdown OCROutputFormat = "markdown"
)

// OCRConfig controls OCR processing behaviour.
type OCRConfig struct {
	LanguageHint      string          `json:"language_hint,omitempty" yaml:"language_hint"`
	OutputFormat      OCROutputFormat `json:"output_format,omitempty" yaml:"output_format"`
	PreferredProvider string          `json:"preferred_provider,omitempty" yaml:"preferred_provider"`
}

// OCRResult holds the result of an OCR operation.
type OCRResult struct {
	Text             string      `json:"text"`
	Confidence       float64     `json:"confidence,omitempty"`
	LanguageDetected string      `json:"language_detected,omitempty"`
	Regions          []OCRRegion `json:"regions,omitempty"`
}

// OCRRegion represents a detected text region with optional bounding box.
type OCRRegion struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence,omitempty"`
	X          int     `json:"x,omitempty"`
	Y          int     `json:"y,omitempty"`
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
}

// ---------------------------------------------------------------------------
// OCR provider interface
// ---------------------------------------------------------------------------

// OCRProvider defines the interface for OCR backends.
type OCRProvider interface {
	// ID returns the provider identifier.
	ID() string
	// PerformOCR extracts text from an image.
	PerformOCR(ctx context.Context, imageData []byte, mimeType string, cfg OCRConfig) (*OCRResult, error)
}

// ---------------------------------------------------------------------------
// Vision-based OCR provider
// ---------------------------------------------------------------------------

// VisionOCRProvider uses vision-capable LLM providers to perform OCR.
type VisionOCRProvider struct {
	registry *Registry
}

// NewVisionOCRProvider creates an OCR provider backed by vision models.
func NewVisionOCRProvider(registry *Registry) *VisionOCRProvider {
	return &VisionOCRProvider{registry: registry}
}

// ID returns the provider identifier.
func (p *VisionOCRProvider) ID() string { return "vision" }

// PerformOCR extracts text from an image using a vision model.
func (p *VisionOCRProvider) PerformOCR(ctx context.Context, imageData []byte, mimeType string, cfg OCRConfig) (*OCRResult, error) {
	if len(imageData) == 0 {
		return nil, fmt.Errorf("media/ocr: image data is required")
	}

	provider, err := p.registry.FindImageProvider(cfg.PreferredProvider)
	if err != nil {
		return nil, fmt.Errorf("media/ocr: %w", err)
	}

	prompt := buildOCRPrompt(cfg)

	result, err := provider.DescribeImage(ctx, ImageRequest{
		Data:     imageData,
		MIMEType: mimeType,
		Prompt:   prompt,
	})
	if err != nil {
		return nil, fmt.Errorf("media/ocr: vision provider %q failed: %w", provider.ID(), err)
	}

	return decodeOCRResponse(result.Text, cfg), nil
}

// ---------------------------------------------------------------------------
// PerformOCR top-level function
// ---------------------------------------------------------------------------

// PerformOCR extracts text from image data using the provided OCR provider.
// It applies pre-processing (resize for vision) and returns the OCR result.
func PerformOCR(ctx context.Context, provider OCRProvider, imageData []byte, mimeType string, cfg OCRConfig) (*OCRResult, error) {
	if len(imageData) == 0 {
		return nil, fmt.Errorf("media/ocr: image data is required")
	}

	if cfg.OutputFormat == "" {
		cfg.OutputFormat = OCRFormatText
	}

	// Pre-process: resize image for optimal vision model input.
	if DetectKind(mimeType) == KindImage {
		resized, newMIME, err := ResizeForVision(imageData, mimeType)
		if err == nil {
			imageData = resized
			mimeType = newMIME
		}
		// On resize error, proceed with original data.
	}

	return provider.PerformOCR(ctx, imageData, mimeType, cfg)
}

// ---------------------------------------------------------------------------
// PerformOCRMultiPage
// ---------------------------------------------------------------------------

// PerformOCRMultiPage processes multiple image pages sequentially and
// concatenates the results. Each page is a separate image (e.g., from
// a scanned document).
func PerformOCRMultiPage(ctx context.Context, provider OCRProvider, pages [][]byte, mimeType string, cfg OCRConfig) (*OCRResult, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("media/ocr: at least one page is required")
	}
	if len(pages) > ocrMaxPageCount {
		return nil, fmt.Errorf("media/ocr: page count %d exceeds maximum of %d", len(pages), ocrMaxPageCount)
	}

	var allText strings.Builder
	var totalConfidence float64
	var confidenceCount int

	for i, pageData := range pages {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("media/ocr: cancelled at page %d: %w", i+1, ctx.Err())
		default:
		}

		result, err := PerformOCR(ctx, provider, pageData, mimeType, cfg)
		if err != nil {
			return nil, fmt.Errorf("media/ocr: page %d failed: %w", i+1, err)
		}

		if i > 0 {
			allText.WriteString("\n\n---\n\n")
		}
		allText.WriteString(result.Text)

		if result.Confidence > 0 {
			totalConfidence += result.Confidence
			confidenceCount++
		}
	}

	ocrResult := &OCRResult{
		Text: allText.String(),
	}
	if confidenceCount > 0 {
		ocrResult.Confidence = totalConfidence / float64(confidenceCount)
	}

	return ocrResult, nil
}

// ---------------------------------------------------------------------------
// OCR prompt builders
// ---------------------------------------------------------------------------

// buildOCRPrompt constructs an OCR-specific prompt based on the configuration.
func buildOCRPrompt(cfg OCRConfig) string {
	var b strings.Builder

	switch cfg.OutputFormat {
	case OCRFormatJSON:
		b.WriteString("Extract all text from this image. Return the result as a JSON object with a \"text\" field containing the full extracted text, and a \"regions\" array where each element has \"text\" for the text content. If you see tables, represent them as structured data.")
	case OCRFormatMarkdown:
		b.WriteString("Extract all text from this image and format the output as markdown. Preserve the document structure: use headings for titles, bullet points for lists, and markdown tables for tabular data. Maintain the reading order.")
	default:
		b.WriteString("Extract all visible text from this image exactly as it appears. Preserve the reading order and layout as much as possible. Include all text, numbers, and labels visible in the image.")
	}

	if cfg.LanguageHint != "" {
		b.WriteString(fmt.Sprintf(" The text is expected to be in %s.", cfg.LanguageHint))
	}

	return b.String()
}

func decodeOCRResponse(text string, cfg OCRConfig) *OCRResult {
	out := &OCRResult{
		Text: strings.TrimSpace(text),
	}
	if cfg.OutputFormat != OCRFormatJSON {
		return out
	}

	structured := parseStructuredOutput(text)
	obj, ok := structured.(map[string]any)
	if !ok {
		return out
	}

	if extracted, ok := obj["text"].(string); ok && strings.TrimSpace(extracted) != "" {
		out.Text = strings.TrimSpace(extracted)
	}
	if regions, ok := obj["regions"].([]any); ok {
		out.Regions = decodeOCRRegions(regions)
	}
	if confidence, ok := floatFromAny(obj["confidence"]); ok {
		out.Confidence = confidence
	}
	if language, ok := obj["language_detected"].(string); ok && strings.TrimSpace(language) != "" {
		out.LanguageDetected = strings.TrimSpace(language)
	}
	return out
}

func decodeOCRRegions(items []any) []OCRRegion {
	if len(items) == 0 {
		return nil
	}
	out := make([]OCRRegion, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		region := OCRRegion{}
		if text, ok := obj["text"].(string); ok {
			region.Text = strings.TrimSpace(text)
		}
		if confidence, ok := floatFromAny(obj["confidence"]); ok {
			region.Confidence = confidence
		}
		if value, ok := intFromAny(obj["x"]); ok {
			region.X = value
		}
		if value, ok := intFromAny(obj["y"]); ok {
			region.Y = value
		}
		if value, ok := intFromAny(obj["width"]); ok {
			region.Width = value
		}
		if value, ok := intFromAny(obj["height"]); ok {
			region.Height = value
		}
		if region.Text == "" && region.Confidence == 0 && region.X == 0 && region.Y == 0 && region.Width == 0 && region.Height == 0 {
			continue
		}
		out = append(out, region)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func floatFromAny(value any) (float64, bool) {
	switch current := value.(type) {
	case float64:
		return current, true
	case float32:
		return float64(current), true
	case int:
		return float64(current), true
	case int32:
		return float64(current), true
	case int64:
		return float64(current), true
	default:
		return 0, false
	}
}

func intFromAny(value any) (int, bool) {
	switch current := value.(type) {
	case int:
		return current, true
	case int32:
		return int(current), true
	case int64:
		return int(current), true
	case float64:
		return int(current), true
	case float32:
		return int(current), true
	default:
		return 0, false
	}
}
