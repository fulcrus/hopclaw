package media

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Multi-image comparison constants
// ---------------------------------------------------------------------------

const (
	// maxCompareImages is the maximum number of images that can be compared
	// in a single request.
	maxCompareImages = 10

	// minCompareImages is the minimum number of images required for comparison.
	minCompareImages = 2
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	// ErrTooFewImages is returned when fewer than 2 images are provided for comparison.
	ErrTooFewImages = fmt.Errorf("at least %d images are required for comparison", minCompareImages)

	// ErrTooManyImages is returned when more than maxCompareImages are provided.
	ErrTooManyImages = fmt.Errorf("at most %d images are allowed for comparison", maxCompareImages)
)

// ---------------------------------------------------------------------------
// Request and result types
// ---------------------------------------------------------------------------

// ImageInput describes a single image for multi-image comparison.
type ImageInput struct {
	// Data holds the raw image bytes.
	Data []byte `json:"-"`
	// MIMEType is the MIME type of the image.
	MIMEType string `json:"mime_type,omitempty"`
	// Label identifies this image in the comparison (e.g., "before", "after").
	Label string `json:"label,omitempty"`
}

// ComparisonResult holds the output from comparing multiple images.
type ComparisonResult struct {
	// Similarities describes what the images have in common.
	Similarities []string `json:"similarities"`
	// Differences describes what has changed between the images.
	Differences []string `json:"differences"`
	// Summary is a brief overall assessment.
	Summary string `json:"summary"`
	// Provider is the ID of the provider that performed the comparison.
	Provider string `json:"provider"`
	// Model is the model identifier used by the provider.
	Model string `json:"model,omitempty"`
	// DurationMs is the wall-clock duration in milliseconds.
	DurationMs int64 `json:"duration_ms"`
}

// ---------------------------------------------------------------------------
// ImageComparer
// ---------------------------------------------------------------------------

// ImageComparer provides multi-image comparison capabilities. It uses a
// sequential single-image analysis approach with cross-reference prompting,
// since not all providers support multiple images in a single request.
type ImageComparer struct {
	registry *Registry
}

// NewImageComparer creates an ImageComparer backed by the given registry.
func NewImageComparer(registry *Registry) *ImageComparer {
	return &ImageComparer{registry: registry}
}

// ---------------------------------------------------------------------------
// CompareImages
// ---------------------------------------------------------------------------

// CompareImages analyzes multiple images and produces a comparison result.
// It sends each image individually for description, then synthesizes the
// results into a structured comparison using a final cross-reference prompt.
func (c *ImageComparer) CompareImages(ctx context.Context, images []ImageInput, compType ComparisonType, detail DetailLevel) (*ComparisonResult, error) {
	if len(images) < minCompareImages {
		return nil, fmt.Errorf("media/vision: %w", ErrTooFewImages)
	}
	if len(images) > maxCompareImages {
		return nil, fmt.Errorf("media/vision: %w", ErrTooManyImages)
	}

	if detail == "" {
		detail = defaultAnalysisDetail
	}

	provider, err := c.registry.FindImageProvider("")
	if err != nil {
		return nil, fmt.Errorf("media/vision: %w", err)
	}

	start := time.Now()

	// Phase 1: Describe each image individually.
	descriptions := make([]string, len(images))
	for i, img := range images {
		label := img.Label
		if label == "" {
			label = fmt.Sprintf("Image %d", i+1)
		}

		mimeType := img.MIMEType
		if mimeType == "" && len(img.Data) > 0 {
			mimeType = DetectMIMETypeFromBytes(img.Data)
		}

		prompt := fmt.Sprintf("Describe this image in detail. This image is labeled %q. Provide a thorough description covering all visible elements, text, layout, and any notable features.", label)

		result, descErr := provider.DescribeImage(ctx, ImageRequest{
			Data:     img.Data,
			MIMEType: mimeType,
			Prompt:   prompt,
		})
		if descErr != nil {
			return nil, fmt.Errorf("media/vision: describing image %q: %w", label, descErr)
		}
		descriptions[i] = fmt.Sprintf("[%s]: %s", label, result.Text)
	}

	// Phase 2: Synthesize comparison from individual descriptions.
	labels := make([]string, len(images))
	for i, img := range images {
		if img.Label != "" {
			labels[i] = img.Label
		} else {
			labels[i] = fmt.Sprintf("Image %d", i+1)
		}
	}

	synthesisPrompt := c.buildSynthesisPrompt(descriptions, labels, compType, detail)

	// Use the first image as the visual anchor for the synthesis call.
	firstMIME := images[0].MIMEType
	if firstMIME == "" && len(images[0].Data) > 0 {
		firstMIME = DetectMIMETypeFromBytes(images[0].Data)
	}

	synthResult, err := provider.DescribeImage(ctx, ImageRequest{
		Data:     images[0].Data,
		MIMEType: firstMIME,
		Prompt:   synthesisPrompt,
	})
	if err != nil {
		return nil, fmt.Errorf("media/vision: synthesizing comparison: %w", err)
	}

	// Parse the synthesis result.
	comparison := c.parseComparisonResult(synthResult.Text)
	comparison.Provider = provider.ID()
	comparison.Model = synthResult.Model
	comparison.DurationMs = time.Since(start).Milliseconds()

	return comparison, nil
}

// ---------------------------------------------------------------------------
// Prompt and parsing helpers
// ---------------------------------------------------------------------------

// buildSynthesisPrompt constructs the cross-reference comparison prompt.
func (c *ImageComparer) buildSynthesisPrompt(descriptions, labels []string, compType ComparisonType, detail DetailLevel) string {
	var sb strings.Builder
	sb.WriteString("I have described multiple images individually. Based on these descriptions, provide a comparison.\n\n")

	for _, desc := range descriptions {
		sb.WriteString(desc)
		sb.WriteString("\n\n")
	}

	comparePrompt := BuildComparisonPrompt(labels, compType, detail, FormatJSON, "")
	sb.WriteString(comparePrompt)

	sb.WriteString("\n\nFormat your response as JSON:\n")
	sb.WriteString(`{"similarities": ["..."], "differences": ["..."], "summary": "..."}`)

	return sb.String()
}

// parseComparisonResult attempts to parse the model's response into a
// structured ComparisonResult. Falls back to placing the raw text in the
// Summary field if parsing fails.
func (c *ImageComparer) parseComparisonResult(text string) *ComparisonResult {
	structured := parseStructuredOutput(text)
	if structured == nil {
		return &ComparisonResult{
			Summary: text,
		}
	}

	obj, ok := structured.(map[string]any)
	if !ok {
		return &ComparisonResult{
			Summary: text,
		}
	}

	result := &ComparisonResult{}

	if sims, ok := obj["similarities"].([]any); ok {
		for _, s := range sims {
			if str, ok := s.(string); ok {
				result.Similarities = append(result.Similarities, str)
			}
		}
	}

	if diffs, ok := obj["differences"].([]any); ok {
		for _, d := range diffs {
			if str, ok := d.(string); ok {
				result.Differences = append(result.Differences, str)
			}
		}
	}

	if summary, ok := obj["summary"].(string); ok {
		result.Summary = summary
	} else {
		result.Summary = text
	}

	return result
}
